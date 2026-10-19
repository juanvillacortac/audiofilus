package decode

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mewkiz/flac"
	"github.com/mewkiz/flac/frame"
	"github.com/mewkiz/flac/meta"
)

// Los archivos de testdata son un seno de 440 Hz de 2 s a 44,1 kHz, salvo
// mono24.wav (1 s, mono, 24 bits, 48 kHz) y opus.ogg (códec no soportado).
// Se generan con ffmpeg; ver testdata/README.md.

func readAll(t *testing.T, s Source) []float32 {
	t.Helper()
	var out []float32
	buf := make([]float32, 1000) // no es múltiplo del tamaño de bloque de ningún códec
	for {
		n, err := s.Read(buf)
		if n%2 != 0 {
			t.Fatalf("Read returned an odd sample count %d", n)
		}
		out = append(out, buf[:n]...)
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
	}
}

func peak(s []float32) float64 {
	var p float64
	for _, v := range s {
		p = math.Max(p, math.Abs(float64(v)))
	}
	return p
}

// TestDecodeFormats comprueba que cada formato dé la duración, la frecuencia y
// un nivel de señal correctos, y que Position cuente lo leído.
func TestDecodeFormats(t *testing.T) {
	for _, name := range []string{"tone.mp3", "tone.ogg", "tone.flac", "tone.wav", "mono24.wav", "tone.aiff"} {
		t.Run(name, func(t *testing.T) {
			s, err := Open("testdata/" + name)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			wantRate, wantSec := 44100, 2.0
			if name == "mono24.wav" {
				wantRate, wantSec = 48000, 1.0
			}
			if s.SampleRate() != wantRate {
				t.Errorf("SampleRate = %d, want %d", s.SampleRate(), wantRate)
			}
			samples := readAll(t, s)
			gotSec := float64(len(samples)/2) / float64(wantRate)
			// MP3 y Vorbis añaden relleno del codificador; se tolera 0,1 s.
			if math.Abs(gotSec-wantSec) > 0.1 {
				t.Errorf("decoded %.3f s, want ~%.1f s", gotSec, wantSec)
			}
			if p := peak(samples); p < 0.05 || p > 1.01 {
				t.Errorf("peak %.3f out of range: decoding or scaling is wrong", p)
			}
			if s.Position() != int64(len(samples)/2) {
				t.Errorf("Position = %d, want %d", s.Position(), len(samples)/2)
			}
		})
	}
}

// TestSeek comprueba que saltar a 1 s deje la posición exacta y el resto del audio.
func TestSeek(t *testing.T) {
	for _, name := range []string{"tone.mp3", "tone.ogg", "tone.flac", "tone.wav", "tone.aiff"} {
		t.Run(name, func(t *testing.T) {
			s, err := Open("testdata/" + name)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			target := int64(s.SampleRate()) // 1 s
			if err := s.SeekFrame(target); err != nil {
				t.Fatal(err)
			}
			if s.Position() != target {
				t.Errorf("Position after seek = %d, want %d", s.Position(), target)
			}
			rest := readAll(t, s)
			if got := float64(len(rest)/2) / float64(s.SampleRate()); math.Abs(got-1) > 0.1 {
				t.Errorf("after seeking to 1 s, %.3f s remain, want ~1", got)
			}
		})
	}
}

// TestOpusWithoutLibrary comprueba que, sin las bibliotecas en C, un Opus
// falle con ErrUnsupported en vez de reproducir basura.
func TestOpusWithoutLibrary(t *testing.T) {
	t.Setenv("AUDIOFILUS_DECODER", "go")
	_, err := Open("testdata/opus.ogg")
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
}

// needLib salta el test si la biblioteca de ese formato no está en el sistema.
func needLib(t *testing.T, load func() error) {
	t.Helper()
	if err := load(); err != nil {
		t.Skipf("skipping: %v", err)
	}
}

// TestOpus comprueba la duración, la frecuencia (siempre 48 kHz), el nivel y el
// salto de un Opus (con libopusfile).
func TestOpus(t *testing.T) {
	needLib(t, loadOpusfile)
	for _, name := range []string{"tone.opus", "opus.ogg"} {
		s, err := Open("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if s.SampleRate() != 48000 {
			t.Errorf("%s: SampleRate = %d, want 48000", name, s.SampleRate())
		}
		samples := readAll(t, s)
		if p := peak(samples); p < 0.05 || p > 1.01 {
			t.Errorf("%s: peak %.3f out of range", name, p)
		}
		if name == "tone.opus" {
			if got := float64(len(samples)/2) / 48000; math.Abs(got-2) > 0.1 {
				t.Errorf("decoded %.3f s, want ~2", got)
			}
			if err := s.SeekFrame(48000); err != nil || s.Position() != 48000 {
				t.Errorf("seek to 1 s: position %d, err %v", s.Position(), err)
			}
		}
		s.Close()
	}
}

// TestModule comprueba que un módulo tracker (un MOD mínimo hecho aquí) suene,
// dure lo que dice y termine (con libxmp).
func TestModule(t *testing.T) {
	needLib(t, loadXMP)
	path := filepath.Join(t.TempDir(), "tone.mod")
	if err := os.WriteFile(path, tinyMOD(), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	samples := readAll(t, s) // si el módulo no terminara, no volvería nunca
	sec := float64(len(samples)/2) / float64(s.SampleRate())
	// 64 filas a velocidad 6 y 125 BPM: 64 × 6 × 20 ms = 7,68 s.
	if math.Abs(sec-7.68) > 0.3 {
		t.Errorf("played %.2f s, want ~7.68", sec)
	}
	if want := float64(s.Length()) / float64(s.SampleRate()); math.Abs(want-7.68) > 0.3 {
		t.Errorf("Length %.2f s, want ~7.68", want)
	}
	if p := peak(samples); p < 0.01 {
		t.Errorf("peak %.3f: the module is silent", p)
	}
}

// tinyMOD es un ProTracker MOD de cuatro canales con una onda cuadrada que
// suena desde la primera fila de su único patrón.
func tinyMOD() []byte {
	var b []byte
	b = append(b, make([]byte, 20)...) // título
	for i := 0; i < 31; i++ {
		smp := make([]byte, 30)
		if i == 0 {
			binary.BigEndian.PutUint16(smp[22:], 32) // 32 palabras = 64 bytes
			smp[25] = 64                             // volumen
			binary.BigEndian.PutUint16(smp[28:], 32) // bucle de toda la muestra
		}
		b = append(b, smp...)
	}
	b = append(b, 1, 127) // un patrón en la canción
	b = append(b, make([]byte, 128)...)
	b = append(b, "M.K."...)
	pattern := make([]byte, 64*4*4)
	// Fila 0, canal 0: muestra 1, periodo 428 (Do).
	pattern[0], pattern[1], pattern[2] = 0x01, 0xAC, 0x10
	b = append(b, pattern...)
	for i := 0; i < 64; i++ {
		v := byte(0x60)
		if i >= 32 {
			v = 0xA0
		}
		b = append(b, v)
	}
	return b
}

// TestAAC comprueba un M4A con AAC (con libfdk-aac): frecuencia, duración,
// nivel y salto exacto.
func TestAAC(t *testing.T) {
	needLib(t, loadFDK)
	s, err := Open("testdata/tone.m4a")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.SampleRate() != 44100 {
		t.Errorf("SampleRate = %d, want 44100", s.SampleRate())
	}
	if got := float64(s.Length()) / 44100; math.Abs(got-2) > 0.1 {
		t.Errorf("Length %.3f s, want ~2", got)
	}
	samples := readAll(t, s)
	if got := float64(len(samples)/2) / 44100; math.Abs(got-2) > 0.1 {
		t.Errorf("decoded %.3f s, want ~2", got)
	}
	if p := peak(samples); p < 0.05 || p > 1.01 {
		t.Errorf("peak %.3f out of range", p)
	}
	if err := s.SeekFrame(44100); err != nil {
		t.Fatal(err)
	}
	rest := readAll(t, s)
	if got := float64(len(rest)/2) / 44100; math.Abs(got-1) > 0.1 {
		t.Errorf("after seeking to 1 s, %.3f s remain, want ~1", got)
	}
}

// TestALAC comprueba un M4A con Apple Lossless (en Go puro): sin pérdida, el
// tono sale con su nivel exacto, y el salto deja el resto.
func TestALAC(t *testing.T) {
	s, err := Open("testdata/alac.m4a")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.SampleRate() != 44100 {
		t.Errorf("SampleRate = %d, want 44100", s.SampleRate())
	}
	samples := readAll(t, s)
	if got := float64(len(samples)/2) / 44100; math.Abs(got-2) > 0.05 {
		t.Errorf("decoded %.3f s, want ~2", got)
	}
	// Sin pérdida: las muestras son las mismas que las del WAV del mismo tono.
	w, err := Open("testdata/tone.wav")
	if err != nil {
		t.Fatal(err)
	}
	ref := readAll(t, w)
	w.Close()
	for i := 0; i < min(len(ref), len(samples)); i++ {
		if math.Abs(float64(ref[i]-samples[i])) > 1e-6 {
			t.Fatalf("sample %d = %v, want %v (lossless)", i, samples[i], ref[i])
		}
	}
	if err := s.SeekFrame(44100); err != nil {
		t.Fatal(err)
	}
	if got := float64(len(readAll(t, s))/2) / 44100; math.Abs(got-1) > 0.05 {
		t.Errorf("after seeking to 1 s, %.3f s remain, want ~1", got)
	}
}

// TestALAC24Mono comprueba ALAC de 24 bits y mono (el decodificador original
// solo se había probado en estéreo de 16 bits).
func TestALAC24Mono(t *testing.T) {
	s, err := Open("testdata/alac24mono.m4a")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	samples := readAll(t, s)
	if got := float64(len(samples)/2) / float64(s.SampleRate()); s.SampleRate() != 48000 || math.Abs(got-1) > 0.05 {
		t.Errorf("rate %d, decoded %.3f s; want 48000 and ~1 s", s.SampleRate(), got)
	}
	if p := peak(samples); p < 0.05 || p > 1.01 {
		t.Errorf("peak %.3f out of range", p)
	}
	for i := 0; i+1 < len(samples); i += 2 {
		if samples[i] != samples[i+1] {
			t.Fatalf("mono should be the same on both sides (frame %d)", i/2)
		}
	}
}

// TestESDSCookie comprueba la "magic cookie" de AAC para AudioToolbox (iOS):
// la AudioSpecificConfig dentro de un descriptor ES con sus tamaños, como la
// arma FFmpeg.
func TestESDSCookie(t *testing.T) {
	asc := []byte{0x12, 0x10} // AAC-LC, 44,1 kHz, estéreo
	c := esdsCookie(asc)
	if len(c) != 5+3+5+13+5+len(asc) {
		t.Fatalf("cookie is %d bytes", len(c))
	}
	if c[0] != 0x03 || c[4] != byte(3+5+13+5+len(asc)) || c[8] != 0x04 || c[13] != 0x40 || c[26] != 0x05 || c[30] != byte(len(asc)) {
		t.Errorf("bad descriptors: % x", c)
	}
	if string(c[len(c)-2:]) != string(asc) {
		t.Errorf("the config is not at the end: % x", c)
	}
}

// fakeAAC es un aacCodec de prueba: apunta los bloques y saca uno de silencio
// por bloque.
type fakeAAC struct {
	asc     []byte
	packets *[][]byte
}

func (f *fakeAAC) decode(p []byte, out []float32) ([]float32, int, error) {
	*f.packets = append(*f.packets, append([]byte(nil), p...))
	return append(out, 0, 0), 44100, nil
}
func (f *fakeAAC) reset() {}
func (f *fakeAAC) close() {}

// adtsHeader arma una cabecera ADTS para un bloque de n bytes de datos.
func adtsHeader(profile, sf, ch byte, n int, crc bool) []byte {
	head := 7
	if crc {
		head = 9
	}
	size := head + n
	h := []byte{0xff, 0xf1, profile<<6 | sf<<2 | ch>>2, (ch&3)<<6 | byte(size>>11)&3, byte(size >> 3), byte(size&7)<<5 | 0x1f, 0xfc}
	if crc {
		h[1] = 0xf0
		h = append(h, 0, 0)
	}
	return h
}

// TestADTSStream comprueba el lector de ADTS de las radios sin libfdk-aac:
// separa los bloques aunque lleguen partidos y con basura delante, salta el
// CRC, saca la configuración de la cabecera (AAC-LC, 44,1 kHz, estéreo:
// 12 10) y rehace el decodificador si cambia.
func TestADTSStream(t *testing.T) {
	var packets [][]byte
	var ascs []string
	s := &adtsStream{newCodec: func(asc []byte) (aacCodec, error) {
		ascs = append(ascs, fmt.Sprintf("% x", asc))
		return &fakeAAC{asc: asc, packets: &packets}, nil
	}}
	var flow []byte
	flow = append(flow, 0x00, 0x13, 0x37) // basura, como al conectar a mitad
	flow = append(flow, adtsHeader(1, 4, 2, 3, false)...)
	flow = append(flow, 'a', 'b', 'c')
	flow = append(flow, adtsHeader(1, 4, 2, 2, true)...)
	flow = append(flow, 'd', 'e')
	flow = append(flow, adtsHeader(1, 3, 1, 1, false)...) // otra configuración: 48 kHz, mono
	flow = append(flow, 'f')
	dst := make([]float32, 64)
	for i := 0; i < len(flow); i += 4 { // a trozos de 4 bytes
		s.Feed(flow[i:min(i+4, len(flow))])
		if _, err := s.Decode(dst); err != nil {
			t.Fatal(err)
		}
	}
	if got := fmt.Sprintf("%q", packets); got != `["abc" "de" "f"]` {
		t.Errorf("packets %s", got)
	}
	if got := fmt.Sprint(ascs); got != "[12 10 11 88]" {
		t.Errorf("configs %s, want [12 10 11 88]", got)
	}
}

// writeRampFLAC escribe un FLAC estéreo de n muestras cuyo canal izquierdo
// vale i % 30000 en la muestra i: así se ve en qué muestra quedó un salto.
func writeRampFLAC(t *testing.T, path string, n int) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	info := &meta.StreamInfo{BlockSizeMin: 4096, BlockSizeMax: 4096, SampleRate: 44100, NChannels: 2, BitsPerSample: 16}
	enc, err := flac.NewEncoder(f, info)
	if err != nil {
		t.Fatal(err)
	}
	for start := 0; start < n; start += 4096 {
		size := min(4096, n-start)
		l, r := make([]int32, size), make([]int32, size)
		for i := range l {
			l[i] = int32((start + i) % 30000)
			r[i] = -l[i]
		}
		fr := &frame.Frame{
			Header: frame.Header{HasFixedBlockSize: true, BlockSize: uint16(size), SampleRate: 44100, Channels: frame.ChannelsLR, BitsPerSample: 16},
			Subframes: []*frame.Subframe{
				{SubHeader: frame.SubHeader{Pred: frame.PredVerbatim}, Samples: l, NSamples: size},
				{SubHeader: frame.SubHeader{Pred: frame.PredVerbatim}, Samples: r, NSamples: size},
			},
		}
		if err := enc.WriteFrame(fr); err != nil {
			t.Fatal(err)
		}
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestFLACSeekWithoutSeekTable comprueba que en un FLAC sin SEEKTABLE el
// salto (por bisección sobre las cabeceras) caiga en la muestra exacta, sea
// rápido, y que al final se pueda seguir leyendo.
func TestFLACSeekWithoutSeekTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ramp.flac")
	const n = 44100 * 60
	writeRampFLAC(t, path, n)
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Length() != n {
		t.Fatalf("Length = %d, want %d", s.Length(), n)
	}
	buf := make([]float32, 2)
	for _, target := range []int64{44100 * 45, 10, 4096 * 100, 4096*300 + 7, n - 5000, 44100 * 30, 0} {
		start := time.Now()
		if err := s.SeekFrame(target); err != nil {
			t.Fatalf("seek to %d: %v", target, err)
		}
		if d := time.Since(start); d > 200*time.Millisecond {
			t.Errorf("seek to %d took %v", target, d)
		}
		if s.Position() != target {
			t.Errorf("Position after seek = %d, want %d", s.Position(), target)
		}
		if _, err := s.Read(buf); err != nil {
			t.Fatal(err)
		}
		if got, want := int(math.Round(float64(buf[0])*32768)), int(target%30000); got != want {
			t.Errorf("after seeking to %d the sample is %d, want %d", target, got, want)
		}
	}
	if err := s.SeekFrame(n - 100); err != nil {
		t.Fatal(err)
	}
	if rest := readAll(t, s); len(rest)/2 != 100 {
		t.Errorf("after seeking to the end, %d samples remain, want 100", len(rest)/2)
	}
}
