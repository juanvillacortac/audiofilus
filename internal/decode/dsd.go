package decode

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
)

// DSD (el audio de 1 bit de los SACD) en sus dos archivos: DSF (Sony) y
// DFF/DSDIFF (Philips), sin comprimir. Se convierte a PCM a 44,1 kHz en
// Go puro, en dos etapas, como el clásico dsd2pcm:
//
//  1. Los bits pasan por un filtro paso bajo FIR de 288 coeficientes por
//     cada 2,8224 MHz (Kaiser: paso hasta 20 kHz, rechazo de unos 70 dB
//     desde 64,1 kHz, que es lo que se doblaría a lo audible) y se diezman
//     por 32 hasta 88,2 kHz. El truco que lo abarata: cada byte son 8 bits
//     ±1, así que la suma de esos 8 bits por sus 8 coeficientes se
//     precalcula para los 256 valores posibles del byte. Cada muestra
//     intermedia cuesta 36 lecturas de tabla (por canal, en DSD64) en vez de
//     288 productos.
//  2. Un segundo FIR, en coma flotante, de 96 coeficientes (Kaiser, paso
//     hasta 20 kHz y unos 70 dB desde 24,1 kHz) diezma por 2 hasta
//     44,1 kHz. Es simétrico, así que se suman las parejas y se multiplica
//     una vez: 48 productos por muestra de salida y canal.
//
// El compromiso: menos coeficientes gastarían menos (cuenta en una
// Cortex-A53), pero el ruido que el DSD empuja por encima de 20 kHz es muy
// fuerte y, mal filtrado, se dobla dentro de lo audible. Con estos filtros
// queda a unos 70 dB por debajo, que está bien para escuchar; no es una
// conversión de referencia. Ir primero a 88,2 kHz (y no a 352,8 kHz con un
// filtro corto, como dsd2pcm) sale unas 2,7 veces más barato: el filtro de
// bits es más largo, pero se calcula 4 veces menos, y la etapa en coma
// flotante, la cara, se queda en 48 productos. La salida a 44,1 kHz (y no
// a 88,2) es a propósito: el remuestreo del motor (lineal, sin filtro)
// doblaría a lo audible lo que hubiera entre 24 y 44 kHz. En DSD128 y
// DSD256 la primera etapa usa 2 y 4 veces más coeficientes y diezma 2 y 4
// veces más, para llegar a los mismos 88,2 kHz; la segunda es la misma.

// dsdBase es la frecuencia de DSD64; las demás son múltiplos (×2, ×4).
const dsdBase = 2822400

// Las medidas de los filtros (ver arriba).
const (
	dsdTaps1   = 288 // coeficientes de la primera etapa por cada ×1 de DSD64
	dsdStep1   = 4   // bytes (32 bits) por muestra intermedia, por cada ×1
	dsdTaps2   = 96  // coeficientes de la segunda etapa
	dsdDecim2  = 2   // 88,2 kHz → 44,1 kHz
	dsdOutRate = dsdBase / 64
	// dsdGain baja 6 dB: en DSD el 0 dB de referencia de los SACD es una
	// modulación del 50 %, así que a escala completa (todo unos) la señal
	// valdría el doble que un PCM a 0 dBFS. Así los niveles se parecen a los
	// del resto de la biblioteca y los picos no saturan.
	dsdGain = 0.5
	// dsdChunk son las muestras de salida que se calculan de una vez.
	dsdChunk = 512
)

// isDSD dice si ext (con punto, en cualquier caja) es de DSD.
func isDSD(ext string) bool {
	switch strings.ToLower(ext) {
	case ".dsf", ".dff":
		return true
	}
	return false
}

// dsdBytes da los bytes de cada canal en orden de tiempo, con el primer bit
// en el más significativo.
type dsdBytes interface {
	// read llena hasta n bytes de cada canal en chans; devuelve cuántos.
	read(chans [][]byte, n int) (int, error)
	// seek se pone en el byte b de cada canal.
	seek(b int64) error
}

// dsdSource es un archivo DSD decodificado a PCM.
type dsdSource struct {
	f     *os.File
	in    dsdBytes
	ch    int   // canales que se decodifican (1 o 2)
	m     int   // ×1, ×2 o ×4 (DSD64, 128, 256)
	total int64 // bytes de cada canal
	pos   int64 // próximo frame de salida

	tab1  [][256]float32 // las tablas de la primera etapa, una por byte del filtro
	h2    []float32      // la mitad de la segunda etapa (es simétrica)
	tail1 [2][]byte      // los últimos bytes de cada canal (historia de la etapa 1)
	tail2 [2][]float32   // las últimas muestras intermedias (historia de la etapa 2)
	raw   [][]byte
	ext1  [2][]byte
	ext2  [2][]float32
	out   []float32 // salida calculada que aún no se entregó (intercalada)
}

func newDSDSource(f *os.File, in dsdBytes, rate, ch int, total int64) (Source, error) {
	m := rate / dsdBase
	if rate%dsdBase != 0 || (m != 1 && m != 2 && m != 4) {
		return nil, fmt.Errorf("dsd: unsupported rate %d Hz: %w", rate, ErrUnsupported)
	}
	if ch < 1 {
		return nil, fmt.Errorf("dsd: no channels: %w", ErrUnsupported)
	}
	s := &dsdSource{f: f, in: in, ch: min(ch, 2), m: m, total: total}
	s.tab1, s.h2 = dsdFilters(m)
	s.raw = make([][]byte, ch)
	for c := range s.raw {
		s.raw[c] = make([]byte, dsdChunk*dsdDecim2*dsdStep1*m)
	}
	s.reset()
	return s, nil
}

// reset borra la historia de los filtros (al empezar y tras un salto).
func (s *dsdSource) reset() {
	for c := 0; c < 2; c++ {
		// El silencio en DSD es 0x69 (0110 1001): tantos unos como ceros.
		s.tail1[c] = bytesOf(0x69, len(s.tab1))
		s.tail2[c] = make([]float32, dsdTaps2)
	}
	s.out = s.out[:0]
}

func bytesOf(v byte, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = v
	}
	return b
}

func (s *dsdSource) SampleRate() int { return dsdOutRate }

func (s *dsdSource) Length() int64 { return s.total / int64(s.perFrame()) }

// perFrame son los bytes de cada canal por muestra de salida.
func (s *dsdSource) perFrame() int { return dsdDecim2 * dsdStep1 * s.m }

func (s *dsdSource) Position() int64 { return s.pos }

func (s *dsdSource) Close() error { return s.f.Close() }

func (s *dsdSource) Read(dst []float32) (int, error) {
	n := 0
	for n+1 < len(dst) {
		if len(s.out) == 0 {
			if err := s.decode(); err != nil {
				if n > 0 && err == io.EOF {
					return n, nil
				}
				return n, err
			}
		}
		c := copy(dst[n:len(dst)-len(dst)%2], s.out)
		s.out = s.out[c:]
		n += c
	}
	s.pos += int64(n / 2)
	return n, nil
}

// decode calcula el siguiente trozo de salida en s.out.
func (s *dsdSource) decode() error {
	per := s.perFrame()
	step := dsdStep1 * s.m // bytes por muestra intermedia
	got, err := s.in.read(s.raw, dsdChunk*per)
	frames := got / per
	if frames == 0 {
		if err == nil {
			err = io.EOF
		}
		return err
	}
	used := frames * per
	l1 := len(s.tab1)
	for c := 0; c < s.ch; c++ {
		// Etapa 1: cada step bytes nuevos, una muestra a 88,2 kHz.
		ext := append(append(s.ext1[c][:0], s.tail1[c]...), s.raw[c][:used]...)
		s.ext1[c] = ext
		mids := frames * dsdDecim2
		ext2 := append(s.ext2[c][:0], s.tail2[c]...)
		for j := 0; j < mids; j++ {
			w := ext[(j+1)*step : (j+1)*step+l1]
			tab := s.tab1[:len(w)]
			var acc float32
			for k, b := range w {
				acc += tab[k][b]
			}
			ext2 = append(ext2, acc)
		}
		s.ext2[c] = ext2
		copy(s.tail1[c], ext[len(ext)-l1:])
		copy(s.tail2[c], ext2[len(ext2)-dsdTaps2:])
	}
	// Etapa 2: cada 2 muestras intermedias, una de salida.
	if cap(s.out) < frames*2 {
		s.out = make([]float32, 0, dsdChunk*2)
	}
	out := s.out[:frames*2]
	half := len(s.h2)
	for c := 0; c < s.ch; c++ {
		ext2 := s.ext2[c]
		for i := 0; i < frames; i++ {
			w := ext2[(i+1)*dsdDecim2 : (i+1)*dsdDecim2+dsdTaps2]
			lo, hi := w[:half], w[half:2*half]
			var acc float32
			for k, h := range s.h2[:len(lo)] {
				acc += h * (lo[k] + hi[half-1-k])
			}
			out[2*i+c] = acc
		}
	}
	if s.ch == 1 {
		for i := 0; i < frames; i++ {
			out[2*i+1] = out[2*i]
		}
	}
	s.out = out
	return nil
}

// dsdWarm son las muestras de salida que se decodifican y tiran antes del
// destino de un salto, para que los filtros ya tengan historia de verdad.
const dsdWarm = dsdTaps2/dsdDecim2 + 2

// SeekFrame salta calculando el byte de cada canal (sin leer lo de antes),
// con unas pocas muestras de calentamiento.
func (s *dsdSource) SeekFrame(frame int64) error {
	frame = clampFrame(frame, s.Length())
	start := max(0, frame-dsdWarm)
	if err := s.in.seek(start * int64(s.perFrame())); err != nil {
		return err
	}
	s.reset()
	s.pos = start
	buf := make([]float32, 2*(frame-start))
	for len(buf) > 0 {
		n, err := s.Read(buf)
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		buf = buf[n:]
	}
	s.pos = frame
	return nil
}

// dsdFilters calcula las tablas de la primera etapa para ×m y la mitad de
// la segunda (con la ganancia de dsdGain).
func dsdFilters(m int) ([][256]float32, []float32) {
	const beta = 6.76 // Kaiser: unos 70 dB de rechazo
	n1 := dsdTaps1 * m
	rate := float64(dsdBase * m)
	h1 := lowpass(n1, (20000.0+64100.0)/2/rate, beta)
	tab := make([][256]float32, n1/8)
	for k := range tab {
		for v := 0; v < 256; v++ {
			acc := 0.0
			for b := 0; b < 8; b++ {
				bit := float64((v>>(7-b))&1)*2 - 1
				acc += h1[8*k+b] * bit
			}
			tab[k][v] = float32(acc)
		}
	}
	// La segunda, a 88,2 kHz: corte entre 20 y 24,1 kHz.
	h2 := lowpass(dsdTaps2, (20000.0+24100.0)/2/(2*dsdOutRate), beta)
	half := make([]float32, dsdTaps2/2)
	for i := range half {
		half[i] = float32(h2[i] * dsdGain)
	}
	return tab, half
}

// lowpass es un FIR paso bajo de n coeficientes con corte en fc (en
// fracciones de la frecuencia de muestreo), ventana de Kaiser y ganancia 1.
func lowpass(n int, fc, beta float64) []float64 {
	h := make([]float64, n)
	sum := 0.0
	for i := range h {
		x := float64(i) - float64(n-1)/2
		h[i] = sinc(2*fc*x) * kaiser(i, n, beta)
		sum += h[i]
	}
	for i := range h {
		h[i] /= sum
	}
	return h
}

func sinc(x float64) float64 {
	if x == 0 {
		return 1
	}
	return math.Sin(math.Pi*x) / (math.Pi * x)
}

func kaiser(i, n int, beta float64) float64 {
	r := 2*float64(i)/float64(n-1) - 1
	return besselI0(beta*math.Sqrt(1-r*r)) / besselI0(beta)
}

// besselI0 es la función de Bessel modificada de orden 0 (por su serie).
func besselI0(x float64) float64 {
	sum, term := 1.0, 1.0
	for k := 1; k < 50; k++ {
		term *= (x / (2 * float64(k))) * (x / (2 * float64(k)))
		sum += term
		if term < sum*1e-12 {
			break
		}
	}
	return sum
}

// bitReverse da la vuelta a los bits de un byte (DSF guarda el primer bit
// en el menos significativo).
var bitReverse = func() (t [256]byte) {
	for v := range t {
		var r byte
		for b := 0; b < 8; b++ {
			if v&(1<<b) != 0 {
				r |= 1 << (7 - b)
			}
		}
		t[v] = r
	}
	return t
}()

// --- DSF ---

// dsfBytes lee los bloques de DSF: por cada grupo, block bytes del canal 0,
// luego block del 1, etc.
type dsfBytes struct {
	f         *os.File
	data      int64 // dónde empiezan los datos
	ch, block int
	lsb       bool  // el primer bit es el menos significativo
	total     int64 // bytes de cada canal
	at        int64 // próximo byte de cada canal
	grp       []byte
	grpAt     int64 // primer byte (de cada canal) del grupo cargado; -1 ninguno
}

// newDSF abre un .dsf.
func newDSF(f *os.File) (Source, error) {
	var hdr [28 + 52]byte
	if _, err := io.ReadFull(f, hdr[:]); err != nil {
		return nil, fmt.Errorf("dsf: %w", err)
	}
	le := binary.LittleEndian
	if string(hdr[:4]) != "DSD " || string(hdr[28:32]) != "fmt " {
		return nil, fmt.Errorf("dsf: bad header: %w", ErrUnsupported)
	}
	fmtSize := int64(le.Uint64(hdr[32:]))
	fm := hdr[40:]
	if le.Uint32(fm[4:]) != 0 {
		return nil, fmt.Errorf("dsf: not raw DSD: %w", ErrUnsupported)
	}
	ch := int(le.Uint32(fm[12:]))
	rate := int(le.Uint32(fm[16:]))
	bits := le.Uint32(fm[20:])
	samples := int64(le.Uint64(fm[24:]))
	block := int(le.Uint32(fm[32:]))
	if bits != 1 && bits != 8 {
		return nil, fmt.Errorf("dsf: bad bit order %d: %w", bits, ErrUnsupported)
	}
	if block <= 0 || ch <= 0 || ch > 8 {
		return nil, fmt.Errorf("dsf: bad block size or channels: %w", ErrUnsupported)
	}
	// El chunk "data" va tras el "fmt ".
	dataHdr := 28 + fmtSize
	var dh [12]byte
	if _, err := f.ReadAt(dh[:], dataHdr); err != nil {
		return nil, fmt.Errorf("dsf: %w", err)
	}
	if string(dh[:4]) != "data" {
		return nil, fmt.Errorf("dsf: no data chunk: %w", ErrUnsupported)
	}
	in := &dsfBytes{f: f, data: dataHdr + 12, ch: ch, block: block, lsb: bits == 1, total: samples / 8, grpAt: -1}
	in.grp = make([]byte, block*ch)
	return newDSDSource(f, in, rate, ch, in.total)
}

func (d *dsfBytes) seek(b int64) error {
	d.at = min(max(b, 0), d.total)
	return nil
}

func (d *dsfBytes) read(chans [][]byte, n int) (int, error) {
	got := 0
	for got < n && d.at < d.total {
		g := d.at / int64(d.block) * int64(d.block)
		if g != d.grpAt {
			off := d.data + g/int64(d.block)*int64(d.block*d.ch)
			k, err := d.f.ReadAt(d.grp, off)
			if k < len(d.grp) && err != nil && err != io.EOF {
				return got, err
			}
			for i := k; i < len(d.grp); i++ {
				d.grp[i] = 0x69 // un último bloque cortado: silencio
			}
			if d.lsb {
				for i, v := range d.grp {
					d.grp[i] = bitReverse[v]
				}
			}
			d.grpAt = g
		}
		o := int(d.at - g)
		k := min(n-got, d.block-o, int(d.total-d.at))
		for c := range chans {
			copy(chans[c][got:got+k], d.grp[c*d.block+o:])
		}
		got += k
		d.at += int64(k)
	}
	if got == 0 {
		return 0, io.EOF
	}
	return got, nil
}

// --- DFF (DSDIFF) ---

// dffBytes lee el chunk "DSD " de un DFF: un byte de cada canal por turno.
type dffBytes struct {
	f     *os.File
	data  int64
	ch    int
	total int64
	at    int64
	r     *bufio.Reader
	tmp   []byte
}

// newDFF abre un .dff (DSDIFF sin comprimir).
func newDFF(f *os.File) (Source, error) {
	be := binary.BigEndian
	var hdr [16]byte
	if _, err := io.ReadFull(f, hdr[:]); err != nil {
		return nil, fmt.Errorf("dff: %w", err)
	}
	if string(hdr[:4]) != "FRM8" || string(hdr[12:16]) != "DSD " {
		return nil, fmt.Errorf("dff: bad header: %w", ErrUnsupported)
	}
	end := 12 + int64(be.Uint64(hdr[4:]))
	rate, ch := 0, 0
	off := int64(16)
	for off+12 <= end {
		var c [12]byte
		if _, err := f.ReadAt(c[:], off); err != nil {
			return nil, fmt.Errorf("dff: %w", err)
		}
		id, size := string(c[:4]), int64(be.Uint64(c[4:]))
		body := off + 12
		switch id {
		case "PROP":
			var err error
			if rate, ch, err = dffProp(f, body, size); err != nil {
				return nil, err
			}
		case "DST ":
			return nil, fmt.Errorf("dff: DST-compressed DSD: %w", ErrUnsupported)
		case "DSD ":
			if rate == 0 || ch == 0 {
				return nil, fmt.Errorf("dff: data before properties: %w", ErrUnsupported)
			}
			in := &dffBytes{f: f, data: body, ch: ch, total: size / int64(ch)}
			if err := in.seek(0); err != nil {
				return nil, err
			}
			return newDSDSource(f, in, rate, ch, in.total)
		}
		off = body + size + size%2
	}
	return nil, fmt.Errorf("dff: no DSD data: %w", ErrUnsupported)
}

// dffProp lee el chunk PROP: la frecuencia (FS), los canales (CHNL) y la
// compresión (CMPR, solo "DSD ").
func dffProp(f *os.File, off, size int64) (rate, ch int, err error) {
	be := binary.BigEndian
	var kind [4]byte
	if _, err := f.ReadAt(kind[:], off); err != nil || string(kind[:]) != "SND " {
		return 0, 0, fmt.Errorf("dff: bad PROP chunk: %w", ErrUnsupported)
	}
	end := off + size
	for p := off + 4; p+12 <= end; {
		var c [12]byte
		if _, err := f.ReadAt(c[:], p); err != nil {
			return 0, 0, fmt.Errorf("dff: %w", err)
		}
		id, n := string(c[:4]), int64(be.Uint64(c[4:]))
		body := make([]byte, min(n, 64))
		if _, err := f.ReadAt(body, p+12); err != nil {
			return 0, 0, fmt.Errorf("dff: %w", err)
		}
		switch id {
		case "FS  ":
			if len(body) >= 4 {
				rate = int(be.Uint32(body))
			}
		case "CHNL":
			if len(body) >= 2 {
				ch = int(be.Uint16(body))
			}
		case "CMPR":
			if len(body) >= 4 && string(body[:4]) != "DSD " {
				return 0, 0, fmt.Errorf("dff: compressed (%q): %w", body[:4], ErrUnsupported)
			}
		}
		p += 12 + n + n%2
	}
	if rate == 0 || ch == 0 {
		return 0, 0, fmt.Errorf("dff: missing sample rate or channels: %w", ErrUnsupported)
	}
	return rate, ch, nil
}

func (d *dffBytes) seek(b int64) error {
	d.at = min(max(b, 0), d.total)
	if _, err := d.f.Seek(d.data+d.at*int64(d.ch), io.SeekStart); err != nil {
		return err
	}
	if d.r == nil {
		d.r = bufio.NewReaderSize(d.f, 64<<10)
	} else {
		d.r.Reset(d.f)
	}
	return nil
}

func (d *dffBytes) read(chans [][]byte, n int) (int, error) {
	n = int(min(int64(n), d.total-d.at))
	if n <= 0 {
		return 0, io.EOF
	}
	if cap(d.tmp) < n*d.ch {
		d.tmp = make([]byte, n*d.ch)
	}
	tmp := d.tmp[:n*d.ch]
	k, err := io.ReadFull(d.r, tmp)
	n = k / d.ch
	for i := 0; i < n; i++ {
		for c := range chans {
			chans[c][i] = tmp[i*d.ch+c]
		}
	}
	d.at += int64(n)
	if n == 0 {
		if err == nil || errors.Is(err, io.ErrUnexpectedEOF) {
			err = io.EOF
		}
		return 0, err
	}
	return n, nil
}
