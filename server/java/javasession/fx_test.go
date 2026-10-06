package javasession

import (
	"bytes"
	"encoding/hex"
	"image/color"
	"strings"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	v777 "github.com/ezchr/go-mcjava/v777"
	"github.com/ezchr/go-mcjava/version"
	"github.com/ezchr/go-mcjava/wire"
	"github.com/go-gl/mathgl/mgl64"
)

func fxTestSession() *Session {
	s := &Session{wake: make(chan struct{}, 1), ver: version.Newest}
	s.writers.New = func() any { return &wire.Writer{B: make([]byte, 0, 256)} }
	return s
}

// take returns the queued packets and forgets them.
func (s *Session) take() []outPacket {
	s.outMu.Lock()
	defer s.outMu.Unlock()
	o := s.out
	s.out = nil
	return o
}

// recycle drops the queued packets like the writer does after sending them.
func (s *Session) recycle() {
	s.outMu.Lock()
	for _, p := range s.out {
		s.writers.Put(p.w)
	}
	s.out = s.out[:0]
	s.outMu.Unlock()
	select {
	case <-s.wake:
	default:
	}
}

func TestFxNamesResolve(t *testing.T) {
	for i, id := range soundIDs {
		if id < 0 {
			t.Errorf("sound %q not in 26.3", soundNames[i])
		}
	}
	for i, id := range particleIDs {
		if id < 0 {
			t.Errorf("particle %q not in 26.3", particleNames[i])
		}
	}
	for i, g := range blockSoundGroups {
		for j, id := range [5]int32{g.brk, g.step, g.place, g.hit, g.fall} {
			if id < 0 {
				t.Errorf("block sound group %d: %q not in 26.3", i, blockSoundGroupDefs[i].names[j])
			}
		}
	}
	for i, p := range openCloseIDs[1:] {
		if p[0] < 0 || p[1] < 0 {
			t.Errorf("open/close %v not in 26.3", openCloseDefs[i+1])
		}
	}
	for id := 1; id <= 30; id++ {
		if typ, ok := effect.ByID(id); ok {
			if _, ok := javaEffect(typ); !ok {
				t.Errorf("Dragonfly effect %d (%T) has no Java effect", id, typ)
			}
		}
	}
	for _, d := range sound.MusicDiscs() {
		if jukeboxSongs[d.Uint8()] < 0 {
			t.Errorf("disc %v has no jukebox song", d)
		}
	}
	if len(stateSoundGroup) != javaStateCount || len(v777.Registries) == 0 {
		t.Fatal("tables out of date")
	}
	if javaEggItem < 0 || barrierState <= 0 || decoratedPotJava < 0 {
		t.Fatal("item/block ids missing")
	}
}

func TestBlockSoundTables(t *testing.T) {
	stone, _ := javaState(block.Stone{})
	if g := soundGroupOf(stone); g.place != soundEventID("block.stone.place") {
		t.Errorf("stone place sound = %d", g.place)
	}
	if b := javaBlockOf(stone); b != v777.BuiltinID("minecraft:block", "minecraft:stone") {
		t.Errorf("stone block = %d", b)
	}
	if b := javaBlockOf(0); b != v777.BuiltinID("minecraft:block", "minecraft:air") {
		t.Errorf("state 0 block = %d", b)
	}
	if b := javaBlockOf(javaStateCount - 1); int(b) != len(blockFirstState)-1 {
		t.Errorf("last state block = %d", b)
	}
	for _, c := range []struct {
		b    world.Block
		open string
	}{
		{block.WoodDoor{}, "block.wooden_door.open"},
		{block.WoodDoor{Wood: block.CherryWood()}, "block.cherry_wood_door.open"},
		{block.CopperDoor{}, "block.copper_door.open"},
		{block.WoodTrapdoor{Wood: block.CrimsonWood()}, "block.nether_wood_trapdoor.open"},
		{block.WoodFenceGate{Wood: block.BambooWood()}, "block.bamboo_wood_fence_gate.open"},
	} {
		st, _ := javaState(c.b)
		if got := openCloseOf(st)[0]; got != soundEventID(c.open) {
			t.Errorf("%T: open sound %d, want %s", c.b, got, c.open)
		}
	}
	if st, _ := javaState(block.Stone{}); openCloseOf(st)[0] != -1 {
		t.Error("stone has an open sound")
	}
}

// parseSound checks a sound packet and returns its sound event id (-1 for inline) and source.
func parseSound(t *testing.T, b []byte) (int32, int32) {
	r := wire.NewReader(b)
	id := r.VarInt() - 1
	if id < 0 {
		r.String(32767)
		if r.Bool() {
			r.Float32()
		}
	} else if int(id) >= len(v777.Builtin["minecraft:sound_event"]) {
		t.Fatalf("sound id %d out of range", id)
	}
	src := r.VarInt()
	r.Int32()
	r.Int32()
	r.Int32()
	r.Float32()
	r.Float32()
	r.Int64()
	if r.Err != nil || r.Len() != 0 || src < 0 || src > int32(srcUI) {
		t.Fatalf("bad sound packet %x (%v)", b, r.Err)
	}
	return id, src
}

// parseParticles checks a level_particles packet against the 26.3 codec.
func parseParticles(t *testing.T, b []byte) int32 {
	r := wire.NewReader(b)
	typ := r.VarInt()
	switch typ {
	case particleIDs[ptDust]:
		r.Int32()
		r.Float32()
	case particleIDs[ptBlockMarker]:
		if st := r.VarInt(); st <= 0 || st >= javaStateCount {
			t.Fatalf("block marker state %d", st)
		}
	case particleIDs[ptEntityEffect]:
		r.Int32()
	case particleIDs[ptItem]:
		r.VarInt()
		r.VarInt()
		r.VarInt()
		r.VarInt()
	case particleIDs[ptFlame], particleIDs[ptNote], particleIDs[ptDrippingWater], particleIDs[ptDrippingLava],
		particleIDs[ptLava], particleIDs[ptDustPlume], particleIDs[ptExplosionEmitter], particleIDs[ptItemSnowball]:
	default:
		t.Fatalf("unexpected particle type %d", typ)
	}
	r.Bool()
	r.Bool()
	for range 3 {
		r.Float64()
	}
	for range 6 {
		r.Float32()
	}
	r.VarInt()
	if rt := r.VarInt(); r.Err != nil || r.Len() != 0 || rt != 0 {
		t.Fatalf("bad level_particles %x (%v)", b, r.Err)
	}
	return typ
}

func checkLevelEvent(t *testing.T, b []byte) int32 {
	r := wire.NewReader(b)
	typ := r.Int32()
	r.Position()
	r.Int32()
	r.Bool()
	if r.Err != nil || r.Len() != 0 {
		t.Fatalf("bad level_event %x", b)
	}
	return typ
}

func TestEverySound(t *testing.T) {
	s := fxTestSession()
	pos := mgl64.Vec3{10.5, 64, -3.25}
	for _, snd := range allSounds {
		s.ViewSound(pos, snd)
		out := s.take()
		if len(out) == 0 {
			t.Errorf("%T: no packet", snd)
		}
		for _, p := range out {
			switch p.id {
			case v777.ClientboundPlaySound:
				parseSound(t, p.w.B)
			case v777.ClientboundPlayLevelEvent:
				checkLevelEvent(t, p.w.B)
			default:
				t.Errorf("%T: packet %#x", snd, p.id)
			}
		}
	}
	// Unknown names that are not identifiers are dropped rather than disconnecting the client.
	s.ViewSound(pos, sound.Custom{Name: "Bad Name", Volume: 1, Pitch: 1})
	if len(s.take()) != 0 {
		t.Error("invalid custom sound name sent")
	}
	s.ViewSound(pos, sound.Custom{Name: "mypack:custom.thing", Volume: 1, Pitch: 1})
	if out := s.take(); len(out) != 1 {
		t.Error("custom sound not sent")
	} else if id, _ := parseSound(t, out[0].w.B); id != -1 {
		t.Error("custom sound not inline")
	}
}

func TestEveryParticle(t *testing.T) {
	s := fxTestSession()
	for _, pa := range allParticles {
		s.ViewParticle(mgl64.Vec3{1.5, 70, 2.5}, pa)
		out := s.take()
		if len(out) != 1 {
			t.Errorf("%T: %d packets", pa, len(out))
			continue
		}
		switch out[0].id {
		case v777.ClientboundPlayLevelParticles:
			parseParticles(t, out[0].w.B)
		case v777.ClientboundPlayLevelEvent:
			checkLevelEvent(t, out[0].w.B)
		default:
			t.Errorf("%T: packet %#x", pa, out[0].id)
		}
	}
}

// Bytes vanilla 26.3 sent (tools/capture/fx-vanilla), minus the random seed.
func TestFxMatchesVanilla(t *testing.T) {
	s := fxTestSession()
	// /particle flame ~ ~1 ~ 0 0 0 0 1
	w := s.particleType(ptFlame)
	s.particleAt(w, false, mgl64.Vec3{0.5, -59, -4.5}, 0, 0, 0, 0, 1)
	// /particle dust{color:16711680,scale:1.0} ...
	w = s.particleType(ptDust)
	w.Int32(rgb(color.RGBA{R: 255}))
	w.Float32(1)
	s.particleAt(w, false, mgl64.Vec3{0.5, -59, -4.5}, 0, 0, 0, 0, 1)
	// /particle entity_effect{color:-16711936} ...
	w = s.particleType(ptEntityEffect)
	w.Int32(argb(color.RGBA{G: 255, A: 255}))
	s.particleAt(w, false, mgl64.Vec3{0.5, -59, -4.5}, 0, 0, 0, 0, 1)
	// a registry sound (entity.slime.squish, hostile)
	s.soundID(mgl64.Vec3{-123.0 / 8, -480.0 / 8, -49.0 / 8}, v777.BuiltinID("minecraft:sound_event", "minecraft:entity.slime.squish"), srcHostile, 0.8, 1.33)
	out := s.take()
	tail := "0000" + "3fe0000000000000" + "c04d800000000000" + "c012000000000000" + strings.Repeat("00", 24) + "0100"
	want := []string{
		"27" + tail,
		"15" + "00ff0000" + "3f800000" + tail,
		"1c" + "ff00ff00" + tail,
	}
	for i, h := range want {
		if got := hex.EncodeToString(out[i].w.B); got != h {
			t.Errorf("particle %d:\n got %s\nwant %s", i, got, h)
		}
	}
	// de0b 05 ffffff85 fffffe20 ffffffcf (volume, pitch, seed differ)
	if got := out[3].w.B[:15]; !bytes.Equal(got, mustHex("de0b05ffffff85fffffe20ffffffcf")) {
		t.Errorf("sound head %x", got)
	}

	// effect give @a speed 30 1 -> entity, speed, amplifier 1, 600 ticks, visible|icon|blend
	s.SendEffect(effect.New(effect.Speed, 2, 30*time.Second))
	s.SendEffect(effect.NewInfinite(effect.NightVision, 1).WithoutParticles())
	s.SendEffectRemoval(effect.Speed)
	s.SendSpeed(0.15)
	out = s.take()
	for i, h := range []string{"010001d8040e", "010f00ffffffff0f0c", "0100", "01011a3fc3333333333333" + "00"} {
		if got := hex.EncodeToString(out[i].w.B); got != h {
			t.Errorf("effect packet %d:\n got %s\nwant %s", i, got, h)
		}
	}
}

func mustHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

func TestPredictedSuppression(t *testing.T) {
	s := fxTestSession()
	pos := cube.Pos{4, 64, 4}
	s.fxDestroyAction(actionStartDestroy, pos)
	s.ViewParticle(pos.Vec3(), particle.PunchBlock{Block: block.Stone{}, Face: cube.FaceUp})
	s.ViewSound(pos.Vec3(), sound.BlockBreaking{Block: block.Stone{}})
	s.fxDestroyAction(actionStopDestroy, pos)
	s.ViewParticle(pos.Vec3Centre(), particle.BlockBreak{Block: block.Stone{}})
	if out := s.take(); len(out) != 0 {
		t.Fatalf("own breaking sent %d packets", len(out))
	}
	// Someone else breaking the next block: shown.
	s.ViewParticle(pos.Side(cube.FaceEast).Vec3Centre(), particle.BlockBreak{Block: block.Stone{}})
	s.fxUsedOn(pos)
	s.ViewSound(pos.Side(cube.FaceUp).Vec3(), sound.BlockPlace{Block: block.Stone{}})
	s.PlaySound(sound.BlockPlace{Block: block.Stone{}}, pos.Side(cube.FaceUp).Vec3())
	if out := s.take(); len(out) != 2 {
		t.Fatalf("got %d packets, want break of another block and the PlaySound", len(out))
	}
}

func TestCrack(t *testing.T) {
	s := fxTestSession()
	pos := cube.Pos{1, 2, 3}
	s.ViewBlockAction(pos, block.StartCrackAction{BreakTime: 200 * time.Millisecond})
	time.Sleep(250 * time.Millisecond)
	s.ViewBlockAction(pos, block.StopCrackAction{})
	var stages []byte
	for _, p := range s.take() {
		if p.id != v777.ClientboundPlayBlockDestruction {
			t.Fatalf("packet %#x", p.id)
		}
		r := wire.NewReader(p.w.B)
		if id := r.VarInt(); id >= 0 {
			t.Fatalf("crack id %d", id)
		}
		r.Position()
		stages = append(stages, r.Byte())
	}
	if len(stages) != 11 || stages[0] != 0 || stages[9] != 9 || stages[10] != 255 {
		t.Fatalf("stages %v", stages)
	}
}

func BenchmarkViewSoundBlockPlace(b *testing.B) {
	benchSound(b, sound.BlockPlace{Block: block.Planks{Wood: block.OakWood()}})
}
func BenchmarkViewSoundAttack(b *testing.B) { benchSound(b, sound.Attack{Damage: true}) }
func BenchmarkViewSoundNote(b *testing.B) {
	benchSound(b, sound.Note{Instrument: sound.Bell(), Pitch: 7})
}
func BenchmarkViewSoundDoor(b *testing.B) { benchSound(b, sound.DoorOpen{Block: block.CopperDoor{}}) }
func BenchmarkViewSoundCustomBedrockName(b *testing.B) {
	benchSound(b, sound.Custom{Name: "random.levelup", Volume: 1, Pitch: 1})
}

func benchSound(b *testing.B, snd world.Sound) {
	s := fxTestSession()
	s.fxUsedOn(cube.Pos{100, 0, 100}) // a session with fx state, as after the first click
	pos := mgl64.Vec3{1, 2, 3}
	s.ViewSound(pos, snd) // warm up: the block tables are built on first use
	s.recycle()
	b.ReportAllocs()
	for b.Loop() {
		s.ViewSound(pos, snd)
		s.recycle()
	}
}

func BenchmarkViewParticleBlockBreak(b *testing.B) {
	benchParticle(b, particle.BlockBreak{Block: block.Stone{}})
}
func BenchmarkViewParticleDust(b *testing.B) {
	benchParticle(b, particle.Dust{Colour: color.RGBA{R: 255, A: 255}})
}
func BenchmarkViewParticleFlame(b *testing.B) { benchParticle(b, particle.Flame{}) }

func benchParticle(b *testing.B, pa world.Particle) {
	s := fxTestSession()
	pos := mgl64.Vec3{1, 2, 3}
	s.ViewParticle(pos, pa)
	s.recycle()
	b.ReportAllocs()
	for b.Loop() {
		s.ViewParticle(pos, pa)
		s.recycle()
	}
}

func BenchmarkSendEffect(b *testing.B) {
	s := fxTestSession()
	e := effect.New(effect.Speed, 2, 30*time.Second)
	b.ReportAllocs()
	for b.Loop() {
		s.SendEffect(e)
		s.recycle()
	}
}
