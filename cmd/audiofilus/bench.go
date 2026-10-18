package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/decode"
	"github.com/juanvillacortac/audiofilus/internal/resample"
)

// runBench mide cuánta CPU necesita cada etapa por segundo de audio,
// decodificando archivos enteros lo más rápido posible.
func runBench(files []string) int {
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "usage: audiofilus bench <files...>")
		return 2
	}
	for _, f := range files {
		dec, audio, err := stage(f, false, false)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		res, _, _ := stage(f, true, false)
		full, _, _ := stage(f, true, true)
		pct := func(d time.Duration) string { return fmt.Sprintf("%.2f%%", 100*d.Seconds()/audio.Seconds()) }
		fmt.Printf("%s (%.0f s of audio)\n", f, audio.Seconds())
		fmt.Printf("  decode only:                 %s of one core\n", pct(dec))
		fmt.Printf("  + resample to 48 kHz:        %s\n", pct(res))
		fmt.Printf("  + float32 -> bytes (engine): %s\n", pct(full))
	}
	return 0
}

// stage procesa path completo con las etapas pedidas y devuelve el tiempo de
// CPU y la duración del audio.
func stage(path string, doResample, toBytes bool) (time.Duration, time.Duration, error) {
	src, err := decode.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer src.Close()
	var r interface{ Read([]float32) (int, error) } = src
	if doResample {
		r = resample.New(src, 48000)
	}
	buf := make([]float32, 4096)
	out := make([]byte, len(buf)*4)
	frames := 0
	start := time.Now()
	for {
		n, err := r.Read(buf)
		frames += n / 2
		if toBytes {
			for i := 0; i < n; i++ {
				binary.LittleEndian.PutUint32(out[i*4:], math.Float32bits(buf[i]))
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, 0, err
		}
	}
	rate := src.SampleRate()
	if doResample {
		rate = 48000
	}
	return time.Since(start), time.Duration(float64(frames) / float64(rate) * float64(time.Second)), nil
}
