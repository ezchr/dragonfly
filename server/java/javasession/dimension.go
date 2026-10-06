package javasession

import (
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	v777 "github.com/ezchr/go-mcjava/v777"
)

// dimSections is how many chunk sections a Java client expects in a dimension (its dimension
// type's height / 16). Dragonfly's nether is 128 blocks high but Java's is 256, so nether chunks
// are padded with empty sections: a column with fewer sections than the client expects fails to
// decode.
func dimSections(dim string) int {
	switch dim {
	case "minecraft:the_nether", "minecraft:the_end":
		return 16 // y 0 to 255
	}
	return 24 // y -64 to 319
}

// writeRespawn sends the respawn packet for dim. keep: 1 attributes, 2 entity data.
func (s *Session) writeRespawn(dim string, c session.Controllable, keep byte) {
	w := s.packet()
	w.VarInt(s.ver.RegistryID("minecraft:dimension_type", dim))
	w.String(dim)
	w.Int64(0)
	w.VarInt(gameModeID(c.GameMode()))
	w.VarInt(0)
	w.Bool(false)
	w.Bool(false)
	w.Bool(false) // no death location
	w.VarInt(0)
	w.VarInt(63)
	w.Byte(keep)
	s.queue(v777.ClientboundPlayRespawn, w)
}

// switchWorld follows the player into another world (portals, teleports between worlds).
func (s *Session) switchWorld(tx *world.Tx, w *world.World, c session.Controllable) {
	s.closeWindows(tx, c)
	if dim := dimensionKey(w.Dimension()); dim != s.dim {
		s.dim = dim
		s.forgetAllChunks(false)
		s.writeRespawn(dim, c, 3)
		// The client forgets every entity and chunk when it changes dimension.
		s.entMu.Lock()
		clear(s.entityIDs)
		clear(s.tracks)
		s.entMu.Unlock()
		s.centreSent = false
		s.batchInFlight.Store(false)
		p := s.packet()
		p.Byte(13) // start waiting for level chunks
		p.Float32(0)
		s.queue(v777.ClientboundPlayGameEvent, p)
		s.resendInventory()
		s.SendAbilities(c)
		s.SendHealth(c.Health(), c.MaxHealth(), c.Absorption())
		s.SendFood(c.Food(), 0, 0)
		s.resendLevelInfo() // the client has a new level: no time or weather yet
	} else {
		s.forgetAllChunks(true)
	}
	pos, rot := c.Position(), c.Rotation()
	s.teleport(pos[0], pos[1], pos[2], float32(rot.Yaw()), float32(rot.Pitch()))
	s.loader.ChangeWorld(tx, w)
	s.sendCentre(pos)
}
