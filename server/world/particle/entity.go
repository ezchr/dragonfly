package particle

import "image/color"

// HugeExplosion is a particle shown when TNT or a creeper explodes.
type HugeExplosion struct{ particle }

// EndermanTeleport is a particle that shows up when an enderman teleports.
type EndermanTeleport struct{ particle }

// SnowballPoof is a particle shown when a snowball collides with something.
type SnowballPoof struct{ particle }

// EggSmash is a particle shown when an egg smashes on something.
type EggSmash struct{ particle }

// Heart is the heart particle an animal shows while in love.
type Heart struct{ particle }

// Named is a vanilla particle picked by name, for particles that have no type of their own.
type Named struct {
	particle

	// Bedrock is the particle effect Bedrock clients are shown, such as "minecraft:endrod". Bedrock
	// clients are shown nothing if it is empty.
	Bedrock string
	// Java is the particle type Java clients are shown, such as "end_rod". It must be a type
	// without options. Java clients are shown nothing if it is empty.
	Java string
}

// Splash is a particle that shows up when a splash potion is splashed.
type Splash struct {
	particle

	// Colour is the colour that should be splashed.
	Colour color.RGBA
}

// Effect is a particle that shows up around an entity when it has effects on.
type Effect struct {
	particle

	// Colour is the colour of the particle.
	Colour color.RGBA
}

// EntityFlame is a particle shown when an entity is set on fire.
type EntityFlame struct{ particle }
