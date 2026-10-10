package session

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// Checks for bedrocktool, a proxy that saves every chunk a server sends into a world file, and for
// clients that are not the game itself. They catch the tool as people download it today, not world
// copying in general: a server has to send its chunks, and the tool is open source.
//
//   - radius: a very large view distance (the tool forces 76).
//   - contradiction: a view distance above the maximum the same packet states.
//   - maxzero: no maximum at all. The tool's own request leaves it out.
//   - title: the Xbox title the player signed in with belongs to another device than the one the
//     client says it is (the tool signs in as Android by default, whatever the real device is).
//   - notitle: the login names no Xbox title at all. Not the tool: a sign of an unofficial client,
//     if the game always names one.
//   - notitle_maxzero: both at once, no Xbox title and no maximum view distance. Kicks.
//
// Every rule a player trips is logged and flags the player for good, in toolcheck_flags.json in
// the working directory (by XUID: name, which rules, how often, first and last time). A rule set
// to kick also disconnects them. DF_TOOLCHECK sets what each rule does, for example
// "contradiction=kick,maxzero=flag,notitle=off"; a value is off, flag or kick ("log" means flag),
// and a bare value sets all of them. Unset rules keep their default. radius and notitle never
// kick. Players whose XUID is a line of toolcheck_allow.txt (read at every check, "#" starts a
// comment) are never checked.

const (
	toolOff = iota
	toolFlag
	toolKick
)

const (
	ruleRadius        = "radius"
	ruleContradiction = "contradiction"
	ruleMaxZero       = "maxzero"
	ruleTitle         = "title"
	ruleNoTitle       = "notitle"
	// ruleNoTitleMaxZero is notitle and maxzero together: neither kicks alone, the pair does.
	ruleNoTitleMaxZero = "notitle_maxzero"
)

const (
	// toolAllowFile lists the XUIDs that are never checked, one a line.
	toolAllowFile = "toolcheck_allow.txt"
	// toolFlagFile holds the flagged players.
	toolFlagFile = "toolcheck_flags.json"
	// toolKickMessage does not say which check it was.
	toolKickMessage = "Flagged by anticheat"
	// suspiciousRadius is the view distance, in chunks, from which a request is flagged.
	suspiciousRadius = 64
)

// toolRules is what each rule does. The contradiction and the pair of notitle and maxzero kick by
// default; the single rules rest on assumptions about real clients that were not measured yet.
var toolRules = parseToolRules(os.Getenv("DF_TOOLCHECK"))

func parseToolRules(v string) map[string]int {
	rules := map[string]int{ruleRadius: toolFlag, ruleContradiction: toolKick, ruleMaxZero: toolFlag, ruleTitle: toolFlag, ruleNoTitle: toolFlag, ruleNoTitleMaxZero: toolKick}
	mode := func(s string) (int, bool) {
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "off":
			return toolOff, true
		case "flag", "log":
			return toolFlag, true
		case "kick":
			return toolKick, true
		}
		return 0, false
	}
	for _, part := range strings.Split(v, ",") {
		name, value, pair := strings.Cut(part, "=")
		if !pair {
			if m, ok := mode(part); ok {
				for rule := range rules {
					rules[rule] = m
				}
			}
			continue
		}
		name = strings.ToLower(strings.TrimSpace(name))
		if _, known := rules[name]; !known {
			continue
		}
		if m, ok := mode(value); ok {
			rules[name] = m
		}
	}
	// A large view distance or an unofficial client alone is never a reason to kick.
	for _, rule := range []string{ruleRadius, ruleNoTitle} {
		if rules[rule] == toolKick {
			rules[rule] = toolFlag
		}
	}
	return rules
}

// toolVerdict drops the signals whose rule is off, and reports whether one of the rest kicks.
func toolVerdict(signals []string) (active []string, kick bool) {
	for _, rule := range signals {
		switch toolRules[rule] {
		case toolKick:
			kick = true
			active = append(active, rule)
		case toolFlag:
			active = append(active, rule)
		}
	}
	return active, kick
}

// radiusSignals are the rules a view-distance request trips, from a player who signed in with the
// Xbox title passed.
func radiusSignals(radius int32, max uint8, titleID string) []string {
	var out []string
	switch {
	case max == 0:
		out = append(out, ruleMaxZero)
		if strings.TrimSpace(titleID) == "" {
			out = append(out, ruleNoTitleMaxZero)
		}
	case radius > int32(max):
		out = append(out, ruleContradiction)
	}
	if radius >= suspiciousRadius {
		out = append(out, ruleRadius)
	}
	return out
}

// titleDevices are the devices each known Xbox title of the game runs on.
var titleDevices = map[string][]protocol.DeviceOS{
	"1739947436": {protocol.DeviceAndroid},
	"1810924247": {protocol.DeviceIOS},
	"1944307183": {protocol.DeviceFireOS},
	"896928775":  {protocol.DeviceWin10, protocol.DeviceWin32},
	"2044456598": {protocol.DeviceOrbis},
	"2047319603": {protocol.DeviceNX},
	"1828326430": {protocol.DeviceXBOX},
	// Preview
	"1904044383": {protocol.DeviceWin10, protocol.DeviceWin32, protocol.DeviceIOS, protocol.DeviceXBOX},
}

// titleMismatch reports whether a known title and a known device disagree. An empty or unknown
// title, or a device none of the titles runs on, is no mismatch.
func titleMismatch(titleID string, device protocol.DeviceOS) bool {
	devices, known := titleDevices[titleID]
	if !known {
		return false
	}
	knownDevice := false
	for _, ds := range titleDevices {
		for _, d := range ds {
			knownDevice = knownDevice || d == device
		}
	}
	if !knownDevice {
		return false
	}
	for _, d := range devices {
		if d == device {
			return false
		}
	}
	return true
}

// titleSignals are the rules the Xbox title of a login trips.
func titleSignals(titleID string, device protocol.DeviceOS) []string {
	switch {
	case strings.TrimSpace(titleID) == "":
		return []string{ruleNoTitle}
	case titleMismatch(titleID, device):
		return []string{ruleTitle}
	}
	return nil
}

// toolAllowed reports whether the XUID is listed in toolAllowFile.
func toolAllowed(xuid string) bool {
	if xuid == "" {
		return false
	}
	b, err := os.ReadFile(toolAllowFile)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(b), "\n") {
		line, _, _ = strings.Cut(line, "#")
		if strings.TrimSpace(line) == xuid {
			return true
		}
	}
	return false
}

// flaggedPlayer is one player of toolFlagFile.
type flaggedPlayer struct {
	Name string `json:"name"`
	// Rules counts how often the player tripped each rule.
	Rules  map[string]int `json:"rules"`
	First  string         `json:"first"`
	Last   string         `json:"last"`
	Detail string         `json:"detail"` // what was seen the last time
}

var toolFlagMu sync.Mutex

// flagPlayer records, for good, that the player tripped the rules passed. detail says what was
// seen. An error is returned for the caller to log; the check itself goes on.
func flagPlayer(xuid, name string, rules []string, detail string) error {
	if xuid == "" || len(rules) == 0 {
		return nil
	}
	toolFlagMu.Lock()
	defer toolFlagMu.Unlock()
	flagged := map[string]*flaggedPlayer{}
	if b, err := os.ReadFile(toolFlagFile); err == nil {
		if err := json.Unmarshal(b, &flagged); err != nil {
			// Never overwrite a file that could not be read: keep it aside.
			_ = os.Rename(toolFlagFile, toolFlagFile+".unreadable")
			flagged = map[string]*flaggedPlayer{}
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	p := flagged[xuid]
	if p == nil {
		p = &flaggedPlayer{Rules: map[string]int{}, First: now}
		flagged[xuid] = p
	}
	if p.Rules == nil {
		p.Rules = map[string]int{}
	}
	p.Name, p.Last, p.Detail = name, now, detail
	for _, rule := range rules {
		p.Rules[rule]++
	}
	b, err := json.MarshalIndent(flagged, "", "  ")
	if err != nil {
		return err
	}
	tmp := toolFlagFile + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, toolFlagFile)
}

// addrIP matches the IP addresses in the text of a connection address.
var addrIP = regexp.MustCompile(`\[([0-9a-fA-F:.]+)\]|(\d{1,3}(?:\.\d{1,3}){3})`)

// toolExempt reports whether a connection from addr is one of ours: this machine's own services
// (the relay, bots, the editor), which connect from loopback or from one of its own addresses. A
// NetherNet address is text with the resolved address of the peer last, so the last IP counts.
func toolExempt(addr net.Addr) bool {
	if addr == nil {
		return false
	}
	found := addrIP.FindAllStringSubmatch(addr.String(), -1)
	if len(found) == 0 {
		return false
	}
	last := found[len(found)-1]
	ip := net.ParseIP(last[1] + last[2])
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	own, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, a := range own {
		if n, ok := a.(*net.IPNet); ok && n.IP.Equal(ip) {
			return true
		}
	}
	return false
}

// toolCheck runs the checks on a view-distance request, and the title checks on the first one of
// a session. It reports whether the player was kicked.
func (h *RequestChunkRadiusHandler) toolCheck(pk *packet.RequestChunkRadius, s *Session) bool {
	first := !h.seen
	id := s.conn.IdentityData()
	if first {
		h.seen, h.exempt = true, toolExempt(s.conn.RemoteAddr()) || toolAllowed(id.XUID)
	}
	if h.exempt {
		return false
	}
	cd := s.conn.ClientData()
	raw := radiusSignals(pk.ChunkRadius, pk.MaxChunkRadius, id.TitleID)
	if first {
		raw = append(raw, titleSignals(id.TitleID, cd.DeviceOS)...)
	}
	signals, kick := toolVerdict(raw)
	if first || len(signals) > 0 {
		s.conf.Log.Info("toolcheck: view distance", "player", id.DisplayName, "xuid", id.XUID, "radius", pk.ChunkRadius, "max", pk.MaxChunkRadius,
			"deviceOS", int(cd.DeviceOS), "titleID", id.TitleID, "signals", signals, "remote", s.conn.RemoteAddr())
	}
	detail := fmt.Sprintf("view distance %d, max %d, title %q, device %d", pk.ChunkRadius, pk.MaxChunkRadius, id.TitleID, int(cd.DeviceOS))
	if err := flagPlayer(id.XUID, id.DisplayName, signals, detail); err != nil {
		s.conf.Log.Warn("toolcheck: flag not saved", "err", err)
	}
	if kick {
		s.conf.Log.Warn("toolcheck: kicked", "rules", signals, "player", id.DisplayName, "xuid", id.XUID, "radius", pk.ChunkRadius, "max", pk.MaxChunkRadius)
		s.Disconnect(toolKickMessage)
		return true
	}
	return false
}
