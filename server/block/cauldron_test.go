package block

import (
	"context"
	"image/color"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type cauldronUser struct{ held item.Stack }

func (u *cauldronUser) Close() error                        { return nil }
func (u *cauldronUser) H() *world.EntityHandle              { return nil }
func (u *cauldronUser) Position() mgl64.Vec3                { return mgl64.Vec3{} }
func (u *cauldronUser) Rotation() cube.Rotation             { return cube.Rotation{} }
func (u *cauldronUser) HeldItems() (item.Stack, item.Stack) { return u.held, item.Stack{} }
func (u *cauldronUser) SetHeldItems(m, _ item.Stack)        { u.held = m }
func (u *cauldronUser) UsingItem() bool                     { return false }
func (u *cauldronUser) ReleaseItem()                        {}
func (u *cauldronUser) UseItem()                            {}

func TestCauldron(t *testing.T) {
	w := world.Config{}.New()
	t.Cleanup(func() { _ = w.Close() })
	pos := cube.Pos{0, 64, 0}
	use := func(tx *world.Tx, held item.Stack) (bool, *item.UseContext) {
		ctx := &item.UseContext{}
		return tx.Block(pos).(Cauldron).Activate(pos, cube.FaceUp, tx, &cauldronUser{held: held}, ctx), ctx
	}
	at := func(tx *world.Tx) Cauldron { return tx.Block(pos).(Cauldron) }
	err := w.Do(func(tx *world.Tx) {
		tx.SetBlock(pos, Cauldron{}, nil)
		waterBucket := item.NewStack(item.Bucket{Content: item.LiquidBucketContent(Water{Still: true, Depth: 8})}, 1)
		if ok, ctx := use(tx, waterBucket); !ok || at(tx).Level != 6 || at(tx).Liquid != CauldronWater() {
			t.Errorf("water bucket: ok=%v %+v", ok, at(tx))
		} else if _, isBucket := ctx.NewItem.Item().(item.Bucket); !isBucket || ctx.CountSub != 1 {
			t.Errorf("water bucket gave %v", ctx.NewItem)
		}
		if ok, ctx := use(tx, item.NewStack(item.GlassBottle{}, 1)); !ok || at(tx).Level != 4 || ctx.NewItem.Item() != (item.Potion{Type: potion.Water()}) {
			t.Errorf("bottle: ok=%v level=%d new=%v", ok, at(tx).Level, ctx.NewItem)
		}
		if ok, _ := use(tx, item.NewStack(item.Dye{Colour: item.ColourRed()}, 1)); !ok || at(tx).Colour == (color.RGBA{}) {
			t.Errorf("dye: ok=%v %+v", ok, at(tx))
		}
		boots := item.NewStack(item.Boots{Tier: item.ArmourTierLeather{}}, 1)
		ok, ctx := use(tx, boots)
		if b, isBoots := ctx.NewItem.Item().(item.Boots); !ok || !isBoots || b.Tier.(item.ArmourTierLeather).Colour == (color.RGBA{}) || at(tx).Level != 3 {
			t.Errorf("dye boots: ok=%v new=%v level=%d", ok, ctx.NewItem, at(tx).Level)
		}
		if ok, _ := use(tx, item.NewStack(item.Bucket{}, 1)); ok {
			t.Error("emptied a cauldron that is not full")
		}
		lavaBucket := item.NewStack(item.Bucket{Content: item.LiquidBucketContent(Lava{Still: true, Depth: 8})}, 1)
		if ok, _ := use(tx, lavaBucket); !ok || at(tx).Level != 0 {
			t.Errorf("lava into water should empty it: %+v", at(tx))
		}
		if ok, _ := use(tx, lavaBucket); !ok || at(tx).Liquid != CauldronLava() || at(tx).LightEmissionLevel() != 15 {
			t.Errorf("lava: %+v", at(tx))
		}
		if ok, ctx := use(tx, item.NewStack(item.Bucket{}, 1)); !ok || at(tx).Level != 0 || ctx.NewItem.Empty() {
			t.Errorf("take lava: ok=%v %+v", ok, at(tx))
		}
		m := (Cauldron{Level: 6, Colour: color.RGBA{1, 2, 3, 0xff}}).EncodeNBT()
		if back := (Cauldron{}).DecodeNBT(m).(Cauldron); back.Colour != (color.RGBA{1, 2, 3, 0xff}) {
			t.Errorf("nbt colour %v", back.Colour)
		}
	}).Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}
