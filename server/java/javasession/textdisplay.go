package javasession

import (
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/java/protocol/text"
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/world"
)

// Dragonfly's floating text entity (entity.NewText: "dragonfly:text", a name tag with no body) is
// shown to Java players as a text_display. Indexes are the 26.3 entity data ids (Entity 0-7,
// Display 8-22, TextDisplay 23-27).
const (
	dataDisplayBillboard = 15
	dataTextText         = 23
	dataTextLineWidth    = 24

	billboardCenter = 3 // always faces the viewer, like a Bedrock name tag

	dataTypeInt       = 1
	dataTypeComponent = 5
)

func isTextEntity(e world.Entity) bool {
	return e.H().Type() == entity.TextType
}

// viewTextDisplay sends a text display's text (on spawn and whenever its name tag changes).
func (s *Session) viewTextDisplay(e world.Entity, id int32, full bool) {
	ent, ok := e.(*entity.Ent)
	if !ok {
		return
	}
	c := text.Legacy(ent.NameTag(), text.Bedrock)
	w := s.packet()
	w.VarInt(id)
	if full {
		w.Byte(dataDisplayBillboard)
		w.VarInt(dataTypeByte)
		w.Byte(billboardCenter)
		w.Byte(dataTextLineWidth)
		w.VarInt(dataTypeInt)
		w.VarInt(1000) // Bedrock does not wrap name tags; Java wraps at 200 pixels by default
	}
	w.Byte(dataTextText)
	w.VarInt(dataTypeComponent)
	c.Write(w)
	w.Byte(0xff)
	s.queue(v777.ClientboundPlaySetEntityData, w)
}
