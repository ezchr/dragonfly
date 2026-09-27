package cmd

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// selector holds the arguments of a target selector, the part between the
// brackets of @e[type=zid:gubby,r=10,c=1]. Supported arguments are type, name,
// c, r, rm, x, y, z, dx, dy and dz, as in vanilla. type and name may be
// negated with '!' and repeated.
type selector struct {
	origin   mgl64.Vec3
	types    []selectorMatch
	names    []selectorMatch
	r, rm    float64
	hasR     bool
	hasRM    bool
	volume   mgl64.Vec3
	hasVol   bool
	count    int
	hasCount bool
}

type selectorMatch struct {
	value  string
	negate bool
}

// joinSelector puts a selector the command line split on its spaces, such as
// "@e[type=gubby," "c=1]", back together as the next argument of line.
func joinSelector(line *Line) string {
	joined := line.args[0]
	if !strings.Contains(joined, "[") {
		return joined
	}
	n := 1
	for !strings.Contains(joined, "]") && n < len(line.args) {
		joined += " " + line.args[n]
		n++
	}
	line.args = append([]string{joined}, line.args[n:]...)
	return joined
}

// parseSelector splits s into its kind ("@e") and arguments. The selector
// is centred on origin, the source's position, which "~" and "~n" are
// relative to.
func parseSelector(s string, origin mgl64.Vec3) (string, selector, error) {
	sel := selector{origin: origin}
	kind, body := s, ""
	if i := strings.IndexByte(s, '['); i >= 0 {
		if !strings.HasSuffix(s, "]") {
			return "", sel, fmt.Errorf("missing ']'")
		}
		kind, body = s[:i], s[i+1:len(s)-1]
	}
	for _, part := range strings.Split(body, ",") {
		if part = strings.TrimSpace(part); part == "" {
			continue
		}
		key, val, ok := strings.Cut(part, "=")
		if !ok {
			return "", sel, fmt.Errorf("%q is not key=value", part)
		}
		key, val = strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(val)
		var err error
		switch key {
		case "type", "name":
			m := selectorMatch{value: strings.Trim(strings.TrimPrefix(val, "!"), `"`), negate: strings.HasPrefix(val, "!")}
			if key == "type" {
				sel.types = append(sel.types, m)
			} else {
				sel.names = append(sel.names, m)
			}
		case "c":
			sel.count, err = strconv.Atoi(val)
			sel.hasCount = true
		case "r":
			sel.r, err = strconv.ParseFloat(val, 64)
			sel.hasR = true
		case "rm":
			sel.rm, err = strconv.ParseFloat(val, 64)
			sel.hasRM = true
		case "x", "y", "z":
			i := int(key[0] - 'x')
			sel.origin[i], err = selectorCoord(val, sel.origin[i])
		case "dx", "dy", "dz":
			i := int(key[1] - 'x')
			sel.volume[i], err = strconv.ParseFloat(val, 64)
			sel.hasVol = true
		default:
			return "", sel, fmt.Errorf("unsupported selector argument %q", key)
		}
		if err != nil {
			return "", sel, fmt.Errorf("bad value for %s: %q", key, val)
		}
	}
	return strings.ToLower(kind), sel, nil
}

// selectorCoord parses a selector coordinate: a number, "~" or "~n".
func selectorCoord(val string, src float64) (float64, error) {
	if rel, ok := strings.CutPrefix(val, "~"); ok {
		if rel == "" {
			return src, nil
		}
		f, err := strconv.ParseFloat(rel, 64)
		return src + f, err
	}
	return strconv.ParseFloat(val, 64)
}

// matches reports whether t passes every filter of the selector.
func (sel selector) matches(t Target) bool {
	pos := t.Position()
	dist := pos.Sub(sel.origin).Len()
	if (sel.hasR && dist > sel.r) || (sel.hasRM && dist < sel.rm) {
		return false
	}
	if sel.hasVol {
		for i := 0; i < 3; i++ {
			lo, hi := min(sel.origin[i], sel.origin[i]+sel.volume[i]), max(sel.origin[i], sel.origin[i]+sel.volume[i])+1
			if pos[i] < lo || pos[i] > hi {
				return false
			}
		}
	}
	id := targetType(t)
	for _, m := range sel.types {
		if typeMatches(id, m.value) == m.negate {
			return false
		}
	}
	name := targetName(t)
	for _, m := range sel.names {
		if strings.EqualFold(name, m.value) == m.negate {
			return false
		}
	}
	return true
}

// targetType is the entity identifier of t, such as "minecraft:player".
func targetType(t Target) string {
	if e, ok := t.(world.Entity); ok {
		return strings.ToLower(e.H().Type().EncodeEntity())
	}
	return ""
}

// typeMatches reports whether identifier id is the type asked for. The
// namespace may be left out: "gubby" matches "zid:gubby" and "item" matches
// "minecraft:item".
func typeMatches(id, want string) bool {
	want = strings.ToLower(want)
	if id == want {
		return true
	}
	if strings.Contains(want, ":") {
		return false
	}
	_, name, _ := strings.Cut(id, ":")
	return name == want
}

// targetName is the name of a player, or the name tag of another entity.
func targetName(t Target) string {
	if n, ok := t.(NamedTarget); ok {
		return n.Name()
	}
	if n, ok := t.(interface{ NameTag() string }); ok {
		return n.NameTag()
	}
	return ""
}

// selectTargets narrows pool down to the targets the selector picks. @p and
// @r pick one target unless c says otherwise; c sorts nearest first, or
// farthest first when negative.
func (sel selector) selectTargets(kind string, pool []Target) []Target {
	out := slices.DeleteFunc(slices.Clone(pool), func(t Target) bool { return !sel.matches(t) })
	count := 0
	if kind == "@p" || kind == "@r" {
		count = 1
	}
	if sel.hasCount {
		count = sel.count
	}
	if kind == "@r" {
		rand.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	} else if count != 0 {
		dist := func(t Target) float64 { return t.Position().Sub(sel.origin).LenSqr() }
		slices.SortStableFunc(out, func(a, b Target) int {
			if count < 0 {
				a, b = b, a
			}
			switch da, db := dist(a), dist(b); {
			case da < db:
				return -1
			case da > db:
				return 1
			}
			return 0
		})
	}
	if count < 0 {
		count = -count
	}
	if count > 0 && len(out) > count {
		out = out[:count]
	}
	return out
}
