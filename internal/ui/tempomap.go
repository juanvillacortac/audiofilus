package ui

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/decode"
	"github.com/juanvillacortac/audiofilus/internal/engine"
)

// El mapa de tempo de una canción (#14), como el [SyncTrack] de los charts de
// Clone Hero o los timing points de osu!: dónde cae cada golpe, en segundos de
// la canción. El tempo que se adivina mientras suena (tempo.go) duda los
// primeros segundos y no sigue a una banda que acelera; el mapa sale de
// analizar el archivo entero antes (más rápido que en tiempo real, en segundo
// plano), así que la rejilla de los juegos es buena desde el principio y sigue
// los cambios. Se guarda en data/tempo, uno por canción. Lo que no se puede
// analizar entero (la radio, un servidor, una canción de consola sin fin, el
// tracker) sigue con el tempo en vivo.

const (
	tempoMapVersion = 4       // súbela si cambia el análisis: los mapas guardados se rehacen
	tempoMapMaxSecs = 20 * 60 // más larga (una mezcla, un audiolibro): no se analiza
	tempoMapStep    = 2.0     // segundos entre las ventanas que siguen el tempo local
	tempoMapDrift   = 0.08    // cuánto puede alejarse el tempo local del global
	tempoMapTight   = 100.0   // lo que cuesta un golpe fuera de tempo (la "tightness" de Ellis)
	kickBands       = 3       // las bandas del bombo (40–123 Hz), las que deciden la fase
	fixPhaseSpan    = 16      // golpes por tramo al decidir la fase (cuatro compases)
	chartSnap       = 0.07    // cuánto puede alejarse un golpe de su corchea para ir al chart
)

// tempoMap son los golpes de una canción.
type tempoMap struct {
	Version int         `json:"v"`
	Size    int64       `json:"size"`            // del archivo, para saber si cambió
	Mod     int64       `json:"mod"`             // su fecha (Unix)
	Beats   []float64   `json:"beats"`           // en segundos de la canción; vacío: sin ritmo claro
	Notes   []chartNote `json:"notes,omitempty"` // la batería, una nota por corchea (el chart de Ritmo)
	// Pos es el lugar de cada golpe en su compás (0: el primero). Los
	// compases son de 3 o 4 golpes según la canción (meterOf), y en una
	// sección que se siente con otro pulso del mismo compás (la intro de 3
	// Libras: 2 golpes a 96 en vez de 3 a 144), de los de esa sección.
	Pos []int `json:"pos,omitempty"`
}

// chartNote es un golpe de batería del chart: en qué segundo de la canción,
// qué instrumento (drumKick...) y en qué corchea de su compás (desde 0, dos
// por golpe; como beat.slot con mapBeat).
type chartNote struct {
	T float64 `json:"t"`
	K int     `json:"k"`
	S int     `json:"s"`
}

// tempoMapState es el mapa de lo que suena: el de path, cuando esté listo.
type tempoMapState struct {
	path   string
	ready  atomic.Pointer[tempoMap] // nil mientras se analiza (o si falló)
	cancel context.CancelFunc       // corta el análisis (otra canción)
}

// at es la rejilla en el segundo pos de la canción: el periodo (la mediana
// de los intervalos de alrededor: no tiembla, y no lo deforma el medio golpe
// que deja fixPhase donde corrige la fase) y el último golpe. ok es false si
// el mapa no tiene golpes.
func (m *tempoMap) at(pos float64) (per, beat float64, ok bool) {
	n := len(m.Beats)
	if n < 2 {
		return 0, 0, false
	}
	i := sort.Search(n, func(i int) bool { return m.Beats[i] > pos }) - 1 // el último golpe hasta pos
	lo, hi := max(i-2, 0), min(i+3, n-1)
	if hi-lo < 1 {
		lo, hi = n-2, n-1
	}
	ivs := make([]float64, 0, hi-lo)
	for j := lo; j < hi; j++ {
		ivs = append(ivs, m.Beats[j+1]-m.Beats[j])
	}
	sort.Float64s(ivs)
	per = ivs[len(ivs)/2]
	switch {
	case i < 0: // antes del primero: hacia atrás con el mismo paso
		k := math.Ceil((m.Beats[0] - pos) / per)
		return per, m.Beats[0] - k*per, true
	case i >= n-1: // después del último: sigue con el último paso
		k := math.Floor((pos - m.Beats[n-1]) / per)
		return per, m.Beats[n-1] + k*per, true
	}
	return per, m.Beats[i], true
}

// tempoMapFile es dónde se guarda el mapa de path.
func tempoMapFile(data, path string) string {
	h := sha1.Sum([]byte(path))
	return filepath.Join(data, "tempo", hex.EncodeToString(h[:10])+".json")
}

// audioStat es el tamaño y la fecha del archivo de audio de path (el de una
// pista CUE o una canción de libgme es el archivo que la contiene).
func audioStat(path string) (size, mod int64, err error) {
	if file, _, _, ok := decode.SplitCuePath(path); ok {
		path = file
	} else if file, _, ok := decode.SplitGMEPath(path); ok {
		path = file
	}
	st, err := os.Stat(path)
	if err != nil {
		st, err = os.Stat(decode.FindFile(path))
	}
	if err != nil {
		return 0, 0, err
	}
	return st.Size(), st.ModTime().Unix(), nil
}

// tempoMapDelay es cuánto tiene que sonar una canción antes de analizarla:
// pasando canciones, cada una lanzaba un análisis de la canción entera, se
// juntaban varios y la consola se arrastraba.
const tempoMapDelay = 3 * time.Second

// analyzeMu deja analizar una canción a la vez: el análisis de la anterior se
// cancela al cambiar, pero tarda un trozo en enterarse.
var analyzeMu sync.Mutex

// loadTempoMap es el mapa de path: el guardado si sigue valiendo, o uno
// nuevo (que se guarda) si la canción sigue sonando tras tempoMapDelay. Tarda:
// va en una goroutine, y ctx lo corta (otra canción).
func loadTempoMap(ctx context.Context, data, path string) (*tempoMap, error) {
	size, mod, err := audioStat(path)
	if err != nil {
		return nil, err
	}
	file := tempoMapFile(data, path)
	if b, err := os.ReadFile(file); err == nil {
		var m tempoMap
		if json.Unmarshal(b, &m) == nil && m.Version == tempoMapVersion && m.Size == size && m.Mod == mod {
			return &m, nil
		}
	}
	select {
	case <-time.After(tempoMapDelay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	analyzeMu.Lock()
	defer analyzeMu.Unlock()
	start := time.Now()
	beats, pos, notes, err := analyzeTempo(ctx, path)
	if err != nil {
		return nil, err
	}
	m := &tempoMap{Version: tempoMapVersion, Size: size, Mod: mod, Beats: beats, Notes: notes, Pos: pos}
	log.Printf("tempo map: %s: %d beats, %d drum notes in %v", filepath.Base(path), len(beats), len(notes), time.Since(start).Round(time.Millisecond))
	if b, err := json.Marshal(m); err == nil {
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err == nil {
			err = os.WriteFile(file, b, 0o644)
		}
		if err != nil {
			log.Printf("tempo map: save: %v", err)
		}
	}
	return m, nil
}

// errTempoMapLength: la canción no tiene final (libgme) o es demasiado larga.
var errTempoMapLength = errors.New("length unknown or too long for a tempo map")

// analyzeTempo encuentra los golpes del archivo path y su batería: lo mide
// entero con el mismo medidor de la música en vivo, busca en el flujo los
// golpes (tempoBeats) y en cada banda, la batería (drumChart).
func analyzeTempo(ctx context.Context, path string) (beats []float64, pos []int, notes []chartNote, err error) {
	src, err := decode.Open(path)
	if err != nil {
		return nil, nil, nil, err
	}
	defer src.Close()
	rate := src.SampleRate()
	if l := src.Length(); l <= 0 || float64(l)/float64(rate) > tempoMapMaxSecs {
		return nil, nil, nil, errTempoMapLength
	}
	an := engine.NewAnalyzer(rate, 0)
	an.NoKey()
	buf := make([]float32, 2048) // una ventana del medidor (1024 frames): un punto
	var env, kick []envPoint
	var rises [][engine.MeterBands]float64 // cuánto subió cada banda en cada punto, para la batería
	var prev [engine.MeterBands]float64
	epoch := time.Unix(0, 0)
	for k := 0; ; k++ {
		if k%256 == 0 && ctx.Err() != nil { // cada ~6 s de canción
			return nil, nil, nil, ctx.Err()
		}
		n := 0
		for n < len(buf) {
			got, err := src.Read(buf[n:])
			n += got
			if err != nil {
				if !errors.Is(err, io.EOF) && got == 0 {
					return nil, nil, nil, err
				}
				break
			}
		}
		if n < len(buf) {
			break
		}
		an.AddAt(buf, epoch)
		p := an.Last()
		// El flujo de todas las bandas, como el del tempo en vivo (meter.go),
		// y el de las del bombo, para la fase (fixPhase).
		var f, low float64
		var rise [engine.MeterBands]float64
		for i := 0; i < len(p.Bands) && i < engine.MeterBands; i++ {
			v := float64(p.Bands[i]) / 255
			if k > 0 {
				rise[i] = math.Max(0, v-prev[i])
				f += rise[i]
				if i < kickBands {
					low += rise[i]
				}
			}
			prev[i] = v
		}
		rises = append(rises, rise)
		t := (float64(k) + 0.5) * 1024 / float64(rate) // el centro de la ventana: ahí cae, de media, lo que subió
		env = append(env, envPoint{t, f})
		kick = append(kick, envPoint{t, low})
	}
	times := make([]float64, len(env))
	for j, e := range env {
		times[j] = e.t
	}
	beats, bpb := tempoBeats(pickTempoEnv(times, rises))
	fixPhase(beats, kick)
	pos = barPositions(bpb)
	return beats, pos, drumChart(beats, pos, env, rises), nil
}

// barPositions es el lugar de cada golpe en su compás, de los golpes por
// compás de cada uno (bpb): se cuentan desde el primero, y donde cambia el
// pulso (otra sección) empieza un compás, pero solo al acabar el que va: un
// compás no se corta a medias.
func barPositions(bpb []int) []int {
	pos := make([]int, len(bpb))
	c, cur := 0, 0
	for i, n := range bpb {
		if c == 0 {
			cur = n // un compás nuevo: con el pulso de su sección
		}
		pos[i] = c
		c = (c + 1) % cur
	}
	return pos
}

// drumChart es la batería de la canción para Ritmo: los golpes que reconoce
// el detector de la batería en vivo (drumDetector), cada uno en la corchea de
// la rejilla del mapa más cercana (si cae a menos de chartSnap), y en cada
// corchea uno solo, como en vivo: el bombo, si no la caja, el platillo o el
// charles. pos es el lugar de cada golpe en su compás (para la corchea de
// cada nota), env da la hora de cada punto y rises, cuánto subió cada banda.
func drumChart(beats []float64, pos []int, env []envPoint, rises [][engine.MeterBands]float64) []chartNote {
	if len(beats) < 2 {
		return nil
	}
	var det drumDetector
	got := map[int][drumCount]bool{} // por corchea de la canción (2 por golpe)
	for j, e := range env {
		for _, k := range det.feed(e.t, rises[j], true) {
			i := sort.Search(len(beats), func(i int) bool { return beats[i] > e.t }) - 1
			if i < 0 || i >= len(beats)-1 {
				continue // antes del primer golpe o después del último
			}
			mid := (beats[i] + beats[i+1]) / 2
			best, d := 2*i, math.Abs(e.t-beats[i])
			if dm := math.Abs(e.t - mid); dm < d {
				best, d = 2*i+1, dm
			}
			if dn := math.Abs(e.t - beats[i+1]); dn < d {
				best, d = 2*i+2, dn
			}
			if d > chartSnap {
				continue // entre corcheas: un redoble o un adorno, no va al chart
			}
			h := got[best]
			h[k] = true
			got[best] = h
		}
	}
	eighths := make([]int, 0, len(got))
	for e := range got {
		eighths = append(eighths, e)
	}
	sort.Ints(eighths)
	notes := make([]chartNote, 0, len(eighths))
	for _, e := range eighths {
		t := beats[e/2]
		if e%2 == 1 {
			t = (beats[e/2] + beats[e/2+1]) / 2
		}
		for _, k := range []int{drumKick, drumSnare, drumCrash, drumHat} {
			if got[e][k] {
				notes = append(notes, chartNote{T: math.Round(t*1000) / 1000, K: k, S: 2*pos[e/2] + e%2})
				break
			}
		}
	}
	return notes
}

// fixPhase pone los golpes en el bombo, tramo a tramo: con todas las bandas,
// en house (bajo y charles a contratiempo) o en rock (la guitarra entre
// golpes) la serie puede quedar a contratiempo, medio golpe corrida. En cada
// tramo de fixPhaseSpan golpes se compara cuánto suben los graves en los
// golpes y en los puntos medios, y si ganan los medios, se corren ahí.
func fixPhase(beats []float64, kick []envPoint) {
	if len(beats) < 4 || len(kick) == 0 {
		return
	}
	// La subida de los graves cerca de t (la ventana del medidor y una más:
	// un golpe cae en una u otra).
	near := func(t float64) float64 {
		i := sort.Search(len(kick), func(i int) bool { return kick[i].t >= t-0.03 })
		var s float64
		for ; i < len(kick) && kick[i].t <= t+0.05; i++ {
			s = math.Max(s, kick[i].f)
		}
		return s
	}
	for lo := 0; lo < len(beats)-1; lo += fixPhaseSpan {
		hi := min(lo+fixPhaseSpan, len(beats)-1)
		var on, off float64
		for i := lo; i < hi; i++ {
			on += near(beats[i])
			off += near((beats[i] + beats[i+1]) / 2)
		}
		if off <= on*1.15 { // en duda, se queda
			continue
		}
		for i := lo; i < hi; i++ {
			beats[i] = (beats[i] + beats[i+1]) / 2
		}
	}
}

// tempoWin es una ventana de tempoWindow segundos del flujo: su centro y su
// autocorrelación.
type tempoWin struct {
	mid float64
	ac  []float64
}

// songAC son las ventanas del flujo env de una canción (cada tempoMapStep) y
// su autocorrelación media, cada una relativa a su energía (una parte fuerte
// no pesa más que una suave). Sin ventanas si es más corta que una.
func songAC(env []envPoint) ([]tempoWin, []float64) {
	if len(env) < 2 || env[len(env)-1].t-env[0].t < tempoWindow {
		return nil, nil
	}
	var wins []tempoWin
	var acc []float64
	for t0 := env[0].t; t0+tempoWindow <= env[len(env)-1].t; t0 += tempoMapStep {
		i := sort.Search(len(env), func(i int) bool { return env[i].t >= t0 })
		j := sort.Search(len(env), func(i int) bool { return env[i].t >= t0+tempoWindow })
		ac := tempoAC(env[i:j])
		if ac == nil || ac[0] <= 0 {
			continue
		}
		if acc == nil {
			acc = make([]float64, len(ac))
		}
		acc = acc[:min(len(acc), len(ac))] // la longitud varía en un punto (tempoAC)
		for l := range acc {
			acc[l] += ac[l] / ac[0]
		}
		wins = append(wins, tempoWin{t0 + tempoWindow/2, ac})
	}
	for l := range acc {
		acc[l] /= float64(len(wins))
	}
	return wins, acc
}

// tempoBands son los grupos de bandas cuyo flujo se prueba para el tempo:
// todas (la primera, la de siempre), los graves, los medios y los agudos.
// Otro grupo solo gana si se repite bastante más (bandSwitch veces): con un
// muro de guitarras distorsionadas (Be Quiet and Drive de Deftones) todas
// juntas casi no se repiten y los platillos sí, pero en una canción sin
// batería un grupo suelto encontraba un tempo que no hay.
var tempoBands = [][2]int{{0, engine.MeterBands}, {0, 6}, {4, 10}, {10, engine.MeterBands}}

const bandSwitch = 1.5

// pickTempoEnv es el flujo de tempoBands con que se busca el tempo, de los
// puntos de la canción (t, la hora de cada uno, y rises, cuánto subió cada
// banda).
func pickTempoEnv(t []float64, rises [][engine.MeterBands]float64) []envPoint {
	var best []envPoint
	var base, bestScore float64
	for n, r := range tempoBands {
		env := make([]envPoint, len(t))
		for j := range t {
			var f float64
			for i := r[0]; i < r[1]; i++ {
				f += rises[j][i]
			}
			env[j] = envPoint{t[j], f}
		}
		sc := 0.0
		if _, acc := songAC(env); acc != nil {
			_, sc = bestTempo(acc, 1)
		}
		switch {
		case n == 0:
			best, base, bestScore = env, sc, sc
		case sc > bestScore && sc >= bandSwitch*base:
			best, bestScore = env, sc
		}
	}
	return best
}

// tempoBeats son los golpes del flujo env de una canción entera (en sus
// segundos), o nil si no tiene un ritmo claro. Como Ellis (2007), el método de
// librosa: primero el tempo de toda la canción (la autocorrelación promediada
// de ventanas, como en vivo), luego el tempo de cada tramo cerca de ese (una
// banda acelera o frena) y por último, con programación dinámica, la serie de
// golpes que más cae en el flujo sin salirse del tempo.
func tempoBeats(env []envPoint) (beats []float64, bpb []int) {
	wins, acc := songAC(env)
	if len(wins) == 0 {
		return nil, nil
	}
	global, score := bestTempo(acc, 1)
	if score < 0.05 {
		return nil, nil // nada se repite: sin ritmo (ambiente, voz)
	}
	meter := meterOf(acc, global, 1)
	// El tempo local de cada ventana (cerca del global, o de otra lectura del
	// mismo compás: sectionBeats) y cuántos golpes tiene su compás.
	local := make([]float64, len(wins))
	winBar := make([]int, len(wins))
	for k, w := range wins {
		per, sc := nearTempo(w.ac, global)
		local[k], winBar[k] = per, meter
		for _, n := range sectionBeats[meter] {
			p, s := nearTempo(w.ac, global*float64(meter)/float64(n))
			if s >= sectionMargin*sc && s > 0 {
				per, sc = p, s
				local[k], winBar[k] = p, n
			}
		}
	}
	// Una sección con otro pulso tiene que durar sectionMin ventanas seguidas:
	// si no, era una ventana rara y vuelve al de la canción.
	for k := 0; k < len(wins); {
		j := k
		for j < len(wins) && winBar[j] == winBar[k] {
			j++
		}
		if winBar[k] != meter && j-k < sectionMin {
			for i := k; i < j; i++ {
				local[i], _ = nearTempo(wins[i].ac, global)
				winBar[i] = meter
			}
		}
		k = j
	}
	smooth := make([]float64, len(local))
	for k := range local {
		w := append([]float64(nil), local[max(k-2, 0):min(k+3, len(local))]...)
		sort.Float64s(w)
		smooth[k] = w[len(w)/2]
	}
	// El flujo en la rejilla de tempoBin, relativo a su desviación (Ellis).
	x, t0 := envGrid(env)
	var mean, sd float64
	for _, v := range x {
		mean += v
	}
	mean /= float64(len(x))
	for _, v := range x {
		sd += (v - mean) * (v - mean)
	}
	sd = math.Sqrt(sd / float64(len(x)))
	if sd == 0 {
		return nil, nil
	}
	o := make([]float64, len(x))
	for i, v := range x {
		o[i] = v / sd
	}
	// La ventana más cercana a la hora t.
	winAt := func(t float64) int {
		k := sort.Search(len(wins), func(k int) bool { return wins[k].mid >= t })
		k = min(k, len(wins)-1)
		if k > 0 && t-wins[k-1].mid < wins[k].mid-t {
			k--
		}
		return k
	}
	// El periodo (en casillas) en cada casilla: el de la ventana más cercana.
	perAt := func(i int) float64 {
		return smooth[winAt(t0+float64(i)*tempoBin)] / tempoBin
	}
	// La programación dinámica: score[i] es lo mejor de una serie de golpes
	// que acaba en i; el golpe anterior va entre medio y dos periodos antes,
	// y cuesta más cuanto más se aleja de un periodo.
	n := len(o)
	sc := make([]float64, n)
	back := make([]int, n)
	for i := range o {
		p := perAt(i)
		best, from := 0.0, -1
		for j := i - int(2*p); j <= i-int(p/2); j++ {
			if j < 0 {
				continue
			}
			d := math.Log(float64(i-j) / p)
			if s := sc[j] - tempoMapTight*d*d; from < 0 || s > best {
				best, from = s, j
			}
		}
		if from >= 0 && best > 0 {
			sc[i], back[i] = o[i]+best, from
		} else {
			sc[i], back[i] = o[i], -1
		}
	}
	// El último golpe: el mejor del último periodo; de ahí, hacia atrás.
	end := n - 1
	for i := n - 1; i >= max(0, n-1-int(perAt(n-1))); i-- {
		if sc[i] > sc[end] {
			end = i
		}
	}
	for i := end; i >= 0; i = back[i] {
		beats = append(beats, t0+float64(i)*tempoBin)
	}
	for l, r := 0, len(beats)-1; l < r; l, r = l+1, r-1 {
		beats[l], beats[r] = beats[r], beats[l]
	}
	lo, hi := trimBeats(beats, env)
	beats = beats[lo:hi]
	bpb = make([]int, len(beats))
	for i, b := range beats {
		bpb[i] = winBar[winAt(b)]
	}
	// Una sección con otro pulso, de al menos dos compases enteros: un golpe
	// suelto en el borde (la ventana más cercana cambia a mitad de compás)
	// vuelve al de la canción.
	for i := 0; i < len(bpb); {
		j := i
		for j < len(bpb) && bpb[j] == bpb[i] {
			j++
		}
		if bpb[i] != meter && j-i < 2*bpb[i] {
			for k := i; k < j; k++ {
				bpb[k] = meter
			}
		}
		i = j
	}
	return beats, bpb
}

// sectionBeats son, por compás de la canción, los golpes por compás de las
// otras lecturas del mismo compás que puede tener una sección: en 3/4, 2
// (la intro de 3 Libras a 96 sobre 144) o 4; en 4/4, 3 o 6. El doble y la
// mitad no: ahí casi siempre es la misma música contada distinto.
var sectionBeats = map[int][]int{3: {2, 4}, 4: {3, 6}}

const (
	sectionMargin = 1.15 // cuánto más tiene que repetirse la otra lectura en una ventana
	sectionMin    = 3    // ventanas seguidas (unos 16 s) para que una sección cambie de pulso
)

// nearTempo es el periodo cerca de per (tempoMapDrift) que más se repite en
// ac, y su puntuación.
func nearTempo(ac []float64, per float64) (best, score float64) {
	best, score = per, math.Inf(-1)
	for p := per * (1 - tempoMapDrift); p <= per*(1+tempoMapDrift); p += 0.001 {
		if s := tempoScore(ac, p, 1); s > score {
			best, score = p, s
		}
	}
	return best, score
}

// trimBeats es el trozo de beats sin los golpes del principio y del final que
// caen en silencio (la programación dinámica los pone para unir; ahí no hay
// música): beats[lo:hi], vacío si quedan menos de cuatro.
func trimBeats(beats []float64, env []envPoint) (lo, hi int) {
	var mean float64
	for _, e := range env {
		mean += e.f
	}
	mean /= float64(len(env))
	// La energía del flujo alrededor de t (medio segundo).
	near := func(t float64) float64 {
		i := sort.Search(len(env), func(i int) bool { return env[i].t >= t-0.25 })
		var s float64
		var c int
		for ; i < len(env) && env[i].t <= t+0.25; i++ {
			s += env[i].f
			c++
		}
		if c == 0 {
			return 0
		}
		return s / float64(c)
	}
	lo, hi = 0, len(beats)
	for lo < hi && near(beats[lo]) < 0.2*mean {
		lo++
	}
	for hi > lo && near(beats[hi-1]) < 0.2*mean {
		hi--
	}
	if hi-lo < 4 {
		return 0, 0
	}
	return lo, hi
}

// tempoMapPath dice si path se puede analizar entero: un archivo de la
// tarjeta (no la radio ni un servidor).
func tempoMapPath(path string) bool {
	return path != "" && !strings.Contains(path, "://")
}

// currentTempoMap es el mapa de lo que suena, si ya está; si es otra canción,
// empieza a buscarlo (en una goroutine) y por ahora no hay.
func (a *App) currentTempoMap() *tempoMap {
	path := a.status.Track.Path
	if !tempoMapPath(path) || a.trackerSounding() {
		return nil
	}
	st := a.tempoMap
	if st == nil || st.path != path {
		if st != nil {
			st.cancel() // la anterior ya no hace falta
		}
		ctx, cancel := context.WithCancel(context.Background())
		st = &tempoMapState{path: path, cancel: cancel}
		a.tempoMap = st
		data := a.paths.Data
		go func() {
			m, err := loadTempoMap(ctx, data, path)
			if err != nil {
				if !errors.Is(err, errTempoMapLength) && !errors.Is(err, decode.ErrUnsupported) && !errors.Is(err, context.Canceled) {
					log.Printf("tempo map: %s: %v", filepath.Base(path), err)
				}
				return
			}
			st.ready.Store(m)
		}()
	}
	return st.ready.Load()
}

// mapBeat pone en b la rejilla del mapa de tempo de lo que suena, a la hora
// now (beatClock). Dice si la puso: sin mapa (aún), sin golpes o sin saber
// en qué segundo de la canción se está, sigue el tempo en vivo.
func (a *App) mapBeat(b *beat, now float64) bool {
	m := a.currentTempoMap()
	if m == nil {
		return false
	}
	pos, speed, ok := a.songPos(now)
	if !ok {
		return false
	}
	per, beatPos, ok := m.at(pos)
	if !ok {
		return false
	}
	// El ancla, en el primer golpe del compás y no en el último golpe: las
	// corcheas del compás (beat.slot) se cuentan desde ella, y si avanzara
	// golpe a golpe la batería apuntada en cada corchea (drums) se correría y
	// Ritmo casi no tendría notas.
	b.bar = 8
	if i := sort.Search(len(m.Beats), func(i int) bool { return m.Beats[i] > pos }) - 1; i >= 0 && i < len(m.Beats) {
		start, n := m.barAt(i)
		beatPos, b.bar = m.Beats[start], 2*n
	}
	b.mapped = true
	b.per = per / speed
	b.anchor = now - (pos-beatPos)/speed
	return true
}

// songPos es el segundo de la canción que se oye a la hora now (beatClock), y
// la velocidad; ok es false si el servicio aún no dijo dónde está (MeterAt).
func (a *App) songPos(now float64) (pos, speed float64, ok bool) {
	if a.status.MeterAt == 0 {
		return 0, 0, false
	}
	speed = a.status.Speed
	if speed <= 0 {
		speed = 1
	}
	return a.status.MeterPos + (now-float64(a.status.MeterAt)/1000)*speed, speed, true
}

// chartNotes pone en g las notas del chart de la canción que llegan en
// rhTravel, en la hora real (beatClock), y dice si la canción tiene chart.
// Al cambiar de canción o al saltar (seek), sigue desde donde se oye.
func (a *App) chartNotes(g *rhythmGame, now float64) bool {
	m := a.currentTempoMap()
	if m == nil || len(m.Notes) == 0 {
		return false
	}
	pos, speed, ok := a.songPos(now)
	if !ok {
		return false
	}
	path := a.status.Track.Path
	if g.chartPath != path || pos < g.chartPos-0.5 || pos > g.chartPos+rhTravel*speed+2 {
		if g.chartPath == path {
			g.notes = g.notes[:0] // un salto: las que bajaban eran de otra parte
		}
		g.chartPath = path
		g.chartIdx = sort.Search(len(m.Notes), func(i int) bool { return m.Notes[i].T > pos })
	}
	g.chartPos = pos
	for horizon := pos + rhTravel*speed; g.chartIdx < len(m.Notes) && m.Notes[g.chartIdx].T <= horizon; g.chartIdx++ {
		n := m.Notes[g.chartIdx]
		g.notes = append(g.notes, rhNote{now + (n.T-pos)/speed, drumLane(n.K, n.S)})
	}
	return true
}

// barAt es el compás del golpe i: su primer golpe y cuántos tiene. Sin Pos
// (un mapa sin compases), de cuatro en cuatro desde el primero.
func (m *tempoMap) barAt(i int) (start, n int) {
	if len(m.Pos) != len(m.Beats) {
		return i - i%4, 4
	}
	start = i - m.Pos[i]
	end := start + 1
	for end < len(m.Pos) && m.Pos[end] != 0 {
		end++
	}
	n = end - start
	if end == len(m.Pos) && start > 0 { // el último compás puede estar a medias
		_, n = m.barAt(start - 1)
	}
	return start, max(n, 1)
}
