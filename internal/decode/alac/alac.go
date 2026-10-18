// Package alac decodifica Apple Lossless (ALAC). decode.go es el decodificador
// de github.com/alicebob/alac (licencia MIT, ver LICENSE), un port a Go del de
// David Hammerton; aquí se le añade la configuración por la "cookie" del
// archivo (allí estaba fijada para AirPlay: 16 bits, estéreo, 352 muestras).
package alac

import (
	"encoding/binary"
	"errors"
)

// Decoder decodifica los bloques de una pista ALAC.
type Decoder struct {
	a *Alac
	// Channels y BitDepth son los de la pista; SampleRate, su frecuencia.
	Channels, BitDepth, SampleRate int
}

// New prepara un decodificador con la cookie de la pista (ALACSpecificConfig,
// 24 bytes big-endian: la caja "alac" de MP4 sin versión ni flags).
func New(cookie []byte) (*Decoder, error) {
	if len(cookie) < 24 {
		return nil, errors.New("alac: short cookie")
	}
	be := binary.BigEndian
	bits, ch := int(cookie[5]), int(cookie[9])
	if (bits != 16 && bits != 24) || ch < 1 || ch > 2 {
		return nil, errors.New("alac: only 16 or 24 bits, mono or stereo")
	}
	a := create_alac(bits, ch)
	a.setinfo_max_samples_per_frame = be.Uint32(cookie[0:])
	a.setinfo_7a = cookie[4]
	a.setinfo_sample_size = cookie[5]
	a.setinfo_rice_historymult = cookie[6]
	a.setinfo_rice_initialhistory = cookie[7]
	a.setinfo_rice_kmodifier = cookie[8]
	a.setinfo_7f = cookie[9]
	a.setinfo_80 = be.Uint16(cookie[10:])
	a.setinfo_82 = be.Uint32(cookie[12:])
	a.setinfo_86 = be.Uint32(cookie[16:])
	a.setinfo_8a_rate = be.Uint32(cookie[20:])
	if a.setinfo_max_samples_per_frame == 0 || a.setinfo_max_samples_per_frame > 1<<16 {
		return nil, errors.New("alac: bad frame length")
	}
	a.allocateBuffers()
	return &Decoder{a: a, Channels: ch, BitDepth: bits, SampleRate: int(a.setinfo_8a_rate)}, nil
}

// Decode decodifica un bloque y devuelve sus muestras intercaladas, en
// little-endian de BitDepth bits.
func (d *Decoder) Decode(block []byte) []byte {
	return d.a.decodeFrame(block)
}
