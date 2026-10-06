package javasession

import (
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/world"
	v777 "github.com/ezchr/go-mcjava/v777"
	"github.com/ezchr/go-mcjava/version"
	"github.com/ezchr/go-mcjava/wire"
)

// update_mob_effect flags.
const (
	effectAmbient = 1
	effectVisible = 2 // particles
	effectIcon    = 4
	effectBlend   = 8 // fade in (vanilla sets it for effects sent to the player itself)
)

// javaEffect is the Java mob_effect id of a Dragonfly effect type.
func javaEffect(t effect.Type) (int32, bool) {
	id, ok := effect.ID(t)
	if !ok || id < 0 || id >= len(mobEffects) || mobEffects[id] < 0 {
		return 0, false
	}
	return mobEffects[id], true
}

// SendEffect shows one of the player's own effects (HUD icon, and the client side of jump boost,
// levitation, slow falling, night vision, blindness, darkness, nausea, haste and mining fatigue).
func (s *Session) SendEffect(e effect.Effect) {
	id, ok := javaEffect(e.Type())
	if !ok {
		return
	}
	if l := s.legacy(); l != nil {
		if id = version.Map(l.mobEffect, id); id < 0 {
			return
		}
	}
	dur := int32(e.Duration() / (time.Second / 20))
	if e.Infinite() {
		dur = -1
	}
	flags := byte(effectIcon | effectBlend)
	if e.Ambient() {
		flags |= effectAmbient
	}
	if !e.ParticlesHidden() {
		flags |= effectVisible
	}
	w := s.packet()
	w.VarInt(selfEntityID)
	w.VarInt(id) // Holder<MobEffect> by registry id
	w.VarInt(int32(e.Level() - 1))
	w.VarInt(dur)
	w.Byte(flags)
	s.queue(v777.ClientboundPlayUpdateMobEffect, w)
}

// SendEffectRemoval removes one of the player's own effects.
func (s *Session) SendEffectRemoval(t effect.Type) {
	id, ok := javaEffect(t)
	if !ok {
		return
	}
	if l := s.legacy(); l != nil {
		if id = version.Map(l.mobEffect, id); id < 0 {
			return
		}
	}
	w := s.packet()
	w.VarInt(selfEntityID)
	w.VarInt(id)
	s.queue(v777.ClientboundPlayRemoveMobEffect, w)
}

// attributeMovementSpeed is minecraft:movement_speed in the minecraft:attribute registry.
var attributeMovementSpeed = v777.BuiltinID("minecraft:attribute", "minecraft:movement_speed")

// SendSpeed sets the player's movement speed. Dragonfly's speed already holds sprinting (x1.3) and
// the speed and slowness effects, so it is sent as the base value without modifiers: the client
// then drops its own sprint modifier (handleUpdateAttributes clears modifiers) and moves at
// exactly Dragonfly's speed, which a vanilla client also does between sprint toggles.
func (s *Session) SendSpeed(speed float64) {
	attr := attributeMovementSpeed
	if l := s.legacy(); l != nil {
		if attr = version.Map(l.attribute, attr); attr < 0 {
			return
		}
	}
	w := s.packet()
	w.VarInt(selfEntityID)
	w.VarInt(1)
	w.VarInt(attr)
	w.Float64(speed)
	w.VarInt(0) // modifiers
	s.queue(v777.ClientboundPlayUpdateAttributes, w)
}

// LivingEntity metadata (26.3): after Entity's 0-7 come flags 8, health 9, effect particles 10,
// effect ambience 11, arrows 12, stingers 13, sleeping position 14.
const (
	metaPose            = 6
	metaEffectParticles = 10
	metaEffectAmbience  = 11
	metaSleepingPos     = 14

	dataTypeBoolean     = 8
	dataTypeOptBlockPos = 11
	dataTypeParticles   = 17

	poseStandingID = 0
	poseSleepingID = 2
)

// writeLivingFxData appends another living entity's effect particles (what the Java client shows
// as the swirls around a player with effects) and, for a sleeping player, the sleeping pose and
// bed position to a set_entity_data body. Hook: self.go, ViewEntityState, before
// w.Byte(0xff): s.writeLivingFxData(w, e)
func (s *Session) writeLivingFxData(w *wire.Writer, e world.Entity) {
	if eb, ok := e.(interface{ Effects() []effect.Effect }); ok {
		effects := eb.Effects()
		n, ambient := 0, true
		for _, ef := range effects {
			if !ef.ParticlesHidden() {
				n++
				ambient = ambient && ef.Ambient()
			}
		}
		w.Byte(metaEffectParticles)
		w.VarInt(dataTypeParticles)
		w.VarInt(int32(n))
		for _, ef := range effects {
			if ef.ParticlesHidden() {
				continue
			}
			// MobEffect.createParticleOptions: entity_effect in the effect's colour, alpha 38
			// for ambient (beacon) effects.
			c := ef.Type().RGBA()
			a := int32(255)
			if ef.Ambient() {
				a = 38
			}
			w.VarInt(s.particleID(ptEntityEffect))
			w.Int32(a<<24 | rgb(c))
		}
		w.Byte(metaEffectAmbience)
		w.VarInt(dataTypeBoolean)
		w.Bool(n > 0 && ambient)
	}
	if sl, ok := e.(interface{ Sleeping() (cube.Pos, bool) }); ok {
		pos, sleeping := sl.Sleeping()
		w.Byte(metaSleepingPos)
		w.VarInt(dataTypeOptBlockPos)
		w.Bool(sleeping)
		if sleeping {
			w.Position(pos[0], pos[1], pos[2])
			// Overrides the pose ViewEntityState wrote: later entries win.
			w.Byte(metaPose)
			w.VarInt(dataTypePose)
			w.VarInt(poseSleepingID)
		}
	}
}
