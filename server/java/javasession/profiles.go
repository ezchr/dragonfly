package javasession

import (
	"sync"

	jserver "github.com/df-mc/dragonfly/server/java/protocol/server"
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/google/uuid"
)

// profiles holds the profile properties (signed skin textures) of the Java players online, so
// other Java clients can be shown their real skins. Bedrock players have none: Java clients pick
// a default skin from their UUID until Bedrock skins get signed textures.
var profiles sync.Map // uuid.UUID -> []jserver.Property

// javaPlayers holds the UUIDs of the Java players online: everyone else is a Bedrock player,
// whose skin comes from GeyserMC's skin database (geyserskin.go).
var javaPlayers sync.Map // uuid.UUID -> struct{}

func registerProfile(id uuid.UUID, props []jserver.Property) {
	javaPlayers.Store(id, struct{}{})
	if len(props) > 0 {
		profiles.Store(id, props)
	}
}

func forgetProfile(id uuid.UUID) {
	profiles.Delete(id)
	javaPlayers.Delete(id)
	skinParts.Delete(id)
}

// skinParts holds the skin layers each Java player shows (the Skin Customization settings: cape,
// jacket, sleeves, trouser legs, hat). Java clients draw a player's outer skin layer only for the
// parts the server says are on; Bedrock players have no such setting and show them all.
var skinParts sync.Map // uuid.UUID -> byte

// allSkinParts is every layer shown.
const allSkinParts = 0x7f

func registerSkinParts(id uuid.UUID, parts byte) { skinParts.Store(id, parts) }

func skinPartsOf(id uuid.UUID) byte {
	if v, ok := skinParts.Load(id); ok {
		return v.(byte)
	}
	return allSkinParts
}

// Avatar.DATA_PLAYER_MODE_CUSTOMISATION: the shown skin layers, a BYTE.
const dataPlayerSkinParts = 16

// sendSkinParts tells the client which skin layers a player shows.
func (s *Session) sendSkinParts(entityID int32, parts byte) {
	w := s.packet()
	w.VarInt(entityID)
	w.Byte(dataPlayerSkinParts)
	w.VarInt(dataTypeByte)
	w.Byte(parts)
	w.Byte(0xff)
	s.queue(v777.ClientboundPlaySetEntityData, w)
}

func isJavaPlayer(id uuid.UUID) bool {
	_, ok := javaPlayers.Load(id)
	return ok
}

func profileProperties(id uuid.UUID) []jserver.Property {
	if v, ok := profiles.Load(id); ok {
		return v.([]jserver.Property)
	}
	return nil
}
