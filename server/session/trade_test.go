package session

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

func TestEncodeTradeOffers(t *testing.T) {
	offers := []TradeOffer{
		{Buy: item.NewStack(item.IronIngot{}, 10), Sell: item.NewStack(item.Diamond{}, 1), MaxUses: 5, Uses: 2},
		{Buy: item.NewStack(item.Diamond{}, 3), Buy2: item.NewStack(item.Book{}, 1), Sell: item.NewStack(block.Stone{}, 64)},
	}
	b, err := encodeTradeOffers(offers)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := nbt.UnmarshalEncoding(b, &m, nbt.NetworkLittleEndian); err != nil {
		t.Fatal(err)
	}
	recipes, ok := m["Recipes"].([]any)
	if !ok || len(recipes) != 2 {
		t.Fatalf("Recipes = %#v", m["Recipes"])
	}
	if _, ok := m["TierExpRequirements"].([]any); !ok {
		t.Fatal("missing TierExpRequirements")
	}

	r0 := recipes[0].(map[string]any)
	if r0["netId"] != int32(tradeNetworkIDBase) || r0["maxUses"] != int32(5) || r0["uses"] != int32(2) || r0["buyCountA"] != int32(10) {
		t.Fatalf("offer 0 header wrong: %#v", r0)
	}
	buyA := r0["buyA"].(map[string]any)
	if buyA["Name"] != "minecraft:iron_ingot" || buyA["Count"] != byte(10) {
		t.Fatalf("offer 0 buyA wrong: %#v", buyA)
	}
	if _, ok := r0["buyB"]; ok {
		t.Fatal("offer 0 should have no buyB")
	}

	r1 := recipes[1].(map[string]any)
	if r1["netId"] != int32(tradeNetworkIDBase+1) || r1["maxUses"] != int32(math.MaxInt32) || r1["buyCountB"] != int32(1) {
		t.Fatalf("offer 1 header wrong: %#v", r1)
	}
	if r1["buyB"].(map[string]any)["Name"] != "minecraft:book" {
		t.Fatalf("offer 1 buyB wrong: %#v", r1["buyB"])
	}
	sell := r1["sell"].(map[string]any)
	if sell["Name"] != "minecraft:stone" || sell["Count"] != byte(64) {
		t.Fatalf("offer 1 sell wrong: %#v", sell)
	}
	if _, ok := sell["Block"]; !ok {
		t.Fatal("a block item on sale needs its Block compound")
	}
}

func TestTradeInputMatches(t *testing.T) {
	iron := func(n int) item.Stack { return item.NewStack(item.IronIngot{}, n) }
	cases := []struct {
		have, want item.Stack
		times      int
		ok         bool
	}{
		{iron(10), iron(10), 1, true},
		{iron(64), iron(10), 6, true},
		{iron(64), iron(10), 7, false},
		{iron(9), iron(10), 1, false},
		{item.NewStack(item.GoldIngot{}, 10), iron(10), 1, false},
		{item.Stack{}, iron(1), 1, false},
		{iron(10).WithCustomName("Fake"), iron(10), 1, false},
	}
	for i, c := range cases {
		if got := tradeInputMatches(c.have, c.want, c.times); got != c.ok {
			t.Errorf("case %d: tradeInputMatches(%v, %v, %d) = %v, want %v", i, c.have, c.want, c.times, got, c.ok)
		}
	}
}

func TestIsTradeContainer(t *testing.T) {
	for _, id := range []byte{protocol.ContainerTradeTwoIngredientOne, protocol.ContainerTradeTwoIngredientTwo, protocol.ContainerTradeIngredientOne} {
		if !isTradeContainer(id) {
			t.Errorf("%d should be a trade container", id)
		}
	}
	for _, id := range []byte{protocol.ContainerCraftingInput, protocol.ContainerCreatedOutput, protocol.ContainerStonecutterInput} {
		if isTradeContainer(id) {
			t.Errorf("%d should not be a trade container", id)
		}
	}
}
