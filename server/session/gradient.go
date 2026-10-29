package session

import (
	"maps"

	"github.com/df-mc/dragonfly/server/textcolour"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// expandGradients turns the colour gradients in the text of an outgoing packet into the nearest
// Bedrock colour codes (textcolour.Expand), so a gradient written anywhere text is shown reaches a
// Bedrock client as colours. Maps that other packets may share are copied before they change.
func expandGradients(pk packet.Packet) packet.Packet {
	switch pk := pk.(type) {
	case *packet.Text:
		pk.Message = bedrockText(pk.Message)
		for i, p := range pk.Parameters {
			pk.Parameters[i] = bedrockText(p)
		}
	case *packet.SetTitle:
		pk.Text = bedrockText(pk.Text)
	case *packet.ModalFormRequest:
		// The form is JSON; a gradient's tag and codes hold no character JSON escapes.
		if s := string(pk.FormData); textcolour.Has(s) {
			pk.FormData = []byte(bedrockText(s))
		}
	case *packet.BossEvent:
		pk.BossBarTitle = bedrockText(pk.BossBarTitle)
		pk.FilteredBossBarTitle = bedrockText(pk.FilteredBossBarTitle)
	case *packet.SetDisplayObjective:
		pk.DisplayName = bedrockText(pk.DisplayName)
	case *packet.SetScore:
		for i, e := range pk.Entries {
			pk.Entries[i].DisplayName = bedrockText(e.DisplayName)
		}
	case *packet.SetActorData:
		pk.EntityMetadata = metadataGradients(pk.EntityMetadata)
	case *packet.AddActor:
		pk.EntityMetadata = metadataGradients(pk.EntityMetadata)
	case *packet.AddPlayer:
		pk.EntityMetadata = metadataGradients(pk.EntityMetadata)
	case *packet.BlockActorData:
		pk.NBTData = blockNBTGradients(pk.NBTData)
	case *packet.InventoryContent:
		for i, it := range pk.Content {
			pk.Content[i].Stack.NBTData = itemNBTGradients(it.Stack.NBTData)
		}
	case *packet.InventorySlot:
		pk.NewItem.Stack.NBTData = itemNBTGradients(pk.NewItem.Stack.NBTData)
	}
	return pk
}

// bedrockText is s with its gradients as Bedrock colour codes.
func bedrockText(s string) string { return textcolour.Expand(s, false) }

// metadataGradients expands a name tag's gradients, in a copy of m.
func metadataGradients(m protocol.EntityMetadata) protocol.EntityMetadata {
	name, ok := m[protocol.EntityDataKeyName].(string)
	if !ok || !textcolour.Has(name) {
		return m
	}
	m = maps.Clone(m)
	m[protocol.EntityDataKeyName] = bedrockText(name)
	return m
}

// blockNBTGradients expands the gradients of a block's custom name (a container's title) and of a
// sign's text, in a copy of nbt.
func blockNBTGradients(nbt map[string]any) map[string]any {
	out, copied := nbt, false
	put := func(key string, v any) {
		if !copied {
			out, copied = maps.Clone(nbt), true
		}
		out[key] = v
	}
	if s, ok := nbt["CustomName"].(string); ok && textcolour.Has(s) {
		put("CustomName", bedrockText(s))
	}
	for _, side := range []string{"FrontText", "BackText"} {
		text, ok := nbt[side].(map[string]any)
		if !ok {
			continue
		}
		if s, ok := text["Text"].(string); ok && textcolour.Has(s) {
			t := maps.Clone(text)
			t["Text"] = bedrockText(s)
			put(side, t)
		}
	}
	return out
}

// itemNBTGradients expands the gradients of an item's custom name and lore, in a copy of nbt.
func itemNBTGradients(nbt map[string]any) map[string]any {
	display, ok := nbt["display"].(map[string]any)
	if !ok {
		return nbt
	}
	var d map[string]any // the changed display; nil while nothing changed
	change := func(key string, v any) {
		if d == nil {
			d = maps.Clone(display)
		}
		d[key] = v
	}
	if name, ok := display["Name"].(string); ok && textcolour.Has(name) {
		change("Name", bedrockText(name))
	}
	switch lore := display["Lore"].(type) {
	case []string:
		if anyGradient(lore) {
			out := make([]string, len(lore))
			for i, l := range lore {
				out[i] = bedrockText(l)
			}
			change("Lore", out)
		}
	case []any:
		var strs []string
		for _, l := range lore {
			if s, ok := l.(string); ok {
				strs = append(strs, s)
			}
		}
		if anyGradient(strs) {
			out := make([]any, len(lore))
			for i, l := range lore {
				if s, ok := l.(string); ok {
					l = bedrockText(s)
				}
				out[i] = l
			}
			change("Lore", out)
		}
	}
	if d == nil {
		return nbt
	}
	nbt = maps.Clone(nbt)
	nbt["display"] = d
	return nbt
}

func anyGradient(lines []string) bool {
	for _, l := range lines {
		if textcolour.Has(l) {
			return true
		}
	}
	return false
}
