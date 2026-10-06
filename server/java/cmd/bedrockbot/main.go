// bedrockbot is a Bedrock test client for the native Java work: it joins a Dragonfly server's
// Bedrock port (offline, the server must have auth disabled) and reports the players it is shown,
// so we can check Bedrock players see Java players as real players with a skin.
//
//	go run ./cmd/bedrockbot -addr 127.0.0.1:19160 -secs 20
package main

import (
	"flag"
	"log"
	"time"

	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:19160", "Bedrock address")
	name := flag.String("name", "BedrockBot", "name")
	secs := flag.Int("secs", 20, "seconds to stay")
	flag.Parse()
	conn, err := minecraft.Dialer{IdentityData: login.IdentityData{DisplayName: *name}}.DialTimeout("raknet", *addr, 15*time.Second)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	if err := conn.DoSpawnTimeout(15 * time.Second); err != nil {
		log.Fatal(err)
	}
	log.Printf("%s: spawned", *name)
	deadline := time.Now().Add(time.Duration(*secs) * time.Second)
	for time.Now().Before(deadline) {
		conn.SetReadDeadline(deadline)
		pk, err := conn.ReadPacket()
		if err != nil {
			break
		}
		switch p := pk.(type) {
		case *packet.UpdateAbilities:
			for _, l := range p.AbilityData.Layers {
				log.Printf("BedrockBot: abilities layer %d values %#x mayfly=%v flying=%v", l.Type, l.Values, l.Values&protocol.AbilityMayFly != 0, l.Values&protocol.AbilityFlying != 0)
			}
		case *packet.SetPlayerGameType:
			log.Printf("BedrockBot: game type %d", p.GameType)
		case *packet.PlayerList:
			for _, e := range p.Entries {
				if e.ActionType == protocol.PlayerListActionAdd {
					log.Printf("%s: player list ADD %q xuid=%q skin %dx%d (%d bytes) geometry=%q arm=%q", *name, e.Username, e.XUID,
						e.Skin.SkinImageWidth, e.Skin.SkinImageHeight, len(e.Skin.SkinData), e.Skin.SkinResourcePatch, e.Skin.ArmSize)
				} else {
					log.Printf("%s: player list REMOVE %s", *name, e.UUID)
				}
			}
		case *packet.AddPlayer:
			log.Printf("%s: AddPlayer %q runtime id %d at %v", *name, p.Username, p.EntityRuntimeID, p.Position)
		case *packet.AddActor:
			log.Printf("%s: AddActor %s", *name, p.EntityType)
		case *packet.RemoveActor:
			log.Printf("%s: RemoveActor %d", *name, p.EntityUniqueID)
		}
	}
	log.Printf("%s: done", *name)
}
