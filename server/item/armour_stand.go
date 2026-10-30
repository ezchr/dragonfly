package item

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// ArmourStand is an item that places an armour stand entity, which can wear
// armour and hold items.
type ArmourStand struct{}

// UseOnBlock places an armour stand on the clicked face, facing the user in
// steps of 45 degrees. The two blocks it takes up must be air or replaceable,
// such as grass, which is removed, and no other armour stand may stand there.
func (ArmourStand) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user User, ctx *UseContext) bool {
	if g, ok := user.(interface{ GameMode() world.GameMode }); ok && !g.GameMode().AllowsEditing() {
		return false
	}
	at := pos.Side(face)
	if placeableIn(tx, pos) {
		at = pos
	}
	above := at.Side(cube.FaceUp)
	if at.OutOfBounds(tx.Range()) || above.OutOfBounds(tx.Range()) || !placeableIn(tx, at) || !placeableIn(tx, above) {
		return false
	}
	box := cube.Box(0, 0, 0, 1, 1, 1).Translate(at.Vec3())
	for e := range tx.EntitiesWithin(box.Grow(1)) {
		if e.H().Type().EncodeEntity() == "minecraft:armor_stand" && e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(box) {
			return false
		}
	}
	for _, p := range []cube.Pos{at, above} {
		if tx.Block(p) != air() {
			tx.SetBlock(p, nil, nil)
		}
	}

	yaw := math.Round((user.Rotation().Yaw()+180)/45)*45 - 360
	opts := world.EntitySpawnOpts{Position: at.Vec3Middle(), Rotation: cube.Rotation{yaw, 0}}
	if held, _ := user.HeldItems(); held.Item() == (ArmourStand{}) {
		opts.NameTag = held.CustomName()
	}
	tx.AddEntity(tx.World().EntityRegistry().Config().ArmourStand(opts))
	tx.PlaySound(at.Vec3Middle(), sound.ArmourStandPlace{})
	ctx.SubtractFromCount(1)
	return true
}

// placeableIn reports whether the block at pos is air or may be replaced by
// another block, like tall grass or a snow layer.
func placeableIn(tx *world.Tx, pos cube.Pos) bool {
	b := tx.Block(pos)
	if b == air() {
		return true
	}
	r, ok := b.(replaceable)
	return ok && r.ReplaceableBy(air())
}

// EncodeItem ...
func (ArmourStand) EncodeItem() (name string, meta int16) {
	return "minecraft:armor_stand", 0
}
