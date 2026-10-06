package javasession

import (
	"errors"
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	v777 "github.com/ezchr/go-mcjava/v777"
	"github.com/ezchr/go-mcjava/wire"
	"github.com/go-gl/mathgl/mgl64"
)

// handle handles one packet from the client.
func (s *Session) handle(id int32, body []byte) error {
	r := wire.NewReader(body)
	switch id {
	case v777.ServerboundPlayKeepAlive:
		// Only the keep-alive we are waiting for counts (ids are send times in nanoseconds).
		if ka := r.Int64(); ka != 0 && s.keepAlive.CompareAndSwap(ka, 0) {
			// The whole round trip, smoothed like vanilla (3/4 old, 1/4 new; the first
			// measurement as it is).
			sent := ka
			if t := s.keepAliveSent.Load(); t > ka {
				sent = t // when it left the server, not when it was queued
			}
			rtt := int64(min(time.Duration(time.Now().UnixNano()-sent), time.Minute))
			if old := s.latency.Load(); old != 0 {
				rtt = (old*3 + rtt) / 4
			}
			s.latency.Store(max(rtt, 1))
		}
	case v777.ServerboundPlayAcceptTeleportation:
		tp := r.VarInt()
		s.pendingTeleport.CompareAndSwap(tp, 0)
	case v777.ServerboundPlayMovePlayerPos:
		x, y, z := r.Float64(), r.Float64(), r.Float64()
		flags := r.Byte()
		if r.Err == nil {
			return s.move(&mgl64.Vec3{x, y, z}, nil, flags)
		}
	case v777.ServerboundPlayMovePlayerPosRot:
		x, y, z := r.Float64(), r.Float64(), r.Float64()
		yaw, pitch := r.Float32(), r.Float32()
		flags := r.Byte()
		if r.Err == nil {
			return s.move(&mgl64.Vec3{x, y, z}, &[2]float32{yaw, pitch}, flags)
		}
	case v777.ServerboundPlayMovePlayerRot:
		yaw, pitch := r.Float32(), r.Float32()
		flags := r.Byte()
		if r.Err == nil {
			return s.move(nil, &[2]float32{yaw, pitch}, flags)
		}
	case v777.ServerboundPlayChunkBatchReceived:
		rate := float64(r.Float32())
		if rate != rate || rate < 0.01 { // NaN or nonsense: vanilla clamps the same way
			rate = 0.01
		}
		s.chunkRate.Store(int64(min(rate, 64) * 1000))
		s.batchInFlight.Store(false)
	default:
		if ok, err := s.handleTextPacket(id, body); ok {
			return err
		}
		if ok, err := s.handleInventoryPacket(id, body); ok {
			return err
		}
		if ok, err := s.handleInput(id, body); ok {
			return err
		}
	}
	return r.Err
}

// errInvalidMove is returned for a move a vanilla client cannot send (NaN, infinite or far
// outside any world); the client is disconnected, like vanilla does.
var errInvalidMove = errors.New("invalid move")

// validMove reports whether a client position and rotation are finite and inside the limits
// vanilla accepts (ServerGamePacketListenerImpl.containsInvalidValues / clamps).
func validMove(pos *mgl64.Vec3, rot *[2]float32) bool {
	if pos != nil {
		for _, v := range pos {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return false
			}
		}
		if math.Abs(pos[0]) > 3e7 || math.Abs(pos[2]) > 3e7 || math.Abs(pos[1]) > 2e7 {
			return false
		}
	}
	if rot != nil {
		for _, v := range rot {
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				return false
			}
		}
	}
	return true
}

// move applies a client move. Positions are feet positions on both editions.
func (s *Session) move(pos *mgl64.Vec3, rot *[2]float32, flags byte) error {
	if !validMove(pos, rot) {
		return errInvalidMove
	}
	if rot != nil {
		rot[1] = max(-90, min(90, rot[1]))
	}
	err := s.withPlayer(func(tx *world.Tx, c session.Controllable) {
		// Checked in the world transaction: a teleport made there after this packet was read
		// must not be undone by it.
		if s.pendingTeleport.Load() != 0 {
			return // the client has not caught up with our last teleport yet
		}
		if tx.World() != s.loader.World() {
			return // moved to another world: the next tick's switchWorld teleports the client
		}
		var delta mgl64.Vec3
		if pos != nil {
			delta = pos.Sub(c.Position())
		}
		var dyaw, dpitch float64
		if rot != nil {
			r := c.Rotation()
			dyaw, dpitch = float64(rot[0])-r.Yaw(), float64(rot[1])-r.Pitch()
		}
		c.Move(delta, dyaw, dpitch)
	})
	if err != nil && !stopped(err) {
		s.log.Debug("move", "err", err)
	}
	return nil
}
