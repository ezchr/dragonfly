package javasession

import (
	"time"

	"github.com/df-mc/dragonfly/server/event"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	jitem "github.com/ezchr/go-mcjava/item"
	v777 "github.com/ezchr/go-mcjava/v777"
	"github.com/ezchr/go-mcjava/wire"
)

// container_click modes (ContainerInput).
const (
	clickPickup = iota
	clickQuickMove
	clickSwap
	clickClone
	clickThrow
	clickQuickCraft
	clickPickupAll
)

// handleInventoryPacket handles the inventory packets: held slot, creative slots, clicks in and
// closing of windows, window buttons, anvil names and beacons. handled is false for other packets.
func (s *Session) handleInventoryPacket(id int32, body []byte) (handled bool, err error) {
	r := wire.NewReader(body)
	st := s.items()
	switch id {
	case v777.ServerboundPlaySetCarriedItem:
		slot := int(r.Int16())
		if r.Err != nil {
			return true, r.Err
		}
		if slot < 0 || slot > 8 {
			return true, nil // vanilla ignores it too
		}
		s.do(func(_ *world.Tx, c session.Controllable) {
			st.changingSlot.Store(true)
			defer st.changingSlot.Store(false)
			_ = c.SetHeldSlot(slot)
		})
	case v777.ServerboundPlaySetCreativeModeSlot:
		slot := int(r.Int16())
		if r.Err != nil {
			return true, r.Err
		}
		if st.creative.Load() {
			st.in.DecodeUntrustedFor(r, st.proto)
			if r.Err != nil {
				return true, r.Err
			}
			s.do(func(_ *world.Tx, c session.Controllable) { s.creativeSlot(c, slot, &st.in) })
			return true, nil
		}
		// Not creative in the player's latest transaction: the stack (whose text can be costly to
		// decode) is only decoded if the game mode changed since.
		s.do(func(_ *world.Tx, c session.Controllable) {
			if !c.GameMode().CreativeInventory() {
				s.sendInventory()
				return
			}
			st.creative.Store(true)
			st.in.DecodeUntrustedFor(r, st.proto)
			if r.Err == nil {
				s.creativeSlot(c, slot, &st.in)
			}
		})
		return true, r.Err
	case v777.ServerboundPlayContainerClick:
		var k click
		window := r.VarInt()
		stateID := r.VarInt()
		k.slot = int(r.Int16())
		k.button = int(r.Int8())
		k.mode = int(r.VarInt())
		n := int(r.VarInt())
		if r.Err == nil && (n < 0 || n > 128) {
			return true, jitem.ErrInvalid
		}
		var hs jitem.HashedStack
		var changed [128]int16 // the slots the client changed (its prediction)
		for i := range n {
			changed[i] = r.Int16()
			hs.DecodeFor(r, st.proto)
		}
		hs.DecodeFor(r, st.proto) // carried
		if r.Err != nil {
			return true, r.Err
		}
		s.do(func(tx *world.Tx, c session.Controllable) { s.containerClick(tx, c, window, stateID, k, changed[:n]) })
	case v777.ServerboundPlayContainerClose:
		window := r.VarInt()
		if r.Err != nil {
			return true, r.Err
		}
		s.do(func(tx *world.Tx, c session.Controllable) { s.clientClosed(tx, c, window) })
	case v777.ServerboundPlayContainerButtonClick:
		window, button := r.VarInt(), r.VarInt()
		if r.Err != nil {
			return true, r.Err
		}
		s.do(func(tx *world.Tx, c session.Controllable) { s.menuButton(tx, c, window, int(button)) })
	case v777.ServerboundPlayRenameItem:
		name := r.String(32767)
		if r.Err != nil {
			return true, r.Err
		}
		s.do(func(tx *world.Tx, c session.Controllable) { s.renameItem(tx, c, name) })
	case v777.ServerboundPlaySetBeacon:
		var effects [2]int32
		for i := range effects {
			effects[i] = -1
			if r.Bool() {
				effects[i] = st.proto.EffectIn(r.VarInt())
			}
		}
		if r.Err != nil {
			return true, r.Err
		}
		s.do(func(tx *world.Tx, c session.Controllable) { s.setBeacon(tx, c, effects[0], effects[1]) })
	default:
		return false, nil
	}
	return true, nil
}

// dropHeldItem drops the held item (Q, or Ctrl+Q for the whole stack): call it for player_action
// DROP_ITEM / DROP_ALL_ITEMS.
func (s *Session) dropHeldItem(c session.Controllable, all bool) {
	st := s.items()
	if st.inv == nil {
		return
	}
	hs := st.heldSlot.Load()
	if hs == nil {
		return
	}
	slot := int(*hs)
	it, _ := st.inv.Item(slot)
	if it.Empty() {
		return
	}
	if !all {
		it = it.Grow(1 - it.Count())
	}
	ctx := event.C(inventory.Holder(c))
	if st.inv.Handler().HandleDrop(ctx, slot, it); ctx.Cancelled() {
		s.sendSlot(mainToJava(slot), st.slotItem(mainToJava(slot)))
		return
	}
	n := c.Drop(it)
	cur, _ := st.inv.Item(slot)
	_ = st.inv.SetItem(slot, cur.Grow(-n))
	if n < it.Count() {
		s.sendSlot(mainToJava(slot), st.slotItem(mainToJava(slot)))
	}
}

// swapHands swaps the main hand and off-hand items (F): call it for player_action
// SWAP_ITEM_WITH_OFFHAND.
func (s *Session) swapHands(c session.Controllable) {
	main, off := c.HeldItems()
	c.SetHeldItems(off, main)
}

// creativeSlot handles set_creative_mode_slot: a creative client sets a slot of its inventory (or
// drops a stack, slot -1) to a stack it made.
func (s *Session) creativeSlot(c session.Controllable, slot int, js *jitem.Stack) {
	st := s.items()
	if !c.GameMode().CreativeInventory() || st.inv == nil {
		s.sendInventory()
		return
	}
	if slot == -1 {
		ds, _, ok := st.creativeStack(js, -1)
		if !ok || ds.Empty() {
			return
		}
		// Vanilla's dropSpamThrottler: creative drops spawn entities, so they are limited.
		if st.drops.allow(time.Now()) {
			c.Drop(ds)
		}
		return
	}
	ref, ok := st.playerRef(slot)
	if !ok || ref.inv == nil {
		return
	}
	old, _ := ref.inv.Item(ref.idx)
	// Was the last copy a move out of this slot? Then the old stack is in the copy's slot now.
	moved := !old.Empty() && st.restoreClone(slot, old)
	ds, src, ok := st.creativeStack(js, slot)
	if !ok {
		s.sendInventory()
		return
	}
	if !ref.mayPlace(ds) {
		s.sendSlot(slot, st.slotItem(slot))
		return
	}
	switch {
	case moved:
	case src == creativeSelf:
		// The client took part of the stack (onto its cursor): that part may be put down again.
		if old.Count() > ds.Count() {
			st.remember(old.Grow(-ds.Count()))
		}
	case !old.Empty():
		st.remember(old)
	}
	_ = ref.inv.SetItem(ref.idx, ds)
	if src == creativeCopy {
		st.clone.target, st.clone.stripped = slot, ds
	}
}

// remember keeps a stack a creative client took out of a slot: it may come back in another slot.
func (st *itemState) remember(it item.Stack) {
	copy(st.recent[:], st.recent[1:])
	st.recent[len(st.recent)-1] = it
}

// creativeClone is the last stack a creative client put down while the stack it matched was still
// in another slot (src). That is a copy, so it was converted from its Java form, without the
// Dragonfly-only data (plugin values, a shulker box's inventory). But the creative screen also
// sends a shift-click move as "set the target" before "clear the source": clearing src right
// after makes it a move, and the target gets the original back.
type creativeClone struct {
	target, src    int
	orig, stripped item.Stack
}

// restoreClone is called when a creative client empties or replaces slot js that held old. If the
// last copy was of exactly this stack and is still in its target slot, the copy was a move: the
// target gets the original back (with the data that was stripped), and old is not remembered.
func (st *itemState) restoreClone(js int, old item.Stack) bool {
	cl := st.clone
	st.clone = creativeClone{}
	if cl.orig.Empty() || cl.src != js || !old.Equal(cl.orig) {
		return false
	}
	ref, ok := st.playerRef(cl.target)
	if !ok || ref.inv == nil {
		return false
	}
	cur, _ := ref.inv.Item(ref.idx)
	if cur.Empty() || !cur.Equal(cl.stripped) {
		return false
	}
	_ = ref.inv.SetItem(ref.idx, cl.orig.Grow(cur.Count()-cl.orig.Count()))
	return true
}

// Where creativeStack found the stack a creative client sent.
const (
	creativeNew  = iota // converted from the Java stack
	creativeSelf        // the stack already in the target slot, at the same or a lower count
	creativeMove        // a stack the client took out of a slot before
	creativeCopy        // a copy of a stack that is still in another slot
)

// creativeStack turns a stack from a creative client into a Dragonfly stack. A stack the server
// sent (moved around by the client) is matched to the original, so data Java items can't carry
// (Dragonfly item values, a shulker box's inventory) survives a move; a copy of a stack that is
// still in the inventory is converted from its Java form instead, like any new stack, so it carries
// nothing a Bedrock creative player could not get either (and no shulker box shares an inventory).
func (st *itemState) creativeStack(js *jitem.Stack, target int) (item.Stack, int, bool) {
	if js.Empty() {
		return item.Stack{}, creativeNew, true
	}
	count := js.Count
	js.Count = 1
	var want, got wire.Writer
	js.EncodeFor(&want, st.proto)
	js.Count = count
	var conv jitem.Stack
	match := func(ds item.Stack) bool {
		if ds.Empty() {
			return false
		}
		got.Reset()
		javaStack(ds, &conv)
		conv.Count = 1
		conv.EncodeFor(&got, st.proto)
		return string(got.B) == string(want.B)
	}
	n := max(1, int(count))
	if target > 0 {
		if ds := st.slotItem(target); match(ds) && n <= ds.Count() {
			return ds.Grow(n - ds.Count()), creativeSelf, true
		}
	}
	for i := len(st.recent) - 1; i >= 0; i-- {
		if ds := st.recent[i]; match(ds) && n <= ds.Count() {
			// Taken from where it was: the rest stays there for a later slot.
			st.recent[i] = ds.Grow(-n)
			return ds.Grow(n - ds.Count()), creativeMove, true
		}
	}
	for slot := 1; slot < playerSlots; slot++ {
		if ds := st.slotItem(slot); slot != target && match(ds) {
			out, ok := dragonflyStack(js)
			if ok {
				st.clone = creativeClone{src: slot, orig: ds}
			}
			return out, creativeCopy, ok
		}
	}
	out, ok := dragonflyStack(js)
	return out, creativeNew, ok
}

// dropThrottle is vanilla's dropSpamThrottler (TickThrottler(20, 1480)): each drop adds 20, each
// tick takes 1 away, and drops stop at 1480 (a burst of 74, then one a second).
type dropThrottle struct {
	count float64
	last  time.Time
}

func (d *dropThrottle) allow(now time.Time) bool {
	if !d.last.IsZero() {
		d.count = max(0, d.count-float64(now.Sub(d.last))/float64(50*time.Millisecond))
	}
	d.last = now
	if d.count >= 1480 {
		return false
	}
	d.count += 20
	return true
}

// containerClick handles a container_click in window, which the client sent with state id stateID
// and its prediction of the slots that changed.
func (s *Session) containerClick(tx *world.Tx, c session.Controllable, window, stateID int32, k click, changed []int16) {
	st := s.items()
	m := st.menu()
	if window != m.id {
		s.syncWindow(tx, c, m)
		return
	}
	if !s.menuStillValid(tx, c, m) {
		// Vanilla ignores clicks in a window that is no longer valid (stillValid): the chest may
		// have paired or unpaired since, and its old inventory must not be used.
		s.closeMenu(tx, c, true)
		return
	}
	// The client echoes the last state id it got: if it missed something, it gets the whole window
	// again, else only what the click changed (vanilla broadcastChanges).
	full := stateID != st.stateID.Load()
	before := s.click(tx, c, m, k)
	if full || before == nil {
		s.syncWindow(tx, c, m)
		return
	}
	s.syncClick(tx, c, m, before, changed)
}

// click is a container_click.
type click struct {
	slot, button, mode int
}

// dragState is a quick-craft (drag) in progress.
type dragState struct {
	active bool
	kind   int // 0: split evenly, 1: one each, 2: full stacks (creative)
	slots  []int
}

// view is a window during a click: changes are made here, checked against the inventory handlers,
// then applied at once (vanilla AbstractContainerMenu.doClick, on copies).
type view struct {
	st    *itemState
	c     session.Controllable
	tx    *world.Tx
	m     *menu
	n     int // slots the client sees
	total int // with the hidden off-hand of block windows
	res   int // computed result slot, -1 if none
	// creative is whether the player has infinite materials (vanilla hasInfiniteMaterials), read
	// once per click from its game mode.
	creative bool

	refs   [maxWindow]slotRef
	orig   [maxWindow]item.Stack
	slots  [maxWindow]item.Stack
	cursor item.Stack
	ocur   item.Stack
	drops  []drop
	after  []func() // effects of taking results (levels, experience, sounds), run once applied

	craft craftMatch // the recipe behind the crafting result
}

// drop is a stack a click throws out of the window, from a slot or the cursor.
type drop struct {
	from int // window slot, cursorDrop or resultDrop
	it   item.Stack
}

const (
	cursorDrop = -1
	resultDrop = -2
)

func (st *itemState) newView(tx *world.Tx, c session.Controllable, m *menu) *view {
	v := &view{st: st, c: c, tx: tx, m: m, n: m.slots(), res: m.resultSlot()}
	v.total = st.refs(m, &v.refs)
	for js := range v.total {
		if r := &v.refs[js]; r.inv != nil {
			v.orig[js], _ = r.inv.Item(r.idx)
		}
	}
	v.slots = v.orig
	v.ocur = st.cursor()
	v.cursor = v.ocur
	v.creative = c.GameMode().CreativeInventory()
	v.updateResult()
	return v
}

// click applies a container_click. It returns the window's slots as the client saw them before
// (nil if nothing was done), for syncClick.
func (s *Session) click(tx *world.Tx, c session.Controllable, m *menu, k click) *[maxWindow]item.Stack {
	st := s.items()
	if st.inv == nil {
		return nil
	}
	v := st.newView(tx, c, m)
	before := v.slots
	creative := v.creative

	if k.mode != clickQuickCraft {
		st.drag = dragState{}
	}
	switch k.mode {
	case clickPickup:
		v.pickup(k.slot, k.button)
	case clickQuickMove:
		if v.valid(k.slot) && (k.button == 0 || k.button == 1) && v.mayPickup(k.slot) {
			v.quickMove(k.slot)
		}
	case clickSwap:
		v.swap(k.slot, k.button)
	case clickClone:
		if creative && v.valid(k.slot) && v.cursor.Empty() && !v.slots[k.slot].Empty() {
			it := v.slots[k.slot]
			v.cursor = it.Grow(it.MaxCount() - it.Count())
		}
	case clickThrow:
		v.throw(k.slot, k.button)
	case clickQuickCraft:
		v.quickCraft(k, creative)
	case clickPickupAll:
		v.pickupAll(k.slot, k.button)
	}
	v.commit()
	return &before
}

// mayPickup reports whether the stack in slot js may be taken out (vanilla Slot.mayPickup).
func (v *view) mayPickup(js int) bool {
	return v.refs[js].mayPickup(v.slots[js], v.creative)
}

// valid reports whether js is a slot of the window.
func (v *view) valid(js int) bool { return js >= 0 && js < v.n && v.refs[js].kind != kindNone }

func (v *view) pickup(js, button int) {
	if button != 0 && button != 1 {
		return
	}
	if js == slotOutside {
		if !v.cursor.Empty() {
			n := v.cursor.Count()
			if button == 1 {
				n = 1
			}
			v.drops = append(v.drops, drop{cursorDrop, v.cursor.Grow(n - v.cursor.Count())})
			v.cursor = v.cursor.Grow(-n)
		}
		return
	}
	if !v.valid(js) {
		return
	}
	if js == v.res {
		res := v.slots[js]
		switch {
		case res.Empty() || !v.mayTakeResult():
		case v.cursor.Empty():
			v.cursor = res
			v.takeResult()
		case v.cursor.Comparable(res) && v.cursor.Count()+res.Count() <= v.cursor.MaxCount():
			v.cursor = v.cursor.Grow(res.Count())
			v.takeResult()
		}
		return
	}
	r := &v.refs[js]
	it, cur := v.slots[js], v.cursor
	switch {
	case it.Empty():
		n := cur.Count()
		if button == 1 {
			n = 1
		}
		v.insert(js, n)
	case !v.mayPickup(js):
		// Curse of Binding on worn armour (vanilla ArmorSlot.mayPickup): no taking, no swapping.
	case cur.Empty():
		n := it.Count()
		if button == 1 {
			n = (n + 1) / 2
		}
		v.cursor = it.Grow(n - it.Count())
		v.slots[js] = it.Grow(-n)
	case r.mayPlace(cur):
		if it.Comparable(cur) {
			n := cur.Count()
			if button == 1 {
				n = 1
			}
			v.insert(js, n)
		} else if cur.Count() <= r.maxIn(cur) {
			v.slots[js], v.cursor = cur, it
		}
	case it.Comparable(cur):
		// A slot that takes nothing (furnace output): take all of it if it fits on the cursor.
		// Vanilla's Slot.tryRemove does the same: allowModification is false for such a slot, so
		// it refuses a limit below the slot's count instead of taking part of it.
		if it.Count() <= cur.MaxCount()-cur.Count() {
			v.cursor = cur.Grow(it.Count())
			v.slots[js] = item.Stack{}
		}
	}
}

// insert puts up to n of the cursor into slot js (Slot.safeInsert).
func (v *view) insert(js, n int) {
	r, cur := &v.refs[js], v.cursor
	if cur.Empty() || !r.mayPlace(cur) {
		return
	}
	it := v.slots[js]
	have := 0
	if !it.Empty() {
		if !it.Comparable(cur) {
			return
		}
		have = it.Count()
	}
	n = min(n, cur.Count(), r.maxIn(cur)-have)
	if n <= 0 {
		return
	}
	v.slots[js] = cur.Grow(have + n - cur.Count())
	v.cursor = cur.Grow(-n)
}

// moveStack moves as much of *it as fits into slots [from, to): first onto equal stacks, then into
// one empty slot (AbstractContainerMenu.moveItemStackTo). It reports whether anything moved.
func (v *view) moveStack(it *item.Stack, from, to int, backwards bool) bool {
	changed := false
	start, step := from, 1
	if backwards {
		start, step = to-1, -1
	}
	in := func(js int) bool { return js >= from && js < to }
	if it.MaxCount() > 1 {
		for js := start; !it.Empty() && in(js); js += step {
			r, s := &v.refs[js], v.slots[js]
			if s.Empty() || r.kind == kindResult || r.kind == kindNone || !s.Comparable(*it) {
				continue
			}
			if n := min(it.Count(), r.maxIn(s)-s.Count()); n > 0 {
				v.slots[js] = s.Grow(n)
				*it = it.Grow(-n)
				changed = true
			}
		}
	}
	if !it.Empty() {
		for js := start; in(js); js += step {
			r := &v.refs[js]
			if v.slots[js].Empty() && r.inv != nil && r.mayPlace(*it) {
				n := min(it.Count(), r.maxIn(*it))
				v.slots[js] = it.Grow(n - it.Count())
				*it = it.Grow(-n)
				changed = true
				break
			}
		}
	}
	return changed
}

// addToInventory adds a stack to the player's inventory part of the window like Inventory.add
// (onto equal stacks, then empty slots, hotbar first) and returns what did not fit.
func (v *view) addToInventory(it item.Stack) item.Stack {
	m := v.m
	order := func(f func(js int) bool) {
		for i := range 9 {
			if !f(m.hotbar(i)) {
				return
			}
		}
		for js := m.invStart(); js < m.hotbarStart(); js++ {
			if !f(js) {
				return
			}
		}
	}
	order(func(js int) bool {
		s := v.slots[js]
		if !s.Empty() && s.Comparable(it) {
			n := min(it.Count(), s.MaxCount()-s.Count())
			if n > 0 {
				v.slots[js] = s.Grow(n)
				it = it.Grow(-n)
			}
		}
		return !it.Empty()
	})
	order(func(js int) bool {
		if v.slots[js].Empty() {
			n := min(it.Count(), it.MaxCount())
			v.slots[js] = it.Grow(n - it.Count())
			it = it.Grow(-n)
		}
		return !it.Empty()
	})
	return it
}

// quickMove is a shift-click: vanilla repeats quickMoveStack while the slot still holds the same
// item (crafting as many as possible from a result slot).
func (v *view) quickMove(js int) {
	for range 256 {
		before := v.slots[js]
		if before.Empty() || !v.quickMoveOnce(js) {
			return
		}
		if !sameKindStacks(v.slots[js], before) {
			return
		}
	}
}

// quickMoveOnce is one quickMoveStack. It reports whether anything moved.
func (v *view) quickMoveOnce(js int) bool {
	if js == v.res {
		res := v.slots[js]
		if res.Empty() || !v.mayTakeResult() {
			return false
		}
		it := res
		if !v.moveStack(&it, v.m.invStart(), v.m.invEnd(), true) {
			return false
		}
		v.takeResult()
		if !it.Empty() {
			v.drops = append(v.drops, drop{resultDrop, it})
		}
		return true
	}
	it := v.slots[js]
	count := it.Count()
	ok := v.quickRoute(js, &it)
	v.slots[js] = it
	return ok && it.Count() != count
}

// quickRoute moves *it out of slot js where the window's quickMoveStack sends it. It returns false
// where vanilla stops early.
func (v *view) quickRoute(js int, it *item.Stack) bool {
	m := v.m
	inv, hot, end := m.invStart(), m.hotbarStart(), m.invEnd()
	// The usual move between the main inventory and the hotbar.
	between := func() bool {
		if js >= inv && js < hot {
			return v.moveStack(it, hot, end, false)
		}
		if js >= hot && js < end {
			return v.moveStack(it, inv, hot, false)
		}
		return false
	}
	switch m.kind {
	case menuPlayer:
		if js >= 1 && js < slotMain {
			return v.moveStack(it, slotMain, slotOffhand, false)
		}
		for a := slotArmour; a < slotMain; a++ {
			if v.refs[a].mayPlace(*it) && v.slots[a].Empty() {
				return v.moveStack(it, a, a+1, false)
			}
		}
		if _, shield := it.Item().(item.Shield); shield && v.slots[slotOffhand].Empty() {
			return v.moveStack(it, slotOffhand, slotOffhand+1, false)
		}
		if js < slotOffhand {
			return between()
		}
		return v.moveStack(it, slotMain, slotOffhand, false)
	case menuChest, menuShulker, menuHopper:
		if js < m.size {
			return v.moveStack(it, m.size, m.slots(), true)
		}
		return v.moveStack(it, 0, m.size, false)
	case menuFurnace:
		switch {
		case js == 2:
			return v.moveStack(it, inv, end, true)
		case js > 2:
			switch {
			case m.canSmelt(*it):
				return v.moveStack(it, 0, 1, false)
			case isFuel(*it):
				return v.moveStack(it, 1, 2, false)
			}
			return between()
		}
		return v.moveStack(it, inv, end, false)
	case menuBrewing:
		if js < 5 {
			return v.moveStack(it, inv, end, true)
		}
		fuel, ing := &v.refs[4], &v.refs[3]
		switch {
		case fuel.mayPlace(*it):
			if v.moveStack(it, 4, 5, false) {
				return false
			}
			return !ing.mayPlace(*it) || v.moveStack(it, 3, 4, false)
		case ing.mayPlace(*it):
			return v.moveStack(it, 3, 4, false)
		case v.refs[0].mayPlace(*it):
			return v.moveStack(it, 0, 3, false)
		}
		return between()
	case menuCrafting:
		if js < 10 {
			return v.moveStack(it, inv, end, false)
		}
		if v.moveStack(it, 1, 10, false) {
			return true
		}
		return between()
	case menuAnvil, menuSmithing:
		res := m.resultSlot()
		if js < res {
			return v.moveStack(it, inv, end, false)
		}
		if v.canMoveIntoInputs(*it) {
			return v.moveStack(it, 0, res, false)
		}
		return between()
	case menuEnchanting:
		switch {
		case js < 2:
			return v.moveStack(it, inv, end, true)
		case v.refs[1].mayPlace(*it):
			return v.moveStack(it, 1, 2, true)
		case !v.slots[0].Empty() || !v.refs[0].mayPlace(*it):
			return false
		}
		v.slots[0] = it.Grow(1 - it.Count())
		*it = it.Grow(-1)
		return true
	case menuGrindstone:
		if js < 2 {
			return v.moveStack(it, inv, end, false)
		}
		if !v.slots[0].Empty() && !v.slots[1].Empty() {
			return between()
		}
		return v.moveStack(it, 0, 2, false)
	case menuStonecutter:
		switch {
		case js == 0:
			return v.moveStack(it, inv, end, false)
		case len(stonecutterFor(*it, v.st.proto)) > 0:
			return v.moveStack(it, 0, 1, false)
		}
		return between()
	case menuLoom:
		if js < 3 {
			return v.moveStack(it, inv, end, false)
		}
		for i := range 3 {
			if v.refs[i].mayPlace(*it) {
				return v.moveStack(it, i, i+1, false)
			}
		}
		return between()
	case menuBeacon:
		switch {
		case js == 0:
			return v.moveStack(it, inv, end, true)
		case v.slots[0].Empty() && v.refs[0].mayPlace(*it) && it.Count() == 1:
			return v.moveStack(it, 0, 1, false)
		}
		return between()
	}
	return false
}

// canMoveIntoInputs is ItemCombinerMenu.canMoveIntoInputSlots: anvils take anything, smithing
// tables only what fits one of their empty input slots.
func (v *view) canMoveIntoInputs(it item.Stack) bool {
	if v.m.kind != menuSmithing {
		return true
	}
	for i := range 3 {
		if v.refs[i].mayPlace(it) && v.slots[i].Empty() {
			return true
		}
	}
	return false
}

// swap swaps a slot with a hotbar slot (number keys, button 0-8) or the off-hand (F, button 40).
func (v *view) swap(js, button int) {
	var hb int
	switch {
	case button >= 0 && button < 9:
		hb = v.m.hotbar(button)
	case button == 40:
		hb = v.m.offhand()
	default:
		return
	}
	if !v.valid(js) {
		return
	}
	src, tgt := v.slots[hb], v.slots[js]
	if js == v.res {
		if src.Empty() && !tgt.Empty() && v.mayTakeResult() {
			v.slots[hb] = tgt
			v.takeResult()
		}
		return
	}
	r := &v.refs[js]
	switch {
	case src.Empty() && tgt.Empty():
	case !tgt.Empty() && !v.mayPickup(js):
	case src.Empty():
		v.slots[hb], v.slots[js] = tgt, item.Stack{}
	case !r.mayPlace(src):
	case src.Count() > r.maxIn(src):
		n := r.maxIn(src)
		v.slots[js] = src.Grow(n - src.Count())
		v.slots[hb] = src.Grow(-n)
		if !tgt.Empty() {
			if left := v.addToInventory(tgt); !left.Empty() {
				v.drops = append(v.drops, drop{resultDrop, left})
			}
		}
	default:
		v.slots[hb], v.slots[js] = tgt, src
	}
}

// throw is Q (button 0, one item) or Ctrl+Q (button 1, the stack) over a slot.
func (v *view) throw(js, button int) {
	if !v.cursor.Empty() || !v.valid(js) || v.slots[js].Empty() || !v.mayPickup(js) {
		return
	}
	if js == v.res {
		for range 256 {
			res := v.slots[js]
			if res.Empty() || !v.mayTakeResult() {
				return
			}
			v.drops = append(v.drops, drop{resultDrop, res})
			v.takeResult()
			if button == 0 || !sameKindStacks(v.slots[js], res) {
				return
			}
		}
		return
	}
	it := v.slots[js]
	n := it.Count()
	if button == 0 {
		n = 1
	}
	v.drops = append(v.drops, drop{js, it.Grow(n - it.Count())})
	v.slots[js] = it.Grow(-n)
}

// quickCraft handles a drag: start (slot -999), one packet per slot, end (slot -999).
func (v *view) quickCraft(k click, creative bool) {
	d := &v.st.drag
	stage, kind := k.button&3, k.button>>2&3
	switch stage {
	case 0:
		*d = dragState{}
		if v.cursor.Empty() || kind > 2 || (kind == 2 && !creative) {
			return
		}
		d.active, d.kind, d.slots = true, kind, d.slots[:0]
	case 1:
		if d.kind == 2 && !creative {
			*d = dragState{} // no longer creative: the drag ends here
			return
		}
		if !d.active || kind != d.kind || !v.valid(k.slot) || len(d.slots) >= maxWindow {
			return
		}
		r, it := &v.refs[k.slot], v.slots[k.slot]
		if !r.mayPlace(v.cursor) || (!it.Empty() && !it.Comparable(v.cursor)) {
			return
		}
		for _, js := range d.slots {
			if js == k.slot {
				return
			}
		}
		if d.kind != 2 && len(d.slots) >= v.cursor.Count() {
			return
		}
		d.slots = append(d.slots, k.slot)
	case 2:
		slots, kind := d.slots, d.kind
		active := d.active
		*d = dragState{slots: slots[:0]}
		if !active || len(slots) == 0 || v.cursor.Empty() || (kind == 2 && !creative) {
			return
		}
		if len(slots) == 1 {
			if kind < 2 { // vanilla: a one-slot drag is a click with that button
				v.pickup(slots[0], kind)
			}
			return
		}
		cur := v.cursor
		left := cur.Count()
		for _, js := range slots {
			r, it := &v.refs[js], v.slots[js]
			if !r.mayPlace(cur) || (!it.Empty() && !it.Comparable(cur)) {
				continue
			}
			var n int
			switch kind {
			case 0:
				n = cur.Count() / len(slots)
			case 1:
				n = 1
			case 2:
				n = cur.MaxCount()
			}
			have := 0
			if !it.Empty() {
				have = it.Count()
			}
			n = min(n, r.maxIn(cur)-have)
			if kind != 2 {
				n = min(n, left)
			}
			if n <= 0 {
				continue
			}
			v.slots[js] = cur.Grow(have + n - cur.Count())
			if kind != 2 {
				left -= n
			}
		}
		if kind != 2 {
			v.cursor = cur.Grow(left - cur.Count())
		}
	}
}

// pickupAll is a double click: gather equal stacks into the cursor, non-full stacks first (button
// 1 goes through the window backwards).
func (v *view) pickupAll(js, button int) {
	cur := v.cursor
	if cur.Empty() || js < 0 || (v.valid(js) && !v.slots[js].Empty()) {
		return
	}
	start, step := 0, 1
	if button != 0 {
		start, step = v.n-1, -1
	}
	limit := cur.MaxCount()
	for pass := 0; pass < 2 && cur.Count() < limit; pass++ {
		for s := start; s >= 0 && s < v.n && cur.Count() < limit; s += step {
			it := v.slots[s]
			if v.refs[s].inv == nil || it.Empty() || !it.Comparable(cur) || (pass == 0 && it.Count() == it.MaxCount()) || !v.mayPickup(s) {
				continue
			}
			n := min(it.Count(), limit-cur.Count())
			cur = cur.Grow(n)
			v.slots[s] = it.Grow(-n)
		}
	}
	v.cursor = cur
}

// commit runs the inventory handlers for every change and applies the click if none cancels it.
// The caller resends the whole window either way.
func (v *view) commit() {
	st := v.st
	ctx := event.C(inventory.Holder(v.c))
	changed := false
	for js := range v.total {
		r := &v.refs[js]
		if r.inv == nil || v.orig[js].Equal(v.slots[js]) {
			continue
		}
		changed = true
		takeAndPlace(ctx, r.inv.Handler(), r.idx, v.orig[js], v.slots[js])
	}
	if !v.ocur.Equal(v.cursor) {
		changed = true
		takeAndPlace(ctx, st.ui.Handler(), cursorUISlot, v.ocur, v.cursor)
	}
	for _, d := range v.drops {
		switch d.from {
		case cursorDrop:
			st.ui.Handler().HandleDrop(ctx, cursorUISlot, d.it)
		case resultDrop:
		default:
			r := &v.refs[d.from]
			r.inv.Handler().HandleDrop(ctx, r.idx, d.it)
		}
	}
	if !changed || ctx.Cancelled() {
		return
	}
	// The drops come first: a player handler may cancel one (Player.Drop returns how many it
	// dropped), and what was not dropped goes back where it came from before anything is applied.
	dropped := false
	var lost []item.Stack
	for _, d := range v.drops {
		n := v.c.Drop(d.it)
		dropped = dropped || n > 0
		if n >= d.it.Count() {
			continue
		}
		if rest := v.putBack(d.from, d.it.Grow(-n)); !rest.Empty() {
			if !dropped {
				return // nothing dropped and nowhere to put it: the click does nothing
			}
			lost = append(lost, rest)
		}
	}
	smelted := v.m.kind == menuFurnace && v.slots[2].Count() < v.orig[2].Count()
	st.applying.Store(true)
	for js := range v.total {
		if r := &v.refs[js]; r.inv != nil && !v.orig[js].Equal(v.slots[js]) {
			_ = r.inv.SetItem(r.idx, v.slots[js])
		}
	}
	if !v.ocur.Equal(v.cursor) {
		_ = st.ui.SetItem(cursorUISlot, v.cursor)
	}
	st.applying.Store(false)
	for _, it := range lost {
		// Only when one click drops several stacks and the handler cancels a later one with the
		// inventory full: as close as it gets.
		if n, err := st.inv.AddItem(it); err != nil && st.s.log != nil {
			st.s.log.Debug("cancelled drop did not fit back", "item", it.Grow(n-it.Count()))
		}
	}
	for _, f := range v.after {
		f()
	}
	if smelted {
		v.furnaceExperience()
	}
}

// putBack puts back the part of a drop that was not dropped: into the cursor or the slot it was
// thrown from, or (results, overflow) into the inventory and then the cursor. It returns what did
// not fit anywhere.
func (v *view) putBack(from int, rest item.Stack) item.Stack {
	merge := func(into *item.Stack, limit int) {
		switch {
		case rest.Empty():
		case into.Empty():
			n := min(rest.Count(), limit)
			*into = rest.Grow(n - rest.Count())
			rest = rest.Grow(-n)
		case into.Comparable(rest):
			n := min(rest.Count(), limit-into.Count())
			if n > 0 {
				*into = into.Grow(n)
				rest = rest.Grow(-n)
			}
		}
	}
	switch {
	case rest.Empty():
	case from == cursorDrop:
		merge(&v.cursor, rest.MaxCount())
	case from >= 0:
		merge(&v.slots[from], v.refs[from].maxIn(rest))
	default:
		if rest = v.addToInventory(rest); !rest.Empty() {
			merge(&v.cursor, rest.MaxCount())
		}
	}
	return rest
}

// takeAndPlace calls HandleTake and HandlePlace for a slot going from before to after.
func takeAndPlace(ctx *inventory.Context, h inventory.Handler, slot int, before, after item.Stack) {
	same := !before.Empty() && !after.Empty() && before.Comparable(after)
	switch {
	case same && after.Count() < before.Count():
		h.HandleTake(ctx, slot, before.Grow(-after.Count()))
	case same && after.Count() > before.Count():
		h.HandlePlace(ctx, slot, after.Grow(-before.Count()))
	case same:
	default:
		if !before.Empty() {
			h.HandleTake(ctx, slot, before)
		}
		if !after.Empty() {
			h.HandlePlace(ctx, slot, after)
		}
	}
}
