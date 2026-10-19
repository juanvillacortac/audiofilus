package tracker

import "strings"

// Las canciones de ejemplo: para oír lo que da el tracker y para aprender
// mirándolas. Se escriben con ayudas (note, drums, chord...) en vez de celda a
// celda.

// note lee "C-4", "F#2" u "OFF" (0 si no se entiende).
func note(s string) int8 {
	if s == "OFF" {
		return NoteOff
	}
	if len(s) != 3 {
		return NoteEmpty
	}
	i := strings.Index("C-C#D-D#E-F-F#G-G#A-A#B-", s[:2])
	if i < 0 || i%2 != 0 || s[2] < '0' || s[2] > '7' {
		return NoteEmpty
	}
	return int8(i/2 + int(s[2]-'0')*12 + 1)
}

// pat ayuda a escribir un patrón.
type pat struct{ p Pattern }

func newPat() *pat { return &pat{NewPattern()} }

// put pone una nota en el paso step del canal ch.
func (b *pat) put(step, ch int, n string, vol int8) *pat {
	b.p[step][ch].Note, b.p[step][ch].Vol = note(n), vol
	return b
}

// fx pone un efecto en un paso.
func (b *pat) fx(step, ch int, f byte, arg uint8) *pat {
	b.p[step][ch].Fx, b.p[step][ch].Arg = f, arg
	return b
}

// drums pone la batería en el ruido: k bombo, s caja, h charles, por paso
// ("k.h.s.h.k.h.s.h."; '.' nada).
func (b *pat) drums(steps string) *pat {
	for i, c := range steps {
		switch c {
		case 'k':
			b.put(i, Noise, "C-1", 13)
		case 's':
			b.put(i, Noise, "C-4", 11)
		case 'h':
			b.put(i, Noise, "C-6", 4)
		case 'o': // charles abierto
			b.put(i, Noise, "G-5", 6)
		}
	}
	return b
}

// line pone una línea de notas en un canal, una por paso ("C-2 --- G-2 ...";
// "---" nada, "OFF" corta).
func (b *pat) line(ch int, vol int8, notes string) *pat {
	for i, n := range strings.Fields(notes) {
		if n != "---" {
			b.put(i, ch, n, vol)
		}
	}
	return b
}

// chord pone un acorde arpegiado en el pulso 1: la nota y el arpegio (0x47
// mayor, 0x37 menor).
func (b *pat) chord(step int, root string, arp uint8, vol int8) *pat {
	return b.put(step, Pulse1, root, vol).fx(step, Pulse1, FxArp, arp)
}

const (
	maj = 0x47
	mnr = 0x37
)

// Examples son las canciones de ejemplo.
func Examples() []*Song {
	return []*Song{pocketPop(), cryptCrawl(), nightDrive(), amenChip()}
}

// pocketPop: pop alegre en Do, 120 BPM. Intro, estrofa y estribillo.
func pocketPop() *Song {
	intro := newPat().drums("k...s...k.k.s...").
		line(Triangle, 15, "C-2 --- --- --- C-2 --- --- --- G-1 --- --- --- G-1 --- A#1 ---")
	verse := newPat().drums("k.h.s.h.k.hks.h.").
		line(Triangle, 15, "C-2 --- C-3 --- C-2 --- C-3 --- A-1 --- A-2 --- A-1 --- A-2 ---").
		chord(0, "C-4", maj, 8).chord(8, "A-3", mnr, 8).
		line(Pulse2, 7, "--- --- --- --- E-5 --- G-5 --- --- --- E-5 --- D-5 --- C-5 ---").
		fx(4, Pulse2, FxDuty, 1)
	verse2 := newPat().drums("k.h.s.h.k.hks.ho").
		line(Triangle, 15, "F-1 --- F-2 --- F-1 --- F-2 --- G-1 --- G-2 --- G-1 --- G-2 ---").
		chord(0, "F-4", maj, 8).chord(8, "G-4", maj, 8).
		line(Pulse2, 7, "A-5 --- --- --- G-5 --- F-5 --- E-5 --- --- --- D-5 --- --- ---").
		fx(0, Pulse2, FxDuty, 1)
	chorus := newPat().drums("kkh.s.hkk.h.s.ho").
		line(Triangle, 15, "F-2 F-2 F-3 F-2 G-2 G-2 G-3 G-2 E-2 E-2 E-3 E-2 A-1 A-1 A-2 A-1").
		chord(0, "F-4", maj, 10).chord(4, "G-4", maj, 10).chord(8, "E-4", mnr, 10).chord(12, "A-3", mnr, 10).
		line(Pulse2, 9, "C-6 --- A-5 --- D-6 --- B-5 --- E-6 --- --- D-6 C-6 --- --- ---").
		fx(0, Pulse2, FxDuty, 2)
	return &Song{Name: "Pocket Pop", BPM: 120,
		Patterns: []Pattern{intro.p, verse.p, verse2.p, chorus.p},
		Order:    []int{0, 1, 2, 1, 2, 3, 3, 1, 2, 3, 3}}
}

// cryptCrawl: el tema de la mazmorra, en La menor, 140 BPM: arpegios oscuros,
// un bajo que camina y una melodía que sube.
func cryptCrawl() *Song {
	a := newPat().drums("k...h...k.k.h...").
		line(Triangle, 14, "A-1 --- A-1 C-2 --- A-1 E-2 --- A-1 --- G-1 --- F-1 --- E-1 ---").
		chord(0, "A-3", mnr, 7).chord(8, "F-3", maj, 7).fx(0, Pulse1, FxArp, mnr)
	b := newPat().drums("k...s...k.k.s..h").
		line(Triangle, 14, "A-1 --- A-1 C-2 --- A-1 E-2 --- E-1 --- E-1 G#1 --- E-1 B-1 ---").
		chord(0, "A-3", mnr, 7).chord(8, "E-3", maj, 7).
		line(Pulse2, 8, "A-4 --- C-5 --- E-5 --- D-5 --- C-5 --- B-4 --- G#4 --- --- ---").
		fx(0, Pulse2, FxDuty, 0)
	c := newPat().drums("k.k.s.k.k.k.s.ss").
		line(Triangle, 15, "D-2 --- D-2 F-2 --- D-2 A-2 --- E-2 --- E-2 G#2 --- E-2 B-2 ---").
		chord(0, "D-4", mnr, 9).chord(8, "E-4", maj, 9).
		line(Pulse2, 9, "F-5 --- E-5 --- D-5 --- A-5 --- G#5 --- --- --- B-5 --- OFF ---").
		fx(8, Pulse2, FxBend, 0x02)
	return &Song{Name: "Crypt Crawl", BPM: 140,
		Patterns: []Pattern{a.p, b.p, c.p},
		Order:    []int{0, 0, 1, 1, 2, 1, 2, 2}}
}

// nightDrive: synthwave lento, en Mi menor, 100 BPM: bajo en corcheas, un
// arpegio largo y una melodía con deslizamientos.
func nightDrive() *Song {
	a := newPat().drums("k.h.s.h.k.h.s.hh").
		line(Triangle, 15, "E-1 E-2 E-1 E-2 E-1 E-2 E-1 E-2 C-1 C-2 C-1 C-2 D-1 D-2 D-1 D-2").
		chord(0, "E-4", mnr, 6).chord(8, "C-4", maj, 6).chord(12, "D-4", maj, 6).
		fx(0, Pulse2, FxDuty, 3)
	b := newPat().drums("k.h.s.h.k.hks.ho").
		line(Triangle, 15, "E-1 E-2 E-1 E-2 E-1 E-2 E-1 E-2 C-1 C-2 C-1 C-2 D-1 D-2 D-1 D-2").
		chord(0, "E-4", mnr, 6).chord(8, "C-4", maj, 6).chord(12, "D-4", maj, 6).
		line(Pulse2, 8, "B-4 --- --- --- --- --- G-5 --- --- --- F#5 --- --- --- D-5 ---").
		fx(0, Pulse2, FxDuty, 3).fx(4, Pulse2, FxBend, 0x81).fx(6, Pulse2, FxBend, 0x01).fx(14, Pulse2, FxBend, 0x84)
	return &Song{Name: "Night Drive", BPM: 100,
		Patterns: []Pattern{a.p, b.p},
		Order:    []int{0, 0, 1, 1, 0, 1, 1}}
}

// amenChip: breakbeat rápido, 170 BPM: la batería partida, un bajo que salta
// de octava y pulsos que cambian de ancho.
func amenChip() *Song {
	a := newPat().drums("k.k.s..sk.k.s..s").
		line(Triangle, 15, "C-2 --- C-3 --- --- C-2 --- D#2 --- --- F-2 --- G-2 --- --- ---")
	b := newPat().drums("k.k.s..s.ks.s..s").
		line(Triangle, 15, "C-2 --- C-3 --- --- C-2 --- D#2 --- --- F-2 --- A#1 --- --- ---").
		chord(0, "C-4", mnr, 8).fx(0, Pulse1, FxDuty, 0).chord(8, "G#3", maj, 8).
		line(Pulse2, 6, "G-5 --- G-5 --- A#5 --- G-5 --- F-5 --- D#5 --- F-5 --- --- ---")
	c := newPat().drums("kss.skskk.s.ssss").
		line(Triangle, 15, "F-2 F-3 F-2 F-3 G-2 G-3 G-2 G-3 G#2 G#3 G#2 G#3 A#2 A#3 A#2 A#3").
		chord(0, "F-4", mnr, 9).chord(4, "G-4", mnr, 9).chord(8, "G#4", maj, 9).chord(12, "A#4", maj, 9).
		fx(0, Pulse2, FxDuty, 1)
	return &Song{Name: "Amen Chip", BPM: 170,
		Patterns: []Pattern{a.p, b.p, c.p},
		Order:    []int{0, 0, 1, 1, 1, 2, 1, 2}}
}

// Empty es una canción vacía, para empezar de cero.
func Empty(name string) *Song {
	return &Song{Name: name, BPM: 120, Patterns: []Pattern{NewPattern()}, Order: []int{0}}
}
