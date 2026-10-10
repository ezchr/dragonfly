package session

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// RequestChunkRadiusHandler handles the RequestChunkRadius packet.
type RequestChunkRadiusHandler struct {
	// seen: the session sent a request before. exempt: one of this machine's own services, not
	// checked for bedrocktool (toolcheck.go).
	seen, exempt bool
}

// Handle ...
func (h *RequestChunkRadiusHandler) Handle(p packet.Packet, s *Session, tx *world.Tx, _ Controllable) error {
	pk := p.(*packet.RequestChunkRadius)
	if h.toolCheck(pk, s) {
		return nil
	}

	if pk.ChunkRadius > s.maxChunkRadius {
		pk.ChunkRadius = s.maxChunkRadius
	}
	s.chunkRadius = pk.ChunkRadius

	s.chunkLoader.ChangeRadius(tx, int(pk.ChunkRadius))

	s.writePacket(&packet.ChunkRadiusUpdated{ChunkRadius: s.chunkRadius})
	return nil
}
