// chunkdiff decodes two level_chunk_with_light bodies (from pktcap) and reports where their
// blocks, biomes, counts, heightmaps and light differ.
//
//	chunkdiff a.bin b.bin
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/ezchr/go-mc/java/chunk"
	"github.com/ezchr/go-mc/java/wire"
)

func load(path string) *chunk.Column {
	b, err := os.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}
	var c chunk.Column
	if err := chunk.NewDecoder(chunk.VanillaBlockStates, chunk.VanillaBiomes).Decode(wire.NewReader(b), &c); err != nil {
		log.Fatalf("%s: %v", path, err)
	}
	return &c
}

func main() {
	a, b := load(os.Args[1]), load(os.Args[2])
	fmt.Printf("A chunk %d,%d  B chunk %d,%d  sections %d/%d\n", a.X, a.Z, b.X, b.Z, len(a.Sections), len(b.Sections))
	diffs := 0
	for i := range a.Sections {
		if i >= len(b.Sections) {
			break
		}
		sa, sb := &a.Sections[i], &b.Sections[i]
		nb, nbio := 0, 0
		first := ""
		for j := range sa.Blocks {
			if sa.Blocks[j] != sb.Blocks[j] {
				if nb == 0 {
					first = fmt.Sprintf(" first at index %d: %d vs %d", j, sa.Blocks[j], sb.Blocks[j])
				}
				nb++
			}
		}
		for j := range sa.Biomes {
			if sa.Biomes[j] != sb.Biomes[j] {
				nbio++
			}
		}
		if nb > 0 || nbio > 0 || sa.BlockCount != sb.BlockCount || sa.FluidCount != sb.FluidCount {
			diffs++
			fmt.Printf("section %2d: %d block diffs%s, %d biome diffs (A biome %d, B biome %d), counts %d/%d fluid %d/%d\n",
				i, nb, first, nbio, sa.Biomes[0], sb.Biomes[0], sa.BlockCount, sb.BlockCount, sa.FluidCount, sb.FluidCount)
		}
	}
	for _, ha := range a.Heightmaps {
		for _, hb := range b.Heightmaps {
			if ha.Type == hb.Type {
				var x, y [256]uint16
				chunk.UnpackHeightmap(ha.Data, 384, &x)
				chunk.UnpackHeightmap(hb.Data, 384, &y)
				if x != y {
					diffs++
					fmt.Printf("heightmap %d differs: A[0]=%d B[0]=%d\n", ha.Type, x[0], y[0])
				}
			}
		}
	}
	lightDiff := func(name string, la, lb []chunk.Light) {
		for i := range la {
			if i >= len(lb) {
				return
			}
			if la[i].State != lb[i].State || string(la[i].Data) != string(lb[i].Data) {
				diffs++
				fmt.Printf("%s light section %d: state %d vs %d\n", name, i, la[i].State, lb[i].State)
			}
		}
	}
	lightDiff("sky", a.SkyLight, b.SkyLight)
	lightDiff("block", a.BlockLight, b.BlockLight)
	fmt.Println("differences:", diffs)
}
