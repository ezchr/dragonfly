package server

import (
	"encoding/base64"
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
)

// TestParseSkinLoginFields checks that parseSkin decodes the base64 login fields and carries the persona
// data through unchanged.
func TestParseSkinLoginFields(t *testing.T) {
	enc := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
	data := login.ClientData{
		SkinImageWidth:      64,
		SkinImageHeight:     64,
		SkinData:            base64.StdEncoding.EncodeToString(make([]byte, 64*64*4)),
		SkinGeometry:        enc("{}"),
		SkinResourcePatch:   enc(`{"geometry":{"default":"geometry.humanoid.custom"}}`),
		SkinGeometryVersion: enc("1.21.20"),
		SkinAnimationData:   enc(`{"anim":"test"}`),
		SkinID:              "skin-id",
		CapeID:              "cape-id",
		ArmSize:             "slim",
		SkinColour:          "#ffaabbcc",
		PersonaSkin:         true,
		PersonaPieces: []login.PersonaPiece{
			{PieceID: "piece-1", PieceType: "persona_body", PackID: "11111111-2222-3333-4444-555555555555", Default: true},
			{PieceID: "piece-2", PieceType: "persona_hair", PackID: "66666666-7777-8888-9999-000000000000", ProductID: "product-2"},
		},
		PieceTintColours: []login.PersonaPieceTintColour{
			{PieceType: "persona_hair", Colours: [4]string{"#ff112233", "#0", "#0", "#0"}},
		},
	}
	s := (&Server{}).parseSkin(data)
	if s.GeometryVersion != "1.21.20" {
		t.Errorf("GeometryVersion = %q, want 1.21.20", s.GeometryVersion)
	}
	if s.AnimationData != `{"anim":"test"}` {
		t.Errorf("AnimationData = %q, want {\"anim\":\"test\"}", s.AnimationData)
	}
	if s.ArmSize != "slim" || s.SkinID != "skin-id" || s.CapeID != "cape-id" || s.FullID != "skin-id" || s.SkinColour != "#ffaabbcc" || !s.Persona {
		t.Errorf("ids, arm size, colour or persona flag changed: %+v", s)
	}
	if len(s.PersonaPieces) != 2 || s.PersonaPieces[1].PieceType != "persona_hair" || s.PersonaPieces[1].ProductID != "product-2" || !s.PersonaPieces[0].Default {
		t.Errorf("persona pieces changed: %+v", s.PersonaPieces)
	}
	if len(s.PieceTintColours) != 1 || s.PieceTintColours[0].Colours != data.PieceTintColours[0].Colours {
		t.Errorf("tint colours changed: %+v", s.PieceTintColours)
	}

	// Fields that are empty or not valid base64 decode to nothing rather than garbage.
	data.SkinGeometryVersion, data.SkinAnimationData = "", "not base64!"
	s = (&Server{}).parseSkin(data)
	if s.GeometryVersion != "" || s.AnimationData != "" {
		t.Errorf("empty/invalid fields decoded to %q and %q, want empty", s.GeometryVersion, s.AnimationData)
	}
}
