package library

import (
	"os"
	"strconv"
	"strings"

	"github.com/dhowden/tag"
)

// Gain es la ganancia ReplayGain de una canción (sus etiquetas
// REPLAYGAIN_*): cuánto subir o bajar para que todas suenen igual de
// fuerte, por canción y por álbum, y sus picos (para no recortar).
type Gain struct {
	Track, Album         float64 // dB
	TrackPeak, AlbumPeak float64 // 1 = el máximo; 0 si no se sabe
	HasTrack, HasAlbum   bool
}

// ReadGain lee las etiquetas ReplayGain de path: los campos de Vorbis y
// FLAC (REPLAYGAIN_TRACK_GAIN...), los TXXX de ID3, los "----" de MP4, y en
// Opus las R128_TRACK_GAIN/R128_ALBUM_GAIN (en 1/256 dB, respecto a -23
// LUFS: se les suman 5 dB para llevarlas a la referencia de ReplayGain).
// ok es false si no tiene ninguna.
func ReadGain(path string) (Gain, bool) {
	f, err := os.Open(path)
	if err != nil {
		return Gain{}, false
	}
	defer f.Close()
	m, err := tag.ReadFrom(f)
	if err != nil {
		return Gain{}, false
	}
	fields := map[string]string{}
	for k, v := range m.Raw() {
		key := strings.ToLower(k)
		switch v := v.(type) {
		case string:
			fields[key] = v
		case []string:
			if len(v) > 0 {
				fields[key] = v[0]
			}
		case *tag.Comm:
			// TXXX de ID3: el nombre va en la descripción.
			fields[strings.ToLower(v.Description)] = v.Text
		}
	}
	return gainFromFields(fields)
}

// gainFromFields saca la ganancia de los campos (con los nombres en
// minúsculas).
func gainFromFields(fields map[string]string) (Gain, bool) {
	var g Gain
	if v, ok := parseDB(fields["replaygain_track_gain"]); ok {
		g.Track, g.HasTrack = v, true
	} else if v, ok := parseR128(fields["r128_track_gain"]); ok {
		g.Track, g.HasTrack = v, true
	}
	if v, ok := parseDB(fields["replaygain_album_gain"]); ok {
		g.Album, g.HasAlbum = v, true
	} else if v, ok := parseR128(fields["r128_album_gain"]); ok {
		g.Album, g.HasAlbum = v, true
	}
	g.TrackPeak, _ = parseNum(fields["replaygain_track_peak"])
	g.AlbumPeak, _ = parseNum(fields["replaygain_album_peak"])
	return g, g.HasTrack || g.HasAlbum
}

// parseDB lee "-6.54 dB" (o "-6,54", o sin unidad).
func parseDB(s string) (float64, bool) {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(clean(s)), "dB"))
	s = strings.TrimSuffix(strings.TrimSuffix(s, "db"), "DB")
	return parseNum(s)
}

// parseR128 lee una ganancia R128 de Opus (entero en 1/256 dB).
func parseR128(s string) (float64, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(clean(s)))
	if err != nil {
		return 0, false
	}
	return float64(n)/256 + 5, true
}

func parseNum(s string) (float64, bool) {
	s = strings.ReplaceAll(strings.TrimSpace(clean(s)), ",", ".")
	v, err := strconv.ParseFloat(strings.TrimPrefix(s, "+"), 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// clean quita lo que no es texto de los bordes: los valores "----" de MP4
// traen por delante bytes de su cabecera, y algunos TXXX un nulo al final.
func clean(s string) string {
	return strings.TrimFunc(s, func(r rune) bool { return r < 0x20 })
}
