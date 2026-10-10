package javasession

import (
	"strings"

	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/java/protocol/version"
)

// jparticle is a Java particle type the particle mapping uses; particleIDs holds its registry id.
type jparticle uint8

const (
	ptFlame jparticle = iota
	ptSoulFireFlame
	ptDust
	ptBlockMarker
	ptNote
	ptDrippingWater
	ptDrippingLava
	ptLava
	ptDustPlume
	ptExplosionEmitter
	ptItemSnowball
	ptItem
	ptEntityEffect
	ptHeart
	ptCount
)

var particleNames = [ptCount]string{
	ptFlame:            "flame",
	ptSoulFireFlame:    "soul_fire_flame",
	ptDust:             "dust",
	ptBlockMarker:      "block_marker",
	ptNote:             "note",
	ptDrippingWater:    "dripping_water",
	ptDrippingLava:     "dripping_lava",
	ptLava:             "lava",
	ptDustPlume:        "dust_plume",
	ptExplosionEmitter: "explosion_emitter",
	ptItemSnowball:     "item_snowball",
	ptItem:             "item",
	ptEntityEffect:     "entity_effect",
	ptHeart:            "heart",
}

// particleOverrideLimiter is vanilla's per-type overrideLimiter flag (ParticleTypes.register).
var particleOverrideLimiter = [ptCount]bool{
	ptExplosionEmitter: true,
}

// particleByName maps Java particle type names without the namespace ("end_rod") to their 26.3
// registry ids, for particle.Named.
var particleByName = func() map[string]int32 {
	reg := v777.Builtin["minecraft:particle_type"]
	m := make(map[string]int32, len(reg))
	for name, id := range reg {
		m[strings.TrimPrefix(name, "minecraft:")] = id
	}
	return m
}()

// namedParticleID is the client's id of the particle type with this name (-1: its version lacks
// it), and whether 26.3 has such a type.
func (s *Session) namedParticleID(name string) (int32, bool) {
	id, ok := particleByName[name]
	if !ok {
		return -1, false
	}
	if l := s.legacy(); l != nil {
		id = version.Map(l.particle, id)
	}
	return id, true
}
