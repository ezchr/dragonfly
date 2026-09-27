package session

import (
	"fmt"
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// Villager-style trading windows. Not part of upstream Dragonfly - added for
// server-defined traders. The flow matches vanilla and PowerNukkitX
// (EntityVillagerV2.updateTrades / CraftRecipeActionProcessor):
//
//   - UpdateTrade alone opens the window (no ContainerOpen). Each offer carries
//     a "netId", and the client sends that id back as the RecipeNetworkID of a
//     CraftRecipe action when the player takes a trade.
//   - The payment sits in the UI inventory's trade ingredient slots (4 and 5)
//     and the client lists them in Consume actions; the sold item is created
//     through the usual created-output path, like stonecutting.
//   - Closing the window returns anything left in those slots through
//     MoveItemsToInventory, the same as the crafting grid.

// TradeOffer is one offer in a trading window. Buy2 may be empty. MaxUses <= 0
// means unlimited.
type TradeOffer struct {
	Buy, Buy2, Sell item.Stack
	Uses, MaxUses   int
}

type tradeState struct {
	traderRID uint64
	traderID  int64
	name      string
	offers    []TradeOffer
	onTrade   func(index, times int) bool
}

// Offer network ids live far above any real recipe network id.
const tradeNetworkIDBase uint32 = 0x70000000

// Trade ingredient slots of the UI inventory, as used by the new trade UI.
const (
	tradeIngredientSlotA = 4
	tradeIngredientSlotB = 5
)

func isTradeContainer(id byte) bool {
	switch id {
	case protocol.ContainerTradeIngredientOne, protocol.ContainerTradeIngredientTwo, protocol.ContainerTradeResultPreview,
		protocol.ContainerTradeTwoIngredientOne, protocol.ContainerTradeTwoIngredientTwo, protocol.ContainerTradeTwoResultPreview:
		return true
	}
	return false
}

// OpenTrade opens a trading window with trader for the session. onTrade is
// called for every completed trade with the offer's index and the number of
// times it was traded; returning false refuses the trade.
func (s *Session) OpenTrade(tx *world.Tx, trader world.Entity, name string, offers []TradeOffer, onTrade func(index, times int) bool) {
	s.closeCurrentContainer(tx, false)

	windowID := s.nextWindowID()
	// Several container paths read the block at openedPos while a window is
	// open, so point it at the trader's own (air) block rather than leave it
	// unset or stale.
	pos := cube.PosFromVec3(trader.Position())
	s.openedPos.Store(&pos)
	s.openedWindow.Store(inventory.New(1, nil))
	s.openedContainerID.Store(uint32(protocol.ContainerTypeTrade))
	rid := s.entityRuntimeID(trader)
	st := &tradeState{
		traderRID: rid,
		traderID:  int64(rid),
		name:      name,
		offers:    append([]TradeOffer(nil), offers...),
		onTrade:   onTrade,
	}
	s.trade.Store(st)
	s.containerOpened.Store(true)
	s.setTradeTarget(rid, int64(selfEntityRuntimeID))
	s.sendTradeOffers(st, windowID)
	s.conf.Log.Debug("trade window opened", "window", windowID, "trader", rid, "offers", len(offers))
}

// setTradeTarget sets the trader's "trading with" metadata for this client
// only. The client marks a villager busy by itself once its trading window
// opens and will not open it again until the server clears the mark, which
// is what PowerNukkitX's setTradingPlayer(0) on close does. Only the changed
// key is sent; the client merges it into the entity's existing metadata.
func (s *Session) setTradeTarget(traderRID uint64, target int64) {
	s.writePacket(&packet.SetActorData{
		EntityRuntimeID: traderRID,
		EntityMetadata:  protocol.EntityMetadata{protocol.EntityDataKeyTradeTarget: target},
	})
}

// UpdateTradeOffers replaces the offers of the open trading window, if any.
func (s *Session) UpdateTradeOffers(offers []TradeOffer) {
	old := s.trade.Load()
	if old == nil {
		return
	}
	st := &tradeState{traderRID: old.traderRID, traderID: old.traderID, name: old.name, offers: append([]TradeOffer(nil), offers...), onTrade: old.onTrade}
	s.trade.Store(st)
	s.sendTradeOffers(st, byte(s.openedWindowID.Load()))
}

func (s *Session) sendTradeOffers(st *tradeState, windowID byte) {
	data, err := encodeTradeOffers(st.offers)
	if err != nil {
		s.conf.Log.Error("encode trade offers: " + err.Error())
		return
	}
	s.writePacket(&packet.UpdateTrade{
		WindowID:         windowID,
		WindowType:       protocol.ContainerTypeTrade,
		Size:             int32(len(st.offers)),
		VillagerUniqueID: st.traderID,
		EntityUniqueID:   int64(selfEntityRuntimeID),
		DisplayName:      st.name,
		NewTradeUI:       true,
		SerialisedOffers: data,
	})
}

// encodeTradeOffers builds the network NBT compound UpdateTrade carries, in
// the vanilla recipe format (see PowerNukkitX TradeRecipeBuildUtils).
func encodeTradeOffers(offers []TradeOffer) ([]byte, error) {
	recipes := make([]map[string]any, 0, len(offers))
	for i, o := range offers {
		maxUses := o.MaxUses
		if maxUses <= 0 {
			maxUses = math.MaxInt32
		}
		r := map[string]any{
			"buyA":             tradeItemNBT(o.Buy),
			"buyCountA":        int32(o.Buy.Count()),
			"buyCountB":        int32(0),
			"sell":             tradeItemNBT(o.Sell),
			"uses":             int32(o.Uses),
			"maxUses":          int32(maxUses),
			"netId":            int32(tradeNetworkIDBase + uint32(i)),
			"tier":             int32(0),
			"demand":           int32(0),
			"rewardExp":        byte(0),
			"traderExp":        int32(0),
			"priceMultiplierA": float32(0),
			"priceMultiplierB": float32(0),
		}
		if !o.Buy2.Empty() {
			r["buyB"] = tradeItemNBT(o.Buy2)
			r["buyCountB"] = int32(o.Buy2.Count())
		}
		recipes = append(recipes, r)
	}
	return nbt.MarshalEncoding(map[string]any{
		"Recipes":             recipes,
		"TierExpRequirements": []map[string]any{{"0": int32(0)}},
	}, nbt.NetworkLittleEndian)
}

func tradeItemNBT(s item.Stack) map[string]any {
	m := item.WriteNBT(s, true)
	m["WasPickedUp"] = byte(0)
	return m
}

// closeTrade forgets the trade state and frees the trader again client-side.
// It reports whether a trade was open.
func (s *Session) closeTrade() bool {
	st := s.trade.Swap(nil)
	if st == nil {
		return false
	}
	s.setTradeTarget(st.traderRID, 0)
	s.conf.Log.Debug("trade window closed", "trader", st.traderRID)
	return true
}

// handleTrade handles a CraftRecipe (or AutoCraftRecipe) action taken in a
// trading window.
func (h *ItemStackRequestHandler) handleTrade(netID uint32, times int, req protocol.ItemStackRequest, s *Session, tx *world.Tx) error {
	st := s.trade.Load()
	if st == nil {
		return fmt.Errorf("no trading window open")
	}
	if netID < tradeNetworkIDBase || netID >= tradeNetworkIDBase+uint32(len(st.offers)) {
		return fmt.Errorf("unknown trade offer network id %v", netID)
	}
	if times < 1 {
		return fmt.Errorf("times traded must be at least 1")
	}
	index := int(netID - tradeNetworkIDBase)
	o := st.offers[index]
	if o.MaxUses > 0 && o.Uses+times > o.MaxUses {
		return fmt.Errorf("trade offer %v has %v of %v uses left, %v requested", index, o.MaxUses-o.Uses, o.MaxUses, times)
	}

	// The payment slots, as the client names them in its Consume actions.
	slotA := protocol.StackRequestSlotInfo{Container: protocol.FullContainerName{ContainerID: protocol.ContainerTradeTwoIngredientOne}, Slot: tradeIngredientSlotA}
	slotB := protocol.StackRequestSlotInfo{Container: protocol.FullContainerName{ContainerID: protocol.ContainerTradeTwoIngredientTwo}, Slot: tradeIngredientSlotB}
	for _, action := range req.Actions {
		c, ok := action.(*protocol.ConsumeStackRequestAction)
		if !ok || !isTradeContainer(c.Source.Container.ContainerID) {
			continue
		}
		switch c.Source.Slot {
		case tradeIngredientSlotA:
			slotA = c.Source
		case tradeIngredientSlotB:
			slotB = c.Source
		}
	}
	inA, _ := h.itemInSlot(slotA, s, tx)
	inB, _ := h.itemInSlot(slotB, s, tx)
	if !tradeInputMatches(inA, o.Buy, times) {
		return fmt.Errorf("trade offer %v: first payment slot holds %v, need %v x%v", index, inA, o.Buy, times)
	}
	if !o.Buy2.Empty() && !tradeInputMatches(inB, o.Buy2, times) {
		return fmt.Errorf("trade offer %v: second payment slot holds %v, need %v x%v", index, inB, o.Buy2, times)
	}
	if st.onTrade != nil && !st.onTrade(index, times) {
		return fmt.Errorf("trade offer %v refused by trader", index)
	}

	h.setItemInSlot(slotA, inA.Grow(-o.Buy.Count()*times), s, tx)
	if !o.Buy2.Empty() {
		h.setItemInSlot(slotB, inB.Grow(-o.Buy2.Count()*times), s, tx)
	}
	st.offers[index].Uses += times
	return h.createResults(s, tx, repeatStacks([]item.Stack{o.Sell}, times)...)
}

func tradeInputMatches(have, want item.Stack, times int) bool {
	return !have.Empty() && have.Comparable(want) && have.Count() >= want.Count()*times
}
