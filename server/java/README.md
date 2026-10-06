# Java Edition crossplay (server/java)

**Java Edition crossplay for Dragonfly**, on the `crossplay` branch of this fork. Java Edition players join a
[Dragonfly](https://github.com/df-mc/dragonfly) (Bedrock) server directly: the Java protocol is
spoken in Go inside the server, with no ViaProxy, ViaBedrock or Geyser in between. Java and
Bedrock players share one world and see each other.

> **Not in normal Dragonfly.** This exists only on the `crossplay` branch of
> [ezchr/dragonfly](https://github.com/ezchr/dragonfly/tree/crossplay). Normal Dragonfly
> (df-mc/dragonfly) only accepts players that come with a Bedrock session; this branch adds the hooks
> a Java session needs (a player session interface, `Server.LoadPlayer`/`AddPlayer`, and listing
> Java players in the Bedrock player list) and the Java session itself, in this folder.

Everything is in this branch: the hooks in Dragonfly's own packages, the Java session in
`server/java/javasession`, and the Java protocol in `server/java/protocol`. Switching your server to
the branch is all it takes. The session and the protocol are also mirrored to their own repos,
[ezchr/dfjava](https://github.com/ezchr/dfjava) and [ezchr/go-mcjava](https://github.com/ezchr/go-mcjava)
(the protocol works on its own there, for proxies or bots); this branch is where they are developed.

## Versions

Java **26.3** and **26.2** clients. Bedrock clients are whatever the fork supports (1.26.50).

[UPDATING.md](UPDATING.md) is the runbook for a new Java version, a new Bedrock version or a newer
Dragonfly.

## Setup

Make your server use this branch instead of normal Dragonfly: one line in your `go.mod`, then
`go mod tidy` (it turns the branch name into a version). Nothing else about your server changes.

```
replace github.com/df-mc/dragonfly => github.com/ezchr/dragonfly crossplay
```

Start the Java listener next to your Bedrock one:

```go
import (
	"log/slog"

	"github.com/df-mc/dragonfly/server/java/javasession"
	jserver "github.com/df-mc/dragonfly/server/java/protocol/server"
)

srv := conf.New()
srv.Listen()

l, err := jserver.Listen("0.0.0.0:25565", jserver.Config{
	OnlineMode:           true, // Microsoft accounts only
	CompressionThreshold: 256,
	Status: func() jserver.Status {
		return jserver.Status{MOTD: conf.Name, MaxPlayers: conf.MaxPlayers, Online: srv.PlayerCount()}
	},
})
if err != nil {
	panic(err)
}
go javasession.Run(javasession.Config{
	Server:      srv,
	Listener:    l,
	ChunkRadius: 8,
	JoinMessage: conf.JoinMessage,
	QuitMessage: conf.QuitMessage,
})

for p := range srv.Accept() {
	// Java players arrive here like Bedrock players do.
}
```

`server/java/cmd/dfjtest` is a complete test server.

## What works

- Joining, chunks (byte-identical to vanilla), movement, block breaking and placing, survival and
  creative.
- Combat between the editions, knockback, damage, death and respawn, nether and other worlds.
- Inventories, chests, furnaces, crafting, anvils, enchanting and the other block windows.
- Chat, commands with tab completion, titles, the sidebar, boss bars, name tags, floating text,
  signs, forms (as dialogs).
- Sounds, particles, potion effects, entities and riding.
- Skins:
  - Java players' skins are shown on Bedrock.
  - Bedrock players' skins are shown on Java, from GeyserMC's global skin database (players
    whose skin was uploaded by a Floodgate server).
- A shared tab list, and real ping on both editions.
- Identity: a Java player gets the XUID and UUID ViaBedrock would give them, so players who
  used ViaProxy before keep their data. `Config.Identity` changes this.

Not done: the recipe book, villager trades.

## Licence

MIT, like Dragonfly. The Bedrock to Java tables were first generated from
[GeyserMC/mappings](https://github.com/GeyserMC/mappings) (MIT) and are kept up to date from
Mojang's data reports and Dragonfly's registries. The
player model in `javasession/skins/geo.json` is from [Geyser](https://github.com/GeyserMC/Geyser)
(MIT). `javasession/skins/steve.png` is Mojang's default skin.
