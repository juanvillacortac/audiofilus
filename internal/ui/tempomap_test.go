package ui

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// writeTestWAV guarda samples (mono, -1..1) como WAV de 16 bits estéreo.
func writeTestWAV(t *testing.T, samples []float64, rate int) string {
	t.Helper()
	n := len(samples)
	b := make([]byte, 44+n*4)
	copy(b[0:], "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(36+n*4))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 2)
	binary.LittleEndian.PutUint32(b[24:], uint32(rate))
	binary.LittleEndian.PutUint32(b[28:], uint32(rate*4))
	binary.LittleEndian.PutUint16(b[32:], 4)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(n*4))
	for i, s := range samples {
		v := int16(math.Max(-1, math.Min(1, s)) * 32000)
		binary.LittleEndian.PutUint16(b[44+i*4:], uint16(v))
		binary.LittleEndian.PutUint16(b[46+i*4:], uint16(v))
	}
	p := filepath.Join(t.TempDir(), "song.wav")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// houseSong es un loop de house de secs segundos: bombo en cada golpe y, más
// fuertes, bajo y charles a contratiempo. beatAt da la hora del golpe k (para
// un tempo que cambia). Devuelve las muestras y las horas de los bombos.
func houseSong(secs float64, rate int, beatAt func(k int) float64) ([]float64, []float64) {
	x := make([]float64, int(secs*float64(rate)))
	rng := rand.New(rand.NewPCG(1, 2))
	add := func(at, dur float64, f func(u float64) float64) {
		i0 := int(at * float64(rate))
		for i := 0; i < int(dur*float64(rate)) && i0+i < len(x); i++ {
			if i0+i >= 0 {
				x[i0+i] += f(float64(i) / float64(rate))
			}
		}
	}
	var kicks []float64
	for k := 0; ; k++ {
		t := beatAt(k)
		if t >= secs-1 {
			break
		}
		kicks = append(kicks, t)
		next := beatAt(k + 1)
		off := (t + next) / 2
		// Bombo: un seno que baja de 120 a 45 Hz y se apaga.
		add(t, 0.25, func(u float64) float64 {
			return 0.5 * math.Exp(-u*14) * math.Sin(2*math.Pi*(45*u+75*(1-math.Exp(-u*30))/30))
		})
		// Bajo a contratiempo, más fuerte: una sierra de 70 Hz.
		add(off, 0.18, func(u float64) float64 {
			return 0.45 * math.Exp(-u*8) * (2*math.Mod(70*u, 1) - 1)
		})
		// Charles a contratiempo, también fuerte: ruido corto.
		add(off, 0.05, func(u float64) float64 {
			return 0.35 * math.Exp(-u*60) * (rng.Float64()*2 - 1)
		})
	}
	return x, kicks
}

// beatErrors son, para cada bombo, la distancia al golpe más cercano del mapa.
func beatErrors(beats, kicks []float64) []float64 {
	var out []float64
	for _, k := range kicks {
		i := sort.SearchFloat64s(beats, k)
		best := math.Inf(1)
		for _, j := range []int{i - 1, i} {
			if j >= 0 && j < len(beats) && math.Abs(beats[j]-k) < math.Abs(best) {
				best = beats[j] - k
			}
		}
		out = append(out, best)
	}
	return out
}

// TestTempoMapOnTheKick comprueba que el mapa pone los golpes en el bombo y
// no a contratiempo, aunque el bajo y el charles de entre medias suenen más
// (el house del #14, donde la rejilla en vivo se iba al contratiempo).
func TestTempoMapOnTheKick(t *testing.T) {
	const rate, bpm = 44100, 124.0
	x, kicks := houseSong(60, rate, func(k int) float64 { return 0.5 + float64(k)*60/bpm })
	beats, _, _, err := analyzeTempo(context.Background(), writeTestWAV(t, x, rate))
	if err != nil {
		t.Fatal(err)
	}
	errs := beatErrors(beats, kicks[10:len(kicks)-5])
	med := append([]float64(nil), errs...)
	sort.Float64s(med)
	t.Logf("median error %.3f s", med[len(med)/2])
	bad := 0
	for _, e := range errs {
		if math.Abs(e) > 0.03 {
			bad++
		}
	}
	if bad > len(errs)/20 {
		t.Errorf("%d of %d kicks have no beat within 30 ms (errors %v)", bad, len(errs), errs)
	}
}

// TestTempoMapFollowsDrift comprueba que el mapa sigue a una banda que
// acelera (de 116 a 128 BPM en un minuto), sin perder el golpe.
func TestTempoMapFollowsDrift(t *testing.T) {
	const rate = 44100
	// El tempo sube linealmente: la hora del golpe k sale de sumar periodos.
	var at []float64
	tm := 0.5
	for k := 0; k < 200; k++ {
		at = append(at, tm)
		bpm := 116 + 12*math.Min(1, tm/60)
		tm += 60 / bpm
	}
	x, kicks := houseSong(60, rate, func(k int) float64 { return at[k] })
	beats, _, _, err := analyzeTempo(context.Background(), writeTestWAV(t, x, rate))
	if err != nil {
		t.Fatal(err)
	}
	errs := beatErrors(beats, kicks[10:len(kicks)-5])
	med := append([]float64(nil), errs...)
	sort.Float64s(med)
	t.Logf("median error %.3f s", med[len(med)/2])
	bad := 0
	for _, e := range errs {
		if math.Abs(e) > 0.03 {
			bad++
		}
	}
	if bad > len(errs)/20 {
		t.Errorf("%d of %d kicks have no beat within 30 ms (errors %v)", bad, len(errs), errs)
	}
}

// TestTempoMapAt comprueba la rejilla que da el mapa: dentro, antes del
// primer golpe y después del último.
func TestTempoMapAt(t *testing.T) {
	m := &tempoMap{Beats: []float64{1, 1.5, 2, 2.5, 3}}
	for _, c := range []struct{ pos, per, beat float64 }{
		{1.7, 0.5, 1.5},
		{0.2, 0.5, 0},
		{4.1, 0.5, 4},
	} {
		per, beat, ok := m.at(c.pos)
		if !ok || math.Abs(per-c.per) > 1e-9 || math.Abs(beat-c.beat) > 1e-9 {
			t.Errorf("at(%v) = %v, %v, %v; want %v, %v", c.pos, per, beat, ok, c.per, c.beat)
		}
	}
	if _, _, ok := (&tempoMap{}).at(1); ok {
		t.Error("an empty map gave a grid")
	}
}

// TestMapBeat comprueba que la rejilla del mapa se pone en la hora real: el
// servicio dice qué segundo de la canción se oye a qué hora (MeterAt,
// MeterPos), y a doble velocidad los golpes llegan el doble de seguido.
func TestMapBeat(t *testing.T) {
	a := &App{isPlaying: true}
	a.status.Track.Path = "/music/song.flac"
	st := &tempoMapState{path: a.status.Track.Path}
	st.ready.Store(&tempoMap{Beats: []float64{10, 10.5, 11, 11.5, 12, 12.5, 13}})
	a.tempoMap = st
	a.status.MeterAt, a.status.MeterPos = 1_000_000, 10.2 // a los 1000 s Unix suena el 10,2
	for _, c := range []struct{ speed, now, per, anchor float64 }{
		{1, 1000.6, 0.5, 1000.3},    // suena el 10,8: el golpe del 10,5 fue a las 1000,3
		{2, 1000.15, 0.25, 1000.15}, // suena el 10,5, justo un golpe
	} {
		a.status.Speed = c.speed
		var b beat
		if !a.mapBeat(&b, c.now) {
			t.Fatalf("speed %v: no grid from the map", c.speed)
		}
		// El ancla es cualquier golpe de la rejilla: se compara su fase.
		d := math.Mod(b.anchor-c.anchor, c.per)
		d = math.Min(math.Abs(d), c.per-math.Abs(d))
		if math.Abs(b.per-c.per) > 1e-6 || d > 1e-6 {
			t.Errorf("speed %v: per %v anchor %v, want %v %v", c.speed, b.per, b.anchor, c.per, c.anchor)
		}
	}
	a.status.MeterAt = 0 // sin saber en qué segundo se está: sigue el tempo en vivo
	if a.mapBeat(&beat{}, 1000) {
		t.Error("a grid without the service's position")
	}
}

// TestMapBeatBar comprueba que, con el mapa, cada corchea del compás guarda
// su número mientras suena la canción: Ritmo apunta la batería por corchea
// (beat.slot) y con el ancla en el último golpe la numeración se corría dos
// corcheas por golpe, y casi no salían notas.
func TestMapBeatBar(t *testing.T) {
	a := &App{isPlaying: true}
	a.status.Track.Path = "/music/song.flac"
	var beats []float64
	for k := 0; k < 64; k++ {
		beats = append(beats, 1+0.5*float64(k))
	}
	st := &tempoMapState{path: a.status.Track.Path}
	st.ready.Store(&tempoMap{Beats: beats})
	a.tempoMap = st
	a.status.Speed = 1
	a.status.MeterAt, a.status.MeterPos = 1_000_000, 0 // a los 1000 s Unix suena el 0
	var b beat
	first := -1
	for now := 1002.0; now < 1025; now += 0.37 {
		if !a.mapBeat(&b, now) {
			t.Fatal("no grid from the map")
		}
		// El golpe 4 de la canción (segundo 3), en la hora real.
		got := b.slot(1000 + beats[4])
		if first < 0 {
			first = got
		} else if got != first {
			t.Fatalf("at %v the same eighth is slot %d, before %d", now, got, first)
		}
	}
}

// TestDrumChart comprueba el chart de batería de Ritmo: en el loop de house,
// una nota de graves en cada bombo, en su golpe. El bajo a contratiempo cae
// en las mismas bandas y también da notas de graves (por frecuencia no se
// distinguen del bombo): en el juego ese carril es "los graves".
func TestDrumChart(t *testing.T) {
	const rate, bpm = 44100, 124.0
	x, kicks := houseSong(60, rate, func(k int) float64 { return 0.5 + float64(k)*60/bpm })
	beats, _, notes, err := analyzeTempo(context.Background(), writeTestWAV(t, x, rate))
	if err != nil {
		t.Fatal(err)
	}
	if len(beats) == 0 || len(notes) == 0 {
		t.Fatalf("%d beats, %d notes", len(beats), len(notes))
	}
	kickAt := map[int]bool{}
	for _, n := range notes {
		if n.K != drumKick {
			continue
		}
		kickAt[int(math.Round(n.T*100))] = true
	}
	found := 0
	inner := kicks[10 : len(kicks)-5]
	for _, k := range inner {
		for d := -3; d <= 3; d++ {
			if kickAt[int(math.Round(k*100))+d] {
				found++
				break
			}
		}
	}
	if found < len(inner)*9/10 {
		t.Errorf("kick notes on %d of %d kicks", found, len(inner))
	}
}

// TestChartNotes comprueba que las notas del chart llegan a Ritmo en la hora
// real, solo las que caen en rhTravel, y que tras un salto sigue desde donde
// se oye (sin las que bajaban).
func TestChartNotes(t *testing.T) {
	a := &App{isPlaying: true}
	a.status.Track.Path = "/music/song.flac"
	var notes []chartNote
	for k := 0; k < 200; k++ {
		notes = append(notes, chartNote{T: float64(k) * 0.5, K: drumKick, S: (2 * k) % 8})
	}
	st := &tempoMapState{path: a.status.Track.Path}
	st.ready.Store(&tempoMap{Beats: []float64{0, 0.5, 1}, Notes: notes})
	a.tempoMap = st
	a.status.Speed = 1
	a.status.MeterAt, a.status.MeterPos = 1_000_000, 10 // a los 1000 s Unix suena el 10
	g := &rhythmGame{}
	if !a.chartNotes(g, 1000) {
		t.Fatal("no chart")
	}
	want := int(math.Floor(rhTravel / 0.5)) // las de (10, 10+rhTravel]
	if len(g.notes) != want || math.Abs(g.notes[0].t-1000.5) > 1e-9 {
		t.Fatalf("got %d notes starting at %v, want %d from 1000.5", len(g.notes), g.notes[0].t, want)
	}
	// Un salto adelante: lo que bajaba se va y sigue desde el 60.
	a.status.MeterPos = 60
	a.chartNotes(g, 1000)
	if len(g.notes) != want || math.Abs(g.notes[0].t-1000.5) > 1e-9 {
		t.Errorf("after a seek got %d notes starting at %v", len(g.notes), g.notes[0].t)
	}
}

// TestSongACLengths comprueba que promediar ventanas de distinta longitud
// (tempoAC la limita al tamaño de cada una, que varía en un punto) no se sale
// del arreglo: la interfaz entraba en pánico al analizar algunas canciones.
func TestSongACLengths(t *testing.T) {
	var env []envPoint
	for i := 0; i < 3000; i++ {
		// Puntos con un paso irregular: cada ventana tiene un número distinto.
		tm := float64(i)*0.0232 + 0.003*float64(i%3)
		f := 0.0
		if i%22 == 0 {
			f = 1
		}
		env = append(env, envPoint{tm, f})
	}
	wins, acc := songAC(env)
	if len(wins) == 0 || len(acc) == 0 {
		t.Fatal("no windows")
	}
	for _, w := range wins {
		if len(acc) > len(w.ac) {
			t.Fatalf("average longer (%d) than a window (%d)", len(acc), len(w.ac))
		}
	}
	// Y el tempo en vivo, ventana tras ventana.
	var b beat
	for _, e := range env {
		b.env = append(b.env, e)
		if b.envStart == 0 {
			b.envStart = e.t
		}
		if e.t-b.tempoT >= tempoEvery {
			b.tempoT = e.t
			b.updateTempo()
		}
	}
}

// TestTempoMapCanceled comprueba que cambiar de canción antes de
// tempoMapDelay no analiza ni guarda nada: pasando canciones se juntaban
// varios análisis y la consola se arrastraba.
func TestTempoMapCanceled(t *testing.T) {
	const rate = 44100
	x, _ := houseSong(20, rate, func(k int) float64 { return 0.5 + float64(k)*0.5 })
	song := writeTestWAV(t, x, rate)
	data := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	start := time.Now()
	if _, err := loadTempoMap(ctx, data, song); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("took %v to give up", d)
	}
	if _, err := os.Stat(tempoMapFile(data, song)); err == nil {
		t.Error("a map was saved for a song that was skipped")
	}
}
