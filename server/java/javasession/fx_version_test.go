package javasession

import (
	"encoding/hex"
	"image/color"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	v776 "github.com/ezchr/go-mcjava/v776"
	v777 "github.com/ezchr/go-mcjava/v777"
	"github.com/ezchr/go-mcjava/version"
	"github.com/ezchr/go-mcjava/wire"
	"github.com/go-gl/mathgl/mgl64"
)

func fxTestSession262() *Session {
	s := fxTestSession()
	s.ver = version.V776
	return s
}

// Every id the fx, effect and entity code writes exists in 26.2, or is knowingly left out.
func TestFxNamesResolve262(t *testing.T) {
	l := legacyTables[version.V776]
	if l == nil {
		t.Fatal("no 26.2 tables")
	}
	for i, id := range soundIDs {
		if version.Map(l.sound, id) < 0 {
			t.Errorf("sound %q not in 26.2", soundNames[i])
		}
	}
	for i, id := range particleIDs {
		if version.Map(l.particle, id) < 0 {
			t.Errorf("particle %q not in 26.2", particleNames[i])
		}
	}
	for i, p := range openCloseIDs[1:] {
		if version.Map(l.sound, p[0]) < 0 || version.Map(l.sound, p[1]) < 0 {
			t.Errorf("open/close %v not in 26.2", openCloseDefs[i+1])
		}
	}
	missing := 0
	for i, g := range blockSoundGroups {
		for j, id := range [5]int32{g.brk, g.step, g.place, g.hit, g.fall} {
			if version.Map(l.sound, id) < 0 {
				missing++
				t.Logf("block sound group %d: %q not in 26.2 (26.3 blocks only)", i, blockSoundGroupDefs[i].names[j])
			}
		}
	}
	for id := 1; id <= 30; id++ {
		if typ, ok := effect.ByID(id); ok {
			if j, _ := javaEffect(typ); version.Map(l.mobEffect, j) < 0 {
				t.Errorf("effect %d not in 26.2", id)
			}
		}
	}
	for _, d := range sound.MusicDiscs() {
		if version.Map(l.jukeboxSong, jukeboxSongs[d.Uint8()]) < 0 {
			t.Errorf("disc %v has no 26.2 jukebox song", d)
		}
	}
	if version.Map(l.attribute, attributeMovementSpeed) != v776.BuiltinID("minecraft:attribute", "minecraft:movement_speed") {
		t.Error("movement_speed")
	}
	if version.Map(l.damageType, damageGeneric) != v776.RegistryID("minecraft:damage_type", "minecraft:generic") {
		t.Error("damage_type generic")
	}
	if version.Map(l.item, javaEggItem) != v776.BuiltinID("minecraft:item", "minecraft:egg") ||
		version.Map(l.block, decoratedPotJava) != v776.BuiltinID("minecraft:block", "minecraft:decorated_pot") {
		t.Error("egg / decorated pot")
	}
	if version.V776.BlockState(barrierState) == 1 {
		t.Error("barrier state")
	}
	for _, p := range []int32{parserBool, parserDouble, parserInteger, parserString, parserEntity, parserVec3} {
		if version.Map(l.argumentType, p) != p {
			t.Errorf("command parser %d moved in 26.2: check its properties", p)
		}
	}
	if l.particleBlock < 0 || l.particlePortal < 0 {
		t.Error("block / portal particle")
	}
}

// parseParticles262 checks a level_particles packet against the 26.2 codec (particle last).
func parseParticles262(t *testing.T, b []byte) int32 {
	t.Helper()
	r := wire.NewReader(b)
	r.Bool()
	r.Bool()
	for range 3 {
		r.Float64()
	}
	for range 4 {
		r.Float32()
	}
	r.Int32()
	typ := r.VarInt()
	id := func(n string) int32 { return v776.BuiltinID("minecraft:particle_type", "minecraft:"+n) }
	switch typ {
	case id("dust"):
		r.Int32()
		r.Float32()
	case id("block_marker"), id("block"):
		if st := r.VarInt(); st <= 0 || st >= int32(version.V776.BlockStates) {
			t.Fatalf("block state %d", st)
		}
	case id("entity_effect"):
		r.Int32()
	case id("item"):
		if it := r.VarInt(); it != v776.BuiltinID("minecraft:item", "minecraft:egg") {
			t.Fatalf("item %d", it)
		}
		r.VarInt()
		r.VarInt()
		r.VarInt()
	case id("flame"), id("note"), id("dripping_water"), id("dripping_lava"), id("lava"), id("dust_plume"),
		id("explosion_emitter"), id("item_snowball"), id("portal"):
	default:
		t.Fatalf("unexpected 26.2 particle type %d", typ)
	}
	if r.Err != nil || r.Len() != 0 {
		t.Fatalf("bad 26.2 level_particles %x (%v)", b, r.Err)
	}
	return typ
}

func TestEverySound262(t *testing.T) {
	s := fxTestSession262()
	pos := mgl64.Vec3{10.5, 64, -3.25}
	n := len(v776.Builtin["minecraft:sound_event"])
	for _, snd := range allSounds {
		s.ViewSound(pos, snd)
		out := s.take()
		if len(out) == 0 {
			t.Errorf("%T: no packet", snd)
		}
		for _, p := range out {
			switch p.id {
			case v777.ClientboundPlaySound:
				r := wire.NewReader(p.w.B)
				if id := r.VarInt() - 1; id >= int32(n) {
					t.Errorf("%T: sound %d out of the 26.2 range", snd, id)
				}
			case v777.ClientboundPlayLevelEvent:
				typ := checkLevelEvent(t, p.w.B)
				if typ >= 2014 && typ <= 2020 || typ == 1053 || typ == 1054 {
					t.Errorf("%T: level event %d is 26.3 only", snd, typ)
				}
			default:
				t.Errorf("%T: packet %#x", snd, p.id)
			}
		}
	}
	// The wax-on level event plays the sound itself in 26.2.
	s.ViewSound(pos, sound.SignWaxed{})
	if out := s.take(); len(out) != 1 || out[0].id != v777.ClientboundPlayLevelEvent {
		t.Errorf("sign waxed: %d packets", len(out))
	}
	// A disc's song is the 26.2 jukebox_song id.
	s.ViewSound(pos, sound.MusicDiscPlay{DiscType: sound.DiscLavaChicken()})
	if out := s.take(); len(out) != 1 {
		t.Fatal("disc")
	} else {
		r := wire.NewReader(out[0].w.B)
		r.Int32()
		r.Position()
		if d := r.Int32(); d != v776.RegistryID("minecraft:jukebox_song", "minecraft:lava_chicken") {
			t.Errorf("disc song %d", d)
		}
	}
}

func TestEveryParticle262(t *testing.T) {
	s := fxTestSession262()
	for _, pa := range allParticles {
		s.ViewParticle(mgl64.Vec3{1.5, 70, 2.5}, pa)
		out := s.take()
		if _, crack := pa.(particle.PunchBlock); crack {
			if len(out) != 0 {
				t.Errorf("crack particles sent to 26.2 (vanilla 26.2 has none for others' mining)")
			}
			continue
		}
		if len(out) != 1 {
			t.Errorf("%T: %d packets", pa, len(out))
			continue
		}
		switch out[0].id {
		case v777.ClientboundPlayLevelParticles:
			parseParticles262(t, out[0].w.B)
		case v777.ClientboundPlayLevelEvent:
			if typ := checkLevelEvent(t, out[0].w.B); typ >= 2014 && typ <= 2020 {
				t.Errorf("%T: level event %d is 26.3 only", pa, typ)
			}
		default:
			t.Errorf("%T: packet %#x", pa, out[0].id)
		}
	}
	// 2001 carries the 26.2 state.
	s.ViewParticle(mgl64.Vec3{1.5, 70, 2.5}, particle.BlockBreak{Block: block.Stone{}})
	out := s.take()
	r := wire.NewReader(out[0].w.B)
	r.Int32()
	r.Position()
	if st := r.Int32(); st != 1 { // stone is state 1 in both versions
		t.Errorf("2001 state %d", st)
	}
	st, _ := javaState(block.Calcite{})
	s.levelEvent(levelEventDestroyBlock, cube.Pos{}, st, false)
	r = wire.NewReader(s.take()[0].w.B)
	r.Int32()
	r.Position()
	if got, want := r.Int32(), version.V776.BlockState(st); got != want || got == st {
		t.Errorf("2001 calcite state %d, want %d (26.3 %d)", got, want, st)
	}
}

func TestEffects262(t *testing.T) {
	s := fxTestSession262()
	s.SendEffect(effect.New(effect.Speed, 2, 30*time.Second))
	s.SendEffectRemoval(effect.Speed)
	s.SendSpeed(0.15)
	out := s.take()
	// Same bytes as 26.2 vanilla (effect and attribute ids did not move).
	for i, h := range []string{"010001d8040e", "0100", "01011a3fc3333333333333" + "00"} {
		if got := hex.EncodeToString(out[i].w.B); got != h {
			t.Errorf("effect packet %d:\n got %s\nwant %s", i, got, h)
		}
	}
}

func TestPositionSync262(t *testing.T) {
	s := fxTestSession262()
	rot := cube.Rotation{90, 10}
	s.positionSync262(7, true, true, true, true, [3]int64{4096, -1, 2}, 64, 7, mgl64.Vec3{}, rot, true)
	s.positionSync262(7, true, true, false, false, [3]int64{1, 2, 3}, 64, 7, mgl64.Vec3{}, rot, false)
	s.positionSync262(7, true, false, true, false, [3]int64{}, 64, 7, mgl64.Vec3{}, rot, true)
	s.positionSync262(7, false, true, true, true, [3]int64{}, 64, 7, mgl64.Vec3{1, 2, 3}, rot, true)
	out := s.take()
	want := []struct {
		id  int32
		hex string
	}{
		{v777.ClientboundPlayMoveEntityPosRot, "07" + "1000" + "ffff" + "0002" + "40" + "07" + "01"},
		{v777.ClientboundPlayRotateHead, "0740"},
		{v777.ClientboundPlayMoveEntityPos, "07" + "0001" + "0002" + "0003" + "00"},
		{v777.ClientboundPlayMoveEntityRot, "07" + "40" + "07" + "01"},
		{v777.ClientboundPlayEntityPositionSync, "07" + "3ff0000000000000" + "4000000000000000" + "4008000000000000" +
			"0000000000000000" + "0000000000000000" + "0000000000000000" + "42b40000" + "41200000" + "01"},
		{v777.ClientboundPlayRotateHead, "0740"},
	}
	if len(out) != len(want) {
		t.Fatalf("%d packets", len(out))
	}
	for i, w := range want {
		if out[i].id != w.id || hex.EncodeToString(out[i].w.B) != w.hex {
			t.Errorf("packet %d: %#x %x, want %#x %s", i, out[i].id, out[i].w.B, w.id, w.hex)
		}
	}
}

func TestAnimate262(t *testing.T) {
	s := fxTestSession262()
	s.animate(3, 0) // wake up
	s.animate(3, 1) // critical hit
	s.animate(3, 2) // magic critical hit
	for i, p := range s.take() {
		if got, want := hex.EncodeToString(p.w.B), []string{"0302", "0304", "0305"}[i]; got != want {
			t.Errorf("animate %d: %s, want %s", i, got, want)
		}
	}
	s = fxTestSession()
	s.animate(3, 1)
	if got := hex.EncodeToString(s.take()[0].w.B); got != "0301" {
		t.Errorf("26.3 crit %s", got)
	}
}

func TestBlockEvent262(t *testing.T) {
	s := fxTestSession262()
	s.ViewBlockAction(cube.Pos{1, 2, 3}, block.DecoratedPotWobbleAction{Success: true})
	r := wire.NewReader(s.take()[0].w.B)
	r.Position()
	r.Byte()
	r.Byte()
	if b := r.VarInt(); b != v776.BuiltinID("minecraft:block", "minecraft:decorated_pot") {
		t.Errorf("block %d", b)
	}
}

func TestCommandParsers262(t *testing.T) {
	tr := &cmdTree{}
	root := tr.add(cmdNode{kind: nodeRoot, redirect: -1})
	lit := tr.add(cmdNode{kind: nodeLiteral, name: "x", redirect: -1})
	arg := tr.add(cmdNode{kind: nodeArgument, name: "v", parser: parserVec3, exec: true, redirect: -1})
	tr.nodes[root].children = []int32{lit}
	tr.nodes[lit].children = []int32{arg}
	var a, b wire.Writer
	tr.encode(&a, nil)
	tr.encode(&b, legacyTables[version.V776].argumentType)
	if hex.EncodeToString(a.B) != hex.EncodeToString(b.B) {
		t.Errorf("26.2 tree %x, 26.3 %x", b.B, a.B)
	}
}

func TestOpenSign262(t *testing.T) {
	for _, c := range []struct {
		s     *Session
		front string
		back  string
	}{{fxTestSession(), "01", "00"}, {fxTestSession262(), "01", "00"}} {
		c.s.OpenSign(cube.Pos{1, 2, 3}, true)
		c.s.OpenSign(cube.Pos{1, 2, 3}, false)
		out := c.s.take()
		if len(out) != 2 || hex.EncodeToString(out[0].w.B[8:]) != c.front || hex.EncodeToString(out[1].w.B[8:]) != c.back {
			t.Errorf("%s: open_sign_editor %x %x", c.s.ver.Name, out[0].w.B, out[1].w.B)
		}
	}
}

// Bytes vanilla 26.2 sent for /particle (tools/capture/fx-vanilla262).
func TestParticlesMatchVanilla262(t *testing.T) {
	s := fxTestSession262()
	pos := mgl64.Vec3{math.Float64frombits(0x401e97ad731b5acf), -59, math.Float64frombits(0x40357bac52a02f6c)}
	s.simpleParticle(ptFlame, pos, 1)
	w := s.particleType(ptDust)
	w.Int32(rgb(color.RGBA{R: 255}))
	w.Float32(1)
	s.particleAt(w, false, pos, 0, 0, 0, 0, 1)
	w = s.particleType(ptEntityEffect)
	w.Int32(argb(color.RGBA{G: 255, A: 255}))
	s.particleAt(w, false, pos, 0, 0, 0, 0, 1)
	w = s.particleType(ptBlockMarker)
	w.VarInt(s.ver.BlockState(barrierState))
	s.particleAt(w, false, pos, 0, 0, 0, 0, 1)
	w = s.particleType(ptItem)
	w.VarInt(s.itemID(javaEggItem))
	w.VarInt(1)
	w.VarInt(0)
	w.VarInt(0)
	s.particleAt(w, false, pos, 0, 0, 0, 0.03, 8)
	head := "0000401e97ad731b5acfc04d80000000000040357bac52a02f6c" + strings.Repeat("00", 12)
	want := []string{
		head + "00000000" + "00000001" + "27",
		head + "00000000" + "00000001" + "15" + "00ff0000" + "3f800000",
		head + "00000000" + "00000001" + "1c" + "ff00ff00",
		// Vanilla sent f661 (barrier, waterlogged=false); barrierState is the first state
		// (waterlogged=true) on both versions, as before.
		head + "00000000" + "00000001" + "02" + "f561",
		head + "3cf5c28f" + "00000008" + "36" + "a408" + "01" + "0000",
	}
	out := s.take()
	if len(out) != len(want) {
		t.Fatalf("%d packets", len(out))
	}
	for i, h := range want {
		if got := hex.EncodeToString(out[i].w.B); got != h {
			t.Errorf("particle %d:\n got %s\nwant %s", i, got, h)
		}
	}
}

// Bytes vanilla 26.2 sent for a mob's delta move (move_entity_pos: shorts, then onGround).
func TestMoveMatchesVanilla262(t *testing.T) {
	s := fxTestSession262()
	s.positionSync262(396, true, true, false, false, [3]int64{}, 0, 0, mgl64.Vec3{}, cube.Rotation{}, true)
	if got := hex.EncodeToString(s.take()[0].w.B); got != "8c0300000000000001" {
		t.Errorf("move_entity_pos %s", got)
	}
}

func TestSignUpdate262(t *testing.T) {
	var w wire.Writer
	w.Position(1, -2, 3)
	w.Bool(false) // back
	for _, l := range []string{"a", "b", "", ""} {
		w.String(l)
	}
	pos, lines, front := readSignUpdate(wire.NewReader(w.B), false)
	if pos != (cube.Pos{1, -2, 3}) || lines != [4]string{"a", "b", "", ""} || front {
		t.Errorf("26.2 sign_update: %v %q %v", pos, lines, front)
	}
	w.Reset()
	w.Position(1, -2, 3)
	for _, l := range []string{"a", "b", "", ""} {
		w.String(l)
	}
	w.VarInt(1) // front
	r := wire.NewReader(w.B)
	pos, lines, front = readSignUpdate(r, true)
	if r.Err != nil || r.Len() != 0 || pos != (cube.Pos{1, -2, 3}) || lines != [4]string{"a", "b", "", ""} || !front {
		t.Errorf("26.3 sign_update: %v %q %v", pos, lines, front)
	}
}
