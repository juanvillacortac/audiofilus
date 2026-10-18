package ytm

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestSearchLive busca de verdad en YouTube Music (solo con
// AUDIOFILUS_NET_TESTS=1: necesita red).
func TestSearchLive(t *testing.T) {
	if os.Getenv("AUDIOFILUS_NET_TESTS") == "" {
		t.Skip("set AUDIOFILUS_NET_TESTS=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	res, err := Search(ctx, "chevelle the red")
	if err != nil || len(res) == 0 {
		t.Fatalf("search: %v (%d results)", err, len(res))
	}
	for _, r := range res[:min(3, len(res))] {
		t.Logf("%s | %s | %s | %s | %s", r.ID, r.Title, r.Artist(), r.Album, r.Duration)
	}
	if res[0].Artist() == "" || res[0].Album == "" || res[0].Duration == "" {
		t.Errorf("incomplete first result: %+v", res[0])
	}
}
