package javasession

import (
	"encoding/json"
	"errors"
	"math"
	"strconv"

	"github.com/df-mc/dragonfly/server/player/dialogue"
	"github.com/df-mc/dragonfly/server/player/form"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/ezchr/go-mcjava/text"
	v777 "github.com/ezchr/go-mcjava/v777"
	"github.com/ezchr/go-mcjava/wire"
)

// Forms are shown as Java dialogs (show_dialog, 1.21.6+), built from the form's Bedrock JSON so
// every form.Form works:
//
//	menu ("form")  -> multi_action dialog: body text, labels and headers as body, a button per
//	                  form button (one column); a Cancel exit action closes it
//	modal          -> multi_action dialog with the two buttons side by side and a Cancel exit action
//	custom_form    -> multi_action dialog: labels/headers as body, inputs as dialog inputs
//	                  (input -> text, toggle -> boolean, slider -> number_range, dropdown and
//	                  step_slider -> single_option), a Submit button and a Cancel exit action
//
// Every button runs a dynamic/custom action (id dragonfly:form) whose payload carries the form id
// and the button, plus the input values; the client sends it back as custom_click_action. The
// session rebuilds the Bedrock response JSON from it and calls SubmitJSON, as the Bedrock session
// does for ModalFormResponse. Cancel (or Escape) submits nil, which runs the form's Closer.
// Button images are not shown. NPC dialogues (SendDialogue) are the same multi_action dialog.

const (
	formAction     = "dragonfly:form"
	dialogueAction = "dragonfly:dialogue"
	maxOpenForms   = 10
)

type bedrockForm struct {
	Type     string          `json:"type"`
	Title    string          `json:"title"`
	Content  json.RawMessage `json:"content"`
	Elements []formElement   `json:"elements"`
	Button1  string          `json:"button1"`
	Button2  string          `json:"button2"`
}

type formElement struct {
	Type        string   `json:"type"`
	Text        string   `json:"text"`
	Default     any      `json:"default"`
	Placeholder string   `json:"placeholder"`
	Min         float64  `json:"min"`
	Max         float64  `json:"max"`
	Step        float64  `json:"step"`
	Options     []string `json:"options"`
	Steps       []string `json:"steps"`
}

// SendForm shows a form as a dialog.
func (s *Session) SendForm(f form.Form) {
	b, err := json.Marshal(f)
	if err != nil {
		s.log.Debug("form", "err", err)
		return
	}
	var bf bedrockForm
	if err := json.Unmarshal(b, &bf); err != nil {
		s.log.Debug("form", "err", err)
		return
	}
	ts := s.txt()
	ts.mu.Lock()
	if ts.forms == nil {
		ts.forms = map[int32]form.Form{}
	}
	if len(ts.forms) >= maxOpenForms {
		for k := range ts.forms {
			delete(ts.forms, k)
			break
		}
	}
	ts.nextForm++
	id := ts.nextForm
	ts.forms[id] = f
	ts.mu.Unlock()

	w := s.packet()
	w.VarInt(0) // a dialog given inline, not a registry entry
	w.Byte(text.TagCompound)
	text.StringTag(w, "type", "minecraft:multi_action")
	title := bedrockText(bf.Title)
	text.ComponentTag(w, "title", &title)
	text.BoolTag(w, "pause", false)
	text.StringTag(w, "after_action", "close")

	var body []string
	var buttons []string
	var inputs []formElement
	switch bf.Type {
	case "form":
		var content string
		_ = json.Unmarshal(bf.Content, &content)
		body = appendBody(body, content)
		for _, e := range bf.Elements {
			switch e.Type {
			case "button":
				buttons = append(buttons, e.Text)
			case "label", "header":
				body = appendBody(body, e.Text)
			}
		}
	case "modal":
		var content string
		_ = json.Unmarshal(bf.Content, &content)
		body = appendBody(body, content)
		buttons = []string{bf.Button1, bf.Button2}
	case "custom_form":
		_ = json.Unmarshal(bf.Content, &inputs)
		for _, e := range inputs {
			if e.Type == "label" || e.Type == "header" {
				body = appendBody(body, e.Text)
			}
		}
	}
	writeBody(w, body)
	if bf.Type == "custom_form" {
		writeInputs(w, inputs)
		text.ListStart(w, "actions", text.TagCompound, 1)
		writeButton(w, text.Translatable("gui.done"), formAction, id, -1)
	} else {
		if len(buttons) == 0 {
			buttons = []string{""} // a dialog needs at least one action
		}
		text.ListStart(w, "actions", text.TagCompound, len(buttons))
		for i, b := range buttons {
			writeButton(w, labelText(b), formAction, id, int32(i))
		}
	}
	if bf.Type == "modal" {
		text.IntTag(w, "columns", 2)
	} else {
		text.IntTag(w, "columns", 1)
	}
	text.Key(w, text.TagCompound, "exit_action")
	writeButtonBody(w, text.Translatable("gui.cancel"), formAction, id, -2)
	w.Byte(text.TagEnd)
	s.queue(v777.ClientboundPlayShowDialog, w)
}

// javaKeys are Bedrock UI translation keys (form buttons such as form.YesButton) that Java has too.
var javaKeys = map[string]bool{"gui.yes": true, "gui.no": true, "gui.ok": true, "gui.cancel": true,
	"gui.done": true, "gui.back": true, "gui.close": true}

// labelText converts a button or title text, translating the keys Java knows.
func labelText(s string) text.Component {
	if javaKeys[s] {
		return text.Translatable(s)
	}
	return bedrockText(s)
}

func appendBody(body []string, t string) []string {
	if t == "" {
		return body
	}
	return append(body, t)
}

// writeBody writes the dialog body: one plain_message per text.
func writeBody(w *wire.Writer, body []string) {
	if len(body) == 0 {
		return
	}
	text.ListStart(w, "body", text.TagCompound, len(body))
	for _, b := range body {
		text.StringTag(w, "type", "minecraft:plain_message")
		c := bedrockText(b)
		text.ComponentTag(w, "contents", &c)
		text.IntTag(w, "width", 300)
		w.Byte(text.TagEnd)
	}
}

// writeInputs writes a custom form's input elements as dialog inputs keyed e<index>.
func writeInputs(w *wire.Writer, elems []formElement) {
	n := 0
	for _, e := range elems {
		if isInput(e.Type) {
			n++
		}
	}
	if n == 0 {
		return
	}
	text.ListStart(w, "inputs", text.TagCompound, n)
	for i, e := range elems {
		if !isInput(e.Type) {
			continue
		}
		text.StringTag(w, "key", "e"+strconv.Itoa(i))
		label := bedrockText(e.Text)
		switch e.Type {
		case "input":
			text.StringTag(w, "type", "minecraft:text")
			text.ComponentTag(w, "label", &label)
			def, _ := e.Default.(string)
			text.StringTag(w, "initial", def)
			text.IntTag(w, "max_length", int32(max(256, len(def))))
			text.IntTag(w, "width", 300)
		case "toggle":
			text.StringTag(w, "type", "minecraft:boolean")
			text.ComponentTag(w, "label", &label)
			def, _ := e.Default.(bool)
			text.BoolTag(w, "initial", def)
		case "slider":
			text.StringTag(w, "type", "minecraft:number_range")
			text.ComponentTag(w, "label", &label)
			lo, hi := e.Min, e.Max
			if hi <= lo {
				hi = lo + 1
			}
			text.FloatTag(w, "start", float32(lo))
			text.FloatTag(w, "end", float32(hi))
			if def, ok := e.Default.(float64); ok {
				text.FloatTag(w, "initial", float32(min(max(def, lo), hi)))
			}
			if e.Step > 0 {
				text.FloatTag(w, "step", float32(e.Step))
			}
			text.IntTag(w, "width", 300)
		case "dropdown", "step_slider":
			opts := e.Options
			if e.Type == "step_slider" {
				opts = e.Steps
			}
			if len(opts) == 0 {
				opts = []string{""}
			}
			def := 0
			if d, ok := e.Default.(float64); ok {
				def = int(d)
			}
			text.StringTag(w, "type", "minecraft:single_option")
			text.ComponentTag(w, "label", &label)
			text.ListStart(w, "options", text.TagCompound, len(opts))
			for j, o := range opts {
				text.StringTag(w, "id", strconv.Itoa(j))
				c := bedrockText(o)
				text.ComponentTag(w, "display", &c)
				if j == def {
					text.BoolTag(w, "initial", true)
				}
				w.Byte(text.TagEnd)
			}
			text.IntTag(w, "width", 300)
		}
		w.Byte(text.TagEnd)
	}
}

func isInput(t string) bool {
	switch t {
	case "input", "toggle", "slider", "dropdown", "step_slider":
		return true
	}
	return false
}

// writeButton writes one element of an actions list.
func writeButton(w *wire.Writer, label text.Component, action string, id, button int32) {
	writeButtonBody(w, label, action, id, button)
}

// writeButtonBody writes a button compound's entries and its end: a label and a dynamic/custom
// action whose payload is {f: id, b: button} (b -1: submit, -2: closed).
func writeButtonBody(w *wire.Writer, label text.Component, action string, id, button int32) {
	text.ComponentTag(w, "label", &label)
	text.Key(w, text.TagCompound, "action")
	text.StringTag(w, "type", "minecraft:dynamic/custom")
	text.StringTag(w, "id", action)
	text.Key(w, text.TagCompound, "additions")
	text.IntTag(w, "f", id)
	text.IntTag(w, "b", button)
	w.Byte(text.TagEnd)
	w.Byte(text.TagEnd)
	w.Byte(text.TagEnd)
}

// CloseForm closes an open dialog.
func (s *Session) CloseForm() {
	s.queue(v777.ClientboundPlayClearDialog, s.packet())
}

// SendDialogue shows an NPC dialogue as a dialog: the NPC's name as the title, the text as the
// body, a button per dialogue button.
func (s *Session) SendDialogue(d dialogue.Dialogue, _ world.Entity) {
	ts := s.txt()
	ts.mu.Lock()
	if ts.dialogues == nil {
		ts.dialogues = map[int32]dialogue.Dialogue{}
	}
	clear(ts.dialogues) // one NPC dialogue at a time, like Bedrock
	ts.nextForm++
	id := ts.nextForm
	ts.dialogues[id] = d
	ts.mu.Unlock()

	w := s.packet()
	w.VarInt(0)
	w.Byte(text.TagCompound)
	text.StringTag(w, "type", "minecraft:multi_action")
	title := bedrockText(d.Title())
	text.ComponentTag(w, "title", &title)
	text.BoolTag(w, "pause", false)
	writeBody(w, appendBody(nil, d.Body()))
	buttons := d.Buttons()
	if len(buttons) == 0 {
		text.ListStart(w, "actions", text.TagCompound, 1)
		writeButton(w, text.Translatable("gui.ok"), dialogueAction, id, -2)
	} else {
		text.ListStart(w, "actions", text.TagCompound, len(buttons))
		for i, b := range buttons {
			writeButton(w, labelText(b.Text), dialogueAction, id, int32(i))
		}
	}
	text.IntTag(w, "columns", 1)
	text.Key(w, text.TagCompound, "exit_action")
	writeButtonBody(w, text.Translatable("gui.cancel"), dialogueAction, id, -2)
	w.Byte(text.TagEnd)
	s.queue(v777.ClientboundPlayShowDialog, w)
}

// CloseDialogue closes an open NPC dialogue.
func (s *Session) CloseDialogue() {
	ts := s.txt()
	ts.mu.Lock()
	open := len(ts.dialogues) > 0
	clear(ts.dialogues)
	ts.mu.Unlock()
	if open {
		s.CloseForm()
	}
}

// handleClickAction handles a custom_click_action: a form or dialogue button.
func (s *Session) handleClickAction(id string, payload []byte) {
	if id != formAction && id != dialogueAction {
		return
	}
	vals, err := readClickPayload(payload)
	if err != nil {
		s.log.Debug("click action payload", "err", err)
		return
	}
	fid, _ := vals["f"].(int32)
	button, _ := vals["b"].(int32)
	ts := s.txt()
	ts.mu.Lock()
	if id == dialogueAction {
		d, ok := ts.dialogues[fid]
		delete(ts.dialogues, fid)
		ts.mu.Unlock()
		if !ok {
			return
		}
		s.inTxTx(func(tx *world.Tx, c session.Controllable) {
			if button < 0 {
				d.Close(c, tx)
				return
			}
			if err := d.Submit(uint(button), c, tx); err != nil {
				s.log.Debug("dialogue submit", "err", err)
			}
		})
		return
	}
	f, ok := ts.forms[fid]
	delete(ts.forms, fid)
	ts.mu.Unlock()
	if !ok {
		return
	}
	resp, err := formResponse(f, button, vals)
	if err != nil {
		// The form is gone from ts.forms: answer it as closed so its Closer still runs.
		s.log.Debug("form response", "err", err)
		resp = nil
	}
	s.inTxTx(func(tx *world.Tx, c session.Controllable) {
		if err := f.SubmitJSON(resp, c, tx); err != nil {
			s.log.Debug("form submit", "err", err)
		}
	})
}

func (s *Session) inTxTx(f func(tx *world.Tx, c session.Controllable)) {
	if err := s.withPlayer(f); err != nil && !stopped(err) {
		s.log.Debug("form", "err", err)
	}
}

// formResponse builds the Bedrock ModalFormResponse JSON for a dialog answer: nil when closed, the
// button index for a menu, true/false for a modal, the value array for a custom form.
func formResponse(f form.Form, button int32, vals map[string]any) ([]byte, error) {
	if button == -2 {
		return nil, nil
	}
	b, err := json.Marshal(f)
	if err != nil {
		return nil, err
	}
	var bf bedrockForm
	if err := json.Unmarshal(b, &bf); err != nil {
		return nil, err
	}
	switch bf.Type {
	case "form":
		if button < 0 {
			return nil, errors.New("menu answered without a button")
		}
		return json.Marshal(button)
	case "modal":
		return json.Marshal(button == 0)
	case "custom_form":
		var elems []formElement
		_ = json.Unmarshal(bf.Content, &elems)
		out := make([]any, len(elems))
		for i, e := range elems {
			v := vals["e"+strconv.Itoa(i)]
			switch e.Type {
			case "input":
				s, _ := v.(string)
				out[i] = s
			case "toggle":
				b, _ := v.(int8)
				out[i] = b != 0
			case "slider":
				fv, _ := v.(float32)
				if math.IsNaN(float64(fv)) || math.IsInf(float64(fv), 0) {
					// Not a number a slider can hold (the payload is the client's): the
					// answer is unusable, so the form counts as closed.
					return nil, errors.New("slider value is not a finite number")
				}
				out[i] = roundSlider(float64(fv), e)
			case "dropdown", "step_slider":
				s, _ := v.(string)
				n, _ := strconv.Atoi(s)
				out[i] = n
			}
		}
		return json.Marshal(out)
	}
	return nil, errors.New("unknown form type " + bf.Type)
}

// roundSlider snaps a slider value to the slider's step and range (the float32 round trip and
// the client's own steps can leave it a hair outside).
func roundSlider(v float64, e formElement) float64 {
	if e.Step > 0 {
		v = e.Min + math.Round((v-e.Min)/e.Step)*e.Step
	}
	return min(max(v, e.Min), e.Max)
}

// readClickPayload reads the payload of a custom_click_action: optional network NBT (tag type 0
// for none), a compound of the button's additions and the inputs' values. Values: string, int8
// (booleans), int32, float32.
func readClickPayload(b []byte) (map[string]any, error) {
	r := wire.NewReader(b)
	t := r.Byte()
	if r.Err != nil || t == text.TagEnd {
		return map[string]any{}, nil
	}
	if t == 1 && len(b) > 1 && b[1] == text.TagCompound { // a presence boolean, as some libraries write
		t = r.Byte()
	}
	if t != text.TagCompound {
		return nil, errors.New("payload is not a compound")
	}
	m := map[string]any{}
	for i := 0; i < 64; i++ {
		tt := r.Byte()
		if r.Err != nil {
			return nil, r.Err
		}
		if tt == text.TagEnd {
			return m, nil
		}
		name := readNBTString(r)
		switch tt {
		case text.TagByte:
			m[name] = r.Int8()
		case text.TagShort:
			m[name] = int32(r.Int16())
		case text.TagInt:
			m[name] = r.Int32()
		case text.TagFloat:
			m[name] = r.Float32()
		case text.TagDouble:
			m[name] = float32(r.Float64())
		case text.TagString:
			m[name] = readNBTString(r)
		default:
			return nil, errors.New("unexpected tag type in payload")
		}
		if r.Err != nil {
			return nil, r.Err
		}
	}
	return nil, errors.New("payload too large")
}

// readNBTString reads an NBT string (u16 length, modified UTF-8: an emoji typed into a dialog
// text input arrives as two surrogates, a NUL as C0 80) as a Go string.
func readNBTString(r *wire.Reader) string { return text.ReadString(r) }
