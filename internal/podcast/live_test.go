package podcast

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestPodcastLive busca, saca los populares de Venezuela y lee un RSS (solo
// con AUDIOFILUS_NET_TESTS=1: necesita red).
func TestPodcastLive(t *testing.T) {
	if os.Getenv("AUDIOFILUS_NET_TESTS") == "" {
		t.Skip("set AUDIOFILUS_NET_TESTS=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	top, err := Top(ctx, "VE")
	if err != nil || len(top) == 0 {
		t.Fatalf("top: %v (%d)", err, len(top))
	}
	t.Logf("top VE: %d, first %q by %q", len(top), top[0].Title, top[0].Author)
	res, err := Search(ctx, "nadie sabe nada", "ES")
	if err != nil || len(res) == 0 {
		t.Fatalf("search: %v", err)
	}
	show, eps, err := Fetch(ctx, res[0].Feed)
	if err != nil || len(eps) == 0 {
		t.Fatalf("feed: %v (%d)", err, len(eps))
	}
	e := eps[0]
	t.Logf("%q: %d episodes; newest %q %s %ds %s", show.Title, len(eps), e.Title, e.Date.Format("2006-01-02"), e.Duration, e.Ext())
	if e.Date.IsZero() {
		t.Error("no date")
	}
}
