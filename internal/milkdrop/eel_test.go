package milkdrop

import (
	"math"
	"strings"
	"testing"
)

// eval compila y evalúa src en un contexto nuevo y devuelve el valor y el
// contexto.
func eval(t *testing.T, src string) (float64, *Scope) {
	t.Helper()
	s := NewScope(NewGlobals(1))
	c, err := Compile(src, s)
	if err != nil {
		t.Fatalf("compile %q: %v", src, err)
	}
	return c.Run(), s
}

// TestOperators comprueba la aritmética, la precedencia y los operadores de
// comparación y lógicos.
func TestOperators(t *testing.T) {
	cases := map[string]float64{
		"1+2*3":          7,
		"(1+2)*3":        9,
		"10-4-3":         3,
		"2^3^2":          512,
		"-2^2":           -4,
		"7%3":            1,
		"7.9%3.2":        1,
		"-7%3":           -1,
		"1/0":            0,
		"5%0":            0,
		"3<4":            1,
		"3>4":            0,
		"4<=4":           1,
		"4>=5":           0,
		"1==1.000001":    1,
		"1!=2":           1,
		"1&&0":           0,
		"1||0":           1,
		"!0":             1,
		"!3":             0,
		"5|2":            7,
		"6&3":            2,
		"1 ? 10 : 20":    10,
		"0 ? 10 : 20":    20,
		".5+1.":          1.5,
		"1e2":            100,
		"0x10":           16,
		"$pi":            math.Pi,
		"(1; 2; 3)":      3,
		"x = 2; x += 3":  5,
		"x = 2; x *= 4":  8,
		"x = 9; x /= 0":  0,
		"x = 9; x -= 10": -1,
		"x = 10; x %= 4": 2,
	}
	for src, want := range cases {
		if got, _ := eval(t, src); math.Abs(got-want) > 1e-9 {
			t.Errorf("%s = %v, want %v", src, got, want)
		}
	}
}

// TestFunctions comprueba las funciones de MilkDrop, con sus casos raros
// (sqrt y log de negativos, división en sigmoid...).
func TestFunctions(t *testing.T) {
	cases := map[string]float64{
		"sin(0)":                         0,
		"cos(0)":                         1,
		"tan(0)":                         0,
		"asin(2)":                        math.Pi / 2,
		"acos(1)":                        0,
		"atan(0)":                        0,
		"atan2(1, 0)":                    math.Pi / 2,
		"sqrt(16)":                       4,
		"sqrt(-16)":                      4,
		"pow(2, 10)":                     1024,
		"pow(-8, 0.5)":                   0,
		"exp(0)":                         1,
		"log(1)":                         0,
		"log(0)":                         0,
		"log10(1000)":                    3,
		"abs(-3)":                        3,
		"sign(-2)":                       -1,
		"sign(0)":                        0,
		"min(3, 4)":                      3,
		"max(3, 4)":                      4,
		"sigmoid(0, 1)":                  0.5,
		"int(2.7)":                       2,
		"int(-2.7)":                      -2,
		"above(2, 1)":                    1,
		"below(2, 1)":                    0,
		"equal(2, 2)":                    1,
		"bnot(0)":                        1,
		"band(1, 0)":                     0,
		"bor(1, 0)":                      1,
		"if(1, 2, 3)":                    2,
		"if(0, 2, 3)":                    3,
		"sqr(3)":                         9,
		"floor(-1.5)":                    -2,
		"ceil(1.2)":                      2,
		"exec2(1, 2)":                    2,
		"exec3(1, 2, 3)":                 3,
		"i = 0; loop(5, i += 2); i":      10,
		"i = 0; while(i += 1; i < 7); i": 7,
		"megabuf(3) = 5; megabuf(3) + megabuf(4)": 5,
		"gmegabuf(10) = 2; gmegabuf(10)":          2,
		"megabuf(-1) = 5; megabuf(-1)":            0,
	}
	for src, want := range cases {
		if got, _ := eval(t, src); math.Abs(got-want) > 1e-9 {
			t.Errorf("%s = %v, want %v", src, got, want)
		}
	}
}

// TestLazy comprueba que if, band, bor, && y || no evalúan la rama que no toca.
func TestLazy(t *testing.T) {
	_, s := eval(t, "if(1, a = 1, b = 1); band(0, c = 1); bor(1, d = 1); 0 && (e = 1); 1 || (f = 1)")
	if s.Get("a") != 1 {
		t.Error("if did not run the true branch")
	}
	for _, n := range []string{"b", "c", "d", "e", "f"} {
		if s.Get(n) != 0 {
			t.Errorf("%s was evaluated", n)
		}
	}
}

// TestRand comprueba que rand(n) es un entero de 0 a n-1.
func TestRand(t *testing.T) {
	s := NewScope(NewGlobals(7))
	c, err := Compile("rand(10)", s)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[float64]bool{}
	for i := 0; i < 1000; i++ {
		v := c.Run()
		if v < 0 || v > 9 || v != math.Floor(v) {
			t.Fatalf("rand(10) = %v", v)
		}
		seen[v] = true
	}
	if len(seen) != 10 {
		t.Errorf("rand(10) gave %d distinct values, want 10", len(seen))
	}
}

// TestCaseInsensitive comprueba que las variables y las funciones no
// distinguen mayúsculas, y que las regNN son globales.
func TestCaseInsensitive(t *testing.T) {
	g := NewGlobals(1)
	a, b := NewScope(g), NewScope(g)
	c1, err := Compile("Zoom = SIN(0) + 2; REG05 = 4", a)
	if err != nil {
		t.Fatal(err)
	}
	c1.Run()
	if a.Get("zoom") != 2 {
		t.Errorf("zoom = %v, want 2", a.Get("zoom"))
	}
	if b.Get("reg05") != 4 || g.Reg[5] != 4 {
		t.Errorf("reg05 not shared: %v", b.Get("reg05"))
	}
	if b.Get("zoom") != 0 {
		t.Error("ordinary variables leaked between scopes")
	}
}

// TestLines comprueba que las líneas sin ';' final, los comentarios y las
// sentencias vacías se aceptan, como en los presets de verdad.
func TestLines(t *testing.T) {
	v, s := eval(t, "a = 1\nb = a + 1 // comentario\n;; /* otro */ c = b * 3")
	if v != 6 || s.Get("b") != 2 {
		t.Errorf("got %v, b = %v", v, s.Get("b"))
	}
	if _, err := Compile("", NewScope(NewGlobals(1))); err != nil {
		t.Errorf("empty code: %v", err)
	}
}

// TestErrors comprueba que el código roto da un error y no un pánico.
func TestErrors(t *testing.T) {
	for _, src := range []string{"1 +", "(1", "foo(1)", "sin(1, 2)", "3 = 4", "a = ", "@", "if(1, 2)"} {
		if _, err := Compile(src, NewScope(NewGlobals(1))); err == nil {
			t.Errorf("%q compiled", src)
		} else if strings.Contains(err.Error(), "%!") {
			t.Errorf("%q: bad message %v", src, err)
		}
	}
}

// TestNoAllocs comprueba que evaluar no crea memoria.
func TestNoAllocs(t *testing.T) {
	s := NewScope(NewGlobals(1))
	c, err := Compile("x = sin(time)*0.5 + above(bass, 1)*rand(4); y = if(x > 0, x, -x); megabuf(2) = y; q1 = megabuf(2) + pow(y, 2)", s)
	if err != nil {
		t.Fatal(err)
	}
	c.Run() // el bloque de megabuf se crea aquí
	if n := testing.AllocsPerRun(1000, func() { c.Run() }); n != 0 {
		t.Errorf("Run allocates %v times", n)
	}
}

// BenchmarkEval mide una ecuación típica por vértice.
func BenchmarkEval(b *testing.B) {
	s := NewScope(NewGlobals(1))
	c, _ := Compile("zoom = 1 + 0.05*sin(rad*10 - time*3)*q1; rot = 0.015*sin(ang*3 + time)", s)
	for i := 0; i < b.N; i++ {
		c.Run()
	}
}
