package javasession

import (
	"log/slog"
	"os"
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
	jitem "github.com/ezchr/go-mcjava/item"
	v776 "github.com/ezchr/go-mcjava/v776"
	"github.com/ezchr/go-mcjava/version"
	"github.com/ezchr/go-mcjava/wire"
)

// Dragonfly registers its recipes when a server is made.
var testServer = sync.OnceFunc(func() {
	dir, _ := os.MkdirTemp("", "dfj-recipes")
	uc := server.DefaultConfig()
	uc.World.Folder = dir
	uc.Players.SaveData = false
	conf, err := uc.Config(slog.New(slog.DiscardHandler))
	if err != nil {
		panic(err)
	}
	conf.Listeners = nil
	conf.New()
})

func stack(it world.Item, n int) item.Stack { return item.NewStack(it, n) }

func TestCraftMatch(t *testing.T) {
	testServer()
	ix := recipes()
	t.Logf("%d crafting table recipes indexed, %d stonecutter recipes", ix.count, len(stonecutterRecipes()))
	var (
		e      item.Stack
		oak    = stack(block.Planks{Wood: block.OakWood()}, 1)
		birch  = stack(block.Planks{Wood: block.BirchWood()}, 1)
		log    = stack(block.Log{Wood: block.OakWood()}, 1)
		stick  = stack(item.Stick{}, 1)
		cobble = stack(block.Cobblestone{}, 1)
		milk   = stack(item.Bucket{Content: item.MilkBucketContent()}, 1)
		sugar  = stack(item.Sugar{}, 1)
		egg    = stack(item.Egg{}, 1)
		wheat  = stack(item.Wheat{}, 1)
		iron   = stack(item.IronIngot{}, 1)
		flint  = stack(item.Flint{}, 1)
	)
	cases := []struct {
		name  string
		width int
		grid  []item.Stack
		want  string // EncodeItem name, "" for no result
		count int
	}{
		{"planks 2x2", 2, []item.Stack{e, e, e, log}, "minecraft:oak_planks", 4},
		{"sticks", 2, []item.Stack{oak, e, oak, e}, "minecraft:stick", 4},
		{"sticks mixed planks (tag)", 3, []item.Stack{e, e, e, e, oak, e, e, birch, e}, "minecraft:stick", 4},
		{"crafting table", 2, []item.Stack{oak, oak, oak, oak}, "minecraft:crafting_table", 1},
		{"wooden pickaxe", 3, []item.Stack{oak, oak, oak, e, stick, e, e, stick, e}, "minecraft:wooden_pickaxe", 1},
		{"upside-down pickaxe is nothing", 3, []item.Stack{e, stick, e, e, stick, e, oak, oak, oak}, "", 0},
		{"furnace", 3, []item.Stack{cobble, cobble, cobble, cobble, e, cobble, cobble, cobble, cobble}, "minecraft:furnace", 1},
		{"cake", 3, []item.Stack{milk, milk, milk, sugar, egg, sugar, wheat, wheat, wheat}, "minecraft:cake", 1},
		{"stairs", 3, []item.Stack{oak, e, e, oak, oak, e, oak, oak, oak}, "minecraft:oak_stairs", 4},
		{"stairs mirrored", 3, []item.Stack{e, e, oak, e, oak, oak, oak, oak, oak}, "minecraft:oak_stairs", 4},
		{"bread in the bottom row", 3, []item.Stack{e, e, e, e, e, e, wheat, wheat, wheat}, "minecraft:bread", 1},
		{"flint and steel (shapeless) anywhere", 3, []item.Stack{e, e, flint, e, e, e, iron, e, e}, "minecraft:flint_and_steel", 1},
		{"chest from mixed planks", 3, []item.Stack{oak, birch, oak, birch, e, oak, oak, oak, birch}, "minecraft:chest", 1},
		{"pickaxe does not fit 2x2", 2, []item.Stack{oak, oak, stick, e}, "", 0},
	}
	for _, c := range cases {
		cm, ok := ix.match(c.width, c.grid)
		got, n := "", 0
		if ok {
			got, _ = cm.output.Item().EncodeItem()
			n = cm.output.Count()
		}
		if got != c.want || n != c.count {
			t.Errorf("%s: got %q x%d, want %q x%d", c.name, got, n, c.want, c.count)
		}
	}
	if r := craftRemainder(milk); itemName(r) != "minecraft:bucket" {
		t.Errorf("milk bucket remainder %v", r)
	}
}

func TestStonecutterList(t *testing.T) {
	testServer()
	list := stonecutterFor(stack(block.Stone{}, 1), nil)
	if len(list) == 0 {
		t.Fatal("no stonecutter recipes for stone")
	}
	for _, r := range list {
		t.Logf("stone -> %s x%d", itemName(r.out), r.out.Count())
	}
	if len(recipesPacket()) < 100 {
		t.Errorf("update_recipes is %d bytes", len(recipesPacket()))
	}
}

func TestAnvilAndGrindstone(t *testing.T) {
	testServer()
	pick := stack(item.Pickaxe{Tier: item.ToolTierIron}, 1).Damage(200)
	res, cost, n := anvilResult(pick, stack(item.IronIngot{}, 8), nil, false, nil)
	if res.Empty() || n == 0 || cost == 0 || res.Durability() <= pick.Durability() {
		t.Errorf("repair with ingots: %v cost %d uses %d", res, cost, n)
	}
	name := "Iron Pickaxe"
	if res, _, _ := anvilResult(pick, item.Stack{}, &name, false, nil); !res.Empty() {
		t.Errorf("the default name is not a rename: %v", res)
	}
	name = "Digger"
	if res, cost, _ := anvilResult(pick, item.Stack{}, &name, false, nil); res.CustomName() != "Digger" || cost != 1 {
		t.Errorf("rename: %v cost %d", res, cost)
	}
	if g := grindResult(pick, item.Stack{}); !g.Empty() {
		t.Errorf("grindstone gave %v for one unenchanted pickaxe (vanilla: nothing)", g)
	}
	if g := grindResult(pick.WithEnchantments(item.NewEnchantment(enchantment.Efficiency, 2)), item.Stack{}); g.Empty() || len(g.Enchantments()) != 0 {
		t.Errorf("grindstone gave %v for one enchanted pickaxe", g)
	}
}

// update_recipes for a 26.2 client: every item id is one 26.2 has, the stonecutter list keeps every
// recipe in the same order, and the layout reads to the end.
func TestRecipes262(t *testing.T) {
	testServer()
	p := jitem.For(version.V776)
	var w wire.Writer
	appendRecipes(&w, p)
	maxItem := int32(len(v776.Builtin["minecraft:item"]))
	r := wire.NewReader(w.B)
	readIDs := func(n int32) {
		for range n {
			if id := r.VarInt(); id < 0 || id >= maxItem {
				t.Fatalf("item id %d is not a 26.2 item", id)
			}
		}
	}
	sets := r.VarInt()
	for range sets {
		r.String(256)
		readIDs(r.VarInt())
	}
	n := r.VarInt()
	if int(n) != len(stonecutterRecipes()) {
		t.Fatalf("%d stonecutter recipes for 26.2, %d for 26.3", n, len(stonecutterRecipes()))
	}
	for range n {
		readIDs(r.VarInt() - 1)
		if d := r.VarInt(); d != slotDisplayItem {
			t.Fatalf("slot display %d", d)
		}
		readIDs(1)
	}
	if r.Err != nil || r.Len() != 0 {
		t.Fatalf("26.2 update_recipes: %v, %d bytes left", r.Err, r.Len())
	}
	// The newest version's packet is unchanged by the version code.
	w.Reset()
	appendRecipes(&w, nil)
	if string(w.B) != string(recipesPacket()) {
		t.Error("26.3 update_recipes differs from the cached one")
	}
	// A 26.2 client's stonecutter list for stone is the 26.3 one.
	if a, b := stonecutterFor(stack(block.Stone{}, 1), p), stonecutterFor(stack(block.Stone{}, 1), nil); len(a) != len(b) {
		t.Errorf("stone: %d stonecutter recipes for 26.2, %d for 26.3", len(a), len(b))
	}
}
