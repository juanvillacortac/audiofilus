package sfx

import (
	"math"
	"testing"
)

// Cada palabra del locutor se sintetiza: dura lo que una palabra dicha y su
// eco, no satura y no tiene valores rotos; una frase es más larga que sus
// palabras.
func TestVoiceWords(t *testing.T) {
	for w := range words {
		s := Samples(Voice + w)
		d := float64(len(s)/2) / Rate
		if d < 0.8+echoRepeats*echoDelay || d > 1.5+echoRepeats*echoDelay {
			t.Errorf("%s: %.2f s", w, d)
		}
		peak := 0.0
		for _, x := range s {
			if math.IsNaN(float64(x)) || math.Abs(float64(x)) > 1 {
				t.Fatalf("%s: bad sample %f", w, x)
			}
			peak = math.Max(peak, math.Abs(float64(x)))
		}
		if peak < 0.5 {
			t.Errorf("%s: too quiet (peak %.2f)", w, peak)
		}
	}
	if len(Samples(Voice+"b2b+tetra")) <= len(Samples(Voice+"tetra")) {
		t.Error("a phrase is not longer than its last word")
	}
	if Samples(Voice+"nope") != nil {
		t.Error("an unknown word gave a sound")
	}
}

// La voz es grave: en una vocal sola, el tono (contado por los pulsos, un
// cruce de la onda hacia arriba por pulso) queda por debajo de 110 Hz.
func TestVoiceIsDeep(t *testing.T) {
	mono := speak([]seg{{dur: 0.3, f: phones["AA"].f, bw: phones["AA"].bw, av: 1}})
	period := 0
	for i := 1; i < len(mono); i++ {
		if mono[i-1] < -0.5 && mono[i] >= -0.5 { // un cruce por pulso
			period++
		}
	}
	if f0 := float64(period) / 0.3; f0 < 50 || f0 > 110 {
		t.Errorf("voice pitch ~%.0f Hz, want a deep one (50 to 110)", f0)
	}
}

// Los sonidos de Bloques existen todos y no saturan.
func TestBlocksSounds(t *testing.T) {
	names := []string{"bk_hold", "bk_lock", "bk_drop", "bk_beat", "bk_level", "bk_over"}
	for i := 0; i < 10; i++ {
		names = append(names, "bk_move_"+string(rune('0'+i)))
	}
	for i := 0; i < 4; i++ {
		names = append(names, "bk_rot_"+string(rune('0'+i)), "bk_clear_"+string(rune('1'+i)))
	}
	for _, n := range names {
		s := Samples(n)
		if len(s) == 0 {
			t.Errorf("%s: missing", n)
		}
		for _, x := range s {
			if math.Abs(float64(x)) > 1 {
				t.Errorf("%s: clips", n)
				break
			}
		}
	}
}

// El eco se oye después de la voz y va de un lado al otro: la primera
// repetición, a la izquierda.
func TestVoiceEcho(t *testing.T) {
	dry := speak(parse(words["tetra"]))
	l, r := echo(dry)
	from, to := len(dry), len(dry)+int(echoDelay*Rate)
	var el, er float64
	for i := from; i < to && i < len(l); i++ {
		el += l[i] * l[i]
		er += r[i] * r[i]
	}
	if el == 0 || el <= er {
		t.Errorf("after the voice: left %.3f, right %.3f; want the first echo on the left", el, er)
	}
}

// Las campanitas van en la tonalidad: la tónica de re mayor es re5, la de la
// menor es la4 y su segundo grado do5 (pentatónica menor); sin tonalidad, do.
func TestBlocksKeyScale(t *testing.T) {
	near := func(got, want float64) bool { return math.Abs(got-want) < 0.5 }
	if f := keyScale("D").deg(0); !near(f, 587.33) {
		t.Errorf("D tonic %.2f Hz", f)
	}
	if am := keyScale("Am"); !near(am.deg(0), 440) || !near(am.deg(1), 523.25) {
		t.Errorf("Am: %.2f, %.2f Hz", am.deg(0), am.deg(1))
	}
	if f := keyScale("").deg(5); !near(f, 1046.5) {
		t.Errorf("no key: octave %.2f Hz, want C6", f)
	}
	if len(Samples("bk_move_3@F#m")) == 0 || Samples("bk_nope@C") != nil {
		t.Error("block sounds in a key")
	}
}

// La voz endurecida tiene más cuerpo (más energía) que la limpia, sin pasarse
// del pico.
func TestVoiceHard(t *testing.T) {
	clean := speak(parse(words["tetra"]))
	rms := func(x []float64) float64 {
		s, peak := 0.0, 0.0
		for _, v := range x {
			s += v * v
			peak = math.Max(peak, math.Abs(v))
		}
		if peak > 1.0001 {
			t.Errorf("peak %.3f", peak)
		}
		return math.Sqrt(s / float64(len(x)))
	}
	if c, h := rms(clean), rms(hard(clean)); h < 1.5*c {
		t.Errorf("hard voice rms %.3f, clean %.3f: not much harder", h, c)
	}
}
