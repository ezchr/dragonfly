package session

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// PlayerSkinHandler handles the PlayerSkin packet.
type PlayerSkinHandler struct{}

// Handle ...
func (PlayerSkinHandler) Handle(p packet.Packet, s *Session, tx *world.Tx, c Controllable) error {
	pk := p.(*packet.PlayerSkin)

	playerSkin, err := protocolToSkin(pk.Skin)
	if err != nil {
		return fmt.Errorf("error decoding skin: %w", err)
	}

	c.SetSkin(playerSkin)

	// Changing to a persona skin mid-session goes through here rather than
	// through the login path, so resolution has to be kicked off from here too.
	ResolvePersona(tx.World(), c.H(), c.XUID(), playerSkin, s.conf.Log)

	return nil
}
