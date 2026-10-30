package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

func TestArmourStandNBTRoundTrip(t *testing.T) {
	b := newArmourStandBehaviour()
	b.armour.SetHelmet(item.NewStack(item.Helmet{Tier: item.ArmourTierDiamond{}}, 1))
	b.armour.SetBoots(item.NewStack(item.Boots{Tier: item.ArmourTierIron{}}, 1))
	b.mainHand = item.NewStack(item.Sword{Tier: item.ToolTierNetherite}, 1).WithCustomName("Blade")
	b.pose, b.lastSignal = 7, 8
	data := &world.EntityData{Data: b}
	m := ArmourStandType.EncodeNBT(data)

	var back world.EntityData
	ArmourStandType.DecodeNBT(m, &back)
	got := back.Data.(*armourStandBehaviour)
	if !got.armour.Helmet().Equal(b.armour.Helmet()) || !got.armour.Boots().Equal(b.armour.Boots()) {
		t.Fatalf("armour not kept: %v %v", got.armour.Helmet(), got.armour.Boots())
	}
	if !got.armour.Chestplate().Empty() || !got.offHand.Empty() {
		t.Fatalf("empty slots came back filled")
	}
	if got.mainHand.CustomName() != "Blade" || got.pose != 7 || got.lastSignal != 8 {
		t.Fatalf("got %v pose %d signal %d", got.mainHand, got.pose, got.lastSignal)
	}
	if back.AlwaysShowNameTag {
		t.Fatalf("name tag should show only when looked at")
	}
}

func TestArmourStandClickedSlot(t *testing.T) {
	for dy, want := range map[float64]int{0.05: -1, 0.3: 5, 0.5: 5, 0.7: 4, 1.2: 3, 1.7: 2} {
		if got := clickedSlot(dy); got != want {
			t.Errorf("clickedSlot(%v) = %d, want %d", dy, got, want)
		}
	}
	if wornSlot(item.NewStack(item.Shield{}, 1)) != 1 || wornSlot(item.NewStack(item.Elytra{}, 1)) != 3 || wornSlot(item.NewStack(item.Stick{}, 1)) != 0 {
		t.Error("wrong worn slot")
	}
}
