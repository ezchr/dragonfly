package javasession

import (
	"reflect"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	v777 "github.com/ezchr/go-mcjava/v777"
	"github.com/ezchr/go-mcjava/version"
	"github.com/ezchr/go-mcjava/wire"
)

// menu returns the open window (the player's inventory if no block window is open).
func (st *itemState) menu() *menu {
	if m := st.open.Load(); m != nil {
		return m
	}
	return &st.player
}

// nextWindowID is vanilla's container counter: 1-100, then around again.
func (st *itemState) nextWindowID() int32 {
	st.windowID = st.windowID%100 + 1
	return st.windowID
}

// OpenBlockContainer opens the window of the block at pos (chests, furnaces, crafting tables...).
func (s *Session) OpenBlockContainer(pos cube.Pos, tx *world.Tx) {
	st := s.items()
	_, c := st.current()
	if c == nil || st.inv == nil {
		return
	}
	if m := st.open.Load(); m != nil && m.pos == pos && s.menuStillValid(tx, c, m) {
		return // already open (a window that is no longer valid is replaced by a fresh one)
	}
	if _, _, ok := menuFor(tx.Block(pos)); !ok {
		return
	}
	s.closeMenu(tx, c, true)

	b := tx.Block(pos)
	switch cb := b.(type) {
	case block.EnderChest:
		cb.AddViewer(tx, pos)
	case block.Container:
		cb.AddViewer(s, tx, pos) // a chest may pair with its neighbour here
		b = tx.Block(pos)
	}
	m, title, ok := menuFor(b)
	if !ok {
		return
	}
	m.pos, m.w = pos, tx.World()
	switch {
	case m.ender:
		m.inv = c.EnderChestInventory()
	case m.kind == menuChest || m.kind == menuShulker || m.kind == menuHopper || m.kind == menuFurnace || m.kind == menuBrewing:
		m.inv = b.(block.Container).Inventory(tx, pos)
		if m.kind == menuChest && m.inv.Size() != m.size {
			m.size = m.inv.Size()
		}
	}
	m.id = st.nextWindowID()
	st.drag = dragState{}
	st.open.Store(m)

	w := s.packet()
	w.VarInt(m.id)
	w.VarInt(version.Map(st.menus, m.typ))
	title.Write(w)
	s.queue(v777.ClientboundPlayOpenScreen, w)
	s.syncWindow(tx, c, m)
}

// closeMenu closes the open block window (if any): the block forgets the viewer and the items in
// the window's input slots and on the cursor go back to the inventory. send tells the client
// (it is false when the client closed the window itself).
func (s *Session) closeMenu(tx *world.Tx, c session.Controllable, send bool) {
	st := s.items()
	m := st.open.Swap(nil)
	if m == nil {
		return
	}
	st.drag = dragState{}
	if send {
		w := s.packet()
		w.VarInt(m.id)
		s.queue(v777.ClientboundPlayContainerClose, w)
	}
	switch {
	case tx.World() == m.w:
		s.removeViewer(tx, m)
	case m.w != nil:
		// The player is in another world now: the block forgets the viewer in its own world (else
		// the lid stays open and the block keeps the session).
		m.w.Do(func(tx *world.Tx) { s.removeViewer(tx, m) })
	}
	st.applying.Store(true)
	c.MoveItemsToInventory()
	st.applying.Store(false)
	s.sendInventory()
	if m.virtual && m.onClose != nil {
		m.onClose()
	}
}

// removeViewer tells the block of window m (in tx's world) that the session no longer views it.
func (s *Session) removeViewer(tx *world.Tx, m *menu) {
	if m.virtual {
		return // no block
	}
	switch b := tx.Block(m.pos).(type) {
	case block.EnderChest:
		if m.ender {
			b.RemoveViewer(tx, m.pos)
		}
	case block.Container:
		b.RemoveViewer(s, tx, m.pos)
	}
}

// menuStillValid is vanilla's stillValid for the open window, checked before every window action
// (clicks, buttons, anvil names, beacons) and every few ticks: same world, the same kind of block
// within 8 blocks and, for blocks with an inventory, still the same inventory. Dragonfly gives a
// chest a new inventory when it pairs or unpairs, and the old one must not be used any more (it
// still holds the items: taking them would duplicate them).
func (s *Session) menuStillValid(tx *world.Tx, c session.Controllable, m *menu) bool {
	if m.kind == menuPlayer || m.virtual {
		return true
	}
	if tx.World() != m.w || c.Position().Sub(m.pos.Vec3Centre()).Len() > 8 {
		return false
	}
	b := tx.Block(m.pos)
	if reflect.TypeOf(b) != m.btype {
		return false
	}
	switch {
	case m.ender:
		return m.inv == c.EnderChestInventory()
	case m.inv != nil:
		cb, ok := b.(block.Container)
		return ok && cb.Inventory(tx, m.pos) == m.inv
	}
	return true
}

// closeWindows closes the open block window and puts the items of the UI inventory back. For the
// world change (dimension.go switchWorld): the client closes its screen for the new world.
func (s *Session) closeWindows(tx *world.Tx, c session.Controllable) {
	s.closeMenu(tx, c, true)
}

// closeWindowsAfterRespawn closes the open block window once the player respawned (self.go
// SendRespawn): the respawn packet took the client back to its own inventory, so the server must
// not keep the block window open. It runs in a transaction of its own.
func (s *Session) closeWindowsAfterRespawn() {
	m := s.items().open.Load()
	if m == nil {
		return
	}
	go s.do(func(tx *world.Tx, c session.Controllable) {
		if s.items().open.Load() == m {
			s.closeMenu(tx, c, false)
		}
	})
}

// clientClosed handles container_close. Like vanilla, the window id hardly matters: whatever is
// open closes.
func (s *Session) clientClosed(tx *world.Tx, c session.Controllable, window int32) {
	st := s.items()
	if st.open.Load() != nil {
		s.closeMenu(tx, c, false)
		return
	}
	st.drag = dragState{}
	if window == windowPlayer && st.inv != nil {
		// Vanilla puts the cursor and the crafting grid back into the inventory, or drops them.
		st.applying.Store(true)
		c.MoveItemsToInventory()
		st.applying.Store(false)
		s.sendInventory()
	}
}

// checkMenu closes the open window when the player walked away from the block or the block is gone
// (vanilla stillValid). It is called from HandleInventories, so at most every few ticks it looks.
func (s *Session) checkMenu(tx *world.Tx, c session.Controllable) {
	st := s.items()
	m := st.open.Load()
	if m == nil {
		return
	}
	now := time.Now()
	if now.Sub(m.lastCheck) < 200*time.Millisecond {
		return
	}
	m.lastCheck = now
	if m.virtual {
		s.refreshVirtual(m)
	}
	if s.menuStillValid(tx, c, m) {
		return
	}
	// HandleInventories runs while the player entity is being opened: close in a transaction of its own.
	go s.do(func(tx *world.Tx, c session.Controllable) {
		if st.open.Load() == m {
			s.closeMenu(tx, c, true)
		}
	})
}

// syncWindow sends the whole window (and its data) again: the click engine resyncs after every
// click instead of trusting the client's prediction.
func (s *Session) syncWindow(tx *world.Tx, c session.Controllable, m *menu) {
	if m.kind == menuPlayer {
		s.sendInventory()
		return
	}
	st := s.items()
	if st.inv == nil {
		return
	}
	v := st.newView(tx, c, m)
	w := s.packet()
	w.VarInt(m.id)
	w.VarInt(st.nextStateID())
	w.VarInt(int32(v.n))
	for js := range v.n {
		s.writeStack(w, v.slots[js])
	}
	s.writeStack(w, v.cursor)
	s.queue(v777.ClientboundPlayContainerSetContent, w)
	s.syncMenuData(tx, c, m, v)
}

// syncClick sends what a click changed (vanilla broadcastChanges): the slots whose stack is not
// what it was before the click, the slots the client changed itself (its prediction, right or
// wrong), the cursor and the data slots that changed. A click costs a few bytes back, not the whole
// window.
func (s *Session) syncClick(tx *world.Tx, c session.Controllable, m *menu, before *[maxWindow]item.Stack, changed []int16) {
	st := s.items()
	if st.inv == nil {
		return
	}
	v := st.newView(tx, c, m)
	if v.total > v.n {
		// The hidden off-hand of a block window (F) is slot 45 of window 0. First, so the last
		// state id the client gets is the window's.
		if off := m.offhand(); !s.sameStack(before[off], v.slots[off]) {
			s.sendSlot(slotOffhand, v.slots[off])
		}
	}
	var sent [maxWindow]bool
	send := func(js int) {
		if sent[js] {
			return
		}
		sent[js] = true
		if m.kind == menuPlayer {
			s.sendSlot(js, v.slots[js])
		} else {
			s.sendWindowSlot(m.id, js, v.slots[js])
		}
	}
	for js := range v.n {
		if !s.sameStack(before[js], v.slots[js]) {
			send(js)
		}
	}
	for _, js := range changed {
		if js >= 0 && int(js) < v.n {
			send(int(js))
		}
	}
	s.sendCursor(v.cursor)
	s.syncMenuData(tx, c, m, v)
}

// sameStack reports whether a and b look the same to the client.
func (s *Session) sameStack(a, b item.Stack) bool {
	if a.Empty() || b.Empty() {
		return a.Empty() == b.Empty()
	}
	if !a.Equal(b) {
		return false
	}
	var wa, wb wire.Writer
	s.writeStack(&wa, a)
	s.writeStack(&wb, b)
	return string(wa.B) == string(wb.B)
}

// syncMenuData sends the window's data slots (progress bars, costs) that changed, all of them the
// first time like vanilla's initMenu.
func (s *Session) syncMenuData(tx *world.Tx, c session.Controllable, m *menu, v *view) {
	var vals [10]int32
	n := 0
	switch m.kind {
	case menuFurnace:
		n = 4
		if d, ok := tx.Block(m.pos).(interface {
			Durations() (remaining, max, cook time.Duration)
		}); ok {
			rem, mx, cook := d.Durations()
			vals = [10]int32{durTicks(rem), durTicks(mx), durTicks(cook), m.cookTotal}
		}
	case menuBrewing:
		n = v.st.brewingData
		if b, ok := tx.Block(m.pos).(interface {
			Duration() time.Duration
			Fuel() (int32, int32)
		}); ok {
			fuel, total := b.Fuel()
			vals = [10]int32{durTicks(b.Duration()), fuel, 400, total}
		}
	case menuAnvil:
		n, vals[0] = 1, int32(m.cost)
	case menuSmithing:
		n = 1 // has recipe error: never
	case menuStonecutter:
		n, vals[0] = 1, int32(m.sel)
	case menuEnchanting:
		n, vals = 10, enchantData(tx, c, m.pos, v.slots[0], v.st.proto)
	case menuBeacon:
		n = 3
		if b, ok := tx.Block(m.pos).(block.Beacon); ok {
			vals[0], vals[1], vals[2] = int32(b.Level()), beaconEffectData(b.Primary, v.st.proto), beaconEffectData(b.Secondary, v.st.proto)
		}
	}
	for i := range n {
		if m.dataSent && m.data[i] == vals[i] {
			continue
		}
		m.data[i] = vals[i]
		s.sendData(m.id, i, vals[i])
	}
	m.dataN, m.dataSent = n, true
}

func (s *Session) sendData(window int32, key int, val int32) {
	w := s.packet()
	w.VarInt(window)
	w.Int16(int16(key))
	w.Int16(int16(val))
	s.queue(v777.ClientboundPlayContainerSetData, w)
}

func durTicks(d time.Duration) int32 { return int32(d / (50 * time.Millisecond)) }

// ViewSlotChange shows a change in the open block's inventory (another player, a hopper, smelting).
func (s *Session) ViewSlotChange(slot int, it item.Stack) {
	st := s.items()
	m := st.open.Load()
	if m == nil || m.inv == nil || m.ender || st.applying.Load() {
		return
	}
	js := slot
	if m.kind == menuBrewing {
		js = brewingWindowSlot(slot)
	}
	if js >= m.size {
		return
	}
	s.sendWindowSlot(m.id, js, it)
}

func (s *Session) sendWindowSlot(window int32, js int, it item.Stack) {
	w := s.packet()
	w.VarInt(window)
	w.VarInt(s.items().nextStateID())
	w.Int16(int16(js))
	s.writeStack(w, it)
	s.queue(v777.ClientboundPlayContainerSetSlot, w)
}

// ViewFurnaceUpdate updates the progress arrow and flame of the open furnace.
func (s *Session) ViewFurnaceUpdate(prevCook, cook, prevRemaining, remaining, prevMax, max time.Duration) {
	m := s.items().open.Load()
	if m == nil || m.kind != menuFurnace {
		return
	}
	s.updateData(m, 0, prevRemaining != remaining, durTicks(remaining))
	s.updateData(m, 1, prevMax != max, durTicks(max))
	s.updateData(m, 2, prevCook != cook, durTicks(cook))
}

// ViewBrewingUpdate updates the bubbles and fuel bar of the open brewing stand.
func (s *Session) ViewBrewingUpdate(prevBrew, brew time.Duration, prevFuel, fuel, prevTotal, total int32) {
	m := s.items().open.Load()
	if m == nil || m.kind != menuBrewing {
		return
	}
	s.updateData(m, 0, prevBrew != brew, durTicks(brew))
	s.updateData(m, 1, prevFuel != fuel, fuel)
	if s.items().brewingData > 3 {
		s.updateData(m, 3, prevTotal != total, total)
	}
}

func (s *Session) updateData(m *menu, key int, changed bool, val int32) {
	if !changed || (m.dataSent && m.data[key] == val) {
		return
	}
	m.data[key] = val
	s.sendData(m.id, key, val)
}

// enderSlotChanged shows a change in the ender chest while it is open (a slot func of the ender
// chest inventory).
func (s *Session) enderSlotChanged(slot int, it item.Stack) {
	st := s.items()
	m := st.open.Load()
	if m == nil || !m.ender || st.applying.Load() {
		return
	}
	s.sendWindowSlot(m.id, slot, it)
}

// uiSlotChanged shows a change in the UI inventory made outside a click (plugins, closing).
func (s *Session) uiSlotChanged(slot int, it item.Stack) {
	st := s.items()
	if st.applying.Load() {
		return
	}
	m := st.menu()
	if m.kind == menuPlayer {
		if slot >= uiCraftSmall && slot < uiCraftSmall+4 {
			s.sendSlot(1+slot-uiCraftSmall, it)
			s.sendSlot(0, st.playerCraftResult())
		}
		return
	}
	for js, sec := range uiSection[m.kind] {
		if sec.ui == slot {
			s.sendWindowSlot(m.id, js, it)
		}
	}
}

// closeContainers is for Session.Close, before the player is saved: it closes the open window
// without telling the client (the block forgets the viewer) and puts the items of the UI inventory
// (crafting grid, station inputs, cursor) back into the inventory, as the Bedrock session does.
//
// The session's inventory state goes at the end (releaseItems): not before, or the window would
// be lost and its items with it.
func (s *Session) closeContainers(tx *world.Tx, c session.Controllable) {
	defer s.releaseItems()
	st := s.items()
	if st.inv == nil || tx == nil || c == nil {
		return
	}
	if st.open.Load() != nil {
		s.closeMenu(tx, c, false)
		return
	}
	st.applying.Store(true)
	c.MoveItemsToInventory()
	st.applying.Store(false)
}
