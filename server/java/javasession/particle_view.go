package javasession

import (
	"bytes"
	"image/color"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	v777 "github.com/ezchr/go-mcjava/v777"
	"github.com/ezchr/go-mcjava/wire"
	"github.com/go-gl/mathgl/mgl64"
)

// ViewParticle shows a Dragonfly particle. Block break and crack particles are level events (the
// client takes the block from the state id or its own world, and plays the break sound with
// 2001); the others are level_particles. Particles the client predicted (its own breaking) are
// dropped.
func (s *Session) ViewParticle(pos mgl64.Vec3, p world.Particle) {
	s.maybeProbe()
	switch pa := p.(type) {
	case particle.BlockBreak:
		bp := cube.PosFromVec3(pos)
		if s.predictedBreak(bp) {
			return
		}
		if st, ok := javaState(pa.Block); ok {
			s.levelEvent(levelEventDestroyBlock, bp, st, false)
		}
	case particle.BlockBreakNoSound:
		if st, ok := javaState(pa.Block); ok {
			s.levelEvent(levelEventDestroyNoSound, cube.PosFromVec3(pos), st, false)
		}
	case particle.PunchBlock:
		bp := cube.PosFromVec3(pos)
		if s.selfMining(bp) {
			return // the client spawns its own crack particles
		}
		// cube.Face and Java's Direction share their order (down, up, north, south, west, east).
		s.levelEvent(levelEventDestroyProgress, bp, int32(pa.Face), false)
	case particle.Flame:
		s.simpleParticle(ptFlame, pos, 0)
	case particle.EntityFlame:
		s.simpleParticle(ptFlame, pos, 0)
	case particle.Dust:
		w := s.particleType(ptDust)
		w.Int32(rgb(pa.Colour))
		w.Float32(1) // scale
		s.particleAt(w, false, pos, 0, 0, 0, 0, 0)
	case particle.Note:
		// Vanilla: count 0 makes the speed the velocity, and the note particle colours itself
		// from the x velocity (note/24).
		b := cube.PosFromVec3(pos)
		at := mgl64.Vec3{float64(b[0]) + 0.5, float64(b[1]) + 1.2, float64(b[2]) + 0.5}
		w := s.particleType(ptNote)
		s.particleAt(w, false, at, float32(pa.Pitch)/24, 0, 0, 1, 0)
	case particle.BlockForceField:
		w := s.particleType(ptBlockMarker)
		w.VarInt(s.ver.BlockState(barrierState))
		s.particleAt(w, false, pos, 0, 0, 0, 0, 0)
	case particle.BoneMeal:
		s.levelEvent(levelEventBoneMeal, cube.PosFromVec3(pos), 15, false)
	case particle.DragonEggTeleport:
		// Dragonfly's Diff is old minus new; Java packs new minus old around radii 16, 8, 16.
		d := pa.Diff
		data := int32((-d[0]+16)&0xff)<<16 | int32((-d[1]+8)&0xff)<<8 | int32((-d[2]+16)&0xff)
		s.levelEvent(levelEventDragonEggTeleport, cube.PosFromVec3(pos), data, false)
	case particle.Evaporate:
		s.levelEvent(levelEventEvaporate, cube.PosFromVec3(pos), 0, false)
	case particle.WaterDrip:
		s.simpleParticle(ptDrippingWater, pos, 0)
	case particle.LavaDrip:
		s.simpleParticle(ptDrippingLava, pos, 0)
	case particle.Lava:
		s.simpleParticle(ptLava, pos, 0)
	case particle.DustPlume:
		s.simpleParticle(ptDustPlume, pos, 0)
	case particle.HugeExplosion:
		s.simpleParticle(ptExplosionEmitter, pos, 0)
	case particle.EndermanTeleport:
		// 128 portal particles around pos (the destination packed as no difference: 127 each).
		s.levelEvent(levelEventEndermanTeleport, cube.PosFromVec3(pos), 0x7f7f7f, false)
	case particle.SnowballPoof:
		s.simpleParticle(ptItemSnowball, pos, 8)
	case particle.EggSmash:
		w := s.particleType(ptItem)
		w.VarInt(s.itemID(javaEggItem)) // ItemStackTemplate: item, count, empty component patch
		w.VarInt(1)
		w.VarInt(0)
		w.VarInt(0)
		s.particleAt(w, false, pos, 0, 0, 0, 0.03, 8)
	case particle.Splash:
		c := pa.Colour
		if c == (color.RGBA{}) {
			c = color.RGBA{R: 0x38, G: 0x5d, B: 0xc6, A: 0xff}
		}
		s.levelEvent(levelEventPotionSplash, cube.PosFromVec3(pos), rgb(c), false)
	case particle.Effect:
		w := s.particleType(ptEntityEffect)
		w.Int32(argb(pa.Colour))
		s.particleAt(w, false, pos, 0, 0, 0, 0, 0)
	}
}

func rgb(c color.RGBA) int32 { return int32(c.R)<<16 | int32(c.G)<<8 | int32(c.B) }

func argb(c color.RGBA) int32 {
	a := c.A
	if a == 0 {
		a = 0xff
	}
	return int32(a)<<24 | rgb(c)
}

// particleType starts a level_particles packet: the particle type, then its options.
func (s *Session) particleType(t jparticle) *wire.Writer {
	w := s.packet()
	w.VarInt(s.particleID(t))
	return w
}

// simpleParticle sends a particle without options. count 0 is one particle at pos, at rest.
func (s *Session) simpleParticle(t jparticle, pos mgl64.Vec3, count int32) {
	w := s.particleType(t)
	s.particleAt(w, particleOverrideLimiter[t], pos, 0, 0, 0, 0, count)
}

// particleAt writes the rest of a 26.3 level_particles packet and queues it: override limiter,
// always show, position, spread (or with count 0 the direction), the per-axis max speed, count and
// randomisation (0: gaussian, like vanilla's /particle).
func (s *Session) particleAt(w *wire.Writer, override bool, pos mgl64.Vec3, dx, dy, dz, speed float32, count int32) {
	if !s.ver.Native() {
		s.particleAt262(w, override, pos, dx, dy, dz, speed, count)
		return
	}
	w.Bool(override)
	w.Bool(false)
	w.Float64(pos[0])
	w.Float64(pos[1])
	w.Float64(pos[2])
	w.Float32(dx)
	w.Float32(dy)
	w.Float32(dz)
	w.Float32(speed)
	w.Float32(speed)
	w.Float32(speed)
	w.VarInt(count)
	w.VarInt(0)
	s.queue(v777.ClientboundPlayLevelParticles, w)
}

// particleAt262 writes a 26.2 level_particles packet: override limiter, always show, position,
// spread, one max speed, an int count and then the particle (w holds its type and options, as
// particleType and the caller wrote them). A type the client lacks is not sent.
func (s *Session) particleAt262(opts *wire.Writer, override bool, pos mgl64.Vec3, dx, dy, dz, speed float32, count int32) {
	if len(opts.B) == 0 || bytes.HasPrefix(opts.B, varIntMinus1) {
		s.writers.Put(opts) // no such particle in this version
		return
	}
	w := s.packet()
	w.Bool(override)
	w.Bool(false)
	w.Float64(pos[0])
	w.Float64(pos[1])
	w.Float64(pos[2])
	w.Float32(dx)
	w.Float32(dy)
	w.Float32(dz)
	w.Float32(speed)
	w.Int32(count)
	w.Raw(opts.B)
	s.writers.Put(opts)
	s.queue(v777.ClientboundPlayLevelParticles, w)
}

// varIntMinus1 is the VarInt -1, the particle type id of a particle the client's version lacks.
var varIntMinus1 = wire.AppendVarInt(nil, -1)
