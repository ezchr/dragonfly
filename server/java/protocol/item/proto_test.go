package item

import (
	"bytes"
	"testing"

	"github.com/df-mc/dragonfly/server/java/protocol/version"
	"github.com/df-mc/dragonfly/server/java/protocol/wire"
)

// container_set_slot bodies a vanilla 26.2 server sent for the /give commands of vanillaSetSlot, in
// the same order (tools/capture/items-vanilla262), then a few more.
var vanilla262SetSlot = []string{
	"000a002440010000",
	"000b002501c40701000305",
	"000c002601c407010004",
	"000d002701c40701000608000548656c6c6f",
	"000e002801c40701000b020800054c696e65310800054c696e6532",
	"000f002901c40701000d012105",
	"0010002a01c407010011023fc00000400000000101010361626301000000ff",
	"0011002b01c40701001501",
	"0012002c01fa0901002a012103",
	"0013000901d60701002c00ff0000",
	"0014000a01fe080100330119000000",
	"0015000b01fe080100330001000000ff010001c801000101000103666f6f",
	"0016000c05ab0a010033011c000000",
	"0017000d01c407050003070406080001580b01080001610d012105",
	"0018000e01c407000110",
	"0019000f01c4070100000a03000161000000010800016200017800",
	"001a001001c407020002641303",
	"001b001101c4070100060a080005636f6c6f7200037265640800047465787400035265640100066974616c69630000",
	"001c001246ce0701000163",
	"001d001301c40701000d0228032105",
	"001e001401fe080100330000010001c8010100010000",
}

// More 26.2 stacks: what they are in 26.3 terms and the vanilla 26.2 bytes.
var vanilla262More = []struct {
	give string
	make func(t testing.TB) Stack
	hex  string
}{
	{`diamond_sword[tooltip_display={hidden_components:["minecraft:enchantments","minecraft:damage"]}]`, func(t testing.TB) Stack {
		s := Stack{Count: 1, ID: mustID(t, "minecraft:diamond_sword")}
		s.TooltipDisplay.Hidden = []int32{CompEnchantments, CompDamage}
		s.Add(CompTooltipDisplay)
		return s
	}, "001f001501c40701001200020d03"},
	{"birch_planks 3", func(t testing.TB) Stack { return Stack{Count: 3, ID: mustID(t, "minecraft:birch_planks")} }, "0020001603410000"},
	{"white_wool 2", func(t testing.TB) Stack { return Stack{Count: 2, ID: mustID(t, "minecraft:white_wool")} }, "0021001702f0010000"},
	{"white_carpet", func(t testing.TB) Stack { return Stack{Count: 1, ID: mustID(t, "minecraft:white_carpet")} }, "002200180195040000"},
}

// A 26.3 stack encoded for 26.2 is what vanilla 26.2 sends for it, and a 26.2 stack decodes to the
// 26.3 stack, in both the server and the client-to-server forms.
func TestVanilla262(t *testing.T) {
	p := For(version.V776)
	if p == nil || For(version.Newest) != nil {
		t.Fatal("For: wrong protos")
	}
	if len(vanilla262SetSlot) > len(vanillaSetSlot) {
		t.Fatal("more 26.2 vectors than 26.3 ones")
	}
	check := func(give string, s *Stack, want []byte) {
		t.Helper()
		var w wire.Writer
		s.EncodeFor(&w, p)
		if !bytes.Equal(w.B, want) {
			t.Errorf("%s: 26.2 encode:\n got %x\nwant %x", give, w.B, want)
		}
		var native wire.Writer
		s.Encode(&native)
		var back Stack
		r := wire.NewReader(want)
		back.DecodeFor(r, p)
		if r.Err != nil || r.Len() != 0 {
			t.Fatalf("%s: 26.2 decode: %v, %d bytes left", give, r.Err, r.Len())
		}
		var got wire.Writer
		back.Encode(&got)
		if !bytes.Equal(got.B, native.B) {
			t.Errorf("%s: 26.2 decode is not the 26.3 stack:\n got %x\nwant %x", give, got.B, native.B)
		}
		// Client-to-server form.
		w.Reset()
		s.EncodeUntrustedFor(&w, p)
		r = wire.NewReader(w.B)
		back.DecodeUntrustedFor(r, p)
		if r.Err != nil || r.Len() != 0 {
			t.Fatalf("%s: 26.2 untrusted decode: %v", give, r.Err)
		}
		got.Reset()
		back.Encode(&got)
		if !bytes.Equal(got.B, native.B) {
			t.Errorf("%s: 26.2 untrusted round trip:\n got %x\nwant %x", give, got.B, native.B)
		}
	}
	var s Stack
	for i, h := range vanilla262SetSlot {
		v := vanillaSetSlot[i]
		r := wire.NewReader(slotBytes(t, v.hex))
		s.Decode(r)
		if r.Err != nil {
			t.Fatalf("%s: %v", v.give, r.Err)
		}
		check(v.give, &s, slotBytes(t, h))
	}
	for _, v := range vanilla262More {
		s := v.make(t)
		check(v.give, &s, slotBytes(t, v.hex))
	}
}

// Items 26.2 lacks are sent as their stand-in; components it lacks are left out; raw components with
// a wire form that may differ are left out; a 26.2 client's stand-in decodes to the 26.2 item.
func TestProto262Remap(t *testing.T) {
	p := For(version.V776)
	poplar, ok := ByName("minecraft:poplar_planks")
	if !ok {
		t.Skip("no poplar planks in this version")
	}
	birch := mustID(t, "minecraft:birch_planks")
	if p.Item(poplar) != p.Item(birch) || p.ItemIn(p.Item(poplar)) != birch {
		t.Fatalf("poplar planks: 26.2 id %d (birch %d), back %d", p.Item(poplar), p.Item(birch), p.ItemIn(p.Item(poplar)))
	}
	if n := p.ItemName(poplar); n != "minecraft:birch_planks" {
		t.Errorf("poplar planks show as %q", n)
	}
	s := Stack{Count: 2, ID: poplar, Raw: []RawComponent{{CompWaxed, nil}, {CompRarity, []byte{1}}, {CompTrim, []byte{1, 2, 3}}}}
	s.Damage = 3
	s.Add(CompDamage)
	s.Remove(CompCushionColor)
	s.Remove(CompAttributeModifiers)
	var w wire.Writer
	s.EncodeFor(&w, p)
	var back Stack
	r := wire.NewReader(w.B)
	back.DecodeFor(r, p)
	if r.Err != nil || r.Len() != 0 {
		t.Fatalf("decode: %v", r.Err)
	}
	if back.ID != birch || back.Count != 2 || !back.Has(CompDamage) || back.Damage != 3 || !back.Has(CompRarity) ||
		back.Has(CompWaxed) || back.Has(CompTrim) || !back.Removed.Has(CompAttributeModifiers) || back.Removed.Has(CompCushionColor) {
		t.Errorf("26.2 round trip: %+v", back)
	}
	// A cushion (stacks to 16) is a carpet in 26.2 (64): it keeps its stack size.
	if cushion, ok := ByName("minecraft:white_cushion"); ok {
		s := Stack{Count: 3, ID: cushion}
		w.Reset()
		s.EncodeFor(&w, p)
		r := wire.NewReader(w.B)
		back.DecodeFor(r, p)
		if r.Err != nil || back.ID != mustID(t, "minecraft:white_carpet") || !back.Has(CompMaxStackSize) || back.MaxStackSize != 16 {
			t.Errorf("cushion for 26.2: %v %+v", r.Err, back)
		}
		s.MaxStackSize = 8
		s.Add(CompMaxStackSize)
		w.Reset()
		s.EncodeFor(&w, p)
		r = wire.NewReader(w.B)
		back.DecodeFor(r, p)
		if r.Err != nil || back.MaxStackSize != 8 || len(back.Order) > 1 {
			t.Errorf("cushion with its own stack size for 26.2: %v %+v", r.Err, back)
		}
	}
	// Stacks a 26.2 client sends: ids it has, components 26.3 lacks skipped.
	mapColor := int32(-1)
	for i, n := range p.compsIn {
		if n < 0 && p.oldKinds[int32(i)] == kindInt32 {
			mapColor = int32(i)
		}
	}
	if mapColor < 0 {
		t.Fatal("no map_color in 26.2")
	}
	w.Reset()
	w.VarInt(1)
	w.VarInt(p.Item(mustID(t, "minecraft:filled_map")))
	w.VarInt(2)
	w.VarInt(0)
	w.VarInt(mapColor)
	w.Int32(0xff00)
	w.VarInt(p.Component(CompDamage))
	w.VarInt(4)
	r = wire.NewReader(w.B)
	back.DecodeFor(r, p)
	if r.Err != nil || r.Len() != 0 || back.ID != mustID(t, "minecraft:filled_map") || !back.Has(CompDamage) || back.Damage != 4 || len(back.Raw) != 0 {
		t.Errorf("26.2 map_color stack: %v %+v", r.Err, back)
	}
	// Ids past 26.2's registries are rejected.
	w.Reset()
	w.VarInt(1)
	w.VarInt(int32(len(p.itemsIn)))
	w.VarInt(0)
	w.VarInt(0)
	r = wire.NewReader(w.B)
	back.DecodeFor(r, p)
	if r.Err == nil {
		t.Error("item id past 26.2's accepted")
	}
}

func BenchmarkEncodeEnchantedSword262(b *testing.B) {
	p := For(version.V776)
	s := Stack{Count: 1, ID: mustID(b, "minecraft:diamond_sword"), Damage: 7, Enchantments: []Enchantment{{33, 5}, {40, 3}}}
	s.CustomName.Text = "Excalibur"
	s.Add(CompDamage)
	s.Add(CompEnchantments)
	s.Add(CompCustomName)
	var w wire.Writer
	b.ReportAllocs()
	for range b.N {
		w.Reset()
		s.EncodeFor(&w, p)
	}
}

func FuzzDecode262(f *testing.F) {
	for _, h := range vanilla262SetSlot {
		f.Add(slotBytes(f, h))
	}
	p := For(version.V776)
	f.Fuzz(func(t *testing.T, b []byte) {
		for _, delimited := range []bool{false, true} {
			var s Stack
			r := wire.NewReader(b)
			if delimited {
				s.DecodeUntrustedFor(r, p)
			} else {
				s.DecodeFor(r, p)
			}
			if r.Err != nil {
				continue
			}
			// What decoded encodes and decodes again, in both versions' forms.
			var w wire.Writer
			s.EncodeUntrustedFor(&w, p)
			var s2 Stack
			r2 := wire.NewReader(w.B)
			if s2.DecodeUntrustedFor(r2, p); r2.Err != nil {
				t.Fatalf("re-decode: %v (%x)", r2.Err, w.B)
			}
			w.Reset()
			s.EncodeUntrusted(&w)
			r2 = wire.NewReader(w.B)
			if s2.DecodeUntrusted(r2); r2.Err != nil {
				t.Fatalf("re-decode as 26.3: %v (%x)", r2.Err, w.B)
			}
		}
	})
}
