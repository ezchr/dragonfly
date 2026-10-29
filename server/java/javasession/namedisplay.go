package javasession

import (
	"github.com/google/uuid"

	"github.com/df-mc/dragonfly/server/java/protocol/text"
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

// A Java client draws a player's own name in one of 16 team colours, so a player's name tag (and
// the score tag under it) is drawn by a text display riding the player instead, in its exact
// colours, gradients included; the team hides the real name (applyNameTag). As a passenger, the
// client moves it with the player each frame: it never lags behind as a followed entity would.

// nameDisplayLift is how far above the top of the player the bottom of the text sits, in blocks:
// where a vanilla name tag's lowest line ends.
const nameDisplayLift = 0.25

// Entity data of a display (Display 8-22) and of a text display's text (TextDisplay 23-27).
const (
	dataDisplayBrightness = 16
	dataTextOpacity       = 26
	dataTextFlags         = 27

	fullBrightness     = 15<<4 | 15<<20 // block light and sky light, both 15
	textFlagSeeThrough = 0x02
	sneakTextOpacity   = 0x20 // the alpha vanilla draws a sneaking player's name with
)

// nameDisplay is the text display a client is shown above a player.
type nameDisplay struct {
	id    int32
	text  string
	sneak bool
}

// nameDisplayText is the text shown above p: their name tag and, under it, their score tag ("" when
// the name is hidden), as this client sees them.
func (s *Session) nameDisplayText(p *player.Player) string {
	ts := s.txt()
	ts.mu.Lock()
	tag, ok := ts.nameOverride[p.H()]
	score, sok := ts.scoreOverride[p.H()]
	ts.mu.Unlock()
	if !ok {
		tag = p.NameTag()
	}
	if !sok {
		score = p.ScoreTag()
	}
	if tag == "" {
		return ""
	}
	if score != "" {
		tag += "\n§r" + score
	}
	return tag
}

// viewNameDisplay shows p's name above them as a text display riding them, creates or updates it,
// and removes it when their name is hidden.
func (s *Session) viewNameDisplay(p *player.Player) {
	if p.H() == s.ent {
		return // a player does not see their own name
	}
	pid, ok := s.entityID(p)
	if !ok {
		return
	}
	txt, sneak := s.nameDisplayText(p), p.Sneaking()
	s.entMu.Lock()
	if s.nameDisplays == nil {
		s.nameDisplays = map[int32]*nameDisplay{}
	}
	d := s.nameDisplays[pid]
	if d != nil && d.text == txt && d.sneak == sneak {
		s.entMu.Unlock()
		return
	}
	if txt == "" {
		delete(s.nameDisplays, pid)
		s.entMu.Unlock()
		if d != nil {
			s.removeIDs(d.id)
			s.sendPassengers(p.H())
		}
		return
	}
	created := d == nil
	if created {
		s.nextEntityID++
		if s.nextEntityID == selfEntityID {
			s.nextEntityID++
		}
		d = &nameDisplay{id: s.nextEntityID}
		s.nameDisplays[pid] = d
	}
	d.text, d.sneak = txt, sneak
	id := d.id
	s.entMu.Unlock()

	if created {
		pos := p.Position()
		w := s.packet()
		w.VarInt(id)
		w.UUID(uuid.New())
		w.VarInt(s.ver.Builtin("minecraft:entity_type", v777.BuiltinID("minecraft:entity_type", "minecraft:text_display")))
		w.Float64(pos[0])
		w.Float64(pos[1] + 1.8)
		w.Float64(pos[2])
		w.LpVec3(0, 0, 0)
		w.Angle(0)
		w.Angle(0)
		w.Angle(0)
		w.VarInt(0)
		s.queue(v777.ClientboundPlayAddEntity, w)
	}
	w := s.packet()
	w.VarInt(id)
	if created {
		w.Byte(dataDisplayBillboard)
		w.VarInt(dataTypeByte)
		w.Byte(billboardCenter)
		w.Byte(dataTextLineWidth)
		w.VarInt(dataTypeInt)
		w.VarInt(1000)
		w.Byte(dataDisplayBrightness)
		w.VarInt(dataTypeInt)
		w.VarInt(fullBrightness)
		w.Byte(dataDisplayTranslation)
		w.VarInt(dataTypeVector3)
		w.Float32(0)
		w.Float32(nameDisplayLift)
		w.Float32(0)
	}
	// Like a vanilla name tag: seen through blocks, but faint and not through blocks while the
	// player sneaks.
	flags, opacity := byte(textFlagSeeThrough), byte(0xff)
	if sneak {
		flags, opacity = 0, sneakTextOpacity
	}
	w.Byte(dataTextFlags)
	w.VarInt(dataTypeByte)
	w.Byte(flags)
	w.Byte(dataTextOpacity)
	w.VarInt(dataTypeByte)
	w.Byte(opacity)
	c := text.Legacy(txt, text.Bedrock)
	w.Byte(dataTextText)
	w.VarInt(dataTypeComponent)
	c.Write(w)
	w.Byte(0xff)
	s.queue(v777.ClientboundPlaySetEntityData, w)
	if created {
		s.sendPassengers(p.H())
	}
}

// nameDisplayOf is the id of the text display riding the player with Java id pid, if it has one.
// entMu held.
func (s *Session) nameDisplayOf(pid int32) (int32, bool) {
	d, ok := s.nameDisplays[pid]
	if !ok {
		return 0, false
	}
	return d.id, true
}

// hideNameDisplay removes the text display above the player with Java id pid.
func (s *Session) hideNameDisplay(pid int32) {
	s.entMu.Lock()
	d := s.nameDisplays[pid]
	delete(s.nameDisplays, pid)
	s.entMu.Unlock()
	if d != nil {
		s.removeIDs(d.id)
	}
}

// removeIDs removes entities only the client has.
func (s *Session) removeIDs(ids ...int32) {
	w := s.packet()
	w.VarInt(int32(len(ids)))
	for _, id := range ids {
		w.VarInt(id)
	}
	s.queue(v777.ClientboundPlayRemoveEntities, w)
}

// viewNameDisplayOf updates the name display of e, if e is a player.
func (s *Session) viewNameDisplayOf(e world.Entity) {
	if p, ok := e.(*player.Player); ok {
		s.viewNameDisplay(p)
	}
}
