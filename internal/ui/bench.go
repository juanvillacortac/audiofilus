package ui

import (
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
	"sort"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
)

// La prueba de rendimiento de un visualizador (-bench): sin servicio, con el
// ritmo de la maqueta, pinta benchFrames fotogramas seguidos (un tercio con
// cada cámara) en un lienzo de 320×240 y dice cuánto tarda la CPU en cada
// uno, cuánta memoria crea y cuántos triángulos salen. Así se mide en el PC
// antes de llevar un cambio a la consola. Con BenchProfile, guarda además un
// perfil de CPU de toda la prueba.

const benchFrames = 600

// benchState es lo que lleva la prueba entre fotogramas.
type benchState struct {
	canvas *ebiten.Image
	times  []time.Duration
	alloc  uint64
	tris   int
	prof   *os.File
}

// benchVisual hace un fotograma de la prueba; al acabar, imprime el resultado
// y termina.
func (a *App) benchVisual() error {
	b := &a.bench
	if b.canvas == nil {
		kind := -1
		for k, n := range visNames {
			if n[4:] == a.BenchVisual {
				kind = k
			}
		}
		if kind < 0 {
			return fmt.Errorf("unknown visualizer %q", a.BenchVisual)
		}
		a.screen, a.vis.kind = Visual, kind
		a.isPlaying = true
		b.canvas = ebiten.NewImage(320, ScreenH)
		if a.BenchProfile != "" {
			f, err := os.Create(a.BenchProfile)
			if err != nil {
				return err
			}
			if err := pprof.StartCPUProfile(f); err != nil {
				return err
			}
			b.prof = f
		}
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		b.alloc = ms.TotalAlloc
	}
	i := len(b.times)
	cam := i * 3 / benchFrames
	switch a.vis.kind {
	case visDrift:
		if a.drift != nil {
			a.drift.cam = cam
		}
	case visTrench:
		if a.trench != nil {
			a.trench.cam = cam
		}
	case visLake:
		if a.lake != nil {
			a.lake.cam = cam
		}
	}
	start := time.Now()
	// Como en la consola: dos pasos de 1/60 por cada dibujo (30 fps).
	for k := 0; k < 2; k++ {
		a.beatHit = a.updateBeat()
		a.updateVisual()
		a.t += 1.0 / 60
	}
	a.drawVisual(b.canvas)
	b.times = append(b.times, time.Since(start))
	for _, r := range []*r3d{a.r3dOf()} {
		if r != nil {
			b.tris += r.drawn
		}
	}
	if len(b.times) < benchFrames {
		return nil
	}
	if b.prof != nil {
		pprof.StopCPUProfile()
		b.prof.Close()
		// Y dónde se crea memoria, junto al de CPU.
		if f, err := os.Create(a.BenchProfile + ".mem"); err == nil {
			pprof.Lookup("allocs").WriteTo(f, 0)
			f.Close()
		}
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	ts := append([]time.Duration(nil), b.times...)
	sort.Slice(ts, func(i, j int) bool { return ts[i] < ts[j] })
	var sum time.Duration
	for _, t := range ts {
		sum += t
	}
	fmt.Printf("%s: %d frames  mean %.2f ms  p50 %.2f ms  p95 %.2f ms  max %.2f ms  alloc %.1f KB/frame  tris %d/frame\n",
		a.BenchVisual, len(ts), ms2(sum/time.Duration(len(ts))), ms2(ts[len(ts)/2]), ms2(ts[len(ts)*95/100]), ms2(ts[len(ts)-1]),
		float64(ms.TotalAlloc-b.alloc)/1024/float64(len(ts)), b.tris/len(ts))
	return ebiten.Termination
}

// r3dOf es el motor 3D del visualizador elegido (nil si no es 3D).
func (a *App) r3dOf() *r3d {
	switch {
	case a.vis.kind == visDrift && a.drift != nil:
		return &a.drift.r3d
	case a.vis.kind == visTrench && a.trench != nil:
		return &a.trench.r3d
	case a.vis.kind == visLake && a.lake != nil:
		return &a.lake.r3d
	}
	return nil
}

// ms2 pasa una duración a milisegundos.
func ms2(d time.Duration) float64 { return float64(d) / 1e6 }
