// Package javasession is a Dragonfly session for Java Edition clients: it implements player.Session
// (and so world.Viewer) by writing Java packets, and turns the client's packets into calls on the
// player. Java players join through Run, which accepts clients from the java/server listener.
package javasession

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	jchunk "github.com/ezchr/go-mcjava/chunk"
	"github.com/ezchr/go-mcjava/server"
	v777 "github.com/ezchr/go-mcjava/v777"
	"github.com/ezchr/go-mcjava/version"
	"github.com/ezchr/go-mcjava/wire"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
)

// Session is one Java Edition player's connection.
type Session struct {
	log *slog.Logger
	jp  *server.Player
	id  uuid.UUID // the player's Dragonfly UUID (see Identity)
	// selfID is the UUID the client logged in with (Mojang's): the client finds its own tab
	// entry, and so its own skin and game mode, under this one, not under id.
	selfID uuid.UUID
	xuid   string
	skin   skin.Skin
	peer   *session.Peer // how Bedrock clients list this player
	conn   *wire.Conn
	// ver is the client's protocol version. The session writes v777 (26.3) ids everywhere and
	// remaps them with ver where they are written; blk is the block table for ver.
	ver *version.Version
	blk *blockInfo

	ent     *world.EntityHandle
	onClose func(*world.Tx, session.Controllable)

	joinMessage chat.Translation
	quitMessage chat.Translation

	tabs        *tabList
	tab         tabState
	cleanupOnce sync.Once
	spawned     atomic.Bool

	// Java entity ids of the entities this client sees (it is selfEntityID itself).
	entMu        sync.Mutex
	entityIDs    map[*world.EntityHandle]int32
	riders       map[*world.EntityHandle][]*world.EntityHandle // vehicle -> riders (riding.go)
	deferred     map[*world.EntityHandle]struct{}              // Bedrock players waiting for their skin (deferForSkin)
	tracks       map[int32]*track
	nextEntityID int32

	chunkRadius int32
	dim         string // the client's current Java dimension
	loader      *world.Loader

	vitalsMu sync.Mutex
	vitals   vitals

	input inputState

	timeMu      sync.Mutex
	time        int
	timeStopped bool
	raining     bool
	thunder     bool
	lastCentre  world.ChunkPos
	centreSent  bool
	closeOnce   sync.Once

	// Chunk sending: the client says how many chunks per tick it can take (in thousandths);
	// one batch waits for its acknowledgement at a time.
	chunkRate     atomic.Int64
	batchInFlight atomic.Bool
	chunks        chunkState
	col           jchunk.Column
	hm            [1][]uint64

	// Teleports: moves from the client are ignored until it accepts the last one.
	teleportID      atomic.Int32
	pendingTeleport atomic.Int32

	latency   atomic.Int64 // round trip in nanoseconds, smoothed like vanilla; 0 until measured
	keepAlive atomic.Int64 // id (queue time in Unix nanoseconds) of the keep-alive we're waiting on, 0 if none
	// keepAliveSent is when the pending keep-alive was written to the socket (Unix nanoseconds):
	// the round trip is timed from there, not from the queue, which waits up to flushDelay.
	keepAliveSent atomic.Int64

	outMu  sync.Mutex
	out    []outPacket
	wake   chan struct{}
	closed chan struct{}
	once   sync.Once

	writers sync.Pool
}

type outPacket struct {
	id int32
	w  *wire.Writer
}

func newSession(jp *server.Player, radius int32, log *slog.Logger) *Session {
	s := &Session{
		log:         log.With("player", jp.Profile.Name, "edition", "java"),
		jp:          jp,
		conn:        jp.Conn,
		ver:         jp.Version,
		chunkRadius: radius,
		wake:        make(chan struct{}, 1),
		closed:      make(chan struct{}),
		entityIDs:   map[*world.EntityHandle]int32{},
		tracks:      map[int32]*track{},
		vitals:      vitals{health: 20, food: 20, saturation: 5},
	}
	s.tab.shown = map[uuid.UUID]tabShown{}
	s.chunks.sent = map[world.ChunkPos]struct{}{}
	if s.ver == nil {
		s.ver = version.Newest
	}
	s.blk = blocksFor(s.ver)
	s.chunkRate.Store(9000) // vanilla's starting rate: 9 chunks per tick
	s.writers.New = func() any { return &wire.Writer{B: make([]byte, 0, 256)} }
	go s.writeLoop()
	return s
}

// packet returns a writer for a packet body; pass it to queue.
func (s *Session) packet() *wire.Writer {
	w := s.writers.Get().(*wire.Writer)
	w.Reset()
	return w
}

// queue sends a packet built with s.packet(). It never blocks on the network: viewer methods run
// on the world goroutine.
func (s *Session) queue(id int32, w *wire.Writer) {
	select {
	case <-s.closed:
		return // nothing will send it
	default:
	}
	s.outMu.Lock()
	s.out = append(s.out, outPacket{id, w})
	n := len(s.out)
	s.outMu.Unlock()
	if n > maxQueued {
		// The client stopped reading: drop it before its backlog eats the memory.
		s.log.Info("send queue full", "packets", n)
		s.CloseConnection()
		return
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// flushDelay is how long the writer waits after the first queued packet before writing: the world
// queues packets in bursts (a tick's movement of every visible entity), and one write per burst
// instead of one per packet saves most of the CPU (it was 70% syscalls at 30 players).
const flushDelay = 2 * time.Millisecond

// maxQueued is how many packets may wait for the writer before the client is dropped: a client
// that stops reading would otherwise make the session hold everything the world sends it.
const maxQueued = 1 << 16

// writeLoop writes queued packets. Once the connection is closing it writes what is still queued
// (a disconnect reason) and closes the socket; CloseConnection limits how long that may take.
func (s *Session) writeLoop() {
	defer s.conn.Close()
	var batch []outPacket
	for {
		closing := false
		select {
		case <-s.wake:
			time.Sleep(flushDelay)
		case <-s.closed:
			closing = true
		}
		s.outMu.Lock()
		batch, s.out = s.out, batch[:0]
		s.outMu.Unlock()
		for _, p := range batch {
			var err error
			if id := s.ver.ClientboundPlay(p.id); id >= 0 { // -1: the client's version has no such packet
				err = s.conn.WritePacket(id, p.w.B)
				if p.id == v777.ClientboundPlayKeepAlive {
					// Written now; flushed with the batch right after.
					s.keepAliveSent.Store(time.Now().UnixNano())
				}
			}
			if cap(p.w.B) <= 1<<16 { // don't keep huge chunk buffers around
				s.writers.Put(p.w)
			}
			if err != nil {
				s.CloseConnection()
				return
			}
		}
		if err := s.conn.Flush(); err != nil || closing {
			s.CloseConnection()
			return
		}
	}
}

// Addr ...
func (s *Session) Addr() net.Addr { return s.conn.NetConn().RemoteAddr() }

// Latency ...
// Latency is the round trip to the client, as the Java tab list shows it.
func (s *Session) Latency() time.Duration { return time.Duration(s.latency.Load()) }

// ChunkRadius ...
func (s *Session) ChunkRadius() int32 { return s.chunkRadius }

// ClientData describes the Java client in the Bedrock form Dragonfly keeps.
func (s *Session) ClientData() login.ClientData {
	return login.ClientData{
		GameVersion:    server.GameVersion + " (Java)",
		LanguageCode:   s.jp.Info.Locale,
		DeviceModel:    "Java Edition",
		ThirdPartyName: s.jp.Profile.Name,
	}
}

// Disconnect sends the reason and closes the connection.
func (s *Session) Disconnect(message string) {
	w := s.packet()
	server.TextComponent(w, message)
	s.queue(v777.ClientboundPlayDisconnect, w)
	s.CloseConnection() // the writer sends the reason first
}

// CloseConnection closes the network connection after writing what is queued, which may take at
// most a second; the read loop then removes the player. A session that never spawned is cleaned
// up here, since Close is never called for it.
func (s *Session) CloseConnection() {
	s.once.Do(func() {
		_ = s.conn.NetConn().SetWriteDeadline(time.Now().Add(time.Second))
		close(s.closed)
		if !s.spawned.Load() {
			s.cleanup()
		}
	})
}

// cleanup undoes what joining registered outside the world: the Bedrock peer, the Java profile
// and the tab list subscription.
func (s *Session) cleanup() {
	s.cleanupOnce.Do(func() {
		if s.tabs != nil {
			s.tabs.remove(s)
		}
		forgetProfile(s.id)
		forgetProfile(s.selfID)
		untrackJavaSession(s.id)
		if s.peer != nil {
			session.RemovePeer(s.peer)
		}
	})
}

// Close is called by the player when it is closed (in its world transaction, or with a nil tx if
// its world is gone). It hands the player to the server's close handler, which saves their data.
//
// Same order as the Bedrock session: save the player, close the chunk loader, and only then
// remove the player entity from the world (a player with a session is removed by the session).
func (s *Session) Close(tx *world.Tx, c session.Controllable) {
	s.closeOnce.Do(func() {
		if s.spawned.Load() && !s.quitMessage.Zero() {
			chat.Global.Writet(s.quitMessage, s.jp.Profile.Name)
		}
		s.closeContainers(tx, c)
		if s.onClose != nil {
			s.onClose(tx, c)
		}
		if tx != nil {
			if s.loader != nil {
				// The player may have been moved to another world since the last tick (quitting
				// on the death screen respawns them): the loader must leave the world it views.
				if s.loader.World() != tx.World() {
					s.loader.ChangeWorld(tx, tx.World())
				}
				s.loader.Close(tx)
			}
			tx.RemoveEntity(c)
			if s.ent != nil {
				_ = s.ent.Close()
			}
		}
		s.CloseConnection()
		s.cleanup()
		s.entMu.Lock()
		clear(s.entityIDs)
		s.entMu.Unlock()
	})
}

// Spawn starts the session once the player entity is in the world.
func (s *Session) Spawn(c session.Controllable, tx *world.Tx) {
	s.ent = c.H()
	s.spawned.Store(true)
	trackJavaSession(s.id, s)
	s.SendHealth(c.Health(), c.MaxHealth(), c.Absorption())
	s.SendFood(c.Food(), 0, 0)
	s.SendExperience(c.ExperienceLevel(), c.ExperienceProgress())
	s.SendAbilities(c)
	pos := c.Position()
	s.loader = world.NewLoader(int(s.chunkRadius), tx.World(), s)
	s.loader.Move(tx, pos)
	s.sendCentre(pos)
	s.showSelfTab(s.jp.Profile.Name, gameModeID(c.GameMode()))
	if !s.joinMessage.Zero() {
		chat.Global.Writet(s.joinMessage, s.jp.Profile.Name)
	}
	if s.tabs != nil {
		s.tabs.add(s)
	}
	s.SpawnText(c)
	go s.tickLoop()
	go s.readLoop()
}

// errPanic is returned by withPlayer when f (or what it called in Dragonfly) panicked.
var errPanic = errors.New("panic in player task")

// withPlayer runs f in the player's world transaction. A panic is logged and disconnects the
// client instead of taking the server down (the world re-raises task panics in the caller).
func (s *Session) withPlayer(f func(tx *world.Tx, c session.Controllable)) (err error) {
	defer func() {
		if r := recover(); r != nil {
			s.log.Error("panic in player task", "panic", r, "stack", string(debug.Stack()))
			s.Disconnect("Internal server error")
			err = errPanic
		}
	}()
	_, err = world.CallRef(context.Background(), world.NewEntityRef[session.Controllable](s.ent), func(tx *world.Tx, c session.Controllable) (struct{}, error) {
		f(tx, c)
		return struct{}{}, nil
	})
	return err
}

func stopped(err error) bool {
	return errors.Is(err, world.ErrEntityClosed) || errors.Is(err, world.ErrWorldClosed) || errors.Is(err, world.ErrTaskCancelled)
}

// tickLoop moves the chunk loader with the player and sends chunks, 20 times a second.
func (s *Session) tickLoop() {
	t := time.NewTicker(time.Second / 20)
	defer t.Stop()
	// Keep-alives measure the latency: one right away (so /ping has a value at once), then
	// every keepAliveInterval. A client that leaves one unanswered for keepAliveTimeout is
	// dropped, like vanilla.
	ka := time.NewTicker(keepAliveInterval)
	defer ka.Stop()
	s.sendKeepAlive()
	for {
		select {
		case <-s.closed:
			return
		case <-ka.C:
			if id := s.keepAlive.Load(); id != 0 {
				if time.Since(time.Unix(0, id)) > keepAliveTimeout {
					s.log.Info("keep-alive timed out")
					s.Disconnect("Timed out")
					return
				}
				continue // still waiting for the answer
			}
			s.sendKeepAlive()
		case <-t.C:
			err := s.withPlayer(func(tx *world.Tx, c session.Controllable) {
				if w := tx.World(); w != s.loader.World() {
					s.switchWorld(tx, w, c)
				}
				pos := c.Position()
				s.loader.Move(tx, pos)
				s.sendCentre(pos)
				s.sendChunkBatch(tx)
				s.continueBreaking(c)
			})
			if err != nil {
				if !stopped(err) {
					s.log.Debug("tick", "err", err)
				}
				return
			}
		}
	}
}

const (
	keepAliveInterval = 5 * time.Second
	keepAliveTimeout  = 20 * time.Second
)

// sendKeepAlive sends a keep-alive whose id is its send time.
func (s *Session) sendKeepAlive() {
	id := time.Now().UnixNano()
	s.keepAlive.Store(id)
	w := s.packet()
	w.Int64(id)
	s.queue(v777.ClientboundPlayKeepAlive, w)
}

// sendCentre tells the client which chunk it is in when that changes (it unloads chunks around it).
func (s *Session) sendCentre(pos mgl64.Vec3) {
	cp := world.ChunkPos{int32(math.Floor(pos[0])) >> 4, int32(math.Floor(pos[2])) >> 4}
	if s.centreSent && cp == s.lastCentre {
		return
	}
	s.lastCentre, s.centreSent = cp, true
	s.forgetFarChunks(cp)
	w := s.packet()
	w.VarInt(cp[0])
	w.VarInt(cp[1])
	s.queue(v777.ClientboundPlaySetChunkCacheCenter, w)
}

// readLoop handles the client's packets until the connection closes, then removes the player.
func (s *Session) readLoop() {
	defer func() {
		s.CloseConnection()
		// Closing the player calls s.Close, which saves the player through the server.
		err := s.withPlayer(func(tx *world.Tx, c session.Controllable) {
			if err := c.Close(); err != nil {
				s.log.Debug("close player", "err", err)
			}
		})
		if err != nil && !stopped(err) {
			s.log.Error("player not closed: data not saved", "err", err)
		}
		s.cleanup()
		s.log.Info("left")
	}()
	defer func() {
		// A packet our decoding or handling code chokes on drops the client, not the server.
		if r := recover(); r != nil {
			s.log.Error("panic handling packet", "panic", r, "stack", string(debug.Stack()))
			s.Disconnect("Internal server error")
		}
	}()
	for {
		id, body, err := s.conn.ReadPacket()
		if err != nil {
			s.log.Debug("read", "err", err)
			return
		}
		if nid := s.ver.ServerboundPlay(id); nid >= 0 {
			err = s.handle(nid, body)
		} else {
			err = s.handleLegacy(id, body)
		}
		if err != nil {
			s.log.Info("bad packet", "id", id, "err", err)
			s.Disconnect("Bad packet")
			return
		}
	}
}
