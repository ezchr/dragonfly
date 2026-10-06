package javasession

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/ezchr/dfjava/javamap"
	jitem "github.com/ezchr/go-mcjava/item"
	v777 "github.com/ezchr/go-mcjava/v777"
	"github.com/ezchr/go-mcjava/wire"
)

// update_recipes gives the client what it decides on its own: which items each furnace, smithing
// and brewing slot accepts (recipe property sets) and the stonecutter recipes (the client lists
// them; the button it sends is an index in that list). The recipe book is not sent.

// slotDisplayItem is the minecraft:item slot display (registry minecraft:slot_display).
const slotDisplayItem = 4

// sendRecipes sends update_recipes, with the item ids of the client's protocol.
func (s *Session) sendRecipes() {
	w := s.packet()
	if p := s.items().proto; p != nil {
		appendRecipes(w, p)
	} else {
		w.B = append(w.B, recipesPacket()...)
	}
	s.queue(v777.ClientboundPlayUpdateRecipes, w)
}

// propertySet is a recipe property set: the items a station slot accepts (newest Java ids).
type propertySet struct {
	key string
	ids []int32
}

var propertySets = onceRecipes(func() []propertySet {
	sets := []propertySet{
		{key: "minecraft:smithing_base"}, {key: "minecraft:smithing_template"}, {key: "minecraft:smithing_addition"},
		{key: "minecraft:furnace_input"}, {key: "minecraft:blast_furnace_input"}, {key: "minecraft:smoker_input"},
		{key: "minecraft:campfire_input"}, {key: "minecraft:brewing_input"}, {key: "minecraft:brewing_reagent"},
	}
	add := func(i int, it world.Item) {
		if id := javamap.Item(it); id > 0 && !containsID(sets[i].ids, id) {
			sets[i].ids = append(sets[i].ids, id)
		}
	}
	sm := smithing()
	for _, it := range world.Items() {
		name, _ := it.EncodeItem()
		if sm.base[name] {
			add(0, it)
		}
		if sm.template[name] {
			add(1, it)
		}
		if sm.addition[name] {
			add(2, it)
		}
		if s, ok := it.(item.Smeltable); ok && !s.SmeltInfo().Product.Empty() {
			info := s.SmeltInfo()
			add(3, it)
			if info.Ores {
				add(4, it)
			}
			if info.Food {
				add(5, it)
				add(6, it)
			}
		}
		switch it.(type) {
		case item.Potion, item.SplashPotion, item.LingeringPotion, item.GlassBottle:
			add(7, it)
		}
		if recipe.ValidBrewingReagent(it) {
			add(8, it)
		}
	}
	// The Java potion items are one id whatever the potion; make sure they are in.
	for _, n := range []string{"minecraft:potion", "minecraft:splash_potion", "minecraft:lingering_potion", "minecraft:glass_bottle"} {
		if id, ok := jitem.ByName(n); ok && !containsID(sets[7].ids, id) {
			sets[7].ids = append(sets[7].ids, id)
		}
	}
	return sets
})

// recipesPacket is the update_recipes body for clients of the newest protocol.
var recipesPacket = onceRecipes(func() []byte {
	var w wire.Writer
	appendRecipes(&w, nil)
	return w.B
})

// appendRecipes writes the update_recipes body with the item ids of protocol p (nil: the newest).
// Items an older protocol shows as one (a stand-in for an item it lacks) are listed once; the
// stonecutter list keeps every recipe, in the same order.
func appendRecipes(w *wire.Writer, p *jitem.Proto) {
	var buf [64]int32
	ids := func(in []int32) []int32 {
		if p == nil {
			return in
		}
		out := buf[:0]
		for _, id := range in {
			if o := p.Item(id); o >= 0 && !containsID(out, o) {
				out = append(out, o)
			}
		}
		return out
	}
	sets := propertySets()
	w.VarInt(int32(len(sets)))
	for _, s := range sets {
		w.String(s.key)
		in := ids(s.ids)
		w.VarInt(int32(len(in)))
		for _, id := range in {
			w.VarInt(id)
		}
	}
	list := stonecutterRecipes()
	w.VarInt(int32(len(list)))
	for _, r := range list {
		in := ids(r.in)
		w.VarInt(int32(len(in)) + 1) // holder set of items
		for _, id := range in {
			w.VarInt(id)
		}
		w.VarInt(slotDisplayItem)
		w.VarInt(p.Item(r.jo))
	}
}
