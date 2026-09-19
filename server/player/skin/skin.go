package skin

import (
	"image"
	"image/color"
)

// Skin holds the data of a skin that a player has equipped. It includes geometry data, the texture and the
// cape, if one is present.
// Skin implements the image.Image interface to ease working with the value as an image.
type Skin struct {
	w, h int
	// Persona specifies if the skin uses the persona skin system.
	Persona   bool
	// ForceRejected marks a skin to be sent in a shape that other clients are
	// known to refuse and fall back to their own default appearance for,
	// rather than the shape the wearer's own client actually sent. Set by
	// DisableIfAnimatedPersona; skinToProtocol (session/session_list.go) is
	// what actually acts on it.
	ForceRejected bool
	PlayFabID string
	FullID    string

	// Pix holds the raw pixel data of the skin. This is an RGBA byte slice, meaning that every first byte is
	// a Red value, the second a Green value, the third a Blue value and the fourth an Alpha value.
	Pix []uint8

	// ModelConfig specifies how the Model field below should be used to form the total skin.
	ModelConfig ModelConfig
	// Model holds the raw JSON data that represents the model of the skin. If empty, it means the skin holds
	// the standard skin data (geometry.humanoid).
	// TODO: Write a full API for this. The model should be able to be easily modified or created runtime.
	Model []byte

	// Cape holds the cape of the skin. By default, an empty cape is set in the skin. Cape.Exists() may be
	// called to check if the cape actually has any data.
	Cape Cape

	// Animations holds a list of all animations that the skin has. These animations must be pointed to in the
	// ModelConfig, in order to display them on the skin.
	Animations []Animation

	// ArmSize is the size of the arms of the player's model - either "wide" (generally for male/Steve-style
	// skins) or "slim" (generally for female/Alex-style skins), as sent by the real client in
	// login.ClientData.ArmSize. Previously never captured or re-forwarded here, meaning every player shown
	// through this skin type was rebroadcast to other clients with arm geometry defaulting to the protocol
	// zero value (ArmSizeSlim), regardless of the real skin's actual arm size.
	ArmSize string
	// SkinColour is a hex representation (including '#') of the base colour of the skin, as sent by the real
	// client in login.ClientData.SkinColour. Previously never captured or re-forwarded here.
	SkinColour string
}

// DisableIfAnimatedPersona marks the skin to be rejected by other clients if
// it is both a Persona skin and carries animation data.
//
// Persona plus animation is also the one combination nether2rak's relay is
// unable to render correctly to other players: the affected player becomes an
// invisible body with only a floating head visible to everyone else, while
// looking completely normal to themselves. Since only the wearer can tell
// anything is wrong, this was being used deliberately to grief other players.
//
// This does not blank the skin's own pixel data - an empty 64x64 texture is
// still a technically valid skin, and sending one made the wearer fully
// invisible with no name tag at all, worse than the bug it was meant to
// mitigate. Instead ForceRejected is set, and skinToProtocol (session_list.go)
// turns that into the specific combination already confirmed, live, to make a
// client refuse the skin and fall back to its own built-in default appearance
// on its own: PersonaSkin true with no PersonaPieces data at all. That is
// exactly the shape that produced the "skin shows as default Steve" bug this
// session fixed for legitimate persona skins by forcing PersonaSkin false -
// deliberately reintroduced here, only for this one flagged case, because a
// real client-side default skin is what closes the exploit without the server
// needing a texture asset of its own.
//
// This is a mitigation, not a fix: the actual rendering bug is still
// unresolved (see PERSONA_SKIN_FIX.md). It trades the exploit for the same
// default skin a client already shows itself whenever it rejects a skin,
// which is a worthwhile trade until the real cause is found.
func (s *Skin) DisableIfAnimatedPersona() {
	if s.Persona && len(s.Animations) > 0 {
		s.ForceRejected = true
	}
}

// New creates a new skin using the width and height passed. The dimensions passed must be either 64x32,
// 64x64 or 128x128. An error is returned if other dimensions are used.
// The skin pixels are initialised for the skin, and a random skin ID is picked. The model name and model is
// left empty.
func New(width, height int) Skin {
	return Skin{
		w:   width,
		h:   height,
		Pix: make([]uint8, width*height*4),
	}
}

// Bounds returns the bounds of the skin. These are either 64x32, 64x64 or 128, depending on the bounds of the
// skin of the player.
func (s Skin) Bounds() image.Rectangle {
	return image.Rectangle{
		Max: image.Point{X: s.w, Y: s.h},
	}
}

// ColorModel returns color.RGBAModel.
func (s Skin) ColorModel() color.Model {
	return color.RGBAModel
}

// At returns the colour at a given position in the skin. The concrete value of the colour returned is a color.RGBA
// value.
// If the x or y values exceed the bounds of the skin, At will panic.
func (s Skin) At(x, y int) color.Color {
	if x < 0 || y < 0 || x >= s.w || y >= s.h {
		panic("pixel coordinates out of bounds")
	}
	offset := x*4 + s.w*y*4
	return color.RGBA{
		R: s.Pix[offset],
		G: s.Pix[offset+1],
		B: s.Pix[offset+2],
		A: s.Pix[offset+3],
	}
}
