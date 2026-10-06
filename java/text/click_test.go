package text

import (
	"bytes"
	"testing"

	"github.com/ezchr/go-mc/java/wire"
)

func TestClickValid(t *testing.T) {
	for _, tc := range []struct {
		action, value string
		ok            bool
	}{
		{ClickRunCommand, "/spawn", true},
		{ClickRunCommand, "/say §cred", false},
		{ClickRunCommand, "/say a\nb", false},
		{ClickRunCommand, "/say \x7f", false},
		{ClickSuggestCommand, "/msg Steve \U0001F600", true},
		{ClickSuggestCommand, "/msg \x00", false},
		{ClickOpenURL, "https://example.com/a?b=c#d", true},
		{ClickOpenURL, "HTTP://example.com", true},
		{ClickOpenURL, "http://[::1]:8080/", true},
		{ClickOpenURL, "https://example.com/ü", true},
		{ClickOpenURL, "https://example.com/%zz", false},
		{ClickOpenURL, "https://example.com/a b", false},
		{ClickOpenURL, "https://example.com/{x}", false},
		{ClickOpenURL, "https://example.com/a[0]", false},
		{ClickOpenURL, "https://a#b#c", false},
		{ClickOpenURL, "file:///etc/passwd", false},
		{ClickOpenURL, "javascript:alert(1)", false},
		{ClickOpenURL, "example.com", false},
		{ClickOpenURL, "http:", false},
		{ClickOpenURL, "", false},
		{ClickChangePage, "1", true},
		{ClickChangePage, "2147483647", true},
		{ClickChangePage, "0", false},
		{ClickChangePage, "", false},
		{ClickChangePage, "-3", false},
		{ClickChangePage, "2x", false},
		{ClickChangePage, "2147483648", false},
		{ClickCopy, "anything §at all\n", true},
		{ClickCustom, "myplugin:menu/open", true},
		{ClickCustom, "open", true},
		{ClickCustom, "Bad:Id", false},
		{ClickShowDialog, "minecraft:server_links", false},
		{"open_file", "/tmp/x", false},
		{"nonsense", "x", false},
	} {
		e := &ClickEvent{Action: tc.action, Value: tc.value}
		if got := e.Valid(); got != tc.ok {
			t.Errorf("%s %q: valid %v, want %v", tc.action, tc.value, got, tc.ok)
		}
	}
	var nilEvent *ClickEvent
	if nilEvent.Valid() {
		t.Error("nil click event valid")
	}
}

// TestInvalidClickDropped: a component with an invalid click event is written without it (the
// same bytes as the component without a click), never with a value the client rejects.
func TestInvalidClickDropped(t *testing.T) {
	plain := Component{Text: "hi", Color: "red"}
	bad := plain
	bad.Click = &ClickEvent{Action: ClickChangePage, Value: "0"}
	if !bytes.Equal(plain.Bytes(), bad.Bytes()) {
		t.Fatalf("invalid click written:\n% x\n% x", bad.Bytes(), plain.Bytes())
	}
	// Only text and an invalid click: a bare string, like plain text.
	onlyText := Component{Text: "hi", Click: &ClickEvent{Action: ClickOpenURL, Value: "ftp://x"}}
	if p := Plain("hi"); !bytes.Equal(onlyText.Bytes(), p.Bytes()) {
		t.Fatal("text with an invalid click is not a plain string")
	}
	good := plain
	good.Click = &ClickEvent{Action: ClickChangePage, Value: "12"}
	b := good.Bytes()
	if !bytes.Contains(b, []byte("change_page")) || !bytes.Contains(b, []byte{TagInt, 0, 4, 'p', 'a', 'g', 'e', 0, 0, 0, 12}) {
		t.Fatalf("change_page 12 not written as page 12: % x", b)
	}
	custom := plain
	custom.Click = &ClickEvent{Action: ClickCustom, Value: "df:x"}
	if b := custom.Bytes(); !bytes.Contains(b, []byte{TagString, 0, 2, 'i', 'd', 0, 4, 'd', 'f', ':', 'x'}) {
		t.Fatalf("custom click id not written: % x", b)
	}
}

func TestDecodeModifiedUTF8(t *testing.T) {
	for _, s := range []string{"", "plain", "é€", "a\U0001F600b", "nul\x00here", "\U0010FFFF"} {
		var w wire.Writer
		String(&w, s)
		r := wire.NewReader(w.B)
		if got := ReadString(r); got != s || r.Err != nil || r.Len() != 0 {
			t.Errorf("%q: got %q err %v", s, got, r.Err)
		}
	}
	// A lone surrogate becomes U+FFFD; a raw NUL is accepted like DataInput.readUTF.
	if got, err := DecodeModifiedUTF8([]byte{0xed, 0xa0, 0xbd, 'x', 0}); err != nil || got != "\uFFFDx\x00" {
		t.Errorf("lone surrogate: %q %v", got, err)
	}
	for _, bad := range [][]byte{{0x80}, {0xc3}, {0xe2, 0x82}, {0xf0, 0x9f, 0x98, 0x80}} {
		if _, err := DecodeModifiedUTF8(bad); err == nil {
			t.Errorf("% x accepted", bad)
		}
	}
	r := wire.NewReader([]byte{0, 5, 'a'})
	if ReadString(r); r.Err == nil {
		t.Error("short string accepted")
	}
}
