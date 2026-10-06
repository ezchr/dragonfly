package javasession

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
}

// particleOverrideLimiter is vanilla's per-type overrideLimiter flag (ParticleTypes.register).
var particleOverrideLimiter = [ptCount]bool{
	ptExplosionEmitter: true,
}
