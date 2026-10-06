package javasession

import (
	"github.com/ezchr/go-mcjava/version"
)

// The entity, fx, effect and text code computes 26.3 (v777) ids. For a client of an older version
// they are turned into its own ids where they are written, with these tables. The newest version
// has none (Session.legacy returns nil), so its path stays a single bool check.

// legacyIDs is the remap tables of one older version.
type legacyIDs struct {
	v *version.Version

	sound, particle, block, item, mobEffect, attribute, argumentType []int32
	damageType, jukeboxSong                                          []int32

	// Ids of the old version's own registries the old-version code paths need.
	particleBlock, particlePortal int32
}

var legacyTables = func() map[*version.Version]*legacyIDs {
	m := map[*version.Version]*legacyIDs{}
	for _, v := range version.All {
		if v.Native() {
			continue
		}
		m[v] = &legacyIDs{
			v:              v,
			sound:          v.BuiltinTable("minecraft:sound_event"),
			particle:       v.BuiltinTable("minecraft:particle_type"),
			block:          v.BuiltinTable("minecraft:block"),
			item:           v.BuiltinTable("minecraft:item"),
			mobEffect:      v.BuiltinTable("minecraft:mob_effect"),
			attribute:      v.BuiltinTable("minecraft:attribute"),
			argumentType:   v.BuiltinTable("minecraft:command_argument_type"),
			damageType:     v.SyncedTable("minecraft:damage_type"),
			jukeboxSong:    v.SyncedTable("minecraft:jukebox_song"),
			particleBlock:  v.BuiltinID("minecraft:particle_type", "minecraft:block"),
			particlePortal: v.BuiltinID("minecraft:particle_type", "minecraft:portal"),
		}
	}
	return m
}()

// legacy is the remap tables of the session's client version, or nil for the newest version.
func (s *Session) legacy() *legacyIDs {
	if s.ver.Native() {
		return nil
	}
	return legacyTables[s.ver]
}

// particleID is the client's id of a particle type of the mapping (-1: its version lacks it).
func (s *Session) particleID(t jparticle) int32 {
	if l := s.legacy(); l != nil {
		return version.Map(l.particle, particleIDs[t])
	}
	return particleIDs[t]
}

// blockID is the client's id of a 26.3 minecraft:block registry id (-1: its version lacks it).
func (s *Session) blockID(b int32) int32 {
	if l := s.legacy(); l != nil {
		return version.Map(l.block, b)
	}
	return b
}

// itemID is the client's id of a 26.3 minecraft:item registry id (-1: its version lacks it).
func (s *Session) itemID(it int32) int32 {
	if l := s.legacy(); l != nil {
		return version.Map(l.item, it)
	}
	return it
}
