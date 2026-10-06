package skin

// PersonaPiece is one of the pieces a persona skin is assembled from. A persona skin is not a flat texture:
// the client builds it from a list of pieces, each naming marketplace content by ID rather than carrying any
// pixel or mesh data of its own. A server that re-broadcasts a persona skin without these pieces gives the
// receiving clients nothing to assemble, which is why they have to be carried through unchanged.
type PersonaPiece struct {
	// PieceID is a UUID identifying this specific piece.
	PieceID string
	// PieceType is the kind of piece this is, in the persona_* form the client sends at login, such as
	// "persona_body" or "persona_facial_hair".
	PieceType string
	// PackID is a UUID identifying the pack the piece belongs to.
	PackID string
	// Default specifies whether the piece is one of the default pieces every Steve or Alex skin has.
	Default bool
	// ProductID is a UUID identifying the piece for purchases. It is empty for default pieces.
	ProductID string
}

// PersonaPieceTintColour holds the tint colours applied to one persona piece. Only some piece types carry
// tints: persona_mouth, persona_eyes and persona_hair are the usual ones.
type PersonaPieceTintColour struct {
	// PieceType is the piece the tints apply to, in the same persona_* form as PersonaPiece.PieceType. The
	// type must also be present in the piece list.
	PieceType string
	// Colours holds four ARGB colours in hex notation, such as "#ffa12722". Unused entries are "#0". For
	// persona_eyes the first three are the iris, the eyebrows and the sclera.
	Colours [4]string
}

// personaPieceTypes maps the persona_* piece names the client sends at login onto the numeric piece type the
// skin packet carries on the wire.
//
// The values are the ordinals of the piece type enum as the Bedrock client itself orders them, taken from a
// reference implementation that renders persona skins correctly rather than inferred from names. Note that
// "persona_coco" sits at 28 and pushes "unsupported" to 29: gophertunnel's own PieceType constants omit
// persona_coco entirely and so number "unsupported" one lower, which is why this table is spelled out here
// instead of being built from those constants.
var personaPieceTypes = map[string]uint32{
	"persona_unknown":        0,
	"persona_skeleton":       1,
	"persona_body":           2,
	"persona_skin":           3,
	"persona_bottom":         4,
	"persona_feet":           5,
	"persona_dress":          6,
	"persona_top":            7,
	"persona_high_pants":     8,
	"persona_hand":           9,
	"persona_outerwear":      10,
	"persona_facial_hair":    11,
	"persona_mouth":          12,
	"persona_eyes":           13,
	"persona_hair":           14,
	"persona_hood":           15,
	"persona_back":           16,
	"persona_face_accessory": 17,
	"persona_head":           18,
	"persona_legs":           19,
	"persona_left_leg":       20,
	"persona_right_leg":      21,
	"persona_arms":           22,
	"persona_left_arm":       23,
	"persona_right_arm":      24,
	"persona_capes":          25,
	"persona_classic_skin":   26,
	"persona_emote":          27,
	"persona_coco":           28,
	"unsupported":            29,
}

// personaPieceNames is the reverse of personaPieceTypes, built once so a skin arriving over the wire can be
// turned back into the persona_* names the rest of this package works in.
var personaPieceNames = func() map[uint32]string {
	names := make(map[uint32]string, len(personaPieceTypes))
	for name, id := range personaPieceTypes {
		names[id] = name
	}
	return names
}()

// PersonaPieceTypeID returns the numeric piece type a persona_* piece name maps to, falling back to the
// unknown type for a name this package does not know.
func PersonaPieceTypeID(name string) uint32 {
	return personaPieceTypes[name]
}

// PersonaPieceTypeName returns the persona_* piece name a numeric piece type maps to, falling back to
// "persona_unknown" for a type this package does not know.
func PersonaPieceTypeName(id uint32) string {
	if name, ok := personaPieceNames[id]; ok {
		return name
	}
	return "persona_unknown"
}
