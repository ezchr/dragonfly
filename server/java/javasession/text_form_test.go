package javasession

import (
	"testing"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player/form"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/ezchr/go-mcjava/wire"
)

type testCustom struct {
	L form.Label
	I form.Input
	T form.Toggle
	S form.Slider
	D form.Dropdown
}

func (testCustom) Submit(form.Submitter, *world.Tx) {}

type testMenu struct{ A, B form.Button }

func (testMenu) Submit(form.Submitter, form.Button, *world.Tx) {}

// TestFormResponse checks that dialog answers become the Bedrock response JSON SubmitJSON takes.
func TestFormResponse(t *testing.T) {
	// The payload a client sends for the custom form: additions {f, b} plus the input values.
	var p wire.Writer
	p.Byte(10)
	for _, e := range []struct {
		tag  byte
		name string
		val  func()
	}{
		{3, "f", func() { p.Int32(4) }},
		{3, "b", func() { p.Int32(-1) }},
		{8, "e1", func() { p.Uint16(2); p.Raw([]byte("hi")) }},
		{1, "e2", func() { p.Byte(1) }},
		{5, "e3", func() { p.Float32(2.9999) }},
		{8, "e4", func() { p.Uint16(1); p.Raw([]byte("2")) }},
	} {
		p.Byte(e.tag)
		p.Uint16(uint16(len(e.name)))
		p.Raw([]byte(e.name))
		e.val()
	}
	p.Byte(0)
	vals, err := readClickPayload(p.B)
	if err != nil {
		t.Fatal(err)
	}
	f := form.New(testCustom{
		L: form.NewLabel("l"),
		I: form.NewInput("i", "", ""),
		T: form.NewToggle("t", false),
		S: form.NewSlider("s", 0, 10, 1, 0),
		D: form.NewDropdown("d", []string{"a", "b", "c"}, 0),
	}, "c")
	b, err := formResponse(f, vals["b"].(int32), vals)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `[null,"hi",true,3,2]` {
		t.Errorf("custom response %s", b)
	}
	m := form.NewMenu(testMenu{A: form.NewButton("a", ""), B: form.NewButton("b", "")}, "m")
	if b, _ := formResponse(m, 1, nil); string(b) != "1" {
		t.Errorf("menu response %s", b)
	}
	if b, _ := formResponse(m, -2, nil); b != nil {
		t.Errorf("closed response %s", b)
	}
	// No payload at all (tag type 0).
	if v, err := readClickPayload([]byte{0}); err != nil || len(v) != 0 {
		t.Errorf("empty payload: %v %v", v, err)
	}
}

type testCmd struct {
	Sub    cmd.SubCommand `cmd:"give"`
	Target []cmd.Target   `cmd:"target"`
	Amount cmd.Optional[int]
}

func (testCmd) Run(cmd.Source, *cmd.Output, *world.Tx) {}

// TestCommandTree checks the Brigadier graph of a command with a subcommand, targets and an
// optional int.
func TestCommandTree(t *testing.T) {
	cmd.Register(cmd.New("jtest", "", []string{"jt"}, testCmd{}))
	tr := buildCommandTree(nil)
	byName := map[string]cmdNode{}
	for _, n := range tr.nodes {
		byName[n.name] = n
	}
	if n := byName["jtest"]; n.kind != nodeLiteral || n.exec || len(n.children) != 1 {
		t.Errorf("jtest node %+v", n)
	}
	if n := byName["jt"]; n.redirect < 0 || tr.nodes[n.redirect].name != "jtest" {
		t.Errorf("alias node %+v", n)
	}
	if n := byName["target"]; n.kind != nodeArgument || n.parser != parserEntity || !n.exec {
		t.Errorf("target node %+v", n)
	}
	if n := byName["Amount"]; n.parser != parserInteger || !n.exec {
		t.Errorf("amount node %+v", n)
	}
	var w wire.Writer
	tr.encode(&w, nil)
	if r := wire.NewReader(w.B); r.VarInt() != int32(len(tr.nodes)) {
		t.Error("node count")
	}
}
