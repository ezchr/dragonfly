package javasession

// Tests for the fixes of notes/review_items.md (items / inventories / containers / crafting).

import (
	"context"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/ezchr/dfjava/javamap"
	jitem "github.com/ezchr/go-mcjava/item"
	v777 "github.com/ezchr/go-mcjava/v777"
	"github.com/ezchr/go-mcjava/wire"
	"github.com/go-gl/mathgl/mgl64"
)

// itemsFakePlayer is the part of a player the click engine uses.
type itemsFakePlayer struct {
	session.Controllable
	st      *itemState
	gm      world.GameMode
	pos     mgl64.Vec3
	ender   *inventory.Inventory
	drop    func(item.Stack) int // nil: everything drops
	dropped []item.Stack
	moved   int
}

func (p *itemsFakePlayer) GameMode() world.GameMode                  { return p.gm }
func (p *itemsFakePlayer) Position() mgl64.Vec3                      { return p.pos }
func (p *itemsFakePlayer) EnderChestInventory() *inventory.Inventory { return p.ender }
func (p *itemsFakePlayer) ExperienceLevel() int                      { return 30 }

func (p *itemsFakePlayer) Drop(s item.Stack) int {
	n := s.Count()
	if p.drop != nil {
		n = p.drop(s)
	}
	if n > 0 {
		p.dropped = append(p.dropped, s.Grow(n-s.Count()))
	}
	return n
}

func (p *itemsFakePlayer) MoveItemsToInventory() {
	p.moved++
	for _, it := range p.st.ui.Clear() {
		_, _ = p.st.inv.AddItem(it)
	}
}

func itemsTestSession() *Session {
	s := &Session{
		log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		wake:   make(chan struct{}, 1),
		closed: make(chan struct{}),
	}
	s.writers.New = func() any { return &wire.Writer{} }
	return s
}

// itemsTestState is the inventory state of a test session with empty inventories.
func itemsTestState() *itemState {
	st := itemsTestSession().items()
	st.inv, st.offHand, st.ui = inventory.New(36, nil), inventory.New(1, nil), inventory.New(54, nil)
	st.armour = inventory.NewArmour(nil)
	held := uint32(0)
	st.heldSlot.Store(&held)
	return st
}

func itemsTestPlayer(gm world.GameMode) (*Session, *itemState, *itemsFakePlayer) {
	st := itemsTestState()
	c := &itemsFakePlayer{st: st, gm: gm, ender: inventory.New(27, nil)}
	st.c = c
	return st.s, st, c
}

// itemsPackets returns the ids of the packets the session queued since the last call.
func itemsPackets(s *Session) []int32 {
	s.outMu.Lock()
	defer s.outMu.Unlock()
	ids := make([]int32, len(s.out))
	for i, p := range s.out {
		ids[i] = p.id
	}
	s.out = s.out[:0]
	return ids
}

func itemsCount(ids []int32, id int32) int {
	n := 0
	for _, x := range ids {
		if x == id {
			n++
		}
	}
	return n
}

// itemsClick clicks in the open window (window 0 if none) with the right state id.
func itemsClick(s *Session, tx *world.Tx, c session.Controllable, slot, button, mode int) {
	st := s.items()
	s.containerClick(tx, c, st.menu().id, st.stateID.Load(), click{slot: slot, button: button, mode: mode}, nil)
}

func itemsTestWorld(t *testing.T) *world.World {
	w := world.Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func itemsDo(t *testing.T, w *world.World, f func(tx *world.Tx)) {
	t.Helper()
	if err := w.Do(f).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// itemsViewers is the number of viewers of a chest or barrel (unexported in Dragonfly).
func itemsViewers(b world.Block) int {
	return reflect.ValueOf(b).FieldByName("viewers").Len()
}

func itemsFill(inv *inventory.Inventory, it item.Stack) {
	for i := range inv.Size() {
		_ = inv.SetItem(i, it)
	}
}

func itemsTotal(inv *inventory.Inventory, it world.Item) int {
	n := 0
	for _, s := range inv.Items() {
		if s.Item() == it {
			n += s.Count()
		}
	}
	return n
}

// 1. A chest window whose chest paired (Dragonfly gives both halves new inventories) closes on the
// next click, and the click does not take anything from the old inventory.
func TestStaleChestWindow(t *testing.T) {
	s, st, c := itemsTestPlayer(world.GameModeSurvival)
	w := itemsTestWorld(t)
	a, b := cube.Pos{0, 0, 0}, cube.Pos{1, 0, 0}
	c.pos = mgl64.Vec3{0.5, 0, 2}
	itemsDo(t, w, func(tx *world.Tx) {
		st.tx = tx
		chest := block.NewChest()
		itemsFill(chest.Inventory(tx, a), item.NewStack(item.Diamond{}, 64))
		tx.SetBlock(a, chest, nil)
		s.OpenBlockContainer(a, tx)
		m := st.open.Load()
		if m == nil || m.kind != menuChest || m.size != 27 {
			t.Fatalf("chest window: %+v", m)
		}
		old := m.inv
		if !s.menuStillValid(tx, c, m) {
			t.Fatal("fresh window not valid")
		}

		// A chest placed next to it pairs: both halves get cloned inventories.
		pair := block.Chest{}.DecodeNBT(map[string]any{"pairx": int32(a[0]), "pairz": int32(a[2])}).(block.Chest)
		tx.SetBlock(b, pair, nil)
		merged := tx.Block(b).(block.Container).Inventory(tx, b)
		if merged == old || merged.Size() != 54 {
			t.Fatalf("no pairing: %v", merged.Size())
		}
		itemsPackets(s)
		itemsClick(s, tx, c, 0, 0, clickQuickMove)
		if st.open.Load() != nil {
			t.Error("stale window still open")
		}
		if ids := itemsPackets(s); itemsCount(ids, v777.ClientboundPlayContainerClose) != 1 {
			t.Errorf("no container_close: %v", ids)
		}
		if n := itemsTotal(st.inv, item.Diamond{}); n != 0 {
			t.Errorf("took %d diamonds from the stale inventory", n)
		}
		if n := itemsTotal(old, item.Diamond{}); n != 27*64 {
			t.Errorf("old inventory changed: %d", n)
		}
		if n := itemsTotal(merged, item.Diamond{}); n != 27*64 {
			t.Errorf("double chest has %d diamonds", n)
		}

		// A stale window that is still open is replaced when the chest is used again.
		tx.SetBlock(a, block.Air{}, nil)
		tx.SetBlock(b, block.Air{}, nil)
		chest = block.NewChest()
		itemsFill(chest.Inventory(tx, a), item.NewStack(item.Diamond{}, 64))
		tx.SetBlock(a, chest, nil)
		s.OpenBlockContainer(a, tx)
		stale := st.open.Load()
		tx.SetBlock(b, pair, nil)
		merged = tx.Block(b).(block.Container).Inventory(tx, b)
		s.OpenBlockContainer(a, tx)
		if m := st.open.Load(); m == stale || m.inv != merged {
			t.Fatalf("using the chest again kept the stale window")
		}
		s.closeMenu(tx, c, true)

		// Opening it again gives the double chest; a second open of the same valid window is a no-op.
		s.OpenBlockContainer(a, tx)
		m = st.open.Load()
		if m == nil || m.inv != merged || m.size != 54 {
			t.Fatalf("reopened: %+v", m)
		}
		id := m.id
		s.OpenBlockContainer(a, tx)
		if st.open.Load().id != id {
			t.Error("reopening a valid window replaced it")
		}
		js := 0
		for it, _ := merged.Item(js); it.Empty(); it, _ = merged.Item(js) {
			js++
		}
		itemsClick(s, tx, c, js, 0, clickQuickMove)
		if n := itemsTotal(merged, item.Diamond{}); n != 26*64 {
			t.Errorf("valid double chest: shift-click moved %d", 27*64-n)
		}
	})
}

// 2. Every session has its own window-0 menu, and the creative flag is per click (no shared state
// for -race to find, and a survival click never acts creative).
func TestPlayerMenuPerSession(t *testing.T) {
	sc, stc, cc := itemsTestPlayer(world.GameModeCreative)
	ss, sts, cs := itemsTestPlayer(world.GameModeSurvival)
	if stc.menu() == sts.menu() {
		t.Fatal("sessions share the player menu")
	}
	_ = sts.inv.SetItem(9, item.NewStack(item.Diamond{}, 1))
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 500 {
			itemsClick(sc, nil, cc, 10, 0, clickPickup)
		}
	}()
	go func() {
		defer wg.Done()
		for range 500 {
			itemsClick(ss, nil, cs, 9, 0, clickClone) // creative only
			if cur := sts.cursor(); !cur.Empty() {
				t.Errorf("survival CLONE gave %v", cur)
				return
			}
		}
	}()
	wg.Wait()
}

// 3. The inventory state outlives the closed connection until Session.Close closed the windows,
// so the open chest forgets the viewer and the cursor and grids come back.
func TestItemsSurviveConnectionClose(t *testing.T) {
	s, st, c := itemsTestPlayer(world.GameModeSurvival)
	w := itemsTestWorld(t)
	pos := cube.Pos{0, 0, 0}
	c.pos = mgl64.Vec3{0.5, 0, 2}
	itemsDo(t, w, func(tx *world.Tx) {
		st.tx = tx
		tx.SetBlock(pos, block.NewChest(), nil)
		s.OpenBlockContainer(pos, tx)
		if itemsViewers(tx.Block(pos)) != 1 {
			t.Fatal("not a viewer")
		}
		_ = st.ui.SetItem(cursorUISlot, item.NewStack(item.Diamond{}, 5))
		_ = st.ui.SetItem(uiCraftSmall, item.NewStack(item.Stick{}, 2))

		s.closeForItemsTest()
		time.Sleep(20 * time.Millisecond) // the old code deleted the state here
		if s.items() != st {
			t.Fatal("state replaced after the connection closed")
		}
		s.closeContainers(tx, c)
		if itemsViewers(tx.Block(pos)) != 0 {
			t.Error("chest still has the viewer")
		}
		if itemsTotal(st.inv, item.Diamond{}) != 5 || itemsTotal(st.inv, item.Stick{}) != 2 {
			t.Errorf("items not back: %v", st.inv.Items())
		}
		// Gone now, and never made again (no leak through later viewer calls).
		if got := s.items(); got == st {
			t.Error("state kept after Close")
		}
		s.ViewSlotChange(0, item.NewStack(item.Diamond{}, 1))
		if _, ok := itemStates.Load(s); ok {
			t.Error("a state is stored for the closed session")
		}
	})
}

// closeForItemsTest closes the session's closed channel like CloseConnection (which also
// closes the network connection a test session does not have).
func (s *Session) closeForItemsTest() { s.once.Do(func() { close(s.closed) }) }

// 4. A drop the player handler cancels goes back where it came from, even with a full inventory.
func TestCancelledDropGoesBack(t *testing.T) {
	s, st, c := itemsTestPlayer(world.GameModeSurvival)
	itemsFill(st.inv, item.NewStack(item.Stick{}, 64))
	c.drop = func(item.Stack) int { return 0 }
	_ = st.ui.SetItem(cursorUISlot, item.NewStack(item.Diamond{}, 64))

	itemsClick(s, nil, c, slotOutside, 0, clickPickup)
	if cur := st.cursor(); cur.Count() != 64 {
		t.Errorf("cursor after a cancelled drop: %v", cur)
	}
	itemsClick(s, nil, c, slotOutside, 1, clickPickup)
	if cur := st.cursor(); cur.Count() != 64 {
		t.Errorf("cursor after a cancelled single drop: %v", cur)
	}
	// The handler lets 10 of the 64 go: the other 54 stay on the cursor.
	c.drop = func(s item.Stack) int { return min(10, s.Count()) }
	itemsClick(s, nil, c, slotOutside, 0, clickPickup)
	if cur := st.cursor(); cur.Count() != 54 || len(c.dropped) != 1 {
		t.Errorf("cursor after a partly cancelled drop: %v (dropped %v)", cur, c.dropped)
	}
	c.dropped = nil
	// Q over a slot, the handler letting 10 of 64 go with Ctrl+Q.
	_ = st.ui.SetItem(cursorUISlot, item.Stack{})
	c.drop = func(s item.Stack) int { return min(10, s.Count()) }
	itemsClick(s, nil, c, 9, 1, clickThrow)
	if it, _ := st.inv.Item(9); it.Count() != 54 || len(c.dropped) != 1 || c.dropped[0].Count() != 10 {
		t.Errorf("throw: slot %v, dropped %v", it, c.dropped)
	}
	// Shift-clicking the 2x2 result with a full inventory drops the rest; cancelled, it is not
	// crafted at all.
	c.drop, c.dropped = func(item.Stack) int { return 0 }, nil
	planks := item.NewStack(block.Planks{Wood: block.OakWood()}, 62)
	_ = st.inv.SetItem(9, planks)
	_ = st.ui.SetItem(uiCraftSmall, item.NewStack(block.Log{Wood: block.OakWood()}, 1))
	_ = st.ui.SetItem(cursorUISlot, item.NewStack(item.Diamond{}, 1))
	itemsClick(s, nil, c, 0, 0, clickQuickMove)
	if it, _ := st.ui.Item(uiCraftSmall); it.Count() != 1 || itemsTotal(st.inv, planks.Item()) != 62 {
		t.Errorf("crafted with nowhere to put 2 of the planks: grid %v, planks %d", it, itemsTotal(st.inv, planks.Item()))
	}
}

// 5/6. Closing the window from another world (respawn into another world, portals) still removes
// the viewer in the old world, and closeWindows (for dimension changes) closes it.
func TestCloseFromOtherWorld(t *testing.T) {
	s, st, c := itemsTestPlayer(world.GameModeSurvival)
	w1, w2 := itemsTestWorld(t), itemsTestWorld(t)
	pos := cube.Pos{0, 0, 0}
	c.pos = mgl64.Vec3{0.5, 0, 2}
	itemsDo(t, w1, func(tx *world.Tx) {
		st.tx = tx
		tx.SetBlock(pos, block.NewBarrel(), nil)
		s.OpenBlockContainer(pos, tx)
	})
	if st.open.Load() == nil {
		t.Fatal("barrel not open")
	}
	itemsDo(t, w2, func(tx *world.Tx) {
		if s.menuStillValid(tx, c, st.open.Load()) {
			t.Error("window valid from another world")
		}
		s.closeWindows(tx, c)
	})
	if st.open.Load() != nil || c.moved != 1 {
		t.Errorf("not closed (moved %d)", c.moved)
	}
	itemsDo(t, w1, func(tx *world.Tx) {
		if n := itemsViewers(tx.Block(pos)); n != 0 {
			t.Errorf("barrel in the old world still has %d viewers", n)
		}
	})
}

type itemsBinding struct{}

func (itemsBinding) Name() string                                        { return "Curse of Binding" }
func (itemsBinding) MaxLevel() int                                       { return 1 }
func (itemsBinding) Cost(int) (int, int)                                 { return 25, 50 }
func (itemsBinding) Rarity() item.EnchantmentRarity                      { return item.EnchantmentRarityVeryRare }
func (itemsBinding) CompatibleWithEnchantment(item.EnchantmentType) bool { return true }
func (itemsBinding) CompatibleWithItem(world.Item) bool                  { return true }

// 7. Worn armour with Curse of Binding stays on in survival, whatever the click.
func TestCurseOfBinding(t *testing.T) {
	if _, ok := item.EnchantmentByID(bindingCurse); !ok {
		item.RegisterEnchantment(bindingCurse, itemsBinding{})
	}
	et, _ := item.EnchantmentByID(bindingCurse)
	helmet := item.NewStack(item.Helmet{Tier: item.ArmourTierIron{}}, 1).WithEnchantments(item.NewEnchantment(et, 1))
	for _, k := range []click{
		{slot: 5, button: 0, mode: clickPickup},
		{slot: 5, button: 0, mode: clickQuickMove},
		{slot: 5, button: 0, mode: clickSwap},
		{slot: 5, button: 1, mode: clickThrow},
	} {
		s, st, c := itemsTestPlayer(world.GameModeSurvival)
		_ = st.armour.Inventory().SetItem(0, helmet)
		itemsClick(s, nil, c, k.slot, k.button, k.mode)
		if it, _ := st.armour.Inventory().Item(0); !it.Equal(helmet) || len(c.dropped) != 0 {
			t.Errorf("mode %d took the cursed helmet (slot %v)", k.mode, it)
		}
	}
	// Creative players may take it off.
	s, st, c := itemsTestPlayer(world.GameModeCreative)
	_ = st.armour.Inventory().SetItem(0, helmet)
	itemsClick(s, nil, c, 5, 0, clickPickup)
	if it, _ := st.armour.Inventory().Item(0); !it.Empty() || st.cursor().Empty() {
		t.Error("creative could not take the cursed helmet off")
	}
}

// 8. The grindstone gives nothing for a single item without enchantments.
func TestGrindstoneUnenchanted(t *testing.T) {
	renamed := item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1).WithCustomName("x").WithAnvilCost(7)
	if res := grindResult(renamed, item.Stack{}); !res.Empty() {
		t.Errorf("one unenchanted item: %v", res)
	}
	if res := grindResult(item.Stack{}, renamed); !res.Empty() {
		t.Errorf("one unenchanted item (second slot): %v", res)
	}
	ench := renamed.WithEnchantments(item.NewEnchantment(enchantment.Sharpness, 2))
	if res := grindResult(ench, item.Stack{}); res.Empty() || len(res.Enchantments()) != 0 || res.AnvilCost() != 0 {
		t.Errorf("enchanted: %v", res)
	}
	if res := grindResult(renamed, renamed.Damage(10)); res.Empty() {
		t.Error("two unenchanted swords no longer repair")
	}
}

// 9. Furnace output: like vanilla's Slot.tryRemove, a full take only (no partial take onto a cursor
// that cannot hold all of it).
func TestFurnaceOutputAllOrNothing(t *testing.T) {
	_, st, c := itemsTestPlayer(world.GameModeSurvival)
	m := &menu{kind: menuFurnace, size: 3, inv: inventory.New(3, nil)}
	_ = m.inv.SetItem(2, item.NewStack(item.IronIngot{}, 10))
	_ = st.ui.SetItem(cursorUISlot, item.NewStack(item.IronIngot{}, 60))
	v := st.newView(nil, c, m)
	v.pickup(2, 0)
	if v.cursor.Count() != 60 || v.slots[2].Count() != 10 {
		t.Errorf("partial take: cursor %v, output %v", v.cursor, v.slots[2])
	}
	_ = st.ui.SetItem(cursorUISlot, item.NewStack(item.IronIngot{}, 54))
	v = st.newView(nil, c, m)
	v.pickup(2, 0)
	if v.cursor.Count() != 64 || !v.slots[2].Empty() {
		t.Errorf("full take: cursor %v, output %v", v.cursor, v.slots[2])
	}
}

// 9. Shift-clicking a shield goes to an empty off-hand.
func TestShieldToOffhand(t *testing.T) {
	s, st, c := itemsTestPlayer(world.GameModeSurvival)
	_ = st.inv.SetItem(12, item.NewStack(item.Shield{}, 1))
	itemsClick(s, nil, c, 12, 0, clickQuickMove)
	if off, _ := st.offHand.Item(0); off.Empty() {
		t.Error("shield not in the off-hand")
	}
	// Taken: the next one goes to the hotbar as before.
	_ = st.inv.SetItem(12, item.NewStack(item.Shield{}, 1))
	itemsClick(s, nil, c, 12, 0, clickQuickMove)
	if it, _ := st.inv.Item(0); it.Empty() {
		t.Error("second shield not in the hotbar")
	}
}

// 9. Creative drops are throttled like vanilla's dropSpamThrottler.
func TestDropThrottle(t *testing.T) {
	var d dropThrottle
	now := time.Now()
	n := 0
	for range 200 {
		if d.allow(now) {
			n++
		}
	}
	if n != 74 {
		t.Errorf("burst of %d drops", n)
	}
	at := func(ms int) bool { return d.allow(now.Add(time.Duration(ms) * time.Millisecond)) }
	if at(0) || !at(100) || at(100) || at(500) || !at(1200) {
		t.Error("not one drop a second after the burst")
	}
}

// 9. A click in a double chest sends what changed, not the whole window.
func TestClickSendsChanges(t *testing.T) {
	s, st, c := itemsTestPlayer(world.GameModeSurvival)
	w := itemsTestWorld(t)
	pos := cube.Pos{0, 0, 0}
	c.pos = mgl64.Vec3{0.5, 0, 2}
	itemsDo(t, w, func(tx *world.Tx) {
		st.tx = tx
		chest := block.NewChest()
		itemsFill(chest.Inventory(tx, pos), item.NewStack(item.Stick{}, 1))
		tx.SetBlock(pos, chest, nil)
		s.OpenBlockContainer(pos, tx)
		m := st.open.Load()
		if ids := itemsPackets(s); m == nil || itemsCount(ids, v777.ClientboundPlayContainerSetContent) != 1 {
			t.Fatalf("open: %v", ids)
		}
		itemsClick(s, tx, c, 3, 0, clickPickup)
		ids := itemsPackets(s)
		if itemsCount(ids, v777.ClientboundPlayContainerSetContent) != 0 || itemsCount(ids, v777.ClientboundPlayContainerSetSlot) != 1 {
			t.Errorf("pickup sent %v", ids)
		}
		// The client's own prediction of other slots is answered too.
		s.containerClick(tx, c, m.id, st.stateID.Load(), click{slot: 3, button: 0, mode: clickPickup}, []int16{3, 10, 11, 500})
		if ids := itemsPackets(s); itemsCount(ids, v777.ClientboundPlayContainerSetSlot) != 3 {
			t.Errorf("prediction resync: %v", ids)
		}
		// A wrong state id gets the whole window.
		s.containerClick(tx, c, m.id, st.stateID.Load()-1, click{slot: 3, button: 0, mode: clickPickup}, nil)
		if ids := itemsPackets(s); itemsCount(ids, v777.ClientboundPlayContainerSetContent) != 1 {
			t.Errorf("stale state id: %v", ids)
		}
	})
}

// 9. What a creative client makes is bounded, and a copy of a shulker box does not share its
// inventory (nor plugin values).
func TestCreativeLimits(t *testing.T) {
	js := jitem.Stack{Count: 1, ID: javamap.Item(item.Sword{Tier: item.ToolTierDiamond})}
	js.CustomName.Text = strings.Repeat("n", 5000)
	js.Add(jitem.CompCustomName)
	for range 200 {
		js.Lore = append(js.Lore, jitem.Text{Text: strings.Repeat("l", 70000)})
	}
	js.Add(jitem.CompLore)
	js.Add(jitem.CompUnbreakable)
	sharp := enchantments().toJava[9]
	js.Enchantments = []jitem.Enchantment{{ID: sharp, Level: 255}}
	js.Add(jitem.CompEnchantments)
	ds, ok := dragonflyStack(&js)
	if !ok {
		t.Fatal("not converted")
	}
	if len([]rune(ds.CustomName())) != maxCreativeName || len(ds.Lore()) != maxCreativeLore || len(ds.Lore()[0]) != maxCreativeLoreChars {
		t.Errorf("name %d, lore %d lines", len(ds.CustomName()), len(ds.Lore()))
	}
	if ds.Unbreakable() {
		t.Error("unbreakable taken")
	}
	if e := ds.Enchantments(); len(e) != 1 || e[0].Level() != 5 {
		t.Errorf("enchantments %v", e)
	}

	// A shulker box with contents and a value, copied by a creative client (middle click).
	s, st, c := itemsTestPlayer(world.GameModeCreative)
	box := block.NewShulkerBox()
	_ = box.Inventory(nil, cube.Pos{}).SetItem(0, item.NewStack(item.Diamond{}, 64))
	orig := item.NewStack(box, 1).WithValue("crate", "key")
	_ = st.inv.SetItem(0, orig)
	var bj jitem.Stack
	javaStack(orig, &bj)
	s.creativeSlot(c, 20, &bj)
	cp, _ := st.inv.Item(20)
	if cp.Empty() || len(cp.Values()) != 0 {
		t.Fatalf("copy %v", cp)
	}
	if inv := cp.Item().(block.ShulkerBox).Inventory(nil, cube.Pos{}); inv != nil && itemsTotal(inv, item.Diamond{}) != 0 {
		t.Error("the copy has the contents")
	}
	// A move (shift-click: target set first, source cleared after) keeps everything.
	_ = st.inv.SetItem(20, item.Stack{})
	s.creativeSlot(c, 21, &bj)
	s.creativeSlot(c, mainToJava(0), &jitem.Stack{})
	mv, _ := st.inv.Item(21)
	if mv.Values()["crate"] != "key" || itemsTotal(mv.Item().(block.ShulkerBox).Inventory(nil, cube.Pos{}), item.Diamond{}) != 64 {
		t.Errorf("moved %v", mv)
	}
	if it, _ := st.inv.Item(0); !it.Empty() {
		t.Errorf("source not cleared: %v", it)
	}
	// Pick up and put down elsewhere: also a move, once.
	s.creativeSlot(c, 21, &jitem.Stack{})
	s.creativeSlot(c, 22, &bj)
	s.creativeSlot(c, 23, &bj)
	a, _ := st.inv.Item(22)
	b, _ := st.inv.Item(23)
	if a.Values()["crate"] != "key" || len(b.Values()) != 0 {
		t.Errorf("put down twice: %v / %v", a, b)
	}
}

// Hardening: a creative drag (full stacks) ends when the player is no longer creative.
func TestCreativeDragRechecked(t *testing.T) {
	// Survival from the second slot on (creative again at the end), and survival only at the end.
	for _, survivalAt := range []int{2, 3} {
		s, st, c := itemsTestPlayer(world.GameModeCreative)
		_ = st.ui.SetItem(cursorUISlot, item.NewStack(item.Diamond{}, 1))
		for i, k := range []click{{slotOutside, 8, clickQuickCraft}, {10, 9, clickQuickCraft}, {11, 9, clickQuickCraft}, {slotOutside, 10, clickQuickCraft}} {
			c.gm = world.GameModeCreative
			if i == survivalAt {
				c.gm = world.GameModeSurvival
			}
			itemsClick(s, nil, c, k.slot, k.button, k.mode)
		}
		if n := itemsTotal(st.inv, item.Diamond{}); n != 0 {
			t.Errorf("survival at packet %d: a creative drag placed %d diamonds", survivalAt, n)
		}
	}
}
