package javamap

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	_ "github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"
	v777 "github.com/ezchr/go-mcjava/v777"
)

func TestMain(m *testing.M) {
	// the server does this at startup (world.Config.New); registry lookups panic before it
	world.DefaultBlockRegistry.Finalize()
	os.Exit(m.Run())
}

// refsDir holds the Mojang reports the spot checks compare against (JAVAMAP_REFS overrides).
func refsDir() string {
	if d := os.Getenv("JAVAMAP_REFS"); d != "" {
		return d
	}
	return "../../refs"
}

type javaReport struct {
	byID   []string                  // "minecraft:block[k=v,...]"
	byName map[string]map[string]int // block -> sorted prop string -> id
}

var javaBlocks = func() func(t testing.TB) *javaReport {
	var rep *javaReport
	return func(t testing.TB) *javaReport {
		if rep != nil {
			return rep
		}
		raw, err := os.ReadFile(filepath.Join(refsDir(), "mojang-26.3/generated/reports/blocks.json"))
		if err != nil {
			t.Skipf("Mojang reports not available: %v", err)
		}
		var m map[string]struct {
			States []struct {
				ID         int               `json:"id"`
				Properties map[string]string `json:"properties"`
			} `json:"states"`
		}
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		rep = &javaReport{byName: map[string]map[string]int{}}
		for name, b := range m {
			rep.byName[name] = map[string]int{}
			for _, s := range b.States {
				for len(rep.byID) <= s.ID {
					rep.byID = append(rep.byID, "")
				}
				ps := propString(s.Properties)
				rep.byID[s.ID] = name + ps
				rep.byName[name][ps] = s.ID
			}
		}
		return rep
	}
}()

func propString(p map[string]string) string {
	if len(p) == 0 {
		return ""
	}
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + p[k]
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func TestEveryBlockMapsToValidJavaState(t *testing.T) {
	reg := world.DefaultBlockRegistry
	table, misses := BlockStatesWithMisses(reg)
	if len(table) != reg.BlockCount() {
		t.Fatalf("table has %d entries, registry %d", len(table), reg.BlockCount())
	}
	for rid, java := range table {
		if java < 0 || java >= JavaStateCount {
			name, props, _ := reg.RuntimeIDToState(uint32(rid))
			t.Errorf("rid %d (%s %v) -> invalid java state %d", rid, name, props, java)
		}
	}
	for _, rid := range misses {
		name, props, _ := reg.RuntimeIDToState(rid)
		t.Errorf("rid %d (%s %v) is not in the generated table; rerun go generate", rid, name, props)
	}
	if table[reg.AirRuntimeID()] != JavaAir {
		t.Errorf("air -> %d, want %d", table[reg.AirRuntimeID()], JavaAir)
	}
	if got := DefaultBlockStates(); !slices.Equal(got, table) {
		t.Errorf("DefaultBlockStates differs from BlockStates(DefaultBlockRegistry)")
	}
}

func TestBlockSpotChecks(t *testing.T) {
	rep := javaBlocks(t)
	table := DefaultBlockStates()
	oak := block.Planks{Wood: block.OakWood()}
	cases := []struct {
		b    world.Block
		want string
	}{
		{block.Air{}, "minecraft:air"},
		{block.Stone{}, "minecraft:stone"},
		{block.Stone{Smooth: true}, "minecraft:smooth_stone"},
		{block.Grass{}, "minecraft:grass_block[snowy=false]"},
		{block.Log{Wood: block.OakWood(), Axis: cube.X}, "minecraft:oak_log[axis=x]"},
		{block.Log{Wood: block.OakWood(), Axis: cube.Y}, "minecraft:oak_log[axis=y]"},
		{block.Log{Wood: block.OakWood(), Axis: cube.Z}, "minecraft:oak_log[axis=z]"},
		{block.Log{Wood: block.SpruceWood(), Stripped: true, Axis: cube.Z}, "minecraft:stripped_spruce_log[axis=z]"},
		{block.Water{Still: true, Depth: 8}, "minecraft:water[level=0]"},
		{block.Water{Depth: 7}, "minecraft:water[level=1]"},
		{block.Water{Depth: 1}, "minecraft:water[level=7]"},
		{block.Water{Depth: 8, Falling: true}, "minecraft:water[level=8]"},
		{block.Lava{Still: true, Depth: 8}, "minecraft:lava[level=0]"},
		{block.Stairs{Block: oak, Facing: cube.East}, "minecraft:oak_stairs[facing=east,half=bottom,shape=straight,waterlogged=false]"},
		{block.Stairs{Block: oak, Facing: cube.North, UpsideDown: true}, "minecraft:oak_stairs[facing=north,half=top,shape=straight,waterlogged=false]"},
		{block.Stairs{Block: block.Stone{}, Facing: cube.South}, "minecraft:stone_stairs[facing=south,half=bottom,shape=straight,waterlogged=false]"},
		{block.Stairs{Block: oak, Facing: cube.East, Corner: block.InnerLeftStairsCorner()}, "minecraft:oak_stairs[facing=east,half=bottom,shape=inner_left,waterlogged=false]"},
		{block.Chest{Facing: cube.North}, "minecraft:chest[facing=north,type=single,waterlogged=false]"},
		{block.Chest{Facing: cube.West}, "minecraft:chest[facing=west,type=single,waterlogged=false]"},
		{block.Planks{Wood: block.CherryWood()}, "minecraft:cherry_planks"},
		{block.Wool{Colour: item.ColourRed()}, "minecraft:red_wool"},
		{block.WoodFence{Wood: block.OakWood()}, "minecraft:oak_fence[east=false,north=false,south=false,waterlogged=false,west=false]"},
	}
	for _, c := range cases {
		rid := world.BlockRuntimeID(c.b)
		got := rep.byID[table[rid]]
		if got != c.want {
			name, props := c.b.EncodeBlock()
			t.Errorf("%s %v -> %s, want %s", name, props, got, c.want)
		}
	}
}

func TestWaterlogged(t *testing.T) {
	rep := javaBlocks(t)
	slab := rep.byName["minecraft:oak_slab"]
	dry, wet := int32(slab["[type=bottom,waterlogged=false]"]), int32(slab["[type=bottom,waterlogged=true]"])
	if got := Waterlogged(dry); got != wet {
		t.Errorf("Waterlogged(oak_slab bottom) = %s, want %s", rep.byID[got], rep.byID[wet])
	}
	if got := Waterlogged(wet); got != wet {
		t.Errorf("Waterlogged of a waterlogged state changed it to %s", rep.byID[got])
	}
	if !Waterloggable(dry) || Waterloggable(JavaStone) {
		t.Errorf("Waterloggable wrong")
	}
	if Waterlogged(JavaStone) != JavaStone {
		t.Errorf("stone got waterlogged")
	}
	// every waterloggable Java state: Waterlogged gives the same state with waterlogged=true
	for id, s := range rep.byID {
		if !strings.Contains(s, "waterlogged=false") {
			continue
		}
		if got := rep.byID[Waterlogged(int32(id))]; got != strings.Replace(s, "waterlogged=false", "waterlogged=true", 1) {
			t.Fatalf("Waterlogged(%s) = %s", s, got)
		}
	}
}

func TestBiomes(t *testing.T) {
	reg := v777.Registries["minecraft:worldgen/biome"]
	cases := []struct {
		b    world.Biome
		want string
	}{
		{biome.Plains{}, "minecraft:plains"},
		{biome.Desert{}, "minecraft:desert"},
		{biome.NetherWastes{}, "minecraft:nether_wastes"},
		{biome.End{}, "minecraft:the_end"},
		{biome.River{}, "minecraft:river"},
		{biome.CherryGrove{}, "minecraft:cherry_grove"},
	}
	for _, c := range cases {
		if got := reg[Biome(c.b)]; got != c.want {
			t.Errorf("%s -> %s, want %s", c.b.String(), got, c.want)
		}
	}
	for _, b := range world.Biomes() {
		if id := Biome(b); id < 0 || int(id) >= len(reg) {
			t.Errorf("%s -> invalid java biome %d", b.String(), id)
		}
	}
	if reg[BiomeID(100000)] != "minecraft:plains" {
		t.Errorf("unknown biome does not fall back to plains")
	}
}

func TestItems(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(refsDir(), "mojang-26.3/generated/reports/registries.json"))
	if err != nil {
		t.Skipf("Mojang reports not available: %v", err)
	}
	var reg map[string]struct {
		Entries map[string]struct {
			ID int32 `json:"protocol_id"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(raw, &reg); err != nil {
		t.Fatal(err)
	}
	names := map[int32]string{}
	for k, v := range reg["minecraft:item"].Entries {
		names[v.ID] = k
	}
	cases := []struct {
		it   world.Item
		want string
	}{
		{item.Sword{Tier: item.ToolTierDiamond}, "minecraft:diamond_sword"},
		{block.Planks{Wood: block.OakWood()}, "minecraft:oak_planks"},
		{block.Planks{Wood: block.BirchWood()}, "minecraft:birch_planks"},
		{item.Potion{Type: potion.Swiftness()}, "minecraft:potion"},
		{item.Potion{Type: potion.Water()}, "minecraft:potion"},
		{item.SplashPotion{Type: potion.Healing()}, "minecraft:splash_potion"},
		{block.Banner{Colour: item.ColourWhite()}, "minecraft:white_banner"},
		{block.Banner{Colour: item.ColourBlack()}, "minecraft:black_banner"},
		{block.Bed{Colour: item.ColourLime()}, "minecraft:lime_bed"},
		{block.Wool{Colour: item.ColourRed()}, "minecraft:red_wool"},
		{item.Dye{Colour: item.ColourBlue()}, "minecraft:blue_dye"},
		{item.Arrow{}, "minecraft:arrow"},
		{block.Stone{}, "minecraft:stone"},
	}
	for _, c := range cases {
		name, meta := c.it.EncodeItem()
		id, ok := ItemID(name, meta)
		if !ok || names[id] != c.want {
			t.Errorf("%s:%d -> %s (ok=%v), want %s", name, meta, names[id], ok, c.want)
		}
	}
	for _, it := range world.Items() {
		if id := Item(it); id < 0 || id >= JavaItemCount {
			name, meta := it.EncodeItem()
			t.Errorf("%s:%d -> invalid java item %d", name, meta, id)
		}
	}
	if id, ok := ItemID("zid:nothing", 0); ok || id != JavaItemBarrier {
		t.Errorf("unknown item: %d %v", id, ok)
	}
}

func TestNeighbourDependent(t *testing.T) {
	// Bedrock 1.26.50 carries fence/pane/wall connections, stair corners and tripwire connections, and
	// Dragonfly computes them, so those map exactly and must not be listed.
	want := map[string]DependencySource{
		"minecraft:redstone_wire": SourceNeighbours,
		"minecraft:chest":         SourceNeighbours,
		"minecraft:grass_block":   SourceNeighbours,
		"minecraft:fire":          SourceNeighbours,
		"minecraft:note_block":    SourceBlockEntity,
		"minecraft:bed":           SourceBlockEntity,
		"minecraft:flower_pot":    SourceBlockEntity,
		"minecraft:oak_door":      SourceRedstone,
		"minecraft:oak_leaves":    SourceNone,
	}
	notWant := []string{"minecraft:oak_fence", "minecraft:oak_stairs", "minecraft:glass_pane", "minecraft:cobblestone_wall", "minecraft:tripwire"}
	found := map[string]bool{}
	for _, d := range NeighbourDependent() {
		if d.Source == SourceUnknown {
			t.Errorf("%s %v has no source", d.Block, d.Properties)
		}
		if src, ok := want[d.Block]; ok && src == d.Source {
			found[d.Block] = true
		}
		if slices.Contains(notWant, d.Block) {
			t.Errorf("%s %v listed, but Bedrock 1.26.50 carries it", d.Block, d.Properties)
		}
	}
	for b, src := range want {
		if !found[b] {
			t.Errorf("%s (%v) missing from NeighbourDependent", b, src)
		}
	}
}

func BenchmarkBlockStates(b *testing.B) {
	reg := world.DefaultBlockRegistry
	b.ReportAllocs()
	for b.Loop() {
		BlockStates(reg)
	}
}

func BenchmarkTableLookup(b *testing.B) {
	table := DefaultBlockStates()
	rids := make([]uint32, 4096)
	for i := range rids {
		rids[i] = uint32(i*7919) % uint32(len(table))
	}
	var sink int32
	for b.Loop() {
		for _, rid := range rids {
			sink += table[rid]
		}
	}
	_ = sink
}
