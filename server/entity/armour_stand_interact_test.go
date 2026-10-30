package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type testStandUser struct {
	*Ent
	main item.Stack
}

func (u *testStandUser) HeldItems() (item.Stack, item.Stack) { return u.main, item.Stack{} }
func (u *testStandUser) SetHeldItems(m, _ item.Stack)        { u.main = m }
func (u *testStandUser) UsingItem() bool                     { return false }
func (u *testStandUser) ReleaseItem()                        {}
func (u *testStandUser) UseItem()                            {}

func TestArmourStandInteract(t *testing.T) {
	w := world.Config{}.New()
	t.Cleanup(func() { _ = w.Close() })
	pos := mgl64.Vec3{0.5, 64, 0.5}
	mustDo(t, w, func(tx *world.Tx) {
		stand := tx.AddEntity(NewArmourStand(world.EntitySpawnOpts{Position: pos})).(*Ent)
		other := tx.AddEntity(NewArmourStand(world.EntitySpawnOpts{Position: pos.Add(mgl64.Vec3{3, 0, 0})})).(*Ent)
		u := &testStandUser{Ent: other}
		helmet := item.NewStack(item.Helmet{Tier: item.ArmourTierDiamond{}}, 1)
		after, used := stand.Interact(u, helmet, pos.Add(mgl64.Vec3{0, 1.8, 0}))
		if !used || !after.Empty() {
			t.Errorf("helmet: used=%v after=%v", used, after)
		}
		if got := stand.Armour().Helmet(); !got.Equal(helmet) {
			t.Errorf("stand helmet = %v", got)
		}
		sword := item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1)
		if _, used := stand.Interact(u, sword, pos.Add(mgl64.Vec3{0, 1.0, 0})); !used {
			t.Error("sword not taken")
		}
		back, used := stand.Interact(u, item.Stack{}, pos.Add(mgl64.Vec3{0, 1.8, 0}))
		if !used || !back.Equal(helmet) {
			t.Errorf("take helmet: used=%v got=%v", used, back)
		}
	})
}
