package input

import (
	"testing"
	"time"
)

// rg34xxsp son los códigos de botones de la RG34XX-SP, sacados de su
// /proc/bus/input/devices: B: KEY=400000000 1fff000000000000 0 0 c000000000000 2.
var rg34xxspKeys = bits([]string{"400000000", "1fff000000000000", "0", "0", "c000000000000", "2"})

// TestBitsRG34XXSP comprueba la lectura del mapa de bits del kernel.
func TestBitsRG34XXSP(t *testing.T) {
	want := []uint16{1, 114, 115, 304, 305, 306, 307, 308, 309, 310, 311, 312, 313, 314, 315, 316, 354}
	if len(rg34xxspKeys) != len(want) {
		t.Fatalf("got %v, want %v", rg34xxspKeys, want)
	}
	for i := range want {
		if rg34xxspKeys[i] != want[i] {
			t.Fatalf("got %v, want %v", rg34xxspKeys, want)
		}
	}
}

// TestFromSDLNumbering comprueba que el mapeo de Knulli para la RG34XX-SP
// ("ANBERNIC-keys", en PortMaster/knulli/gamecontrollerdb.txt), con la
// numeración de SDL, dé los códigos que se capturaron pulsando cada botón en la
// consola: L1=0x134, R1=0x135, L2=0x13a, R2=0x13b, Start=0x137.
func TestFromSDLNumbering(t *testing.T) {
	mapping := "19000000010000000100000000010000,ANBERNIC-keys,b:b3,a:b4,dpdown:h0.4,leftx:a0,lefty:a1," +
		"rightx:a2,righty:a3,lefttrigger:b13,leftstick:b12,dpleft:h0.8,rightshoulder:b8,leftshoulder:b7," +
		"righttrigger:b14,rightstick:b15,dpright:h0.2,back:b9,start:b10,dpup:h0.1,y:b6,x:b5,guide:b11,platform:Linux,"
	l := FromSDL(mapping, rg34xxspKeys, nil)
	want := map[uint16]Button{0x134: L1, 0x135: R1, 0x13a: L2, 0x13b: R2, 0x137: Start, 0x136: Select, 0x138: FN}
	for code, b := range want {
		if l.Keys[code] != b {
			t.Errorf("code %#x = %v, want %v (layout %s)", code, l.Keys[code], b, l.Describe())
		}
	}
}

// TestFromSDLAxisTriggers comprueba que los gatillos analógicos ("aN") salten
// los hats en la numeración de ejes, como en un mando de Xbox.
func TestFromSDLAxisTriggers(t *testing.T) {
	abs := []uint16{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x10, 0x11} // X Y Z RX RY RZ HAT0X HAT0Y
	l := FromSDL("x,Pad,lefttrigger:a2,righttrigger:a5,", nil, abs)
	if l.Axes[0x02] != L2 || l.Axes[0x05] != R2 {
		t.Fatalf("axes = %v, want Z=L2 and RZ=R2", l.Axes)
	}
}

type step struct {
	b       Button
	pressed bool
	at      int // ms
	want    Action
}

func feed(t *testing.T, on func(Event) Action, steps []step) {
	t.Helper()
	t0 := time.Unix(1000, 0)
	for i, s := range steps {
		got := on(Event{Button: s.b, Pressed: s.pressed, Time: t0.Add(time.Duration(s.at) * time.Millisecond)})
		if got != s.want {
			t.Fatalf("step %d (%v pressed=%v at %dms): got %v, want %v", i, s.b, s.pressed, s.at, got, s.want)
		}
	}
}

// TestSleepGestures comprueba los dobles toques, el doble acorde y que lo suelto
// no haga nada.
func TestSleepGestures(t *testing.T) {
	d := NewDetector(400*time.Millisecond, 600*time.Millisecond)
	feed(t, d.Event, []step{
		{R1, true, 0, NoAction}, {R1, false, 50, NoAction}, {R1, true, 900, NoAction}, // demasiado tarde
		{R1, false, 950, NoAction}, {R1, true, 1100, Next}, {R1, false, 1150, NoAction},
		{L1, true, 2000, NoAction}, {L1, false, 2050, NoAction}, {L1, true, 2200, Prev}, {L1, false, 2250, NoAction},
		{L2, true, 3000, NoAction}, {L2, false, 3050, NoAction}, {L2, true, 3300, SeekBack}, {L2, false, 3350, NoAction},
		{R2, true, 4000, NoAction}, {R2, false, 4050, NoAction}, {R2, true, 4300, SeekForward}, {R2, false, 4350, NoAction},
		// Doble acorde L1+L2, el segundo en el otro orden.
		{L1, true, 5000, NoAction}, {L2, true, 5030, NoAction}, {L1, false, 5100, NoAction}, {L2, false, 5110, NoAction},
		{L2, true, 5300, NoAction}, {L1, true, 5320, TogglePause}, {L1, false, 5400, NoAction}, {L2, false, 5400, NoAction},
		// Doble acorde R1+R2: canción al azar.
		{R2, true, 5800, NoAction}, {R1, true, 5820, NoAction}, {R1, false, 5900, NoAction}, {R2, false, 5900, NoAction},
		{R1, true, 6100, NoAction}, {R2, true, 6110, Random}, {R1, false, 6200, NoAction}, {R2, false, 6200, NoAction},
		{Start, true, 7000, Wake},
	})
}

// TestChordIsNotATap comprueba que el L1 de un acorde no se empareje con un
// toque de L1 justo anterior.
func TestChordIsNotATap(t *testing.T) {
	d := NewDetector(400*time.Millisecond, 600*time.Millisecond)
	feed(t, d.Event, []step{
		{L1, true, 0, NoAction}, {L1, false, 50, NoAction},
		{L2, true, 150, NoAction}, {L1, true, 170, NoAction},
	})
}

// TestComboHotkeys comprueba el atajo de segundo plano: tres toques de FN y
// luego un botón.
func TestComboHotkeys(t *testing.T) {
	c := NewCombo(3, 400*time.Millisecond, 1500*time.Millisecond)
	fn := func(at int) []step { return []step{{FN, true, at, NoAction}, {FN, false, at + 40, NoAction}} }
	var s []step
	s = append(s, fn(0)...)
	s = append(s, fn(200)...)
	s = append(s, step{FN, true, 400, Armed}, step{FN, false, 440, NoAction})
	s = append(s, step{Select, true, 900, TogglePause}, step{Select, false, 950, NoAction})
	// Después de la acción hay que volver a armar: R1 solo no hace nada.
	s = append(s, step{R1, true, 1000, NoAction}, step{R1, false, 1050, NoAction})
	feed(t, c.Event, s)
}

// TestComboNeedsQuickTaps comprueba que tres toques de FN lentos no armen el
// atajo y que un botón tarde no dispare nada.
func TestComboNeedsQuickTaps(t *testing.T) {
	c := NewCombo(3, 400*time.Millisecond, 1500*time.Millisecond)
	feed(t, c.Event, []step{
		{FN, true, 0, NoAction}, {FN, true, 300, NoAction}, {FN, true, 1000, NoAction}, // el tercero llega tarde
		{FN, true, 1200, NoAction}, {FN, true, 1400, Armed},
		{R1, true, 3500, NoAction}, // fuera de la ventana
	})
}

// TestComboOtherButtonCancels comprueba que otro botón entre los toques de FN
// los anule, para que un juego que use FN no arme el atajo por accidente.
func TestComboOtherButtonCancels(t *testing.T) {
	c := NewCombo(3, 400*time.Millisecond, 1500*time.Millisecond)
	feed(t, c.Event, []step{
		{FN, true, 0, NoAction}, {FN, true, 200, NoAction}, {Select, true, 300, NoAction}, {FN, true, 400, NoAction},
	})
}

// TestHookCounter comprueba el botón único de los audífonos de cable: una,
// dos o tres pulsaciones seguidas se cuentan juntas, y una pulsación tras la
// pausa empieza otra cuenta.
func TestHookCounter(t *testing.T) {
	got := make(chan int, 4)
	h := NewHookCounter(60 * time.Millisecond)
	now := time.Now()
	h.Press(now, func(n int) { got <- n })
	h.Press(now.Add(20*time.Millisecond), func(n int) { got <- n })
	if n := <-got; n != 2 {
		t.Errorf("double press counted as %d", n)
	}
	h.Press(now.Add(time.Second), func(n int) { got <- n })
	if n := <-got; n != 1 {
		t.Errorf("single press after a pause counted as %d", n)
	}
	select {
	case n := <-got:
		t.Errorf("extra count %d", n)
	case <-time.After(100 * time.Millisecond):
	}
}
