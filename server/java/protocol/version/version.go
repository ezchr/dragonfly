// Package version lets a server written against the newest protocol (v777, Java 26.3) talk to
// clients of older versions. The server keeps using v777 ids everywhere; a Version turns them into
// the client's ids where they are written (packet ids, block states, registry entries) and turns the
// client's packet ids back. Ids an old client doesn't have map to -1: such packets or entries are
// not sent. Packets whose layout changed between versions are written by the caller per version
// (see Version.Protocol).
package version

import (
	v776 "github.com/df-mc/dragonfly/server/java/protocol/v776"
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
)

// Packet is a packet id and body.
type Packet struct {
	ID   int32
	Body []byte
}

// Version is a protocol version clients may join with.
type Version struct {
	// Protocol is the protocol number (777 for 26.3).
	Protocol int32
	// Name is the game version, as in the vanilla core pack ("26.3").
	Name string
	// BlockStates and Biomes are the registry sizes that set a chunk's direct palette widths.
	BlockStates, Biomes int

	native           bool
	cbConfig, cbPlay []int32 // newest id -> this version's
	sbConfig, sbPlay []int32 // this version's id -> newest
	blocks           []int32
	builtin          map[string][]int32
	synced           map[string][]int32
	configuration    func() []Packet
	registries       map[string][]string
	builtinIDs       map[string]map[string]int32
}

// Newest is the protocol the server is written against.
var Newest = &Version{
	Protocol: 777, Name: "26.3", BlockStates: 35723, Biomes: len(v777.Registries["minecraft:worldgen/biome"]),
	native:     true,
	registries: v777.Registries,
	builtinIDs: v777.Builtin,
	configuration: func() []Packet {
		var out []Packet
		for _, p := range v777.VanillaConfiguration() {
			out = append(out, Packet{p.ID, p.Body})
		}
		return out
	},
}

// V776 is Java 26.2.
var V776 = &Version{
	Protocol: 776, Name: "26.2", BlockStates: 32366, Biomes: len(v776.Registries["minecraft:worldgen/biome"]),
	cbConfig: v776.ClientboundConfiguration(), cbPlay: v776.ClientboundPlay(),
	sbConfig: v776.ServerboundConfiguration(), sbPlay: v776.ServerboundPlay(),
	blocks: v776.BlockStates(), builtin: v776.BuiltinRemap(),
	synced:     syncedRemap(v777.Registries, v776.Registries),
	registries: v776.Registries,
	builtinIDs: v776.Builtin,
	configuration: func() []Packet {
		var out []Packet
		for _, p := range v776.VanillaConfiguration() {
			out = append(out, Packet{p.ID, p.Body})
		}
		return out
	},
}

// All is every supported version, newest first.
var All = []*Version{Newest, V776}

// ByProtocol returns the version with this protocol number, or nil.
func ByProtocol(p int32) *Version {
	for _, v := range All {
		if v.Protocol == p {
			return v
		}
	}
	return nil
}

// Native reports whether v is the protocol the server is written against (nothing to remap).
func (v *Version) Native() bool { return v.native }

// Configuration is the vanilla configuration packets for this version (see v777.VanillaConfiguration),
// with this version's ids.
func (v *Version) Configuration() []Packet { return v.configuration() }

func lookup(t []int32, id int32) int32 {
	if id < 0 || int(id) >= len(t) {
		return -1
	}
	return t[id]
}

// ClientboundConfig is this version's id of a newest-version clientbound configuration packet.
func (v *Version) ClientboundConfig(id int32) int32 {
	if v.native {
		return id
	}
	return lookup(v.cbConfig, id)
}

// ServerboundConfig is the newest-version id of this version's serverbound configuration packet.
func (v *Version) ServerboundConfig(id int32) int32 {
	if v.native {
		return id
	}
	return lookup(v.sbConfig, id)
}

// ClientboundPlay is this version's id of a newest-version clientbound play packet (-1: none).
func (v *Version) ClientboundPlay(id int32) int32 {
	if v.native {
		return id
	}
	return lookup(v.cbPlay, id)
}

// ServerboundPlay is the newest-version id of this version's serverbound play packet (-1: the
// newest version has no such packet; handle it by this version's id).
func (v *Version) ServerboundPlay(id int32) int32 {
	if v.native {
		return id
	}
	return lookup(v.sbPlay, id)
}

// BlockState is this version's id of a newest-version block state. Blocks the version lacks map
// to a stand-in of the same shape.
func (v *Version) BlockState(state int32) int32 {
	if v.native {
		return state
	}
	if s := lookup(v.blocks, state); s >= 0 {
		return s
	}
	return 1 // stone
}

// Builtin is this version's id of a newest-version entry of a built-in registry
// (minecraft:item, minecraft:entity_type, minecraft:sound_event...), or -1.
func (v *Version) Builtin(registry string, id int32) int32 {
	if v.native {
		return id
	}
	return lookup(v.builtin[registry], id)
}

// BuiltinTable is the remap table of a built-in registry for hot paths: this version's id is
// t[newestID] (-1: none). It is nil for the newest version: ids stay as they are.
func (v *Version) BuiltinTable(registry string) []int32 {
	if v.native {
		return nil
	}
	return v.builtin[registry]
}

// Map applies a table from BuiltinTable or SyncedTable (nil: unchanged).
func Map(t []int32, id int32) int32 {
	if t == nil {
		return id
	}
	return lookup(t, id)
}

// Synced is this version's id of a newest-version entry of a registry sent in configuration
// (minecraft:worldgen/biome, minecraft:damage_type, minecraft:dimension_type...), or -1.
func (v *Version) Synced(registry string, id int32) int32 {
	if v.native {
		return id
	}
	return lookup(v.synced[registry], id)
}

// SyncedTable is like BuiltinTable for a synchronised registry.
func (v *Version) SyncedTable(registry string) []int32 {
	if v.native {
		return nil
	}
	return v.synced[registry]
}

// BuiltinID is this version's id of a built-in registry entry by name, or -1.
func (v *Version) BuiltinID(registry, entry string) int32 {
	if id, ok := v.builtinIDs[registry][entry]; ok {
		return id
	}
	return -1
}

// RegistryID is this version's id of a synchronised registry entry by name, or -1.
func (v *Version) RegistryID(registry, entry string) int32 {
	for i, e := range v.registries[registry] {
		if e == entry {
			return int32(i)
		}
	}
	return -1
}

// syncedRemap maps the entries of the newest version's synchronised registries to an old
// version's by name.
func syncedRemap(newest, old map[string][]string) map[string][]int32 {
	out := map[string][]int32{}
	for name, entries := range newest {
		idx := map[string]int32{}
		for i, e := range old[name] {
			idx[e] = int32(i)
		}
		t := make([]int32, len(entries))
		for i, e := range entries {
			t[i] = -1
			if o, ok := idx[e]; ok {
				t[i] = o
			}
		}
		out[name] = t
	}
	return out
}
