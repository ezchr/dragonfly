package javasession

import (
	"math"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	jchunk "github.com/ezchr/go-mcjava/chunk"
	v777 "github.com/ezchr/go-mcjava/v777"
)

// blockInfo is what chunk encoding needs per Dragonfly block runtime id.
type blockInfo struct {
	java        []uint32 // Java block state id
	waterlogged []uint32 // Java state when the block's second layer holds water
	air         []bool   // counts as air (not counted in a section's block count)
	fluid       []bool   // has a fluid (counted in a section's fluid count)
	biomes      []uint32 // 26.3 biome id -> the version's (nil: unchanged)

	// encoder encodes chunks for the version (the palette widths depend on its registry sizes);
	// nil: the shared 26.3 encoder.
	encoder *jchunk.Encoder
	encMu   sync.Mutex
}

var (
	infoOnce sync.Once
	info     *blockInfo
	encoder  = jchunk.NewEncoder(jchunk.VanillaBlockStates, jchunk.VanillaBiomes)
	encMu    sync.Mutex // the encoder keeps scratch buffers
)

func blocks() *blockInfo {
	infoOnce.Do(func() { info = buildBlockInfo() })
	return info
}

// chunkState is per-session chunk sending state.
type chunkState struct {
	batching bool // inside sendChunkBatch: chunks go into a batch
	inBatch  int  // chunks queued in the batch being built this tick

	// sent is every chunk the client has from us. The loader drops chunks without telling its
	// viewer, so the session forgets them on the client itself: the client would otherwise keep
	// showing them (they get no more block updates), or another world's terrain after a switch.
	sentMu sync.Mutex
	sent   map[world.ChunkPos]struct{}
}

// sendChunkBatch loads chunks around the player as one batch, at most the rate the client asked
// for, with at most one batch waiting for the client's acknowledgement.
func (s *Session) sendChunkBatch(tx *world.Tx) {
	if s.batchInFlight.Load() {
		return
	}
	n := int(s.chunkRate.Load() / 1000)
	if n < 1 {
		n = 1
	}
	s.chunks.inBatch = 0
	s.chunks.batching = true
	s.loader.Load(tx, n)
	s.chunks.batching = false
	if s.chunks.inBatch > 0 {
		p := s.packet()
		p.VarInt(int32(s.chunks.inBatch))
		s.queue(v777.ClientboundPlayChunkBatchFinished, p)
		s.batchInFlight.Store(true)
		s.chunks.inBatch = 0
	}
}

// ViewChunk encodes a chunk the loader made available.
func (s *Session) ViewChunk(pos world.ChunkPos, dim world.Dimension, blockEntities map[cube.Pos]world.Block, c *chunk.Chunk) {
	// Chunks the loader hands over outside a batch tick (when it moves) go out on their own; the
	// client accepts chunks outside batches, batches only pace the sending.
	if s.chunks.batching {
		if s.chunks.inBatch == 0 {
			s.queue(v777.ClientboundPlayChunkBatchStart, s.packet())
		}
		s.chunks.inBatch++
	}
	p := s.packet()
	col := s.column(pos, c)
	var err error
	if enc := s.blk.encoder; enc != nil {
		s.blk.encMu.Lock()
		err = enc.Encode(p, col)
		s.blk.encMu.Unlock()
	} else {
		encMu.Lock()
		err = encoder.Encode(p, col)
		encMu.Unlock()
	}
	if err != nil {
		s.log.Error("encode chunk", "pos", pos, "err", err)
		return
	}
	s.queue(v777.ClientboundPlayLevelChunkWithLight, p)
	s.chunks.sentMu.Lock()
	s.chunks.sent[pos] = struct{}{}
	s.chunks.sentMu.Unlock()
}

// forgetFarChunks unloads the chunks the loader dropped around a new centre: those further than
// the radius, measured like the loader does.
func (s *Session) forgetFarChunks(centre world.ChunkPos) {
	r := float64(s.chunkRadius)
	s.chunks.sentMu.Lock()
	defer s.chunks.sentMu.Unlock()
	for pos := range s.chunks.sent {
		dx, dz := float64(pos[0]-centre[0]), float64(pos[1]-centre[1])
		if math.Round(math.Sqrt(dx*dx+dz*dz)) > r {
			s.forgetChunk(pos)
		}
	}
}

// forgetAllChunks unloads every chunk the client has (a switch to another world of the same
// dimension; a dimension change clears the client by itself).
func (s *Session) forgetAllChunks(tell bool) {
	s.chunks.sentMu.Lock()
	defer s.chunks.sentMu.Unlock()
	if !tell {
		clear(s.chunks.sent)
		return
	}
	for pos := range s.chunks.sent {
		s.forgetChunk(pos)
	}
}

// forgetChunk sends forget_level_chunk. sentMu must be held.
func (s *Session) forgetChunk(pos world.ChunkPos) {
	delete(s.chunks.sent, pos)
	w := s.packet()
	w.Int64(int64(uint64(uint32(pos[0])) | uint64(uint32(pos[1]))<<32)) // ChunkPos.pack
	s.queue(v777.ClientboundPlayForgetLevelChunk, w)
}

// column converts a Dragonfly chunk. The Column is reused per session.
func (s *Session) column(pos world.ChunkPos, c *chunk.Chunk) *jchunk.Column {
	bi := s.blk
	col := &s.col
	col.X, col.Z = pos[0], pos[1]
	subs := c.Sub()
	r := c.Range()
	n := dimSections(s.dim)
	if len(subs) > n {
		subs = subs[:n]
	}
	if len(col.Sections) != n {
		col.Sections = make([]jchunk.Section, n)
		col.SkyLight = make([]jchunk.Light, n+2)
		col.BlockLight = make([]jchunk.Light, n+2)
		for i := range col.SkyLight {
			col.SkyLight[i].Data = make([]byte, 2048)
			col.BlockLight[i].Data = make([]byte, 2048)
		}
	}
	var heights [256]uint16
	lastFilled := -1 // highest section with any non-air block
	for i, sub := range subs {
		sec := &col.Sections[i]
		sec.BlockLayout, sec.BiomeLayout = nil, nil
		baseY := r[0] + i*16
		var nonAir, fluid int16
		if sub.Empty() {
			air := bi.java[airRID()]
			for j := range sec.Blocks {
				sec.Blocks[j] = air
			}
		} else {
			layer := sub.Layer(0)
			var water *chunk.PalettedStorage
			if len(sub.Layers()) > 1 {
				water = sub.Layer(1)
			}
			for y := byte(0); y < 16; y++ {
				for z := byte(0); z < 16; z++ {
					for x := byte(0); x < 16; x++ {
						rid := layer.At(x, y, z)
						state := bi.java[rid]
						wet := water != nil && bi.fluid[water.At(x, y, z)]
						if wet {
							state = bi.waterlogged[rid]
						}
						sec.Blocks[jchunk.BlockIndex(int(x), int(y), int(z))] = state
						if !bi.air[rid] {
							nonAir++
							h := uint16(baseY + int(y) - r[0] + 1)
							if h > heights[int(z)<<4|int(x)] {
								heights[int(z)<<4|int(x)] = h
							}
						}
						if bi.fluid[rid] || wet {
							fluid++
						}
					}
				}
			}
		}
		sec.BlockCount, sec.FluidCount = nonAir, fluid
		for y := 0; y < 4; y++ {
			for z := 0; z < 4; z++ {
				for x := 0; x < 4; x++ {
					b := c.Biome(uint8(x*4+2), int16(baseY+y*4+2), uint8(z*4+2))
					id := biomeID(b)
					if bi.biomes != nil && int(id) < len(bi.biomes) {
						id = bi.biomes[id]
					}
					sec.Biomes[jchunk.BiomeIndex(x, y, z)] = id
				}
			}
		}
		if nonAir > 0 {
			lastFilled = i
		}
		// Block light only where there is some (absent means dark).
		if !fillLight(&col.BlockLight[i+1], sub.BlockLight) {
			col.BlockLight[i+1].State = jchunk.LightAbsent
		}
	}
	// Sky light like vanilla: up to one section above the highest section with blocks. The client
	// treats the missing sections above as open sky, so they are not sent (a flat chunk goes from
	// 26 sky arrays to 3, about 70 KB to 10 KB before compression).
	for i, sub := range subs {
		if i <= lastFilled+1 {
			fillLight(&col.SkyLight[i+1], sub.SkyLight)
		} else {
			col.SkyLight[i+1].State = jchunk.LightAbsent
		}
	}
	// Sections the Java dimension has above Dragonfly's world (the nether): empty air.
	for i := len(subs); i < n; i++ {
		sec := &col.Sections[i]
		sec.Fill(bi.java[airRID()], col.Sections[max(len(subs)-1, 0)].Biomes[0])
		sec.BlockCount, sec.FluidCount = 0, 0
		col.SkyLight[i+1].State, col.BlockLight[i+1].State = jchunk.LightAbsent, jchunk.LightAbsent
	}
	// Below and above the world: nothing (open sky above).
	col.SkyLight[0].State, col.BlockLight[0].State = jchunk.LightAbsent, jchunk.LightAbsent
	col.SkyLight[n+1].State, col.BlockLight[n+1].State = jchunk.LightAbsent, jchunk.LightAbsent

	bits := n * 16
	data := jchunk.PackHeightmap(s.hm[0][:0], &heights, bits)
	s.hm[0] = data
	col.Heightmaps = append(col.Heightmaps[:0],
		jchunk.Heightmap{Type: jchunk.WorldSurface, Data: data},
		jchunk.Heightmap{Type: jchunk.MotionBlocking, Data: data},
		jchunk.Heightmap{Type: jchunk.MotionBlockingNoLeaves, Data: data},
	)
	col.BlockEntities = col.BlockEntities[:0]
	return col
}

// fillLight copies a sub chunk's light into a Java nibble array and reports whether any of it
// is non-zero.
func fillLight(l *jchunk.Light, get func(x, y, z byte) uint8) bool {
	l.State = jchunk.LightData
	d := l.Data
	var any byte
	for y := byte(0); y < 16; y++ {
		for z := byte(0); z < 16; z++ {
			for x := byte(0); x < 16; x += 2 {
				i := jchunk.BlockIndex(int(x), int(y), int(z))
				v := get(x, y, z)&0xf | get(x+1, y, z)<<4
				d[i>>1] = v
				any |= v
			}
		}
	}
	return any != 0
}
