// Package textcolour holds the colours of Minecraft's § colour codes as a Bedrock client draws them
// and expands colour gradients written in text, for either edition:
//
//	<gradient:#FF2020:#FFD000>THE SUNSET</gradient>
//	<gradient:&c:&6:&g>THE SUNSET</gradient> &7- the rest
//
// A gradient has two or more stops, each a hex colour (#RRGGBB) or a colour code (c, &c or §c), and
// colours every letter between its tags with a colour blended between the stops. The closing tag is
// optional: without it the gradient runs to the end of the text. After it, the colour (and the
// bold, italic and obfuscated codes) in effect before the gradient apply again.
//
// A Java client is shown every letter in its exact colour (as an §#RRGGBB code, which the Java text
// converter reads); a Bedrock client, which only has its colour codes, gets the code that looks
// closest to each letter's colour.
package textcolour

import (
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// RGB is a colour.
type RGB struct{ R, G, B uint8 }

// Hex is the colour as #RRGGBB.
func (c RGB) Hex() string {
	const digits = "0123456789ABCDEF"
	b := []byte{'#', 0, 0, 0, 0, 0, 0}
	for i, v := range [3]uint8{c.R, c.G, c.B} {
		b[1+2*i], b[2+2*i] = digits[v>>4], digits[v&15]
	}
	return string(b)
}

// Bedrock is the colour a Bedrock client (1.26) draws for each colour code. §0-§6 and §f are the
// same as Java's; §7-§9 and §a-§e are drawn in other shades on Bedrock, and §g-§w do not exist on
// Java (on Bedrock §m and §n are colours, not strikethrough and underline). The material colours are
// the $material_*_color values of the vanilla resource pack's ui/_global_variables.json; the rest
// are Bedrock's formatting table.
var Bedrock = map[byte]RGB{
	'0': {0x00, 0x00, 0x00}, // black
	'1': {0x00, 0x00, 0xAA}, // dark_blue
	'2': {0x00, 0xAA, 0x00}, // dark_green
	'3': {0x00, 0xAA, 0xAA}, // dark_aqua
	'4': {0xAA, 0x00, 0x00}, // dark_red
	'5': {0xAA, 0x00, 0xAA}, // dark_purple
	'6': {0xFF, 0xAA, 0x00}, // gold (drawn orange)
	'7': {0xC5, 0xC5, 0xC5}, // gray
	'8': {0x54, 0x54, 0x54}, // dark_gray
	'9': {0x44, 0x7F, 0xFF}, // blue
	'a': {0x54, 0xFF, 0x54}, // green
	'b': {0x54, 0xFF, 0xFF}, // aqua
	'c': {0xFF, 0x54, 0x54}, // red
	'd': {0xFF, 0x54, 0xFF}, // light_purple
	'e': {0xFF, 0xFF, 0x54}, // yellow
	'f': {0xFF, 0xFF, 0xFF}, // white
	'g': {0xEF, 0xCE, 0x16}, // minecoin_gold
	'h': {0xD9, 0xCC, 0xB8}, // material_quartz
	'i': {0xA9, 0xB4, 0xB7}, // material_iron
	'j': {0x8F, 0x72, 0x7D}, // material_netherite
	'm': {0xEE, 0x22, 0x2C}, // material_redstone
	'n': {0xC8, 0x73, 0x63}, // material_copper
	'p': {0xFF, 0xBF, 0x1E}, // material_gold
	'q': {0x13, 0xA0, 0x45}, // material_emerald
	's': {0x5F, 0xEC, 0xFF}, // material_diamond
	't': {0x57, 0x7B, 0xFF}, // material_lapis
	'u': {0xB6, 0x6C, 0xDD}, // material_amethyst
	'v': {0xFF, 0x6A, 0x00}, // material_resin
	'w': {0x8B, 0xB3, 0xFF}, // party_blue
}

// codes are the keys of Bedrock in a fixed order, so Nearest picks the same code on a tie.
var codes = func() []byte {
	out := make([]byte, 0, len(Bedrock))
	for _, c := range "0123456789abcdefghijmnpqstuvw" {
		out = append(out, byte(c))
	}
	return out
}()

// lab is a colour in OKLab, where equal distances look equally different.
type lab [3]float64

func toLinear(v uint8) float64 {
	c := float64(v) / 255
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

func fromLinear(c float64) uint8 {
	c = max(0, min(1, c))
	if c <= 0.0031308 {
		c *= 12.92
	} else {
		c = 1.055*math.Pow(c, 1/2.4) - 0.055
	}
	return uint8(math.Round(c * 255))
}

func toLab(c RGB) lab {
	r, g, b := toLinear(c.R), toLinear(c.G), toLinear(c.B)
	l := math.Cbrt(0.4122214708*r + 0.5363325363*g + 0.0514459929*b)
	m := math.Cbrt(0.2119034982*r + 0.6806995451*g + 0.1073969566*b)
	s := math.Cbrt(0.0883024619*r + 0.2817188376*g + 0.6299787005*b)
	return lab{
		0.2104542553*l + 0.7936177850*m - 0.0040720468*s,
		1.9779984951*l - 2.4285922050*m + 0.4505937099*s,
		0.0259040371*l + 0.7827717662*m - 0.8086757660*s,
	}
}

func (c lab) rgb() RGB {
	l := c[0] + 0.3963377774*c[1] + 0.2158037573*c[2]
	m := c[0] - 0.1055613458*c[1] - 0.0638541728*c[2]
	s := c[0] - 0.0894841775*c[1] - 1.2914855480*c[2]
	l, m, s = l*l*l, m*m*m, s*s*s
	return RGB{
		fromLinear(4.0767416621*l - 3.3077115913*m + 0.2309699292*s),
		fromLinear(-1.2684380046*l + 2.6097574011*m - 0.3413193965*s),
		fromLinear(-0.0041960863*l - 0.7034186147*m + 1.7076147010*s),
	}
}

var bedrockLab = func() map[byte]lab {
	out := make(map[byte]lab, len(Bedrock))
	for c, rgb := range Bedrock {
		out[c] = toLab(rgb)
	}
	return out
}()

// Nearest is the colour code whose Bedrock colour looks closest to c.
func Nearest(c RGB) byte {
	return nearestLab(toLab(c))
}

func nearestLab(want lab) byte {
	best, bestD := codes[0], math.Inf(1)
	for _, code := range codes {
		p := bedrockLab[code]
		d := (p[0]-want[0])*(p[0]-want[0]) + (p[1]-want[1])*(p[1]-want[1]) + (p[2]-want[2])*(p[2]-want[2])
		if d < bestD {
			best, bestD = code, d
		}
	}
	return best
}

// ParseStop reads a gradient stop: #RRGGBB, or a colour code with or without & or §.
func ParseStop(s string) (RGB, bool) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "#") {
		v, err := strconv.ParseUint(s[1:], 16, 32)
		if err != nil || len(s) != 7 {
			return RGB{}, false
		}
		return RGB{uint8(v >> 16), uint8(v >> 8), uint8(v)}, true
	}
	s = strings.TrimPrefix(strings.TrimPrefix(s, "&"), "§")
	if len(s) != 1 {
		return RGB{}, false
	}
	c, ok := Bedrock[strings.ToLower(s)[0]]
	return c, ok
}

const (
	openTag  = "<gradient:"
	closeTag = "</gradient>"
	section  = "§"
)

// Has reports whether s has a gradient to expand.
func Has(s string) bool { return strings.Contains(s, openTag) }

// Expand replaces the gradients in s with colour codes: exact §#RRGGBB codes for Java (exact true),
// the nearest Bedrock colour codes otherwise. A tag that does not parse is left as it is.
func Expand(s string, exact bool) string {
	if !Has(s) {
		return s
	}
	var b strings.Builder
	var colour, styles string // the colour and the style codes in effect, to restore after a gradient
	for len(s) > 0 {
		i := strings.Index(s, openTag)
		if i < 0 {
			b.WriteString(s)
			break
		}
		colour, styles = track(s[:i], colour, styles)
		b.WriteString(s[:i])
		s = s[i:]
		end := strings.IndexByte(s, '>')
		stops, ok := parseStops(s[len(openTag):max(end, len(openTag))])
		if end < 0 || !ok {
			b.WriteString(openTag)
			s = s[len(openTag):]
			continue
		}
		s = s[end+1:]
		body, rest, closed := s, "", false
		if j := strings.Index(s, closeTag); j >= 0 {
			body, rest, closed = s[:j], s[j+len(closeTag):], true
		}
		writeGradient(&b, body, stops, exact)
		_, styles = track(body, colour, styles) // style codes inside the gradient stay on
		if closed && rest != "" {
			if colour != "" {
				b.WriteString(colour)
			} else {
				b.WriteString(section + "r" + styles)
			}
		}
		s = rest
	}
	return b.String()
}

// parseStops reads the stops of a tag, between "<gradient:" and ">".
func parseStops(s string) ([]lab, bool) {
	parts := strings.Split(s, ":")
	if len(parts) < 2 {
		return nil, false
	}
	stops := make([]lab, len(parts))
	for i, p := range parts {
		c, ok := ParseStop(p)
		if !ok {
			return nil, false
		}
		stops[i] = toLab(c)
	}
	return stops, true
}

// track follows the § codes in s: the last colour code and the style codes since the last reset.
func track(s, colour, styles string) (string, string) {
	for {
		i := strings.Index(s, section)
		if i < 0 {
			return colour, styles
		}
		s = s[i+len(section):]
		if s == "" {
			return colour, styles
		}
		if s[0] == '#' && len(s) >= 7 {
			colour, s = section+s[:7], s[7:]
			continue
		}
		r, size := utf8.DecodeRuneInString(s)
		s = s[size:]
		c := unicode.ToLower(r)
		if c >= 128 {
			continue
		}
		switch _, isColour := Bedrock[byte(c)]; {
		case c == 'r':
			colour, styles = "", ""
		case c == 'k' || c == 'l' || c == 'o':
			styles += section + string(c)
		case isColour:
			colour = section + string(c)
		}
	}
}

// writeGradient writes body with each letter coloured along the stops. § codes in body (bold, for
// example) are kept; whitespace takes no colour and does not count as a letter.
func writeGradient(b *strings.Builder, body string, stops []lab, exact bool) {
	letters := 0
	for s := body; s != ""; {
		if strings.HasPrefix(s, section) {
			s = skipCode(s)
			continue
		}
		r, size := utf8.DecodeRuneInString(s)
		s = s[size:]
		if !unicode.IsSpace(r) {
			letters++
		}
	}
	last, i := "", 0
	for s := body; s != ""; {
		if strings.HasPrefix(s, section) {
			rest := skipCode(s)
			b.WriteString(s[:len(s)-len(rest)])
			s = rest
			continue
		}
		r, size := utf8.DecodeRuneInString(s)
		s = s[size:]
		if unicode.IsSpace(r) {
			b.WriteRune(r)
			if r == '\n' {
				last = "" // a Bedrock name tag starts each line without a colour
			}
			continue
		}
		code := colourAt(stops, i, letters, exact)
		if code != last {
			b.WriteString(code)
			last = code
		}
		b.WriteRune(r)
		i++
	}
}

// skipCode returns s after the § code it starts with.
func skipCode(s string) string {
	s = s[len(section):]
	if s == "" {
		return s
	}
	_, size := utf8.DecodeRuneInString(s)
	return s[size:]
}

// colourAt is the colour code of letter i of n.
func colourAt(stops []lab, i, n int, exact bool) string {
	t := 0.0
	if n > 1 {
		t = float64(i) / float64(n-1) * float64(len(stops)-1)
	}
	k := min(int(t), len(stops)-2)
	f := t - float64(k)
	var c lab
	for j := range c {
		c[j] = stops[k][j] + (stops[k+1][j]-stops[k][j])*f
	}
	if exact {
		return section + c.rgb().Hex()
	}
	return section + string(nearestLab(c))
}
