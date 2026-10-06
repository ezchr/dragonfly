package javasession

import (
	"sync"

	jserver "github.com/ezchr/go-mcjava/server"
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
