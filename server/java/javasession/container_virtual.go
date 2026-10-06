package javasession

import (
	"fmt"
	"sync"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	v777 "github.com/ezchr/go-mcjava/v777"
	"github.com/ezchr/go-mcjava/version"
	"github.com/google/uuid"
)

// Inventory windows with no block behind them: a plugin's own view of an inventory, such as
// /invsee. Bedrock players get these from bedrock-gophers/inv, which fakes a chest in the
// Bedrock session; Java players have none, so a plugin opens them here instead. The window is a
// generic chest (9 wide, inventory size / 9 rows) and clicks go straight into the inventory.

var javaSessions sync.Map // Dragonfly player UUID -> *Session, while spawned

// OpenInventory opens a chest window on inv for p, if p is a Java player, and reports whether it
// did. The window shows title (Bedrock colour codes work) and closes like any other; onClose (if
// not nil) is called when it does. inv's size must be a multiple of 9, at most 54. Changes made
// to inv by someone else show within a few ticks. Call it in p's transaction.
func OpenInventory(p *player.Player, tx *world.Tx, title string, inv *inventory.Inventory, onClose func()) bool {
	v, ok := javaSessions.Load(p.UUID())
	if !ok {
		return false
	}
	n := inv.Size()
	if n <= 0 || n > 54 || n%9 != 0 {
		return false
	}
	s := v.(*Session)
	st := s.items()
	if st.inv == nil {
		return false
	}
	s.closeMenu(tx, p, true)
	m := &menu{
		kind: menuChest, size: n, inv: inv, virtual: true, onClose: onClose, sel: -1,
		typ: v777.BuiltinID("minecraft:menu", fmt.Sprintf("minecraft:generic_9x%d", n/9)),
		w:   tx.World(),
	}
	m.id = st.nextWindowID()
	st.drag = dragState{}
	st.open.Store(m)

	w := s.packet()
	w.VarInt(m.id)
	w.VarInt(version.Map(st.menus, m.typ))
	t := bedrockText(title)
	t.Write(w)
	s.queue(v777.ClientboundPlayOpenScreen, w)
	s.syncWindow(tx, p, m)
	m.shown = inv.Slots()
	return true
}

// CloseInventory closes the window OpenInventory opened for p, if it is still open.
func CloseInventory(p *player.Player, tx *world.Tx) {
	v, ok := javaSessions.Load(p.UUID())
	if !ok {
		return
	}
	s := v.(*Session)
	if m := s.items().open.Load(); m != nil && m.virtual {
		s.closeMenu(tx, p, true)
	}
}

// refreshVirtual sends the slots of a virtual window that changed since they were last sent:
// nothing tells a viewer when someone else changes the inventory.
func (s *Session) refreshVirtual(m *menu) {
	now := m.inv.Slots()
	for i, it := range now {
		if i < len(m.shown) && sameItem(m.shown[i], it) {
			continue
		}
		s.sendWindowSlot(m.id, i, it)
	}
	m.shown = now
}

// sameItem reports whether two stacks look the same to the client.
func sameItem(a, b item.Stack) bool {
	return a.Count() == b.Count() && a.Comparable(b)
}

func trackJavaSession(id uuid.UUID, s *Session) { javaSessions.Store(id, s) }
func untrackJavaSession(id uuid.UUID)           { javaSessions.Delete(id) }

var _ session.Controllable = (*player.Player)(nil)
