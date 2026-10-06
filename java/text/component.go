// Package text builds Java Edition text components ("chat components") and writes them in the
// network NBT form that 26.3 (protocol 777) uses in system_chat, titles, scoreboards, boss bars,
// dialogs and every other packet field of type Component.
//
// The NBT is written the way vanilla writes it: a component that is only text with no style and
// no children is a bare string tag, anything else a compound whose keys come out in the order of
// Java's HashMap (vanilla's CompoundTag), and a list that mixes strings and compounds wraps the
// strings as {"": text}. The output is byte-identical to what a vanilla server sends for the same
// component (see component_test.go, checked against captures of a vanilla 26.3 server).
//
// Legacy strings with section-sign codes are converted with Legacy, which knows both the Java
// palette and the Bedrock one (Bedrock-only colours §g and §h-§v, and §m/§n as colours there).
package text

import (
	"github.com/ezchr/go-mc/java/wire"
)

// Flag is a style flag that a component either sets, clears or inherits from its parent.
type Flag int8

const (
	Inherit Flag = 0
	On      Flag = 1
	Off     Flag = -1
)

// Component is one text component. The zero value is an empty text component.
type Component struct {
	// Text is the literal text. Ignored when Translate is set.
	Text string
	// Translate is a translation key; With holds its arguments and Fallback the text shown when the
	// client does not know the key.
	Translate string
	Fallback  string
	With      []Component

	// Color is "" (inherit), a named colour ("red", "dark_gray", ...) or "#RRGGBB".
	Color                                               string
	Bold, Italic, Underlined, Strikethrough, Obfuscated Flag
	// Insertion is text inserted into the chat box when the component is shift-clicked.
	Insertion string
	Click     *ClickEvent
	// Hover is shown as a tooltip (a show_text hover event) when set.
	Hover *Component
	// Font is a font resource location ("minecraft:uniform"), or "" for the default.
	Font string

	Extra []Component
}

// ClickEvent is what happens when a component is clicked.
type ClickEvent struct {
	// Action is one of ClickRunCommand, ClickSuggestCommand, ClickOpenURL, ClickCopy, ClickChangePage.
	Action string
	// Value is the command (with its leading slash), URL, text to copy or page number.
	Value string
}

// Click event actions.
const (
	ClickRunCommand     = "run_command"
	ClickSuggestCommand = "suggest_command"
	ClickOpenURL        = "open_url"
	ClickCopy           = "copy_to_clipboard"
	ClickChangePage     = "change_page"
)

// Plain returns a component of plain text.
func Plain(s string) Component { return Component{Text: s} }

// Translatable returns a translation component.
func Translatable(key string, with ...Component) Component {
	return Component{Translate: key, With: with}
}

// isString reports whether c is written as a bare string tag (vanilla's tryCollapseToString).
func (c *Component) isString() bool {
	return c.Translate == "" && c.Color == "" && c.Bold == 0 && c.Italic == 0 && c.Underlined == 0 &&
		c.Strikethrough == 0 && c.Obfuscated == 0 && c.Insertion == "" && c.Click == nil &&
		c.Hover == nil && c.Font == "" && len(c.Extra) == 0
}

// IsPlain reports whether c is a plain string with no style and no children.
func (c *Component) IsPlain() bool { return c.isString() }

// Write writes c as nameless network NBT (a tag type byte followed by the payload), the form
// every Component field of a packet takes.
func (c *Component) Write(w *wire.Writer) {
	if c.isString() {
		w.Byte(TagString)
		String(w, c.Text)
		return
	}
	w.Byte(TagCompound)
	c.writeCompound(w)
}

// WriteString writes a plain text component; it is Plain(s).Write(w) without building one.
func WriteString(w *wire.Writer, s string) {
	w.Byte(TagString)
	String(w, s)
}

// Bytes returns c as network NBT.
func (c *Component) Bytes() []byte {
	var w wire.Writer
	c.Write(&w)
	return w.B
}

// Component compound keys in the order vanilla's codec puts them into the CompoundTag.
const (
	kText = iota
	kTranslate
	kFallback
	kWith
	kExtra
	kColor
	kBold
	kItalic
	kUnderlined
	kStrikethrough
	kObfuscated
	kClickEvent
	kHoverEvent
	kInsertion
	kFont
	numKeys
)

var keyNames = [numKeys]string{"text", "translate", "fallback", "with", "extra", "color", "bold",
	"italic", "underlined", "strikethrough", "obfuscated", "click_event", "hover_event", "insertion", "font"}

// keyBucket[k] is the bucket of key k in a 16- and a 32-bucket Java HashMap.
var keyBucket [2][numKeys]uint8

func init() {
	for k, n := range keyNames {
		keyBucket[0][k] = uint8(javaBucket(n, 16))
		keyBucket[1][k] = uint8(javaBucket(n, 32))
	}
}

func (c *Component) writeCompound(w *wire.Writer) {
	var keys [numKeys]uint8
	n := 0
	add := func(k int, ok bool) {
		if ok {
			keys[n] = uint8(k)
			n++
		}
	}
	if c.Translate != "" {
		add(kTranslate, true)
		add(kFallback, c.Fallback != "")
		add(kWith, len(c.With) > 0)
	} else {
		add(kText, true)
	}
	add(kExtra, len(c.Extra) > 0)
	add(kColor, c.Color != "")
	add(kBold, c.Bold != 0)
	add(kItalic, c.Italic != 0)
	add(kUnderlined, c.Underlined != 0)
	add(kStrikethrough, c.Strikethrough != 0)
	add(kObfuscated, c.Obfuscated != 0)
	add(kClickEvent, c.Click != nil)
	add(kHoverEvent, c.Hover != nil)
	add(kInsertion, c.Insertion != "")
	add(kFont, c.Font != "")
	if n <= 12 {
		sortKeys(keys[:n], &keyBucket[0])
	} else { // the HashMap grew to 32 buckets
		sortKeys(keys[:n], &keyBucket[1])
	}

	for _, k := range keys[:n] {
		switch k {
		case kText:
			StringTag(w, "text", c.Text)
		case kTranslate:
			StringTag(w, "translate", c.Translate)
		case kFallback:
			StringTag(w, "fallback", c.Fallback)
		case kWith:
			writeList(w, "with", c.With)
		case kExtra:
			writeList(w, "extra", c.Extra)
		case kColor:
			StringTag(w, "color", c.Color)
		case kBold:
			ByteTag(w, "bold", flagByte(c.Bold))
		case kItalic:
			ByteTag(w, "italic", flagByte(c.Italic))
		case kUnderlined:
			ByteTag(w, "underlined", flagByte(c.Underlined))
		case kStrikethrough:
			ByteTag(w, "strikethrough", flagByte(c.Strikethrough))
		case kObfuscated:
			ByteTag(w, "obfuscated", flagByte(c.Obfuscated))
		case kClickEvent:
			Key(w, TagCompound, "click_event")
			c.Click.write(w)
		case kHoverEvent:
			// {action: "show_text", value: component}; "action" iterates before "value" (tested).
			Key(w, TagCompound, "hover_event")
			StringTag(w, "action", "show_text")
			if c.Hover.isString() {
				Key(w, TagString, "value")
				String(w, c.Hover.Text)
			} else {
				Key(w, TagCompound, "value")
				c.Hover.writeCompound(w)
			}
			w.Byte(TagEnd)
		case kInsertion:
			StringTag(w, "insertion", c.Insertion)
		case kFont:
			StringTag(w, "font", c.Font)
		}
	}
	w.Byte(TagEnd)
}

func flagByte(f Flag) byte {
	if f > 0 {
		return 1
	}
	return 0
}

// write writes the payload of a click_event compound: "action" and one value field, in HashMap
// order (the action is inserted first).
func (e *ClickEvent) write(w *wire.Writer) {
	field := "value"
	switch e.Action {
	case ClickRunCommand, ClickSuggestCommand:
		field = "command"
	case ClickOpenURL:
		field = "url"
	case ClickChangePage:
		field = "page"
	}
	valueFirst := javaBucket(field, 16) < javaBucket("action", 16)
	if !valueFirst {
		StringTag(w, "action", e.Action)
	}
	if field == "page" {
		n := int32(0)
		for _, ch := range e.Value {
			if ch < '0' || ch > '9' {
				break
			}
			n = n*10 + int32(ch-'0')
		}
		IntTag(w, field, n)
	} else {
		StringTag(w, field, e.Value)
	}
	if valueFirst {
		StringTag(w, "action", e.Action)
	}
	w.Byte(TagEnd)
}

// writeList writes a named list of components: a list of strings when every element is a plain
// string, otherwise a list of compounds with the plain ones wrapped as {"": text}, like vanilla.
func writeList(w *wire.Writer, name string, l []Component) {
	allStrings := true
	for i := range l {
		if !l[i].isString() {
			allStrings = false
			break
		}
	}
	if allStrings {
		Key(w, TagList, name)
		w.Byte(TagString)
		w.Int32(int32(len(l)))
		for i := range l {
			String(w, l[i].Text)
		}
		return
	}
	Key(w, TagList, name)
	w.Byte(TagCompound)
	w.Int32(int32(len(l)))
	for i := range l {
		if l[i].isString() {
			StringTag(w, "", l[i].Text)
			w.Byte(TagEnd)
			continue
		}
		l[i].writeCompound(w)
	}
}
