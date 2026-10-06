package javasession

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	v777 "github.com/ezchr/go-mcjava/v777"
	"github.com/ezchr/go-mcjava/version"
	"github.com/go-gl/mathgl/mgl64"
)

// damageGeneric is minecraft:generic in the damage_type registry.
var damageGeneric = v777.RegistryID("minecraft:damage_type", "minecraft:generic")

// Java entity type ids (minecraft:entity_type protocol ids in Mojang's registries.json).
const entityTypePlayer = 159

// entityID returns the Java entity id this client knows e by.
func (s *Session) entityID(e world.Entity) (int32, bool) {
	if e.H() == s.ent {
		return selfEntityID, true
	}
	s.entMu.Lock()
	defer s.entMu.Unlock()
	id, ok := s.entityIDs[e.H()]
	return id, ok
}

func (s *Session) addEntityID(e world.Entity) int32 {
	s.entMu.Lock()
	defer s.entMu.Unlock()
	if id, ok := s.entityIDs[e.H()]; ok {
		return id
	}
	s.nextEntityID++
	if s.nextEntityID == selfEntityID {
		s.nextEntityID++
	}
	s.entityIDs[e.H()] = s.nextEntityID
	return s.nextEntityID
}

func (s *Session) removeEntityID(e world.Entity) (int32, bool) {
	s.entMu.Lock()
	defer s.entMu.Unlock()
	id, ok := s.entityIDs[e.H()]
	delete(s.entityIDs, e.H())
	delete(s.tracks, id)
	return id, ok
}

// tabName is a name the Java client accepts in the tab list (at most 16 characters).
func tabName(n string) string {
	r := []rune(n)
	if len(r) > 16 {
		r = r[:16]
	}
	return string(r)
}

// ViewEntity shows an entity that came into view.
func (s *Session) ViewEntity(e world.Entity) {
	if e.H() == s.ent {
		return
	}
	p, ok := e.(*player.Player)
	if !ok {
		s.viewOtherEntity(e)
		return
	}
	if s.deferForSkin(p) {
		return
	}
	id := s.addEntityID(e)
	u := p.UUID()

	// Tab list entry first: the client needs the profile to spawn a player entity.
	s.showTabFor(p)

	pos, rot := p.Position(), p.Rotation()
	w := s.packet()
	w.VarInt(id)
	w.UUID(u)
	w.VarInt(s.ver.Builtin("minecraft:entity_type", entityTypePlayer))
	w.Float64(pos[0])
	w.Float64(pos[1])
	w.Float64(pos[2])
	w.LpVec3(0, 0, 0)
	w.Angle(float32(rot.Pitch()))
	w.Angle(float32(rot.Yaw()))
	w.Angle(float32(rot.Yaw())) // head
	w.VarInt(0)
	s.queue(v777.ClientboundPlayAddEntity, w)
	s.setTrack(id, pos, rot)
	s.viewPlayerNameTag(p)
	s.noteRiding(e)
}

// HideEntity removes an entity that left view.
func (s *Session) HideEntity(e world.Entity) {
	if e.H() == s.ent {
		return
	}
	s.entMu.Lock()
	delete(s.deferred, e.H()) // a deferred spawn that has not happened yet is called off
	s.entMu.Unlock()
	id, ok := s.removeEntityID(e)
	if !ok {
		return
	}
	w := s.packet()
	w.VarInt(1)
	w.VarInt(id)
	s.queue(v777.ClientboundPlayRemoveEntities, w)
	s.forgetEntityText(e)
	s.forgetRiding(e.H())
	if p, ok := e.(*player.Player); ok {
		s.hideTabFor(p.UUID()) // online players stay listed
	}
}

// ViewEntityMovement moves another entity to an absolute position.
func (s *Session) ViewEntityMovement(e world.Entity, pos mgl64.Vec3, rot cube.Rotation, onGround bool) {
	if e.H() == s.ent {
		return // the client moves itself
	}
	s.positionSync(e, pos, rot, onGround)
}

// ViewEntityDisplacement is a server-made move (knockback correction, pushing).
func (s *Session) ViewEntityDisplacement(e world.Entity, pos mgl64.Vec3, rot cube.Rotation, onGround bool) {
	if e.H() == s.ent {
		s.teleport(pos[0], pos[1], pos[2], float32(rot.Yaw()), float32(rot.Pitch()))
		return
	}
	s.positionSync(e, pos, rot, onGround)
}

// ViewEntityTeleport moves an entity instantly.
func (s *Session) ViewEntityTeleport(e world.Entity, pos mgl64.Vec3) {
	rot := e.Rotation()
	if e.H() == s.ent {
		s.teleport(pos[0], pos[1], pos[2], float32(rot.Yaw()), float32(rot.Pitch()))
		return
	}
	s.positionSync(e, pos, rot, false)
}

// track is what the client last knew about an entity's position: the base its 26.3 delta packets
// are relative to (VecDeltaCodec), the last rotation bytes and when it last got a full sync.
type track struct {
	base           mgl64.Vec3
	yaw, pitch, hd byte
	sinceSync      int
	synced         bool
}

func encodeDelta(v float64) int64 { return int64(math.Floor(v*4096 + 0.5)) }

// positionSync moves another entity: a delta move (move_entity_pos/_pos_rot/_rot) when it fits,
// else a full entity_position_sync, which vanilla also sends every 60 moves to undo drift.
func (s *Session) positionSync(e world.Entity, pos mgl64.Vec3, rot cube.Rotation, onGround bool) {
	id, ok := s.entityID(e)
	if !ok {
		return
	}
	yaw, pitch := angleByte(rot.Yaw()), angleByte(rot.Pitch())
	s.entMu.Lock()
	t := s.tracks[id]
	if t == nil {
		t = &track{}
		s.tracks[id] = t
	}
	var d [3]int64
	fits := t.synced && t.sinceSync < 60
	for i := range d {
		d[i] = encodeDelta(pos[i]) - encodeDelta(t.base[i])
		if d[i] < math.MinInt16 || d[i] > math.MaxInt16 {
			fits = false
		}
	}
	moved := d[0] != 0 || d[1] != 0 || d[2] != 0
	turned := yaw != t.yaw || pitch != t.pitch
	headTurned := yaw != t.hd
	if fits {
		t.sinceSync++
		for i := range d {
			if d[i] != 0 { // the client's VecDeltaCodec.decode
				t.base[i] = float64(encodeDelta(t.base[i])+d[i]) / 4096
			}
		}
	} else {
		t.base, t.sinceSync, t.synced = pos, 0, true
	}
	t.yaw, t.pitch, t.hd = yaw, pitch, yaw
	s.entMu.Unlock()

	if !s.ver.Native() {
		s.positionSync262(id, fits, moved, turned, headTurned, d, yaw, pitch, pos, rot, onGround)
		return
	}
	if fits {
		props := int32(0)
		if onGround {
			props = 1 // bit 0 on ground, step count 0 (a linear delta)
		}
		switch {
		case moved && turned:
			w := s.packet()
			w.VarInt(id)
			w.VarInt(props)
			w.Int16(int16(d[0]))
			w.Int16(int16(d[1]))
			w.Int16(int16(d[2]))
			w.Byte(yaw)
			w.Byte(pitch)
			s.queue(v777.ClientboundPlayMoveEntityPosRot, w)
		case moved:
			w := s.packet()
			w.VarInt(id)
			w.VarInt(props)
			w.Int16(int16(d[0]))
			w.Int16(int16(d[1]))
			w.Int16(int16(d[2]))
			s.queue(v777.ClientboundPlayMoveEntityPos, w)
		case turned:
			w := s.packet()
			w.VarInt(id)
			w.Bool(onGround)
			w.Byte(yaw)
			w.Byte(pitch)
			s.queue(v777.ClientboundPlayMoveEntityRot, w)
		}
		if headTurned {
			w := s.packet()
			w.VarInt(id)
			w.Byte(yaw)
			s.queue(v777.ClientboundPlayRotateHead, w)
		}
		return
	}
	w := s.packet()
	w.VarInt(id)
	w.VarInt(0) // PositionPath.LINEAR
	w.Float64(pos[0])
	w.Float64(pos[1])
	w.Float64(pos[2])
	w.Float32(float32(rot.Yaw()))
	w.Float32(float32(rot.Pitch()))
	w.Bool(onGround)
	s.queue(v777.ClientboundPlayEntityPositionSync, w)
	w = s.packet()
	w.VarInt(id)
	w.Angle(float32(rot.Yaw()))
	s.queue(v777.ClientboundPlayRotateHead, w)
}

// positionSync262 writes positionSync's packets in the 26.2 layouts: delta moves are three shorts
// with onGround as a trailing bool (move_entity_rot: yaw, pitch, onGround), and
// entity_position_sync is position, delta movement (unused by the client), yaw, pitch, onGround.
// The delta codec is the same as 26.3's.
func (s *Session) positionSync262(id int32, fits, moved, turned, headTurned bool, d [3]int64, yaw, pitch byte, pos mgl64.Vec3, rot cube.Rotation, onGround bool) {
	if fits {
		switch {
		case moved && turned:
			w := s.packet()
			w.VarInt(id)
			w.Int16(int16(d[0]))
			w.Int16(int16(d[1]))
			w.Int16(int16(d[2]))
			w.Byte(yaw)
			w.Byte(pitch)
			w.Bool(onGround)
			s.queue(v777.ClientboundPlayMoveEntityPosRot, w)
		case moved:
			w := s.packet()
			w.VarInt(id)
			w.Int16(int16(d[0]))
			w.Int16(int16(d[1]))
			w.Int16(int16(d[2]))
			w.Bool(onGround)
			s.queue(v777.ClientboundPlayMoveEntityPos, w)
		case turned:
			w := s.packet()
			w.VarInt(id)
			w.Byte(yaw)
			w.Byte(pitch)
			w.Bool(onGround)
			s.queue(v777.ClientboundPlayMoveEntityRot, w)
		}
		if headTurned {
			w := s.packet()
			w.VarInt(id)
			w.Byte(yaw)
			s.queue(v777.ClientboundPlayRotateHead, w)
		}
		return
	}
	w := s.packet()
	w.VarInt(id)
	w.Float64(pos[0])
	w.Float64(pos[1])
	w.Float64(pos[2])
	w.Float64(0) // delta movement
	w.Float64(0)
	w.Float64(0)
	w.Float32(float32(rot.Yaw()))
	w.Float32(float32(rot.Pitch()))
	w.Bool(onGround)
	s.queue(v777.ClientboundPlayEntityPositionSync, w)
	w = s.packet()
	w.VarInt(id)
	w.Angle(float32(rot.Yaw()))
	s.queue(v777.ClientboundPlayRotateHead, w)
}

// setTrack records what an add_entity told the client: the delta base starts at the spawn position.
func (s *Session) setTrack(id int32, pos mgl64.Vec3, rot cube.Rotation) {
	y := angleByte(rot.Yaw())
	s.entMu.Lock()
	s.tracks[id] = &track{base: pos, yaw: y, pitch: angleByte(rot.Pitch()), hd: y, synced: true}
	s.entMu.Unlock()
}

// angleByte is an angle in degrees as the 1/256-turn byte Java uses.
func angleByte(deg float64) byte { return byte(int32(math.Floor(deg * 256 / 360))) }

// ViewEntityVelocity sets an entity's motion; for the player itself this is knockback.
func (s *Session) ViewEntityVelocity(e world.Entity, vel mgl64.Vec3) {
	id, ok := s.entityID(e)
	if !ok {
		return
	}
	w := s.packet()
	w.VarInt(id)
	w.LpVec3(vel[0], vel[1], vel[2])
	s.queue(v777.ClientboundPlaySetEntityMotion, w)
}

// ViewEntityAction plays an entity animation.
func (s *Session) ViewEntityAction(e world.Entity, a world.EntityAction) {
	id, ok := s.entityID(e)
	if !ok {
		return
	}
	switch a.(type) {
	case entity.SwingArmAction:
		if id == selfEntityID {
			return // the client already swung
		}
		if !s.ver.Native() {
			// 26.2 has no swing_animation: animate action 0 swings the main hand (the client casts
			// the entity to LivingEntity; SwingArmAction is only sent for living entities).
			w := s.packet()
			w.VarInt(id)
			w.Byte(animate262SwingMainHand)
			s.queue(v777.ClientboundPlayAnimate, w)
			return
		}
		// 26.3 swings with swing_animation (animate no longer has a swing action).
		w := s.packet()
		w.VarInt(id)
		w.VarInt(0) // main hand
		w.VarInt(1) // SwingAnimationType.WHACK
		w.VarInt(6) // duration in ticks (SwingAnimation.DEFAULT)
		s.queue(v777.ClientboundPlaySwingAnimation, w)
	case entity.HurtAction:
		// damage_event makes the client play both the hurt animation and the entity's hurt sound.
		dt := damageGeneric
		if l := s.legacy(); l != nil {
			dt = version.Map(l.damageType, dt)
		}
		w := s.packet()
		w.VarInt(id)
		w.VarInt(dt)
		w.VarInt(0) // no cause entity
		w.VarInt(0) // no direct entity
		w.Bool(false)
		s.queue(v777.ClientboundPlayDamageEvent, w)
	case entity.CriticalHitAction:
		s.animate(id, 1) // CRITICAL_HIT
	case entity.EnchantedHitAction:
		s.animate(id, 2) // MAGIC_CRITICAL_HIT
	}
}

// animate sends an animate packet (26.3 actions: 0 wake up, 1 critical hit, 2 magic critical hit).
// 26.2 numbers them by id instead: 0 swing main hand, 2 wake up, 3 swing off hand, 4 critical
// hit, 5 magic critical hit.
func (s *Session) animate(id int32, action byte) {
	if !s.ver.Native() {
		if int(action) >= len(animate262) {
			return
		}
		action = animate262[action]
	}
	w := s.packet()
	w.VarInt(id)
	w.Byte(action)
	s.queue(v777.ClientboundPlayAnimate, w)
}

// animate262 maps the 26.3 animate actions (ordinals) to the 26.2 action ids.
var animate262 = [...]byte{0: 2, 1: 4, 2: 5}

const animate262SwingMainHand = 0

// ViewBlockUpdate sends a changed block.
func (s *Session) ViewBlockUpdate(pos cube.Pos, b world.Block, layer int) {
	if layer != 0 {
		return // water layer changes: waterlogging fixer to come
	}
	rid := world.BlockRuntimeID(b)
	bi := s.blk
	if int(rid) >= len(bi.java) {
		return
	}
	w := s.packet()
	w.Position(pos[0], pos[1], pos[2])
	w.VarInt(int32(bi.java[rid]))
	s.queue(v777.ClientboundPlayBlockUpdate, w)
}
