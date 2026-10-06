package javasession

import (
	"testing"

	jserver "github.com/ezchr/go-mcjava/server"
)

// The XUID for a name must match ViaBedrock's (Fnv1.fnv1_64 then Math.abs), and the UUID the
// Bedrock "pocket-auth-1-xuid:" scheme. The expected values come from the formula written out
// by hand with Java semantics.
func TestViaBedrockIdentity(t *testing.T) {
	// FNV-1 64 of "Notch": offset basis, then for each byte multiply by the prime and xor.
	var h uint64 = 0xcbf29ce484222325
	for _, b := range []byte("Notch") {
		h *= 0x100000001b3
		h ^= uint64(b)
	}
	v := int64(h)
	if v < 0 {
		v = -v
	}
	_, xuid := ViaBedrockIdentity(jserver.Profile{Name: "Notch"})
	if want := itoa(v); xuid != want {
		t.Fatalf("xuid %s, want %s", xuid, want)
	}
	id, _ := ViaBedrockIdentity(jserver.Profile{Name: "Notch"})
	if id.Version() != 3 {
		t.Fatalf("uuid %s is not version 3", id)
	}
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	var b []byte
	for v != 0 {
		d := v % 10
		if d < 0 {
			d = -d
		}
		b = append([]byte{byte('0' + d)}, b...)
		v /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
