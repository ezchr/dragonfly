package javamap

import (
	"slices"
	"sort"
	"sync"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// BlockStates returns the Java 26.3 block state id for every runtime id of reg, indexed by runtime id, so a chunk
// encoder can do `java := table[rid]`. reg must be finalised. The table is built in about a millisecond; build it
// once per registry and share it.
func BlockStates(reg chunk.BlockRegistry) []int32 {
	table, _ := BlockStatesWithMisses(reg)
	return table
}

// BlockStatesWithMisses is BlockStates, also returning the runtime ids that were not in the generated table (custom
// blocks, or a Dragonfly whose block states changed since generation). Those use the Java state chosen for another
// state with the same Bedrock name, else stone.
func BlockStatesWithMisses(reg chunk.BlockRegistry) (table []int32, misses []uint32) {
	n := reg.BlockCount()
	table = make([]int32, n)
	for rid := range uint32(n) {
		if h, ok := reg.RuntimeIDToHash(rid); ok {
			if java, ok := lookupHash(h); ok {
				table[rid] = java
				continue
			}
		}
		misses = append(misses, rid)
		table[rid] = JavaStone
		if name, _, ok := reg.RuntimeIDToState(rid); ok {
			if java, ok := nameDefault[name]; ok {
				table[rid] = java
			}
		}
	}
	return table, misses
}

// DefaultBlockStates returns BlockStates(world.DefaultBlockRegistry), built on first use. The returned slice is
// shared and must not be modified.
func DefaultBlockStates() []int32 { return defaultStates() }

var defaultStates = sync.OnceValue(func() []int32 {
	world.DefaultBlockRegistry.Finalize()
	return BlockStates(world.DefaultBlockRegistry)
})

// lookupHash finds the Java state of a Bedrock network block hash in stateTable.
func lookupHash(h uint32) (int32, bool) {
	key := uint64(h) << 32
	i := sort.Search(len(stateTable), func(i int) bool { return stateTable[i] >= key })
	if i < len(stateTable) && stateTable[i]>>32 == uint64(h) {
		return int32(uint32(stateTable[i])), true
	}
	return 0, false
}

// Waterlogged returns the waterlogged=true variant of a Java state. States that cannot be waterlogged, or already
// are, are returned unchanged. Bedrock keeps water in block layer 1: when layer 1 holds a water source, send
// Waterlogged(table[layer0]).
func Waterlogged(java int32) int32 {
	i := sort.Search(len(waterlogRanges), func(i int) bool { return waterlogRanges[i][1] > java })
	if i == len(waterlogRanges) || java < waterlogRanges[i][0] {
		return java
	}
	r := waterlogRanges[i]
	if (java-r[0])/r[2]%2 == 1 {
		return java - r[2]
	}
	return java
}

// Waterloggable reports whether the Java state has a waterlogged property.
func Waterloggable(java int32) bool {
	i := sort.Search(len(waterlogRanges), func(i int) bool { return waterlogRanges[i][1] > java })
	return i < len(waterlogRanges) && java >= waterlogRanges[i][0]
}

// DependencySource says where a Java property the Bedrock state does not carry must come from.
type DependencySource uint8

const (
	// SourceNeighbours: derive from adjacent blocks (fence/pane connections, stair shape, chest type, snowy, ...).
	SourceNeighbours DependencySource = iota
	// SourceRedstone: derive from redstone power (powered, power, triggered, enabled).
	SourceRedstone
	// SourceBlockEntity: take from the block's block entity (note pitch, lectern book, bed colour, pot contents...).
	SourceBlockEntity
	// SourceNone: Bedrock does not keep it and the default renders acceptably or is never seen by the client
	// (leaf distance, sapling stage, floor button rotation).
	SourceNone
	// SourceUnknown: not classified yet.
	SourceUnknown
)

func (s DependencySource) String() string {
	switch s {
	case SourceNeighbours:
		return "neighbours"
	case SourceRedstone:
		return "redstone"
	case SourceBlockEntity:
		return "block entity"
	case SourceNone:
		return "none"
	}
	return "unknown"
}

// Dependency names a Java block whose properties are not determined by the Bedrock state. The table maps such
// states to the Java default for those properties; a fixer has to set them for correct rendering.
//
// When Properties is ["<block>"], Block is a Bedrock name shared by several Java blocks (beds, flower pots...) and
// the Java block itself has to be picked from block entity data.
type Dependency struct {
	Block      string
	Properties []string
	Source     DependencySource
}

// NeighbourDependent lists the Java blocks (and properties) that need a fixer to be exact.
func NeighbourDependent() []Dependency { return slices.Clone(neighbourDependent) }
