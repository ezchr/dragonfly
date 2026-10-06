# go-mcjava

A Go library for the Minecraft Java Edition protocol, currently 26.3 (protocol 777).

It is written for speed and to sit inside a server: framing does not allocate, buffers are
bounds-checked, and the encoders are checked byte for byte against vanilla.

| Package | What it does |
|---|---|
| `wire` | Data types, packet framing, compression and encryption |
| `v777` | Packet ids, registries and the configuration packets for 26.3, generated from Mojang's data reports |
| `server` | Handshake, status, offline and online (Microsoft) login, and configuration, with per-IP and auth limits |
| `chunk` | `level_chunk_with_light` encoding and decoding |
| `item` | Item stacks (slots) with data component patches |
| `text` | Network NBT text components and legacy colour codes |
| `cmd/javagen` | Generates `v777` from Mojang's server.jar `--reports` |
| `cmd/jbot`, `cmd/jflat`, `cmd/chunkdiff` | A scripted test client, a flat test server, and a chunk comparison tool |

The AES/CFB8 cipher in `internal/cfb8` comes from
[Tnze/go-mc](https://github.com/Tnze/go-mc) (MIT).
