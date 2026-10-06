package chunk

// kind is the palette format of a paletted container.
type kind uint8

const (
	kindSingle   kind = iota // SingleValuePalette, no data longs
	kindIndirect             // LinearPalette or HashMapPalette (same wire format)
	kindGlobal               // GlobalPalette, raw registry ids
)

// strategy mirrors net.minecraft.world.level.chunk.Strategy for one registry.
type strategy struct {
	blocks      bool
	size        int   // registry size
	globalBits  uint8 // Mth.ceillog2(size)
	entries     int   // 4096 or 64
	maxIndirect int   // largest palette that is not global: 256 for blocks, 8 for biomes
}

func newStrategy(blocks bool, size int) strategy {
	s := strategy{blocks: blocks, size: size, globalBits: ceilLog2(size)}
	if blocks {
		s.entries, s.maxIndirect = SectionBlocks, 256
	} else {
		s.entries, s.maxIndirect = SectionBiomes, 8
	}
	return s
}

// config mirrors getConfigurationForBitCount: it maps a bits-per-entry value (the wire byte, which
// the client reads as a signed byte) to the storage bits and palette kind.
func (s *strategy) config(entryBits int) (storageBits uint8, k kind) {
	if entryBits == 0 {
		return 0, kindSingle
	}
	if s.blocks {
		switch {
		case entryBits >= 1 && entryBits <= 4:
			return 4, kindIndirect
		case entryBits >= 5 && entryBits <= 8:
			return uint8(entryBits), kindIndirect
		}
	} else if entryBits >= 1 && entryBits <= 3 {
		return uint8(entryBits), kindIndirect
	}
	return s.globalBits, kindGlobal
}

// forPaletteSize is getConfigurationForPaletteSize: the layout vanilla uses for a container with
// n distinct values (as produced when a chunk is packed and unpacked). The wire byte equals the
// storage bits (PalettedContainer.Data.write writes storage.getBits()).
func (s *strategy) forPaletteSize(n int) (storageBits uint8, k kind) {
	return s.config(int(ceilLog2(n)))
}
