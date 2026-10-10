package session

import (
	"encoding/json"
	"net"
	"os"
	"reflect"
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

func TestRadiusSignals(t *testing.T) {
	for _, c := range []struct {
		radius int32
		max    uint8
		title  string
		want   []string
	}{
		// Honest clients: a view distance at or below their own maximum.
		{12, 32, "896928775", nil},
		{16, 16, "896928775", nil},
		{32, 32, "", nil},
		{96, 96, "896928775", []string{ruleRadius}},
		// bedrocktool: its own request, and a rewritten one of the client.
		{76, 0, "1739947436", []string{ruleMaxZero, ruleRadius}},
		{76, 32, "1739947436", []string{ruleContradiction, ruleRadius}},
		{8, 0, "1739947436", []string{ruleMaxZero}},
		// No Xbox title and no maximum: the pair.
		{8, 0, "", []string{ruleMaxZero, ruleNoTitleMaxZero}},
		{76, 0, " ", []string{ruleMaxZero, ruleNoTitleMaxZero, ruleRadius}},
	} {
		if got := radiusSignals(c.radius, c.max, c.title); !reflect.DeepEqual(got, c.want) {
			t.Errorf("radius %d, max %d, title %q: %v, want %v", c.radius, c.max, c.title, got, c.want)
		}
	}
}

func TestTitleMismatch(t *testing.T) {
	const android, ios, windows, preview = "1739947436", "1810924247", "896928775", "1904044383"
	for _, c := range []struct {
		title  string
		device protocol.DeviceOS
		want   bool
	}{
		{android, protocol.DeviceAndroid, false},
		{android, protocol.DeviceWin10, true}, // the default sign-in of the tool in front of a Windows client
		{android, protocol.DeviceIOS, true},
		{ios, protocol.DeviceIOS, false},
		{windows, protocol.DeviceWin10, false},
		{windows, protocol.DeviceWin32, false},
		{windows, protocol.DeviceAndroid, true},
		{preview, protocol.DeviceWin10, false},
		{preview, protocol.DeviceIOS, false},
		{preview, protocol.DeviceXBOX, false},
		{preview, protocol.DeviceAndroid, true},
		// Nothing to compare.
		{"", protocol.DeviceWin10, false},
		{"12345", protocol.DeviceWin10, false},
		{android, protocol.DeviceLinux, false},
		{android, protocol.DeviceOS(0), false},
	} {
		if got := titleMismatch(c.title, c.device); got != c.want {
			t.Errorf("title %q on device %d: %v, want %v", c.title, c.device, got, c.want)
		}
	}
}

func TestTitleSignals(t *testing.T) {
	for _, c := range []struct {
		title  string
		device protocol.DeviceOS
		want   []string
	}{
		{"896928775", protocol.DeviceWin10, nil},
		{"1739947436", protocol.DeviceWin10, []string{ruleTitle}},
		{"", protocol.DeviceWin10, []string{ruleNoTitle}},
		{"  ", protocol.DeviceAndroid, []string{ruleNoTitle}},
		{"12345", protocol.DeviceWin10, nil}, // a title we do not know is not a missing one
	} {
		if got := titleSignals(c.title, c.device); !reflect.DeepEqual(got, c.want) {
			t.Errorf("title %q on device %d: %v, want %v", c.title, c.device, got, c.want)
		}
	}
}

func TestParseToolRules(t *testing.T) {
	def := parseToolRules("")
	want := map[string]int{ruleContradiction: toolKick, ruleMaxZero: toolFlag, ruleTitle: toolFlag, ruleRadius: toolFlag, ruleNoTitle: toolFlag, ruleNoTitleMaxZero: toolKick}
	if !reflect.DeepEqual(def, want) {
		t.Errorf("defaults = %v, want %v", def, want)
	}
	for rule, m := range parseToolRules("off") {
		if m != toolOff {
			t.Errorf("off: %s = %d", rule, m)
		}
	}
	mixed := parseToolRules("maxzero=kick, title=off, radius=kick, notitle=kick, nonsense=kick, contradiction=what, MaxZero = KICK")
	if mixed[ruleMaxZero] != toolKick || mixed[ruleTitle] != toolOff || mixed[ruleContradiction] != toolKick {
		t.Errorf("mixed = %v", mixed)
	}
	if mixed[ruleRadius] != toolFlag || mixed[ruleNoTitle] != toolFlag {
		t.Errorf("radius and notitle must never kick, got %d and %d", mixed[ruleRadius], mixed[ruleNoTitle])
	}
	if all := parseToolRules("kick"); all[ruleTitle] != toolKick || all[ruleNoTitle] != toolFlag {
		t.Errorf("bare kick = %v", all)
	}
	if parseToolRules("log")[ruleContradiction] != toolFlag {
		t.Error("log should mean flag")
	}
}

func TestToolVerdict(t *testing.T) {
	old := toolRules
	defer func() { toolRules = old }()
	toolRules = parseToolRules("contradiction=kick,maxzero=flag,radius=off")
	if active, kick := toolVerdict([]string{ruleMaxZero, ruleRadius}); kick || !reflect.DeepEqual(active, []string{ruleMaxZero}) {
		t.Errorf("flag only: %v, kick %v", active, kick)
	}
	if active, kick := toolVerdict([]string{ruleContradiction, ruleRadius}); !kick || !reflect.DeepEqual(active, []string{ruleContradiction}) {
		t.Errorf("kick: %v, kick %v", active, kick)
	}
	if active, kick := toolVerdict(nil); kick || active != nil {
		t.Errorf("nothing: %v, kick %v", active, kick)
	}
}

func TestToolAllowed(t *testing.T) {
	t.Chdir(t.TempDir())
	if toolAllowed("2535406275782630") {
		t.Error("allowed without a file")
	}
	if err := os.WriteFile(toolAllowFile, []byte("# admins\n2535406275782630 # ZIDSMP\n\n  2535416316089810\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for xuid, want := range map[string]bool{"2535406275782630": true, "2535416316089810": true, "2535400000000000": false, "": false} {
		if got := toolAllowed(xuid); got != want {
			t.Errorf("%q allowed = %v, want %v", xuid, got, want)
		}
	}
}

func TestFlagPlayer(t *testing.T) {
	t.Chdir(t.TempDir())
	read := func() map[string]flaggedPlayer {
		b, err := os.ReadFile(toolFlagFile)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]flaggedPlayer
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	if err := flagPlayer("1", "Alex", []string{ruleNoTitle}, "first"); err != nil {
		t.Fatal(err)
	}
	if err := flagPlayer("1", "Alex2", []string{ruleNoTitle, ruleMaxZero}, "second"); err != nil {
		t.Fatal(err)
	}
	if err := flagPlayer("2", "Sam", []string{ruleTitle}, "other"); err != nil {
		t.Fatal(err)
	}
	if err := flagPlayer("", "nobody", []string{ruleTitle}, ""); err != nil {
		t.Fatal(err)
	}
	if err := flagPlayer("3", "clean", nil, ""); err != nil {
		t.Fatal(err)
	}
	m := read()
	if len(m) != 2 {
		t.Fatalf("%d players flagged, want 2: %v", len(m), m)
	}
	a := m["1"]
	if a.Name != "Alex2" || a.Rules[ruleNoTitle] != 2 || a.Rules[ruleMaxZero] != 1 || a.Detail != "second" || a.First == "" || a.Last == "" {
		t.Errorf("player 1 = %+v", a)
	}
	// A file that cannot be read is kept aside, not overwritten.
	if err := os.WriteFile(toolFlagFile, []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := flagPlayer("4", "New", []string{ruleTitle}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(toolFlagFile + ".unreadable"); err != nil {
		t.Errorf("the unreadable file was not kept: %v", err)
	}
	if m := read(); len(m) != 1 || m["4"].Name != "New" {
		t.Errorf("after an unreadable file: %v", m)
	}
}

func TestToolExempt(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1:19136": true,
		"[::1]:5000":      true,
		"8.8.8.8:1234":    false,
	} {
		a, err := net.ResolveUDPAddr("udp", addr)
		if err != nil {
			t.Fatal(err)
		}
		if got := toolExempt(a); got != want {
			t.Errorf("%s exempt = %v, want %v", addr, got, want)
		}
	}
	if toolExempt(nil) {
		t.Error("no address is not exempt")
	}
	// NetherNet addresses are text; the resolved peer address comes last.
	for text, want := range map[string]bool{
		"123 (456) (udp4 prflx 204.83.251.251:63264 related :0 (resolved: 204.83.251.251:63264))": false,
		"123 (456) (udp4 host 10.0.0.5:1 (resolved: 127.0.0.1:57207))":                            true,
		"no address here": false,
	} {
		if got := toolExempt(textAddr(text)); got != want {
			t.Errorf("%q exempt = %v, want %v", text, got, want)
		}
	}
}

type textAddr string

func (textAddr) Network() string  { return "nethernet" }
func (a textAddr) String() string { return string(a) }
