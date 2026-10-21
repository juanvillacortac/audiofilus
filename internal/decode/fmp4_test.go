package decode

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// box arma una caja MP4.
func box(typ string, parts ...[]byte) []byte {
	body := bytes.Join(parts, nil)
	b := binary.BigEndian.AppendUint32(nil, uint32(8+len(body)))
	return append(append(b, typ...), body...)
}

func u32(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }

// fmp4Init arma un trozo inicial con una pista AAC (track_ID 1) con ese
// AudioSpecificConfig.
func fmp4Init(asc []byte) []byte {
	esds := append([]byte{0, 0, 0, 0}, // versión y banderas
		0x03, byte(3+2+13+2+len(asc)), 0, 1, 0, // ES_Descriptor
		0x04, byte(13+2+len(asc)), 0x40, 0x15, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // DecoderConfig
		0x05, byte(len(asc)))
	esds = append(esds, asc...)
	mp4a := make([]byte, 28)
	binary.BigEndian.PutUint16(mp4a[16:], 2)
	binary.BigEndian.PutUint32(mp4a[24:], 44100<<16)
	stsd := box("stsd", u32(0), u32(1), box("mp4a", mp4a, box("esds", esds)))
	stbl := box("stbl", stsd, box("stts", u32(0), u32(0)), box("stsc", u32(0), u32(0)), box("stsz", u32(0), u32(0), u32(0)), box("stco", u32(0), u32(0)))
	mdhd := box("mdhd", u32(0), u32(0), u32(0), u32(44100), u32(0), u32(0))
	hdlr := box("hdlr", u32(0), u32(0), []byte("soun"), make([]byte, 13))
	tkhd := box("tkhd", u32(0), u32(0), u32(0), u32(1), make([]byte, 68))
	trak := box("trak", tkhd, box("mdia", mdhd, hdlr, box("minf", stbl)))
	trex := box("trex", u32(0), u32(1), u32(1), u32(1024), u32(0), u32(0))
	return append(box("ftyp", []byte("iso6"), u32(0)), box("moov", box("mvex", trex), trak)...)
}

// fmp4Segment arma un trozo con un moof (default-base-is-moof) y su mdat con
// esas muestras.
func fmp4Segment(samples ...[]byte) []byte {
	trun := func(dataOff uint32) []byte {
		b := append(u32(0x000201), u32(uint32(len(samples)))...) // offset y tamaños
		b = append(b, u32(dataOff)...)
		for _, s := range samples {
			b = append(b, u32(uint32(len(s)))...)
		}
		return box("trun", b)
	}
	tfhd := box("tfhd", u32(0x020000), u32(1))
	moofLen := len(box("moof", box("mfhd", u32(0), u32(1)), box("traf", tfhd, trun(0))))
	moof := box("moof", box("mfhd", u32(0), u32(1)), box("traf", tfhd, trun(uint32(moofLen+8))))
	return append(moof, box("mdat", bytes.Join(samples, nil))...)
}

// Las muestras AAC de un trozo fMP4 salen en orden, y con su cabecera ADTS
// quedan como un flujo que el decodificador de radio entiende.
func TestFMP4Samples(t *testing.T) {
	asc := []byte{0x12, 0x10} // AAC-LC, 44,1 kHz, estéreo
	tr, err := ParseFMP4Init(fmp4Init(asc))
	if err != nil {
		t.Fatal(err)
	}
	if tr.ID != 1 || !bytes.Equal(tr.Config, asc) {
		t.Fatalf("track %+v, want ID 1 and config %x", tr, asc)
	}
	want := [][]byte{bytes.Repeat([]byte{1}, 100), bytes.Repeat([]byte{2}, 57), bytes.Repeat([]byte{3}, 300)}
	got, err := FMP4Samples(tr, fmp4Segment(want...))
	if err != nil || len(got) != len(want) {
		t.Fatalf("got %d samples, err %v; want %d", len(got), err, len(want))
	}
	for i := range want {
		if !bytes.Equal(got[i], want[i]) {
			t.Errorf("sample %d differs", i)
		}
	}
}

// La cabecera ADTS lleva el perfil, la frecuencia, los canales y la longitud
// del frame (con la cabecera); con HE-AAC, los del núcleo.
func TestADTSHeader(t *testing.T) {
	h, err := ADTSHeader([]byte{0x12, 0x10}, 100)
	if err != nil {
		t.Fatal(err)
	}
	// LC (perfil 1), índice 4 (44,1 kHz), 2 canales, 107 bytes.
	if want := []byte{0xFF, 0xF1, 0x50, 0x80, 0x0D, 0x7F, 0xFC}; !bytes.Equal(h, want) {
		t.Errorf("LC: % x, want % x", h, want)
	}
	// HE-AAC explícito: objeto 5, núcleo a 24 kHz (índice 6), 2 canales,
	// extensión a 48 kHz (índice 3), núcleo LC.
	he := []byte{0x2B, 0x11, 0x88, 0x00}
	h, err = ADTSHeader(he, 10)
	if err != nil {
		t.Fatal(err)
	}
	if prof, freq := h[2]>>6, (h[2]>>2)&0xF; prof != 1 || freq != 6 {
		t.Errorf("HE-AAC: profile %d, freq index %d; want 1 (LC) and 6 (24 kHz)", prof, freq)
	}
	if _, err := ADTSHeader([]byte{0xF8, 0x00, 0x00}, 10); err == nil {
		t.Error("an unsupported object type gave no error")
	}
}
