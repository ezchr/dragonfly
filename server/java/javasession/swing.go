package javasession

import (
	"sync"
	"sync/atomic"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/world"
)

// selfSwings counts, per session, the arm swings the server made the player do itself. A Java
// client swings its own arm when it expects a click to do something, so these are not sent back
// to it, except for clicks Java expects to do nothing (see bedrockOnlyUse).
var selfSwings sync.Map // *Session -> *atomic.Int64

func (s *Session) selfSwingCount() *atomic.Int64 {
	n, _ := selfSwings.LoadOrStore(s, new(atomic.Int64))
	return n.(*atomic.Int64)
}

// bedrockOnlyUse reports whether using held on b does something only Bedrock has, so that the
// Java client neither swings its arm nor shows the result: a dye or leather armour on a cauldron.
func bedrockOnlyUse(b world.Block, held item.Stack) bool {
	if _, ok := b.(block.Cauldron); !ok {
		return false
	}
	switch it := held.Item().(type) {
	case item.Dye:
		return true
	case item.Helmet:
		_, ok := it.Tier.(item.ArmourTierLeather)
		return ok
	case item.Chestplate:
		_, ok := it.Tier.(item.ArmourTierLeather)
		return ok
	case item.Leggings:
		_, ok := it.Tier.(item.ArmourTierLeather)
		return ok
	case item.Boots:
		_, ok := it.Tier.(item.ArmourTierLeather)
		return ok
	}
	return false
}

// swingSelf swings the player's own arm on their screen.
func (s *Session) swingSelf() {
	if !s.ver.Native() {
		w := s.packet()
		w.VarInt(selfEntityID)
		w.Byte(animate262SwingMainHand)
		s.queue(v777.ClientboundPlayAnimate, w)
		return
	}
	w := s.packet()
	w.VarInt(selfEntityID)
	w.VarInt(0) // main hand
	w.VarInt(1) // SwingAnimationType.WHACK
	w.VarInt(6) // duration in ticks (SwingAnimation.DEFAULT)
	s.queue(v777.ClientboundPlaySwingAnimation, w)
}
