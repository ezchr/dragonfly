package javasession

import (
	"bytes"
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestCustomName(t *testing.T) {
	w, _, s := testWorld(t)
	do(w, func(tx *world.Tx) {
		h := entity.NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{3, 1, 3}, NameTag: "Trader"}, item.NewStack(item.Apple{}, 1))
		e := tx.AddEntity(h)
		s.viewCustomName(e, 7)
		out := s.take()
		if len(out) != 1 || out[0].id != v777.ClientboundPlaySetEntityData {
			t.Fatalf("want one set_entity_data, got %d packets", len(out))
		}
		b := out[0].w.B
		// entity id 7, index 2, optional component, present
		if !bytes.HasPrefix(b, []byte{7, dataCustomName, dataTypeOptComponent, 1}) {
			t.Fatalf("bad start % x", b[:min(len(b), 8)])
		}
		// visible (name tags with no always-show state are always shown), then the end marker
		if !bytes.HasSuffix(b, []byte{dataCustomNameVisible, dataTypeBoolean, 1, 0xff}) {
			t.Fatalf("bad end % x", b[max(len(b)-6, 0):])
		}
		s.viewCustomName(e, 7)
		if n := len(s.take()); n != 0 {
			t.Fatalf("unchanged name sent again (%d packets)", n)
		}
		e.(*entity.Ent).SetNameTag("")
		s.viewCustomName(e, 7)
		out = s.take()
		if len(out) != 1 || !bytes.HasPrefix(out[0].w.B, []byte{7, dataCustomName, dataTypeOptComponent, 0}) {
			t.Fatalf("removed name not cleared")
		}
	})
}
