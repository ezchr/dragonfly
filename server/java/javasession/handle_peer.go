package javasession

import (
	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
)

// SetHandle is called by Server.AddPlayer before the player is in a world: the player is listed
// for Bedrock clients (as a peer) before any of them is shown its entity.
func (s *Session) SetHandle(h *world.EntityHandle, sk skin.Skin) {
	s.ent = h
	// No XUID in the Bedrock player list: a Java player's (ViaBedrock-style) XUID is not a real
	// Xbox account, and Bedrock clients crashed opening its profile from the player list.
	// Geyser lists Java players without one too.
	s.peer = &session.Peer{Handle: h, Name: s.jp.Profile.Name, Skin: sk}
	session.AddPeer(s.peer)
}
