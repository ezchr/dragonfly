# go-mcjava

The Minecraft Java Edition protocol in Go, written for **crossplay**: it is the protocol half of the
[Java crossplay in ezchr/dragonfly](https://github.com/ezchr/dragonfly/tree/crossplay/server/java), which lets Java Edition players join a
[Dragonfly](https://github.com/df-mc/dragonfly) (Bedrock) server natively, with no ViaProxy or
Geyser in between.

> **Crossplay note.** The packages here have no Dragonfly dependency and can be used on their own.
> The Dragonfly integration that uses them **does not work with normal Dragonfly**: it lives on
> the `crossplay` branch of [ezchr/dragonfly](https://github.com/ezchr/dragonfly/tree/crossplay/server/java), next to the hooks it needs.

Java **26.3** (protocol 777) and **26.2** (protocol 776) clients are supported. A server is written
against 26.3; the `version` package converts its ids for 26.2 clients.

It is written for speed and to sit inside a server: framing does not allocate, buffers are
bounds-checked, and the encoders are checked byte for byte against vanilla.

| Package | What it does |
|---|---|
| `wire` | Data types, packet framing, compression and encryption |
| `v777`, `v776` | Packet ids, registries and configuration packets per version, generated from Mojang's data reports and a vanilla join |
| `version` | Maps the ids a 26.3 server writes to an older client's (packets, block states, registries) |
| `server` | Handshake, status, offline and online (Microsoft) login, and configuration, with per-IP and auth limits |
| `chunk` | `level_chunk_with_light` encoding and decoding |
| `item` | Item stacks (slots) with data component patches, per version |
| `text` | Network NBT text components and legacy colour codes |
| `cmd/javagen`, `cmd/remapgen` | Generate the `vNNN` packages and the remap tables from Mojang's reports |
| `cmd/jbot`, `cmd/jflat`, `cmd/chunkdiff` | A scripted test client, a flat test server, and a chunk comparison tool |

## Licence

MIT. The AES/CFB8 cipher in `internal/cfb8` comes from [Tnze/go-mc](https://github.com/Tnze/go-mc)
(MIT). Data in `v777` and `v776` is generated from Mojang's server reports.
