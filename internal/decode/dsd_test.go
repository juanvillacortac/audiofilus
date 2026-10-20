package decode

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// dsdTone codifica, con un modulador sigma-delta de segundo orden, n bits
// de un seno de freq Hz y amplitud amp (1 es modulación completa) a rate.
func dsdTone(freq, amp float64, rate, n int) []bool {
	bits := make([]bool, n)
	var i1, i2, y float64
	for t := range bits {
		x := amp * math.Sin(2*math.Pi*freq*float64(t)/float64(rate))
		i1 += x - y
		i2 += i1 - y
		y = -1
		if i2 >= 0 {
			y = 1
		}
		bits[t] = y > 0
	}
	return bits
}

// packBits junta los bits en bytes, con el primero en el bit más
// significativo (msb) o en el menos.
func packBits(bits []bool, msb bool) []byte {
	out := make([]byte, (len(bits)+7)/8)
	for t, b := range bits {
		if !b {
			continue
		}
		if msb {
			out[t/8] |= 1 << (7 - t%8)
		} else {
			out[t/8] |= 1 << (t % 8)
		}
	}
	return out
}

// writeDSF escribe un DSF estéreo (bloques de 4096 bytes, primer bit en el
// menos significativo) con los bits de cada canal.
func writeDSF(t *testing.T, path string, rate int, chans [][]bool) {
	t.Helper()
	const block = 4096
	le := binary.LittleEndian
	n := len(chans[0])
	packed := make([][]byte, len(chans))
	for c := range chans {
		packed[c] = packBits(chans[c], false)
	}
	groups := (len(packed[0]) + block - 1) / block
	var data []byte
	for g := 0; g < groups; g++ {
		for c := range packed {
			blk := make([]byte, block)
			copy(blk, packed[c][min(g*block, len(packed[c])):])
			data = append(data, blk...)
		}
	}
	b := make([]byte, 28+52+12)
	copy(b, "DSD ")
	le.PutUint64(b[4:], 28)
	le.PutUint64(b[12:], uint64(len(b)+len(data)))
	copy(b[28:], "fmt ")
	le.PutUint64(b[32:], 52)
	fm := b[40:]
	le.PutUint32(fm[0:], 1)
	le.PutUint32(fm[4:], 0)
	le.PutUint32(fm[8:], 2)
	le.PutUint32(fm[12:], uint32(len(chans)))
	le.PutUint32(fm[16:], uint32(rate))
	le.PutUint32(fm[20:], 1)
	le.PutUint64(fm[24:], uint64(n))
	le.PutUint32(fm[32:], block)
	copy(b[80:], "data")
	le.PutUint64(b[84:], uint64(12+len(data)))
	if err := os.WriteFile(path, append(b, data...), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeDFF escribe un DSDIFF sin comprimir (un byte de cada canal por turno,
// primer bit en el más significativo).
func writeDFF(t *testing.T, path string, rate int, chans [][]bool) {
	t.Helper()
	be := binary.BigEndian
	packed := make([][]byte, len(chans))
	for c := range chans {
		packed[c] = packBits(chans[c], true)
	}
	var data []byte
	for i := range packed[0] {
		for c := range packed {
			data = append(data, packed[c][i])
		}
	}
	chunk := func(id string, body []byte) []byte {
		h := make([]byte, 12)
		copy(h, id)
		be.PutUint64(h[4:], uint64(len(body)))
		out := append(h, body...)
		if len(body)%2 == 1 {
			out = append(out, 0)
		}
		return out
	}
	fs := make([]byte, 4)
	be.PutUint32(fs, uint32(rate))
	chnl := make([]byte, 2+4*len(chans))
	be.PutUint16(chnl, uint16(len(chans)))
	prop := append([]byte("SND "), chunk("FS  ", fs)...)
	prop = append(prop, chunk("CHNL", chnl)...)
	prop = append(prop, chunk("CMPR", append([]byte("DSD "), 0))...)
	body := append([]byte("DSD "), chunk("FVER", []byte{1, 5, 0, 0})...)
	body = append(body, chunk("PROP", prop)...)
	body = append(body, chunk("DSD ", data)...)
	hdr := make([]byte, 12)
	copy(hdr, "FRM8")
	be.PutUint64(hdr[4:], uint64(len(body)))
	if err := os.WriteFile(path, append(hdr, body...), 0o644); err != nil {
		t.Fatal(err)
	}
}

// crossRate cuenta los cruces por cero de un canal y da su frecuencia.
func crossRate(s []float32, ch, rate int) (freq, rms float64) {
	cross, sum, n := 0, 0.0, 0
	prev := float32(0)
	for i := ch; i < len(s); i += 2 {
		v := s[i]
		if i > ch && (prev < 0) != (v < 0) {
			cross++
		}
		prev = v
		sum += float64(v) * float64(v)
		n++
	}
	return float64(cross) / 2 / (float64(n) / float64(rate)), math.Sqrt(sum / float64(n))
}

// openDSD abre un DSD de prueba con su constructor.
func openDSD(t *testing.T, path string) Source {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var src Source
	if filepath.Ext(path) == ".dsf" {
		src, err = newDSF(f)
	} else {
		src, err = newDFF(f)
	}
	if err != nil {
		f.Close()
		t.Fatal(err)
	}
	return src
}

// TestDSD comprueba DSF y DFF, en DSD64 y DSD128: la frecuencia de salida,
// la duración, que cada canal tenga su tono (1 kHz a la izquierda, 2 kHz a
// la derecha) con el nivel esperado (modulación del 50 % con -6 dB de
// ganancia: un seno de amplitud 0,25), y que un salto caiga en la muestra
// exacta, igual que leyendo de corrido.
func TestDSD(t *testing.T) {
	for _, c := range []struct {
		ext  string
		mult int
	}{{".dsf", 1}, {".dff", 1}, {".dsf", 2}, {".dff", 2}} {
		rate := dsdBase * c.mult
		n := rate / 2 // medio segundo
		path := filepath.Join(t.TempDir(), "tone"+c.ext)
		chans := [][]bool{dsdTone(1000, 0.5, rate, n), dsdTone(2000, 0.5, rate, n)}
		if c.ext == ".dsf" {
			writeDSF(t, path, rate, chans)
		} else {
			writeDFF(t, path, rate, chans)
		}
		src := openDSD(t, path)
		if src.SampleRate() != 44100 {
			t.Errorf("%s ×%d: SampleRate = %d, want 44100", c.ext, c.mult, src.SampleRate())
		}
		if got, want := src.Length(), int64(44100/2); got != want {
			t.Errorf("%s ×%d: Length = %d, want %d", c.ext, c.mult, got, want)
		}
		all := readAll(t, src)
		if len(all)/2 != 44100/2 {
			t.Errorf("%s ×%d: decoded %d frames, want %d", c.ext, c.mult, len(all)/2, 44100/2)
		}
		body := all[2*2000:] // sin el arranque de los filtros
		for ch, want := range []float64{1000, 2000} {
			f, rms := crossRate(body, ch, 44100)
			if math.Abs(f-want) > want*0.02 {
				t.Errorf("%s ×%d ch%d: tone at %.0f Hz, want %.0f", c.ext, c.mult, ch, f, want)
			}
			if r := 0.25 / math.Sqrt2; math.Abs(rms-r) > r*0.1 {
				t.Errorf("%s ×%d ch%d: RMS %.3f, want ~%.3f", c.ext, c.mult, ch, rms, r)
			}
		}
		const at = 12345
		if err := src.SeekFrame(at); err != nil {
			t.Fatal(err)
		}
		if src.Position() != at {
			t.Errorf("%s ×%d: Position after seek = %d, want %d", c.ext, c.mult, src.Position(), at)
		}
		buf := make([]float32, 64)
		if _, err := src.Read(buf); err != nil {
			t.Fatal(err)
		}
		for i, v := range buf {
			if math.Abs(float64(v-all[2*at+i])) > 1e-5 {
				t.Fatalf("%s ×%d: after seeking, sample %d = %f, want %f", c.ext, c.mult, i, v, all[2*at+i])
			}
		}
		src.Close()
	}
}

// TestDSDRejectsDST comprueba que un DFF comprimido (DST) se rechace.
func TestDSDRejectsDST(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dst.dff")
	writeDFF(t, path, dsdBase, [][]bool{dsdTone(1000, 0.5, dsdBase, 8000)})
	b, _ := os.ReadFile(path)
	for i := 0; i+8 <= len(b); i++ {
		if string(b[i:i+4]) == "CMPR" {
			copy(b[i+12:], "DST ")
		}
	}
	_ = os.WriteFile(path, b, 0o644)
	f, _ := os.Open(path)
	defer f.Close()
	if _, err := newDFF(f); err == nil {
		t.Error("a DST-compressed DFF was accepted")
	}
	if !isDSD(".DSF") || !isDSD(".dff") || isDSD(".flac") {
		t.Error("isDSD is wrong")
	}
}

// BenchmarkDSD64 mide cuánto cuesta decodificar un segundo de DSD64 estéreo
// (ver "x realtime").
func BenchmarkDSD64(b *testing.B) {
	path := filepath.Join(b.TempDir(), "bench.dsf")
	t := &testing.T{}
	writeDSF(t, path, dsdBase, [][]bool{dsdTone(1000, 0.5, dsdBase, dsdBase), dsdTone(440, 0.5, dsdBase, dsdBase)})
	f, err := os.Open(path)
	if err != nil {
		b.Fatal(err)
	}
	src, err := newDSF(f)
	if err != nil {
		b.Fatal(err)
	}
	defer src.Close()
	buf := make([]float32, 4096)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := src.SeekFrame(0); err != nil {
			b.Fatal(err)
		}
		for {
			if _, err := src.Read(buf); err != nil {
				break
			}
		}
	}
	b.ReportMetric(1/(b.Elapsed().Seconds()/float64(b.N)), "x_realtime")
}
