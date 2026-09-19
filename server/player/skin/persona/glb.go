package persona

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
)

// The binary glTF ("GLB") container magic and the two chunk types a GLB file is
// built out of. The Microsoft persona endpoint returns a GLB with exactly one
// of each: a JSON chunk holding the glTF document, and a BIN chunk holding
// every buffer the document refers to.
const (
	glbMagic      uint32 = 0x46546C67 // "glTF"
	chunkTypeJSON uint32 = 0x4E4F534A // "JSON"
	chunkTypeBIN  uint32 = 0x004E4942 // "BIN\0"
)

// Component types used by glTF accessors. Only the ones a persona export
// actually uses are handled; anything else is rejected rather than guessed at.
const (
	componentByte          = 5120
	componentUnsignedByte  = 5121
	componentShort         = 5122
	componentUnsignedShort = 5123
	componentUnsignedInt   = 5125
	componentFloat         = 5126
)

// gltfDoc is a deliberately partial view of the glTF document: only the fields
// needed to rebuild a Minecraft model out of the export are decoded, so a new
// field appearing upstream cannot break decoding.
type gltfDoc struct {
	Accessors   []gltfAccessor   `json:"accessors"`
	BufferViews []gltfBufferView `json:"bufferViews"`
	Images      []gltfImage      `json:"images"`
	Materials   []gltfMaterial   `json:"materials"`
	Textures    []gltfTexture    `json:"textures"`
	Meshes      []gltfMesh       `json:"meshes"`
	Nodes       []gltfNode       `json:"nodes"`
}

type gltfAccessor struct {
	BufferView    int    `json:"bufferView"`
	ByteOffset    int    `json:"byteOffset"`
	ComponentType int    `json:"componentType"`
	Count         int    `json:"count"`
	Type          string `json:"type"`
}

type gltfBufferView struct {
	ByteOffset int `json:"byteOffset"`
	ByteLength int `json:"byteLength"`
	ByteStride int `json:"byteStride"`
}

type gltfImage struct {
	BufferView int    `json:"bufferView"`
	MimeType   string `json:"mimeType"`
}

type gltfMaterial struct {
	PBR struct {
		BaseColourTexture struct {
			Index int `json:"index"`
		} `json:"baseColorTexture"`
	} `json:"pbrMetallicRoughness"`
}

type gltfTexture struct {
	Source int `json:"source"`
}

type gltfMesh struct {
	Primitives []gltfPrimitive `json:"primitives"`
}

type gltfPrimitive struct {
	Attributes map[string]int `json:"attributes"`
	Indices    *int           `json:"indices"`
	Material   *int           `json:"material"`
}

type gltfNode struct {
	Name        string    `json:"name"`
	Mesh        *int      `json:"mesh"`
	Children    []int     `json:"children"`
	Translation []float64 `json:"translation"`
	Rotation    []float64 `json:"rotation"`
	Scale       []float64 `json:"scale"`
}

// glb is a parsed GLB file: the decoded glTF document plus the raw binary chunk
// its buffer views index into.
type glb struct {
	doc gltfDoc
	bin []byte
}

// parseGLB reads the GLB container format: a 12 byte header (magic, version,
// total length) followed by length-prefixed chunks. Only the first JSON chunk
// and the first BIN chunk are kept, which is all the spec guarantees exist.
func parseGLB(b []byte) (*glb, error) {
	if len(b) < 12 {
		return nil, fmt.Errorf("glb too short: %v bytes", len(b))
	}
	if magic := binary.LittleEndian.Uint32(b); magic != glbMagic {
		return nil, fmt.Errorf("not a glb file: magic %#x", magic)
	}
	total := int(binary.LittleEndian.Uint32(b[8:]))
	if total > len(b) {
		total = len(b)
	}

	var g glb
	var haveJSON bool
	for off := 12; off+8 <= total; {
		length := int(binary.LittleEndian.Uint32(b[off:]))
		typ := binary.LittleEndian.Uint32(b[off+4:])
		off += 8
		if length < 0 || off+length > total {
			return nil, fmt.Errorf("glb chunk at %v overruns file", off)
		}
		switch {
		case typ == chunkTypeJSON && !haveJSON:
			if err := json.Unmarshal(b[off:off+length], &g.doc); err != nil {
				return nil, fmt.Errorf("decode gltf json: %w", err)
			}
			haveJSON = true
		case typ == chunkTypeBIN && g.bin == nil:
			g.bin = b[off : off+length]
		}
		off += length
	}
	if !haveJSON {
		return nil, fmt.Errorf("glb has no json chunk")
	}
	return &g, nil
}

// componentSize returns the byte width of a single accessor component.
func componentSize(componentType int) (int, error) {
	switch componentType {
	case componentByte, componentUnsignedByte:
		return 1, nil
	case componentShort, componentUnsignedShort:
		return 2, nil
	case componentUnsignedInt, componentFloat:
		return 4, nil
	}
	return 0, fmt.Errorf("unsupported gltf component type %v", componentType)
}

// componentCount returns how many components make up one element of an
// accessor of the type passed.
func componentCount(typ string) (int, error) {
	switch typ {
	case "SCALAR":
		return 1, nil
	case "VEC2":
		return 2, nil
	case "VEC3":
		return 3, nil
	case "VEC4":
		return 4, nil
	}
	return 0, fmt.Errorf("unsupported gltf accessor type %q", typ)
}

// readAccessor reads an accessor out of the binary chunk as float64 elements,
// honouring the byte stride of the buffer view so interleaved buffers are read
// correctly rather than assuming elements are tightly packed.
func (g *glb) readAccessor(index int) ([][]float64, error) {
	if index < 0 || index >= len(g.doc.Accessors) {
		return nil, fmt.Errorf("accessor %v out of range", index)
	}
	a := g.doc.Accessors[index]
	if a.BufferView < 0 || a.BufferView >= len(g.doc.BufferViews) {
		return nil, fmt.Errorf("buffer view %v out of range", a.BufferView)
	}
	bv := g.doc.BufferViews[a.BufferView]

	size, err := componentSize(a.ComponentType)
	if err != nil {
		return nil, err
	}
	n, err := componentCount(a.Type)
	if err != nil {
		return nil, err
	}
	stride := bv.ByteStride
	if stride == 0 {
		stride = size * n
	}
	base := bv.ByteOffset + a.ByteOffset
	if base < 0 || a.Count < 0 {
		return nil, fmt.Errorf("accessor %v has a negative offset or count", index)
	}
	if need := base + (a.Count-1)*stride + size*n; a.Count > 0 && need > len(g.bin) {
		return nil, fmt.Errorf("accessor %v overruns bin chunk (%v > %v)", index, need, len(g.bin))
	}

	out := make([][]float64, a.Count)
	for i := range out {
		el := make([]float64, n)
		for c := range el {
			o := base + i*stride + c*size
			switch a.ComponentType {
			case componentFloat:
				el[c] = float64(math.Float32frombits(binary.LittleEndian.Uint32(g.bin[o:])))
			case componentUnsignedInt:
				el[c] = float64(binary.LittleEndian.Uint32(g.bin[o:]))
			case componentUnsignedShort:
				el[c] = float64(binary.LittleEndian.Uint16(g.bin[o:]))
			case componentShort:
				el[c] = float64(int16(binary.LittleEndian.Uint16(g.bin[o:])))
			case componentUnsignedByte:
				el[c] = float64(g.bin[o])
			case componentByte:
				el[c] = float64(int8(g.bin[o]))
			}
		}
		out[i] = el
	}
	return out, nil
}

// imageData returns the raw bytes of an embedded image. Persona exports embed
// every texture in the bin chunk rather than referencing external files.
func (g *glb) imageData(index int) ([]byte, error) {
	if index < 0 || index >= len(g.doc.Images) {
		return nil, fmt.Errorf("image %v out of range", index)
	}
	img := g.doc.Images[index]
	if img.BufferView < 0 || img.BufferView >= len(g.doc.BufferViews) {
		return nil, fmt.Errorf("image %v has no embedded buffer view", index)
	}
	bv := g.doc.BufferViews[img.BufferView]
	if bv.ByteOffset+bv.ByteLength > len(g.bin) {
		return nil, fmt.Errorf("image %v overruns bin chunk", index)
	}
	return g.bin[bv.ByteOffset : bv.ByteOffset+bv.ByteLength], nil
}

// textureOfMaterial resolves a material index to the index of the image it
// samples, following the material -> texture -> image chain.
func (g *glb) textureOfMaterial(material int) (int, error) {
	if material < 0 || material >= len(g.doc.Materials) {
		return 0, fmt.Errorf("material %v out of range", material)
	}
	t := g.doc.Materials[material].PBR.BaseColourTexture.Index
	if t < 0 || t >= len(g.doc.Textures) {
		return 0, fmt.Errorf("texture %v out of range", t)
	}
	return g.doc.Textures[t].Source, nil
}
