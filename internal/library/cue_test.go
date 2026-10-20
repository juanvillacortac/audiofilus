package library

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/decode"
)

// sheet es una hoja CUE como las de EAC: REM, CRLF, comillas, pausas
// previas (INDEX 00), una pista de datos y un título en Windows-1252.
var sheet = "\xef\xbb\xbfREM GENRE Rock\r\n" +
	"REM DATE 1999\r\n" +
	"REM REPLAYGAIN_ALBUM_GAIN -7.50 dB\r\n" +
	"REM REPLAYGAIN_ALBUM_PEAK 0.988\r\n" +
	"PERFORMER \"The Band\"\r\n" +
	"TITLE \"Live Album\"\r\n" +
	"FILE \"Live Album.wav\" WAVE\r\n" +
	"  TRACK 01 AUDIO\r\n" +
	"    TITLE \"Intro\"\r\n" +
	"    INDEX 01 00:00:00\r\n" +
	"  TRACK 02 AUDIO\r\n" +
	"    TITLE \"Canci\xf3n de cuna\"\r\n" +
	"    PERFORMER \"Guest\"\r\n" +
	"    REM REPLAYGAIN_TRACK_GAIN -6.20 dB\r\n" +
	"    INDEX 00 01:58:00\r\n" +
	"    INDEX 01 02:00:37\r\n" +
	"  TRACK 03 AUDIO\r\n" +
	"    TITLE Outro\r\n" +
	"    INDEX 01 05:10:00\r\n" +
	"FILE bonus.wav WAVE\r\n" +
	"  TRACK 04 AUDIO\r\n" +
	"    TITLE \"Bonus\"\r\n" +
	"    INDEX 01 00:00:00\r\n" +
	"  TRACK 05 MODE1/2352\r\n" +
	"    INDEX 01 00:30:00\r\n"

// TestParseCue comprueba los datos del álbum, los inicios y finales de cada
// pista (sin contar la pausa previa), el intérprete heredado, el texto en
// Windows-1252 y que la pista de datos se descarte.
func TestParseCue(t *testing.T) {
	c, err := ParseCue(bytes.NewReader([]byte(sheet)))
	if err != nil {
		t.Fatal(err)
	}
	if c.Title != "Live Album" || c.Performer != "The Band" || c.Genre != "Rock" || c.Date != "1999" {
		t.Errorf("album = %+v", c)
	}
	if !c.Gain.OK || c.Gain.DB != -7.5 || c.Gain.Peak != 0.988 {
		t.Errorf("album gain = %+v", c.Gain)
	}
	if len(c.Tracks) != 4 {
		t.Fatalf("got %d tracks, want 4: %+v", len(c.Tracks), c.Tracks)
	}
	t2 := c.Tracks[1]
	if t2.Title != "Canción de cuna" || t2.Performer != "Guest" {
		t.Errorf("track 2 = %q by %q", t2.Title, t2.Performer)
	}
	if want := 2*time.Minute + 37*time.Second/75; t2.Start != want {
		t.Errorf("track 2 starts at %v, want %v (INDEX 01, not the pregap)", t2.Start, want)
	}
	if c.Tracks[0].End != t2.Start || t2.End != 5*time.Minute+10*time.Second || c.Tracks[2].End != 0 {
		t.Errorf("ends = %v %v %v", c.Tracks[0].End, t2.End, c.Tracks[2].End)
	}
	if !t2.Gain.OK || t2.Gain.DB != -6.2 || c.Tracks[0].Gain.OK {
		t.Errorf("track gains = %+v / %+v", t2.Gain, c.Tracks[0].Gain)
	}
	if c.Tracks[0].Performer != "The Band" || c.Tracks[3].File != "bonus.wav" || c.Tracks[3].Title != "Bonus" {
		t.Errorf("tracks = %+v", c.Tracks)
	}
}

// TestReadCueResolvesFiles comprueba que la hoja encuentre su audio aunque
// diga .wav y el archivo sea .flac, sin distinguir mayúsculas, y que las
// pistas de un archivo que no está se descarten.
func TestReadCueResolvesFiles(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"Live Album.flac", "BONUS.WAV"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cuePath := filepath.Join(dir, "Live Album.cue")
	if err := os.WriteFile(cuePath, []byte(sheet), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := ReadCue(cuePath)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Tracks[0].File; got != filepath.Join(dir, "Live Album.flac") {
		t.Errorf("track 1 file = %q", got)
	}
	if got := c.Tracks[3].File; got != filepath.Join(dir, "BONUS.WAV") {
		t.Errorf("track 4 file = %q", got)
	}
	os.Remove(filepath.Join(dir, "BONUS.WAV"))
	if c, err = ReadCue(cuePath); err != nil || len(c.Tracks) != 3 {
		t.Errorf("without bonus.wav: %d tracks, %v; want 3", len(c.Tracks), err)
	}
}

// TestCuePathRoundTrip comprueba que la ruta de una pista CUE se lea igual
// que se escribió.
func TestCuePathRoundTrip(t *testing.T) {
	tr := CueTrack{File: "/m/a#b.flac", Start: 2*time.Minute + 37*time.Second/75, End: 310 * time.Second}
	file, start, end, ok := decode.SplitCuePath(tr.Path())
	if !ok || file != tr.File || start != tr.Start.Truncate(time.Millisecond) || end != tr.End {
		t.Errorf("SplitCuePath(%q) = %q %v %v %v", tr.Path(), file, start, end, ok)
	}
}

// TestReadCueTrack comprueba que una pista CUE recupere sus etiquetas de la
// hoja de su carpeta.
func TestReadCueTrack(t *testing.T) {
	dir := t.TempDir()
	audio := filepath.Join(dir, "Live Album.flac")
	_ = os.WriteFile(audio, []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "Live Album.cue"), []byte(sheet), 0o644)
	c, err := ReadCue(filepath.Join(dir, "Live Album.cue"))
	if err != nil {
		t.Fatal(err)
	}
	tr, ok := ReadCueTrack(c.Tracks[1].Path())
	if !ok || tr.Title != "Canción de cuna" || tr.Artist != "Guest" || tr.Album != "Live Album" {
		t.Errorf("ReadCueTrack = %+v, %v", tr, ok)
	}
	if _, ok := ReadCueTrack(audio); ok {
		t.Error("a plain file is not a CUE track")
	}
}

// TestScanWithCue comprueba que al escanear una carpeta con un disco entero
// en un archivo y su hoja CUE salgan las pistas de la hoja (con sus
// títulos) en vez del archivo, y que Expand de la hoja dé lo mismo.
func TestScanWithCue(t *testing.T) {
	dir := t.TempDir()
	src, err := os.ReadFile("../decode/testdata/tone.flac")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "album.flac"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.flac"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	cue := "TITLE \"Live\"\nPERFORMER \"Band\"\nFILE \"album.wav\" WAVE\n  TRACK 01 AUDIO\n    TITLE \"One\"\n    INDEX 01 00:00:00\n  TRACK 02 AUDIO\n    TITLE \"Two\"\n    INDEX 01 00:01:00\n"
	if err := os.WriteFile(filepath.Join(dir, "album.cue"), []byte(cue), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := Expand([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range files {
		tr := ReadTrack(f)
		names = append(names, tr.Title)
	}
	if strings.Join(names, "|") != "One|Two|Prueba flac" {
		t.Errorf("scanned titles = %q, want One|Two|Prueba flac (other.flac keeps its tags)", names)
	}
	sheet, err := Expand([]string{filepath.Join(dir, "album.cue")})
	if err != nil || len(sheet) != 2 || sheet[0] != files[0] {
		t.Errorf("Expand(cue) = %q, %v", sheet, err)
	}
}
