package chip

import (
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// bitWriter escribe bits de mayor a menor peso (como los lee bitReader).
type bitWriter struct {
	b []byte
	n int
}

func (w *bitWriter) put(v, bits int) {
	for i := bits - 1; i >= 0; i-- {
		if w.n%8 == 0 {
			w.b = append(w.b, 0)
		}
		w.b[len(w.b)-1] |= byte(v>>i&1) << (7 - w.n%8)
		w.n++
	}
}

// lh5Encode comprime data con -lh5- de la forma más simple que el formato
// permite: los literales con 9 bits ("1" y el byte) y, si copyAt >= 0, una
// copia de 3 bytes a distancia 1 justo antes del byte copyAt, con código
// "0". Basta para probar el decodificador sin un compresor.
func lh5Encode(data []byte, copyAt int) []byte {
	var codes []int // un byte, o -1 para la copia
	for i, b := range data {
		if i == copyAt {
			codes = append(codes, -1)
		}
		codes = append(codes, int(b))
	}
	w := &bitWriter{}
	for start := 0; start < len(codes); start += 0xffff {
		block := codes[start:min(len(codes), start+0xffff)]
		w.put(len(block), 16)
		// La tabla de los largos: los símbolos 3 (largo 1) y 11 (largo
		// 9), con un bit cada uno: 3 es "0" y 11 es "1".
		w.put(12, lhTBit)
		for i, l := range []int{0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 1} {
			w.put(l, 3)
			if i == 2 {
				w.put(0, 2) // ningún cero más tras el tercero
			}
		}
		// Los largos de los 257 primeros símbolos: los literales con 9
		// ("1"), la copia de 3 bytes (256) con 1 ("0").
		w.put(257, lhCBit)
		for i := 0; i < 256; i++ {
			w.put(1, 1)
		}
		w.put(0, 1)
		// Las distancias: un solo símbolo, el 1 (distancia 1).
		w.put(0, lhPBit)
		w.put(1, lhPBit)
		for _, c := range block {
			if c < 0 {
				w.put(0, 1)
			} else {
				w.put(0x100|c, 9)
			}
		}
	}
	return w.b
}

// lh5Literals comprime data solo con literales (para envolver archivos).
func lh5Literals(data []byte) []byte { return lh5Encode(data, -1) }

// lhaWrap arma un archivo LHA de nivel 0 con data comprimido.
func lhaWrap(data []byte) []byte {
	packed := lh5Literals(data)
	name := "song.ym"
	h := make([]byte, 22+len(name)+2)
	h[0] = byte(len(h) - 2)
	copy(h[2:], "-lh5-")
	binary.LittleEndian.PutUint32(h[7:], uint32(len(packed)))
	binary.LittleEndian.PutUint32(h[11:], uint32(len(data)))
	h[20] = 0
	h[21] = byte(len(name))
	copy(h[22:], name)
	return append(h, packed...)
}

// TestLH5 comprueba que -lh5- descomprima literales y copias: "AB" y una
// copia de 3 a distancia 1 (desde la "A") dan "ABABA".
func TestLH5(t *testing.T) {
	// La copia va tras "AB": el codificador mete el código de copia antes
	// del byte 2 y los bytes que siguen son los literales que quedan.
	packed := lh5Encode([]byte("ABC"), 2)
	got, err := lh5Decode(packed, 6)
	if err != nil || string(got) != "ABABAC" {
		t.Fatalf("lh5Decode = %q, %v", got, err)
	}
}

// zeroCrossings cuenta los cruces por cero (de subida) del canal izquierdo.
func zeroCrossings(s []float32) int {
	n := 0
	for i := 2; i < len(s); i += 2 {
		if s[i-2] < 0 && s[i] >= 0 {
			n++
		}
	}
	return n
}

// TestAYTone comprueba la frecuencia del tono del AY: con un periodo de 250
// a 2 MHz suena a 2000000/(16·250) = 500 Hz.
func TestAYTone(t *testing.T) {
	a := newAY(2000000, 48000, true, panMono)
	a.write(0, 250)
	a.write(1, 0)
	a.write(7, 0x3e) // solo el tono A
	a.write(8, 15)
	var d dcBlock
	buf := make([]float32, 48000*2)
	for i := 0; i < len(buf); i += 2 {
		l, _ := a.sample()
		buf[i] = float32(d.next(l))
	}
	if n := zeroCrossings(buf[19200:]); math.Abs(float64(n)-400) > 4 {
		t.Errorf("%d cycles in 0.8 s, want 400 (500 Hz)", n)
	}
}

// ymFile arma un YM5 de n cuadros (sin comprimir) con un tono en el canal A.
func ymFile(n int) []byte {
	b := []byte("YM5!LeOnArD!")
	b = binary.BigEndian.AppendUint32(b, uint32(n))
	b = binary.BigEndian.AppendUint32(b, ymInterleaved)
	b = binary.BigEndian.AppendUint16(b, 0) // digidrums
	b = binary.BigEndian.AppendUint32(b, 2000000)
	b = binary.BigEndian.AppendUint16(b, 50)
	b = binary.BigEndian.AppendUint32(b, 0) // cuadro del bucle
	b = binary.BigEndian.AppendUint16(b, 0) // datos extra
	b = append(b, "Test Tune\x00Tester\x00Synthetic\x00"...)
	regs := [16]byte{0: 250, 7: 0x3e, 8: 15, 13: 0xff}
	for r := 0; r < 16; r++ {
		for f := 0; f < n; f++ {
			b = append(b, regs[r])
		}
	}
	return append(b, "End!"...)
}

// render lee una canción entera (como mucho max segundos).
func render(t *testing.T, path string, song int, max float64) []float32 {
	t.Helper()
	tune, _, err := Open(path, song, 48000)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]float32, int(max*48000)*2)
	return buf[:tune.Render(buf)]
}

// TestYM comprueba un YM5, sin comprimir y comprimido con LHA: el título y
// el autor de la cabecera, lo que dura (100 cuadros a 50 Hz: 2 s) y su tono
// de 500 Hz.
func TestYM(t *testing.T) {
	dir := t.TempDir()
	raw := ymFile(100)
	for name, data := range map[string][]byte{"raw.ym": raw, "packed.ym": lhaWrap(raw)} {
		path := filepath.Join(dir, name)
		os.WriteFile(path, data, 0o644)
		ts, err := Tracks(path)
		if err != nil || len(ts) != 1 || ts[0].Title != "Test Tune" || ts[0].Author != "Tester" ||
			ts[0].Length != 2*time.Second || ts[0].Loops {
			t.Fatalf("%s: tracks = %+v, %v", name, ts, err)
		}
		s := render(t, path, 1, 5)
		if len(s) != 2*96000 {
			t.Errorf("%s: %d frames, want 96000", name, len(s)/2)
		}
		if n := zeroCrossings(s[9600:]); math.Abs(float64(n)-950) > 9 {
			t.Errorf("%s: %d cycles in 1.9 s, want 950", name, n)
		}
	}
}

// TestVTX comprueba un VTX: su cabecera (chip, estéreo, reloj, Hz, textos)
// y sus registros comprimidos.
func TestVTX(t *testing.T) {
	n := 50
	regs := make([]byte, 14*n)
	for f := 0; f < n; f++ {
		regs[0*n+f], regs[7*n+f], regs[8*n+f], regs[13*n+f] = 250, 0x3e, 15, 0xff
	}
	b := []byte("ay\x01")
	b = binary.LittleEndian.AppendUint16(b, 0)
	b = binary.LittleEndian.AppendUint32(b, 1773400)
	b = append(b, 50)
	b = binary.LittleEndian.AppendUint16(b, 1990)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(regs)))
	b = append(b, "Tune\x00Author\x00Game\x00Tracker\x00\x00"...)
	b = append(b, lh5Literals(regs)...)
	path := filepath.Join(t.TempDir(), "t.vtx")
	os.WriteFile(path, b, 0o644)
	ts, err := Tracks(path)
	if err != nil || ts[0].Title != "Tune" || ts[0].Author != "Author" || ts[0].Game != "Game" || ts[0].Length != time.Second {
		t.Fatalf("tracks = %+v, %v", ts, err)
	}
	s := render(t, path, 1, 3)
	// 1773400/(16·250) = 443,35 Hz.
	if n := zeroCrossings(s[19200:]); math.Abs(float64(n)-354.7) > 5 || len(s) != 96000 {
		t.Errorf("%d cycles in 0.8 s (want ~355), %d frames", n, len(s)/2)
	}
}

// pt3File arma un módulo PT3 de un patrón: el canal A toca la nota 48 con la
// muestra 1 y luego rows filas vacías; B y C callan.
func pt3File(rows int) []byte {
	m := make([]byte, 0x200)
	copy(m, "ProTracker 3.5 compilation of ")
	copy(m[0x1e:], "Test PT3")
	copy(m[0x3e:], " by ")
	copy(m[0x42:], "Tester")
	for i := 0x1e + 8; i < 0x3e; i++ {
		m[i] = ' '
	}
	for i := 0x42 + 6; i < 0x63; i++ {
		m[i] = ' '
	}
	m[pt3Table], m[pt3Delay], m[pt3Loop] = 0, 3, 0
	put := func(at, v int) { binary.LittleEndian.PutUint16(m[at:], uint16(v)) }
	const pat, smp, orn, chA, chB = 0x100, 0x110, 0x118, 0x120, 0x1f0
	put(pt3Patterns, pat)
	put(pt3Samples+2, smp)
	put(pt3Ornaments, orn)
	m[pt3Positions], m[pt3Positions+1] = 0, 0xff
	put(pat, chA)
	put(pat+2, chB)
	put(pat+4, chB)
	// La muestra 1: un paso, volumen 15, tono sí, ruido no.
	copy(m[smp:], []byte{0, 1, 0x00, 0x8f, 0, 0})
	copy(m[orn:], []byte{0, 1, 0})
	a := []byte{0xb1, 1, 0xd1, 0x50 + 48}
	for i := 0; i < rows; i++ {
		a = append(a, 0xd0)
	}
	copy(m[chA:], append(a, 0))
	copy(m[chB:], []byte{0xb1, 0xff, 0xd0})
	return m
}

// TestPT3 comprueba un PT3: el título y el autor, lo que dura (una vuelta:
// 1 + 20 filas de 3 ticks a 50 Hz, y el tick en que vuelve) y la nota 48
// de la tabla de la 3.5 (periodo 0xC2: 1773400/(16·194) = 571 Hz).
func TestPT3(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.pt3")
	os.WriteFile(path, pt3File(20), 0o644)
	ts, err := Tracks(path)
	want := time.Duration(21*3+1) * time.Second / 50
	if err != nil || ts[0].Title != "Test PT3" || ts[0].Author != "Tester" || ts[0].Length != want {
		t.Fatalf("tracks = %+v (want %v), %v", ts, want, err)
	}
	s := render(t, path, 1, 3)
	if n := zeroCrossings(s[9600:76800]); math.Abs(float64(n)-571.4*0.7) > 6 {
		t.Errorf("%d cycles in 0.7 s, want ~400", n)
	}
}

// sidFile arma un PSID con dos canciones: la rutina de inicio pone el
// volumen, una envolvente con sostenido máximo y una sierra en la voz 1 (en
// la canción 2, a doble frecuencia); play solo vuelve. Si irq, en vez de
// play, init instala en $0314 una interrupción que cuenta en $2000.
func sidFile(irq bool) []byte {
	h := make([]byte, 0x7c)
	copy(h, "PSID")
	be := binary.BigEndian
	be.PutUint16(h[4:], 2)
	be.PutUint16(h[6:], 0x7c)
	be.PutUint16(h[8:], 0x1000)
	be.PutUint16(h[10:], 0x1000)
	if !irq {
		be.PutUint16(h[12:], 0x1040)
	}
	be.PutUint16(h[14:], 2)
	be.PutUint16(h[16:], 1)
	copy(h[0x16:], "Synth Test")
	copy(h[0x36:], "Tester")
	be.PutUint16(h[0x76:], 0x14) // PAL, 6581
	code := []byte{
		0xaa,                         // TAX
		0xa9, 0x0f, 0x8d, 0x18, 0xd4, // LDA #$0F, STA $D418
		0xa9, 0x00, 0x8d, 0x05, 0xd4, // AD
		0xa9, 0xf0, 0x8d, 0x06, 0xd4, // SR
		0xa9, 0x45, 0x8d, 0x00, 0xd4, // la frecuencia, bajo
		0xbd, 0x38, 0x10, 0x8d, 0x01, 0xd4, // LDA $1038,X: alto
		0xa9, 0x21, 0x8d, 0x04, 0xd4, // sierra y puerta
	}
	if irq {
		code = append(code,
			0x78,                         // SEI
			0xa9, 0x50, 0x8d, 0x14, 0x03, // $0314 = $1050
			0xa9, 0x10, 0x8d, 0x15, 0x03,
			0x58, // CLI
		)
	}
	code = append(code, 0x60)
	data := make([]byte, 0x60)
	copy(data, code)
	data[0x38], data[0x39] = 0x1d, 0x3a
	data[0x40] = 0x60
	// La interrupción: INC $2000, JMP $EA31.
	copy(data[0x50:], []byte{0xee, 0x00, 0x20, 0x4c, 0x31, 0xea})
	return append(h, data...)
}

// TestSID comprueba un PSID: sus dos canciones con el nombre como juego y el
// autor, 3 minutos que se apagan si no se sabe lo que duran, y la sierra de
// la voz 1 en cada una (0x1D45·985248/2²⁴ = 440 Hz; la 2, a 876 Hz).
func TestSID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.sid")
	os.WriteFile(path, sidFile(false), 0o644)
	ts, err := Tracks(path)
	if err != nil || len(ts) != 2 || ts[0].Game != "Synth Test" || ts[0].Author != "Tester" ||
		ts[0].System != "Commodore 64" || ts[1].Length != sidDefault || !ts[1].Loops {
		t.Fatalf("tracks = %+v, %v", ts, err)
	}
	for song, hz := range map[int]float64{1: 440, 2: 876} {
		s := render(t, path, song, 1)
		if n := zeroCrossings(s[19200:]); math.Abs(float64(n)-hz*0.8) > hz*0.02 {
			t.Errorf("song %d: %d cycles in 0.8 s, want %.0f", song, n, hz*0.8)
		}
	}
}

// TestSIDInterrupt comprueba un PSID sin rutina de tocar: la que instala en
// $0314 la llama la interrupción de 60 Hz del CIA 1, a través del KERNAL de
// mentira, unas 60 veces por segundo.
func TestSIDInterrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.sid")
	os.WriteFile(path, sidFile(true), 0o644)
	tune, _, err := Open(path, 1, 48000)
	if err != nil {
		t.Fatal(err)
	}
	tune.Render(make([]float32, 96000))
	if n := tune.(*c64).ram[0x2000]; n < 57 || n > 63 {
		t.Errorf("%d interrupts in 1 s, want ~60", n)
	}
}

// TestSonglengths comprueba que, con la lista de la HVSC
// (DOCUMENTS/Songlengths.md5 unas carpetas más arriba), cada canción dure
// lo que dice y no se apague.
func TestSonglengths(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "MUSICIANS", "T", "Tester")
	os.MkdirAll(dir, 0o755)
	os.MkdirAll(filepath.Join(root, "DOCUMENTS"), 0o755)
	data := sidFile(false)
	sum := md5.Sum(data)
	path := filepath.Join(dir, "t.sid")
	os.WriteFile(path, data, 0o644)
	db := "[Database]\n; /MUSICIANS/T/Tester/t.sid\n" + hex.EncodeToString(sum[:]) + "=0:02 1:05.500(G)\n"
	os.WriteFile(filepath.Join(root, "DOCUMENTS", "Songlengths.md5"), []byte(db), 0o644)
	ts, err := Tracks(path)
	if err != nil || ts[0].Length != 2*time.Second || ts[0].Loops || ts[1].Length != 65500*time.Millisecond {
		t.Fatalf("tracks = %+v, %v", ts, err)
	}
}

// TestBadFiles comprueba que lo que no es de estos formatos dé error (y no
// un pánico).
func TestBadFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.sid", "a.ym", "a.vtx", "a.pt3"} {
		for _, data := range []string{"", "x", strings.Repeat("\xff", 300), "PSID" + strings.Repeat("\x00", 200), "YM5!LeOnArD!" + strings.Repeat("\x7f", 40)} {
			path := filepath.Join(dir, name)
			os.WriteFile(path, []byte(data), 0o644)
			if _, err := Tracks(path); err == nil {
				t.Errorf("%s with %q: no error", name, data[:min(len(data), 12)])
			}
		}
	}
}
