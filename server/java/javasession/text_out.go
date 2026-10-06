package javasession

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/ezchr/go-mcjava/text"
	v777 "github.com/ezchr/go-mcjava/v777"
	"golang.org/x/text/language"
)

// Chat, action bar, titles, toasts, boss bar and command output. Dragonfly's strings use Bedrock
// colour codes; every one is converted with the Bedrock palette (bedrockText).

// SendMessage sends a chat message (system_chat).
func (s *Session) SendMessage(message string) {
	c := bedrockText(message)
	s.sendSystem(&c, false)
}

// javaTranslations are Bedrock translation keys that Java has too, with the same arguments. The
// client translates these itself; others are sent in the server's English.
var javaTranslations = map[string]bool{
	"multiplayer.player.joined": true,
	"multiplayer.player.left":   true,
	"disconnect.disconnected":   true,
}

// SendTranslation sends a translated message: as a Java translate component when Java knows the
// key (join/leave messages), otherwise as the server-side English text.
func (s *Session) SendTranslation(t chat.Translation, l language.Tag, a []any) {
	tr := t.F(a...)
	resolved := tr.Resolve(l) // the format with the Bedrock key, "§e%multiplayer.player.joined"
	c := bedrockText(resolved)
	if replaceKey(&c, tr.Params(l)) {
		s.sendSystem(&c, false)
		return
	}
	c = bedrockText(tr.String())
	s.sendSystem(&c, false)
}

// replaceKey turns the "%key" text of c (or of one of its children) into a translate component
// when Java knows the key.
func replaceKey(c *text.Component, params []string) bool {
	if k, ok := strings.CutPrefix(c.Text, "%"); ok && javaTranslations[k] {
		c.Text, c.Translate = "", k
		for _, p := range params {
			c.With = append(c.With, bedrockText(p))
		}
		return true
	}
	for i := range c.Extra {
		if replaceKey(&c.Extra[i], params) {
			return true
		}
	}
	return false
}

// SendPopup shows text above the hotbar (the action bar).
func (s *Session) SendPopup(message string) { s.SendActionBarMessage(message) }

// SendTip shows text above the hotbar (Java has no separate centre-screen tip).
func (s *Session) SendTip(message string) { s.SendActionBarMessage(message) }

// SendJukeboxPopup shows a "now playing" text above the hotbar.
func (s *Session) SendJukeboxPopup(message string) { s.SendActionBarMessage(message) }

// SendActionBarMessage sets the action bar text.
func (s *Session) SendActionBarMessage(message string) {
	w := s.packet()
	writeText(w, message)
	s.queue(v777.ClientboundPlaySetActionBarText, w)
}

// SendToast shows a toast. Java has no server toasts without advancements, so it is a chat line:
// the title in bold, then the message.
func (s *Session) SendToast(title, message string) {
	c := text.Component{Extra: []text.Component{
		{Bold: text.On, Extra: []text.Component{bedrockText(title)}},
		text.Plain(" "),
		bedrockText(message),
	}}
	s.sendSystem(&c, false)
}

// SetTitleDurations sets the fade in, stay and fade out times of titles (set_titles_animation).
func (s *Session) SetTitleDurations(fadeIn, remain, fadeOut time.Duration) {
	w := s.packet()
	w.Int32(ticks(fadeIn))
	w.Int32(ticks(remain))
	w.Int32(ticks(fadeOut))
	s.queue(v777.ClientboundPlaySetTitlesAnimation, w)
}

func ticks(d time.Duration) int32 {
	return int32(min(d/(time.Second/20), math.MaxInt32))
}

// SendTitle shows a title.
func (s *Session) SendTitle(t string) {
	w := s.packet()
	writeText(w, t)
	s.queue(v777.ClientboundPlaySetTitleText, w)
}

// SendSubtitle sets the subtitle shown with the title.
func (s *Session) SendSubtitle(t string) {
	w := s.packet()
	writeText(w, t)
	s.queue(v777.ClientboundPlaySetSubtitleText, w)
}

// Boss bar: one bar per session, under a fixed UUID.
var bossUUID = [16]byte{0x64, 0x66, 0x2d, 0x62, 0x6f, 0x73, 0x73, 0x2d, 0x80, 0x00, 0, 0, 0, 0, 0, 1}

const (
	bossAdd = iota
	bossRemove
	bossProgress
	bossName
	bossStyle
)

// bossColour maps Dragonfly's boss bar colours (pink, blue, red, green, yellow, purple,
// rebecca purple, white) to Java's (pink, blue, red, green, yellow, purple, white).
func bossColour(c uint8) int32 {
	switch {
	case c <= 5:
		return int32(c)
	case c == 6:
		return 5
	}
	return 6
}

// SendBossBar shows the boss bar, or updates the parts of it that changed.
func (s *Session) SendBossBar(t string, colour uint8, health float64) {
	ts := s.txt()
	ts.mu.Lock()
	defer ts.mu.Unlock()
	col, hp := bossColour(colour), float32(min(max(health, 0), 1))
	if !ts.bossShown {
		w := s.packet()
		w.UUID(bossUUID)
		w.VarInt(bossAdd)
		writeText(w, t)
		w.Float32(hp)
		w.VarInt(col)
		w.VarInt(0) // no notches
		w.Byte(0)   // no flags (darken sky, boss music, fog)
		s.queue(v777.ClientboundPlayBossEvent, w)
		ts.bossShown, ts.bossText, ts.bossColour, ts.bossHealth = true, t, col, hp
		return
	}
	if t != ts.bossText {
		w := s.packet()
		w.UUID(bossUUID)
		w.VarInt(bossName)
		writeText(w, t)
		s.queue(v777.ClientboundPlayBossEvent, w)
		ts.bossText = t
	}
	if hp != ts.bossHealth {
		w := s.packet()
		w.UUID(bossUUID)
		w.VarInt(bossProgress)
		w.Float32(hp)
		s.queue(v777.ClientboundPlayBossEvent, w)
		ts.bossHealth = hp
	}
	if col != ts.bossColour {
		w := s.packet()
		w.UUID(bossUUID)
		w.VarInt(bossStyle)
		w.VarInt(col)
		w.VarInt(0)
		s.queue(v777.ClientboundPlayBossEvent, w)
		ts.bossColour = col
	}
}

// RemoveBossBar removes the boss bar if one is shown.
func (s *Session) RemoveBossBar() {
	ts := s.txt()
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if !ts.bossShown {
		return
	}
	ts.bossShown = false
	w := s.packet()
	w.UUID(bossUUID)
	w.VarInt(bossRemove)
	s.queue(v777.ClientboundPlayBossEvent, w)
}

// SendCommandOutput shows a command's output in chat: messages as they are, errors in red, like
// vanilla.
func (s *Session) SendCommandOutput(output *cmd.Output, _ language.Tag) {
	for _, m := range output.Messages() {
		c := bedrockText(m.String())
		s.sendSystem(&c, false)
	}
	for _, err := range output.Errors() {
		c := text.Component{Color: "red", Extra: []text.Component{bedrockText(errorText(err))}}
		s.sendSystem(&c, false)
	}
}

func errorText(err error) string {
	if st, ok := err.(fmt.Stringer); ok {
		return st.String()
	}
	return err.Error()
}
