package milkdrop

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

// writeFile escribe un archivo creando sus carpetas.
func writeFile(path, data string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(data), 0o644)
}

// identity es el movimiento que no mueve nada.
func identity() [len(motionOuts)]float64 {
	return [len(motionOuts)]float64{1, 1, 0, 0, 0.5, 0.5, 0, 0, 1, 1}
}

// TestMeshIdentity comprueba que sin movimiento cada vértice toma su mismo
// punto.
func TestMeshIdentity(t *testing.T) {
	m := identity()
	wc := newWarpConst(3, 1, 1)
	for _, p := range [][2]float64{{0, 0}, {0.3, 0.7}, {1, 1}, {0.5, 0.5}} {
		u, v := meshUV(&m, &wc, p[0], p[1], 0.5, 1, 0.75)
		if math.Abs(u-p[0]) > 1e-9 || math.Abs(v-p[1]) > 1e-9 {
			t.Errorf("(%v) -> (%v, %v)", p, u, v)
		}
	}
}

// TestMeshZoom comprueba que zoom 2 toma del doble de cerca del centro, y que
// zoomexp hace que el zoom dependa de rad.
func TestMeshZoom(t *testing.T) {
	m := identity()
	m[0] = 2
	wc := newWarpConst(0, 1, 1)
	u, v := meshUV(&m, &wc, 1, 0, 1, 1, 0.75)
	if math.Abs(u-0.75) > 1e-9 || math.Abs(v-0.25) > 1e-9 {
		t.Errorf("zoom 2: (%v, %v), want (0.75, 0.25)", u, v)
	}
	// Con zoomexp 2, en rad 0.5 el exponente es 2^0 = 1 (zoom entero) y en
	// rad 0, 2^-1 (raíz de 2).
	m[1] = 2
	u, _ = meshUV(&m, &wc, 1, 0.5, 0.5, 1, 1)
	if math.Abs(u-0.75) > 1e-9 {
		t.Errorf("zoomexp at rad 0.5: u = %v", u)
	}
	u, _ = meshUV(&m, &wc, 1, 0.5, 0, 1, 1)
	if want := 0.5 + 0.5/math.Sqrt2; math.Abs(u-want) > 1e-9 {
		t.Errorf("zoomexp at rad 0: u = %v, want %v", u, want)
	}
}

// TestMeshRotTranslate comprueba el giro (sin deformar con el aspecto), el
// desplazamiento y el estirado.
func TestMeshRotTranslate(t *testing.T) {
	m := identity()
	m[2] = math.Pi / 2
	wc := newWarpConst(0, 1, 1)
	// Un cuarto de vuelta: el punto a la derecha del centro toma del de
	// abajo (y hacia abajo), en píxeles cuadrados aunque la imagen sea 4:3.
	u, v := meshUV(&m, &wc, 0.75, 0.5, 0.5, 1, 0.75)
	if math.Abs(u-0.5) > 1e-9 || math.Abs(v-(0.5+0.25/0.75)) > 1e-9 {
		t.Errorf("rot: (%v, %v)", u, v)
	}
	m = identity()
	m[6], m[7] = 0.1, 0.05
	u, v = meshUV(&m, &wc, 0.5, 0.5, 0, 1, 1)
	if math.Abs(u-0.4) > 1e-9 || math.Abs(v-0.55) > 1e-9 {
		t.Errorf("dx/dy: (%v, %v)", u, v)
	}
	m = identity()
	m[8] = 2
	u, _ = meshUV(&m, &wc, 1, 0.5, 0, 1, 1)
	if math.Abs(u-0.75) > 1e-9 {
		t.Errorf("sx 2: u = %v", u)
	}
}

// TestMeshWarp comprueba que el warp mueve poco (unas milésimas) y cambia
// con el tiempo.
func TestMeshWarp(t *testing.T) {
	m := identity()
	m[3] = 1
	a, b := newWarpConst(1, 1, 1), newWarpConst(2, 1, 1)
	u1, v1 := meshUV(&m, &a, 0.3, 0.6, 0.4, 1, 1)
	u2, v2 := meshUV(&m, &b, 0.3, 0.6, 0.4, 1, 1)
	if math.Abs(u1-0.3) > 0.01 || math.Abs(v1-0.6) > 0.01 || (u1 == 0.3 && v1 == 0.6) {
		t.Errorf("warp moved to (%v, %v)", u1, v1)
	}
	if u1 == u2 && v1 == v2 {
		t.Error("warp does not animate")
	}
}

// TestQVars comprueba que las q pasan de las ecuaciones por fotograma a las
// de los vértices, las ondas y las formas, que vuelven cada fotograma al
// valor del inicio, y que las t de una onda vuelven al de su inicio.
func TestQVars(t *testing.T) {
	p, err := Parse("q", []byte(`[preset00]
per_frame_init_1=q1 = 10; q2 = 1;
per_frame_1=q2 = q2 + 1; q3 = q1 + 5; zoom = 1.5; keep = keep + 1;
per_pixel_1=seen = q3; zoom = zoom + q2;
wavecode_0_enabled=1
wave_0_init1=t1 = 7;
wave_0_per_frame1=t1 = t1 + 1; wq = q3;
wave_0_per_point1=pt = t1;
shapecode_0_enabled=1
shape_0_per_frame1=sq = q2;
`))
	if err != nil {
		t.Fatal(err)
	}
	r := NewRenderer(Options{Width: 32, Height: 24, MeshX: 4, MeshY: 3})
	if w := r.SetPreset(p, 0); len(w) > 0 {
		t.Fatal(w)
	}
	in := Input{Dt: 1.0 / 30, Bands: make([]float64, 16)}
	r.Update(in)
	r.Update(in)
	c := r.cur
	// q2 empieza cada fotograma en 1 (el valor tras el inicio): sale 2, no 3.
	if c.q[1] != 2 || c.q[2] != 15 {
		t.Errorf("q2 = %v, q3 = %v", c.q[1], c.q[2])
	}
	if c.frame.Get("keep") != 2 {
		t.Errorf("user variable did not persist: keep = %v", c.frame.Get("keep"))
	}
	if c.pix.Get("seen") != 15 || c.pix.Get("zoom") != 3.5 {
		t.Errorf("per_pixel saw q3 = %v, zoom = %v", c.pix.Get("seen"), c.pix.Get("zoom"))
	}
	w := c.waves[0]
	if w.s.Get("wq") != 15 || w.s.Get("t1") != 8 || w.s.Get("pt") != 8 {
		t.Errorf("wave: wq = %v, t1 = %v, pt = %v", w.s.Get("wq"), w.s.Get("t1"), w.s.Get("pt"))
	}
	if c.shapes[0].s.Get("sq") != 2 {
		t.Errorf("shape saw q2 = %v", c.shapes[0].s.Get("sq"))
	}
}

// fakeInput es un sonido de prueba: bandas que suben y bajan a ritmo.
func fakeInput(i int) Input {
	b := make([]float64, 16)
	for k := range b {
		b[k] = 0.4 + 0.3*math.Sin(float64(i)*0.3+float64(k)) + 0.3*float64((i/15)%2)*float64(16-k)/16
	}
	return Input{Dt: 1.0 / 30, Bands: b, Progress: float64(i%300) / 300}
}

// TestAudioLevels comprueba que bass/mid/treb rondan 1 con un sonido estable
// y que en silencio son 0.
func TestAudioLevels(t *testing.T) {
	r := NewRenderer(Options{Width: 32, Height: 24})
	b := make([]float64, 16)
	for k := range b {
		b[k] = 0.5
	}
	for i := 0; i < 900; i++ {
		r.Update(Input{Dt: 1.0 / 30, Bands: b})
	}
	for i, v := range r.imm {
		if math.Abs(v-1) > 0.05 {
			t.Errorf("level %d = %v, want ~1", i, v)
		}
	}
	r.Update(Input{Dt: 1.0 / 30, Bands: make([]float64, 16)})
	if r.imm[0] != 0 {
		t.Errorf("silent bass = %v", r.imm[0])
	}
}

// TestRenderFrames evalúa muchos fotogramas de cada preset de serie (con
// mezclas entre ellos), sin GPU: no debe haber pánicos, ni NaN en la malla,
// ni crear memoria una vez en marcha.
func TestRenderFrames(t *testing.T) {
	r := NewRenderer(Options{Width: 320, Height: 240})
	var presets []*Preset
	for _, e := range Builtin() {
		p, err := Load(e)
		if err != nil {
			t.Fatal(err)
		}
		presets = append(presets, p)
	}
	frame := 0
	for k, p := range presets {
		r.SetPreset(p, 2)
		for i := 0; i < 120; i++ {
			r.Update(fakeInput(frame))
			frame++
		}
		for i, uv := range r.uvCur {
			if math.IsNaN(uv) || math.IsInf(uv, 0) {
				t.Fatalf("%s: uv[%d] = %v", p.Name, i, uv)
			}
		}
		if len(r.geo.cmds) == 0 && k >= 0 && p.Name != "" {
			t.Errorf("%s draws nothing", p.Name)
		}
	}
	// Ya en marcha, un fotograma (con mezcla) no crea memoria.
	r.SetPreset(presets[0], 100)
	r.Update(fakeInput(0))
	if n := testing.AllocsPerRun(50, func() { r.Update(fakeInput(frame)) }); n > 1 {
		// fakeInput crea su slice: una.
		t.Errorf("Update allocates %v times per frame", n)
	}
}

// TestCycler comprueba el cambio cada tanto, con golpe y con candado.
func TestCycler(t *testing.T) {
	c := Cycler{Interval: 1}
	n := 0
	for i := 0; i < 100; i++ {
		if c.Tick(0.05, false) {
			n++
		}
	}
	if n != 5 {
		t.Errorf("%d changes in 5 s, want 5", n)
	}
	c = Cycler{Interval: 1, OnBeat: true}
	if c.Tick(1.5, false) || !c.Tick(0.1, true) {
		t.Error("on beat: did not wait for the beat")
	}
	if !c.Tick(beatWait+1.1, false) {
		t.Error("on beat: did not give up waiting")
	}
	c = Cycler{Interval: 1, Locked: true}
	if c.Tick(10, true) {
		t.Error("locked cycler changed")
	}
}

// BenchmarkUpdate mide la CPU de un fotograma a 320×240 con la malla de
// 32×24 (el preset con más código por vértice).
func BenchmarkUpdate(b *testing.B) {
	for _, e := range Builtin() {
		p, _ := Load(e)
		b.Run(e.Name, func(b *testing.B) {
			r := NewRenderer(Options{Width: 320, Height: 240})
			r.SetPreset(p, 0)
			in := fakeInput(0)
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				r.Update(in)
			}
		})
	}
}
