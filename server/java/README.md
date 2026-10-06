# dfjava

**Java Edition crossplay for Dragonfly.** Java Edition players join a
[Dragonfly](https://github.com/df-mc/dragonfly) (Bedrock) server directly: the Java protocol is
spoken in Go inside the server, with no ViaProxy, ViaBedrock or Geyser in between. Java and
Bedrock players share one world and see each other.

> **Only works with the ezchr/dragonfly fork.** dfjava does **not** work with normal Dragonfly
> (df-mc/dragonfly). Normal Dragonfly only accepts players that come with a Bedrock session; dfjava
> needs the hooks on the
> [`java-native` branch of ezchr/dragonfly](https://github.com/ezchr/dragonfly/tree/java-native)
> (a player session interface, `Server.LoadPlayer`/`AddPlayer`, and listing Java players in the
> Bedrock player list). Build your server against that branch.

The Java protocol itself lives in [go-mcjava](https://github.com/ezchr/go-mcjava).

## Versions

Java **26.3** and **26.2** clients. Bedrock clients are whatever the fork supports (1.26.50).

## Setup

In your server's `go.mod`, use the fork in place of Dragonfly (`go mod tidy` turns the branch into a
version), then add dfjava:

```
replace github.com/df-mc/dragonfly => github.com/ezchr/dragonfly java-native
```

```
go get github.com/ezchr/dfjava@latest
```

Start the Java listener next to your Bedrock one:

```go
import (
	"log/slog"

	"github.com/ezchr/dfjava/javasession"
	jserver "github.com/ezchr/go-mcjava/server"
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

`cmd/dfjtest` is a complete test server.

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

MIT. dfjava's Bedrock to Java tables are generated from
[GeyserMC/mappings](https://github.com/GeyserMC/mappings) (MIT) and Mojang's data reports. The
player model in `javasession/skins/geo.json` is from [Geyser](https://github.com/GeyserMC/Geyser)
(MIT). `javasession/skins/steve.png` is Mojang's default skin.
