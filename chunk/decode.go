package chunk

import (
	"fmt"
	"math/bits"
	"slices"

	"github.com/ezchr/go-mcjava/wire"
)

// Decoder reads level_chunk_with_light bodies the way the 26.3 client does, but strictly: every
// palette entry and global id must be inside its registry, indirect data must index the palette,
// and each light list must match its mask.
type Decoder struct {
	block, biome strategy

	// LongMasks reads light masks as long arrays (see Encoder.LongMasks).
	LongMasks bool
}

// NewDecoder returns a decoder for the given registry sizes (see NewEncoder).
func NewDecoder(blockStates, biomes int) *Decoder {
	return &Decoder{block: newStrategy(true, blockStates), biome: newStrategy(false, biomes)}
}

// Decode reads a packet body (without the packet id) into c, reusing c's slices and layouts. The
// number of sections is however many the data array holds. Light arrays and block entity NBT alias
// r.B; copy them to keep them past the packet buffer. Every section gets its wire layout recorded
// in BlockLayout/BiomeLayout, so Encode reproduces the body exactly; call c.ClearLayouts to have
// the encoder choose instead. Errors are also left in r.Err.
func (d *Decoder) Decode(r *wire.Reader, c *Column) error {
	c.X = r.Int32()
	c.Z = r.Int32()

	n := readLen(r, 2)
	c.Heightmaps = resize(c.Heightmaps, n)
	for i := range c.Heightmaps {
		h := &c.Heightmaps[i]
		h.Type = HeightmapType(r.VarInt())
		h.Data = resize(h.Data, readLen(r, 8))
		for j := range h.Data {
			h.Data[j] = r.Uint64()
		}
	}

	data := r.ByteArray(MaxDataSize)
	if r.Err != nil {
		return r.Err
	}
	sr := wire.NewReader(data)
	c.Sections = c.Sections[:0]
	for sr.Len() > 0 && sr.Err == nil {
		if len(c.Sections) < cap(c.Sections) {
			c.Sections = c.Sections[:len(c.Sections)+1]
		} else {
			c.Sections = append(c.Sections, Section{})
		}
		s := &c.Sections[len(c.Sections)-1]
		s.BlockCount = sr.Int16()
		s.FluidCount = sr.Int16()
		d.container(sr, &d.block, s.Blocks[:], &s.BlockLayout)
		d.container(sr, &d.biome, s.Biomes[:], &s.BiomeLayout)
		if sr.Err != nil {
			fail(r, fmt.Errorf("section %d: %w", len(c.Sections)-1, sr.Err))
			return r.Err
		}
	}

	n = readLen(r, 5)
	c.BlockEntities = resize(c.BlockEntities, n)
	for i := range c.BlockEntities {
		be := &c.BlockEntities[i]
		be.XZ = r.Byte()
		be.Y = r.Int16()
		be.Type = r.VarInt()
		start := r.Off
		switch t := r.Byte(); {
		case r.Err != nil:
		case t == 0:
			be.NBT = nil
		case t == tagCompound:
			skipPayload(r, tagCompound, 0)
			be.NBT = r.B[start:r.Off:r.Off]
		default:
			fail(r, fmt.Errorf("%w: block entity tag type %d", ErrNBT, t))
		}
	}

	d.light(r, c)
	return r.Err
}

func (d *Decoder) light(r *wire.Reader, c *Column) {
	read := func() []byte { return r.ByteArray(r.Len()) }
	if d.LongMasks {
		read = func() []byte { return readLongMask(r) }
	}
	sky := read()
	block := read()
	emptySky := read()
	emptyBlock := read()
	if r.Err != nil {
		return
	}
	n := len(c.Sections) + 2
	for _, m := range [4][]byte{sky, block, emptySky, emptyBlock} {
		n = max(n, maskLen(m))
	}
	c.SkyLight = lightFromMasks(r, c.SkyLight, n, sky, emptySky)
	c.BlockLight = lightFromMasks(r, c.BlockLight, n, block, emptyBlock)
}

// lightFromMasks sets the states from the masks and reads one array list.
func lightFromMasks(r *wire.Reader, ls []Light, n int, mask, empty []byte) []Light {
	ls = resize(ls, n)
	for i := range ls {
		in, ie := bitSet(mask, i), bitSet(empty, i)
		switch {
		case in && ie:
			fail(r, fmt.Errorf("%w: section %d in both the mask and the empty mask", ErrLight, i))
			return ls
		case in:
			ls[i] = Light{State: LightData}
		case ie:
			ls[i] = Light{State: LightEmpty}
		default:
			ls[i] = Light{}
		}
	}
	count := readLen(r, 1)
	want := 0
	for _, b := range mask {
		want += bits.OnesCount8(b)
	}
	if r.Err == nil && count != want {
		fail(r, fmt.Errorf("%w: %d arrays for %d mask bits", ErrLight, count, want))
	}
	for i := range ls {
		if r.Err != nil {
			break
		}
		if ls[i].State == LightData {
			b := r.ByteArray(LightBytes)
			if r.Err == nil && len(b) != LightBytes {
				fail(r, fmt.Errorf("%w: section %d array of %d bytes", ErrLight, i, len(b)))
			}
			ls[i].Data = b[:len(b):len(b)]
		}
	}
	return ls
}

func bitSet(m []byte, i int) bool { return i/8 < len(m) && m[i/8]>>(i%8)&1 != 0 }

// maskLen is one past the highest set bit.
func maskLen(m []byte) int {
	for i := len(m) - 1; i >= 0; i-- {
		if m[i] != 0 {
			return i*8 + bits.Len8(m[i])
		}
	}
	return 0
}

// container reads a PalettedContainer into vals and records its layout.
func (d *Decoder) container(r *wire.Reader, st *strategy, vals []uint32, lp **Layout) {
	bitsByte := r.Byte()
	if r.Err != nil {
		return
	}
	storage, k := st.config(int(int8(bitsByte)))
	lay := *lp
	if lay == nil {
		lay = new(Layout)
		*lp = lay
	}
	lay.Bits = bitsByte
	lay.Palette = lay.Palette[:0]
	limit := uint32(st.size)

	switch k {
	case kindSingle:
		v := uint32(r.VarInt())
		if r.Err == nil && v >= limit {
			fail(r, fmt.Errorf("%w: %d", ErrValueRange, v))
			return
		}
		lay.Palette = append(lay.Palette, v)
		for i := range vals {
			vals[i] = v
		}
		return
	case kindIndirect:
		n := readLen(r, 1)
		if r.Err == nil && (n == 0 || n > 1<<storage) {
			fail(r, fmt.Errorf("%w: %d palette entries for %d bits", ErrLayout, n, storage))
			return
		}
		for i := 0; i < n; i++ {
			v := uint32(r.VarInt())
			if r.Err == nil && v >= limit {
				fail(r, fmt.Errorf("%w: %d", ErrValueRange, v))
				return
			}
			lay.Palette = append(lay.Palette, v)
		}
	case kindGlobal:
		if storage == 0 {
			fail(r, fmt.Errorf("%w: global palette of a 1-entry registry", ErrLayout))
			return
		}
	}

	per := 64 / int(storage)
	mask := uint64(1)<<storage - 1
	pal := lay.Palette
	for i := 0; i < len(vals) && r.Err == nil; i += per {
		l := r.Uint64()
		end := min(i+per, len(vals))
		for j := i; j < end; j++ {
			x := uint32(l >> (uint(j-i) * uint(storage)) & mask)
			if k == kindIndirect {
				if int(x) >= len(pal) {
					fail(r, fmt.Errorf("%w: index %d into a palette of %d", ErrLayout, x, len(pal)))
					return
				}
				x = pal[x]
			} else if x >= limit {
				fail(r, fmt.Errorf("%w: %d", ErrValueRange, x))
				return
			}
			vals[j] = x
		}
	}
}

// readLen reads a VarInt count of elements that are each at least minElem bytes.
func readLen(r *wire.Reader, minElem int) int {
	n := r.VarInt()
	if r.Err != nil {
		return 0
	}
	if n < 0 || int(n) > r.Len()/minElem {
		fail(r, fmt.Errorf("%w: %d", wire.ErrTooLarge, n))
		return 0
	}
	return int(n)
}

func fail(r *wire.Reader, err error) {
	if r.Err == nil {
		r.Err = err
	}
	r.Off = len(r.B)
}

func skip(r *wire.Reader, n int) {
	if r.Err != nil {
		return
	}
	if n < 0 || n > r.Len() {
		fail(r, wire.ErrShort)
		return
	}
	r.Off += n
}

// resize returns s with length n, reusing its capacity (and the elements in it).
func resize[T any](s []T, n int) []T {
	if n <= cap(s) {
		return s[:n]
	}
	return slices.Grow(s[:0], n)[:n]
}

// readLongMask reads a BitSet long array as the equivalent little-endian byte mask.
func readLongMask(r *wire.Reader) []byte {
	n := int(r.VarInt())
	if n < 0 || n > r.Len()/8 {
		fail(r, fmt.Errorf("%w: mask of %d longs", ErrLight, n))
		return nil
	}
	b := make([]byte, 0, n*8)
	for i := 0; i < n; i++ {
		x := uint64(r.Int64())
		for j := 0; j < 8; j++ {
			b = append(b, byte(x>>(8*j)))
		}
	}
	for len(b) > 0 && b[len(b)-1] == 0 {
		b = b[:len(b)-1]
	}
	return b
}
