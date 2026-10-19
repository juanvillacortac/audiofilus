// Command sfxdemo escribe en un WAV los sonidos de los juegos uno tras otro
// (con medio segundo entre ellos), para escucharlos juntos al ajustarlos:
//
//	go run ./tools/sfxdemo out.wav
package main

import (
	"encoding/binary"
	"math"
	"os"

	"github.com/juanvillacortac/audiofilus/internal/sfx"
)

// names son los sonidos de los juegos, en el orden en que suenan.
var names = []string{"cr_step", "cr_bump", "cr_hit", "cr_kill", "cr_coin", "cr_miss", "cr_hurt", "cr_stairs", "cr_dead"}

func main() {
	var pcm []int16
	gap := make([]int16, sfx.Rate) // medio segundo de silencio (estéreo)
	for _, n := range names {
		for _, x := range sfx.Samples(n) {
			pcm = append(pcm, int16(math.Max(-1, math.Min(1, float64(x)*0.6))*32767))
		}
		pcm = append(pcm, gap...)
	}
	f, err := os.Create(os.Args[1])
	if err != nil {
		panic(err)
	}
	defer f.Close()
	size := uint32(len(pcm) * 2)
	hdr := []any{[4]byte{'R', 'I', 'F', 'F'}, 36 + size, [4]byte{'W', 'A', 'V', 'E'}, [4]byte{'f', 'm', 't', ' '},
		uint32(16), uint16(1), uint16(2), uint32(sfx.Rate), uint32(sfx.Rate * 4), uint16(4), uint16(16),
		[4]byte{'d', 'a', 't', 'a'}, size}
	for _, v := range hdr {
		_ = binary.Write(f, binary.LittleEndian, v)
	}
	_ = binary.Write(f, binary.LittleEndian, pcm)
}
