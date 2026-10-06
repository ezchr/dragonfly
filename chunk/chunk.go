package chunk

import (
	"errors"
	"math/bits"
)

// Sizes of the per-section arrays.
const (
	SectionBlocks = 4096 // block states per section, index y<<8 | z<<4 | x
	SectionBiomes = 64   // biomes per section (4x4x4 cells), index y<<4 | z<<2 | x
	LightBytes    = 2048 // bytes in one light nibble array
	MaxDataSize   = 2097152

	// VanillaBlockStates is the size of the 26.3 block state registry (blocks.json); it makes the
	// direct (global) block palette 16 bits per entry.
	VanillaBlockStates = 35723
	// VanillaBiomes is the size of the vanilla minecraft:worldgen/biome registry sent in 26.3
	// configuration; it makes the direct biome palette 7 bits per entry. A server with data-pack
	// biomes must pass its own registry size.
	VanillaBiomes = 67
)

// BlockIndex is the index of a block in Section.Blocks (0 <= x, y, z < 16).
func BlockIndex(x, y, z int) int { return y<<8 | z<<4 | x }

// BiomeIndex is the index of a 4x4x4 biome cell in Section.Biomes (0 <= x, y, z < 4).
func BiomeIndex(x, y, z int) int { return y<<4 | z<<2 | x }

// HeightmapType is the VarInt id of Heightmap.Types.
type HeightmapType int32

const (
	WorldSurfaceWG         HeightmapType = 0
	WorldSurface           HeightmapType = 1 // sent to clients
	OceanFloorWG           HeightmapType = 2
	OceanFloor             HeightmapType = 3
	MotionBlocking         HeightmapType = 4 // sent to clients
	MotionBlockingNoLeaves HeightmapType = 5 // sent to clients
)

// Heightmap is one entry of the heightmap map: its type and packed data (see PackHeightmap).
type Heightmap struct {
	Type HeightmapType
	Data []uint64
}

// Layout is a paletted container exactly as it appears on the wire: the bits-per-entry byte and
// the palette (one entry for single value, the local palette for linear/hashmap, nil for global).
// Decode fills it so a decoded packet re-encodes byte for byte even if the sender's in-memory
// palette was not the one vanilla would pick for the current contents (vanilla palettes only grow,
// and keep insertion order). Servers leave it nil and let the encoder choose.
type Layout struct {
	Bits    uint8
	Palette []uint32
}

// Section is one 16x16x16 chunk section.
type Section struct {
	// BlockCount is the number of non-air blocks (anything but air, cave_air and void_air,
	// fluids included). The client skips rendering a section whose count is 0.
	BlockCount int16
	// FluidCount is the number of blocks whose fluid state is not empty (water, lava,
	// waterlogged blocks).
	FluidCount int16
	Blocks     [SectionBlocks]uint32 // block state ids
	Biomes     [SectionBiomes]uint32 // biome registry ids

	// BlockLayout and BiomeLayout force a wire layout; nil means pick it like vanilla.
	BlockLayout *Layout
	BiomeLayout *Layout
}

// Fill sets every block to state and every biome to biome, and clears the layouts.
func (s *Section) Fill(state, biome uint32) {
	for i := range s.Blocks {
		s.Blocks[i] = state
	}
	for i := range s.Biomes {
		s.Biomes[i] = biome
	}
	s.BlockLayout, s.BiomeLayout = nil, nil
}

// BlockEntity is one entry of the block entity list.
type BlockEntity struct {
	XZ   uint8  // (x&15)<<4 | z&15 within the chunk
	Y    int16  // absolute block y
	Type int32  // minecraft:block_entity_type registry id
	NBT  []byte // network NBT of the update tag: 0x0a and the nameless compound payload; nil if absent
}

// LightState says what a light section carries.
type LightState uint8

const (
	// LightAbsent: no bit in either mask; the client keeps what it has (nothing, for a new chunk).
	LightAbsent LightState = iota
	// LightEmpty: bit in the empty mask; vanilla sends this for a DataLayer that exists but was
	// never allocated (all zero). An allocated all-zero layer is sent as LightData instead.
	LightEmpty
	// LightData: bit in the mask and a 2048-byte nibble array.
	LightData
)

// Light is one light section. Index 0 of Column.SkyLight/BlockLight is the section below the
// world, index len(Sections)+1 the section above it.
type Light struct {
	State LightState
	Data  []byte // LightBytes long when State == LightData; nibble per block, index as BlockIndex
}

// Column is the content of a level_chunk_with_light packet.
type Column struct {
	X, Z int32
	// Heightmaps in wire order. Vanilla sends WORLD_SURFACE, MOTION_BLOCKING and
	// MOTION_BLOCKING_NO_LEAVES; its order comes from a HashMap, so it is not guaranteed.
	Heightmaps    []Heightmap
	Sections      []Section // bottom to top
	BlockEntities []BlockEntity
	SkyLight      []Light // normally len(Sections)+2
	BlockLight    []Light
}

// ClearLayouts drops every forced layout, so encoding picks palettes like vanilla.
func (c *Column) ClearLayouts() {
	for i := range c.Sections {
		c.Sections[i].BlockLayout, c.Sections[i].BiomeLayout = nil, nil
	}
}

// ceilLog2 is Mth.ceillog2 for n >= 1.
func ceilLog2(n int) uint8 {
	if n <= 1 {
		return 0
	}
	return uint8(bits.Len(uint(n - 1)))
}

// HeightmapBits is the bits per height value for a world of the given height (ceil(log2(h+1))).
func HeightmapBits(worldHeight int) int { return int(ceilLog2(worldHeight + 1)) }

// PackHeightmap appends the packed form of 256 heights (index x + z*16), each the y of the first
// free block above the column minus the world's minimum y (0..worldHeight), and returns dst.
func PackHeightmap(dst []uint64, heights *[256]uint16, worldHeight int) []uint64 {
	b := HeightmapBits(worldHeight)
	return packInto(dst, heights[:], b)
}

// UnpackHeightmap reverses PackHeightmap. It returns false if data has the wrong length.
func UnpackHeightmap(data []uint64, worldHeight int, heights *[256]uint16) bool {
	b := HeightmapBits(worldHeight)
	if b == 0 || len(data) != longsFor(256, b) {
		return false
	}
	per := 64 / b
	mask := uint64(1)<<b - 1
	for i := range heights {
		heights[i] = uint16(data[i/per] >> (uint(i%per) * uint(b)) & mask)
	}
	return true
}

func packInto(dst []uint64, v []uint16, b int) []uint64 {
	per := 64 / b
	for i := 0; i < len(v); i += per {
		var acc uint64
		end := min(i+per, len(v))
		for j := i; j < end; j++ {
			acc |= uint64(v[j]) << (uint(j-i) * uint(b))
		}
		dst = append(dst, acc)
	}
	return dst
}

// longsFor is the number of longs SimpleBitStorage uses for n entries of b bits (b > 0).
func longsFor(n, b int) int {
	per := 64 / b
	return (n + per - 1) / per
}

// Errors.
var (
	ErrValueRange   = errors.New("chunk: id out of registry range")
	ErrLayout       = errors.New("chunk: invalid layout")
	ErrNotInPalette = errors.New("chunk: value missing from forced palette")
	ErrLight        = errors.New("chunk: invalid light data")
	ErrTooLarge     = errors.New("chunk: section data larger than 2 MiB")
	ErrNBT          = errors.New("chunk: malformed NBT")
	ErrTrailing     = errors.New("chunk: trailing bytes in section data")
)
