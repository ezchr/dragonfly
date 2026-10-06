package javasession

import (
	"encoding/hex"
	"image/color"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/item/potion"
	jitem "github.com/ezchr/go-mcjava/item"
	"github.com/ezchr/go-mcjava/version"
	"github.com/ezchr/go-mcjava/wire"
)

// Dragonfly stacks whose Java form must be byte-identical to what vanilla 26.3 sent for the /give
// command (go-mc java/item tests, tools/capture/items-vanilla).
var vanillaStacks = []struct {
	give string
	ds   item.Stack
	hex  string
}{
	{"stone 64", item.NewStack(block.Stone{}, 64), "40010000"},
	{"diamond_sword[enchantments={sharpness:5}]", item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1).WithEnchantments(item.NewEnchantment(enchantment.Sharpness, 5)), "019a0801000d012105"},
	{"diamond_sword[unbreakable={}]", item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1).AsUnbreakable(), "019a08010004"},
	{`diamond_sword[custom_name="Hello"]`, item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1).WithCustomName("Hello"), "019a0801000608000548656c6c6f"},
	{`diamond_sword[lore=["Line1","Line2"]]`, item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1).WithLore("Line1", "Line2"), "019a0801000b020800054c696e65310800054c696e6532"},
	{"enchanted_book[stored_enchantments={sharpness:3}]", item.NewStack(item.EnchantedBook{}, 1).WithEnchantments(item.NewEnchantment(enchantment.Sharpness, 3)), "01f30a01002d012103"},
	{"leather_helmet[dyed_color=16711680]", item.NewStack(item.Helmet{Tier: item.ArmourTierLeather{Colour: color.RGBA{R: 255, A: 255}}}, 1), "01ac0801002f00ff0000"},
	{`potion[potion_contents={potion:"minecraft:strong_healing"}]`, item.NewStack(item.Potion{Type: potion.StrongHealing()}, 1), "01f7090100350119000000"},
	{`tipped_arrow[potion_contents={potion:"minecraft:poison"}] 5`, item.NewStack(item.Arrow{Tip: potion.Poison()}, 5), "05a40b010035011c000000"},
}

func javaBytes(ds item.Stack) []byte {
	var js jitem.Stack
	javaStack(ds, &js)
	var w wire.Writer
	js.Encode(&w)
	return w.B
}

func TestJavaStackMatchesVanilla(t *testing.T) {
	for _, v := range vanillaStacks {
		if got := hex.EncodeToString(javaBytes(v.ds)); got != v.hex {
			t.Errorf("%s:\n got %s\nwant %s", v.give, got, v.hex)
		}
	}
}

// Java stacks a creative client sends back convert to the Dragonfly stack they came from.
func TestDragonflyStackRoundTrip(t *testing.T) {
	stacks := []item.Stack{
		item.NewStack(item.Diamond{}, 64),
		item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1).Damage(100).WithCustomName("§bBlade").
			WithLore("a", "b").WithEnchantments(item.NewEnchantment(enchantment.Sharpness, 5), item.NewEnchantment(enchantment.Unbreaking, 3)),
		item.NewStack(item.Pickaxe{Tier: item.ToolTierIron}, 1).Damage(10),
		item.NewStack(item.SplashPotion{Type: potion.LongSwiftness()}, 1),
		item.NewStack(item.Arrow{Tip: potion.Poison()}, 16),
		item.NewStack(item.EnchantedBook{}, 1).WithEnchantments(item.NewEnchantment(enchantment.Power, 4)),
		item.NewStack(item.Boots{Tier: item.ArmourTierLeather{Colour: color.RGBA{R: 1, G: 2, B: 3, A: 255}}}, 1),
	}
	for _, ds := range stacks {
		var js jitem.Stack
		javaStack(ds, &js)
		var w wire.Writer
		js.EncodeUntrusted(&w)
		var in jitem.Stack
		r := wire.NewReader(w.B)
		in.DecodeUntrusted(r)
		if r.Err != nil {
			t.Fatal(r.Err)
		}
		back, ok := dragonflyStack(&in)
		if !ok {
			t.Errorf("%v: not converted back", ds)
			continue
		}
		if !back.Equal(ds) {
			t.Errorf("round trip:\n got %v\nwant %v", back, ds)
		}
	}
}

func TestWitherPotion(t *testing.T) {
	var js jitem.Stack
	javaStack(item.NewStack(item.Potion{Type: potion.Wither()}, 1), &js)
	if js.PotionContents.HasPotion || len(js.PotionContents.Effects) != 1 || js.PotionContents.Effects[0].ID != jitem.EffectWither {
		t.Errorf("decay potion: %+v", js.PotionContents)
	}
}

func TestEnchantmentTable(t *testing.T) {
	tab := enchantments()
	for id, name := range bedrockEnchantments {
		if name != "" && tab.toJava[id] < 0 {
			t.Errorf("Bedrock enchantment %d (%s) has no Java id", id, name)
		}
	}
	for _, e := range item.Enchantments() {
		id, _ := item.EnchantmentID(e)
		if id >= len(tab.toJava) {
			t.Errorf("Dragonfly enchantment %d (%s) unmapped", id, e.Name())
		}
	}
}

func BenchmarkJavaStackPlain(b *testing.B) {
	ds := item.NewStack(item.Diamond{}, 64)
	var js jitem.Stack
	w := wire.Writer{B: make([]byte, 0, 256)}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		w.Reset()
		javaStack(ds, &js)
		js.Encode(&w)
	}
}

func BenchmarkJavaStackSword(b *testing.B) {
	ds := item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1).Damage(100).WithCustomName("§bBlade").
		WithLore("a", "b").WithEnchantments(item.NewEnchantment(enchantment.Sharpness, 5))
	var js jitem.Stack
	w := wire.Writer{B: make([]byte, 0, 256)}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		w.Reset()
		javaStack(ds, &js)
		js.Encode(&w)
	}
}

// A stack a creative client moves around keeps the Dragonfly data Java can't carry; a copy of a
// stack that is still in the inventory does not.
func TestCreativeKeepsValues(t *testing.T) {
	st := itemsTestState()
	ds := item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1).WithCustomName("Kit").WithValue("kit", "pvp")
	var js jitem.Stack
	javaStack(ds, &js)

	_ = st.inv.SetItem(0, ds)
	if got, src, ok := st.creativeStack(&js, 20); !ok || src != creativeCopy || got.CustomName() != "Kit" || len(got.Values()) != 0 {
		t.Errorf("copy of a stack in the inventory: %v (from %d)", got, src)
	}
	if got, src, ok := st.creativeStack(&js, mainToJava(0)); !ok || src != creativeSelf || got.Values()["kit"] != "pvp" {
		t.Errorf("same slot: %v (from %d)", got, src)
	}
	_ = st.inv.SetItem(0, item.Stack{})
	st.recent[len(st.recent)-1] = ds
	if got, src, ok := st.creativeStack(&js, 20); !ok || src != creativeMove || got.Values()["kit"] != "pvp" {
		t.Errorf("from recent: %v (from %d)", got, src)
	}
	// Taken: a second one is not another move.
	if got, src, ok := st.creativeStack(&js, 21); !ok || src != creativeNew || got.CustomName() != "Kit" || len(got.Values()) != 0 {
		t.Errorf("converted: %v (from %d)", got, src)
	}
}

// What vanilla 26.2 sent for the same /give commands (tools/capture/items-vanilla262).
var vanilla262Stacks = []string{
	"40010000", "01c40701000d012105", "01c407010004", "01c40701000608000548656c6c6f",
	"01c40701000b020800054c696e65310800054c696e6532", "01fa0901002a012103", "01d60701002c00ff0000",
	"01fe080100330119000000", "05ab0a010033011c000000",
}

// A 26.2 client gets the bytes vanilla 26.2 sends, and the stacks it sends back (creative) convert
// to the Dragonfly stacks they came from.
func TestJavaStack262(t *testing.T) {
	p := jitem.For(version.V776)
	for i, v := range vanillaStacks {
		var js jitem.Stack
		javaStack(v.ds, &js)
		var w wire.Writer
		js.EncodeFor(&w, p)
		if got := hex.EncodeToString(w.B); got != vanilla262Stacks[i] {
			t.Errorf("%s for 26.2:\n got %s\nwant %s", v.give, got, vanilla262Stacks[i])
		}
		w.Reset()
		js.EncodeUntrustedFor(&w, p)
		var back jitem.Stack
		r := wire.NewReader(w.B)
		back.DecodeUntrustedFor(r, p)
		if r.Err != nil {
			t.Fatalf("%s: %v", v.give, r.Err)
		}
		ds, ok := dragonflyStack(&back)
		if !ok || !ds.Equal(v.ds) || ds.Count() != v.ds.Count() {
			t.Errorf("%s: 26.2 creative round trip gave %v", v.give, ds)
		}
	}
}
