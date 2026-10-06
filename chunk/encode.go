package chunk

import (
	"encoding/binary"
	"fmt"
	"slices"

	"github.com/ezchr/go-mcjava/wire"
)

// Encoder writes level_chunk_with_light bodies. It keeps scratch tables sized to the registries,
// so encoding allocates nothing once the writer's buffer is large enough. An Encoder is not safe
// for concurrent use; keep one per goroutine.
type Encoder struct {
	block, biome strategy
	// slot maps a registry id to its palette index + 1 while a container is being encoded; it is
	// all zero between containers (only the touched entries are cleared).
	blockSlot []uint16
	biomeSlot []uint16
	pal       [256]uint32
	idx       [SectionBlocks]uint16
}

// NewEncoder returns an encoder for a block state registry of blockStates entries and a biome
// registry of biomes entries (VanillaBlockStates, VanillaBiomes for an unmodified 26.3 server).
func NewEncoder(blockStates, biomes int) *Encoder {
	return &Encoder{
		block:     newStrategy(true, blockStates),
		biome:     newStrategy(false, biomes),
		blockSlot: make([]uint16, blockStates),
		biomeSlot: make([]uint16, biomes),
	}
}

// Encode appends the packet body (without the packet id) for c to w. On error w is left as it was.
func (e *Encoder) Encode(w *wire.Writer, c *Column) (err error) {
	start := len(w.B)
	defer func() {
		if err != nil {
			w.B = w.B[:start]
		}
	}()
	w.Int32(c.X)
	w.Int32(c.Z)

	w.VarInt(int32(len(c.Heightmaps)))
	for _, h := range c.Heightmaps {
		w.VarInt(int32(h.Type))
		w.VarInt(int32(len(h.Data)))
		putLongs(w, h.Data)
	}

	// The section data is a byte array; reserve 3 bytes for its VarInt length (enough below
	// 2 MiB) and move the data if the length turns out shorter or longer.
	lenAt := len(w.B)
	w.B = append(w.B, 0, 0, 0)
	for i := range c.Sections {
		if err := e.section(w, &c.Sections[i]); err != nil {
			return fmt.Errorf("section %d: %w", i, err)
		}
	}
	size := len(w.B) - lenAt - 3
	if size > MaxDataSize {
		return ErrTooLarge
	}
	if n := wire.VarIntSize(int32(size)); n < 3 {
		copy(w.B[lenAt+n:], w.B[lenAt+3:])
		w.B = w.B[:len(w.B)-(3-n)]
	} else if n > 3 {
		w.B = append(w.B, make([]byte, n-3)...)
		copy(w.B[lenAt+n:], w.B[lenAt+3:len(w.B)-(n-3)])
	}
	wire.AppendVarInt(w.B[:lenAt], int32(size))

	w.VarInt(int32(len(c.BlockEntities)))
	for i := range c.BlockEntities {
		be := &c.BlockEntities[i]
		w.Byte(be.XZ)
		w.Int16(be.Y)
		w.VarInt(be.Type)
		if len(be.NBT) == 0 {
			w.Byte(0) // TAG_End: Optional.empty()
			continue
		}
		if !validCompound(be.NBT) {
			return fmt.Errorf("block entity %d: %w", i, ErrNBT)
		}
		w.Raw(be.NBT)
	}

	return writeLight(w, c.SkyLight, c.BlockLight)
}

// EncodeSection appends one section (block count, fluid count, block and biome containers) as it
// appears inside the chunk data array.
func (e *Encoder) EncodeSection(w *wire.Writer, s *Section) error {
	start := len(w.B)
	if err := e.section(w, s); err != nil {
		w.B = w.B[:start]
		return err
	}
	return nil
}

func (e *Encoder) section(w *wire.Writer, s *Section) error {
	w.Int16(s.BlockCount)
	w.Int16(s.FluidCount)
	if err := e.container(w, &e.block, e.blockSlot, s.Blocks[:], s.BlockLayout); err != nil {
		return fmt.Errorf("blocks: %w", err)
	}
	if err := e.container(w, &e.biome, e.biomeSlot, s.Biomes[:], s.BiomeLayout); err != nil {
		return fmt.Errorf("biomes: %w", err)
	}
	return nil
}

// container writes a PalettedContainer: bits byte, palette, data longs.
//
// Without a forced layout it does what vanilla's pack/unpack round trip does (which is how every
// chunk loaded from disk is held): palette entries in order of first appearance by index, bits from
// the palette size via Strategy.getConfigurationForPaletteSize.
func (e *Encoder) container(w *wire.Writer, st *strategy, slot []uint16, vals []uint32, lay *Layout) error {
	if lay != nil {
		return e.forced(w, st, slot, vals, lay)
	}
	limit := uint32(st.size)
	pal := e.pal[:0]
	idx := e.idx[:len(vals)]

	// Uniform containers (all-air sections, single-biome sections) are the most common case.
	if v := vals[0]; uniform(vals, v) {
		if v >= limit {
			return fmt.Errorf("%w: %d", ErrValueRange, v)
		}
		w.Byte(0)
		w.VarInt(int32(v))
		return nil
	}

	// Every branch in this loop is almost always predicted the same way, so mixed data (ores in
	// stone) costs no more than long runs.
	global := false
	for i, v := range vals {
		if v >= limit {
			clearSlots(slot, pal)
			return fmt.Errorf("%w: %d", ErrValueRange, v)
		}
		s := slot[v]
		if s == 0 {
			if len(pal) == st.maxIndirect {
				global = true
				break
			}
			pal = append(pal, v)
			s = uint16(len(pal))
			slot[v] = s
		}
		idx[i] = s - 1
	}
	clearSlots(slot, pal)

	if global {
		w.Byte(st.globalBits)
		return packIDs(w, vals, st.globalBits, limit)
	}
	storage, k := st.forPaletteSize(len(pal))
	w.Byte(storage)
	switch k {
	case kindSingle:
		w.VarInt(int32(pal[0]))
		return nil
	case kindIndirect:
		w.VarInt(int32(len(pal)))
		for _, v := range pal {
			w.VarInt(int32(v))
		}
		packIndices(w, idx, storage)
		return nil
	default: // unreachable: a palette that fits maxIndirect is never global
		return packIDs(w, vals, storage, limit)
	}
}

// forced writes a container with a caller-given layout (normally one Decode recorded).
func (e *Encoder) forced(w *wire.Writer, st *strategy, slot []uint16, vals []uint32, lay *Layout) error {
	limit := uint32(st.size)
	storage, k := st.config(int(int8(lay.Bits)))
	switch k {
	case kindSingle:
		if len(lay.Palette) != 1 {
			return fmt.Errorf("%w: single value palette with %d entries", ErrLayout, len(lay.Palette))
		}
		p := lay.Palette[0]
		if p >= limit {
			return fmt.Errorf("%w: %d", ErrValueRange, p)
		}
		for _, v := range vals {
			if v != p {
				return fmt.Errorf("%w: %d", ErrNotInPalette, v)
			}
		}
		w.Byte(lay.Bits)
		w.VarInt(int32(p))
		return nil
	case kindGlobal:
		if len(lay.Palette) != 0 {
			return fmt.Errorf("%w: global layout with a palette", ErrLayout)
		}
		w.Byte(lay.Bits)
		return packIDs(w, vals, storage, limit)
	}

	n := len(lay.Palette)
	if n == 0 || n > 1<<storage {
		return fmt.Errorf("%w: %d palette entries for %d bits", ErrLayout, n, storage)
	}
	for i, p := range lay.Palette {
		if p >= limit {
			clearSlots(slot, lay.Palette[:i])
			return fmt.Errorf("%w: %d", ErrValueRange, p)
		}
		if slot[p] == 0 {
			slot[p] = uint16(i + 1)
		}
	}
	idx := e.idx[:len(vals)]
	for i, v := range vals {
		if v >= limit || slot[v] == 0 {
			clearSlots(slot, lay.Palette)
			return fmt.Errorf("%w: %d", ErrNotInPalette, v)
		}
		idx[i] = slot[v] - 1
	}
	clearSlots(slot, lay.Palette)
	w.Byte(lay.Bits)
	w.VarInt(int32(n))
	for _, p := range lay.Palette {
		w.VarInt(int32(p))
	}
	packIndices(w, idx, storage)
	return nil
}

// uniform reports whether every value is v, eight at a time.
func uniform(vals []uint32, v uint32) bool {
	i := 0
	for ; i+8 <= len(vals); i += 8 {
		s := vals[i : i+8 : i+8]
		if (s[0]^v)|(s[1]^v)|(s[2]^v)|(s[3]^v)|(s[4]^v)|(s[5]^v)|(s[6]^v)|(s[7]^v) != 0 {
			return false
		}
	}
	for ; i < len(vals); i++ {
		if vals[i] != v {
			return false
		}
	}
	return true
}

func clearSlots(slot []uint16, pal []uint32) {
	for _, v := range pal {
		slot[v] = 0
	}
}

// grow extends w.B by n bytes and returns them.
func grow(w *wire.Writer, n int) []byte {
	off := len(w.B)
	w.B = slices.Grow(w.B, n)[:off+n]
	return w.B[off:]
}

func putLongs(w *wire.Writer, longs []uint64) {
	out := grow(w, len(longs)*8)
	for i, l := range longs {
		binary.BigEndian.PutUint64(out[i*8:], l)
	}
}

// packIndices writes palette indices as SimpleBitStorage longs: 64/b entries per long, first entry
// in the low bits, no entry split across longs.
func packIndices(w *wire.Writer, idx []uint16, b uint8) {
	if b == 4 && len(idx)%16 == 0 { // the common case: block sections with 2..16 states
		out := grow(w, len(idx)/2)
		for i, o := 0, 0; i < len(idx); i, o = i+16, o+8 {
			s := idx[i : i+16 : i+16]
			acc := uint64(s[0]) | uint64(s[1])<<4 | uint64(s[2])<<8 | uint64(s[3])<<12 |
				uint64(s[4])<<16 | uint64(s[5])<<20 | uint64(s[6])<<24 | uint64(s[7])<<28 |
				uint64(s[8])<<32 | uint64(s[9])<<36 | uint64(s[10])<<40 | uint64(s[11])<<44 |
				uint64(s[12])<<48 | uint64(s[13])<<52 | uint64(s[14])<<56 | uint64(s[15])<<60
			binary.BigEndian.PutUint64(out[o:], acc)
		}
		return
	}
	per := 64 / int(b)
	out := grow(w, longsFor(len(idx), int(b))*8)
	for i, o := 0, 0; i < len(idx); i, o = i+per, o+8 {
		var acc uint64
		for j, v := range idx[i:min(i+per, len(idx))] {
			acc |= uint64(v) << (uint(j) * uint(b))
		}
		binary.BigEndian.PutUint64(out[o:], acc)
	}
}

// packIDs writes registry ids directly (global palette), checking each against the registry.
func packIDs(w *wire.Writer, vals []uint32, b uint8, limit uint32) error {
	if b == 0 {
		return fmt.Errorf("%w: global palette of a 1-entry registry", ErrLayout)
	}
	per := 64 / int(b)
	start := len(w.B)
	out := grow(w, longsFor(len(vals), int(b))*8)
	for i, o := 0, 0; i < len(vals); i, o = i+per, o+8 {
		var acc uint64
		for j, v := range vals[i:min(i+per, len(vals))] {
			if v >= limit {
				w.B = w.B[:start]
				return fmt.Errorf("%w: %d", ErrValueRange, v)
			}
			acc |= uint64(v) << (uint(j) * uint(b))
		}
		binary.BigEndian.PutUint64(out[o:], acc)
	}
	return nil
}

// writeLight writes ClientboundLightUpdatePacketData: four BitSets (each a VarInt byte count and
// BitSet.toByteArray(), little-endian bytes with trailing zero bytes trimmed), then the sky and
// block nibble arrays, each list a VarInt count of VarInt-length-prefixed 2048-byte arrays.
func writeLight(w *wire.Writer, sky, block []Light) error {
	for _, ls := range [2][]Light{sky, block} {
		for i := range ls {
			switch ls[i].State {
			case LightAbsent, LightEmpty:
			case LightData:
				if len(ls[i].Data) != LightBytes {
					return fmt.Errorf("%w: section %d has %d bytes", ErrLight, i, len(ls[i].Data))
				}
			default:
				return fmt.Errorf("%w: section %d state %d", ErrLight, i, ls[i].State)
			}
		}
	}
	writeMask(w, sky, LightData)
	writeMask(w, block, LightData)
	writeMask(w, sky, LightEmpty)
	writeMask(w, block, LightEmpty)
	writeArrays(w, sky)
	writeArrays(w, block)
	return nil
}

func writeMask(w *wire.Writer, ls []Light, st LightState) {
	hi := -1
	for i := len(ls) - 1; i >= 0; i-- {
		if ls[i].State == st {
			hi = i
			break
		}
	}
	n := hi/8 + 1
	if hi < 0 {
		n = 0
	}
	w.VarInt(int32(n))
	for b := 0; b < n; b++ {
		var x byte
		for j := 0; j < 8; j++ {
			if i := b*8 + j; i <= hi && ls[i].State == st {
				x |= 1 << j
			}
		}
		w.Byte(x)
	}
}

func writeArrays(w *wire.Writer, ls []Light) {
	n := 0
	for i := range ls {
		if ls[i].State == LightData {
			n++
		}
	}
	w.VarInt(int32(n))
	for i := range ls {
		if ls[i].State == LightData {
			w.ByteArray(ls[i].Data)
		}
	}
}
