package text

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/ezchr/go-mc/java/wire"
)

// Components and the bytes a vanilla 26.3 server sent for them (system_chat / set_title_text
// payloads captured with tools/pktcap from tellraw/title/scoreboard console commands, minus the
// packets' trailing fields).
var vanillaCases = []struct {
	name string
	c    Component
	hex  string
}{
	{`tellraw "plain"`, Plain("plain"), "080005706c61696e"},
	{`{"text":"a","color":"red","bold":true,"extra":[{"text":"b","color":"#DDD605"},"c"]}`,
		Component{Text: "a", Color: "red", Bold: On, Extra: []Component{{Text: "b", Color: "#DDD605"}, Plain("c")}},
		"0a080005636f6c6f72000372656409000565787472610a00000002080005636f6c6f7200072344444436303508000474657874000162000800000001630008000474657874000161010004626f6c640100"},
	{`click/hover/flags`,
		Component{Text: "x", Italic: Off, Underlined: On, Strikethrough: On, Obfuscated: On,
			Click: &ClickEvent{Action: ClickRunCommand, Value: "/foo bar"}, Hover: &Component{Text: "hi"}},
		"0a0a000b636c69636b5f6576656e74080006616374696f6e000b72756e5f636f6d6d616e64080007636f6d6d616e6400082f666f6f206261720001000a756e6465726c696e6564010800047465787400017801000d737472696b657468726f756768010a000b686f7665725f6576656e74080006616374696f6e000973686f775f7465787408000576616c756500026869000100066974616c69630001000a6f6266757363617465640100"},
	{`translate with`,
		Component{Translate: "chat.type.text", With: []Component{Plain("a"), {Text: "b", Color: "aqua"}}},
		"0a090004776974680a0000000208000000016100080005636f6c6f7200046171756108000474657874000162000800097472616e736c617465000e636861742e747970652e7465787400"},
	{`["",{gray p},{q}]`,
		Component{Extra: []Component{{Text: "p", Color: "gray"}, Plain("q")}},
		"0a09000565787472610a00000002080005636f6c6f7200046772617908000474657874000170000800000001710008000474657874000000"},
	{`title gold T`, Component{Text: "T", Color: "gold"}, "0a080005636f6c6f720004676f6c640800047465787400015400"},
}

func TestVanillaBytes(t *testing.T) {
	for _, tc := range vanillaCases {
		want, _ := hex.DecodeString(tc.hex)
		got := tc.c.Bytes()
		if !bytes.Equal(got, want) {
			t.Errorf("%s:\n got %x\nwant %x", tc.name, got, want)
		}
	}
}

func TestLegacy(t *testing.T) {
	cases := []struct {
		in   string
		p    Palette
		want Component
	}{
		{"hello", Bedrock, Plain("hello")},
		{"§ahello", Java, Component{Text: "hello", Color: "green"}},
		{"a§lb§cc", Java, Component{Text: "a", Extra: []Component{{Text: "b", Bold: On}, {Text: "c", Color: "red"}}}},
		{"a§lb§cc", Bedrock, Component{Text: "a", Extra: []Component{{Text: "b", Bold: On}, {Text: "c", Color: "red", Bold: On}}}},
		{"§mX§nY", Java, Component{Extra: []Component{{Text: "X", Strikethrough: On}, {Text: "Y", Strikethrough: On, Underlined: On}}}},
		{"§mX§nY", Bedrock, Component{Extra: []Component{{Text: "X", Color: "#971607"}, {Text: "Y", Color: "#B4684D"}}}},
		{"§gGold§r plain§z§", Bedrock, Component{Extra: []Component{{Text: "Gold", Color: "#DDD605"}, {Text: " plain"}}}},
		{"§l§eÄö§r", Bedrock, Component{Text: "Äö", Color: "yellow", Bold: On}},
	}
	for _, tc := range cases {
		got := Legacy(tc.in, tc.p)
		if !bytes.Equal(got.Bytes(), tc.want.Bytes()) {
			t.Errorf("Legacy(%q): got %x want %x", tc.in, got.Bytes(), tc.want.Bytes())
		}
	}
	if s := Strip("§l§eA§zb§"); s != "Ab" {
		t.Errorf("Strip: %q", s)
	}
}

func TestModifiedUTF8(t *testing.T) {
	var w wire.Writer
	String(&w, "a\x00é😀")
	want := []byte{0, 11, 'a', 0xc0, 0x80, 0xc3, 0xa9, 0xed, 0xa0, 0xbd, 0xed, 0xb8, 0x80}
	if !bytes.Equal(w.B, want) {
		t.Errorf("got %x want %x", w.B, want)
	}
}

func BenchmarkLegacyWrite(b *testing.B) {
	var w wire.Writer
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		w.Reset()
		c := Legacy("§l§6ZID §r§7| §aKills: §f12 §gCoins", Bedrock)
		c.Write(&w)
	}
}
