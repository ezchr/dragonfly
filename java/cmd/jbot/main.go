// jbot is a scripted Java Edition 26.3 test client: it logs in (offline mode), finishes
// configuration, then walks a square at walking speed and reports what the server sends back,
// in particular position corrections (teleports) and disconnects.
//
//	go run ./java/cmd/jbot -addr 127.0.0.1:25620 -name Bot1 -secs 20
package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"net"
	"sync/atomic"
	"time"

	"github.com/ezchr/go-mc/java/server"
	"github.com/ezchr/go-mc/java/v777"
	"github.com/ezchr/go-mc/java/wire"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:25620", "server")
	name := flag.String("name", "Bot", "player name")
	secs := flag.Int("secs", 20, "seconds to stay")
	attack := flag.Bool("attack", false, "stand still and attack the first player seen every 600 ms")
	still := flag.Bool("still", false, "stand still")
	flag.Parse()
	if err := run(*addr, *name, time.Duration(*secs)*time.Second, *attack, *still); err != nil {
		log.Fatal(err)
	}
}

func run(addr, name string, stay time.Duration, attack, still bool) error {
	nc, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}
	c := wire.NewConn(nc)
	defer c.Close()
	host, port, _ := net.SplitHostPort(addr)
	var w wire.Writer
	w.VarInt(server.ProtocolVersion)
	w.String(host)
	var p int
	fmt.Sscan(port, &p)
	w.Uint16(uint16(p))
	w.VarInt(2) // login
	c.WritePacket(v777.ServerboundHandshakeIntention, w.B)
	w.Reset()
	w.String(name)
	w.UUID(server.OfflineUUID(name))
	if err := c.Send(v777.ServerboundLoginHello, w.B); err != nil {
		return err
	}
	// Login.
	for done := false; !done; {
		id, body, err := c.ReadPacket()
		if err != nil {
			return fmt.Errorf("login: %w", err)
		}
		switch id {
		case v777.ClientboundLoginLoginCompression:
			c.SetThreshold(int(wire.NewReader(body).VarInt()))
		case v777.ClientboundLoginLoginFinished:
			c.Send(v777.ServerboundLoginLoginAcknowledged, nil)
			done = true
		case v777.ClientboundLoginLoginDisconnect:
			return fmt.Errorf("kicked at login: %s", wire.NewReader(body).String(262144))
		default:
			return fmt.Errorf("login: unexpected packet %#x", id)
		}
	}
	// Configuration.
	w.Reset()
	w.String("en_us")
	w.Int8(8)
	w.VarInt(0)
	w.Bool(true)
	w.Byte(0x7f)
	w.VarInt(1)
	w.Bool(false)
	w.Bool(true)
	w.VarInt(0)
	c.Send(v777.ServerboundConfigurationClientInformation, w.B)
	for done := false; !done; {
		id, body, err := c.ReadPacket()
		if err != nil {
			return fmt.Errorf("configuration: %w", err)
		}
		switch id {
		case v777.ClientboundConfigurationSelectKnownPacks:
			w.Reset()
			w.VarInt(1)
			w.String("minecraft")
			w.String("core")
			w.String(server.GameVersion)
			c.Send(v777.ServerboundConfigurationSelectKnownPacks, w.B)
		case v777.ClientboundConfigurationKeepAlive:
			c.Send(v777.ServerboundConfigurationKeepAlive, body)
		case v777.ClientboundConfigurationPing:
			c.Send(v777.ServerboundConfigurationPong, body)
		case v777.ClientboundConfigurationFinishConfiguration:
			c.Send(v777.ServerboundConfigurationFinishConfiguration, nil)
			done = true
		case v777.ClientboundConfigurationDisconnect:
			return fmt.Errorf("kicked in configuration")
		}
	}
	log.Printf("%s: in play", name)

	// Play: read in the background, walk in the foreground.
	pos := make(chan [3]float64, 16)
	var target atomic.Int32 // Java entity id of the first player seen
	errc := make(chan error, 1)
	stats := map[string]int{}
	var tpCount, chunkCount int
	go func() {
		var rw wire.Writer
		for {
			id, body, err := c.ReadPacket()
			if err != nil {
				errc <- err
				return
			}
			r := wire.NewReader(body)
			switch id {
			case v777.ClientboundPlayPlayerPosition:
				tp := r.VarInt()
				x, y, z := r.Float64(), r.Float64(), r.Float64()
				rw.Reset()
				rw.VarInt(tp)
				c.Send(v777.ServerboundPlayAcceptTeleportation, rw.B)
				tpCount++
				pos <- [3]float64{x, y, z}
			case v777.ClientboundPlayKeepAlive:
				c.Send(v777.ServerboundPlayKeepAlive, body)
			case v777.ClientboundPlayLevelChunkWithLight:
				chunkCount++
			case v777.ClientboundPlayChunkBatchFinished:
				rw.Reset()
				rw.Float32(20)
				c.Send(v777.ServerboundPlayChunkBatchReceived, rw.B)
			case v777.ClientboundPlayDisconnect:
				errc <- fmt.Errorf("kicked")
				return
			case v777.ClientboundPlayAddEntity:
				eid := r.VarInt()
				r.UUID()
				if r.VarInt() == 159 { // player
					target.CompareAndSwap(0, eid)
					log.Printf("%s: sees player entity %d", name, eid)
				}
			case v777.ClientboundPlaySetHealth:
				log.Printf("%s: health %.1f food %d", name, r.Float32(), r.VarInt())
			case v777.ClientboundPlaySetEntityMotion:
				if eid := r.VarInt(); eid == 1 {
					x, y, z := r.LpVec3()
					log.Printf("%s: knockback %.3f %.3f %.3f", name, x, y, z)
				}
			case v777.ClientboundPlayRespawn:
				log.Printf("%s: respawned", name)
			default:
				stats[fmt.Sprintf("%#x", id)]++
			}
		}
	}()
	var cur [3]float64
	select {
	case cur = <-pos:
	case err := <-errc:
		return err
	case <-time.After(10 * time.Second):
		return fmt.Errorf("no spawn position")
	}
	start := time.Now()
	t := time.NewTicker(50 * time.Millisecond)
	defer t.Stop()
	const speed = 4.317 / 20 // blocks per tick, walking
	dirs := [][2]float64{{1, 0}, {0, 1}, {-1, 0}, {0, -1}}
	step := 0
	for time.Since(start) < stay {
		select {
		case err := <-errc:
			return fmt.Errorf("after %v: %w", time.Since(start).Round(time.Millisecond), err)
		case p := <-pos:
			log.Printf("%s: corrected to %.2f %.2f %.2f", name, p[0], p[1], p[2])
			cur = p
		case <-t.C:
			if attack || still {
				step++
				if attack && step%12 == 0 && target.Load() != 0 {
					w.Reset()
					w.VarInt(target.Load())
					c.Send(v777.ServerboundPlayAttack, w.B)
				}
				c.Send(v777.ServerboundPlayClientTickEnd, nil)
				continue
			}
			d := dirs[(step/40)%4] // 2 s per side
			step++
			cur[0] += d[0] * speed
			cur[2] += d[1] * speed
			yaw := float32(math.Atan2(-d[0], d[1]) * 180 / math.Pi)
			w.Reset()
			w.Float64(cur[0])
			w.Float64(cur[1])
			w.Float64(cur[2])
			w.Float32(yaw)
			w.Float32(0)
			w.Byte(1) // on ground
			c.Send(v777.ServerboundPlayMovePlayerPosRot, w.B)
			c.Send(v777.ServerboundPlayClientTickEnd, nil)
		}
	}
	log.Printf("%s: done: %d teleports (1 = just the spawn), %d chunks, end %.2f %.2f %.2f, other packets %v",
		name, tpCount, chunkCount, cur[0], cur[1], cur[2], stats)
	return nil
}
