package ui

import (
	"log"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
)

// Los fotogramas lentos, al log: cuando entre dos Update pasa más de
// slowFrame, se anota cuánto fue y cuánto tardaron el Update y el Draw
// anteriores, y si hubo una recolección de basura en medio. Sirve para ver
// de dónde vienen los tirones (con HDMI, por ejemplo) comparando la hora con
// el log del servicio. Como el hitch.go de Trancazo, anota también lo que
// delata cada causa: la memoria de la GPU que cambió (una página nueva del
// atlas), los fallos de página mayores y el swap (memoria). El fork de
// Ebitengine anota aparte "sdl2: slow frame" con el tiempo de presentar la
// imagen (pantalla, vblank). Cuesta dos time.Now por fotograma.

// slowFrame es desde cuánto un hueco entre fotogramas se anota.
const slowFrame = 250 * time.Millisecond

// frameLog son las medidas del último fotograma.
type frameLog struct {
	lastUpdate time.Time     // cuándo empezó el último Update
	update     time.Duration // lo que tardó
	draw       time.Duration // lo que tardó el último Draw
	drawAt     time.Time
	gcs        uint32
	gpu        int64     // memoria de imágenes en la GPU en el último anotado
	faults     int64     // fallos de página mayores en el último anotado
	logged     time.Time // cuándo se anotó el último (como mucho uno por segundo)
}

// frameStart se llama al empezar Update; devuelve la función que lo cierra.
func (a *App) frameStart() func() {
	f := &a.frames2
	now := time.Now()
	if a.sleeping {
		// En el sleep no se pinta a propósito (un fotograma de vez en cuando):
		// esos huecos no cuentan.
		f.lastUpdate = time.Time{}
		return func() {}
	}
	if !f.lastUpdate.IsZero() {
		if gap := now.Sub(f.lastUpdate); gap > slowFrame && now.Sub(f.logged) > time.Second {
			var ms runtime.MemStats
			runtime.ReadMemStats(&ms)
			var dbg ebiten.DebugInfo
			ebiten.ReadDebugInfo(&dbg)
			faults, swap := procFaults(), procSwapKB()
			log.Printf("slow frame: %v between updates (update %v, draw %v, %v since draw ended; %d GCs, GPU images %+d KB, %d major faults since last; swap %d KB)",
				gap.Round(time.Millisecond), f.update.Round(time.Millisecond), f.draw.Round(time.Millisecond),
				now.Sub(f.drawAt).Round(time.Millisecond), ms.NumGC-f.gcs,
				(dbg.TotalGPUImageMemoryUsageInBytes-f.gpu)/1024, faults-f.faults, swap)
			f.gcs, f.gpu, f.faults, f.logged = ms.NumGC, dbg.TotalGPUImageMemoryUsageInBytes, faults, now
		}
	}
	f.lastUpdate = now
	return func() { f.update = time.Since(now) }
}

// drawStart se llama al empezar Draw; devuelve la función que lo cierra.
func (a *App) drawStart() func() {
	start := time.Now()
	return func() {
		a.frames2.drawAt = time.Now()
		a.frames2.draw = a.frames2.drawAt.Sub(start)
	}
}

// procFaults son los fallos de página mayores del proceso (leer de disco o
// del swap), de /proc/self/stat; 0 si no se sabe (fuera de Linux).
func procFaults() int64 {
	b, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0
	}
	// Tras el nombre (entre paréntesis) van los campos; majflt es el 12.º.
	s := string(b)
	if i := strings.LastIndexByte(s, ')'); i >= 0 {
		s = s[i+1:]
	}
	f := strings.Fields(s)
	if len(f) < 10 {
		return 0
	}
	var n int64
	for _, c := range f[9] {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int64(c-'0')
	}
	return n
}

// procSwapKB es lo que el proceso tiene en el swap (VmSwap), en KB.
func procSwapKB() int64 {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, "VmSwap:"); ok {
			var kb int64
			for _, c := range strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(v), "kB")) {
				if c < '0' || c > '9' {
					break
				}
				kb = kb*10 + int64(c-'0')
			}
			return kb
		}
	}
	return 0
}
