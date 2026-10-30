package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// IronDoor is a 1x2 door that cannot be opened by hand: it is open while
// either of its halves is powered by redstone.
type IronDoor struct {
	transparent
	sourceWaterDisplacer

	// Facing is the direction that the door opens towards. When closed, the door sits on the side of its
	// block on the opposite direction.
	Facing cube.Direction
	// Open is whether the door is open.
	Open bool
	// Top is whether the block is the top or bottom half of a door.
	Top bool
	// Right is whether the door hinge is on the right side.
	Right bool
}

// Model ...
func (d IronDoor) Model() world.BlockModel {
	return model.Door{Facing: d.Facing, Open: d.Open, Right: d.Right}
}

// otherHalf is the position of the door's other half.
func (d IronDoor) otherHalf(pos cube.Pos) cube.Pos {
	if d.Top {
		return pos.Side(cube.FaceDown)
	}
	return pos.Side(cube.FaceUp)
}

// NeighbourUpdateTick ...
func (d IronDoor) NeighbourUpdateTick(pos, changedNeighbour cube.Pos, tx *world.Tx) {
	if pos == changedNeighbour {
		return
	}
	if d.Top {
		if _, ok := tx.Block(pos.Side(cube.FaceDown)).(IronDoor); !ok {
			breakBlockNoDrops(d, pos, tx)
		}
	} else if solid := tx.Block(pos.Side(cube.FaceDown)).Model().FaceSolid(pos.Side(cube.FaceDown), cube.FaceUp, tx); !solid {
		breakBlockNoDrops(d, pos, tx)
		dropItem(tx, item.NewStack(d, 1), pos.Vec3Centre())
	} else if _, ok := tx.Block(pos.Side(cube.FaceUp)).(IronDoor); !ok {
		breakBlockNoDrops(d, pos, tx)
	}
}

// RedstonePowerUpdate opens the door while this half or the other one is
// powered.
func (d IronDoor) RedstonePowerUpdate(pos cube.Pos, tx *world.Tx, power int) (world.Block, bool) {
	open := power > 0 || tx.RedstonePower(d.otherHalf(pos)) > 0
	if open == d.Open {
		return d, false
	}
	d.Open = open
	return d, true
}

// RedstonePowerPostUpdate moves the other half with this one and plays the
// door sound.
func (d IronDoor) RedstonePowerPostUpdate(pos cube.Pos, tx *world.Tx, _, after world.Block, _, _ int) {
	a, ok := after.(IronDoor)
	if !ok {
		return
	}
	otherPos := a.otherHalf(pos)
	if other, ok := tx.Block(otherPos).(IronDoor); ok && other.Open != a.Open {
		other.Open = a.Open
		tx.SetBlock(otherPos, other, nil)
	}
	if a.Open {
		tx.PlaySound(pos.Vec3Centre(), sound.DoorOpen{Block: a})
	} else {
		tx.PlaySound(pos.Vec3Centre(), sound.DoorClose{Block: a})
	}
}

// UseOnBlock handles the directional placing of doors
func (d IronDoor) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	if face != cube.FaceUp {
		// Doors can only be placed when clicking the top face.
		return false
	}
	below := pos
	pos = pos.Side(cube.FaceUp)
	if !replaceableWith(tx, pos, d) || !replaceableWith(tx, pos.Side(cube.FaceUp), d) {
		return false
	}
	if !tx.Block(below).Model().FaceSolid(below, cube.FaceUp, tx) {
		return false
	}
	d.Facing = user.Rotation().Direction()
	left := tx.Block(pos.Side(d.Facing.RotateLeft().Face()))
	right := tx.Block(pos.Side(d.Facing.RotateRight().Face()))
	if _, ok := left.Model().(model.Door); ok {
		d.Right = true
	}
	if diffuser, ok := right.(LightDiffuser); !ok || diffuser.LightDiffusionLevel() != 0 {
		if diffuser, ok := left.(LightDiffuser); ok && diffuser.LightDiffusionLevel() == 0 {
			d.Right = true
		}
	}
	d.Open = tx.RedstonePower(pos) > 0 || tx.RedstonePower(pos.Side(cube.FaceUp)) > 0

	ctx.IgnoreBBox = true
	place(tx, pos, d, user, ctx)
	place(tx, pos.Side(cube.FaceUp), IronDoor{Facing: d.Facing, Open: d.Open, Top: true, Right: d.Right}, user, ctx)
	ctx.CountSub = 1
	return placed(ctx)
}

// BreakInfo ...
func (d IronDoor) BreakInfo() BreakInfo {
	return newBreakInfo(5, pickaxeHarvestable, pickaxeEffective, oneOf(IronDoor{})).withBlastResistance(5)
}

// SideClosed ...
func (d IronDoor) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// EncodeItem ...
func (d IronDoor) EncodeItem() (name string, meta int16) {
	return "minecraft:iron_door", 0
}

// EncodeBlock ...
func (d IronDoor) EncodeBlock() (name string, properties map[string]any) {
	return "minecraft:iron_door", map[string]any{"minecraft:cardinal_direction": d.Facing.RotateRight().String(), "door_hinge_bit": d.Right, "open_bit": d.Open, "upper_block_bit": d.Top}
}

// allIronDoors returns every iron door state.
func allIronDoors() (doors []world.Block) {
	for i := cube.Direction(0); i <= 3; i++ {
		for _, open := range []bool{false, true} {
			for _, top := range []bool{false, true} {
				for _, right := range []bool{false, true} {
					doors = append(doors, IronDoor{Facing: i, Open: open, Top: top, Right: right})
				}
			}
		}
	}
	return
}
