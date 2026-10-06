package javasession

import (
	"sync"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/ezchr/dfjava/javamap"
	jchunk "github.com/ezchr/go-mcjava/chunk"
	v777 "github.com/ezchr/go-mcjava/v777"
	"github.com/ezchr/go-mcjava/version"
)

// buildBlockInfo builds the per-runtime-id tables chunk encoding uses, from the default block
// registry (the one Dragonfly worlds use unless configured otherwise).
func buildBlockInfo() *blockInfo {
	reg := world.DefaultBlockRegistry
	java := javamap.DefaultBlockStates()
	n := len(java)
	bi := &blockInfo{java: make([]uint32, n), air: make([]bool, n), fluid: make([]bool, n), waterlogged: make([]uint32, n)}
	for rid := 0; rid < n; rid++ {
		name, _, _ := reg.RuntimeIDToState(uint32(rid))
		bi.air[rid] = name == "minecraft:air" || name == "minecraft:cave_air" || name == "minecraft:void_air" ||
			name == "minecraft:structure_void" || name == "minecraft:light_block"
		bi.fluid[rid] = reg.LiquidBlock(uint32(rid))
		bi.java[rid] = uint32(java[rid])
		// The state to use when Bedrock keeps water in the block's second layer.
		bi.waterlogged[rid] = bi.java[rid]
		if javamap.Waterloggable(java[rid]) {
			bi.waterlogged[rid] = uint32(javamap.Waterlogged(java[rid]))
		}
	}
	return bi
}

// blocksFor returns the block tables for a client version: the 26.3 tables with the block states
// remapped to the version's own.
func blocksFor(v *version.Version) *blockInfo {
	if v.Native() {
		return blocks()
	}
	if bi, ok := versionBlocks.Load(v); ok {
		return bi.(*blockInfo)
	}
	src := blocks()
	bi := &blockInfo{air: src.air, fluid: src.fluid,
		java: make([]uint32, len(src.java)), waterlogged: make([]uint32, len(src.waterlogged))}
	for i := range src.java {
		bi.java[i] = uint32(v.BlockState(int32(src.java[i])))
		bi.waterlogged[i] = uint32(v.BlockState(int32(src.waterlogged[i])))
	}
	bi.biomes = make([]uint32, len(v777.Registries["minecraft:worldgen/biome"]))
	plains := v.RegistryID("minecraft:worldgen/biome", "minecraft:plains")
	for i := range bi.biomes {
		if b := v.Synced("minecraft:worldgen/biome", int32(i)); b >= 0 {
			bi.biomes[i] = uint32(b)
		} else {
			bi.biomes[i] = uint32(plains)
		}
	}
	bi.encoder = jchunk.NewEncoder(v.BlockStates, v.Biomes)
	bi.encoder.LongMasks = v.Protocol < 777 // 26.3 switched light masks to bytes
	actual, _ := versionBlocks.LoadOrStore(v, bi)
	return actual.(*blockInfo)
}

var versionBlocks sync.Map // *version.Version -> *blockInfo

func airRID() uint32 { return world.DefaultBlockRegistry.AirRuntimeID() }

// biomeID is the Java registry id of a Dragonfly (Bedrock) biome id.
func biomeID(b uint32) uint32 { return uint32(javamap.BiomeID(int(b))) }
