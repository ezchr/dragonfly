// Package item encodes and decodes Java Edition 26.3 (protocol 777) item stacks, the protocol's "Slot"
// type: a count, an item id and a data component patch (components added to or removed from the
// item's defaults).
//
// Stack models the components servers commonly set (names, lore, enchantments, damage, potions, dyes,
// custom model data, ...) as typed fields; others are kept as raw bytes when their length can be told
// (see RawComponent). Encoding appends to a wire.Writer and does not allocate. Decoding reuses the
// Stack's slices, so a Stack decoded into again and again allocates only for strings.
//
// Stacks always hold 26.3 ids. The *For methods write and read them for an older protocol (Java 26.2,
// protocol 776) with that version's ids; see Proto.
package item

import (
	"errors"
	"fmt"
	"math/bits"
	"sync"

	"github.com/ezchr/go-mcjava/wire"
)

var (
	// ErrUnknownComponent is returned when decoding a (non length-prefixed) component whose wire form
	// this package does not know, so the rest of the packet can't be read.
	ErrUnknownComponent = errors.New("item: component with unknown wire form")
	// ErrInvalid is returned for out-of-range ids, counts and lengths.
	ErrInvalid = errors.New("item: invalid value")
)

// Set is a set of data component types.
type Set [2]uint64

// Add adds t to the set.
func (s *Set) Add(t int32) {
	if uint32(t) < 128 {
		s[t>>6] |= 1 << (t & 63)
	}
}

// Del removes t from the set.
func (s *Set) Del(t int32) {
	if uint32(t) < 128 {
		s[t>>6] &^= 1 << (t & 63)
	}
}

// Has reports whether t is in the set.
func (s Set) Has(t int32) bool { return uint32(t) < 128 && s[t>>6]&(1<<(t&63)) != 0 }

// Len is the number of types in the set.
func (s Set) Len() int { return bits.OnesCount64(s[0]) + bits.OnesCount64(s[1]) }

func (s Set) and(o Set) Set { return Set{s[0] & o[0], s[1] & o[1]} }

// Modeled is the set of components Stack has typed fields for.
var Modeled = func() Set {
	var s Set
	for _, t := range []int32{CompCustomData, CompMaxStackSize, CompMaxDamage, CompDamage, CompUnbreakable,
		CompCustomName, CompItemName, CompItemModel, CompLore, CompEnchantments, CompCustomModelData,
		CompTooltipDisplay, CompRepairCost, CompEnchantmentGlintOverride, CompStoredEnchantments,
		CompDyedColor, CompPotionContents} {
		s.Add(t)
	}
	return s
}()

// Text is a chat component (item names, lore lines).
type Text struct {
	// Text is literal text. It is written as a plain NBT string, which is what vanilla sends for a
	// plain name; legacy § formatting codes in it are rendered by the client.
	Text string
	// NBT, when not nil, is the whole component in network NBT and is written instead of Text. Decoding
	// sets it for anything but a plain string, and sets Text to the component's plain text.
	NBT []byte
}

// Enchantment is one enchantment of an enchantments or stored_enchantments component.
type Enchantment struct {
	ID    int32 // minecraft:enchantment network id (the registry_data order the server sent)
	Level int32
}

// Effect is a mob effect instance (potion_contents custom effects).
type Effect struct {
	ID                               int32 // minecraft:mob_effect id; unused for Hidden
	Amplifier, Duration              int32
	Ambient, ShowParticles, ShowIcon bool
	Hidden                           *Effect
}

// PotionContents is the potion_contents component.
type PotionContents struct {
	HasPotion      bool
	Potion         int32 // minecraft:potion id
	HasCustomColor bool
	CustomColor    int32
	Effects        []Effect
	HasCustomName  bool
	CustomName     string
}

// CustomModelData is the custom_model_data component.
type CustomModelData struct {
	Floats  []float32
	Flags   []bool
	Strings []string
	Colors  []int32
}

// TooltipDisplay is the tooltip_display component.
type TooltipDisplay struct {
	HideTooltip bool
	Hidden      []int32 // component types whose tooltip lines are hidden
}

// RawComponent is an added component Stack has no field for, as its encoded value.
type RawComponent struct {
	Type int32
	Data []byte
}

// Stack is an item stack. The zero Stack is the empty slot.
type Stack struct {
	Count int32 // 0: empty
	ID    int32 // minecraft:item protocol id

	// Added holds the modeled components the patch sets (their values are the fields below); Removed
	// the components it removes from the item's defaults (the "!component" form).
	Added, Removed Set
	// Order, when not empty, is the order the added components (modeled and raw) are written in; it
	// must then list all of them. Empty means ascending type id, then Raw in slice order. Decoding
	// records the order it read when it differs from that.
	Order []int32

	MaxStackSize, MaxDamage, Damage, RepairCost int32
	CustomName, ItemName                        Text
	ItemModel                                   string
	Lore                                        []Text
	Enchantments, StoredEnchantments            []Enchantment
	CustomModelData                             CustomModelData
	TooltipDisplay                              TooltipDisplay
	EnchantmentGlintOverride                    bool
	DyedColor                                   int32 // RGB
	PotionContents                              PotionContents
	CustomData                                  []byte // network NBT compound; empty writes an empty one
	Raw                                         []RawComponent
}

// Empty reports whether the stack is the empty slot.
func (s *Stack) Empty() bool { return s.Count <= 0 }

// Has reports whether the patch adds component t.
func (s *Stack) Has(t int32) bool { return s.Added.Has(t) || s.rawIndex(t) >= 0 }

// Add marks component t as added; set its field first. Unbreakable is a component without a value.
func (s *Stack) Add(t int32) {
	s.Added.Add(t)
	s.Removed.Del(t)
}

// Remove adds the removal of component t (from the item's defaults) to the patch.
func (s *Stack) Remove(t int32) {
	s.Added.Del(t)
	s.Removed.Add(t)
}

// Reset empties the stack, keeping its slices for reuse.
func (s *Stack) Reset() {
	*s = Stack{
		Order:        s.Order[:0],
		Lore:         s.Lore[:0],
		Enchantments: s.Enchantments[:0], StoredEnchantments: s.StoredEnchantments[:0],
		CustomModelData: CustomModelData{s.CustomModelData.Floats[:0], s.CustomModelData.Flags[:0],
			s.CustomModelData.Strings[:0], s.CustomModelData.Colors[:0]},
		TooltipDisplay: TooltipDisplay{Hidden: s.TooltipDisplay.Hidden[:0]},
		PotionContents: PotionContents{Effects: s.PotionContents.Effects[:0]},
		CustomData:     s.CustomData[:0],
		Raw:            s.Raw[:0],
	}
}

// WriteEmpty writes an empty slot.
func WriteEmpty(w *wire.Writer) { w.Byte(0) }

// Encode writes the stack the way servers send it (ItemStack.OPTIONAL_STREAM_CODEC).
func (s *Stack) Encode(w *wire.Writer) { s.encode(w, false, nil) }

// EncodeUntrusted writes the stack in the client-to-server form (set_creative_mode_slot), where each
// component value is prefixed with its length.
func (s *Stack) EncodeUntrusted(w *wire.Writer) { s.encode(w, true, nil) }

// EncodeFor is Encode for the protocol version of p (nil: the newest, the same as Encode).
func (s *Stack) EncodeFor(w *wire.Writer, p *Proto) { s.encode(w, false, p) }

// EncodeUntrustedFor is EncodeUntrusted for the protocol version of p.
func (s *Stack) EncodeUntrustedFor(w *wire.Writer, p *Proto) { s.encode(w, true, p) }

func (s *Stack) encode(w *wire.Writer, delimited bool, p *Proto) {
	if s.Count <= 0 {
		w.Byte(0)
		return
	}
	if p != nil {
		s.encodeOld(w, delimited, p)
		return
	}
	w.VarInt(s.Count)
	w.VarInt(s.ID)
	added := s.Added.and(Modeled)
	if len(s.Order) == 0 {
		w.VarInt(int32(added.Len() + len(s.Raw)))
		w.VarInt(int32(s.Removed.Len()))
		for wi, word := range added {
			for word != 0 {
				t := int32(wi*64 + bits.TrailingZeros64(word))
				word &= word - 1
				s.component(w, t, delimited)
			}
		}
		for i := range s.Raw {
			s.raw(w, &s.Raw[i], delimited)
		}
	} else {
		n := 0
		for _, t := range s.Order {
			if added.Has(t) || s.rawIndex(t) >= 0 {
				n++
			}
		}
		w.VarInt(int32(n))
		w.VarInt(int32(s.Removed.Len()))
		for _, t := range s.Order {
			if added.Has(t) {
				s.component(w, t, delimited)
			} else if i := s.rawIndex(t); i >= 0 {
				s.raw(w, &s.Raw[i], delimited)
			}
		}
	}
	for wi, word := range s.Removed {
		for word != 0 {
			w.VarInt(int32(wi*64 + bits.TrailingZeros64(word)))
			word &= word - 1
		}
	}
}

func (s *Stack) rawIndex(t int32) int {
	for i := range s.Raw {
		if s.Raw[i].Type == t {
			return i
		}
	}
	return -1
}

func (s *Stack) raw(w *wire.Writer, c *RawComponent, delimited bool) {
	w.VarInt(c.Type)
	if delimited {
		w.VarInt(int32(len(c.Data)))
	}
	w.Raw(c.Data)
}

// component writes type t and its value.
func (s *Stack) component(w *wire.Writer, t int32, delimited bool) {
	w.VarInt(t)
	if !delimited {
		s.value(w, t)
		return
	}
	start := len(w.B)
	s.value(w, t)
	lengthPrefix(w, start)
}

// lengthPrefix prefixes what was written since start with its length: it moves it right to make
// room for the length.
func lengthPrefix(w *wire.Writer, start int) {
	n := len(w.B) - start
	size := wire.VarIntSize(int32(n))
	for range size {
		w.B = append(w.B, 0)
	}
	copy(w.B[start+size:], w.B[start:start+n])
	wire.AppendVarInt(w.B[start:start], int32(n))
}

func (s *Stack) value(w *wire.Writer, t int32) {
	switch t {
	case CompCustomData:
		if len(s.CustomData) == 0 {
			w.Byte(nbtCompound)
			w.Byte(nbtEnd)
		} else {
			w.Raw(s.CustomData)
		}
	case CompMaxStackSize:
		w.VarInt(s.MaxStackSize)
	case CompMaxDamage:
		w.VarInt(s.MaxDamage)
	case CompDamage:
		w.VarInt(s.Damage)
	case CompRepairCost:
		w.VarInt(s.RepairCost)
	case CompUnbreakable:
	case CompCustomName:
		writeText(w, &s.CustomName)
	case CompItemName:
		writeText(w, &s.ItemName)
	case CompItemModel:
		w.String(s.ItemModel)
	case CompLore:
		w.VarInt(int32(len(s.Lore)))
		for i := range s.Lore {
			writeText(w, &s.Lore[i])
		}
	case CompEnchantments:
		writeEnchantments(w, s.Enchantments)
	case CompStoredEnchantments:
		writeEnchantments(w, s.StoredEnchantments)
	case CompCustomModelData:
		d := &s.CustomModelData
		w.VarInt(int32(len(d.Floats)))
		for _, f := range d.Floats {
			w.Float32(f)
		}
		w.VarInt(int32(len(d.Flags)))
		for _, f := range d.Flags {
			w.Bool(f)
		}
		w.VarInt(int32(len(d.Strings)))
		for _, str := range d.Strings {
			w.String(str)
		}
		w.VarInt(int32(len(d.Colors)))
		for _, c := range d.Colors {
			w.Int32(c)
		}
	case CompTooltipDisplay:
		w.Bool(s.TooltipDisplay.HideTooltip)
		w.VarInt(int32(len(s.TooltipDisplay.Hidden)))
		for _, h := range s.TooltipDisplay.Hidden {
			w.VarInt(h)
		}
	case CompEnchantmentGlintOverride:
		w.Bool(s.EnchantmentGlintOverride)
	case CompDyedColor:
		w.Int32(s.DyedColor)
	case CompPotionContents:
		p := &s.PotionContents
		w.Bool(p.HasPotion)
		if p.HasPotion {
			w.VarInt(p.Potion)
		}
		w.Bool(p.HasCustomColor)
		if p.HasCustomColor {
			w.Int32(p.CustomColor)
		}
		w.VarInt(int32(len(p.Effects)))
		for i := range p.Effects {
			w.VarInt(p.Effects[i].ID)
			writeEffectDetails(w, &p.Effects[i])
		}
		w.Bool(p.HasCustomName)
		if p.HasCustomName {
			w.String(p.CustomName)
		}
	}
}

func writeEnchantments(w *wire.Writer, e []Enchantment) {
	w.VarInt(int32(len(e)))
	for _, en := range e {
		w.VarInt(en.ID)
		w.VarInt(en.Level)
	}
}

func writeEffectDetails(w *wire.Writer, e *Effect) {
	w.VarInt(e.Amplifier)
	w.VarInt(e.Duration)
	w.Bool(e.Ambient)
	w.Bool(e.ShowParticles)
	w.Bool(e.ShowIcon)
	w.Bool(e.Hidden != nil)
	if e.Hidden != nil {
		writeEffectDetails(w, e.Hidden)
	}
}

func writeText(w *wire.Writer, t *Text) {
	if t.NBT != nil {
		w.Raw(t.NBT)
		return
	}
	w.Byte(nbtString)
	writeMUTF8(w, t.Text)
}

// Decode reads a stack the way servers send it (ItemStack.OPTIONAL_STREAM_CODEC). Errors are left in
// r.Err.
func (s *Stack) Decode(r *wire.Reader) { s.decode(r, false, nil) }

// DecodeUntrusted reads a stack in the client-to-server form (set_creative_mode_slot): every component
// is length-prefixed, so components this package does not model are kept in Raw.
func (s *Stack) DecodeUntrusted(r *wire.Reader) { s.decode(r, true, nil) }

// DecodeFor is Decode for the protocol version of p (nil: the newest, the same as Decode). The stack
// gets newest ids; what the newest version has no place for is left out (see Proto).
func (s *Stack) DecodeFor(r *wire.Reader, p *Proto) { s.decode(r, false, p) }

// DecodeUntrustedFor is DecodeUntrusted for the protocol version of p.
func (s *Stack) DecodeUntrustedFor(r *wire.Reader, p *Proto) { s.decode(r, true, p) }

func (s *Stack) decode(r *wire.Reader, delimited bool, p *Proto) {
	s.Reset()
	count := r.VarInt()
	if r.Err != nil || count <= 0 {
		return
	}
	s.Count = count
	s.ID = r.VarInt()
	if p != nil && r.Err == nil {
		if id := mapID(p.itemsIn, s.ID); id >= 0 {
			s.ID = id
		} else {
			s.ID = -1
		}
	}
	if uint32(s.ID) >= Items {
		fail(r, fmt.Errorf("%w: item id %d", ErrInvalid, s.ID))
		return
	}
	pos, neg := r.VarInt(), r.VarInt()
	if pos < 0 || neg < 0 || int(pos)+int(neg) > r.Len() {
		fail(r, fmt.Errorf("%w: %d/%d components", ErrInvalid, pos, neg))
		return
	}
	for range pos {
		t := r.VarInt()
		if p != nil && r.Err == nil {
			ot := t
			if uint32(ot) >= uint32(len(p.compsIn)) {
				fail(r, fmt.Errorf("%w: component type %d", ErrInvalid, ot))
				return
			}
			if t = p.compsIn[ot]; t < 0 {
				// A component the newest version lacks: skipped.
				if delimited {
					take(r, int(r.VarInt()))
				} else if !p.skipOld(r, ot) {
					fail(r, fmt.Errorf("%w: old component type %d", ErrUnknownComponent, ot))
				}
				if r.Err != nil {
					return
				}
				continue
			}
		}
		if r.Err == nil && uint32(t) >= ComponentTypes {
			fail(r, fmt.Errorf("%w: component type %d", ErrInvalid, t))
		}
		if r.Err != nil {
			return
		}
		if delimited {
			b := take(r, int(r.VarInt()))
			if r.Err != nil {
				return
			}
			if Modeled.Has(t) {
				sub := wire.Reader{B: b}
				s.readValue(&sub, t)
				if sub.Err != nil {
					fail(r, sub.Err)
					return
				}
				if p != nil {
					s.fromOld(t, p)
				}
				s.Added.Add(t)
			} else if p != nil && rawKinds[t] == kindNone {
				continue // its wire form may differ in the newest version
			} else {
				s.Raw = append(s.Raw, RawComponent{t, append([]byte(nil), b...)})
			}
			s.Order = append(s.Order, t)
			continue
		}
		if Modeled.Has(t) {
			s.readValue(r, t)
			if p != nil {
				s.fromOld(t, p)
			}
			s.Added.Add(t)
			s.Order = append(s.Order, t)
			continue
		}
		start := r.Off
		if !skipRaw(r, t) {
			fail(r, fmt.Errorf("%w: %s", ErrUnknownComponent, componentNames[t]))
			return
		}
		if r.Err != nil {
			return
		}
		s.Raw = append(s.Raw, RawComponent{t, append([]byte(nil), r.B[start:r.Off]...)})
		s.Order = append(s.Order, t)
	}
	for range neg {
		t := r.VarInt()
		if p != nil && r.Err == nil {
			if uint32(t) >= uint32(len(p.compsIn)) {
				fail(r, fmt.Errorf("%w: component type %d", ErrInvalid, t))
				return
			}
			if t = p.compsIn[t]; t < 0 {
				continue
			}
		}
		if r.Err == nil && uint32(t) >= ComponentTypes {
			fail(r, fmt.Errorf("%w: component type %d", ErrInvalid, t))
		}
		if r.Err != nil {
			return
		}
		s.Removed.Add(t)
	}
	if s.orderIsDefault() {
		s.Order = s.Order[:0]
	}
}

// orderIsDefault reports whether Order is the order the encoder writes without it.
func (s *Stack) orderIsDefault() bool {
	prev, raw := int32(-1), 0
	for _, t := range s.Order {
		if s.Added.Has(t) {
			if t <= prev || raw > 0 {
				return false
			}
			prev = t
			continue
		}
		if raw >= len(s.Raw) || s.Raw[raw].Type != t {
			return false
		}
		raw++
	}
	return raw == len(s.Raw)
}

const (
	kindNone = iota
	kindVarInt
	kindFloat
	kindBool
	kindString
	kindUnit
	kindNBT
	kindInt32
)

// skipRaw skips the value of a component Stack does not model, if its wire form is known.
func skipRaw(r *wire.Reader, t int32) bool { return skipKind(r, rawKinds[t]) }

// skipKind skips a value of wire form kind (false: kindNone, not known).
func skipKind(r *wire.Reader, kind uint8) bool {
	switch kind {
	case kindVarInt:
		r.VarInt()
	case kindFloat:
		r.Float32()
	case kindInt32:
		r.Int32()
	case kindBool:
		r.Bool()
	case kindString:
		r.String(32767)
	case kindUnit:
	case kindNBT:
		skipNBT(r)
	default:
		return false
	}
	return true
}

const maxList = 1 << 16

// length reads a list length of at most max elements of at least minElem bytes each.
func length(r *wire.Reader, max, minElem int) int {
	n := r.VarInt()
	if r.Err != nil {
		return 0
	}
	if n < 0 || int(n) > max || int(n)*minElem > r.Len() {
		fail(r, fmt.Errorf("%w: list of %d", ErrInvalid, n))
		return 0
	}
	return int(n)
}

func (s *Stack) readValue(r *wire.Reader, t int32) {
	switch t {
	case CompCustomData:
		start := r.Off
		skipNBT(r)
		if r.Err == nil {
			s.CustomData = append(s.CustomData[:0], r.B[start:r.Off]...)
		}
	case CompMaxStackSize:
		s.MaxStackSize = r.VarInt()
	case CompMaxDamage:
		s.MaxDamage = r.VarInt()
	case CompDamage:
		s.Damage = r.VarInt()
	case CompRepairCost:
		s.RepairCost = r.VarInt()
	case CompUnbreakable:
	case CompCustomName:
		readText(r, &s.CustomName)
	case CompItemName:
		readText(r, &s.ItemName)
	case CompItemModel:
		s.ItemModel = r.String(32767)
	case CompLore:
		n := length(r, 256, 1)
		for range n {
			s.Lore = append(s.Lore, Text{})
			readText(r, &s.Lore[len(s.Lore)-1])
		}
	case CompEnchantments:
		s.Enchantments = readEnchantments(r, s.Enchantments[:0])
	case CompStoredEnchantments:
		s.StoredEnchantments = readEnchantments(r, s.StoredEnchantments[:0])
	case CompCustomModelData:
		d := &s.CustomModelData
		n := length(r, maxList, 4)
		for range n {
			d.Floats = append(d.Floats, r.Float32())
		}
		n = length(r, maxList, 1)
		for range n {
			d.Flags = append(d.Flags, r.Bool())
		}
		n = length(r, maxList, 1)
		for range n {
			d.Strings = append(d.Strings, r.String(32767))
		}
		n = length(r, maxList, 4)
		for range n {
			d.Colors = append(d.Colors, r.Int32())
		}
	case CompTooltipDisplay:
		s.TooltipDisplay.HideTooltip = r.Bool()
		n := length(r, ComponentTypes, 1)
		for range n {
			s.TooltipDisplay.Hidden = append(s.TooltipDisplay.Hidden, r.VarInt())
		}
	case CompEnchantmentGlintOverride:
		s.EnchantmentGlintOverride = r.Bool()
	case CompDyedColor:
		s.DyedColor = r.Int32()
	case CompPotionContents:
		p := &s.PotionContents
		if p.HasPotion = r.Bool(); p.HasPotion {
			p.Potion = r.VarInt()
		}
		if p.HasCustomColor = r.Bool(); p.HasCustomColor {
			p.CustomColor = r.Int32()
		}
		n := length(r, maxList, 6)
		for range n {
			p.Effects = append(p.Effects, Effect{ID: r.VarInt()})
			readEffectDetails(r, &p.Effects[len(p.Effects)-1], 0)
		}
		if p.HasCustomName = r.Bool(); p.HasCustomName {
			p.CustomName = r.String(32767)
		}
	}
}

func readEnchantments(r *wire.Reader, e []Enchantment) []Enchantment {
	n := length(r, maxList, 2)
	for range n {
		e = append(e, Enchantment{ID: r.VarInt(), Level: r.VarInt()})
	}
	return e
}

func readEffectDetails(r *wire.Reader, e *Effect, depth int) {
	e.Amplifier = r.VarInt()
	e.Duration = r.VarInt()
	e.Ambient = r.Bool()
	e.ShowParticles = r.Bool()
	e.ShowIcon = r.Bool()
	if r.Bool() {
		if depth >= 16 {
			fail(r, fmt.Errorf("%w: hidden effects nested too deep", ErrInvalid))
			return
		}
		e.Hidden = &Effect{}
		readEffectDetails(r, e.Hidden, depth+1)
	}
}

func readText(r *wire.Reader, t *Text) {
	start := r.Off
	typ := r.Byte()
	if typ == nbtString {
		t.Text, t.NBT = readMUTF8(r), nil
		return
	}
	skipPayload(r, typ, 0)
	if r.Err != nil {
		return
	}
	t.NBT = append([]byte(nil), r.B[start:r.Off]...)
	t.Text = PlainText(t.NBT)
}

func fail(r *wire.Reader, err error) {
	if r.Err == nil {
		r.Err = err
	}
	r.Off = len(r.B)
}

func take(r *wire.Reader, n int) []byte {
	if r.Err != nil {
		return nil
	}
	if n < 0 || n > r.Len() {
		fail(r, wire.ErrShort)
		return nil
	}
	b := r.B[r.Off : r.Off+n]
	r.Off += n
	return b
}

// HashedStack is the client's view of a slot in container_click: the item and count, with the
// components as hashes (skipped here: the server resends what it has).
type HashedStack struct {
	Count, ID int32 // Count 0: empty
}

// Decode reads a hashed stack.
func (h *HashedStack) Decode(r *wire.Reader) { h.DecodeFor(r, nil) }

// DecodeFor reads a hashed stack sent by a client of p's version (nil: the newest): ID is the
// newest id, -1 if the newest version has no such item.
func (h *HashedStack) DecodeFor(r *wire.Reader, p *Proto) {
	*h = HashedStack{}
	if !r.Bool() {
		return
	}
	h.ID = r.VarInt()
	if p != nil {
		h.ID = mapID(p.itemsIn, h.ID)
	}
	h.Count = r.VarInt()
	n := length(r, 256, 5)
	for range n {
		r.VarInt()
		r.Int32()
	}
	n = length(r, 256, 1)
	for range n {
		r.VarInt()
	}
}

// Name returns the key of item id ("minecraft:stone"), or "" if there is none.
func Name(id int32) string {
	if uint32(id) >= Items {
		return ""
	}
	return itemNames[id]
}

var (
	byNameOnce sync.Once
	byName     map[string]int32
)

// ByName returns the protocol id of an item key such as "minecraft:stone".
func ByName(name string) (int32, bool) {
	byNameOnce.Do(func() {
		byName = make(map[string]int32, Items)
		for i, n := range itemNames {
			byName[n] = int32(i)
		}
	})
	id, ok := byName[name]
	return id, ok
}

// ComponentName returns the key of data component type t ("minecraft:damage").
func ComponentName(t int32) string {
	if uint32(t) >= ComponentTypes {
		return ""
	}
	return componentNames[t]
}

// DefaultMaxStackSize is item id's max_stack_size without a patch.
func DefaultMaxStackSize(id int32) int32 {
	if uint32(id) >= Items {
		return 1
	}
	return int32(defaultMaxStack[id])
}

// DefaultMaxDamage is item id's max_damage without a patch (0: the item does not take damage).
func DefaultMaxDamage(id int32) int32 {
	if uint32(id) >= Items {
		return 0
	}
	return int32(defaultMaxDamage[id])
}
