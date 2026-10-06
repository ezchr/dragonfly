package item

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ezchr/go-mc/java/wire"
)

// container_set_slot bodies a vanilla 26.3 server sent for /give commands (tools/capture/items-vanilla):
// container id, state id, slot (short), then the stack.
var vanillaSetSlot = []struct {
	give string
	hex  string
}{
	{"stone 64", "0003002440010000"},
	{"diamond_sword[damage=5]", "00040025019a0801000305"},
	{"diamond_sword[unbreakable={}]", "00050026019a08010004"},
	{`diamond_sword[custom_name="Hello"]`, "00060027019a0801000608000548656c6c6f"},
	{`diamond_sword[lore=["Line1","Line2"]]`, "00070028019a0801000b020800054c696e65310800054c696e6532"},
	{"diamond_sword[enchantments={sharpness:5}]", "00080029019a0801000d012105"},
	{`diamond_sword[custom_model_data={floats:[1.5f,2.0f],flags:[1b],strings:["abc"],colors:[255]}]`, "0009002a019a08010011023fc00000400000000101010361626301000000ff"},
	{"diamond_sword[enchantment_glint_override=true]", "000a002b019a0801001501"},
	{"enchanted_book[stored_enchantments={sharpness:3}]", "000b002c01f30a01002d012103"},
	{"leather_helmet[dyed_color=16711680]", "000c000901ac0801002f00ff0000"},
	{`potion[potion_contents={potion:"minecraft:strong_healing"}]`, "000d000a01f7090100350119000000"},
	{`potion[potion_contents={custom_color:255,custom_effects:[{id:"minecraft:speed",amplifier:1b,duration:200}],custom_name:"foo"}]`, "000e000b01f7090100350001000000ff010001c801000101000103666f6f"},
	{`tipped_arrow[potion_contents={potion:"minecraft:poison"}] 5`, "000f000c05a40b010035011c000000"},
	{`diamond_sword[damage=7,unbreakable={},custom_name="X",lore=["a"],enchantments={sharpness:5}]`, "0010000d019a08050003070406080001580b01080001610d012105"},
	{"diamond_sword[!attribute_modifiers]", "0011000e019a08000110"},
	{`diamond_sword[custom_data={a:1,b:"x"}]`, "0012000f019a080100000a03000161000000010800016200017800"},
	{"diamond_sword[max_damage=100,repair_cost=3]", "00130010019a08020002641303"},
	{`diamond_sword[custom_name={text:"Red",color:"red",italic:false}]`, "00140011019a080100060a080005636f6c6f7200037265640800047465787400035265640100066974616c69630000"},
	{"stick[max_stack_size=99] 70", "0015001246a40801000163"},
	{"diamond_sword[enchantments={sharpness:5,unbreaking:3}]", "00160013019a0801000d0228032105"},
	{`potion[potion_contents={custom_effects:[{id:"minecraft:speed",amplifier:1b,duration:200,ambient:1b,show_particles:0b,show_icon:1b}]}]`, "0017001401f7090100350000010001c8010100010000"},
}

func slotBytes(t testing.TB, h string) []byte {
	b, err := hex.DecodeString(h)
	if err != nil {
		t.Fatal(err)
	}
	r := wire.NewReader(b)
	r.VarInt()
	r.VarInt()
	r.Int16()
	return b[r.Off:]
}

// Every vanilla stack decodes and re-encodes to the same bytes.
func TestVanillaRoundTrip(t *testing.T) {
	var s Stack
	for _, v := range vanillaSetSlot {
		b := slotBytes(t, v.hex)
		r := wire.NewReader(b)
		s.Decode(r)
		if r.Err != nil || r.Len() != 0 {
			t.Fatalf("%s: decode: %v, %d bytes left", v.give, r.Err, r.Len())
		}
		var w wire.Writer
		s.Encode(&w)
		if !bytes.Equal(w.B, b) {
			t.Errorf("%s:\n got %x\nwant %x", v.give, w.B, b)
		}
		// The client-to-server form decodes to the same stack.
		var u wire.Writer
		s.EncodeUntrusted(&u)
		var s2 Stack
		r = wire.NewReader(u.B)
		s2.DecodeUntrusted(r)
		if r.Err != nil || r.Len() != 0 {
			t.Fatalf("%s: untrusted decode: %v", v.give, r.Err)
		}
		w.Reset()
		s2.Encode(&w)
		if !bytes.Equal(w.B, b) {
			t.Errorf("%s untrusted round trip:\n got %x\nwant %x", v.give, w.B, b)
		}
	}
}

func mustID(t testing.TB, name string) int32 {
	id, ok := ByName(name)
	if !ok {
		t.Fatalf("no item %s", name)
	}
	return id
}

// Stacks built field by field encode exactly as vanilla does.
func TestVanillaEncode(t *testing.T) {
	sword := mustID(t, "minecraft:diamond_sword")
	cases := map[string]func(s *Stack){
		"stone 64": func(s *Stack) { *s = Stack{Count: 64, ID: mustID(t, "minecraft:stone")} },
		"diamond_sword[damage=5]": func(s *Stack) {
			s.Damage = 5
			s.Add(CompDamage)
		},
		"diamond_sword[unbreakable={}]": func(s *Stack) { s.Add(CompUnbreakable) },
		`diamond_sword[custom_name="Hello"]`: func(s *Stack) {
			s.CustomName.Text = "Hello"
			s.Add(CompCustomName)
		},
		`diamond_sword[lore=["Line1","Line2"]]`: func(s *Stack) {
			s.Lore = []Text{{Text: "Line1"}, {Text: "Line2"}}
			s.Add(CompLore)
		},
		"diamond_sword[enchantments={sharpness:5}]": func(s *Stack) {
			s.Enchantments = []Enchantment{{33, 5}}
			s.Add(CompEnchantments)
		},
		`diamond_sword[custom_model_data={floats:[1.5f,2.0f],flags:[1b],strings:["abc"],colors:[255]}]`: func(s *Stack) {
			s.CustomModelData = CustomModelData{Floats: []float32{1.5, 2}, Flags: []bool{true}, Strings: []string{"abc"}, Colors: []int32{255}}
			s.Add(CompCustomModelData)
		},
		"diamond_sword[enchantment_glint_override=true]": func(s *Stack) {
			s.EnchantmentGlintOverride = true
			s.Add(CompEnchantmentGlintOverride)
		},
		"enchanted_book[stored_enchantments={sharpness:3}]": func(s *Stack) {
			s.ID = mustID(t, "minecraft:enchanted_book")
			s.StoredEnchantments = []Enchantment{{33, 3}}
			s.Add(CompStoredEnchantments)
		},
		"leather_helmet[dyed_color=16711680]": func(s *Stack) {
			s.ID = mustID(t, "minecraft:leather_helmet")
			s.DyedColor = 0xff0000
			s.Add(CompDyedColor)
		},
		`potion[potion_contents={potion:"minecraft:strong_healing"}]`: func(s *Stack) {
			s.ID = mustID(t, "minecraft:potion")
			s.PotionContents = PotionContents{HasPotion: true, Potion: PotionStrongHealing}
			s.Add(CompPotionContents)
		},
		`potion[potion_contents={custom_color:255,custom_effects:[{id:"minecraft:speed",amplifier:1b,duration:200}],custom_name:"foo"}]`: func(s *Stack) {
			s.ID = mustID(t, "minecraft:potion")
			s.PotionContents = PotionContents{HasCustomColor: true, CustomColor: 255, HasCustomName: true, CustomName: "foo",
				Effects: []Effect{{ID: EffectSpeed, Amplifier: 1, Duration: 200, ShowParticles: true, ShowIcon: true}}}
			s.Add(CompPotionContents)
		},
		`tipped_arrow[potion_contents={potion:"minecraft:poison"}] 5`: func(s *Stack) {
			s.ID, s.Count = mustID(t, "minecraft:tipped_arrow"), 5
			s.PotionContents = PotionContents{HasPotion: true, Potion: PotionPoison}
			s.Add(CompPotionContents)
		},
		`diamond_sword[damage=7,unbreakable={},custom_name="X",lore=["a"],enchantments={sharpness:5}]`: func(s *Stack) {
			s.Damage, s.CustomName.Text, s.Lore, s.Enchantments = 7, "X", []Text{{Text: "a"}}, []Enchantment{{33, 5}}
			for _, c := range []int32{CompEnchantments, CompLore, CompCustomName, CompUnbreakable, CompDamage} {
				s.Add(c)
			}
		},
		"diamond_sword[!attribute_modifiers]": func(s *Stack) { s.Remove(CompAttributeModifiers) },
		"diamond_sword[max_damage=100,repair_cost=3]": func(s *Stack) {
			s.MaxDamage, s.RepairCost = 100, 3
			s.Add(CompRepairCost)
			s.Add(CompMaxDamage)
		},
		"stick[max_stack_size=99] 70": func(s *Stack) {
			s.ID, s.Count, s.MaxStackSize = mustID(t, "minecraft:stick"), 70, 99
			s.Add(CompMaxStackSize)
		},
	}
	for _, v := range vanillaSetSlot {
		f, ok := cases[v.give]
		if !ok {
			continue
		}
		s := Stack{Count: 1, ID: sword}
		f(&s)
		var w wire.Writer
		s.Encode(&w)
		if want := slotBytes(t, v.hex); !bytes.Equal(w.B, want) {
			t.Errorf("%s:\n got %x\nwant %x", v.give, w.B, want)
		}
	}
}

func TestDecodedFields(t *testing.T) {
	var s Stack
	for _, v := range vanillaSetSlot {
		s.Decode(wire.NewReader(slotBytes(t, v.hex)))
		switch v.give {
		case `diamond_sword[custom_name={text:"Red",color:"red",italic:false}]`:
			if s.CustomName.Text != "Red" || s.CustomName.NBT == nil {
				t.Errorf("styled name: %+v", s.CustomName)
			}
		case `diamond_sword[custom_data={a:1,b:"x"}]`:
			if !s.Has(CompCustomData) || len(s.CustomData) != 17 {
				t.Errorf("custom data: %x", s.CustomData)
			}
		case "diamond_sword[enchantments={sharpness:5,unbreaking:3}]":
			if len(s.Enchantments) != 2 || s.Enchantments[1] != (Enchantment{33, 5}) {
				t.Errorf("enchantments: %v", s.Enchantments)
			}
		}
	}
}

func TestText(t *testing.T) {
	for _, str := range []string{"", "plain", "§r§bColoured", "nul\x00byte", "emoji 😀 é"} {
		var w wire.Writer
		writeText(&w, &Text{Text: str})
		var got Text
		r := wire.NewReader(w.B)
		readText(r, &got)
		if r.Err != nil || got.Text != str {
			t.Errorf("%q: got %q (%v)", str, got.Text, r.Err)
		}
	}
}

func TestHostile(t *testing.T) {
	for _, h := range []string{
		"01", "019a08", "019a08ffffffff0f00", "019a08017f", "019a0801000b05", "019a080100060aff",
		"019a080100000a090001610a7fffffff", "01ffffffff0f0000", "019a0801001000",
	} {
		b, _ := hex.DecodeString(h)
		var s Stack
		r := wire.NewReader(b)
		s.Decode(r)
		if r.Err == nil {
			t.Errorf("%s: no error", h)
		}
	}
}

func FuzzDecode(f *testing.F) {
	for _, v := range vanillaSetSlot {
		f.Add(slotBytes(f, v.hex))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		var s Stack
		s.Decode(wire.NewReader(b))
		s.DecodeUntrusted(wire.NewReader(b))
	})
}

func BenchmarkEncodePlain(b *testing.B) {
	s := Stack{Count: 64, ID: 1}
	w := wire.Writer{B: make([]byte, 0, 256)}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		w.Reset()
		s.Encode(&w)
	}
}

func BenchmarkEncodeEnchantedSword(b *testing.B) {
	s := Stack{Count: 1, ID: 1050, Damage: 7, CustomName: Text{Text: "§bBlade"}, Lore: []Text{{Text: "line one"}, {Text: "line two"}},
		Enchantments: []Enchantment{{33, 5}, {40, 3}}}
	for _, c := range []int32{CompDamage, CompUnbreakable, CompCustomName, CompLore, CompEnchantments} {
		s.Add(c)
	}
	w := wire.Writer{B: make([]byte, 0, 256)}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		w.Reset()
		s.Encode(&w)
	}
}

func BenchmarkEncodePlayerInventory(b *testing.B) {
	var inv [46]Stack
	for i := 9; i < 45; i += 2 {
		inv[i] = Stack{Count: 64, ID: int32(i)}
	}
	inv[36] = Stack{Count: 1, ID: 1050, Damage: 3, Enchantments: []Enchantment{{33, 5}}}
	inv[36].Add(CompDamage)
	inv[36].Add(CompEnchantments)
	w := wire.Writer{B: make([]byte, 0, 1024)}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		w.Reset()
		for i := range inv {
			inv[i].Encode(&w)
		}
	}
}

func BenchmarkDecodeEnchantedSword(b *testing.B) {
	data := slotBytes(b, vanillaSetSlot[13].hex)
	var s Stack
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		s.Decode(wire.NewReader(data))
	}
}

// nbtTag writes a named tag of a compound.
func nbtTag(b []byte, typ byte, name string) []byte {
	b = append(b, typ, byte(len(name)>>8), byte(len(name)))
	return append(b, name...)
}

func nbtStr(b []byte, s string) []byte {
	b = append(b, byte(len(s)>>8), byte(len(s)))
	return append(b, s...)
}

// textTree is a component nested depth levels deep through "extra", with junk bytes of padding in
// the innermost one. Odd levels put their text before "extra", even levels after it.
func textTree(depth, junk int) []byte {
	b := []byte{nbtCompound}
	text := func(level int) { b = nbtStr(nbtTag(b, nbtString, "text"), fmt.Sprint(level, ";")) }
	for level := 0; level < depth; level++ {
		if level%2 == 1 {
			text(level)
		}
		b = nbtTag(b, nbtList, "extra")
		b = append(b, nbtCompound, 0, 0, 0, 1)
	}
	if depth%2 == 1 {
		text(depth)
	}
	b = nbtTag(b, nbtList, "junk")
	b = append(b, nbtByte, byte(junk>>24), byte(junk>>16), byte(junk>>8), byte(junk))
	b = append(b, make([]byte, junk)...)
	for level := depth; level >= 0; level-- {
		if level%2 == 0 {
			text(level)
		}
		b = append(b, nbtEnd)
	}
	return b
}

func TestPlainTextOrder(t *testing.T) {
	var want strings.Builder
	for i := range 6 {
		fmt.Fprint(&want, i, ";")
	}
	if got := PlainText(textTree(5, 3)); got != want.String() {
		t.Errorf("got %q, want %q", got, want.String())
	}
	// A string, a list of strings and an empty compound.
	if got := PlainText(nbtStr([]byte{nbtString}, "plain")); got != "plain" {
		t.Errorf("string: %q", got)
	}
	list := append([]byte{nbtList, nbtString, 0, 0, 0, 2}, nbtStr(nbtStr(nil, "a"), "b")...)
	if got := PlainText(list); got != "ab" {
		t.Errorf("list: %q", got)
	}
	if got := PlainText([]byte{nbtCompound, nbtEnd}); got != "" {
		t.Errorf("empty: %q", got)
	}
}

// A 2 MB component nested 250 levels deep used to cost seconds of CPU (every level skipped its
// extra, then parsed it again): it is one pass now, so it costs about what a flat one does.
func TestPlainTextLinear(t *testing.T) {
	timed := func(b []byte) (string, time.Duration) {
		start := time.Now()
		s := PlainText(b)
		return s, time.Since(start)
	}
	_, flat := timed(textTree(1, 2<<20))
	deep := textTree(250, 2<<20)
	got, d := timed(deep)
	if !strings.HasPrefix(got, "0;1;2;") || !strings.HasSuffix(got, "250;") {
		t.Errorf("text %.40q...", got)
	}
	if d > 10*flat+20*time.Millisecond {
		t.Errorf("PlainText of a 2 MB component: %v 250 deep, %v flat", d, flat)
	}
	// The same through a creative stack's custom name.
	var w wire.Writer
	w.VarInt(1)
	w.VarInt(1) // stone
	w.VarInt(1)
	w.VarInt(0)
	w.VarInt(CompCustomName)
	w.VarInt(int32(len(deep)))
	w.B = append(w.B, deep...)
	var s Stack
	r := wire.NewReader(w.B)
	start := time.Now()
	s.DecodeUntrusted(r)
	if r.Err != nil || !strings.HasPrefix(s.CustomName.Text, "0;1;") {
		t.Fatalf("decode: %v %.20q", r.Err, s.CustomName.Text)
	}
	if d := time.Since(start); d > 20*flat+40*time.Millisecond {
		t.Errorf("decoding took %v (flat PlainText %v)", d, flat)
	}
}
