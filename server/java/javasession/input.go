package javasession

import (
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	v777 "github.com/ezchr/go-mcjava/v777"
	"github.com/ezchr/go-mcjava/wire"
	"github.com/go-gl/mathgl/mgl64"
)

// inputState is the client input the session keeps between packets.
type inputState struct {
	sneaking  bool
	jumping   bool
	breaking  bool
	breakFace cube.Face
	breakPos  cube.Pos
}

// validFace reports whether a face from the client is one of the six.
func validFace(f cube.Face) bool { return f >= cube.FaceDown && f <= cube.FaceEast }

// clamp01 keeps a click position component within the block (NaN becomes 0.5).
func clamp01(v float32) float64 {
	if v != v {
		return 0.5
	}
	return float64(max(0, min(1, v)))
}

// Java player_action actions.
const (
	actionStartDestroy = iota
	actionChangeDestroyDirection
	actionAbortDestroy
	actionStopDestroy
	actionDropAll
	actionDropItem
	actionReleaseUseItem
	actionSwapOffhand
	actionStab
)

// Java player_input flags.
const (
	inputJump  = 16
	inputShift = 32
)

// Java player_command actions.
const (
	commandStopSleeping = iota
	commandStartSprinting
	commandStopSprinting
	commandStartRidingJump
	commandStopRidingJump
	commandOpenInventory
	commandStartFallFlying
)

// handleInput handles the client's world interaction packets.
func (s *Session) handleInput(id int32, body []byte) (bool, error) {
	r := wire.NewReader(body)
	switch id {
	case v777.ServerboundPlayPlayerAction:
		action := r.VarInt()
		if s.ver.Protocol < 777 && action >= actionChangeDestroyDirection {
			action++ // 26.3 inserted CHANGE_DESTROY_DIRECTION at 1
		}
		x, y, z := r.Position()
		face := cube.Face(r.Byte())
		seq := r.VarInt()
		if r.Err != nil || !validFace(face) {
			return true, r.Err
		}
		pos := cube.Pos{x, y, z}
		s.fxDestroyAction(action, pos)
		s.do(func(tx *world.Tx, c session.Controllable) {
			// Bedrock clients drive the breaking animation with start/continue/stop and send the
			// actual break separately (Dragonfly's BreakBlock). A Java client breaks on START in
			// creative and when the block breaks within a tick (flowers, torches, instamining), and
			// otherwise says STOP when it finished breaking the block it STARTed on.
			switch action {
			case actionStartDestroy:
				if c.GameMode().CreativeInventory() {
					c.BreakBlock(pos)
					return
				}
				if bt, ok := c.(interface{ BreakTime(cube.Pos) time.Duration }); ok && bt.BreakTime(pos) <= time.Second/20 {
					c.BreakBlock(pos)
					s.input.breaking = false
					return
				}
				c.StartBreaking(pos, face)
				s.input.breaking, s.input.breakFace, s.input.breakPos = true, face, pos
			case actionAbortDestroy:
				c.AbortBreaking()
				s.input.breaking = false
			case actionStopDestroy:
				// Only the block being broken: a STOP for any other position (or without a START)
				// would break blocks without breaking them.
				if !s.input.breaking || pos != s.input.breakPos {
					return
				}
				c.FinishBreaking()
				c.BreakBlock(pos)
				s.input.breaking = false
			case actionReleaseUseItem:
				c.ReleaseItem()
			case actionDropAll, actionDropItem:
				s.dropHeldItem(c, action == actionDropAll)
			case actionSwapOffhand:
				s.swapHands(c)
			}
		})
		s.ackBlock(seq)
	case v777.ServerboundPlayUseItemOn:
		hand := r.VarInt()
		x, y, z := r.Position()
		face := cube.Face(r.VarInt())
		cx, cy, cz := r.Float32(), r.Float32(), r.Float32()
		r.Bool() // inside block
		r.Bool() // world border hit
		seq := r.VarInt()
		if r.Err != nil || !validFace(face) {
			return true, r.Err
		}
		if hand == 0 {
			pos := cube.Pos{x, y, z}
			// The click position is within the block; clamp what a hostile client sends (NaN too).
			click := mgl64.Vec3{clamp01(cx), clamp01(cy), clamp01(cz)}
			s.fxUsedOn(pos)
			s.do(func(tx *world.Tx, c session.Controllable) { c.UseItemOnBlock(pos, face, click) })
		}
		s.ackBlock(seq)
	case v777.ServerboundPlayUseItem:
		hand := r.VarInt()
		seq := r.VarInt()
		r.Float32()
		r.Float32()
		if r.Err != nil {
			return true, r.Err
		}
		if hand == 0 {
			s.fxUsedItem()
			s.do(func(tx *world.Tx, c session.Controllable) { c.UseItem() })
		}
		s.ackBlock(seq)
	case v777.ServerboundPlayAttack:
		target := r.VarInt()
		if r.Err != nil {
			return true, r.Err
		}
		s.do(func(tx *world.Tx, c session.Controllable) {
			e, ok := s.entityByID(tx, target)
			if !ok {
				s.log.Debug("attack: unknown target", "id", target)
				return
			}
			// AttackEntity swings the arm itself when the attack is valid.
			hit := c.AttackEntity(e)
			s.log.Debug("attack", "target", target, "hit", hit)
		})
	case v777.ServerboundPlayPunch:
		s.do(func(tx *world.Tx, c session.Controllable) { c.PunchAir() })
	case v777.ServerboundPlayInteract:
		target := r.VarInt()
		hand := r.VarInt()
		cx, cy, cz := r.LpVec3() // where on the entity, relative to its position
		r.Bool()                 // sneaking
		if r.Err != nil {
			return true, r.Err
		}
		if hand == 0 {
			clicked := mgl64.Vec3{cx, cy, cz}
			s.do(func(tx *world.Tx, c session.Controllable) {
				e, ok := s.entityByID(tx, target)
				if !ok {
					return
				}
				// As Dragonfly's Bedrock interact handler: using a rideable (a cushion, a boat)
				// also gets on it, in the seat nearest the click.
				if c.UseItemOnEntity(e) {
					if rideable, ok := e.(entity.Rideable); ok {
						if seat, ok := rideable.NextFreeSeatIndex(e.Position().Add(clicked)); ok {
							c.MountEntity(tx, rideable, seat)
						}
					}
				}
			})
		}
	case v777.ServerboundPlayPlayerInput:
		flags := r.Byte()
		if r.Err != nil {
			return true, r.Err
		}
		sneak, jump := flags&inputShift != 0, flags&inputJump != 0
		s.do(func(tx *world.Tx, c session.Controllable) {
			if sneak && !s.input.sneaking && c.RidingEntityHandle() != nil {
				// Java gets off a seat with the sneak key (Bedrock sends a leave-vehicle interact).
				c.DismountEntity(tx)
			}
			if sneak != s.input.sneaking {
				if sneak {
					c.StartSneaking()
				} else {
					c.StopSneaking()
				}
				s.input.sneaking = sneak
			}
			if jump && !s.input.jumping {
				c.Jump()
			}
			s.input.jumping = jump
		})
	case v777.ServerboundPlayPlayerCommand:
		r.VarInt() // entity id: always the player
		action := r.VarInt()
		r.VarInt()
		if r.Err != nil {
			return true, r.Err
		}
		s.do(func(tx *world.Tx, c session.Controllable) {
			switch action {
			case commandStopSleeping: // the Leave Bed button
				c.Wake()
			case commandStartSprinting:
				c.StartSprinting()
			case commandStopSprinting:
				c.StopSprinting()
			case commandStartFallFlying: // elytra
				c.StartGliding()
			}
		})
	case v777.ServerboundPlayClientCommand:
		if action := r.VarInt(); r.Err == nil && action == 0 { // perform respawn
			s.do(func(tx *world.Tx, c session.Controllable) { c.Respawn() })
		}
	default:
		return false, nil
	}
	return true, r.Err
}

// do runs f in the player's world transaction, logging unexpected failures.
func (s *Session) do(f func(tx *world.Tx, c session.Controllable)) {
	if err := s.withPlayer(f); err != nil && !stopped(err) {
		s.log.Debug("input", "err", err)
	}
}

// ackBlock tells the client the server handled its block action up to seq, so it drops its
// prediction (and shows the server's blocks instead).
func (s *Session) ackBlock(seq int32) {
	w := s.packet()
	w.VarInt(seq)
	s.queue(v777.ClientboundPlayBlockChangedAck, w)
}

// continueBreaking is called every tick while the client holds the break key.
func (s *Session) continueBreaking(c session.Controllable) {
	if s.input.breaking {
		c.ContinueBreaking(s.input.breakFace)
	}
}

// entityByID finds an entity the client knows by its Java id.
func (s *Session) entityByID(tx *world.Tx, id int32) (world.Entity, bool) {
	s.entMu.Lock()
	var h *world.EntityHandle
	for eh, eid := range s.entityIDs {
		if eid == id {
			h = eh
			break
		}
	}
	s.entMu.Unlock()
	if h == nil {
		return nil, false
	}
	return h.Entity(tx)
}
