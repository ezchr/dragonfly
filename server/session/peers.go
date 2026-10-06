package session

import (
	"slices"

	"github.com/df-mc/dragonfly/server/internal/sliceutil"
	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// Peer is a player connected through something other than a Bedrock session, such as a Java
// Edition session. Bedrock clients list peers in the player list and show them as real players
// (with their skin) rather than as NPCs.
type Peer struct {
	Handle *world.EntityHandle
	Name   string
	XUID   string
	Skin   skin.Skin
}

// AddPeer shows p to every Bedrock session, and to sessions that join later, until RemovePeer.
func AddPeer(p *Peer) {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	for _, s := range sessions.s {
		sessions.sendPeerTo(p, s)
	}
	sessions.peers = append(sessions.peers, p)
}

// RemovePeer stops listing p.
func RemovePeer(p *Peer) {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	if !slices.Contains(sessions.peers, p) {
		return
	}
	for _, s := range sessions.s {
		s.entityMutex.Lock()
		delete(s.entities, s.entityRuntimeIDs[p.Handle])
		delete(s.entityRuntimeIDs, p.Handle)
		s.entityMutex.Unlock()
		s.writePacket(&packet.PlayerList{Entries: []protocol.PlayerListEntry{{
			ActionType: protocol.PlayerListActionRemove,
			UUID:       p.Handle.UUID(),
		}}})
	}
	sessions.peers = sliceutil.DeleteVal(sessions.peers, p)
}

// sendPeerTo lists p for the session to. sessions.mu must be held.
func (l *sessionList) sendPeerTo(p *Peer, to *Session) {
	to.entityMutex.Lock()
	to.currentEntityRuntimeID += 1
	runtimeID := to.currentEntityRuntimeID
	to.entityRuntimeIDs[p.Handle] = runtimeID
	to.entities[runtimeID] = p.Handle
	to.entityMutex.Unlock()

	to.writePacket(&packet.PlayerList{Entries: []protocol.PlayerListEntry{{
		ActionType:     protocol.PlayerListActionAdd,
		UUID:           p.Handle.UUID(),
		EntityUniqueID: int64(runtimeID),
		Username:       p.Name,
		XUID:           p.XUID,
		BuildPlatform:  int32(protocol.DeviceUnknown),
		Skin:           skinToProtocol(p.Skin),
	}}})
}

// isPeer reports whether the player with this UUID is a listed peer.
func (l *sessionList) isPeer(id uuid.UUID) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.ContainsFunc(l.peers, func(p *Peer) bool { return p.Handle.UUID() == id })
}
