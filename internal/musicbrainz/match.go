package musicbrainz

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// LocalTrack es una canción de la biblioteca, para emparejarla con una
// pista de una edición.
type LocalTrack struct {
	Path     string
	Title    string
	Disc     int     // 0 si no se sabe
	Number   int     // 0 si no se sabe
	Duration float64 // segundos (0 si no se sabe)
}

// TrackMatch es una canción de la biblioteca y la pista que le toca.
type TrackMatch struct {
	Local int    // su índice en la lista de canciones
	Track *Track // nil si no se encontró ninguna
	Score float64
}

// MatchAlbum empareja las canciones de un álbum de la biblioteca con las
// pistas de una edición: por el título (lo que más pesa), el número de
// pista y la duración. Cada pista se usa una vez, empezando por las parejas
// más seguras. Devuelve una por canción, en su orden, y la confianza media
// (0..1) del emparejamiento.
func MatchAlbum(tracks []LocalTrack, rel Release) ([]TrackMatch, float64) {
	all := rel.Tracks()
	type pair struct {
		l, t  int
		score float64
	}
	var pairs []pair
	for li, l := range tracks {
		for ti, t := range all {
			pairs = append(pairs, pair{li, ti, pairScore(l, t)})
		}
	}
	sort.SliceStable(pairs, func(i, j int) bool { return pairs[i].score > pairs[j].score })
	out := make([]TrackMatch, len(tracks))
	for i := range out {
		out[i].Local = i
	}
	usedL, usedT := map[int]bool{}, map[int]bool{}
	for _, p := range pairs {
		if usedL[p.l] || usedT[p.t] || p.score < 0.35 {
			continue
		}
		usedL[p.l], usedT[p.t] = true, true
		t := all[p.t]
		out[p.l].Track, out[p.l].Score = &t, p.score
	}
	sum := 0.0
	for _, m := range out {
		sum += m.Score
	}
	conf := 0.0
	if len(out) > 0 {
		conf = sum / float64(len(out))
	}
	return out, conf
}

// pairScore es cuánto se parecen una canción y una pista (0..1): el título
// pesa 0,6, el número 0,25 y la duración 0,15; lo que no se sabe no cuenta
// y el resto se reparte su peso.
func pairScore(l LocalTrack, t Track) float64 {
	score, weight := 0.6*similarity(l.Title, t.Title), 0.6
	if l.Number > 0 {
		weight += 0.25
		disc := l.Disc
		if disc == 0 {
			disc = 1
		}
		if l.Number == t.Number && disc == t.Disc {
			score += 0.25
		} else if l.Number == t.Number {
			score += 0.1
		}
	}
	if l.Duration > 0 && t.Length > 0 {
		weight += 0.15
		d := math.Abs(l.Duration - t.Length.Seconds())
		// Hasta 3 s es la misma; a partir de 30 s, nada.
		score += 0.15 * math.Max(0, math.Min(1, (30-d)/27))
	}
	return score / weight
}

// fold deja un título para comparar: minúsculas, sin tildes ni signos, y
// sin lo que va entre paréntesis o corchetes (las "remastered", "feat."...).
func fold(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range strings.ToLower(s) {
		if f, ok := unaccent[r]; ok {
			r = f
		}
		switch {
		case r == '(' || r == '[':
			depth++
		case r == ')' || r == ']':
			depth = max(0, depth-1)
		case depth > 0, unicode.Is(unicode.Mn, r):
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// unaccent pasa las letras con tilde más comunes a la letra sin ella (para
// comparar "Pokémon" con "Pokemon" sin depender de una tabla Unicode
// entera).
var unaccent = func() map[rune]rune {
	m := map[rune]rune{}
	for base, accented := range map[rune]string{
		'a': "àáâãäåāăą", 'c': "çćĉċč", 'd': "ďđ", 'e': "èéêëēĕėęě", 'g': "ĝğġģ", 'h': "ĥħ",
		'i': "ìíîïĩīĭįı", 'j': "ĵ", 'k': "ķ", 'l': "ĺļľŀł", 'n': "ñńņňŉ", 'o': "òóôõöøōŏő",
		'r': "ŕŗř", 's': "śŝşšș", 't': "ţťŧț", 'u': "ùúûüũūŭůűų", 'w': "ŵ", 'y': "ýÿŷ", 'z': "źżž",
	} {
		for _, r := range accented {
			m[r] = base
		}
	}
	return m
}()

// similarity es cuánto se parecen dos títulos (0..1): 1 menos su distancia
// de edición relativa, una vez plegados.
func similarity(a, b string) float64 {
	a, b = fold(a), fold(b)
	if a == "" && b == "" {
		return 1
	}
	if a == "" || b == "" {
		return 0
	}
	if a == b {
		return 1
	}
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			c := 1
			if ra[i-1] == rb[j-1] {
				c = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+c)
		}
		prev, cur = cur, prev
	}
	return 1 - float64(prev[len(rb)])/float64(max(len(ra), len(rb)))
}
