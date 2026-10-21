package decode

import (
	"encoding/binary"
	"io"

	"github.com/hajimehoshi/go-mp3"
)

// goMP3Stream es el respaldo en Go puro (go-mp3) para las radios MP3 cuando
// no está libmpg123: en Windows sin sus DLL, por ejemplo. go-mp3 lee de un
// io.Reader y no sabe esperar, así que solo se le pide un frame cuando hay
// bytes de sobra para él (gmMargin); si aun así algo sale mal (un corte al
// reconectar), se vuelve a crear y se resincroniza solo con el siguiente.
type goMP3Stream struct {
	in   []byte // lo que llegó y aún no leyó go-mp3
	dec  *mp3.Decoder
	rate int
	pcm  []byte
}

// Márgenes: un frame MP3 ocupa como mucho ~1,5 KB (320 kbps a 32 kHz); con
// gmMargin en el búfer, leer uno nunca se queda corto. Para empezar se pide
// más, porque go-mp3 lee el primero al crearse y puede tener que buscarlo.
const (
	gmMargin = 4 << 10
	gmStart  = 16 << 10
	gmFrame  = 1152 * mp3BytesPerFrame // las muestras de un frame, en bytes
)

func newGoMP3Stream() *goMP3Stream { return &goMP3Stream{} }

// Read es lo que lee go-mp3: lo que haya en el búfer.
func (s *goMP3Stream) Read(p []byte) (int, error) {
	if len(s.in) == 0 {
		return 0, io.EOF
	}
	n := copy(p, s.in)
	s.in = s.in[n:]
	return n, nil
}

func (s *goMP3Stream) Feed(b []byte) { s.in = append(s.in, b...) }

func (s *goMP3Stream) Decode(dst []float32) (int, error) {
	frames := len(dst) / 2
	got := 0
	for got < frames {
		if s.dec == nil {
			if len(s.in) < gmStart {
				break
			}
			dec, err := mp3.NewDecoder(struct{ io.Reader }{s})
			if err != nil {
				// No encontró un frame entero: se descarta un poco y se
				// prueba con más bytes.
				s.in = s.in[min(len(s.in), 1<<10):]
				break
			}
			s.dec, s.rate = dec, dec.SampleRate()
		}
		if len(s.in) < gmMargin {
			break
		}
		want := min((frames-got)*mp3BytesPerFrame, gmFrame)
		if cap(s.pcm) < want {
			s.pcm = make([]byte, gmFrame)
		}
		n, err := s.dec.Read(s.pcm[:want])
		n -= n % mp3BytesPerFrame
		for i := 0; i < n/2; i++ {
			dst[got*2+i] = float32(int16(binary.LittleEndian.Uint16(s.pcm[i*2:]))) / 32768
		}
		got += n / mp3BytesPerFrame
		if err != nil {
			s.dec = nil // se vuelve a sincronizar con lo que siga
			break
		}
		if n == 0 {
			break
		}
	}
	return got * 2, nil
}

func (s *goMP3Stream) SampleRate() int { return s.rate }
func (s *goMP3Stream) Close()          { s.in, s.dec = nil, nil }
