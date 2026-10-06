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

	"github.com/ezchr/go-mcjava/server"
	v777 "github.com/ezchr/go-mcjava/v777"
	"github.com/ezchr/go-mcjava/wire"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:25620", "server")
	name := flag.String("name", "Bot", "player name")
	secs := flag.Int("secs", 20, "seconds to stay")
	attack := flag.Bool("attack", false, "stand still and attack the first player seen every 600 ms")
	still := flag.Bool("still", false, "stand still")
	brk := flag.Bool("break", false, "stand still and break the block below once (creative: instant)")
	flag.Parse()
	breakBelow = *brk
	if err := run(*addr, *name, time.Duration(*secs)*time.Second, *attack, *still || *brk); err != nil {
		log.Fatal(err)
	}
}

var breakBelow bool

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
	var target atomic.Int32            // Java entity id of the first player seen
	tracked := map[int32]*[3]float64{} // other entities' positions as the client would compute them
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
				errc <- fmt.Errorf("kicked: %q", body)
				return
			case v777.ClientboundPlayGameEvent:
				if ev := r.Byte(); ev == 13 {
					log.Printf("%s: level chunks load start", name)
				}
			case v777.ClientboundPlayAddEntity:
				eid := r.VarInt()
				r.UUID()
				typ := r.VarInt()
				if typ == 159 { // player
					target.CompareAndSwap(0, eid)
					log.Printf("%s: sees player entity %d", name, eid)
				}
				tracked[eid] = &[3]float64{r.Float64(), r.Float64(), r.Float64()}
			case v777.ClientboundPlayEntityPositionSync:
				eid := r.VarInt()
				r.VarInt() // path type: linear
				tracked[eid] = &[3]float64{r.Float64(), r.Float64(), r.Float64()}
			case v777.ClientboundPlayMoveEntityPos, v777.ClientboundPlayMoveEntityPosRot:
				// Apply the delta like the client's VecDeltaCodec.
				eid := r.VarInt()
				r.VarInt() // properties
				if p := tracked[eid]; p != nil {
					for i := range p {
						if d := int64(r.Int16()); d != 0 {
							p[i] = float64(int64(math.Floor(p[i]*4096+0.5))+d) / 4096
						}
					}
				}
			case v777.ClientboundPlaySetHealth:
				health := r.Float32()
				log.Printf("%s: health %.1f food %d", name, health, r.VarInt())
				if health <= 0 {
					rw.Reset()
					rw.VarInt(0) // perform respawn
					c.Send(v777.ServerboundPlayClientCommand, rw.B)
					log.Printf("%s: died, asked to respawn", name)
				}
			case v777.ClientboundPlayRemoveEntities:
				n := r.VarInt()
				for i := int32(0); i < n; i++ {
					log.Printf("%s: removed entity %d", name, r.VarInt())
				}
			case v777.ClientboundPlayPlayerInfoRemove:
				log.Printf("%s: player info remove %d", name, r.VarInt())
			case v777.ClientboundPlayForgetLevelChunk:
				stats["forget"]++
			case v777.ClientboundPlayPlayerInfoUpdate:
				actions := r.Byte()
				log.Printf("%s: player info update actions %#x entries %d", name, actions, r.VarInt())
			case v777.ClientboundPlaySetEntityMotion:
				if eid := r.VarInt(); eid == 1 {
					x, y, z := r.LpVec3()
					log.Printf("%s: knockback %.3f %.3f %.3f", name, x, y, z)
				}
			case v777.ClientboundPlayRespawn:
				log.Printf("%s: respawned", name)
			case v777.ClientboundPlayBlockUpdate:
				x, y, z := r.Position()
				log.Printf("%s: block update %d %d %d -> state %d", name, x, y, z, r.VarInt())
			case v777.ClientboundPlayBlockChangedAck:
				log.Printf("%s: block ack sequence %d", name, r.VarInt())
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
				if breakBelow && step == 20 {
					w.Reset()
					w.VarInt(0) // START_DESTROY_BLOCK
					w.Position(int(math.Floor(cur[0])), int(math.Floor(cur[1]))-1, int(math.Floor(cur[2])))
					w.Byte(1) // up face
					w.VarInt(7)
					c.Send(v777.ServerboundPlayPlayerAction, w.B)
					log.Printf("%s: start breaking below (sequence 7)", name)
				}
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
	log.Printf("%s: done: %d teleports (1 = just the spawn), %d chunks, end %.4f %.4f %.4f, other packets %v",
		name, tpCount, chunkCount, cur[0], cur[1], cur[2], stats)
	if t := target.Load(); t != 0 {
		if p := tracked[t]; p != nil {
			log.Printf("%s: tracked player %d at %.4f %.4f %.4f", name, t, p[0], p[1], p[2])
		}
	}
	return nil
}
