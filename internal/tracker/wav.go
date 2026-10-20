package tracker

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

var noteNames = [12]string{"C-", "C#", "D-", "D#", "E-", "F-", "F#", "G-", "G#", "A-", "A#", "B-"}

// NoteName es el nombre de una nota: "C-4", "F#2", "---" (vacía) u "OFF".
func NoteName(n int8) string {
	switch {
	case n == NoteEmpty:
		return "---"
	case n == NoteOff:
		return "OFF"
	}
	i := int(n) - 1
	return fmt.Sprintf("%s%d", noteNames[i%12], i/12)
}

// WriteWAV escribe muestras estéreo intercaladas como WAV de 16 bits.
func WriteWAV(w io.Writer, samples []float32, rate int) error {
	data := len(samples) * 2
	hdr := make([]byte, 44)
	copy(hdr[0:], "RIFF")
	binary.LittleEndian.PutUint32(hdr[4:], uint32(36+data))
	copy(hdr[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(hdr[16:], 16)
	binary.LittleEndian.PutUint16(hdr[20:], 1) // PCM
	binary.LittleEndian.PutUint16(hdr[22:], 2)
	binary.LittleEndian.PutUint32(hdr[24:], uint32(rate))
	binary.LittleEndian.PutUint32(hdr[28:], uint32(rate*4))
	binary.LittleEndian.PutUint16(hdr[32:], 4)
	binary.LittleEndian.PutUint16(hdr[34:], 16)
	copy(hdr[36:], "data")
	binary.LittleEndian.PutUint32(hdr[40:], uint32(data))
	if _, err := w.Write(hdr); err != nil {
		return err
	}
	buf := make([]byte, data)
	for i, x := range samples {
		v := int16(math.Max(-1, math.Min(1, float64(x))) * 32767)
		binary.LittleEndian.PutUint16(buf[2*i:], uint16(v))
	}
	_, err := w.Write(buf)
	return err
}
