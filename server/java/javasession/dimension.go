package javasession

import (
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
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
	// Every world change gets the client a new level, even into the same dimension: forgetting the
	// chunks alone left a client moved between two overworlds waiting for chunks that never came
	// (a void), there and back.
	if dim := dimensionKey(w.Dimension()); true {
		s.dim = dim
		s.forgetAllChunks(false)
		// A respawn into the same dimension keeps the client's level and every entity in it (only
		// a new dimension starts a new level): they are removed first, or the old world's entities
		// stay in view in the new one, and one that leaves later is never removed (it is no longer
		// known here).
		s.removeAllEntities()
		s.writeRespawn(dim, c, 3)
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

// removeAllEntities removes every entity the client was shown (world entities and the extra parts
// of their models) and forgets them.
func (s *Session) removeAllEntities() {
	s.entMu.Lock()
	ids := make([]int32, 0, len(s.entityIDs))
	for _, id := range s.entityIDs {
		ids = append(ids, id)
	}
	for _, parts := range s.modelParts {
		ids = append(ids, parts...)
	}
	for _, d := range s.nameDisplays {
		ids = append(ids, d.id)
	}
	clear(s.entityIDs)
	clear(s.tracks)
	clear(s.modelParts)
	clear(s.nameDisplays)
	s.entMu.Unlock()
	if len(ids) == 0 {
		return
	}
	w := s.packet()
	w.VarInt(int32(len(ids)))
	for _, id := range ids {
		w.VarInt(id)
	}
	s.queue(v777.ClientboundPlayRemoveEntities, w)
}
