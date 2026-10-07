package javasession

import (
	"fmt"
	"sync"

	"github.com/df-mc/dragonfly/server/java/javamap"
	jitem "github.com/df-mc/dragonfly/server/java/protocol/item"
	"github.com/df-mc/dragonfly/server/world"
)

// Custom (add-on) items and blocks for Java clients. Dragonfly's custom items and blocks have no
// Java equivalent, so the server says how Java players see them: an item as a vanilla base item
// carrying a minecraft:item_model that the server's Java resource pack provides, a block as a
// vanilla block state the pack re-skins (a powered note block). Paper ports of add-ons use the
// same scheme (the Ores Plus Paper plugin does), so one Java pack serves both servers. Register
// everything before Run.

// CustomItem is how Java clients see a custom item.
type CustomItem struct {
	// Base is the vanilla Java item the stack is built on, e.g. "minecraft:diamond_sword": its
	// behaviour on the client (attack cooldown, stack size, armour slot) comes from it.
	Base string
	// Model is the minecraft:item_model of the stack, e.g. "oresplus:ruby_sword": the resource
	// pack's assets/oresplus/items/ruby_sword.json.
	Model string
	// Name is the item name shown (the item_name component); "" keeps the base item's.
	Name string
	// Slot and Asset make the item wearable with its own look: the equipment slot
	// (jitem.EquipSlot*) and the pack's equipment asset (e.g. "oresplus:ruby"). Asset "" with a
	// slot wears it without one, so the item model itself shows (3D helmets).
	Slot  int32
	Asset string
	Wear  bool
}

type customItemJava struct {
	id          int32
	model, name string
	equip       *jitem.Equippable
}

var (
	customMu      sync.RWMutex
	customItems   = map[string]customItemJava{} // Bedrock item name -> how Java sees it
	customByModel = map[string]string{}         // item model -> Bedrock item name
	customBlocks  = map[string]int32{}          // Bedrock block name -> Java 26.3 block state
)

// RegisterCustomItem says how Java clients see the custom item with the Bedrock name name.
func RegisterCustomItem(name string, it CustomItem) error {
	id, ok := javamap.ItemID(it.Base, 0)
	if !ok {
		return fmt.Errorf("custom item %s: unknown Java base item %q", name, it.Base)
	}
	customMu.Lock()
	defer customMu.Unlock()
	c := customItemJava{id: id, model: it.Model, name: it.Name}
	if it.Wear {
		c.equip = &jitem.Equippable{Slot: it.Slot, EquipSound: "minecraft:item.armor.equip_diamond", Model: it.Asset,
			Dispensable: true, Swappable: true, DamageOnHurt: true}
	}
	customItems[name] = c
	if it.Model != "" {
		customByModel[it.Model] = name
	}
	return nil
}

// RegisterCustomBlock says which Java block state (a 26.3 state id, see NoteBlockState) Java
// clients see for every state of the custom block with the Bedrock name name.
func RegisterCustomBlock(name string, javaState int32) {
	customMu.Lock()
	defer customMu.Unlock()
	customBlocks[name] = javaState
}

// noteBlockBase is the 26.3 state id of minecraft:note_block[instrument=harp,note=0,powered=true].
// The states run instrument, then note, then powered (true first): checked against Mojang's
// generated blocks report.
const noteBlockBase = 680

// NoteBlockInstruments lists the note block instruments in Java's order.
var NoteBlockInstruments = []string{"harp", "basedrum", "snare", "hat", "bass", "flute", "bell", "guitar",
	"chime", "xylophone", "iron_xylophone", "cow_bell", "didgeridoo", "bit", "banjo", "pling", "zombie",
	"skeleton", "creeper", "dragon", "wither_skeleton", "piglin", "custom_head"}

// NoteBlockState is the 26.3 state id of a powered note block with an instrument (an index into
// NoteBlockInstruments) and note (0-24).
func NoteBlockState(instrument, note int) int32 {
	return noteBlockBase + int32(instrument)*50 + int32(note)*2
}

// customItemFor reports how Java sees a custom item, by Bedrock name.
func customItemFor(name string) (customItemJava, bool) {
	customMu.RLock()
	defer customMu.RUnlock()
	c, ok := customItems[name]
	return c, ok
}

// customItemByModel finds the Dragonfly item of a stack a creative client sent, by its item model.
func customItemByModel(model string) (world.Item, bool) {
	customMu.RLock()
	name, ok := customByModel[model]
	customMu.RUnlock()
	if !ok {
		return nil, false
	}
	return world.ItemByName(name, 0)
}

// customBlockState reports the Java state of a custom block, by Bedrock name.
func customBlockState(name string) (int32, bool) {
	customMu.RLock()
	defer customMu.RUnlock()
	s, ok := customBlocks[name]
	return s, ok
}

// applyCustomItem builds a stack of a custom item on its base item. It reports false for items
// that aren't registered custom items.
func applyCustomItem(name string, js *jitem.Stack) bool {
	c, ok := customItemFor(name)
	if !ok {
		return false
	}
	js.ID = c.id
	if c.model != "" {
		js.ItemModel = c.model
		js.Add(jitem.CompItemModel)
	}
	if c.name != "" {
		js.ItemName = jitem.Text{Text: c.name}
		js.Add(jitem.CompItemName)
	}
	if c.equip != nil {
		e := *c.equip
		js.Equippable = &e
		js.Add(jitem.CompEquippable)
	}
	return true
}
