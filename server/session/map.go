package session

import (
	"crypto/sha256"
	"image/color"
	"log/slog"
	"slices"
	"sync"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// MapSize is the width and height of a map's picture in pixels.
const MapSize = 128

// mapPicture is the picture of one map id and the sessions that asked for it.
type mapPicture struct {
	pixels  []color.RGBA
	sum     [32]byte // the key of pixels in sharedPixels
	viewers map[*Session]struct{}
}

// sharedPixel is one picture's pixels, kept once for every map that shows it: walls of item frames
// repeat the same pictures (2237 maps, 555 pictures on pvp), and each copy was 64 KB.
type sharedPixel struct {
	pixels []color.RGBA
	maps   int
}

var (
	mapsMu       sync.Mutex
	mapPictures  = map[int64]*mapPicture{}
	sharedPixels = map[[32]byte]*sharedPixel{}
)

// sharePixels returns the kept copy of pixels, kept for one more map. mapsMu held.
func sharePixels(pixels []color.RGBA) ([]color.RGBA, [32]byte) {
	h := sha256.New()
	buf := make([]byte, 0, len(pixels)*4)
	for _, c := range pixels {
		buf = append(buf, c.R, c.G, c.B, c.A)
	}
	h.Write(buf)
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	if sp, ok := sharedPixels[sum]; ok {
		sp.maps++
		return sp.pixels, sum
	}
	kept := slices.Clone(pixels)
	sharedPixels[sum] = &sharedPixel{pixels: kept, maps: 1}
	return kept, sum
}

// unsharePixels lets go of one map's use of the picture with key sum. mapsMu held.
func unsharePixels(sum [32]byte) {
	if sp, ok := sharedPixels[sum]; ok {
		if sp.maps--; sp.maps <= 0 {
			delete(sharedPixels, sum)
		}
	}
}

// SetMapPixels sets the picture of the map id (item.FilledMap): MapSize*MapSize pixels, row by row.
// Every session that already shows the map gets the new picture.
func SetMapPixels(id int64, pixels []color.RGBA) {
	if len(pixels) != MapSize*MapSize {
		panic("session: a map picture is MapSize*MapSize pixels")
	}
	mapsMu.Lock()
	m, ok := mapPictures[id]
	if !ok {
		m = &mapPicture{viewers: map[*Session]struct{}{}}
		mapPictures[id] = m
	} else if m.pixels != nil {
		unsharePixels(m.sum)
	}
	pixels, m.sum = sharePixels(pixels) // read only from here on: other maps may show the same slice
	m.pixels = pixels
	mapsMu.Unlock()
	// Every Bedrock player gets it, not only those who asked: a client does not always ask for
	// a map in an item frame, and then shows it empty.
	sessions.mu.Lock()
	all := slices.Clone(sessions.s)
	sessions.mu.Unlock()
	for _, s := range all {
		s.writeMap(id, pixels)
	}
	mapListenersMu.Lock()
	listeners := slices.Clone(mapListeners)
	mapListenersMu.Unlock()
	for _, f := range listeners {
		f(id)
	}
}

// RemoveMap forgets the picture of the map id.
func RemoveMap(id int64) {
	mapsMu.Lock()
	if m, ok := mapPictures[id]; ok && m.pixels != nil {
		unsharePixels(m.sum)
	}
	delete(mapPictures, id)
	mapsMu.Unlock()
}

// forgetMaps drops s from the viewers of every map, once it closes.
func forgetMaps(s *Session) {
	mapsMu.Lock()
	for _, m := range mapPictures {
		delete(m.viewers, s)
	}
	mapsMu.Unlock()
}

// writeMap sends the picture of map id as Geyser does: the map lists its own id among the maps it
// is included in (the client shows nothing without it since 1.19.50), and the colours are ABGR,
// which gophertunnel's fixed 4-byte pixels give with red and blue swapped.
func (s *Session) writeMap(id int64, pixels []color.RGBA) {
	px := make([]color.RGBA, len(pixels))
	for i, p := range pixels {
		px[i] = color.RGBA{R: p.B, G: p.G, B: p.R, A: p.A}
	}
	s.writePacket(&packet.ClientBoundMapItemData{
		MapID:          id,
		MapsIncludedIn: protocol.Option([]int64{id}),
		Scale:          protocol.Option(byte(0)),
		Width:          protocol.Option(int32(MapSize)),
		Height:         protocol.Option(int32(MapSize)),
		XOffset:        protocol.Option(int32(0)),
		YOffset:        protocol.Option(int32(0)),
		Pixels:         protocol.Option(px),
	})
}

// MapInfoRequestHandler handles the MapInfoRequest packet, sent when the client first shows a
// map (in a hand or an item frame) and needs its picture.
type MapInfoRequestHandler struct{}

// Handle ...
func (MapInfoRequestHandler) Handle(p packet.Packet, s *Session, _ *world.Tx, _ Controllable) error {
	id := p.(*packet.MapInfoRequest).MapID
	mapsMu.Lock()
	m, ok := mapPictures[id]
	if ok {
		m.viewers[s] = struct{}{}
	}
	mapsMu.Unlock()
	slog.Debug("map info request", "id", id, "known", ok)
	if ok {
		s.writeMap(id, m.pixels)
	}
	return nil
}

var (
	mapListenersMu sync.Mutex
	mapListeners   []func(id int64)
)

// OnMapChange calls f with the id of every map SetMapPixels gives a new picture (for the Java
// side, which pushes pictures instead of being asked for them).
func OnMapChange(f func(id int64)) {
	mapListenersMu.Lock()
	mapListeners = append(mapListeners, f)
	mapListenersMu.Unlock()
}

// MapPixels returns the picture of the map id, if it has one.
func MapPixels(id int64) ([]color.RGBA, bool) {
	mapsMu.Lock()
	defer mapsMu.Unlock()
	m, ok := mapPictures[id]
	if !ok {
		return nil, false
	}
	return m.pixels, true
}
