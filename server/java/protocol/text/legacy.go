package text

import (
	"strings"
	"unicode/utf8"
)

// Palette says how section-sign (§) codes in a legacy string are read.
type Palette uint8

const (
	// Java: §0-§f colours, §k obfuscated, §l bold, §m strikethrough, §n underlined, §o italic,
	// §r reset. A colour code resets the formatting codes before it.
	Java Palette = iota
	// Bedrock: §0-§f, the Bedrock-only colours §g (minecoin gold) and §h-§v (material colours, so
	// §m and §n are colours, not strikethrough/underline), §k §l §o §r. Colour codes do not reset
	// bold/italic/obfuscated on Bedrock, so they don't here either.
	Bedrock
)

// legacyNames are the Java colour names of §0-§f.
var legacyNames = [16]string{"black", "dark_blue", "dark_green", "dark_aqua", "dark_red",
	"dark_purple", "gold", "gray", "dark_gray", "blue", "green", "aqua", "red", "light_purple",
	"yellow", "white"}

// bedrockColours are the Bedrock-only colour codes and their RGB (Bedrock's text formatting table).
var bedrockColours = [128]string{
	'g': "#DDD605", // minecoin_gold
	'h': "#E3D4D1", // material_quartz
	'i': "#CECACA", // material_iron
	'j': "#443A3B", // material_netherite
	'm': "#971607", // material_redstone
	'n': "#B4684D", // material_copper
	'p': "#DEB12D", // material_gold
	'q': "#47A036", // material_emerald
	's': "#2CBAA8", // material_diamond
	't': "#21497B", // material_lapis
	'u': "#9A5CC6", // material_amethyst
	'v': "#EB7114", // material_resin
}

// ColourName returns the Java colour for a legacy colour code character in palette p ("" if c is
// not a colour code).
func ColourName(c byte, p Palette) string {
	if c >= 'A' && c <= 'Z' {
		c += 'a' - 'A'
	}
	switch {
	case c >= '0' && c <= '9':
		return legacyNames[c-'0']
	case c >= 'a' && c <= 'f':
		return legacyNames[c-'a'+10]
	}
	if p == Bedrock && c < 128 {
		return bedrockColours[c]
	}
	return ""
}

type legacyStyle struct {
	colour                           string
	bold, italic, under, strike, obf bool
}

func (s legacyStyle) apply(c *Component) {
	c.Color = s.colour
	if s.bold {
		c.Bold = On
	}
	if s.italic {
		c.Italic = On
	}
	if s.under {
		c.Underlined = On
	}
	if s.strike {
		c.Strikethrough = On
	}
	if s.obf {
		c.Obfuscated = On
	}
}

// Legacy converts a string with § codes to a component. A string without codes becomes a plain
// text component; otherwise the result is an unstyled root whose text is the part before the first
// code, with one child per styled run. Unknown codes are dropped (both characters), as both
// editions do; a trailing lone § is dropped too.
func Legacy(s string, p Palette) Component {
	i := strings.Index(s, section)
	if i < 0 {
		return Component{Text: s}
	}
	root := Component{Text: s[:i], Extra: make([]Component, 0, min(strings.Count(s[i:], section), 16))}
	var st legacyStyle
	// run is the text since the last style change: a substring of s unless an ignored code split it.
	var run string
	add := func(t string) {
		if run == "" {
			run = t
		} else if t != "" {
			run += t
		}
	}
	flush := func() {
		if run == "" {
			return
		}
		c := Component{Text: run}
		st.apply(&c)
		// Merge with the previous child when the style is the same (e.g. "§a§r§aX").
		if n := len(root.Extra); n > 0 && sameStyle(&root.Extra[n-1], &c) {
			root.Extra[n-1].Text += c.Text
		} else {
			root.Extra = append(root.Extra, c)
		}
		run = ""
	}
	rest := s[i:]
	for len(rest) > 0 {
		j := strings.Index(rest, section)
		if j < 0 {
			add(rest)
			break
		}
		add(rest[:j])
		rest = rest[j+2:]
		if len(rest) == 0 {
			break
		}
		code, size := utf8.DecodeRuneInString(rest)
		rest = rest[size:]
		if code >= 0x80 {
			continue
		}
		c := byte(code)
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		next := st
		switch c {
		case 'r':
			next = legacyStyle{}
		case 'k':
			next.obf = true
		case 'l':
			next.bold = true
		case 'o':
			next.italic = true
		case 'm', 'n':
			if p == Java {
				if c == 'm' {
					next.strike = true
				} else {
					next.under = true
				}
				break
			}
			fallthrough
		default:
			col := ColourName(c, p)
			if col == "" {
				continue
			}
			if p == Java {
				next = legacyStyle{colour: col}
			} else {
				next.colour = col
			}
		}
		if next != st {
			flush()
			st = next
		}
	}
	flush()
	if len(root.Extra) == 1 && root.Text == "" {
		return root.Extra[0]
	}
	return root
}

func sameStyle(a, b *Component) bool {
	return a.Color == b.Color && a.Bold == b.Bold && a.Italic == b.Italic && a.Underlined == b.Underlined &&
		a.Strikethrough == b.Strikethrough && a.Obfuscated == b.Obfuscated
}

// Strip removes § codes from s (both characters of each), as the client would not show them.
func Strip(s string) string {
	if !strings.Contains(s, section) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for {
		j := strings.Index(s, section)
		if j < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:j])
		s = s[j+2:]
		if len(s) == 0 {
			return b.String()
		}
		_, size := utf8.DecodeRuneInString(s)
		s = s[size:]
	}
}

// section is the section sign that starts a formatting code.
const section = "\u00a7"
