package javasession

import (
	"context"
	"io"
	"log/slog"
	"math"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/form"
	"github.com/df-mc/dragonfly/server/world"
	v777 "github.com/ezchr/go-mcjava/v777"
	"github.com/ezchr/go-mcjava/wire"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// testWorld is an in-memory flat world with one survival player at (0.5, 1, 0.5); s is a session
// for that player.
func testWorld(t *testing.T) (*world.World, *world.EntityHandle, *Session) {
	t.Helper()
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { w.Close() })
	h := world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 1, 0.5}}.New(player.Type, player.Config{
		Name: "Signer", UUID: uuid.New(), GameMode: world.GameModeSurvival, Health: 20, MaxHealth: 20,
	})
	do(w, func(tx *world.Tx) { tx.AddEntity(h) })
	s := fxTestSession()
	s.ent = h
	s.log = slog.New(slog.NewTextHandler(io.Discard, nil))
	return w, h, s
}

// do runs f in a transaction of w and waits for it.
func do(w *world.World, f func(tx *world.Tx)) {
	_, _ = world.Call(context.Background(), w, func(tx *world.Tx) (struct{}, error) {
		f(tx)
		return struct{}{}, nil
	})
}

func signTextAt(w *world.World, pos cube.Pos) (front, back string) {
	do(w, func(tx *world.Tx) {
		if s, ok := tx.Block(pos).(block.Sign); ok {
			front, back = s.Front.Text, s.Back.Text
		}
	})
	return
}

func signUpdate(pos cube.Pos, front bool, lines ...string) []byte {
	var p wire.Writer
	p.Position(pos[0], pos[1], pos[2])
	for i := 0; i < 4; i++ {
		l := ""
		if i < len(lines) {
			l = lines[i]
		}
		p.String(l)
	}
	if front {
		p.VarInt(1)
	} else {
		p.VarInt(0)
	}
	return p.B
}

// TestSignEdit: a Java sign_update is only applied to the sign side the server opened for this
// player, once, within reach, with formatting codes stripped; other positions are not even loaded.
func TestSignEdit(t *testing.T) {
	w, _, s := testWorld(t)
	pos := cube.Pos{2, 1, 0}
	far := cube.Pos{40, 1, 0}
	do(w, func(tx *world.Tx) {
		tx.SetBlock(pos, block.Sign{Wood: block.OakWood()}, nil)
		tx.SetBlock(far, block.Sign{Wood: block.OakWood()}, nil)
	})
	send := func(b []byte) {
		if handled, err := s.handleTextPacket(v777.ServerboundPlaySignUpdate, b); !handled || err != nil {
			t.Fatalf("sign_update: handled %v err %v", handled, err)
		}
	}

	// Not opened: ignored.
	send(signUpdate(pos, true, "hacked"))
	if f, _ := signTextAt(w, pos); f != "" {
		t.Fatalf("edit without OpenSign applied: %q", f)
	}
	// Opened: applied once, cleaned.
	s.OpenSign(pos, true)
	send(signUpdate(pos, true, "§cHello§r", "line\x07two", "", ""))
	if f, _ := signTextAt(w, pos); f != "Hello\nlinetwo" {
		t.Fatalf("front text %q", f)
	}
	send(signUpdate(pos, true, "again"))
	if f, _ := signTextAt(w, pos); f != "Hello\nlinetwo" {
		t.Fatalf("second edit applied: %q", f)
	}
	// Opened for the front, edit sent for the back: ignored.
	s.OpenSign(pos, true)
	send(signUpdate(pos, false, "back"))
	if _, b := signTextAt(w, pos); b != "" {
		t.Fatalf("other side edited: %q", b)
	}
	// Opened, but the player is out of reach: ignored.
	s.OpenSign(far, true)
	send(signUpdate(far, true, "far"))
	if f, _ := signTextAt(w, far); f != "" {
		t.Fatalf("out-of-reach sign edited: %q", f)
	}
	// A sign_update far away (never opened) loads no chunk.
	away := cube.Pos{1 << 20, 64, 1 << 20}
	send(signUpdate(away, true, "x"))
	do(w, func(tx *world.Tx) {
		if _, loaded := tx.BlockLoaded(away); loaded {
			t.Fatal("sign_update loaded a chunk at a client-chosen position")
		}
	})
}

func TestSignGrant(t *testing.T) {
	ts := &textState{}
	pos := cube.Pos{1, 2, 3}
	now := time.Now()
	if ts.takeSignGrant(pos, true, now) {
		t.Fatal("no grant accepted")
	}
	ts.sign = signGrant{pos: pos, front: true, at: now, ok: true}
	if !ts.takeSignGrant(pos, true, now) || ts.takeSignGrant(pos, true, now) {
		t.Fatal("grant not usable exactly once")
	}
	ts.sign = signGrant{pos: pos, front: true, at: now, ok: true}
	if ts.takeSignGrant(cube.Pos{1, 2, 4}, true, now) {
		t.Fatal("grant used for another sign")
	}
	ts.sign = signGrant{pos: pos, front: true, at: now.Add(-signEditWindow), ok: true}
	if ts.takeSignGrant(pos, true, now) {
		t.Fatal("expired grant accepted")
	}
}

func TestSignTextAndReach(t *testing.T) {
	for in, want := range map[[4]string]string{
		{"§cRed§r", "§lBold§kx", "", ""}:  "Red\nBoldx",
		{"§zodd", "a\x00b\x7fc", "§", ""}: "zodd\nabc",
		{"", "", "", ""}:                  "",
		{"a", "", "b", ""}:                "a\n\nb",
	} {
		if got := signText(in); got != want {
			t.Errorf("signText(%q) = %q, want %q", in, got, want)
		}
	}
	long := strings.Repeat("é", 384)
	got := signText([4]string{long, long, long, long})
	if len(got) > maxSignText || !utf8.ValidString(got) {
		t.Fatalf("long sign text: %d bytes, valid %v", len(got), utf8.ValidString(got))
	}

	pos := cube.Pos{0, 0, 0}
	eyes := func(d float64) mgl64.Vec3 { return pos.Vec3Middle().Add(mgl64.Vec3{d, 0, 0}) }
	for _, tc := range []struct {
		mode world.GameMode
		dead bool
		dist float64
		ok   bool
	}{
		{world.GameModeSurvival, false, 7.9, true},
		{world.GameModeSurvival, false, 8.1, false},
		{world.GameModeAdventure, false, 3, true},
		{world.GameModeCreative, false, 13.9, true},
		{world.GameModeCreative, false, 14.1, false},
		{world.GameModeSpectator, false, 1, false},
		{world.GameModeSurvival, true, 1, false},
	} {
		if got := signReachable(tc.mode, tc.dead, eyes(tc.dist), pos); got != tc.ok {
			t.Errorf("mode %T dead %v dist %v: %v", tc.mode, tc.dead, tc.dist, got)
		}
	}
}

// TestNameTagForget: a player's tag team and score are removed from the client when it leaves
// view, so the same UUID coming back with a plain name shows no stale tag.
func TestNameTagForget(t *testing.T) {
	w, h, s := testWorld(t)
	var p *player.Player
	do(w, func(tx *world.Tx) {
		e, _ := h.Entity(tx)
		p = e.(*player.Player)
		s.applyNameTag(p, "§6[VIP] §r"+p.Name())
		s.applyScoreTag(p, "10 kills")
	})
	ts := s.txt()
	if len(ts.teams) != 1 || len(ts.scoreShown) != 1 {
		t.Fatalf("teams %d scores %d", len(ts.teams), len(ts.scoreShown))
	}
	s.take()
	do(w, func(tx *world.Tx) { s.forgetEntityText(p) })
	var team, reset bool
	for _, o := range s.take() {
		r := wire.NewReader(o.w.B)
		switch o.id {
		case v777.ClientboundPlaySetPlayerTeam:
			r.String(64)
			team = r.Byte() == 1
		case v777.ClientboundPlayResetScore:
			reset = true
		}
	}
	if !team || !reset {
		t.Fatalf("team removed %v, score reset %v", team, reset)
	}
	if len(ts.teams) != 0 || len(ts.scoreShown) != 0 {
		t.Fatal("entries kept after forgetEntityText")
	}
	do(w, func(tx *world.Tx) { s.forgetEntityText(p) })
	if out := s.take(); len(out) != 0 {
		t.Fatalf("second forget sent %d packets", len(out))
	}
	// Overrides of closed handles are pruned.
	ts.mu.Lock()
	closed := world.EntitySpawnOpts{}.New(player.Type, player.Config{Name: "Gone", UUID: uuid.New()})
	closed.Close()
	ts.nameOverride = map[*world.EntityHandle]string{closed: "x", h: "y"}
	ts.pruneClosedOverrides()
	n := len(ts.nameOverride)
	ts.mu.Unlock()
	if n != 1 {
		t.Fatalf("%d overrides left, want the open one", n)
	}
}

// TestDialogStrings: dialog text inputs are modified UTF-8 (emoji as surrogate pairs, NUL as C0 80).
func TestDialogStrings(t *testing.T) {
	// "a😀\x00" in modified UTF-8.
	mutf := []byte{'a', 0xed, 0xa0, 0xbd, 0xed, 0xb8, 0x80, 0xc0, 0x80}
	var p wire.Writer
	p.Byte(10)
	p.Byte(8)
	p.Uint16(2)
	p.Raw([]byte("e0"))
	p.Uint16(uint16(len(mutf)))
	p.Raw(mutf)
	p.Byte(0)
	vals, err := readClickPayload(p.B)
	if err != nil {
		t.Fatal(err)
	}
	if got := vals["e0"]; got != "a\U0001F600\x00" {
		t.Fatalf("decoded %q", got)
	}
}

// TestSliderNaN: a NaN or infinite slider value makes the answer unusable; handleClickAction
// then submits the form as closed, so its Closer runs.
func TestSliderNaN(t *testing.T) {
	f := form.New(testCustom{
		L: form.NewLabel("l"), I: form.NewInput("i", "", ""), T: form.NewToggle("t", false),
		S: form.NewSlider("s", 0, 10, 1, 0), D: form.NewDropdown("d", []string{"a"}, 0),
	}, "c")
	for _, v := range []float32{float32(math.NaN()), float32(math.Inf(1))} {
		vals := map[string]any{"e3": v}
		if _, err := formResponse(f, -1, vals); err == nil {
			t.Fatalf("slider %v accepted", v)
		}
	}
	if b, err := formResponse(f, -1, map[string]any{"e3": float32(4)}); err != nil || b == nil {
		t.Fatalf("finite slider: %s %v", b, err)
	}
}

// TestCrackRestartRace restarts and stops cracks from several goroutines while their timers fire
// (run with -race), then checks that no timer chain outlives the crack.
func TestCrackRestartRace(t *testing.T) {
	s := fxTestSession()
	pos := cube.Pos{1, 2, 3}
	done := make(chan struct{})
	for g := 0; g < 4; g++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for i := 0; i < 200; i++ {
				switch i % 3 {
				case 0:
					s.startCrack(pos, 3*time.Millisecond)
				case 1:
					s.continueCrack(pos, 2*time.Millisecond)
				case 2:
					s.stopCrack(pos)
				}
			}
		}()
	}
	for g := 0; g < 4; g++ {
		<-done
	}
	s.stopCrack(pos)
	time.Sleep(20 * time.Millisecond)
	s.take()
	time.Sleep(30 * time.Millisecond)
	if out := s.take(); len(out) != 0 {
		t.Fatalf("%d packets after the crack stopped", len(out))
	}
}

// TestCrackStaleCallback: a timer callback that was already waiting for the lock when its crack
// was restarted must not start a second timer chain.
func TestCrackStaleCallback(t *testing.T) {
	s := fxTestSession()
	pos := cube.Pos{4, 5, 6}
	st := s.fxMake()
	st.mu.Lock()
	c := &crack{id: crackID(pos), stage: -1, start: time.Now(), total: 10 * time.Millisecond, gen: 1}
	st.cracks = map[cube.Pos]*crack{pos: c}
	s.crackTick(st, pos, c, 1)        // next stage in 1 ms
	time.Sleep(20 * time.Millisecond) // its callback fires and waits for st.mu
	// Restart while it waits, as startCrack does.
	c.timer.Stop()
	c.start, c.total = time.Now(), 10*time.Second
	c.gen++
	s.crackTick(st, pos, c, c.gen)
	current := c.timer
	st.mu.Unlock()
	time.Sleep(20 * time.Millisecond) // the stale callback runs now
	st.mu.Lock()
	replaced := c.timer != current
	st.mu.Unlock()
	s.stopCrack(pos)
	if replaced {
		t.Fatal("a stale timer callback started a second chain")
	}
}
