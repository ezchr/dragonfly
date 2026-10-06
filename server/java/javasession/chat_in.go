package javasession

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	v777 "github.com/ezchr/go-mcjava/v777"
	"github.com/ezchr/go-mcjava/wire"
	"github.com/go-gl/mathgl/mgl64"
)

// handleTextPacket handles the client's chat, command, dialog and sign packets. It reports
// whether id was one of them. Signatures and acknowledgements are ignored: the server is offline
// mode and login says enforces_secure_chat=false.
func (s *Session) handleTextPacket(id int32, body []byte) (handled bool, err error) {
	r := wire.NewReader(body)
	switch id {
	case v777.ServerboundPlayChat:
		msg := r.String(256)
		// timestamp, salt, optional signature, acknowledgements: ignored.
		if r.Err != nil {
			return true, r.Err
		}
		if !validChat(msg) {
			s.log.Debug("chat with illegal characters dropped")
			return true, nil
		}
		s.inTx(func(c session.Controllable) { c.Chat(msg) })
	case v777.ServerboundPlayChatCommand, v777.ServerboundPlayChatCommandSigned:
		command := r.String(32767)
		if r.Err != nil {
			return true, r.Err
		}
		if !validChat(command) {
			return true, nil
		}
		s.inTx(func(c session.Controllable) { c.ExecuteCommand("/" + command) })
	case v777.ServerboundPlayChatAck, v777.ServerboundPlayChatSessionUpdate, v777.ServerboundPlayCommandSuggestion:
		// Nothing to do: no signed chat, no server-side suggestions yet.
	case v777.ServerboundPlayCustomClickAction:
		ident := r.String(32767)
		payload := r.ByteArray(65536)
		if r.Err != nil {
			return true, r.Err
		}
		s.handleClickAction(ident, payload)
	case v777.ServerboundPlaySignUpdate:
		pos, lines, front := readSignUpdate(r, s.ver.Native())
		if r.Err != nil {
			return true, r.Err
		}
		s.editSign(pos, front, lines)
	default:
		return false, nil
	}
	return true, nil
}

// validChat rejects what vanilla rejects in chat (section signs, control characters, DEL).
func validChat(s string) bool {
	for _, r := range s {
		if r == '§' || r < ' ' || r == 0x7f {
			return false
		}
	}
	return true
}

// inTx runs f in the player's transaction, logging failures other than the player being gone.
func (s *Session) inTx(f func(c session.Controllable)) {
	err := s.withPlayer(func(_ *world.Tx, c session.Controllable) { f(c) })
	if err != nil && !stopped(err) {
		s.log.Debug("text packet", "err", err)
	}
}

// OpenSign opens the sign editor for one side of the sign at pos. It also allows this player
// one edit of that side (vanilla's SignBlockEntity.playerWhoMayEdit): a sign_update for any
// other sign or side, or a second one, is ignored.
func (s *Session) OpenSign(pos cube.Pos, frontSide bool) {
	ts := s.txt()
	ts.mu.Lock()
	ts.sign = signGrant{pos: pos, front: frontSide, at: time.Now(), ok: true}
	ts.mu.Unlock()
	w := s.packet()
	w.Position(pos[0], pos[1], pos[2])
	switch {
	case !s.ver.Native():
		w.Bool(frontSide) // 26.2: isFrontText
	case frontSide:
		w.VarInt(1) // SignTextSlot: BACK 0, FRONT 1
	default:
		w.VarInt(0)
	}
	s.queue(v777.ClientboundPlayOpenSignEditor, w)
}

// signGrant is the one sign edit OpenSign allowed.
type signGrant struct {
	pos   cube.Pos
	front bool
	at    time.Time
	ok    bool
}

// signEditWindow is how long the sign editor may stay open.
const signEditWindow = 10 * time.Minute

// takeSignGrant uses up the edit OpenSign allowed and reports whether it was for this side of the
// sign at pos. Any sign_update uses it up, like closing the editor does.
func (ts *textState) takeSignGrant(pos cube.Pos, front bool, now time.Time) bool {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	g := ts.sign
	ts.sign = signGrant{}
	return g.ok && g.pos == pos && g.front == front && now.Sub(g.at) < signEditWindow
}

// maxSignText is the most a sign side holds, in bytes: Dragonfly's Bedrock path refuses more.
const maxSignText = 256

// editSign applies the four lines a Java client wrote on one side of a sign. The other side keeps
// its text. Like vanilla, only the sign the server opened the editor for is accepted, once, and
// only while the player can still reach it; formatting codes are stripped.
func (s *Session) editSign(pos cube.Pos, front bool, lines [4]string) {
	if !s.txt().takeSignGrant(pos, front, time.Now()) {
		s.log.Debug("sign_update for a sign the player may not edit", "pos", pos)
		return
	}
	t := signText(lines)
	err := s.withPlayer(func(tx *world.Tx, c session.Controllable) {
		if !signReachable(c.GameMode(), c.Dead(), entity.EyePosition(c), pos) {
			return
		}
		// The sign was opened for this player, so its chunk is loaded; never load or generate one.
		b, ok := tx.BlockLoaded(pos)
		if !ok {
			return
		}
		sign, ok := b.(block.Sign)
		if !ok {
			return
		}
		frontText, backText := sign.Front.Text, sign.Back.Text
		if front {
			frontText = t
		} else {
			backText = t
		}
		if err := c.EditSign(pos, frontText, backText); err != nil {
			s.log.Debug("edit sign", "err", err)
		}
	})
	if err != nil && !stopped(err) {
		s.log.Debug("edit sign", "err", err)
	}
}

// signReachable is Dragonfly's canReach for block actor data: a living player in a game mode
// that allows interaction (not spectator), with its eyes within 8 blocks of the block's centre
// (14 in creative).
func signReachable(mode world.GameMode, dead bool, eyes mgl64.Vec3, pos cube.Pos) bool {
	if dead || !mode.AllowsInteraction() || mode == world.GameModeSpectator {
		return false
	}
	reach := 8.0
	if mode.CreativeInventory() {
		reach = 14
	}
	return eyes.Sub(pos.Vec3Middle()).Len() <= reach
}

// vanillaFormatting is ChatFormatting.STRIP_FORMATTING_PATTERN.
var vanillaFormatting = regexp.MustCompile(`(?i)§[0-9A-FK-OR]`)

// signText cleans the lines of a sign_update the way vanilla does (ChatFormatting.stripFormatting),
// then also drops any section sign left (Bedrock reads more codes than Java) and control
// characters, and joins the lines, cut to maxSignText bytes on a character boundary.
func signText(lines [4]string) string {
	var clean [4]string
	for i, l := range lines {
		l = vanillaFormatting.ReplaceAllString(l, "")
		clean[i] = strings.Map(func(r rune) rune {
			if r == '§' || r < ' ' || r == 0x7f {
				return -1
			}
			return r
		}, l)
	}
	n := len(clean)
	for n > 0 && clean[n-1] == "" {
		n--
	}
	t := strings.Join(clean[:n], "\n")
	if len(t) > maxSignText {
		cut := maxSignText
		for cut > 0 && !utf8.RuneStart(t[cut]) {
			cut--
		}
		t = strings.TrimRight(t[:cut], "\n")
	}
	return t
}

// readSignUpdate reads a sign_update: 26.3 has the four lines, then the side as a SignTextSlot
// VarInt (BACK 0, FRONT 1); 26.2 has an isFrontText bool before the lines.
func readSignUpdate(r *wire.Reader, native bool) (pos cube.Pos, lines [4]string, front bool) {
	x, y, z := r.Position()
	if !native {
		front = r.Bool()
	}
	for i := range lines {
		lines[i] = r.String(384) // vanilla's limit, in UTF-16 units
	}
	if native {
		front = r.VarInt() == 1
	}
	return cube.Pos{x, y, z}, lines, front
}
