package library

import (
	"math"
	"testing"
)

// TestReadGain comprueba ReplayGain en FLAC (Vorbis), MP3 (TXXX de ID3) y
// Opus (R128, en 1/256 dB y +5 dB), y que un archivo sin etiquetas no tenga.
func TestReadGain(t *testing.T) {
	near := func(a, b float64) bool { return math.Abs(a-b) < 0.01 }
	g, ok := ReadGain("testdata/rg.flac")
	if !ok || !near(g.Track, -6.54) || !near(g.Album, -7.10) || !near(g.TrackPeak, 0.988) || !g.HasAlbum {
		t.Errorf("FLAC gain = %+v, %v", g, ok)
	}
	g, ok = ReadGain("testdata/rg.mp3")
	if !ok || !near(g.Track, 2.30) || !near(g.Album, -1) {
		t.Errorf("MP3 gain = %+v, %v", g, ok)
	}
	g, ok = ReadGain("testdata/rg.opus")
	if !ok || !near(g.Track, 0) || g.HasAlbum {
		t.Errorf("Opus gain = %+v, %v (want -1280/256+5 = 0 dB)", g, ok)
	}
	if _, ok := ReadGain("gain.go"); ok {
		t.Error("a file without tags has no gain")
	}
}

// TestGainFields comprueba los formatos raros de los valores: coma decimal,
// signo +, y los bytes de cabecera que traen los "----" de MP4.
func TestGainFields(t *testing.T) {
	g, ok := gainFromFields(map[string]string{"replaygain_track_gain": "\x00\x00\x00\x00-3,5 dB", "replaygain_album_peak": "+1.02"})
	if !ok || g.Track != -3.5 || g.AlbumPeak != 1.02 || g.HasAlbum {
		t.Errorf("gain = %+v, %v", g, ok)
	}
}
