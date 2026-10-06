package item

import "testing"

func TestSpearIsChargeable(t *testing.T) {
	if _, ok := any(Spear{Tier: ToolTierIron}).(Chargeable); !ok {
		t.Fatal("Spear is not Chargeable")
	}
}
