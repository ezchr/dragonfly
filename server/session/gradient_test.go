package session

import (
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

const gradient = "<gradient:&c:&6>ab</gradient>"

func TestExpandGradientsInText(t *testing.T) {
	pk := expandGradients(&packet.Text{Message: "x" + gradient, Parameters: []string{gradient}}).(*packet.Text)
	if pk.Message != "x§ca§6b" || pk.Parameters[0] != "§ca§6b" {
		t.Fatalf("got %q %q", pk.Message, pk.Parameters)
	}
}

func TestExpandGradientsLeavesSharedMapsAlone(t *testing.T) {
	meta := protocol.EntityMetadata{protocol.EntityDataKeyName: gradient}
	pk := expandGradients(&packet.SetActorData{EntityMetadata: meta}).(*packet.SetActorData)
	if got := pk.EntityMetadata[protocol.EntityDataKeyName]; got != "§ca§6b" {
		t.Fatalf("name tag %q", got)
	}
	if meta[protocol.EntityDataKeyName] != gradient {
		t.Fatal("the original metadata changed")
	}

	sign := map[string]any{"FrontText": map[string]any{"Text": gradient}, "CustomName": "plain"}
	bd := expandGradients(&packet.BlockActorData{NBTData: sign}).(*packet.BlockActorData)
	if got := bd.NBTData["FrontText"].(map[string]any)["Text"]; got != "§ca§6b" {
		t.Fatalf("sign text %q", got)
	}
	if sign["FrontText"].(map[string]any)["Text"] != gradient {
		t.Fatal("the original sign changed")
	}

	display := map[string]any{"Name": gradient, "Lore": []any{"plain", gradient}}
	item := map[string]any{"display": display}
	slot := expandGradients(&packet.InventorySlot{NewItem: protocol.ItemInstance{Stack: protocol.ItemStack{NBTData: item}}}).(*packet.InventorySlot)
	got := slot.NewItem.Stack.NBTData["display"].(map[string]any)
	if got["Name"] != "§ca§6b" || got["Lore"].([]any)[1] != "§ca§6b" || got["Lore"].([]any)[0] != "plain" {
		t.Fatalf("item display %v", got)
	}
	if display["Name"] != gradient || display["Lore"].([]any)[1] != gradient {
		t.Fatal("the original item changed")
	}
}
