package ytm

import (
	"errors"
	"testing"
)

// TestLoginKind comprueba cómo se clasifican los errores de yt-dlp que piden
// sesión: el de bot (de la red) aparte del de edad (del video).
func TestLoginKind(t *testing.T) {
	cases := map[string]error{
		"ERROR: [youtube] BTjQsshhPtM: Sign in to confirm you’re not a bot. Use --cookies": ErrBot,
		"ERROR: [youtube] x: Sign in to confirm your age. This video may be inappropriate": ErrLogin,
		"ERROR: [youtube] x: Join this channel to get access to members-only content":      ErrLogin,
		"ERROR: [youtube] x: Requested format is not available":                            nil,
	}
	for msg, want := range cases {
		if got := loginKind(msg); !errors.Is(got, want) || (want == nil) != (got == nil) {
			t.Errorf("loginKind(%q) = %v, want %v", msg, got, want)
		}
	}
}

// TestNormTitle comprueba la comparación de títulos de las otras subidas.
func TestNormTitle(t *testing.T) {
	if a, b := normTitle("Backdrifts"), normTitle("Radiohead - Backdrifts (Honeymoon Is Over) [HD]"); a != "backdrifts" || b != "radioheadbackdrifts" {
		t.Errorf("normTitle: %q, %q", a, b)
	}
	if d := parseClock("5:23"); d != 323 {
		t.Errorf("parseClock = %v", d)
	}
}
