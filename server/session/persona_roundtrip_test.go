package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/player/skin"
)

// TestPersonaRoundTrip checks that everything a persona skin needs survives being converted out to the
// protocol and back. Dropping any of it is what left persona skins unrenderable for other players.
func TestPersonaRoundTrip(t *testing.T) {
	in := skin.New(128, 128)
	in.Persona = true
	in.Premium = true
	in.CapeOnClassic = true
	in.PrimaryUser = true
	in.OverrideAppearance = true
	in.PlayFabID = "playfab-id"
	in.SkinID = "persona-1234"
	in.CapeID = "cape-5678"
	in.FullID = "full-9012"
	in.GeometryVersion = "1.16.0"
	in.ArmSize = "slim"
	in.SkinColour = "#ffb9674a"
	in.AnimationData = `{"animation":"data"}`
	in.Model = []byte(`{"format_version":"1.12.0"}`)
	in.ModelConfig = skin.ModelConfig{Default: "geometry.persona_x"}
	in.PersonaPieces = []skin.PersonaPiece{
		{PieceID: "p1", PieceType: "persona_body", PackID: "0fba4063-dba1-4a81-9b6e-0c3c8e6e7e10", Default: true, ProductID: ""},
		{PieceID: "p2", PieceType: "persona_facial_hair", PackID: "0fba4063-dba1-4a81-9b6e-0c3c8e6e7e11", Default: false, ProductID: "prod-2"},
		{PieceID: "p3", PieceType: "persona_hand", PackID: "not-a-uuid", Default: false, ProductID: "prod-3"},
	}
	in.PieceTintColours = []skin.PersonaPieceTintColour{
		{PieceType: "persona_eyes", Colours: [4]string{"#ffa12722", "#ff2f1f0f", "#ff3aafd9", "#0"}},
	}

	pk := skinToProtocol(in)

	if !pk.PersonaSkin {
		t.Error("PersonaSkin was not forwarded")
	}
	if !pk.PremiumSkin || !pk.PersonaCapeOnClassicSkin || !pk.PrimaryUser || !pk.OverrideAppearance {
		t.Error("persona boolean flags were not all forwarded")
	}
	if pk.SkinID != "persona-1234" {
		t.Errorf("SkinID was regenerated instead of preserved: %q", pk.SkinID)
	}
	if pk.CapeID != "cape-5678" {
		t.Errorf("CapeID was regenerated instead of preserved: %q", pk.CapeID)
	}
	if string(pk.AnimationData) != `{"animation":"data"}` {
		t.Errorf("AnimationData was dropped: %q", pk.AnimationData)
	}
	if string(pk.GeometryDataEngineVersion) != "1.16.0" {
		t.Errorf("GeometryDataEngineVersion not preserved: %q", pk.GeometryDataEngineVersion)
	}
	if len(pk.PersonaPieces) != 3 {
		t.Fatalf("expected 3 persona pieces, got %v", len(pk.PersonaPieces))
	}
	// persona_body is type 2 and persona_hand is type 9 in the client's own ordering.
	if pk.PersonaPieces[0].PieceType != 2 {
		t.Errorf("persona_body mapped to %v, want 2", pk.PersonaPieces[0].PieceType)
	}
	if pk.PersonaPieces[1].PieceType != 11 {
		t.Errorf("persona_facial_hair mapped to %v, want 11", pk.PersonaPieces[1].PieceType)
	}
	if pk.PersonaPieces[2].PieceType != 9 {
		t.Errorf("persona_hand mapped to %v, want 9", pk.PersonaPieces[2].PieceType)
	}
	if pk.PersonaPieces[0].PackID.String() != "0fba4063-dba1-4a81-9b6e-0c3c8e6e7e10" {
		t.Errorf("pack ID not preserved: %v", pk.PersonaPieces[0].PackID)
	}
	if len(pk.PieceTintColours) != 1 {
		t.Fatalf("expected 1 tint, got %v", len(pk.PieceTintColours))
	}
	if c := pk.PieceTintColours[0].Colours[0]; c.A != 0xff || c.R != 0xa1 || c.G != 0x27 || c.B != 0x22 {
		t.Errorf("tint colour parsed wrong: %+v", c)
	}
	if c := pk.PieceTintColours[0].Colours[3]; c.A != 0 || c.R != 0 || c.G != 0 || c.B != 0 {
		t.Errorf("unused tint slot should be zero, got %+v", c)
	}
	if c := pk.SkinColour; c.A != 0xff || c.R != 0xb9 || c.G != 0x67 || c.B != 0x4a {
		t.Errorf("skin colour parsed wrong: %+v", c)
	}

	// Now back the other way, the path a live in-game skin change takes.
	pk.SkinResourcePatch = in.ModelConfig.Encode()
	out, err := protocolToSkin(pk)
	if err != nil {
		t.Fatalf("protocolToSkin: %v", err)
	}
	if !out.Persona {
		t.Error("Persona lost on the way back")
	}
	if out.AnimationData != in.AnimationData {
		t.Errorf("AnimationData lost on the way back: %q", out.AnimationData)
	}
	if out.SkinID != in.SkinID || out.CapeID != in.CapeID {
		t.Errorf("skin/cape ID lost on the way back: %q %q", out.SkinID, out.CapeID)
	}
	if len(out.PersonaPieces) != 3 {
		t.Fatalf("expected 3 pieces back, got %v", len(out.PersonaPieces))
	}
	for i, want := range []string{"persona_body", "persona_facial_hair", "persona_hand"} {
		if out.PersonaPieces[i].PieceType != want {
			t.Errorf("piece %v came back as %q, want %q", i, out.PersonaPieces[i].PieceType, want)
		}
	}
	if len(out.PieceTintColours) != 1 {
		t.Fatalf("expected 1 tint back, got %v", len(out.PieceTintColours))
	}
	if got := out.PieceTintColours[0].Colours[0]; got != "#ffa12722" {
		t.Errorf("tint came back as %q, want %q", got, "#ffa12722")
	}
	if out.PieceTintColours[0].PieceType != "persona_eyes" {
		t.Errorf("tint piece type came back as %q", out.PieceTintColours[0].PieceType)
	}
	if out.SkinColour != "#ffb9674a" {
		t.Errorf("skin colour came back as %q", out.SkinColour)
	}
	if out.ArmSize != "slim" {
		t.Errorf("arm size came back as %q", out.ArmSize)
	}
}
