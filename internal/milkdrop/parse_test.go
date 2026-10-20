package milkdrop

import (
	"strings"
	"testing"
)

// samplePreset es un preset pequeño con todas las partes.
const samplePreset = `[preset00]
fDecay=0.950000
fGammaAdj=1.5
nWaveMode=3
zoom=1.02
per_frame_init_1=q1 = 2;
per_frame_2=b = a*2;
per_frame_1=a = 3;
per_pixel_1=zoom = zoom + 0.01*rad
wavecode_1_enabled=1
wavecode_1_samples=64
wave_1_init1=t1 = 5;
wave_1_per_frame1=r = q1*0.1;
wave_1_per_point1=x = sample;
wave_1_per_point2=y = 0.5;
shapecode_2_enabled=1
shapecode_2_sides=5
shape_2_per_frame1=x = 0.25;
warp_1=` + "`shader_body {" + `
comp_1=` + "`}" + `
[preset01]
zoom=9
`

// TestParse comprueba los valores, el orden de las líneas de código, las
// ondas y formas, y que los shaders se apuntan pero no se usan.
func TestParse(t *testing.T) {
	p, err := Parse("sample", []byte(samplePreset))
	if err != nil {
		t.Fatal(err)
	}
	if p.Vals["fdecay"] != 0.95 || p.Vals["zoom"] != 1.02 || p.Vals["nwavemode"] != 3 {
		t.Errorf("vals = %v", p.Vals)
	}
	if strings.Index(p.PerFrame, "a = 3") > strings.Index(p.PerFrame, "b = a*2") {
		t.Errorf("per_frame lines out of order: %q", p.PerFrame)
	}
	if !strings.Contains(p.Init, "q1 = 2") || !strings.Contains(p.PerPixel, "0.01*rad") {
		t.Errorf("init %q, per_pixel %q", p.Init, p.PerPixel)
	}
	w := p.Waves[1]
	if w.Vals["enabled"] != 1 || w.Vals["samples"] != 64 || !strings.Contains(w.Init, "t1 = 5") ||
		!strings.Contains(w.PerFrame, "q1*0.1") || !strings.Contains(w.PerPoint, "y = 0.5") {
		t.Errorf("wave 1 = %+v", w)
	}
	sh := p.Shapes[2]
	if sh.Vals["sides"] != 5 || !strings.Contains(sh.PerFrame, "x = 0.25") {
		t.Errorf("shape 2 = %+v", sh)
	}
	if !p.HasShaders {
		t.Error("HasShaders = false")
	}
	if p.Vals["zoom"] == 9 {
		t.Error("second section was read")
	}
}

// TestParseEmpty comprueba que un archivo sin nada da error.
func TestParseEmpty(t *testing.T) {
	if _, err := Parse("x", []byte("hola\n[preset00]\n")); err == nil {
		t.Error("empty preset parsed")
	}
}

// TestBuiltinPresets comprueba que los presets de serie se leen y compilan
// sin errores, y que hay entre seis y diez.
func TestBuiltinPresets(t *testing.T) {
	list := Builtin()
	if len(list) < 6 || len(list) > 10 {
		t.Fatalf("%d builtin presets", len(list))
	}
	r := NewRenderer(Options{Width: 64, Height: 48, MeshX: 8, MeshY: 6})
	for _, e := range list {
		p, err := Load(e)
		if err != nil {
			t.Errorf("%s: %v", e.Name, err)
			continue
		}
		if w := r.SetPreset(p, 0); len(w) > 0 {
			t.Errorf("%s: %v", e.Name, w)
		}
		if strings.Contains(e.Name, "-") {
			t.Errorf("name not prettified: %q", e.Name)
		}
	}
}

// TestUserPresets comprueba que se encuentran los de las subcarpetas.
func TestUserPresets(t *testing.T) {
	dir := t.TempDir()
	if err := writeFile(dir+"/pack/sub/My Preset.MILK", "[preset00]\nzoom=1.1\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(dir+"/readme.txt", "no"); err != nil {
		t.Fatal(err)
	}
	l := NewLibrary(dir)
	all := l.Entries()
	if len(all) != len(Builtin())+1 {
		t.Fatalf("%d entries", len(all))
	}
	e := all[len(all)-1]
	if e.Name != "My Preset" || e.Builtin {
		t.Errorf("entry = %+v", e)
	}
	p, err := Load(e)
	if err != nil || p.Vals["zoom"] != 1.1 {
		t.Errorf("load: %v %v", p, err)
	}
}
