package persona

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"
)

// The two vanilla player geometries a rebuilt persona is published under. Using
// a stock geometry rather than emitting a custom one is deliberate: a custom
// geometry the client rejects for any reason makes it fall back to the default
// skin entirely, whereas these two are the same models every ordinary skin
// already uses and cannot be rejected.
const (
	GeometryWide = "geometry.humanoid.custom"
	GeometrySlim = "geometry.humanoid.customSlim"
)

// epsilon is the tolerance used when comparing the floating point sizes read
// out of the glTF against the exact integers a Minecraft cube is built from.
// The export stores coordinates as float32, so exact equality never holds.
const epsilon = 0.001

// Face names as Bedrock geometry spells them, paired with the axis direction
// each one points in. The pairing is not guessed: it was derived by taking a
// persona export of a plain body cube, whose UV rectangle follows the standard
// Minecraft box unwrap exactly, and matching every unwrap slot against the
// vertex normal the export gives that slot.
var faceNames = map[[3]int]string{
	{0, 1, 0}:  "up",
	{0, -1, 0}: "down",
	{-1, 0, 0}: "east",
	{1, 0, 0}:  "west",
	{0, 0, 1}:  "north",
	{0, 0, -1}: "south",
}

// standardSlots maps each vanilla player bone onto the top left corner of the
// region it occupies in the classic 64x64 skin layout.
//
// A persona export packs the same regions, at the same sizes, but in its own
// order and spread over several images, so rebuilding a normal skin out of one
// is a matter of moving each region to where the stock geometry expects it.
var standardSlots = map[string]image.Point{
	"head": {X: 0, Y: 0},
	"hat":  {X: 32, Y: 0},

	"rightLeg": {X: 0, Y: 16},
	"body":     {X: 16, Y: 16},
	"rightArm": {X: 40, Y: 16},

	"rightPants":  {X: 0, Y: 32},
	"jacket":      {X: 16, Y: 32},
	"rightSleeve": {X: 40, Y: 32},

	"leftPants":  {X: 0, Y: 48},
	"leftLeg":    {X: 16, Y: 48},
	"leftArm":    {X: 32, Y: 48},
	"leftSleeve": {X: 48, Y: 48},
}

// Model is a persona skin rebuilt into an ordinary flat skin: a classic 64x64
// texture plus the name of the stock geometry that wraps it.
type Model struct {
	// Pix is the rebuilt skin texture as non-premultiplied RGBA bytes, laid out
	// exactly as skin.Skin expects.
	Pix           []uint8
	Width, Height int
	// Geometry names the stock player geometry the texture is worn on, either
	// GeometryWide or GeometrySlim.
	Geometry string
	// ArmSize is "wide" or "slim", derived from the width of the arm cubes in
	// the export rather than from anything the client claimed.
	ArmSize string
	// Skipped names the persona parts that have no place in a classic skin, such
	// as the extra decoration cubes a persona may wear. Reported so that what a
	// rebuilt skin loses is visible in the logs rather than silent.
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

// Build converts a persona GLB export into a classic skin texture. It does no
// network access of its own: b is the raw GLB body.
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

	tex, unplaced, err := packStandardSkin(g, parts)
	if err != nil {
		return nil, err
	}
	skipped = append(skipped, unplaced...)

	size := armSize(parts)
	geom := GeometryWide
	if size == "slim" {
		geom = GeometrySlim
	}
	return &Model{
		Pix:      tex.Pix,
		Width:    tex.Bounds().Dx(),
		Height:   tex.Bounds().Dy(),
		Geometry: geom,
		ArmSize:  size,
		Skipped:  skipped,
	}, nil
}

// packStandardSkin moves every part of the export into the place the classic
// 64x64 skin layout keeps it, returning the rebuilt texture along with the
// names of the parts that had nowhere to go.
func packStandardSkin(g *glb, parts []part) (*image.NRGBA, []string, error) {
	images, err := decodeImages(g, parts)
	if err != nil {
		return nil, nil, err
	}

	out := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	var unplaced []string
	var placed int
	for _, p := range parts {
		if !p.box.set || p.image < 0 {
			// A pivot-only node such as the root or the waist, carrying no
			// geometry and therefore no texture either.
			continue
		}
		slot, ok := standardSlots[p.name]
		if !ok {
			unplaced = append(unplaced, p.name)
			continue
		}
		src, ok := images[p.image]
		if !ok {
			unplaced = append(unplaced, p.name)
			continue
		}
		w := int(math.Round(p.uv.max[0] - p.uv.min[0]))
		h := int(math.Round(p.uv.max[1] - p.uv.min[1]))
		if w <= 0 || h <= 0 {
			unplaced = append(unplaced, p.name)
			continue
		}
		from := image.Rect(
			int(math.Round(p.uv.min[0])), int(math.Round(p.uv.min[1])),
			int(math.Round(p.uv.min[0]))+w, int(math.Round(p.uv.min[1]))+h,
		).Intersect(src.Bounds())
		if from.Empty() {
			unplaced = append(unplaced, p.name)
			continue
		}
		draw.Draw(out, image.Rect(slot.X, slot.Y, slot.X+from.Dx(), slot.Y+from.Dy()), src, from.Min, draw.Src)
		placed++
	}
	if placed == 0 {
		return nil, nil, fmt.Errorf("persona export has no parts a classic skin can hold")
	}
	return out, unplaced, nil
}

// decodeImages decodes every texture the parts sample, once each.
func decodeImages(g *glb, parts []part) (map[int]image.Image, error) {
	images := map[int]image.Image{}
	for _, p := range parts {
		if p.image < 0 {
			continue
		}
		if _, ok := images[p.image]; ok {
			continue
		}
		b, err := g.imageData(p.image)
		if err != nil {
			return nil, err
		}
		img, err := png.Decode(bytes.NewReader(b))
		if err != nil {
			return nil, fmt.Errorf("decode persona texture %v: %w", p.image, err)
		}
		images[p.image] = img
	}
	return images, nil
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
// uses, taken from the arm cubes themselves so it matches the texture rather
// than whatever the client claimed.
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

// isIdentityQuat reports whether a glTF rotation quaternion leaves the node
// unrotated, within the float32 precision the export stores it at.
func isIdentityQuat(q []float64) bool {
	return math.Abs(q[0]) < epsilon && math.Abs(q[1]) < epsilon &&
		math.Abs(q[2]) < epsilon && math.Abs(math.Abs(q[3])-1) < epsilon
}

// round trims the float32 noise the export carries so positions hold the exact
// quarter pixel values a Minecraft model is built from.
func round(v float64) float64 {
	return math.Round(v*10000) / 10000
}

func roundVec(v [3]float64) [3]float64 {
	return [3]float64{round(v[0]), round(v[1]), round(v[2])}
}
