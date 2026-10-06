package javasession

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	v777 "github.com/ezchr/go-mcjava/v777"
	"github.com/go-gl/mathgl/mgl64"
)

// Java sends health and food in one packet; keep the last of each.
type vitals struct {
	health, saturation float64
	food               int
}

// SendHealth ...
func (s *Session) SendHealth(health, max, absorption float64) {
	s.vitalsMu.Lock()
	s.vitals.health = health
	v := s.vitals
	s.vitalsMu.Unlock()
	s.sendVitals(v)
}

// SendFood ...
func (s *Session) SendFood(food int, saturation, exhaustion float64) {
	s.vitalsMu.Lock()
	s.vitals.food, s.vitals.saturation = food, saturation
	v := s.vitals
	s.vitalsMu.Unlock()
	s.sendVitals(v)
}

func (s *Session) sendVitals(v vitals) {
	w := s.packet()
	w.Float32(float32(v.health))
	w.VarInt(int32(v.food))
	w.Float32(float32(v.saturation))
	s.queue(v777.ClientboundPlaySetHealth, w)
}

// SendExperience ...
func (s *Session) SendExperience(level int, progress float64) {
	w := s.packet()
	w.Float32(float32(progress))
	w.VarInt(int32(level))
	w.VarInt(0) // total experience is only shown on the death screen
	s.queue(v777.ClientboundPlaySetExperience, w)
}

// SendGameMode ...
func (s *Session) SendGameMode(c session.Controllable) {
	w := s.packet()
	w.Byte(3) // change game mode
	w.Float32(float32(gameModeID(c.GameMode())))
	s.queue(v777.ClientboundPlayGameEvent, w)
	s.updateSelfGameMode(gameModeID(c.GameMode()))
	s.SendAbilities(c)
}

// SendAbilities ...
func (s *Session) SendAbilities(c session.Controllable) {
	gm := c.GameMode()
	var flags byte
	if !gm.AllowsTakingDamage() {
		flags |= 1 // invulnerable
	}
	if c.Flying() {
		flags |= 2
	}
	if gm.AllowsFlying() {
		flags |= 4
	}
	if gm.CreativeInventory() {
		flags |= 8 // instant break
	}
	w := s.packet()
	w.Byte(flags)
	w.Float32(0.05) // flying speed
	w.Float32(0.1)  // walking speed (field of view)
	s.queue(v777.ClientboundPlayPlayerAbilities, w)
}

// SendRespawn is called when the player respawns. It takes the Java client off its death screen;
// in the same dimension the client keeps its chunks. A respawn in another world is followed by
// switchWorld on the next tick.
func (s *Session) SendRespawn(pos mgl64.Vec3, c session.Controllable) {
	s.closeWindowsAfterRespawn()
	s.writeRespawn(s.dim, c, 0)
	rot := c.Rotation()
	s.teleport(pos[0], pos[1], pos[2], float32(rot.Yaw()), float32(rot.Pitch()))
	// Like vanilla's sendLevelInfo: the client waits on "Loading terrain" until this event.
	p := s.packet()
	p.Byte(13) // start waiting for level chunks
	p.Float32(0)
	s.queue(v777.ClientboundPlayGameEvent, p)
	s.resendLevelInfo()
	s.resendInventory()
	s.SendAbilities(c)
}

// SendPlayerSpawn is the player's own spawn point; Java shows nothing for it.
func (s *Session) SendPlayerSpawn(mgl64.Vec3) {}

// ViewWorldSpawn ...
func (s *Session) ViewWorldSpawn(pos cube.Pos) {
	w := s.packet()
	w.String(s.dim)
	w.Position(pos[0], pos[1], pos[2])
	w.Float32(0)
	w.Float32(0)
	s.queue(v777.ClientboundPlaySetDefaultSpawnPosition, w)
}

// Entity data (metadata) for the shared entity flags and pose.
const (
	dataTypeByte = 0
	dataTypePose = 20

	poseStanding  = 0
	poseSwimming  = 3
	poseCrouching = 5
	poseSleeping  = 2
	poseFallFly   = 1
)

// stateful is what ViewEntityState reads from an entity.
type stateful interface {
	Sneaking() bool
	Sprinting() bool
	Swimming() bool
	Gliding() bool
}

// ViewEntityState sends another entity's flags (crouching, sprinting...) and pose.
func (s *Session) ViewEntityState(e world.Entity) {
	if e.H() == s.ent {
		return // the client knows its own state
	}
	id, ok := s.entityID(e)
	if !ok {
		return
	}
	if p, ok := e.(*player.Player); ok {
		s.viewPlayerNameTag(p)
	}
	if isTextEntity(e) {
		s.viewTextDisplay(e, id, false)
		return
	}
	st, ok := e.(stateful)
	if !ok {
		return
	}
	var flags byte
	pose := int32(poseStanding)
	if st.Sneaking() {
		flags |= 0x02
		pose = poseCrouching
	}
	if st.Sprinting() {
		flags |= 0x08
	}
	if st.Swimming() {
		flags |= 0x10
		pose = poseSwimming
	}
	if st.Gliding() {
		flags |= 0x80
		pose = poseFallFly
	}
	if f, ok := e.(interface{ OnFireDuration() int }); ok && f.OnFireDuration() > 0 {
		flags |= 0x01
	}
	if inv, ok := e.(interface{ Invisible() bool }); ok && inv.Invisible() {
		flags |= 0x20
	}
	w := s.packet()
	w.VarInt(id)
	w.Byte(0)
	w.VarInt(dataTypeByte)
	w.Byte(flags)
	w.Byte(6)
	w.VarInt(dataTypePose)
	w.VarInt(pose)
	s.writeLivingFxData(w, e)
	w.Byte(0xff)
	s.queue(v777.ClientboundPlaySetEntityData, w)
}
