package chunk

import (
	"bytes"
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/ezchr/go-mcjava/wire"
)

// Vanilla 26.3 ids used by the superflat captures.
const (
	air     = 0
	grass   = 9 // grass_block[snowy=false]
	dirt    = 10
	bedrock = 88
	plains  = 41
)

var captures = []string{
	"00295_S_play_minecraft_level_chunk_with_light.bin",
	"00296_S_play_minecraft_level_chunk_with_light.bin",
	"00297_S_play_minecraft_level_chunk_with_light.bin",
}

func readCapture(t testing.TB, name string) []byte {
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestVanillaCaptures(t *testing.T) {
	dec := NewDecoder(VanillaBlockStates, VanillaBiomes)
	enc := NewEncoder(VanillaBlockStates, VanillaBiomes)
	for _, name := range captures {
		t.Run(name, func(t *testing.T) {
			body := readCapture(t, name)
			r := wire.NewReader(body)
			var c Column
			if err := dec.Decode(r, &c); err != nil {
				t.Fatal(err)
			}
			if r.Len() != 0 {
				t.Fatalf("%d bytes left unread", r.Len())
			}
			checkSuperflat(t, &c)

			var w wire.Writer
			if err := enc.Encode(&w, &c); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(w.B, body) {
				t.Fatalf("re-encode with recorded layouts differs at byte %d", firstDiff(w.B, body))
			}

			// Without the recorded layouts the encoder must pick the same palettes as vanilla.
			c.ClearLayouts()
			w.Reset()
			if err := enc.Encode(&w, &c); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(w.B, body) {
				t.Fatalf("re-encode choosing palettes differs at byte %d", firstDiff(w.B, body))
			}

			// Built from scratch, no decoder involved.
			fresh := superflatColumn(c.X, c.Z, c.Heightmaps, c.SkyLight, c.BlockLight)
			w.Reset()
			if err := enc.Encode(&w, fresh); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(w.B, body) {
				t.Fatalf("hand-built superflat column differs at byte %d", firstDiff(w.B, body))
			}
		})
	}
}

func checkSuperflat(t *testing.T, c *Column) {
	t.Helper()
	if len(c.Sections) != 24 {
		t.Fatalf("%d sections, want 24", len(c.Sections))
	}
	s0 := &c.Sections[0]
	if s0.BlockCount != 1024 || s0.FluidCount != 0 {
		t.Errorf("section 0 counts %d/%d", s0.BlockCount, s0.FluidCount)
	}
	for y, want := range []uint32{bedrock, dirt, dirt, grass, air} {
		if got := s0.Blocks[BlockIndex(5, y, 7)]; got != want {
			t.Errorf("y=%d: block %d, want %d", y, got, want)
		}
	}
	if l := s0.BlockLayout; l.Bits != 4 || !equalU32(l.Palette, []uint32{bedrock, dirt, grass, air}) {
		t.Errorf("section 0 block layout %+v", *l)
	}
	for i := 1; i < 24; i++ {
		if l := c.Sections[i].BlockLayout; l.Bits != 0 || !equalU32(l.Palette, []uint32{air}) {
			t.Errorf("section %d block layout %+v", i, *l)
		}
	}
	for i := range c.Sections {
		if l := c.Sections[i].BiomeLayout; l.Bits != 0 || !equalU32(l.Palette, []uint32{plains}) {
			t.Errorf("section %d biome layout %+v", i, *l)
		}
	}
	var types []HeightmapType
	var hs [256]uint16
	for _, h := range c.Heightmaps {
		types = append(types, h.Type)
		if !UnpackHeightmap(h.Data, 384, &hs) {
			t.Fatalf("heightmap %d: %d longs", h.Type, len(h.Data))
		}
		for i, v := range hs {
			if v != 4 { // first free block y=-60, world min y=-64
				t.Fatalf("heightmap %d column %d = %d", h.Type, i, v)
			}
		}
		if packed := PackHeightmap(nil, &hs, 384); !equalU64(packed, h.Data) {
			t.Errorf("PackHeightmap differs for type %d", h.Type)
		}
	}
	if len(types) != 3 || types[0] != WorldSurface || types[1] != MotionBlocking || types[2] != MotionBlockingNoLeaves {
		t.Errorf("heightmap types %v", types)
	}
	if len(c.BlockEntities) != 0 {
		t.Errorf("%d block entities", len(c.BlockEntities))
	}
	if len(c.SkyLight) != 26 || len(c.BlockLight) != 26 {
		t.Fatalf("light lengths %d/%d", len(c.SkyLight), len(c.BlockLight))
	}
}

// superflatColumn builds the captured chunk without the decoder (heightmaps and light are copied).
func superflatColumn(x, z int32, hm []Heightmap, sky, block []Light) *Column {
	c := &Column{X: x, Z: z, Heightmaps: hm, Sections: make([]Section, 24)}
	for i := range c.Sections {
		c.Sections[i].Fill(air, plains)
	}
	s := &c.Sections[0]
	for i := range s.Blocks {
		switch i >> 8 {
		case 0:
			s.Blocks[i] = bedrock
		case 1, 2:
			s.Blocks[i] = dirt
		case 3:
			s.Blocks[i] = grass
		}
	}
	s.BlockCount = 1024
	c.SkyLight = append([]Light(nil), sky...)
	c.BlockLight = append([]Light(nil), block...)
	return c
}

// TestTruncated feeds every prefix of a capture to the decoder: it must fail cleanly, not panic.
func TestTruncated(t *testing.T) {
	body := readCapture(t, captures[0])
	dec := NewDecoder(VanillaBlockStates, VanillaBiomes)
	var c Column
	for n := 0; n < len(body); n++ {
		if err := dec.Decode(wire.NewReader(body[:n]), &c); err == nil {
			t.Fatalf("prefix of %d bytes decoded without error", n)
		}
	}
}

func TestPaletteChoice(t *testing.T) {
	enc := NewEncoder(VanillaBlockStates, VanillaBiomes)
	dec := NewDecoder(VanillaBlockStates, VanillaBiomes)
	blockCases := map[int]uint8{1: 0, 2: 4, 3: 4, 16: 4, 17: 5, 32: 5, 33: 6, 64: 6, 65: 7, 128: 7, 129: 8, 256: 8, 257: 16, 4096: 16}
	biomeCases := map[int]uint8{1: 0, 2: 1, 3: 2, 4: 2, 5: 3, 8: 3, 9: 7, 64: 7}
	rng := rand.New(rand.NewSource(1))
	for n, want := range blockCases {
		var s Section
		fillDistinct(rng, s.Blocks[:], n, VanillaBlockStates)
		fillBiomes(&s, plains)
		got := encodeAndCheck(t, enc, dec, &s)
		if got.BlockLayout.Bits != want {
			t.Errorf("%d block states: %d bits, want %d", n, got.BlockLayout.Bits, want)
		}
		if want != 16 && len(got.BlockLayout.Palette) != n {
			t.Errorf("%d block states: palette of %d", n, len(got.BlockLayout.Palette))
		}
	}
	for n, want := range biomeCases {
		var s Section
		fillDistinct(rng, s.Biomes[:], n, VanillaBiomes)
		got := encodeAndCheck(t, enc, dec, &s)
		if got.BiomeLayout.Bits != want {
			t.Errorf("%d biomes: %d bits, want %d", n, got.BiomeLayout.Bits, want)
		}
	}
}

// fillBiomes sets every biome of s to b.
func fillBiomes(s *Section, b uint32) {
	for i := range s.Biomes {
		s.Biomes[i] = b
	}
}

// fillDistinct fills vals with exactly n distinct random ids below size, in random positions.
func fillDistinct(rng *rand.Rand, vals []uint32, n, size int) {
	ids := rng.Perm(size)[:n]
	for i := range vals {
		if i < n {
			vals[i] = uint32(ids[i])
		} else {
			vals[i] = uint32(ids[rng.Intn(n)])
		}
	}
	rng.Shuffle(len(vals), func(i, j int) { vals[i], vals[j] = vals[j], vals[i] })
}

// encodeAndCheck encodes one section in a column, decodes it, compares contents, re-encodes the
// decoded column with its recorded layouts and checks the bytes match; it returns the decoded section.
func encodeAndCheck(t *testing.T, enc *Encoder, dec *Decoder, s *Section) *Section {
	t.Helper()
	c := &Column{X: -3, Z: 7, Sections: []Section{*s}}
	var w wire.Writer
	if err := enc.Encode(&w, c); err != nil {
		t.Fatal(err)
	}
	r := wire.NewReader(w.B)
	var got Column
	if err := dec.Decode(r, &got); err != nil {
		t.Fatal(err)
	}
	if r.Len() != 0 || len(got.Sections) != 1 {
		t.Fatalf("left %d bytes, %d sections", r.Len(), len(got.Sections))
	}
	g := &got.Sections[0]
	if g.Blocks != s.Blocks || g.Biomes != s.Biomes || g.BlockCount != s.BlockCount || g.FluidCount != s.FluidCount {
		t.Fatal("section changed in round trip")
	}
	var w2 wire.Writer
	if err := enc.Encode(&w2, &got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(w.B, w2.B) {
		t.Fatalf("forced-layout re-encode differs at %d", firstDiff(w.B, w2.B))
	}
	return g
}

func TestRoundTripRandom(t *testing.T) {
	enc := NewEncoder(VanillaBlockStates, VanillaBiomes)
	dec := NewDecoder(VanillaBlockStates, VanillaBiomes)
	rng := rand.New(rand.NewSource(2))
	c := &Column{X: 12345, Z: -999, Sections: make([]Section, 24)}
	for i := range c.Sections {
		s := &c.Sections[i]
		n := []int{1, 2, 5, 16, 17, 100, 255, 256, 257, 600, 2000, 4096}[i%12]
		fillDistinct(rng, s.Blocks[:], n, VanillaBlockStates)
		fillDistinct(rng, s.Biomes[:], 1+i%12, VanillaBiomes)
		s.BlockCount = int16(rng.Intn(4097))
		s.FluidCount = int16(rng.Intn(4097))
	}
	var hs [256]uint16
	for i := range hs {
		hs[i] = uint16(rng.Intn(385))
	}
	c.Heightmaps = []Heightmap{
		{Type: MotionBlocking, Data: PackHeightmap(nil, &hs, 384)},
		{Type: WorldSurface, Data: PackHeightmap(nil, &hs, 384)},
	}
	c.SkyLight = make([]Light, 26)
	c.BlockLight = make([]Light, 26)
	for i := range c.SkyLight {
		c.SkyLight[i] = randLight(rng)
		c.BlockLight[i] = randLight(rng)
	}
	c.BlockEntities = []BlockEntity{
		{XZ: 0x3f, Y: -12, Type: 7, NBT: sampleNBT()},
		{XZ: 0x00, Y: 300, Type: 1},
	}

	var w wire.Writer
	if err := enc.Encode(&w, c); err != nil {
		t.Fatal(err)
	}
	var got Column
	r := wire.NewReader(w.B)
	if err := dec.Decode(r, &got); err != nil {
		t.Fatal(err)
	}
	if r.Len() != 0 {
		t.Fatalf("%d bytes left", r.Len())
	}
	if got.X != c.X || got.Z != c.Z || len(got.Sections) != 24 {
		t.Fatal("header or section count changed")
	}
	for i := range c.Sections {
		a, b := &c.Sections[i], &got.Sections[i]
		if a.Blocks != b.Blocks || a.Biomes != b.Biomes || a.BlockCount != b.BlockCount || a.FluidCount != b.FluidCount {
			t.Fatalf("section %d changed", i)
		}
	}
	for i := range c.SkyLight {
		for _, p := range [][2]Light{{c.SkyLight[i], got.SkyLight[i]}, {c.BlockLight[i], got.BlockLight[i]}} {
			if p[0].State != p[1].State || !bytes.Equal(p[0].Data, p[1].Data) {
				t.Fatalf("light section %d changed", i)
			}
		}
	}
	if len(got.BlockEntities) != 2 || !bytes.Equal(got.BlockEntities[0].NBT, sampleNBT()) ||
		got.BlockEntities[0].XZ != 0x3f || got.BlockEntities[0].Y != -12 || got.BlockEntities[0].Type != 7 ||
		got.BlockEntities[1].NBT != nil || got.BlockEntities[1].Y != 300 {
		t.Fatalf("block entities changed: %+v", got.BlockEntities)
	}
	for i, h := range got.Heightmaps {
		if h.Type != c.Heightmaps[i].Type || !equalU64(h.Data, c.Heightmaps[i].Data) {
			t.Fatalf("heightmap %d changed", i)
		}
	}

	// Decoding into the same column again (reusing its buffers) and re-encoding is exact.
	if err := dec.Decode(wire.NewReader(w.B), &got); err != nil {
		t.Fatal(err)
	}
	var w2 wire.Writer
	if err := enc.Encode(&w2, &got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(w.B, w2.B) {
		t.Fatalf("re-encode differs at %d", firstDiff(w.B, w2.B))
	}
}

// A forced layout that is not what vanilla would pick (unordered palette, 4 bits for a
// 1-entry palette) must still encode as given.
func TestForcedLayout(t *testing.T) {
	enc := NewEncoder(VanillaBlockStates, VanillaBiomes)
	dec := NewDecoder(VanillaBlockStates, VanillaBiomes)
	var s Section
	s.Fill(1, plains)
	s.BlockLayout = &Layout{Bits: 4, Palette: []uint32{air, 1}}
	s.BiomeLayout = &Layout{Bits: 7}
	g := encodeAndCheck(t, enc, dec, &s)
	if g.BlockLayout.Bits != 4 || !equalU32(g.BlockLayout.Palette, []uint32{air, 1}) || g.BiomeLayout.Bits != 7 {
		t.Fatalf("layouts %+v %+v", *g.BlockLayout, *g.BiomeLayout)
	}

	s.BlockLayout = &Layout{Bits: 4, Palette: []uint32{air}}
	var w wire.Writer
	if err := enc.Encode(&w, &Column{Sections: []Section{s}}); !errors.Is(err, ErrNotInPalette) || len(w.B) != 0 {
		t.Fatalf("missing palette entry: err %v, %d bytes written", err, len(w.B))
	}
}

func TestEncodeErrors(t *testing.T) {
	enc := NewEncoder(VanillaBlockStates, VanillaBiomes)
	var s Section
	s.Fill(air, plains)
	s.Blocks[100] = VanillaBlockStates
	var w wire.Writer
	w.Raw([]byte{1, 2, 3})
	if err := enc.Encode(&w, &Column{Sections: []Section{s}}); !errors.Is(err, ErrValueRange) {
		t.Fatalf("err %v", err)
	}
	if !bytes.Equal(w.B, []byte{1, 2, 3}) {
		t.Fatal("writer not restored after error")
	}
	// The slot table must be clean after the failure: a valid section encodes as usual.
	s.Blocks[100] = 5
	if err := enc.Encode(&w, &Column{Sections: []Section{s}}); err != nil {
		t.Fatal(err)
	}
	if err := enc.Encode(&w, &Column{SkyLight: []Light{{State: LightData, Data: make([]byte, 10)}}}); !errors.Is(err, ErrLight) {
		t.Fatalf("short light array: err %v", err)
	}
	if err := enc.Encode(&w, &Column{BlockEntities: []BlockEntity{{NBT: []byte{10, 1}}}}); !errors.Is(err, ErrNBT) {
		t.Fatalf("bad NBT: err %v", err)
	}
}

// TestDataLength crosses the 2-byte/3-byte VarInt boundary of the section data length.
func TestDataLength(t *testing.T) {
	enc := NewEncoder(VanillaBlockStates, VanillaBiomes)
	dec := NewDecoder(VanillaBlockStates, VanillaBiomes)
	rng := rand.New(rand.NewSource(3))
	for _, n := range []int{0, 1, 2, 3, 10} {
		c := &Column{Sections: make([]Section, n)}
		for i := range c.Sections {
			fillDistinct(rng, c.Sections[i].Blocks[:], 300, VanillaBlockStates) // 8 KiB each
			fillBiomes(&c.Sections[i], plains)
		}
		var w wire.Writer
		if err := enc.Encode(&w, c); err != nil {
			t.Fatal(err)
		}
		var got Column
		if err := dec.Decode(wire.NewReader(w.B), &got); err != nil || len(got.Sections) != n {
			t.Fatalf("%d sections: err %v, got %d", n, err, len(got.Sections))
		}
	}
}

func randLight(rng *rand.Rand) Light {
	switch rng.Intn(3) {
	case 0:
		return Light{}
	case 1:
		return Light{State: LightEmpty}
	}
	b := make([]byte, LightBytes)
	rng.Read(b)
	return Light{State: LightData, Data: b}
}

// sampleNBT is {id:"x", n:[I;1,2], l:[{a:1b}]} in network NBT.
func sampleNBT() []byte {
	return []byte{
		10,
		8, 0, 2, 'i', 'd', 0, 1, 'x',
		11, 0, 1, 'n', 0, 0, 0, 2, 0, 0, 0, 1, 0, 0, 0, 2,
		9, 0, 1, 'l', 10, 0, 0, 0, 1, 1, 0, 1, 'a', 1, 0,
		0,
	}
}

func firstDiff(a, b []byte) int {
	for i := range min(len(a), len(b)) {
		if a[i] != b[i] {
			return i
		}
	}
	return min(len(a), len(b))
}

func equalU32(a, b []uint32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalU64(a, b []uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// typicalColumn is an overworld-like column: 4 deepslate sections with ores, 4 stone sections with
// caves, water and ores, a surface section, 15 air sections, sky light everywhere and a little
// block light.
func typicalColumn() *Column {
	rng := rand.New(rand.NewSource(4))
	c := &Column{X: 10, Z: -4, Sections: make([]Section, 24)}
	deep := []uint32{27000, 27000, 27000, 27000, 27000, 27000, 27000, 4, 4, 125, 130, 27400, 27410, 27420}
	stone := []uint32{1, 1, 1, 1, 1, 1, 1, 1, air, air, 89, 125, 130, 135, 140, 4, 6, 2}
	surface := []uint32{air, air, air, air, air, grass, dirt, 1, 2111, 2112}
	for i := range c.Sections {
		s := &c.Sections[i]
		s.Fill(air, plains)
		var pick []uint32
		switch {
		case i < 4:
			pick = deep
		case i < 8:
			pick = stone
		case i == 8:
			pick = surface
		default:
			continue
		}
		for j := range s.Blocks {
			s.Blocks[j] = pick[rng.Intn(len(pick))]
		}
		s.BlockCount = 3500
	}
	for j := 0; j < 32; j++ {
		c.Sections[2].Biomes[j] = 7
	}
	var hs [256]uint16
	for i := range hs {
		hs[i] = 140
	}
	c.Heightmaps = []Heightmap{
		{Type: WorldSurface, Data: PackHeightmap(nil, &hs, 384)},
		{Type: MotionBlocking, Data: PackHeightmap(nil, &hs, 384)},
		{Type: MotionBlockingNoLeaves, Data: PackHeightmap(nil, &hs, 384)},
	}
	c.SkyLight = make([]Light, 26)
	c.BlockLight = make([]Light, 26)
	for i := range c.SkyLight {
		d := make([]byte, LightBytes)
		rng.Read(d)
		c.SkyLight[i] = Light{State: LightData, Data: d}
		c.BlockLight[i] = Light{State: LightEmpty}
	}
	for _, i := range []int{3, 6, 9} {
		d := make([]byte, LightBytes)
		rng.Read(d)
		c.BlockLight[i] = Light{State: LightData, Data: d}
	}
	return c
}

func BenchmarkEncodeTypical(b *testing.B) {
	enc := NewEncoder(VanillaBlockStates, VanillaBiomes)
	c := typicalColumn()
	var w wire.Writer
	if err := enc.Encode(&w, c); err != nil { // warm up: grow the buffer once
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Reset()
		if err := enc.Encode(&w, c); err != nil {
			b.Fatal(err)
		}
	}
	b.SetBytes(int64(len(w.B)))
}

func BenchmarkEncodeSuperflat(b *testing.B) {
	body := readCapture(b, captures[0])
	var c Column
	if err := NewDecoder(VanillaBlockStates, VanillaBiomes).Decode(wire.NewReader(body), &c); err != nil {
		b.Fatal(err)
	}
	c.ClearLayouts()
	enc := NewEncoder(VanillaBlockStates, VanillaBiomes)
	var w wire.Writer
	if err := enc.Encode(&w, &c); err != nil { // warm up: grow the buffer once
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Reset()
		if err := enc.Encode(&w, &c); err != nil {
			b.Fatal(err)
		}
	}
	b.SetBytes(int64(len(w.B)))
}

func BenchmarkEncodeGlobal(b *testing.B) {
	enc := NewEncoder(VanillaBlockStates, VanillaBiomes)
	rng := rand.New(rand.NewSource(5))
	c := &Column{Sections: make([]Section, 24)}
	for i := range c.Sections {
		fillDistinct(rng, c.Sections[i].Blocks[:], 1000, VanillaBlockStates)
		fillBiomes(&c.Sections[i], plains)
	}
	var w wire.Writer
	if err := enc.Encode(&w, c); err != nil { // warm up: grow the buffer once
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Reset()
		if err := enc.Encode(&w, c); err != nil {
			b.Fatal(err)
		}
	}
	b.SetBytes(int64(len(w.B)))
}

func BenchmarkDecodeTypical(b *testing.B) {
	var w wire.Writer
	if err := NewEncoder(VanillaBlockStates, VanillaBiomes).Encode(&w, typicalColumn()); err != nil {
		b.Fatal(err)
	}
	dec := NewDecoder(VanillaBlockStates, VanillaBiomes)
	var c Column
	if err := dec.Decode(wire.NewReader(w.B), &c); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := dec.Decode(wire.NewReader(w.B), &c); err != nil {
			b.Fatal(err)
		}
	}
}
