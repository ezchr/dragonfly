package javasession

import (
	"fmt"
	"os"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// containerTest (JAVASESSION_CONTAINER_TEST=1, for test servers only) builds a row of every block
// with a window around each joining Java player, gives them items to use in them and tells them
// where it all is with a chat line "CONTTEST x y z".
var containerTest = os.Getenv("JAVASESSION_CONTAINER_TEST") != ""

// containerTestBlocks are the test blocks, as offsets from the player's feet.
var containerTestBlocks = []struct {
	off cube.Pos
	b   world.Block
}{
	{cube.Pos{2, 0, -2}, block.NewChest()},
	{cube.Pos{2, 0, -1}, block.NewEnderChest()},
	{cube.Pos{2, 0, 0}, block.NewBarrel()},
	{cube.Pos{2, 0, 1}, func() world.Block { s := block.NewShulkerBox(); s.Facing = cube.FaceUp; return s }()},
	{cube.Pos{2, 0, 2}, block.NewHopper()},
	{cube.Pos{2, 0, 3}, block.NewFurnace(cube.North)},
	{cube.Pos{2, 0, 4}, block.NewBlastFurnace(cube.North)},
	{cube.Pos{-2, 0, -4}, block.NewSmoker(cube.North)},
	{cube.Pos{-2, 0, -3}, block.NewBrewingStand()},
	{cube.Pos{-2, 0, -2}, block.CraftingTable{}},
	{cube.Pos{-2, 0, -1}, block.Anvil{}},
	{cube.Pos{-2, 0, 0}, block.EnchantingTable{}},
	{cube.Pos{-2, 0, 1}, block.Grindstone{}},
	{cube.Pos{-2, 0, 2}, block.Stonecutter{}},
	{cube.Pos{-2, 0, 3}, block.SmithingTable{}},
	{cube.Pos{-2, 0, 4}, block.Loom{}},
	{cube.Pos{0, 0, 4}, block.Beacon{}},
}

func (s *Session) containerTestSetup() {
	go s.do(func(tx *world.Tx, c session.Controllable) {
		base := cube.PosFromVec3(c.Position())
		for _, tb := range containerTestBlocks {
			tx.SetBlock(base.Add(tb.off), tb.b, nil)
		}
		// A double chest, placed the way a player does so Dragonfly pairs the halves.
		c.SetHeldItems(item.NewStack(block.NewChest(), 2), item.Stack{})
		for _, x := range []int{2, 3} {
			p := base.Add(cube.Pos{x, -1, -4})
			c.UseItemOnBlock(p, cube.FaceUp, mgl64.Vec3{0.5, 1, 0.5})
		}
		dc, _ := tx.Block(base.Add(cube.Pos{2, 0, -4})).(block.Chest)
		s.log.Info("container test blocks placed", "base", base, "double_chest_paired", dc.Paired())

		st := s.items()
		st.applying.Store(true)
		_ = st.inv.Clear()
		kit := []item.Stack{
			item.NewStack(block.Log{Wood: block.OakWood()}, 64),
			item.NewStack(block.Cobblestone{}, 64),
			item.NewStack(item.Coal{}, 16),
			item.NewStack(item.RawIron{}, 8),
			item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1),
			item.NewStack(item.LapisLazuli{}, 16),
			item.NewStack(item.Book{}, 2),
			item.NewStack(block.Stone{}, 64),
			item.NewStack(item.NetheriteIngot{}, 2),
			item.NewStack(item.SmithingTemplate{Template: item.TemplateNetheriteUpgrade()}, 2),
			item.NewStack(item.Bucket{Content: item.MilkBucketContent()}, 1),
			item.NewStack(item.Bucket{Content: item.MilkBucketContent()}, 1),
			item.NewStack(item.Bucket{Content: item.MilkBucketContent()}, 1),
			item.NewStack(item.Sugar{}, 2),
			item.NewStack(item.Egg{}, 1),
			item.NewStack(item.Wheat{}, 3),
			item.NewStack(item.BlazePowder{}, 4),
			item.NewStack(block.NetherWart{}, 4),
			item.NewStack(item.Potion{Type: potion.Water()}, 1),
			item.NewStack(item.Potion{Type: potion.Water()}, 1),
			item.NewStack(item.Pickaxe{Tier: item.ToolTierIron}, 1).Damage(200),
			item.NewStack(item.IronIngot{}, 8),
			item.NewStack(item.EnchantedBook{}, 1).WithEnchantments(item.NewEnchantment(enchantment.Sharpness, 1)),
			item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1).WithEnchantments(item.NewEnchantment(enchantment.Sharpness, 2)),
			item.NewStack(item.IronIngot{}, 1),
		}
		for i, it := range kit {
			_ = st.inv.SetItem(i, it)
		}
		st.applying.Store(false)
		c.SetExperienceLevel(30)
		s.sendInventory()
		s.SendMessage(fmt.Sprintf("CONTTEST %d %d %d", base[0], base[1], base[2]))
		if os.Getenv("JAVASESSION_CONTAINER_TEST") == "cycle" {
			go s.containerTestCycle(base)
		}
	})
}

// containerTestCycle opens every test window in turn from the server side, with contents and
// progress changing while it is open: for clients that cannot click (the headless real client), so
// they decode every container packet.
func (s *Session) containerTestCycle(base cube.Pos) {
	step := func(f func(tx *world.Tx, c session.Controllable)) {
		time.Sleep(1500 * time.Millisecond)
		s.do(f)
	}
	// The 2x2 grid of the inventory, with a result.
	step(func(tx *world.Tx, c session.Controllable) {
		st := s.items()
		_ = st.ui.SetItem(uiCraftSmall+3, item.NewStack(block.Log{Wood: block.OakWood()}, 2))
	})
	step(func(tx *world.Tx, c session.Controllable) { c.MoveItemsToInventory() })
	positions := []cube.Pos{{2, 0, -4}}
	for _, tb := range containerTestBlocks {
		positions = append(positions, tb.off)
	}
	for _, off := range positions {
		pos := base.Add(off)
		step(func(tx *world.Tx, c session.Controllable) {
			s.OpenBlockContainer(pos, tx)
			st := s.items()
			m := st.open.Load()
			if m == nil {
				return
			}
			switch m.kind {
			case menuChest, menuShulker, menuHopper:
				// A change by someone else while it is open.
				_ = m.inv.SetItem(0, item.NewStack(item.Diamond{}, 3))
			case menuFurnace:
				_ = m.inv.SetItem(0, item.NewStack(item.RawIron{}, 2))
				_ = m.inv.SetItem(1, item.NewStack(item.Coal{}, 1))
			case menuBrewing:
				_ = m.inv.SetItem(4, item.NewStack(item.BlazePowder{}, 1))
				_ = m.inv.SetItem(1, item.NewStack(item.Potion{Type: potion.Water()}, 1))
				_ = m.inv.SetItem(0, item.NewStack(block.NetherWart{}, 1))
			case menuCrafting:
				_ = st.ui.SetItem(uiCraftLarge, item.NewStack(block.Planks{Wood: block.OakWood()}, 1))
				_ = st.ui.SetItem(uiCraftLarge+3, item.NewStack(block.Planks{Wood: block.OakWood()}, 1))
				s.syncWindow(tx, c, m)
			case menuEnchanting:
				_ = st.ui.SetItem(uiEnchantInput, item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1))
				s.syncWindow(tx, c, m)
			case menuAnvil:
				_ = st.ui.SetItem(uiAnvilInput, item.NewStack(item.Pickaxe{Tier: item.ToolTierIron}, 1).Damage(100))
				_ = st.ui.SetItem(uiAnvilMaterial, item.NewStack(item.IronIngot{}, 2))
				s.syncWindow(tx, c, m)
			}
		})
		if off == (cube.Pos{2, 0, 3}) || off == (cube.Pos{-2, 0, -3}) {
			time.Sleep(4 * time.Second) // watch the furnace and brewing stand work
		}
	}
	step(func(tx *world.Tx, c session.Controllable) { s.closeMenu(tx, c, true) })
	s.log.Info("container test cycle done")
}
