package javasession

import (
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/player"
	jserver "github.com/ezchr/go-mcjava/server"
	v777 "github.com/ezchr/go-mcjava/v777"
	"github.com/ezchr/go-mcjava/wire"
	"github.com/google/uuid"
)

// tabList keeps the Java tab list of every Java session in line with the players online, like
// the Bedrock player list: everyone online, not only the players in view. One goroutine per
// server takes a snapshot of the online players once a second and every session applies it.
type tabList struct {
	srv *server.Server

	mu       sync.Mutex
	sessions map[*Session]struct{}
	running  bool
}

// tabEntry is an online player as the tab list shows them.
type tabEntry struct {
	name     string
	gameMode int32
	latency  int32  // milliseconds
	xuid     string // a Bedrock player's, for their skin; "" for Java players
}

// newTabEntry describes p for the tab list.
func newTabEntry(p *player.Player) tabEntry {
	e := tabEntry{
		name:     tabName(p.Name()),
		gameMode: gameModeID(p.GameMode()),
		latency:  int32(min(p.Latency(), time.Minute) / time.Millisecond),
	}
	if !isJavaPlayer(p.UUID()) {
		e.xuid = p.XUID()
	}
	return e
}

// props is the profile properties of a tab entry: a Java player's own, or a Bedrock player's
// skin from GeyserMC's database (nil until it is found).
func (e tabEntry) props(id uuid.UUID) []jserver.Property {
	if e.xuid == "" {
		return profileProperties(id)
	}
	props, _ := bedrockSkinProps(e.xuid)
	return props
}

// tabState is one session's view of the tab list.
type tabState struct {
	mu sync.Mutex
	// shown holds the player infos the client has. listed is false for players it has only for
	// their entity (NPCs, or players the snapshot has not seen yet).
	shown map[uuid.UUID]tabShown
	last  time.Time // last latency update
}

type tabShown struct {
	listed   bool
	gameMode int32
	// skinless: a Bedrock player shown without a skin; re-added once the skin is found.
	skinless bool
}

func newTabList(srv *server.Server) *tabList {
	return &tabList{srv: srv, sessions: map[*Session]struct{}{}}
}

// add starts keeping s's tab list up to date.
func (t *tabList) add(s *Session) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sessions[s] = struct{}{}
	if !t.running {
		t.running = true
		go t.run()
	}
}

func (t *tabList) remove(s *Session) {
	t.mu.Lock()
	delete(t.sessions, s)
	t.mu.Unlock()
}

// run syncs the sessions once a second while there are any.
func (t *tabList) run() {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for range tick.C {
		t.mu.Lock()
		if len(t.sessions) == 0 {
			t.running = false
			t.mu.Unlock()
			return
		}
		sessions := make([]*Session, 0, len(t.sessions))
		for s := range t.sessions {
			sessions = append(sessions, s)
		}
		t.mu.Unlock()

		snap := t.snapshot()
		for _, s := range sessions {
			s.syncTab(snap)
		}
	}
}

// snapshot reads the online players. The fields are copied in the player's own transaction.
func (t *tabList) snapshot() map[uuid.UUID]tabEntry {
	snap := make(map[uuid.UUID]tabEntry, t.srv.PlayerCount())
	for p := range t.srv.Players(nil) {
		e := newTabEntry(p)
		PrefetchBedrockSkin(e.xuid)
		snap[p.UUID()] = e
	}
	return snap
}

// tabAction bits of player_info_update.
const (
	tabAddPlayer      = 1 << 0
	tabUpdateGameMode = 1 << 2
	tabUpdateListed   = 1 << 3
	tabUpdateLatency  = 1 << 4
)

// writeTabAdd writes one ADD_PLAYER|UPDATE_GAME_MODE|UPDATE_LISTED|UPDATE_LATENCY entry. Adding a
// player the client already has only updates it (the client keeps the first profile).
func writeTabAdd(w *wire.Writer, id uuid.UUID, e tabEntry, listed bool) {
	w.UUID(id)
	w.String(e.name)
	// Signed textures show the skin; without properties the client picks a default skin from
	// the UUID.
	props := e.props(id)
	w.VarInt(int32(len(props)))
	for _, pr := range props {
		w.String(pr.Name)
		w.String(pr.Value)
		w.Bool(pr.Signature != "")
		if pr.Signature != "" {
			w.String(pr.Signature)
		}
	}
	w.VarInt(e.gameMode)
	w.Bool(listed)
	w.VarInt(e.latency)
}

// syncTab applies a snapshot of the online players to the client's tab list.
func (s *Session) syncTab(snap map[uuid.UUID]tabEntry) {
	s.tab.mu.Lock()
	defer s.tab.mu.Unlock()

	var add, reskin []uuid.UUID
	for id, e := range snap {
		if id == s.id {
			continue // the client has itself under selfID (showSelfTab)
		}
		sh, ok := s.tab.shown[id]
		if e.xuid != "" && (!ok || sh.skinless) {
			props, settled := bedrockSkinProps(e.xuid)
			if !ok && !settled {
				continue // hold a new Bedrock player back a moment, so they show with their skin
			}
			if ok && sh.skinless && props != nil {
				// The client keeps the first profile it gets: remove and add again.
				reskin = append(reskin, id)
				continue
			}
		}
		if !ok || !sh.listed || sh.gameMode != e.gameMode {
			add = append(add, id)
		}
	}
	if len(reskin) > 0 {
		w := s.packet()
		w.VarInt(int32(len(reskin)))
		for _, id := range reskin {
			w.UUID(id)
			delete(s.tab.shown, id)
		}
		s.queue(v777.ClientboundPlayPlayerInfoRemove, w)
		add = append(add, reskin...)
	}
	if len(add) > 0 {
		w := s.packet()
		w.Byte(tabAddPlayer | tabUpdateGameMode | tabUpdateListed | tabUpdateLatency)
		w.VarInt(int32(len(add)))
		for _, id := range add {
			e := snap[id]
			writeTabAdd(w, id, e, true)
			s.tab.shown[id] = tabShown{listed: true, gameMode: e.gameMode, skinless: e.xuid != "" && e.props(id) == nil}
		}
		s.queue(v777.ClientboundPlayPlayerInfoUpdate, w)
	}

	var gone []uuid.UUID
	for id, sh := range s.tab.shown {
		if _, ok := snap[id]; !ok && sh.listed && id != s.selfID {
			gone = append(gone, id)
		}
	}
	if len(gone) > 0 {
		w := s.packet()
		w.VarInt(int32(len(gone)))
		for _, id := range gone {
			w.UUID(id)
			delete(s.tab.shown, id)
		}
		s.queue(v777.ClientboundPlayPlayerInfoRemove, w)
	}

	// Pings, as often as vanilla (every 600 ticks).
	if time.Since(s.tab.last) >= 30*time.Second && len(snap) > 0 {
		s.tab.last = time.Now()
		w := s.packet()
		w.Byte(tabUpdateLatency)
		w.VarInt(int32(len(snap)))
		for id, e := range snap {
			if id == s.id {
				id = s.selfID
			}
			w.UUID(id)
			w.VarInt(e.latency)
		}
		s.queue(v777.ClientboundPlayPlayerInfoUpdate, w)
	}
}

// showTabFor makes sure the client has p's info before p's entity is spawned (the client needs the
// profile to spawn a player). A player not online yet is added unlisted; the next sync lists them.
func (s *Session) showTabFor(p *player.Player) {
	id := p.UUID()
	s.tab.mu.Lock()
	defer s.tab.mu.Unlock()
	if _, ok := s.tab.shown[id]; ok {
		return
	}
	e := newTabEntry(p)
	w := s.packet()
	w.Byte(tabAddPlayer | tabUpdateGameMode | tabUpdateListed | tabUpdateLatency)
	w.VarInt(1)
	writeTabAdd(w, id, e, false)
	s.queue(v777.ClientboundPlayPlayerInfoUpdate, w)
	s.tab.shown[id] = tabShown{gameMode: e.gameMode, skinless: e.xuid != "" && e.props(id) == nil}
}

// hideTabFor drops p's info when p's entity leaves view, unless p is listed as online.
func (s *Session) hideTabFor(id uuid.UUID) {
	s.tab.mu.Lock()
	defer s.tab.mu.Unlock()
	if sh, ok := s.tab.shown[id]; !ok || sh.listed {
		return
	}
	delete(s.tab.shown, id)
	w := s.packet()
	w.VarInt(1)
	w.UUID(id)
	s.queue(v777.ClientboundPlayPlayerInfoRemove, w)
}

// showSelfTab lists the player for themselves: the client reads its own skin, game mode (so
// spectators fly through blocks) and ping from this entry.
func (s *Session) showSelfTab(name string, gameMode int32) {
	s.tab.mu.Lock()
	defer s.tab.mu.Unlock()
	w := s.packet()
	w.Byte(tabAddPlayer | tabUpdateGameMode | tabUpdateListed | tabUpdateLatency)
	w.VarInt(1)
	writeTabAdd(w, s.selfID, tabEntry{name: tabName(name), gameMode: gameMode}, true)
	s.queue(v777.ClientboundPlayPlayerInfoUpdate, w)
	s.tab.shown[s.selfID] = tabShown{listed: true, gameMode: gameMode}
}

// updateSelfGameMode tells the client its own new game mode in the tab list.
func (s *Session) updateSelfGameMode(gameMode int32) {
	s.tab.mu.Lock()
	defer s.tab.mu.Unlock()
	w := s.packet()
	w.Byte(tabUpdateGameMode)
	w.VarInt(1)
	w.UUID(s.selfID)
	w.VarInt(gameMode)
	s.queue(v777.ClientboundPlayPlayerInfoUpdate, w)
	if sh, ok := s.tab.shown[s.selfID]; ok {
		sh.gameMode = gameMode
		s.tab.shown[s.selfID] = sh
	}
}
