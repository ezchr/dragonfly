package javasession

import (
	"encoding/hex"
	"strings"

	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/scoreboard"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/ezchr/go-mcjava/text"
	v777 "github.com/ezchr/go-mcjava/v777"
	"github.com/ezchr/go-mcjava/wire"
	"github.com/google/uuid"
)

// The sidebar: one objective whose scores are the lines. Each line is a score with a fixed owner
// ("§0".."§e", never a real player name), a display name holding the line's text, and the
// objective's blank number format hiding the numbers. Updates only send the lines that changed,
// and a new title is an objective update, so the sidebar never flickers.

const (
	sidebarObjective = "df_sidebar"
	belowObjective   = "df_scoretag"

	objectiveAdd    = 0
	objectiveRemove = 1
	objectiveUpdate = 2

	slotSidebar   = 1
	slotBelowName = 2
)

var lineOwners = [15]string{"§0", "§1", "§2", "§3", "§4", "§5", "§6", "§7", "§8", "§9", "§a", "§b", "§c", "§d", "§e"}

// SendScoreboard shows the sidebar or updates it.
func (s *Session) SendScoreboard(sb *scoreboard.Scoreboard) {
	lines := sb.Lines()
	if len(lines) > len(lineOwners) {
		lines = lines[:len(lineOwners)]
	}
	ts := s.txt()
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if !ts.sbShown {
		w := s.packet()
		w.String(sidebarObjective)
		w.Byte(objectiveAdd)
		writeObjectiveInfo(w, sb.Name())
		s.queue(v777.ClientboundPlaySetObjective, w)
		w = s.packet()
		w.VarInt(slotSidebar)
		w.String(sidebarObjective)
		s.queue(v777.ClientboundPlaySetDisplayObjective, w)
		ts.sbShown, ts.sbName, ts.sbLines = true, sb.Name(), ts.sbLines[:0]
	} else if ts.sbName != sb.Name() {
		w := s.packet()
		w.String(sidebarObjective)
		w.Byte(objectiveUpdate)
		writeObjectiveInfo(w, sb.Name())
		s.queue(v777.ClientboundPlaySetObjective, w)
		ts.sbName = sb.Name()
	}
	// Java sorts the sidebar by score, highest first. Dragonfly puts lines[0] on top (or, for a
	// descending board, the last line: Lines() is already reversed and Bedrock sorts descending).
	desc := sb.Descending()
	n := len(lines)
	for i, line := range lines {
		score := int32(len(lineOwners) - i)
		if desc {
			score = int32(i + 1)
		}
		if i < len(ts.sbLines) && ts.sbLines[i] == line && desc == ts.sbDesc {
			continue
		}
		w := s.packet()
		w.String(lineOwners[i])
		w.String(sidebarObjective)
		w.VarInt(score)
		w.Bool(true)
		writeText(w, line)
		w.Bool(false) // the objective's number format
		s.queue(v777.ClientboundPlaySetScore, w)
	}
	for i := n; i < len(ts.sbLines); i++ {
		w := s.packet()
		w.String(lineOwners[i])
		w.Bool(true)
		w.String(sidebarObjective)
		s.queue(v777.ClientboundPlayResetScore, w)
	}
	ts.sbLines, ts.sbDesc = append(ts.sbLines[:0], lines...), desc
}

// writeObjectiveInfo writes the add/update part of set_objective: title, integer render type and
// the blank number format.
func writeObjectiveInfo(w *wire.Writer, title string) {
	writeText(w, title)
	w.VarInt(0)  // render type: integer
	w.Bool(true) // has a number format
	w.VarInt(0)  // blank
}

// RemoveScoreboard removes the sidebar.
func (s *Session) RemoveScoreboard() {
	ts := s.txt()
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if !ts.sbShown {
		return
	}
	w := s.packet()
	w.String(sidebarObjective)
	w.Byte(objectiveRemove)
	s.queue(v777.ClientboundPlaySetObjective, w)
	ts.sbShown, ts.sbName, ts.sbLines = false, "", ts.sbLines[:0]
}

// Name tags. A Java client draws a player's name from its profile name, decorated by the
// player's scoreboard team (prefix, colour, suffix); the custom_name entity data does nothing on
// players. So a Bedrock name tag is shown with one client-side team per player: the text before
// the player's name becomes the prefix, the text after it the suffix, the colour at the name the
// team colour. An empty name tag hides the name. Score tags (Bedrock's line under the name) use
// a below_name objective whose scores carry the tag as a fixed number format.

// ViewNameTag shows a different name tag for e to this client only.
func (s *Session) ViewNameTag(e world.Entity, nameTag string) {
	ts := s.txt()
	ts.mu.Lock()
	if ts.nameOverride == nil {
		ts.nameOverride = map[*world.EntityHandle]string{}
	}
	ts.nameOverride[e.H()] = nameTag
	ts.mu.Unlock()
	s.applyNameTag(e, nameTag)
}

// ViewPublicNameTag removes this client's name tag override of e.
func (s *Session) ViewPublicNameTag(e world.Entity) {
	ts := s.txt()
	ts.mu.Lock()
	delete(ts.nameOverride, e.H())
	ts.mu.Unlock()
	if p, ok := e.(*player.Player); ok {
		s.applyNameTag(e, p.NameTag())
	}
}

// ViewAlwaysShowNameTag: Java shows player names within 64 blocks either way.
func (s *Session) ViewAlwaysShowNameTag(world.Entity, bool) {}

// ViewPublicAlwaysShowNameTag: see ViewAlwaysShowNameTag.
func (s *Session) ViewPublicAlwaysShowNameTag(world.Entity) {}

// viewPlayerNameTag shows p's current name tag (this client's override, or the public one). Call
// it when a player comes into view and when its name tag changes (ViewEntity, ViewEntityState).
func (s *Session) viewPlayerNameTag(p *player.Player) {
	ts := s.txt()
	ts.mu.Lock()
	tag, ok := ts.nameOverride[p.H()]
	scoreTag, sok := ts.scoreOverride[p.H()]
	ts.mu.Unlock()
	if !ok {
		tag = p.NameTag()
	}
	s.applyNameTag(p, tag)
	if !sok {
		scoreTag = p.ScoreTag()
	}
	s.applyScoreTag(p, scoreTag)
}

// applyNameTag sends the team that makes e's name read as tag.
func (s *Session) applyNameTag(e world.Entity, tag string) {
	p, ok := e.(*player.Player)
	if !ok {
		return // only players are shown to Java clients so far
	}
	name := tabName(p.Name())
	u := p.UUID()
	team := "df_tag" + hex.EncodeToString(u[:8])
	ts := s.txt()
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.teams == nil {
		ts.teams = map[uuid.UUID]string{}
	}
	_, exists := ts.teams[u]
	if tag == name || tag == p.Name() {
		if exists { // plain name again: drop the team
			w := s.packet()
			w.String(team)
			w.Byte(1)
			s.queue(v777.ClientboundPlaySetPlayerTeam, w)
			delete(ts.teams, u)
		}
		return
	}
	if line, _, ok := strings.Cut(tag, "\n"); ok && strings.Contains(line, name) {
		tag = line
	} else if ok {
		// The name is on a later line: keep that line.
		for _, l := range strings.Split(tag, "\n") {
			if strings.Contains(l, name) {
				tag = l
				break
			}
		}
	}
	var prefix, suffix text.Component
	colour := int32(-1)
	visible := tag != ""
	if i := strings.Index(tag, name); i >= 0 {
		prefix, suffix = bedrockText(tag[:i]), bedrockText(tag[i+len(name):])
		colour = teamColour(lastColour(tag[:i]))
	} else if visible {
		prefix = bedrockText(tag + "§r ")
	}
	w := s.packet()
	w.String(team)
	if exists {
		w.Byte(2) // update
	} else {
		w.Byte(0) // create
	}
	text.WriteString(w, "")
	prefix.Write(w)
	suffix.Write(w)
	if visible {
		w.VarInt(0) // name tag: always
	} else {
		w.VarInt(1) // never
	}
	w.VarInt(0) // collision: always
	if colour >= 0 {
		w.Bool(true)
		w.VarInt(colour)
	} else {
		w.Bool(false)
	}
	w.Byte(0) // no friendly fire flags
	if !exists {
		w.VarInt(1)
		w.String(name)
	}
	s.queue(v777.ClientboundPlaySetPlayerTeam, w)
	ts.teams[u] = team
}

// lastColour returns the last colour code in s ("" if none or reset after it).
func lastColour(s string) string {
	col := ""
	for {
		i := strings.Index(s, "§")
		if i < 0 || i+2 >= len(s) {
			return col
		}
		c := s[i+2]
		if c == 'r' || c == 'R' {
			col = ""
		} else if n := text.ColourName(c, text.Bedrock); n != "" {
			col = n
		}
		s = s[i+3:]
	}
}

var teamColours = map[string]int32{"black": 0, "dark_blue": 1, "dark_green": 2, "dark_aqua": 3,
	"dark_red": 4, "dark_purple": 5, "gold": 6, "gray": 7, "dark_gray": 8, "blue": 9, "green": 10,
	"aqua": 11, "red": 12, "light_purple": 13, "yellow": 14, "white": 15,
	// Bedrock-only colours: the nearest of the 16 (teams only take those).
	"#DDD605": 14, "#E3D4D1": 15, "#CECACA": 7, "#443A3B": 8, "#971607": 4, "#B4684D": 6,
	"#DEB12D": 6, "#47A036": 2, "#2CBAA8": 3, "#21497B": 1, "#9A5CC6": 5, "#EB7114": 6}

func teamColour(name string) int32 {
	if c, ok := teamColours[name]; ok {
		return c
	}
	return -1
}

// ViewScoreTag shows a different score tag (the line under the name) for e to this client only.
func (s *Session) ViewScoreTag(e world.Entity, scoreTag string) {
	ts := s.txt()
	ts.mu.Lock()
	if ts.scoreOverride == nil {
		ts.scoreOverride = map[*world.EntityHandle]string{}
	}
	ts.scoreOverride[e.H()] = scoreTag
	ts.mu.Unlock()
	s.applyScoreTag(e, scoreTag)
}

// ViewPublicScoreTag removes this client's score tag override of e.
func (s *Session) ViewPublicScoreTag(e world.Entity) {
	ts := s.txt()
	ts.mu.Lock()
	delete(ts.scoreOverride, e.H())
	ts.mu.Unlock()
	if p, ok := e.(*player.Player); ok {
		s.applyScoreTag(e, p.ScoreTag())
	}
}

// applyScoreTag shows tag under e's name: a below_name score with the tag as its fixed number
// format. The objective's own format is blank, so players without a tag show nothing.
func (s *Session) applyScoreTag(e world.Entity, tag string) {
	p, ok := e.(*player.Player)
	if !ok {
		return
	}
	name := tabName(p.Name())
	u := p.UUID()
	ts := s.txt()
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if tag == "" {
		if owner, shown := ts.scoreShown[u]; shown {
			s.resetScoreTag(owner)
			delete(ts.scoreShown, u)
		}
		return
	}
	if !ts.belowName {
		w := s.packet()
		w.String(belowObjective)
		w.Byte(objectiveAdd)
		writeObjectiveInfo(w, "")
		s.queue(v777.ClientboundPlaySetObjective, w)
		w = s.packet()
		w.VarInt(slotBelowName)
		w.String(belowObjective)
		s.queue(v777.ClientboundPlaySetDisplayObjective, w)
		ts.belowName = true
	}
	if ts.scoreShown == nil {
		ts.scoreShown = map[uuid.UUID]string{}
	}
	if owner, shown := ts.scoreShown[u]; shown && owner != name {
		s.resetScoreTag(owner) // renamed: the old score holder would stay
	}
	w := s.packet()
	w.String(name)
	w.String(belowObjective)
	w.VarInt(0)
	w.Bool(false) // no display name
	w.Bool(true)  // number format:
	w.VarInt(2)   // fixed
	writeText(w, tag)
	s.queue(v777.ClientboundPlaySetScore, w)
	ts.scoreShown[u] = name
}

// resetScoreTag removes the below_name score of the score holder owner. ts.mu is held.
func (s *Session) resetScoreTag(owner string) {
	w := s.packet()
	w.String(owner)
	w.Bool(true)
	w.String(belowObjective)
	s.queue(v777.ClientboundPlayResetScore, w)
}

// forgetEntityText drops what this client was sent about e's name and score tag: the team that
// decorates its name is removed and its below_name score reset, so nothing stale is left when e
// comes back (a player that logs in again under the same UUID with a different tag) and the
// tables do not grow with every entity the session ever saw. Overrides of entities that are
// gone for good (closed handles) are dropped too.
//
// Hook: entity.go, HideEntity, after the remove_entities packet: s.forgetEntityText(e)
func (s *Session) forgetEntityText(e world.Entity) {
	p, ok := e.(*player.Player)
	if !ok {
		return
	}
	u := p.UUID()
	v, ok := texts.Load(s)
	if !ok {
		return // no text state yet: nothing was sent
	}
	ts := v.(*textState)
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if team, ok := ts.teams[u]; ok {
		w := s.packet()
		w.String(team)
		w.Byte(1) // remove
		s.queue(v777.ClientboundPlaySetPlayerTeam, w)
		delete(ts.teams, u)
	}
	if owner, ok := ts.scoreShown[u]; ok {
		s.resetScoreTag(owner)
		delete(ts.scoreShown, u)
	}
	ts.pruneClosedOverrides()
}

// pruneClosedOverrides drops the name and score tag overrides of entities whose handle is closed
// (a player that left: a new login gets a new handle). ts.mu is held.
func (ts *textState) pruneClosedOverrides() {
	for h := range ts.nameOverride {
		if h.Closed() {
			delete(ts.nameOverride, h)
		}
	}
	for h := range ts.scoreOverride {
		if h.Closed() {
			delete(ts.scoreOverride, h)
		}
	}
}
