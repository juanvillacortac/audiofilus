package ui

import (
	"math"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/daemon"
	"github.com/juanvillacortac/audiofilus/internal/engine"
)

// meterSong es el medidor de una canción inventada a bpm: un bombo en cada
// golpe salvo cada séptimo (perdido), ruido de fondo, y los últimos quiet
// segundos sin golpes.
func meterSong(bpm, secs, quiet float64, end time.Time) []engine.MeterPoint {
	r := rand.New(rand.NewPCG(1, 2))
	per := 60 / bpm
	step := 1024.0 / 48000
	var out []engine.MeterPoint
	for t := 0.0; t < secs; t += step {
		beatN := math.Floor(t / per)
		since := t - beatN*per
		kick := 0.0
		if int(beatN)%7 != 6 && t < secs-quiet {
			kick = math.Exp(-since * 14)
		}
		bands := make([]byte, engine.MeterBands)
		for b := range bands {
			low := 1 - float64(b)/engine.MeterBands
			v := 0.25 + 0.1*r.Float64() + 0.6*kick*low
			bands[b] = byte(math.Min(255, v*255))
		}
		at := end.Add(time.Duration((t - secs) * float64(time.Second)))
		out = append(out, engine.MeterPoint{At: at.UnixMilli(), Level: 120, Bass: byte(60 + 180*kick), Bands: bands})
	}
	return out
}

// TestBeatTempo comprueba que el tempo se detecte con golpes perdidos y que
// no se pierda en un pasaje sin golpes (la rejilla sigue).
func TestBeatTempo(t *testing.T) {
	a := &App{isPlaying: true}
	a.status = daemon.FullStatus{Meter: meterSong(125, 16, 0, time.Now())}
	a.updateBeat()
	if bpm := a.beat.BPM(); bpm < 123 || bpm > 127 {
		t.Fatalf("BPM = %d, want ~125", bpm)
	}
	// Ahora, cinco segundos sin golpes al final: el tempo se mantiene.
	a = &App{isPlaying: true}
	a.status = daemon.FullStatus{Meter: meterSong(125, 20, 5, time.Now())}
	a.updateBeat()
	if bpm := a.beat.BPM(); bpm < 123 || bpm > 127 {
		t.Fatalf("after a quiet passage BPM = %d, want ~125 (kept)", bpm)
	}
	// Y la rejilla sigue marcando golpes: el último fue hace menos de uno.
	if since := a.beatClock() - a.beat.at; since > a.beat.period()+0.05 {
		t.Errorf("last grid beat %.2f s ago, want under one period (%.2f s)", since, a.beat.period())
	}
}

// syncopatedSong es el medidor de una canción tranquila inventada a bpm,
// como la del #14: bombo suave en los tiempos 1 y 3, caja débil en el 2 y
// el 4, una guitarra a contratiempo con swing (a 2/3 del golpe) y a veces en
// la semicorchea, y un pad que sube y baja. Termina en end.
func syncopatedSong(bpm, secs float64, end time.Time, seed uint64) []engine.MeterPoint {
	r := rand.New(rand.NewPCG(seed, 7))
	per := 60 / bpm
	step := 1024.0 / 48000
	var hits []struct{ t, lo, mid, hi float64 }
	for b := 0; float64(b)*per < secs; b++ {
		t0 := float64(b) * per
		switch b % 4 {
		case 0, 2:
			hits = append(hits, struct{ t, lo, mid, hi float64 }{t0, 0.5, 0.1, 0})
		default:
			hits = append(hits, struct{ t, lo, mid, hi float64 }{t0, 0.05, 0.3, 0.2})
		}
		if r.Float64() < 0.7 {
			hits = append(hits, struct{ t, lo, mid, hi float64 }{t0 + per*0.66, 0.1, 0.35, 0.15})
		}
		if r.Float64() < 0.3 {
			hits = append(hits, struct{ t, lo, mid, hi float64 }{t0 + per*0.25, 0.05, 0.25, 0.1})
		}
	}
	var out []engine.MeterPoint
	for t := 0.0; t < secs; t += step {
		var lo, mid, hi float64
		for _, h := range hits {
			if d := t - h.t; d >= 0 && d < 1 {
				e := math.Exp(-d * 10)
				lo += h.lo * e
				mid += h.mid * e
				hi += h.hi * e
			}
		}
		pad := 0.1 * math.Sin(t*0.7)
		bands := make([]byte, engine.MeterBands)
		for b := range bands {
			v := 0.35 + pad + 0.06*r.Float64()
			switch {
			case b < 4:
				v += lo
			case b < 11:
				v += mid
			default:
				v += hi
			}
			bands[b] = byte(math.Max(0, math.Min(255, v*255)))
		}
		at := end.Add(time.Duration((t - secs) * float64(time.Second)))
		out = append(out, engine.MeterPoint{At: at.UnixMilli(), Level: 120, Bass: byte(math.Min(255, 60+180*lo)), Bands: bands})
	}
	return out
}

// tempoTrace pasa pts al ritmo poco a poco (un segundo por fotograma, como
// llegan) y devuelve el BPM tras cada uno.
func tempoTrace(pts []engine.MeterPoint) []int {
	a := &App{isPlaying: true}
	var out []int
	for i := 0; i < len(pts); i += 47 {
		j := min(i+47, len(pts))
		a.status = daemon.FullStatus{Meter: pts[max(0, j-300):j]}
		a.updateBeat()
		out = append(out, a.beat.BPM())
	}
	return out
}

// TestBeatTempoSyncopated comprueba el #14: en una canción tranquila con
// golpes sincopados el tempo es el de la canción (ni su doble ni 3/2) y,
// una vez encontrado, no se mueve.
func TestBeatTempoSyncopated(t *testing.T) {
	for _, bpm := range []int{72, 95, 125} {
		for seed := uint64(1); seed <= 3; seed++ {
			tr := tempoTrace(syncopatedSong(float64(bpm), 60, time.Now(), seed))
			for i, got := range tr[10:] { // tras los primeros 10 s
				if got < bpm-1 || got > bpm+1 {
					t.Fatalf("%d BPM (seed %d): at %d s BPM = %d; trace %v", bpm, seed, i+10, got, tr)
				}
			}
		}
	}
}

// TestBeatTempoChange comprueba que un tempo distinto de verdad (otra
// sección, o una canción que sigue sin pausa) sí entre, en menos de 20
// segundos (tempoJump).
func TestBeatTempoChange(t *testing.T) {
	end := time.Now()
	pts := append(syncopatedSong(90, 30, end.Add(-30*time.Second), 1), syncopatedSong(128, 30, end, 2)...)
	tr := tempoTrace(pts)
	if got := tr[25]; got < 89 || got > 91 {
		t.Errorf("before the change BPM = %d, want 90", got)
	}
	for i, got := range tr[50:] {
		if got < 127 || got > 129 {
			t.Fatalf("20 s after the change BPM = %d at %d s, want 128; trace %v", got, i+50, tr)
		}
	}
}

// TestSameBar comprueba qué tempos cuentan como el mismo compás.
func TestSameBar(t *testing.T) {
	for _, c := range []float64{0.5, 1, 0.25, 0.75, 1.0 / 3, 0.51} {
		if !sameBar(c, 0.5) {
			t.Errorf("sameBar(%v, 0.5) = false, want true", c)
		}
	}
	for _, c := range []float64{0.6, 0.4, 0.9} {
		if sameBar(c, 0.5) {
			t.Errorf("sameBar(%v, 0.5) = true, want false", c)
		}
	}
}

// drumSong es el medidor de una batería inventada a 120 BPM: bombo en los
// tiempos 1 y 3, caja en el 2 y el 4, y charles en las corcheas a contratiempo.
func drumSong(secs float64, end time.Time) []engine.MeterPoint {
	r := rand.New(rand.NewPCG(3, 4))
	const per = 0.5
	step := 1024.0 / 48000
	var out []engine.MeterPoint
	for t := 0.0; t < secs; t += step {
		eighth := int(math.Floor(t / (per / 2)))
		since := t - float64(eighth)*(per/2)
		hitAt := math.Exp(-since * 25)
		slot := eighth % 8
		kick, snare, hat := 0.0, 0.0, 0.0
		switch {
		case slot == 0 || slot == 4:
			kick = hitAt
		case slot == 2 || slot == 6:
			snare = hitAt
		default:
			hat = hitAt
		}
		bands := make([]byte, engine.MeterBands)
		for b := range bands {
			v := 0.2 + 0.05*r.Float64()
			switch {
			case b < 3:
				v += 0.7 * kick
			case b >= 4 && b < 9:
				v += 0.6 * snare
			case b >= 11:
				v += 0.5 * hat
			}
			bands[b] = byte(math.Min(255, v*255))
		}
		at := end.Add(time.Duration((t - secs) * float64(time.Second)))
		out = append(out, engine.MeterPoint{At: at.UnixMilli(), Level: 120, Bass: byte(60 + 180*kick), Bands: bands})
	}
	return out
}

// TestDrumPattern comprueba que cada instrumento quede en sus corcheas: el
// bombo y la caja en tiempos alternos, el charles entre medias.
func TestDrumPattern(t *testing.T) {
	a := &App{isPlaying: true}
	a.status = daemon.FullStatus{Meter: drumSong(16, time.Now())}
	a.updateBeat()
	b := &a.beat
	if b.per <= 0 || !b.drums.any {
		t.Fatalf("no tempo (%v) or no drums", b.per)
	}
	now := a.beatClock()
	count := func(k int) (n int, slots []int) {
		for s := 0; s < 8; s++ {
			if b.drums.at(s, now, b.per)[k] {
				n++
				slots = append(slots, s)
			}
		}
		return
	}
	nk, ks := count(drumKick)
	ns, ss := count(drumSnare)
	nh, _ := count(drumHat)
	// Dónde empieza el compás no se sabe (la rejilla no conoce el primer
	// tiempo): lo que importa es que bombo y caja se alternen cada dos corcheas.
	if nk != 2 || ns != 2 || ks[1]-ks[0] != 4 || ss[1]-ss[0] != 4 || (ss[0]-ks[0]+8)%4 != 2 {
		t.Errorf("kick slots %v, snare slots %v: want two each, a half bar apart, alternating every two eighths", ks, ss)
	}
	if nh < 3 {
		t.Errorf("hat in %d slots, want the off-beat eighths", nh)
	}
}

// El último carril del juego de ritmo recibe los platillos y la mitad de los
// golpes de charles (antes solo los platillos, y casi nunca tenía notas).
func TestDrumLanes(t *testing.T) {
	if drumLane(drumCrash, 0) != rhLanes-1 {
		t.Errorf("crash not on the last lane")
	}
	if drumLane(drumHat, 0) != drumHat || drumLane(drumHat, 1) != rhLanes-1 {
		t.Errorf("hats don't alternate between lanes %d and %d", drumHat, rhLanes-1)
	}
	if drumLane(drumKick, 1) != drumKick || drumLane(drumSnare, 1) != drumSnare {
		t.Errorf("kick or snare moved")
	}
}
