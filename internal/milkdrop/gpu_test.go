//go:build milkdropgpu

package milkdrop

import (
	"errors"
	"os"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
)

// Las pruebas con GPU necesitan una pantalla (abren una ventana): van aparte,
// con go test -tags milkdropgpu ./internal/milkdrop.

// gpuGame corre f dentro del bucle de Ebitengine (ahí ya se puede leer la GPU).
type gpuGame struct {
	f    func() error
	err  error
	done bool
}

func (g *gpuGame) Update() error {
	if !g.done {
		g.done = true
		g.err = g.f()
	}
	return errStop
}
func (g *gpuGame) Draw(*ebiten.Image)         {}
func (g *gpuGame) Layout(int, int) (int, int) { return 64, 64 }

var errStop = errors.New("stop")

var gpuTests []func() error

// TestMain corre las pruebas en el hilo principal, dentro del juego.
func TestMain(m *testing.M) {
	code := 0
	g := &gpuGame{f: func() error { code = m.Run(); return nil }}
	ebiten.SetWindowSize(64, 64)
	if err := ebiten.RunGame(g); err != nil && !errors.Is(err, errStop) {
		panic(err)
	}
	os.Exit(code)
}

// TestGPUColors pinta una onda roja sin aditiva ni gamma y comprueba que en
// la salida hay rojo (y no gris), que hay estela y que con el tiempo se apaga.
func TestGPUColors(t *testing.T) {
	p, err := Parse("red", []byte(`[preset00]
fGammaAdj=1
fDecay=0.9
nWaveMode=6
fWaveAlpha=1
bMaximizeWaveColor=0
wave_r=1
wave_g=0
wave_b=0
zoom=1.05
warp=0
`))
	if err != nil {
		t.Fatal(err)
	}
	r := NewRenderer(Options{Width: 64, Height: 48, MeshX: 8, MeshY: 6})
	r.SetPreset(p, 0)
	b := make([]float64, 16)
	for i := range b {
		b[i] = 0.6
	}
	var img *ebiten.Image
	for i := 0; i < 30; i++ {
		r.Update(Input{Dt: 1.0 / 30, Bands: b})
		if img, err = r.Draw(); err != nil {
			t.Fatal(err)
		}
	}
	px := make([]byte, 4*64*48)
	img.ReadPixels(px)
	var red, gray, lit int
	for i := 0; i < len(px); i += 4 {
		rr, gg, bb := int(px[i]), int(px[i+1]), int(px[i+2])
		if rr > 20 {
			lit++
		}
		if rr > 40 && gg < rr/4 && bb < rr/4 {
			red++
		}
		if rr > 40 && gg > rr*3/4 {
			gray++
		}
	}
	t.Logf("lit %d red %d gray %d", lit, red, gray)
	if red == 0 || gray > red/10 {
		t.Errorf("colors lost: red %d gray %d", red, gray)
	}
	if lit < 200 {
		t.Errorf("no trails: %d lit pixels", lit)
	}
	// Sin onda (alfa 0) todo se apaga hasta negro.
	r.cur.base[oWaveA] = 0
	for i := 0; i < 200; i++ {
		r.Update(Input{Dt: 1.0 / 30, Bands: b})
		img, _ = r.Draw()
	}
	img.ReadPixels(px)
	for i := 0; i < len(px); i += 4 {
		if px[i] > 2 || px[i+1] > 2 || px[i+2] > 2 {
			t.Fatalf("did not fade to black: %v", px[i:i+4])
		}
	}
}
