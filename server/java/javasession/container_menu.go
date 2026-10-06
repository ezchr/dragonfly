package javasession

import (
	"reflect"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/ezchr/go-mcjava/text"
	v777 "github.com/ezchr/go-mcjava/v777"
)

// A menu is a Java container window: the player's own inventory (window 0) or a block's window.
// Its slots are the container's slots followed by the 36 slots of the player's inventory (27 main,
// then the hotbar), exactly like vanilla's AbstractContainerMenu.addStandardInventorySlots.

type menuKind uint8

const (
	menuPlayer menuKind = iota
	menuChest           // generic_9x3 / 9x6: chests, barrels, ender chests
	menuShulker
	menuHopper
	menuFurnace // furnace, blast furnace, smoker
	menuBrewing
	menuCrafting
	menuAnvil
	menuEnchanting
	menuGrindstone
	menuStonecutter
	menuSmithing
	menuLoom
	menuBeacon
)

// Slots of the UI inventory that hold the inputs of block windows without an inventory of their
// own. They are the slots the Bedrock session uses, so Player.MoveItemsToInventory gives the items
// back when the window closes, whichever edition the player is on.
const (
	uiAnvilInput     = 1
	uiAnvilMaterial  = 2
	uiStonecutter    = 3
	uiLoomBanner     = 9
	uiLoomDye        = 10
	uiLoomPattern    = 11
	uiEnchantInput   = 14
	uiEnchantLapis   = 15
	uiGrindFirst     = 16
	uiGrindSecond    = 17
	uiBeacon         = 27
	uiCraftSmall     = 28 // 2x2 grid: 28-31
	uiCraftLarge     = 32 // 3x3 grid: 32-40
	uiSmithBase      = 51
	uiSmithAddition  = 52
	uiSmithTemplate  = 53
	invSlotsInWindow = 36
	maxWindow        = 54 + invSlotsInWindow + 1 // double chest + inventory + hidden off-hand
)

// slotKind says what may go in a slot.
type slotKind uint8

const (
	kindNone           slotKind = iota
	kindPlain                   // anything
	kindArmour                  // idx is the armour slot (0 helmet - 3 boots)
	kindResult                  // computed result (crafting, stations): taken whole, nothing placed
	kindTakeOnly                // furnace output: taken like a normal slot, nothing placed
	kindFuel                    // furnace fuel
	kindPotion                  // brewing bottles, one each
	kindBrewIngredient          // brewing reagent
	kindBlazePowder             // brewing fuel
	kindSingle                  // one item (enchanting input)
	kindLapis                   // lapis lazuli only
	kindShulker                 // anything but shulker boxes
	kindGrind                   // damageable or enchanted items
	kindBeacon                  // beacon payment, one item
	kindBanner
	kindDye
	kindPattern
	kindTemplate // smithing template
	kindSmithBase
	kindSmithAddition
)

type slotRef struct {
	inv  *inventory.Inventory
	idx  int
	kind slotKind
}

// menu is an open window.
type menu struct {
	kind  menuKind
	id    int32 // window id
	typ   int32 // minecraft:menu id
	size  int   // slots before the player's inventory
	pos   cube.Pos
	btype reflect.Type // the block opened, for the still-valid check
	w     *world.World
	inv   *inventory.Inventory // the block's inventory, nil for windows with inputs in the UI inventory
	ender bool

	// virtual: a window on an inventory with no block behind it (OpenInventory). onClose is
	// called when it closes; shown is what the client was last sent, for refreshVirtual.
	virtual bool
	onClose func()
	shown   []item.Stack

	cookTotal int32 // furnaces: cooking time in ticks
	smoker    bool

	// Anvil: the name the client typed (nil until it sends one) and the last cost.
	name    *string
	cost    int
	repairN int

	// Stonecutter: the selected recipe (index in the input's list), -1 for none.
	sel     int
	selItem string

	data      [10]int32
	dataN     int
	dataSent  bool
	lastCheck time.Time
}

// slots is the number of slots the client sees.
func (m *menu) slots() int {
	if m.kind == menuPlayer {
		return playerSlots
	}
	return m.size + invSlotsInWindow
}

// total includes the hidden off-hand slot of block windows (swap with F).
func (m *menu) total() int {
	if m.kind == menuPlayer {
		return playerSlots
	}
	return m.size + invSlotsInWindow + 1
}

// offhand is the window slot of the off-hand (hidden in block windows).
func (m *menu) offhand() int {
	if m.kind == menuPlayer {
		return slotOffhand
	}
	return m.size + invSlotsInWindow
}

// hotbar is the window slot of hotbar slot i.
func (m *menu) hotbar(i int) int {
	if m.kind == menuPlayer {
		return slotHotbar + i
	}
	return m.size + 27 + i
}

// invStart, invEnd and hotbarStart delimit the player's inventory in the window.
func (m *menu) invStart() int {
	if m.kind == menuPlayer {
		return slotMain
	}
	return m.size
}

func (m *menu) hotbarStart() int { return m.invStart() + 27 }
func (m *menu) invEnd() int      { return m.invStart() + 36 }

// resultSlot is the computed result slot, -1 if the window has none.
func (m *menu) resultSlot() int {
	switch m.kind {
	case menuPlayer, menuCrafting:
		return 0
	case menuAnvil, menuGrindstone:
		return 2
	case menuStonecutter:
		return 1
	case menuSmithing:
		return 3
	}
	return -1
}

// gridSlots is the crafting grid: first window slot and width.
func (m *menu) grid() (first, width int) {
	switch m.kind {
	case menuPlayer:
		return 1, 2
	case menuCrafting:
		return 1, 3
	}
	return 0, 0
}

// uiSection is the container section of windows whose inputs are in the UI inventory: the UI slot
// of each window slot (-1 for the result) and its kind.
var uiSection = map[menuKind][]struct {
	ui   int
	kind slotKind
}{
	menuCrafting: {{-1, kindResult},
		{uiCraftLarge, kindPlain}, {uiCraftLarge + 1, kindPlain}, {uiCraftLarge + 2, kindPlain},
		{uiCraftLarge + 3, kindPlain}, {uiCraftLarge + 4, kindPlain}, {uiCraftLarge + 5, kindPlain},
		{uiCraftLarge + 6, kindPlain}, {uiCraftLarge + 7, kindPlain}, {uiCraftLarge + 8, kindPlain}},
	menuAnvil:       {{uiAnvilInput, kindPlain}, {uiAnvilMaterial, kindPlain}, {-1, kindResult}},
	menuEnchanting:  {{uiEnchantInput, kindSingle}, {uiEnchantLapis, kindLapis}},
	menuGrindstone:  {{uiGrindFirst, kindGrind}, {uiGrindSecond, kindGrind}, {-1, kindResult}},
	menuStonecutter: {{uiStonecutter, kindPlain}, {-1, kindResult}},
	menuSmithing:    {{uiSmithTemplate, kindTemplate}, {uiSmithBase, kindSmithBase}, {uiSmithAddition, kindSmithAddition}, {-1, kindResult}},
	menuLoom:        {{uiLoomBanner, kindBanner}, {uiLoomDye, kindDye}, {uiLoomPattern, kindPattern}, {-1, kindResult}},
	menuBeacon:      {{uiBeacon, kindBeacon}},
}

// refs fills the slot references of the window and returns how many there are (with the hidden
// off-hand).
func (st *itemState) refs(m *menu, out *[maxWindow]slotRef) int {
	if m.kind == menuPlayer {
		for js := range playerSlots {
			out[js], _ = st.playerRef(js)
		}
		return playerSlots
	}
	switch m.kind {
	case menuChest, menuHopper:
		for i := range m.size {
			out[i] = slotRef{m.inv, i, kindPlain}
		}
	case menuShulker:
		for i := range m.size {
			out[i] = slotRef{m.inv, i, kindShulker}
		}
	case menuFurnace:
		out[0] = slotRef{m.inv, 0, kindPlain}
		out[1] = slotRef{m.inv, 1, kindFuel}
		out[2] = slotRef{m.inv, 2, kindTakeOnly}
	case menuBrewing:
		// Java: bottles 0-2, ingredient 3, fuel 4. Dragonfly: ingredient 0, bottles 1-3, fuel 4.
		for i := range 3 {
			out[i] = slotRef{m.inv, i + 1, kindPotion}
		}
		out[3] = slotRef{m.inv, 0, kindBrewIngredient}
		out[4] = slotRef{m.inv, 4, kindBlazePowder}
	default:
		for i, s := range uiSection[m.kind] {
			if s.ui < 0 {
				out[i] = slotRef{kind: s.kind}
			} else {
				out[i] = slotRef{st.ui, s.ui, s.kind}
			}
		}
	}
	base := m.size
	for i := range 27 {
		out[base+i] = slotRef{st.inv, 9 + i, kindPlain}
	}
	for i := range 9 {
		out[base+27+i] = slotRef{st.inv, i, kindPlain}
	}
	out[base+36] = slotRef{st.offHand, 0, kindPlain}
	return base + 37
}

// brewingWindowSlot is the Java window slot of Dragonfly brewing stand slot i.
func brewingWindowSlot(i int) int {
	switch {
	case i == 0:
		return 3
	case i < 4:
		return i - 1
	}
	return i
}

// mayPlace reports whether stack it may be put in a slot of this kind.
func (r *slotRef) mayPlace(it item.Stack) bool {
	if it.Empty() {
		return true
	}
	switch r.kind {
	case kindNone, kindResult, kindTakeOnly:
		return false
	case kindArmour:
		switch r.idx {
		case 0:
			h, ok := it.Item().(item.HelmetType)
			return ok && h.Helmet()
		case 1:
			c, ok := it.Item().(item.ChestplateType)
			return ok && c.Chestplate()
		case 2:
			l, ok := it.Item().(item.LeggingsType)
			return ok && l.Leggings()
		default:
			b, ok := it.Item().(item.BootsType)
			return ok && b.Boots()
		}
	case kindShulker:
		_, nested := it.Item().(block.ShulkerBox)
		return !nested
	case kindFuel:
		return isFuel(it)
	case kindPotion:
		switch it.Item().(type) {
		case item.Potion, item.SplashPotion, item.LingeringPotion, item.GlassBottle:
			return true
		}
		return false
	case kindBrewIngredient:
		return recipe.ValidBrewingReagent(it.Item())
	case kindBlazePowder:
		_, ok := it.Item().(item.BlazePowder)
		return ok
	case kindLapis:
		_, ok := it.Item().(item.LapisLazuli)
		return ok
	case kindGrind:
		_, durable := it.Item().(item.Durable)
		return durable || len(it.Enchantments()) > 0
	case kindBeacon:
		p, ok := it.Item().(item.BeaconPayment)
		return ok && p.PayableForBeacon()
	case kindBanner:
		name, _ := it.Item().EncodeItem()
		return name == "minecraft:banner"
	case kindDye:
		_, ok := it.Item().(item.Dye)
		return ok
	case kindPattern:
		_, ok := it.Item().(item.BannerPattern)
		return ok
	case kindTemplate:
		return smithing().template[itemName(it)]
	case kindSmithBase:
		return smithing().base[itemName(it)]
	case kindSmithAddition:
		return smithing().addition[itemName(it)]
	}
	return true
}

// bindingCurse is the Bedrock id of Curse of Binding. Dragonfly does not register it yet, but a
// plugin may.
const bindingCurse = 27

// mayPickup reports whether it, the stack in the slot, may be taken out (vanilla Slot.mayPickup):
// not worn armour with Curse of Binding, unless the player is in creative (ArmorSlot.mayPickup).
func (r *slotRef) mayPickup(it item.Stack, creative bool) bool {
	if r.kind != kindArmour || creative || it.Empty() {
		return true
	}
	for _, e := range it.Enchantments() {
		if id, ok := item.EnchantmentID(e.Type()); ok && id == bindingCurse {
			return false
		}
	}
	return true
}

// maxIn is how many of it fit in the slot.
func (r *slotRef) maxIn(it item.Stack) int {
	switch r.kind {
	case kindArmour, kindPotion, kindSingle, kindBeacon:
		return 1
	}
	return it.MaxCount()
}

func isFuel(it item.Stack) bool {
	if f, ok := it.Item().(item.Fuel); ok && f.FuelInfo().Duration > 0 {
		return true
	}
	b, ok := it.Item().(item.Bucket)
	return ok && b.Empty() // vanilla lets an empty bucket in (to take lava back out)
}

func itemName(it item.Stack) string {
	if it.Empty() {
		return ""
	}
	name, _ := it.Item().EncodeItem()
	return name
}

// menuFor describes the window of block b, or ok false if it has none on Java.
func menuFor(b any) (m *menu, titleC text.Component, ok bool) {
	var title string
	m = &menu{sel: -1}
	custom := ""
	switch b := b.(type) {
	case block.Chest:
		m.kind, m.size, title, custom = menuChest, b.ContainerSize(), "container.chest", b.CustomName
		if m.size == 54 {
			title = "container.chestDouble"
		}
	case block.Barrel:
		m.kind, m.size, title, custom = menuChest, 27, "container.barrel", b.CustomName
	case block.EnderChest:
		m.kind, m.size, title, m.ender = menuChest, 27, "container.enderchest", true
	case block.ShulkerBox:
		m.kind, m.size, title, custom = menuShulker, 27, "container.shulkerBox", b.CustomName
	case block.Hopper:
		m.kind, m.size, title, custom = menuHopper, 5, "container.hopper", b.CustomName
	case block.Furnace:
		m.kind, m.size, title, m.cookTotal = menuFurnace, 3, "container.furnace", 200
	case block.BlastFurnace:
		m.kind, m.size, title, m.cookTotal = menuFurnace, 3, "container.blast_furnace", 100
	case block.Smoker:
		m.kind, m.size, title, m.cookTotal, m.smoker = menuFurnace, 3, "container.smoker", 100, true
	case block.BrewingStand:
		m.kind, m.size, title = menuBrewing, 5, "container.brewing"
	case block.CraftingTable:
		m.kind, title = menuCrafting, "container.crafting"
	case block.Anvil:
		m.kind, title = menuAnvil, "container.repair"
	case block.EnchantingTable:
		m.kind, title = menuEnchanting, "container.enchant"
	case block.Grindstone:
		m.kind, title = menuGrindstone, "container.grindstone_title"
	case block.Stonecutter:
		m.kind, title = menuStonecutter, "container.stonecutter"
	case block.SmithingTable:
		m.kind, title = menuSmithing, "container.upgrade"
	case block.Loom:
		m.kind, title = menuLoom, "container.loom"
	case block.Beacon:
		m.kind, title = menuBeacon, "container.beacon"
	default:
		return nil, text.Component{}, false
	}
	if s, ok := uiSection[m.kind]; ok {
		m.size = len(s)
	}
	name := ""
	switch m.kind {
	case menuChest:
		name = [...]string{"minecraft:generic_9x1", "minecraft:generic_9x2", "minecraft:generic_9x3",
			"minecraft:generic_9x4", "minecraft:generic_9x5", "minecraft:generic_9x6"}[min(max(m.size/9, 1), 6)-1]
	case menuShulker:
		name = "minecraft:shulker_box"
	case menuHopper:
		name = "minecraft:hopper"
	case menuFurnace:
		name = map[string]string{"container.furnace": "minecraft:furnace", "container.blast_furnace": "minecraft:blast_furnace", "container.smoker": "minecraft:smoker"}[title]
	case menuBrewing:
		name = "minecraft:brewing_stand"
	case menuCrafting:
		name = "minecraft:crafting"
	case menuAnvil:
		name = "minecraft:anvil"
	case menuEnchanting:
		name = "minecraft:enchantment"
	case menuGrindstone:
		name = "minecraft:grindstone"
	case menuStonecutter:
		name = "minecraft:stonecutter"
	case menuSmithing:
		name = "minecraft:smithing"
	case menuLoom:
		name = "minecraft:loom"
	case menuBeacon:
		name = "minecraft:beacon"
	}
	m.typ = v777.BuiltinID("minecraft:menu", name)
	if m.typ < 0 {
		return nil, text.Component{}, false
	}
	m.btype = reflect.TypeOf(b)
	if custom != "" {
		return m, bedrockText(custom), true
	}
	return m, text.Translatable(title), true
}
