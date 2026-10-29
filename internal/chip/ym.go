package chip

import (
	"bytes"
	"encoding/binary"
	"errors"
	"time"
)

// Los YM (del Atari ST, de Leonard/Arnaud Carré) y los VTX (de Vortex
// Tracker, sobre todo del Spectrum) son volcados de los registros del chip:
// los 14 (o 16) registros de cada cuadro, uno por interrupción (50 Hz casi
// siempre). Vienen comprimidos con LHA (lh5.go) y guardados por registro (el
// registro 0 de todos los cuadros, luego el 1...), que comprime mejor.
//
// Los YM5 y YM6 traen además los efectos del Atari, que el chip no hace solo
// y que el ST hacía con los temporizadores del MFP: la voz SID (el volumen
// de un canal que se enciende y se apaga muy rápido), los digidrums (sonidos
// muestreados por el volumen), el sync buzzer (la envolvente reiniciada sin
// parar) y, en los YM6, la SID senoidal (aquí, como la SID).

// regSong es un volcado de registros.
type regSong struct {
	frames [][16]byte
	hz     float64 // cuadros por segundo
	clock  float64 // reloj del chip
	ym     bool    // YM2149 (si no, AY)
	pan    [3]float64
	fx     int // 5 o 6: efectos de los YM5 o YM6
	drums  [][]float64
	track  Track
}

func (s *regSong) tracks() []Track { return []Track{s.track} }

func (s *regSong) play(_ int, rate int) (Tune, error) {
	t := &regTune{s: s, chip: newAY(s.clock, rate, s.ym, s.pan), rate: float64(rate)}
	t.per = t.rate / s.hz
	for i := range t.sid {
		t.sid[i].voice, t.drum[i].voice = -1, -1
	}
	return t, nil
}

// regLength es lo que dura un volcado de n cuadros a hz.
func regLength(n int, hz float64) time.Duration {
	return time.Duration(float64(n) / hz * float64(time.Second))
}

// deinterleave pasa de "por registro" a "por cuadro": n cuadros de nregs.
func deinterleave(b []byte, n, nregs int, interleaved bool) [][16]byte {
	out := make([][16]byte, n)
	for f := 0; f < n; f++ {
		for r := 0; r < nregs; r++ {
			if interleaved {
				out[f][r] = b[r*n+f]
			} else {
				out[f][r] = b[f*nregs+r]
			}
		}
	}
	return out
}

// mfpPrediv son los divisores del MFP del Atari; su reloj es mfpClock.
var mfpPrediv = [8]float64{0, 4, 10, 16, 50, 64, 100, 200}

const mfpClock = 2457600

// Los atributos de los YM5/YM6.
const (
	ymInterleaved = 1
	ymDrumSigned  = 2
	ymDrum4Bits   = 4
)

// loadYM lee un YM (comprimido con LHA o ya descomprimido).
func loadYM(b []byte, base string) (song, error) {
	if len(b) > 22 && string(b[2:4]) == "-l" {
		var err error
		if b, err = lhaExtract(b); err != nil {
			return nil, err
		}
	}
	if len(b) < 4 {
		return nil, ErrFormat
	}
	s := &regSong{hz: 50, clock: 2000000, ym: true, pan: panMono,
		track: Track{Song: 1, Title: base, System: "Atari ST"}}
	switch string(b[:4]) {
	case "YM2!", "YM3!", "YM3b":
		// Solo los 14 registros, a 50 Hz y 2 MHz.
		data := b[4:]
		if string(b[:4]) == "YM3b" && len(data) >= 4 {
			data = data[:len(data)-4] // el cuadro del bucle
		}
		n := len(data) / 14
		if n == 0 {
			return nil, ErrFormat
		}
		s.frames = deinterleave(data, n, 14, true)
	case "YM5!", "YM6!":
		if err := s.readYM56(b); err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("chip: unsupported ym version")
	}
	s.track.Length = regLength(len(s.frames), s.hz)
	return s, nil
}

// readYM56 lee la cabecera, los digidrums, los textos y los cuadros de un
// YM5 o YM6 (todo en big endian).
func (s *regSong) readYM56(b []byte) error {
	if len(b) < 34 || string(b[4:12]) != "LeOnArD!" {
		return ErrFormat
	}
	be := binary.BigEndian
	n := int(be.Uint32(b[12:]))
	attr := be.Uint32(b[16:])
	ndrums := int(be.Uint16(b[20:]))
	s.clock = float64(be.Uint32(b[22:]))
	s.hz = float64(be.Uint16(b[26:]))
	extra := int(be.Uint16(b[32:]))
	if s.clock < 100000 || s.hz < 1 || s.hz > 1000 || n <= 0 {
		return ErrFormat
	}
	s.fx = 5
	if b[2] == '6' {
		s.fx = 6
	}
	at := 34 + extra
	for i := 0; i < ndrums; i++ {
		if at+4 > len(b) {
			return ErrFormat
		}
		size := int(be.Uint32(b[at:]))
		at += 4
		if size < 0 || at+size > len(b) {
			return ErrFormat
		}
		d := make([]float64, size)
		for j, v := range b[at : at+size] {
			switch {
			case attr&ymDrum4Bits != 0:
				d[j] = ymDAC[int(v&15)*2+1]
			case attr&ymDrumSigned != 0:
				d[j] = float64(v^0x80) / 255
			default:
				d[j] = float64(v) / 255
			}
		}
		s.drums = append(s.drums, d)
		at += size
	}
	s.track.Title = orStr(cstr(b, &at), s.track.Title)
	s.track.Author = cstr(b, &at)
	cstr(b, &at) // el comentario
	if at+n*16 > len(b) {
		return ErrFormat
	}
	s.frames = deinterleave(b[at:at+n*16], n, 16, attr&ymInterleaved != 0)
	return nil
}

// orStr es s, o def si s está vacío.
func orStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// loadVTX lee un VTX: una cabecera corta, los textos y los registros
// comprimidos con -lh5- (sin cabecera de LHA).
func loadVTX(b []byte, base string) (song, error) {
	if len(b) < 16 {
		return nil, ErrFormat
	}
	id := string(bytes.ToLower(b[:2]))
	if id != "ay" && id != "ym" {
		return nil, ErrFormat
	}
	le := binary.LittleEndian
	s := &regSong{ym: id == "ym", clock: float64(le.Uint32(b[5:])), hz: float64(b[9])}
	s.pan = [...][3]float64{panMono, panABC, panACB, panBAC, panBCA, panCAB, panCBA}[min(int(b[2]), 6)]
	size := int(le.Uint32(b[12:]))
	if s.clock < 100000 || s.hz < 1 || size <= 0 || size > 64<<20 {
		return nil, ErrFormat
	}
	at := 16
	title, author := cstr(b, &at), cstr(b, &at)
	from := cstr(b, &at)
	cstr(b, &at) // el tracker
	cstr(b, &at) // el comentario
	if at > len(b) {
		return nil, ErrFormat
	}
	data, err := lh5Decode(b[at:], size)
	if err != nil {
		return nil, err
	}
	n := len(data) / 14
	if n == 0 {
		return nil, ErrFormat
	}
	s.frames = deinterleave(data, n, 14, true)
	s.track = Track{Song: 1, Title: orStr(title, base), Author: author, Game: from,
		System: "ZX Spectrum", Length: regLength(n, s.hz)}
	return s, nil
}

// regTune toca un volcado de registros.
type regTune struct {
	s     *regSong
	chip  *ay
	rate  float64
	per   float64 // muestras por cuadro
	left  float64 // muestras que faltan del cuadro
	frame int
	dc    [2]dcBlock
	// Los efectos del Atari, en marcha.
	sid  [3]fxTimer
	drum [3]fxTimer
	buzz fxTimer
}

// fxTimer es un efecto en marcha, llevado por un temporizador del MFP.
type fxTimer struct {
	voice int     // el canal (-1: parado)
	step  float64 // ticks del temporizador por muestra
	phase float64
	on    bool      // voz SID: medio ciclo encendido
	vol   float64   // voz SID: el volumen encendido
	data  []float64 // digidrum: la muestra
	shape int       // sync buzzer: la forma de la envolvente
}

// timerFreq es la frecuencia de un temporizador del MFP (0 si está parado).
func timerFreq(prediv byte, count byte) float64 {
	p := mfpPrediv[prediv&7]
	if p == 0 || count == 0 {
		return 0
	}
	return mfpClock / (p * float64(count))
}

// apply escribe los registros del cuadro y arranca sus efectos.
func (t *regTune) apply(r *[16]byte) {
	for i := 0; i < 13; i++ {
		t.chip.write(i, r[i])
	}
	// 0xff en la forma de la envolvente es "no la toques" (escribirla la
	// reinicia).
	if r[13] != 0xff {
		t.chip.write(13, r[13])
	}
	if t.s.fx == 0 {
		return
	}
	for i := range t.sid {
		t.sid[i].voice = -1
		t.chip.ch[i].override = -1
	}
	t.buzz.voice = -1
	if t.s.fx == 5 {
		if code := int(r[1]>>4) & 3; code != 0 {
			t.startSID(code-1, timerFreq(r[6]>>5, r[14]), r[7+code])
		}
		if code := int(r[3]>>4) & 3; code != 0 {
			t.startDrum(code-1, int(r[7+code]&31), timerFreq(r[8]>>5, r[15]))
		}
		return
	}
	for _, e := range [2][3]int{{1, 6, 14}, {3, 8, 15}} {
		code := r[e[0]] & 0xf0
		if code&0x30 == 0 {
			continue
		}
		voice := int(code&0x30>>4) - 1
		freq := timerFreq(r[e[1]]>>5, r[e[2]])
		switch code & 0xc0 {
		case 0x00, 0x80: // SID (y SID senoidal)
			t.startSID(voice, freq, r[8+voice])
		case 0x40:
			t.startDrum(voice, int(r[8+voice]&31), freq)
		case 0xc0: // sync buzzer
			if freq > 0 {
				t.buzz = fxTimer{voice: voice, step: freq / t.rate, shape: int(r[13] & 15)}
			}
		}
	}
}

func (t *regTune) startSID(voice int, freq float64, vol byte) {
	if voice < 0 || voice > 2 || freq <= 0 {
		return
	}
	f := &t.sid[voice]
	if f.voice < 0 {
		f.phase, f.on = 0, true
	}
	f.voice, f.step, f.vol = voice, freq/t.rate, t.chip.dac[int(vol&15)*2+1]
}

func (t *regTune) startDrum(voice, n int, freq float64) {
	if voice < 0 || voice > 2 || n >= len(t.s.drums) || freq <= 0 {
		return
	}
	t.drum[voice] = fxTimer{voice: voice, step: freq / t.rate, data: t.s.drums[n]}
}

// effects avanza los efectos una muestra.
func (t *regTune) effects() {
	for i := range t.sid {
		f := &t.sid[i]
		if f.voice < 0 {
			continue
		}
		f.phase += f.step
		for f.phase >= 1 {
			f.phase--
			f.on = !f.on
		}
		if t.drum[i].voice < 0 {
			t.chip.ch[i].override = 0
			if f.on {
				t.chip.ch[i].override = f.vol
			}
		}
	}
	for i := range t.drum {
		d := &t.drum[i]
		if d.voice < 0 {
			continue
		}
		p := int(d.phase)
		if p >= len(d.data) {
			d.voice = -1
			t.chip.ch[i].override = -1
			continue
		}
		t.chip.ch[i].override = d.data[p]
		d.phase += d.step
	}
	if t.buzz.voice >= 0 {
		t.buzz.phase += t.buzz.step
		for t.buzz.phase >= 1 {
			t.buzz.phase--
			t.chip.setShape(t.buzz.shape)
		}
	}
}

// Skip avanza frames cuadros sin sonar: solo escribe los registros.
func (t *regTune) Skip(frames int) {
	for f := float64(frames); f > 0; {
		if t.left <= 0 {
			if t.frame >= len(t.s.frames) {
				return
			}
			t.apply(&t.s.frames[t.frame])
			t.frame++
			t.left += t.per
		}
		k := min(t.left, f)
		t.left -= k
		f -= k
	}
}

func (t *regTune) Render(dst []float32) int {
	n := 0
	for ; n+1 < len(dst); n += 2 {
		if t.left <= 0 {
			if t.frame >= len(t.s.frames) {
				return n
			}
			t.apply(&t.s.frames[t.frame])
			t.frame++
			t.left += t.per
		}
		t.left--
		if t.s.fx != 0 {
			t.effects()
		}
		l, r := t.chip.sample()
		dst[n] = clamp1(t.dc[0].next(l) * 1.2)
		dst[n+1] = clamp1(t.dc[1].next(r) * 1.2)
	}
	return n
}
