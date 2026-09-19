package persona

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"
	"sort"
)

// GeometryIdentifier is the model name the rebuilt geometry is published
// under. It is referenced from the ModelConfig of the rebuilt skin, so it has
// to match what Build writes into the geometry JSON.
const GeometryIdentifier = "geometry.persona_resolved"

// epsilon is the tolerance used when comparing the floating point sizes read
// out of the glTF against the exact integers a Minecraft cube is built from.
// The export stores coordinates as float32, so exact equality never holds.
const epsilon = 0.001

// Face names as Bedrock geometry spells them, paired with the axis direction
// each one points in. The pairing is not guessed: it was derived by taking a
// persona export of a plain body cube, whose UV rectangle follows the standard
// Minecraft box unwrap exactly, and matching every unwrap slot against the
// vertex normal the export gives that slot. That yielded up=+Y, down=-Y,
// east=-X, west=+X, north=+Z and south=-Z.
var faceNames = map[[3]int]string{
	{0, 1, 0}:  "up",
	{0, -1, 0}: "down",
	{-1, 0, 0}: "east",
	{1, 0, 0}:  "west",
	{0, 0, 1}:  "north",
	{0, 0, -1}: "south",
}

// Model is a persona skin rebuilt into the two things a Minecraft client needs
// to render a player without any marketplace content of its own: one flat
// texture and one geometry describing how that texture wraps onto boxes.
type Model struct {
	// Pix is the packed texture atlas as non-premultiplied RGBA bytes, laid
	// out exactly as skin.Skin expects.
	Pix           []uint8
	Width, Height int
	// Geometry is the encoded Bedrock geometry JSON defining GeometryIdentifier.
	Geometry []byte
	// ArmSize is "wide" or "slim", derived from the width of the arm cubes in
	// the export rather than from anything the client claimed.
	ArmSize string
	// Skipped names the bones that could not be represented as Minecraft cubes
	// and were left out of the geometry. Reported so a persona using a shape
	// this converter cannot express shows up in the logs instead of silently
	// losing a body part.
	Skipped []string
}

// bounds3 is an axis aligned bounding box accumulated over a set of vertices.
type bounds3 struct {
	min, max [3]float64
	set      bool
}

func (b *bounds3) add(v []float64) {
	b.addPoint([3]float64{v[0], v[1], v[2]})
}

// addPoint extends the box to contain the point passed.
func (b *bounds3) addPoint(v [3]float64) {
	for i := 0; i < 3; i++ {
		if !b.set || v[i] < b.min[i] {
			b.min[i] = v[i]
		}
		if !b.set || v[i] > b.max[i] {
			b.max[i] = v[i]
		}
	}
	b.set = true
}

// bounds2 is the two dimensional equivalent of bounds3, used for UV rectangles.
type bounds2 struct {
	min, max [2]float64
	set      bool
}

func (b *bounds2) add(u, v float64) {
	if !b.set || u < b.min[0] {
		b.min[0] = u
	}
	if !b.set || u > b.max[0] {
		b.max[0] = u
	}
	if !b.set || v < b.min[1] {
		b.min[1] = v
	}
	if !b.set || v > b.max[1] {
		b.max[1] = v
	}
	b.set = true
}

// part is one meshed glTF node reduced to the box it describes.
type part struct {
	name, parent string
	pivot        [3]float64
	box          bounds3
	uv           bounds2 // over the whole cube, in source texture pixels
	faces        map[string]bounds2
	image        int // index into gltfDoc.Images
}

// geometryFile and the types below it mirror the Bedrock geometry JSON schema
// closely enough to encode a model the client will accept.
type geometryFile struct {
	FormatVersion string     `json:"format_version"`
	Geometry      []geometry `json:"minecraft:geometry"`
}

type geometry struct {
	Description geometryDescription `json:"description"`
	Bones       []bone              `json:"bones"`
}

type geometryDescription struct {
	Identifier          string     `json:"identifier"`
	TextureWidth        int        `json:"texture_width"`
	TextureHeight       int        `json:"texture_height"`
	VisibleBoundsWidth  float64    `json:"visible_bounds_width"`
	VisibleBoundsHeight float64    `json:"visible_bounds_height"`
	VisibleBoundsOffset [3]float64 `json:"visible_bounds_offset"`
}

type bone struct {
	Name   string     `json:"name"`
	Parent string     `json:"parent,omitempty"`
	Pivot  [3]float64 `json:"pivot"`
	Cubes  []cube     `json:"cubes,omitempty"`
}

type cube struct {
	Origin  [3]float64 `json:"origin"`
	Size    [3]float64 `json:"size"`
	UV      any        `json:"uv"`
	Inflate float64    `json:"inflate,omitempty"`
}

// faceUV is the explicit per face form of a cube UV, used for cubes whose
// unwrap does not follow the standard Minecraft box layout.
type faceUV struct {
	UV     [2]float64 `json:"uv"`
	UVSize [2]float64 `json:"uv_size"`
}

// Build converts a persona GLB export into a flat texture plus geometry. It
// does no network access of its own: b is the raw GLB body.
func Build(b []byte) (*Model, error) {
	g, err := parseGLB(b)
	if err != nil {
		return nil, err
	}
	parts, skipped, err := collectParts(g)
	if err != nil {
		return nil, err
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("persona export contains no usable boxes")
	}

	atlas, offsets, err := packAtlas(g, parts)
	if err != nil {
		return nil, err
	}
	size := atlas.Bounds().Dx()

	bones, model := buildBones(parts, offsets)
	enc, err := json.Marshal(geometryFile{
		FormatVersion: "1.12.0",
		Geometry: []geometry{{
			Description: geometryDescription{
				Identifier:    GeometryIdentifier,
				TextureWidth:  size,
				TextureHeight: atlas.Bounds().Dy(),
				// Bedrock culls an entity once its visible bounds leave the
				// screen, so these are taken from the real extent of the model
				// with a little room to spare rather than hardcoded, in case a
				// persona is taller or wider than a vanilla player.
				VisibleBoundsWidth:  round(math.Max(3, maxSpan(model)/16+1)),
				VisibleBoundsHeight: round(math.Max(3.5, (model.max[1]-model.min[1])/16+1)),
				VisibleBoundsOffset: [3]float64{0, round((model.max[1] + model.min[1]) / 32), 0},
			},
			Bones: bones,
		}},
	})
	if err != nil {
		return nil, fmt.Errorf("encode geometry: %w", err)
	}

	return &Model{
		Pix:      atlas.Pix,
		Width:    atlas.Bounds().Dx(),
		Height:   atlas.Bounds().Dy(),
		Geometry: enc,
		ArmSize:  armSize(parts),
		Skipped:  skipped,
	}, nil
}

// collectParts walks the node tree, accumulating the world space pivot of every
// node along the way, and reduces each meshed node to the box it describes.
func collectParts(g *glb) ([]part, []string, error) {
	nodes := g.doc.Nodes
	parent := make(map[int]int, len(nodes))
	for i, n := range nodes {
		for _, c := range n.Children {
			if c >= 0 && c < len(nodes) {
				parent[c] = i
			}
		}
	}
	// pivot walks up to the root summing translations. Rotation and scale are
	// not applied: no persona export observed so far uses either, and silently
	// dropping a rotation would place a box wrongly, so any node carrying one
	// is skipped and reported instead.
	pivot := func(i int) ([3]float64, bool) {
		var p [3]float64
		for {
			n := nodes[i]
			if len(n.Rotation) == 4 && !isIdentityQuat(n.Rotation) {
				return p, false
			}
			if len(n.Scale) == 3 && (n.Scale[0] != 1 || n.Scale[1] != 1 || n.Scale[2] != 1) {
				return p, false
			}
			if len(n.Translation) == 3 {
				for k := 0; k < 3; k++ {
					p[k] += n.Translation[k]
				}
			}
			up, ok := parent[i]
			if !ok {
				return p, true
			}
			i = up
		}
	}
	// A bone may only name a parent that itself became a bone. Persona exports
	// put every mesh on its own node, but a node without a mesh (the root and
	// the waist) still has to appear as a pivot-only bone so the chain holds.
	name := func(i int) string {
		if n := nodes[i].Name; n != "" {
			return n
		}
		return fmt.Sprintf("bone%d", i)
	}

	var parts []part
	var skipped []string
	for i, n := range nodes {
		p, ok := pivot(i)
		if !ok {
			skipped = append(skipped, name(i)+" (rotated or scaled node)")
			continue
		}
		pt := part{name: name(i), pivot: roundVec(p), image: -1}
		if up, ok := parent[i]; ok {
			pt.parent = name(up)
		}
		if n.Mesh == nil {
			parts = append(parts, pt)
			continue
		}
		if *n.Mesh < 0 || *n.Mesh >= len(g.doc.Meshes) {
			skipped = append(skipped, pt.name+" (mesh out of range)")
			continue
		}
		prims := g.doc.Meshes[*n.Mesh].Primitives
		if len(prims) != 1 {
			skipped = append(skipped, fmt.Sprintf("%v (%v primitives)", pt.name, len(prims)))
			continue
		}
		if err := readBox(g, prims[0], &pt); err != nil {
			skipped = append(skipped, fmt.Sprintf("%v (%v)", pt.name, err))
			continue
		}
		parts = append(parts, pt)
	}
	return parts, skipped, nil
}

// readBox reads a primitive into pt, bucketing its vertices by face normal. A
// primitive only qualifies as a Minecraft cube if every normal is axis aligned
// and all six faces are present.
func readBox(g *glb, prim gltfPrimitive, pt *part) error {
	posAcc, ok := prim.Attributes["POSITION"]
	if !ok {
		return fmt.Errorf("no POSITION")
	}
	uvAcc, ok := prim.Attributes["TEXCOORD_0"]
	if !ok {
		return fmt.Errorf("no TEXCOORD_0")
	}
	normAcc, ok := prim.Attributes["NORMAL"]
	if !ok {
		return fmt.Errorf("no NORMAL")
	}
	pos, err := g.readAccessor(posAcc)
	if err != nil {
		return err
	}
	uv, err := g.readAccessor(uvAcc)
	if err != nil {
		return err
	}
	norm, err := g.readAccessor(normAcc)
	if err != nil {
		return err
	}
	if len(pos) != len(uv) || len(pos) != len(norm) {
		return fmt.Errorf("attribute length mismatch")
	}

	order := make([]int, len(pos))
	for i := range order {
		order[i] = i
	}
	if prim.Indices != nil {
		idx, err := g.readAccessor(*prim.Indices)
		if err != nil {
			return err
		}
		order = order[:0]
		for _, v := range idx {
			order = append(order, int(v[0]))
		}
	}

	if prim.Material == nil {
		return fmt.Errorf("no material")
	}
	img, err := g.textureOfMaterial(*prim.Material)
	if err != nil {
		return err
	}
	pt.image = img
	pt.faces = make(map[string]bounds2, 6)

	texW, texH, err := imageSize(g, img)
	if err != nil {
		return err
	}
	for _, i := range order {
		if i < 0 || i >= len(pos) {
			return fmt.Errorf("index out of range")
		}
		face, ok := faceNames[axis(norm[i])]
		if !ok {
			return fmt.Errorf("non axis aligned normal")
		}
		pt.box.add(pos[i])
		u, v := uv[i][0]*float64(texW), uv[i][1]*float64(texH)
		pt.uv.add(u, v)
		f := pt.faces[face]
		f.add(u, v)
		pt.faces[face] = f
	}
	if len(pt.faces) != 6 {
		return fmt.Errorf("%v faces, not 6", len(pt.faces))
	}
	return nil
}

// axis snaps a normal onto the closest unit axis, reporting the zero vector for
// a normal that is not axis aligned so readBox can reject it.
func axis(n []float64) [3]int {
	var out [3]int
	for i := 0; i < 3; i++ {
		switch {
		case n[i] > 1-epsilon:
			out[i] = 1
		case n[i] < -1+epsilon:
			out[i] = -1
		case math.Abs(n[i]) > epsilon:
			return [3]int{}
		}
	}
	return out
}

// buildBones turns the collected parts into geometry bones, returning the bones
// and the overall model bounds used for the visible bounds of the geometry.
func buildBones(parts []part, offsets map[int]image.Point) ([]bone, bounds3) {
	var model bounds3
	bones := make([]bone, 0, len(parts)+2)
	var haveLeftArm, haveRightArm, haveLeftItem, haveRightItem bool
	for _, p := range parts {
		b := bone{Name: p.name, Parent: p.parent, Pivot: p.pivot}
		switch p.name {
		case "leftArm":
			haveLeftArm = true
		case "rightArm":
			haveRightArm = true
		case "leftItem":
			haveLeftItem = true
		case "rightItem":
			haveRightItem = true
		}
		if p.box.set {
			off := offsets[p.image]
			c := makeCube(p, off)
			b.Cubes = []cube{c}
			model.addPoint([3]float64{
				p.pivot[0] + p.box.min[0], p.pivot[1] + p.box.min[1], p.pivot[2] + p.box.min[2],
			})
			model.addPoint([3]float64{
				p.pivot[0] + p.box.max[0], p.pivot[1] + p.box.max[1], p.pivot[2] + p.box.max[2],
			})
		}
		bones = append(bones, b)
	}
	// A persona export carries no item bones, but the client positions a held
	// item on them. Without these two bones anything the player holds renders
	// at the model origin, so they are added at the vanilla humanoid pivots.
	if haveRightArm && !haveRightItem {
		bones = append(bones, bone{Name: "rightItem", Parent: "rightArm", Pivot: [3]float64{-6, 15, 1}})
	}
	if haveLeftArm && !haveLeftItem {
		bones = append(bones, bone{Name: "leftItem", Parent: "leftArm", Pivot: [3]float64{6, 15, 1}})
	}
	return bones, model
}

// makeCube converts one part into a cube. Where the UV rectangle matches the
// standard Minecraft box unwrap the short form is used, which lets the client
// derive every face itself and keeps the geometry close to what a normal skin
// would contain. The outer layers of a persona (the jacket, sleeves and hat)
// are modelled as slightly larger boxes sharing the UV rectangle of the layer
// underneath, which is exactly what the inflate field expresses, so the
// inflation is recovered from the mismatch between the cube size and its UV
// rectangle. Anything that does not fit that pattern, such as a flat decal
// piece with its own packing, falls back to explicit per face UVs.
func makeCube(p part, off image.Point) cube {
	size := [3]float64{
		p.box.max[0] - p.box.min[0],
		p.box.max[1] - p.box.min[1],
		p.box.max[2] - p.box.min[2],
	}
	origin := [3]float64{
		p.pivot[0] + p.box.min[0],
		p.pivot[1] + p.box.min[1],
		p.pivot[2] + p.box.min[2],
	}
	uvW, uvH := p.uv.max[0]-p.uv.min[0], p.uv.max[1]-p.uv.min[1]

	// Solving uvH == h+d and size == base+2*inflate for the inflation.
	inflate := (size[1] + size[2] - uvH) / 4
	base := [3]float64{size[0] - 2*inflate, size[1] - 2*inflate, size[2] - 2*inflate}
	standard := inflate > -epsilon &&
		base[0] > epsilon && base[1] > epsilon && base[2] > epsilon &&
		math.Abs(2*(base[0]+base[2])-uvW) < epsilon

	if standard {
		return cube{
			Origin:  roundVec([3]float64{origin[0] + inflate, origin[1] + inflate, origin[2] + inflate}),
			Size:    roundVec(base),
			UV:      [2]float64{round(p.uv.min[0] + float64(off.X)), round(p.uv.min[1] + float64(off.Y))},
			Inflate: round(inflate),
		}
	}

	faces := make(map[string]faceUV, len(p.faces))
	for name, f := range p.faces {
		faces[name] = faceUV{
			UV:     [2]float64{round(f.min[0] + float64(off.X)), round(f.min[1] + float64(off.Y))},
			UVSize: [2]float64{round(f.max[0] - f.min[0]), round(f.max[1] - f.min[1])},
		}
	}
	return cube{Origin: roundVec(origin), Size: roundVec(size), UV: faces}
}

// packAtlas decodes every texture the model samples and packs them into a
// single square power of two atlas, since a Bedrock geometry samples exactly
// one texture while a persona export splits itself across several.
func packAtlas(g *glb, parts []part) (*image.NRGBA, map[int]image.Point, error) {
	used := map[int]image.Image{}
	for _, p := range parts {
		if p.image < 0 {
			continue
		}
		if _, ok := used[p.image]; ok {
			continue
		}
		b, err := g.imageData(p.image)
		if err != nil {
			return nil, nil, err
		}
		img, err := png.Decode(bytes.NewReader(b))
		if err != nil {
			return nil, nil, fmt.Errorf("decode persona texture %v: %w", p.image, err)
		}
		used[p.image] = img
	}
	if len(used) == 0 {
		return nil, nil, fmt.Errorf("persona export has no textures")
	}

	order := make([]int, 0, len(used))
	for i := range used {
		order = append(order, i)
	}
	// Tallest first, so the shelves a row based packer produces stay tight.
	sort.Slice(order, func(a, b int) bool {
		ha, hb := used[order[a]].Bounds().Dy(), used[order[b]].Bounds().Dy()
		if ha != hb {
			return ha > hb
		}
		return order[a] < order[b]
	})

	for _, size := range []int{64, 128, 256, 512, 1024} {
		offsets, ok := shelfPack(used, order, size)
		if !ok {
			continue
		}
		atlas := image.NewNRGBA(image.Rect(0, 0, size, size))
		for i, off := range offsets {
			src := used[i]
			draw.Draw(atlas, src.Bounds().Add(off), src, src.Bounds().Min, draw.Src)
		}
		return atlas, offsets, nil
	}
	return nil, nil, fmt.Errorf("persona textures do not fit a 1024x1024 atlas")
}

// shelfPack lays the images out in rows of decreasing height, reporting whether
// they all fit within a square of the size passed.
func shelfPack(images map[int]image.Image, order []int, size int) (map[int]image.Point, bool) {
	offsets := make(map[int]image.Point, len(order))
	x, y, shelf := 0, 0, 0
	for _, i := range order {
		b := images[i].Bounds()
		w, h := b.Dx(), b.Dy()
		if w > size {
			return nil, false
		}
		if x+w > size {
			x, y = 0, y+shelf
			shelf = 0
		}
		if y+h > size {
			return nil, false
		}
		offsets[i] = image.Pt(x, y)
		x += w
		if h > shelf {
			shelf = h
		}
	}
	return offsets, true
}

// imageSize reads the width and height out of the IHDR of an embedded PNG
// without decoding the pixels, which is all that is needed to turn normalised
// UVs into texture pixels while collecting parts.
func imageSize(g *glb, index int) (int, int, error) {
	b, err := g.imageData(index)
	if err != nil {
		return 0, 0, err
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return 0, 0, fmt.Errorf("decode persona texture %v header: %w", index, err)
	}
	return cfg.Width, cfg.Height, nil
}

// armSize reports the arm width of the model in the same vocabulary the client
// uses, taken from the arm cubes themselves so it matches the geometry rather
// than whatever the client claimed in its login data.
func armSize(parts []part) string {
	for _, p := range parts {
		if p.name != "leftArm" && p.name != "rightArm" {
			continue
		}
		if !p.box.set {
			continue
		}
		if p.box.max[0]-p.box.min[0] < 3.5 {
			return "slim"
		}
		return "wide"
	}
	return "wide"
}

// maxSpan returns the larger of the horizontal extents of the model.
func maxSpan(b bounds3) float64 {
	return math.Max(b.max[0]-b.min[0], b.max[2]-b.min[2])
}

// isIdentityQuat reports whether a glTF rotation quaternion leaves the node
// unrotated, within the float32 precision the export stores it at.
func isIdentityQuat(q []float64) bool {
	return math.Abs(q[0]) < epsilon && math.Abs(q[1]) < epsilon &&
		math.Abs(q[2]) < epsilon && math.Abs(math.Abs(q[3])-1) < epsilon
}

// round trims the float32 noise the export carries so the encoded geometry
// holds the exact quarter pixel values a Minecraft model is built from rather
// than values like 11.800000000000001.
func round(v float64) float64 {
	return math.Round(v*10000) / 10000
}

func roundVec(v [3]float64) [3]float64 {
	return [3]float64{round(v[0]), round(v[1]), round(v[2])}
}
