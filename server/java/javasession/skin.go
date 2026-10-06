package javasession

import (
	"bytes"
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/draw"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/player/skin"
	jserver "github.com/ezchr/go-mcjava/server"
	"github.com/google/uuid"
)

//go:embed skins/steve.png skins/geo.json
var skinFS embed.FS

// humanoidGeometry is the classic player model (geometry.humanoid.custom and customSlim), from
// Geyser (MIT): Bedrock clients only draw a skin a server sends with its geometry data, not
// with the model name alone (Dragonfly sends "{}" for a skin without a model).
var humanoidGeometry, _ = skinFS.ReadFile("skins/geo.json")

// Bedrock players see a Java player with the skin their Java profile names (downloaded from
// Mojang's texture server), or Steve. A Dragonfly skin with no pixels would make them invisible.

var (
	skinCache  sync.Map // texture URL -> skin.Skin
	skinClient = &http.Client{Timeout: 4 * time.Second}
)

// javaSkin returns the Bedrock skin for a Java profile.
func javaSkin(props []jserver.Property) skin.Skin {
	texURL, slim := textureURL(props)
	if texURL != "" {
		if v, ok := skinCache.Load(texURL); ok {
			return v.(skin.Skin)
		}
		if img, err := fetchTexture(texURL); err == nil {
			s := bedrockSkin(img, slim)
			skinCache.Store(texURL, s)
			return s
		}
	}
	return steveSkin()
}

// textureURL reads the skin URL and model from a signed "textures" property.
func textureURL(props []jserver.Property) (string, bool) {
	for _, p := range props {
		if p.Name != "textures" {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(p.Value)
		if err != nil {
			return "", false
		}
		var t struct {
			Textures struct {
				Skin struct {
					URL      string `json:"url"`
					Metadata struct {
						Model string `json:"model"`
					} `json:"metadata"`
				} `json:"SKIN"`
			} `json:"textures"`
		}
		if json.Unmarshal(raw, &t) != nil {
			return "", false
		}
		return t.Textures.Skin.URL, t.Textures.Skin.Metadata.Model == "slim"
	}
	return "", false
}

// fetchTexture downloads a skin PNG, only from Mojang's texture server.
func fetchTexture(raw string) (image.Image, error) {
	u, err := url.Parse(raw)
	if err != nil || !strings.HasSuffix(u.Hostname(), "textures.minecraft.net") {
		return nil, io.ErrUnexpectedEOF
	}
	u.Scheme = "https"
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	resp, err := skinClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, io.ErrUnexpectedEOF
	}
	return png.Decode(io.LimitReader(resp.Body, 1<<20))
}

var (
	steveOnce sync.Once
	steve     skin.Skin
)

func steveSkin() skin.Skin {
	steveOnce.Do(func() {
		b, _ := skinFS.ReadFile("skins/steve.png")
		img, err := png.Decode(bytes.NewReader(b))
		if err != nil {
			panic("javasession: embedded steve.png: " + err.Error())
		}
		steve = bedrockSkin(img, false)
	})
	return steve
}

// bedrockSkin turns a 64x64 (or legacy 64x32) Java skin texture into a Dragonfly skin.
func bedrockSkin(img image.Image, slim bool) skin.Skin {
	s := skin.New(64, 64)
	rgba := image.NewRGBA(image.Rect(0, 0, 64, 64))
	draw.Draw(rgba, img.Bounds().Intersect(rgba.Bounds()), img, img.Bounds().Min, draw.Src)
	copy(s.Pix, rgba.Pix)
	s.ArmSize = "wide"
	s.ModelConfig = skin.ModelConfig{Default: "geometry.humanoid.custom"}
	if slim {
		s.ArmSize = "slim"
		s.ModelConfig = skin.ModelConfig{Default: "geometry.humanoid.customSlim"}
	}
	s.Model = humanoidGeometry
	s.Premium = true // as Geyser sends Java skins
	id := uuid.New().String()
	s.SkinID = id
	s.FullID = id
	return s
}
