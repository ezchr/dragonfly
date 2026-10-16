package javasession

import (
	"github.com/df-mc/dragonfly/server/java/protocol/text"
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

// A mob's or trader's name tag is the Java entity's custom name (Entity data 2 and 3). Players
// get theirs through teams (applyNameTag) and floating text is a text display, so neither comes
// here.
const (
	dataCustomName        = 2
	dataCustomNameVisible = 3

	dataTypeOptComponent = 6
)

type nameTagged interface {
	NameTag() string
}

// viewCustomName sends e's name tag when it differs from the one last sent for id. A name tag
// that is not always shown appears when the player looks at the entity, as on Bedrock.
func (s *Session) viewCustomName(e world.Entity, id int32) {
	if _, ok := e.(*player.Player); ok || isTextEntity(e) {
		return
	}
	n, ok := e.(nameTagged)
	if !ok {
		return
	}
	tag := n.NameTag()
	always := true
	if a, ok := e.(interface{ AlwaysShowNameTag() bool }); ok {
		always = a.AlwaysShowNameTag()
	}
	key := tag
	if always {
		key += "\x00a"
	}
	s.entMu.Lock()
	if s.named == nil {
		s.named = map[int32]string{}
	}
	last, sent := s.named[id]
	if (!sent && tag == "") || last == key {
		s.entMu.Unlock()
		return
	}
	s.named[id] = key
	s.entMu.Unlock()

	w := s.packet()
	w.VarInt(id)
	w.Byte(dataCustomName)
	w.VarInt(dataTypeOptComponent)
	if tag == "" {
		w.Bool(false)
	} else {
		w.Bool(true)
		c := text.Legacy(tag, text.Bedrock)
		c.Write(w)
	}
	w.Byte(dataCustomNameVisible)
	w.VarInt(dataTypeBoolean)
	w.Bool(tag != "" && always)
	w.Byte(0xff)
	s.queue(v777.ClientboundPlaySetEntityData, w)
}

// forgetCustomName drops what was sent for an entity id that left view.
func (s *Session) forgetCustomName(id int32) {
	s.entMu.Lock()
	delete(s.named, id)
	s.entMu.Unlock()
}
