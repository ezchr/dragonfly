// jflat is a test server for the java packages: a real Java client joins, gets a superflat world
// and can walk around. Chunks are a captured vanilla chunk body with its coordinates rewritten.
//
//	go run ./java/cmd/jflat -addr 127.0.0.1:25610 -chunk capture/.../level_chunk_with_light.bin
package main

import (
	"flag"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/ezchr/go-mcjava/server"
	"github.com/ezchr/go-mcjava/v777"
	"github.com/ezchr/go-mcjava/wire"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:25610", "listen address")
	chunkFile := flag.String("chunk", "", "a captured level_chunk_with_light body to repeat")
	radius := flag.Int("radius", 4, "chunk radius")
	flag.Parse()
	chunk, err := os.ReadFile(*chunkFile)
	if err != nil {
		log.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ln, err := server.Listen(*addr, server.Config{
		CompressionThreshold: 256,
		Brand:                "zid-jflat",
		Log:                  logger,
		Status:               func() server.Status { return server.Status{MOTD: "jflat", MaxPlayers: 10} },
	})
	if err != nil {
		log.Fatal(err)
	}
	logger.Info("listening", "addr", ln.Addr())
	for {
		p, err := ln.Accept()
		if err != nil {
			log.Fatal(err)
		}
		logger.Info("player configured", "name", p.Profile.Name, "locale", p.Info.Locale, "view", p.Info.ViewDistance)
		go play(p, chunk, *radius, logger)
	}
}

func play(p *server.Player, chunk []byte, radius int, logger *slog.Logger) {
	c := p.Conn
	defer c.Close()
	var w wire.Writer
	send := func(id int32) {
		if err := c.WritePacket(id, w.B); err != nil {
			logger.Error("write", "err", err)
		}
		w.Reset()
	}
	// Login (play): the same fields vanilla sends, see capture 00047.
	w.Int32(1)    // entity id
	w.Bool(false) // hardcore
	w.VarInt(1)   // dimensions
	w.String("minecraft:overworld")
	w.VarInt(10)            // max players
	w.VarInt(int32(radius)) // view distance
	w.VarInt(int32(radius)) // simulation distance
	w.Bool(false)           // reduced debug info
	w.Bool(true)            // death screen
	w.Bool(false)           // limited crafting
	w.VarInt(v777.RegistryID("minecraft:dimension_type", "minecraft:overworld"))
	w.String("minecraft:overworld")
	w.Int64(0)    // hashed seed
	w.VarInt(1)   // creative (GameType.STREAM_CODEC is a VarInt id)
	w.VarInt(0)   // previous game mode: optional VarInt, 0 = none, else id+1
	w.Bool(false) // debug world
	w.Bool(true)  // flat
	w.Bool(false) // no death location
	w.VarInt(0)   // portal cooldown
	w.VarInt(-63) // sea level
	w.Bool(false) // online mode
	w.Bool(false) // enforces secure chat
	send(v777.ClientboundPlayLogin)

	w.String("minecraft:overworld")
	w.Position(0, -60, 0)
	w.Float32(0)
	w.Float32(0)
	send(v777.ClientboundPlaySetDefaultSpawnPosition)

	w.VarInt(1) // teleport id
	w.Float64(0.5)
	w.Float64(-60)
	w.Float64(0.5)
	w.Float64(0)
	w.Float64(0)
	w.Float64(0)
	w.Float32(0)
	w.Float32(0)
	w.Int32(0) // absolute
	send(v777.ClientboundPlayPlayerPosition)

	w.Byte(13) // start waiting for level chunks
	w.Float32(0)
	send(v777.ClientboundPlayGameEvent)

	w.VarInt(0)
	w.VarInt(0)
	send(v777.ClientboundPlaySetChunkCacheCenter)

	send(v777.ClientboundPlayChunkBatchStart)
	n := 0
	body := append([]byte(nil), chunk...)
	for x := -radius; x <= radius; x++ {
		for z := -radius; z <= radius; z++ {
			var pos wire.Writer
			pos.Int32(int32(x))
			pos.Int32(int32(z))
			copy(body, pos.B)
			if err := c.WritePacket(v777.ClientboundPlayLevelChunkWithLight, body); err != nil {
				logger.Error("chunk", "err", err)
				return
			}
			n++
		}
	}
	w.VarInt(int32(n))
	send(v777.ClientboundPlayChunkBatchFinished)
	if err := c.Flush(); err != nil {
		logger.Error("flush", "err", err)
		return
	}
	logger.Info("sent world", "chunks", n)

	go func() {
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		var kw wire.Writer
		for range t.C {
			kw.Reset()
			kw.Int64(time.Now().UnixMilli())
			if err := c.Send(v777.ClientboundPlayKeepAlive, kw.B); err != nil {
				return
			}
		}
	}()
	counts := map[int32]int{}
	last := time.Now()
	for {
		id, body, err := c.ReadPacket()
		if err != nil {
			logger.Info("player left", "name", p.Profile.Name, "err", err, "packets", counts)
			return
		}
		counts[id]++
		r := wire.NewReader(body)
		switch id {
		case v777.ServerboundPlayMovePlayerPos, v777.ServerboundPlayMovePlayerPosRot:
			x, y, z := r.Float64(), r.Float64(), r.Float64()
			if time.Since(last) > 2*time.Second {
				logger.Info("position", "x", x, "y", y, "z", z)
				last = time.Now()
			}
		case v777.ServerboundPlayAcceptTeleportation:
			logger.Info("teleport accepted", "id", r.VarInt())
		case v777.ServerboundPlayChunkBatchReceived:
			logger.Info("chunk batch received", "chunks per tick", r.Float32())
		case v777.ServerboundPlayPlayerLoaded:
			logger.Info("player loaded")
		case v777.ServerboundPlayChat:
			logger.Info("chat", "msg", r.String(256))
		}
	}
}
