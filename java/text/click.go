package text

import (
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ClickShowDialog and ClickCustom are the other click actions a 26.3 server may send. show_dialog
// needs a dialog (by registry id or inline), which a ClickEvent's Value cannot carry, so it is
// never written; custom is written with Value as its id.
const (
	ClickShowDialog = "show_dialog"
	ClickCustom     = "custom"
)

// Valid reports whether the 26.3 client accepts e. The client decodes a whole packet with one
// codec, so a single invalid click event would make it disconnect: Write leaves invalid click
// events out (the text is still shown, it just does nothing when clicked).
//
// The checks are the codecs of net.minecraft.network.chat.ClickEvent: run_command and
// suggest_command take a CHAT_STRING (no section sign, control character or DEL), open_url an
// UNTRUSTED_URI (a java.net.URI with the http or https scheme), change_page a POSITIVE_INT,
// custom an Identifier, copy_to_clipboard any string. open_file is client-only and show_dialog is
// not supported.
func (e *ClickEvent) Valid() bool {
	if e == nil {
		return false
	}
	switch e.Action {
	case ClickRunCommand, ClickSuggestCommand:
		return chatString(e.Value)
	case ClickOpenURL:
		return untrustedURI(e.Value)
	case ClickChangePage:
		n, err := strconv.ParseInt(e.Value, 10, 32)
		return err == nil && n >= 1
	case ClickCopy:
		return utf8.ValidString(e.Value)
	case ClickCustom:
		return validIdentifier(e.Value)
	}
	return false
}

// chatString is vanilla's ExtraCodecs.CHAT_STRING: every character passes
// StringUtil.isAllowedChatCharacter.
func chatString(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r == '§' || r < ' ' || r == 0x7f {
			return false
		}
	}
	return true
}

// untrustedURI is vanilla's Util.parseAndValidateUntrustedUri: java.net.URI must parse it and the
// scheme must be http or https. java.net.URI is stricter than net/url: it rejects spaces, control
// characters, the ASCII characters RFC 2396 leaves out (" < > \ ^ ` { | }), a % not followed by
// two hex digits, and brackets outside an IPv6 host.
func untrustedURI(s string) bool {
	if !utf8.ValidString(s) || s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c < 0x80 && (c <= ' ' || c == 0x7f || strings.IndexByte("\"<>\\^`{|}", c) >= 0):
			return false
		case c == '%':
			if i+2 >= len(s) || !isHex(s[i+1]) || !isHex(s[i+2]) {
				return false
			}
		}
	}
	for _, r := range s {
		// Non-ASCII is allowed ("other" characters) unless it is a control or space character.
		if r >= 0x80 && (unicode.IsControl(r) || unicode.IsSpace(r) || unicode.Is(unicode.Zs, r)) {
			return false
		}
	}
	if strings.Count(s, "#") > 1 {
		return false
	}
	if i := strings.IndexByte(s, ':'); i < 0 || i == len(s)-1 {
		return false // java.net.URI wants a scheme-specific part
	}
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return false
	}
	if n := strings.Count(s, "[") + strings.Count(s, "]"); n > 0 {
		if !strings.HasPrefix(u.Host, "[") || strings.Count(u.Host, "[")+strings.Count(u.Host, "]") != n {
			return false
		}
	}
	return true
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// validIdentifier is vanilla's Identifier rule: an optional namespace of [a-z0-9_.-] and a colon,
// then a path of [a-z0-9_.-/].
func validIdentifier(s string) bool {
	ns, path, ok := strings.Cut(s, ":")
	if !ok {
		ns, path = "minecraft", s
	}
	for i := 0; i < len(ns); i++ {
		if c := ns[i]; !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '.' || c == '-') {
			return false
		}
	}
	for i := 0; i < len(path); i++ {
		if c := path[i]; !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '.' || c == '-' || c == '/') {
			return false
		}
	}
	return path != ""
}
