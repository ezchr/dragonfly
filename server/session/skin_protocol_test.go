package session

import (
	"image/color"
	"testing"

	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// TestSkinToProtocol checks that skinToProtocol carries the persona data, the decoded login fields and the
// IDs through unchanged, falls back to the current version for an empty geometry version, and that
// protocolToSkin turns the result back into the same skin data.
func TestSkinToProtocol(t *testing.T) {
	s := skin.New(64, 64)
	s.Persona = true
	s.GeometryVersion = "1.21.20"
	s.AnimationData = `{"anim":"test"}`
	s.ArmSize = "slim"
	s.SkinID, s.CapeID, s.FullID = "skin-id", "cape-id", "full-id"
	s.SkinColour = "#ffaabbcc"
	s.PersonaPieces = []skin.PersonaPiece{
		{PieceID: "piece-1", PieceType: "persona_body", PackID: "11111111-2222-3333-4444-555555555555", Default: true},
		{PieceID: "piece-2", PieceType: "unsupported", PackID: "66666666-7777-8888-9999-000000000000", ProductID: "product-2"},
	}
	s.PieceTintColours = []skin.PersonaPieceTintColour{
		{PieceType: "persona_hair", Colours: [4]string{"#ff112233", "#0", "#0", "#0"}},
	}

	p := skinToProtocol(s)
	if string(p.GeometryDataEngineVersion) != "1.21.20" {
		t.Errorf("GeometryDataEngineVersion = %q, want 1.21.20", p.GeometryDataEngineVersion)
	}
	if string(p.AnimationData) != `{"anim":"test"}` {
		t.Errorf("AnimationData = %q, want {\"anim\":\"test\"}", p.AnimationData)
	}
	if p.ArmSize != protocol.ArmSizeSlim || p.SkinID != "skin-id" || p.CapeID != "cape-id" || p.FullID != "full-id" || !p.PersonaSkin {
		t.Errorf("arm size, ids or persona flag changed: %+v", p)
	}
	if p.SkinColour != (color.RGBA{R: 0xaa, G: 0xbb, B: 0xcc, A: 0xff}) {
		t.Errorf("SkinColour = %v", p.SkinColour)
	}
	if len(p.PersonaPieces) != 2 || p.PersonaPieces[0].PieceType != protocol.PieceTypeBody || p.PersonaPieces[1].PieceType != protocol.PieceTypeUnsupported ||
		p.PersonaPieces[1].ProductID != "product-2" || p.PersonaPieces[0].PackID.String() != "11111111-2222-3333-4444-555555555555" {
		t.Errorf("persona pieces changed: %+v", p.PersonaPieces)
	}
	if protocol.PieceTypeUnsupported != 28 {
		t.Errorf("unsupported is piece type %d, want 28", protocol.PieceTypeUnsupported)
	}
	if len(p.PieceTintColours) != 1 || p.PieceTintColours[0].Colours[0] != (color.RGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xff}) {
		t.Errorf("tint colours changed: %+v", p.PieceTintColours)
	}

	back, err := protocolToSkin(p)
	if err != nil {
		t.Fatal(err)
	}
	if back.GeometryVersion != "1.21.20" || back.AnimationData != `{"anim":"test"}` || back.ArmSize != "slim" || back.SkinColour != "#ffaabbcc" {
		t.Errorf("protocolToSkin changed fields: %+v", back)
	}
	if len(back.PersonaPieces) != 2 || back.PersonaPieces[0].PieceType != "persona_body" || back.PersonaPieces[1].PieceType != "unsupported" {
		t.Errorf("protocolToSkin piece types: %+v", back.PersonaPieces)
	}

	s.GeometryVersion = ""
	if v := string(skinToProtocol(s).GeometryDataEngineVersion); v != protocol.CurrentVersion {
		t.Errorf("empty geometry version sent as %q, want %q", v, protocol.CurrentVersion)
	}
}
