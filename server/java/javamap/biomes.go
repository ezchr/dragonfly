package javamap

import (
	"sync"

	"github.com/df-mc/dragonfly/server/world"
	v777 "github.com/ezchr/go-mcjava/v777"
)

// BiomeID returns the Java biome network id (index in the minecraft:worldgen/biome registry sent during
// configuration, v777.Registries) for a Bedrock biome id as stored in Dragonfly chunks. Unknown ids map to plains.
func BiomeID(bedrockID int) int32 {
	t := biomeTable()
	if bedrockID >= 0 && bedrockID < len(t.ids) {
		return t.ids[bedrockID]
	}
	return t.plains
}

// Biome returns the Java biome network id of a Dragonfly biome.
func Biome(b world.Biome) int32 { return BiomeID(b.EncodeBiome()) }

type biomes struct {
	ids    []int32 // indexed by Bedrock biome id
	plains int32
}

var biomeTable = sync.OnceValue(func() biomes {
	index := map[string]int32{}
	for i, name := range v777.Registries["minecraft:worldgen/biome"] {
		index[name] = int32(i)
	}
	plains, ok := index["minecraft:plains"]
	if !ok {
		panic("javamap: minecraft:plains missing from the biome registry")
	}
	maxID := 1
	for id := range biomeNames {
		maxID = max(maxID, id)
	}
	t := make([]int32, maxID+1)
	for i := range t {
		t[i] = plains
	}
	for id, name := range biomeNames {
		if j, ok := index[name]; ok && id >= 0 {
			t[id] = j
		}
	}
	return biomes{ids: t, plains: plains}
})
