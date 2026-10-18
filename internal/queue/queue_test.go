package queue

import (
	"slices"
	"testing"
)

var songs = []string{"a", "b", "c", "d"}

// TestLinearAndRepeat comprueba el avance y los tres modos de repetición.
func TestLinearAndRepeat(t *testing.T) {
	q := New()
	q.Set(songs, 0)
	for _, want := range []string{"b", "c", "d"} {
		if !q.Next(true) || q.Current() != want {
			t.Fatalf("Next -> %q, want %q", q.Current(), want)
		}
	}
	if q.Next(true) {
		t.Fatal("Next at the end with repeat off should stop")
	}
	q.Repeat = RepeatAll
	if !q.Next(true) || q.Current() != "a" {
		t.Fatalf("repeat all should wrap to a, got %q", q.Current())
	}
	q.Repeat = RepeatOne
	if !q.Next(true) || q.Current() != "a" {
		t.Fatal("repeat one should stay on the song when it ends")
	}
	if !q.Next(false) || q.Current() != "b" {
		t.Fatal("a manual skip should move on even with repeat one")
	}
}

// TestPrev comprueba ir atrás, con y sin repetición.
func TestPrev(t *testing.T) {
	q := New()
	q.Set(songs, 2)
	if !q.Prev() || q.Current() != "b" {
		t.Fatal("Prev from c should go to b")
	}
	q.Set(songs, 0)
	if q.Prev() {
		t.Fatal("Prev at the start with repeat off should fail")
	}
	q.Repeat = RepeatAll
	if !q.Prev() || q.Current() != "d" {
		t.Fatal("Prev at the start with repeat all should wrap to d")
	}
}

// TestShuffleKeepsCurrentAndAllSongs comprueba que el aleatorio no pierda
// canciones ni cambie la que suena, y que al quitarlo vuelva el orden original.
func TestShuffleKeepsCurrentAndAllSongs(t *testing.T) {
	q := New()
	q.Set(songs, 2)
	q.SetShuffle(true)
	if q.Current() != "c" || q.Index() != 0 {
		t.Fatalf("shuffle should keep c playing first, got %q at %d", q.Current(), q.Index())
	}
	got := q.Tracks()
	slices.Sort(got)
	if !slices.Equal(got, songs) {
		t.Fatalf("shuffled queue lost songs: %v", got)
	}
	q.SetShuffle(false)
	if q.Current() != "c" || !slices.Equal(q.Tracks(), songs) {
		t.Fatalf("unshuffle should restore order and keep c, got %v at %q", q.Tracks(), q.Current())
	}
}

// TestPlayNextSurvivesUnshuffle comprueba que una canción encolada con PlayNext
// siga en su sitio tras activar y quitar el aleatorio.
func TestPlayNextSurvivesUnshuffle(t *testing.T) {
	q := New()
	q.Set(songs, 0)
	q.PlayNext("x")
	if !slices.Equal(q.Tracks(), []string{"a", "x", "b", "c", "d"}) {
		t.Fatalf("PlayNext order: %v", q.Tracks())
	}
	q.SetShuffle(true)
	q.SetShuffle(false)
	if !slices.Equal(q.Tracks(), []string{"a", "x", "b", "c", "d"}) {
		t.Fatalf("after shuffle on/off: %v", q.Tracks())
	}
}
