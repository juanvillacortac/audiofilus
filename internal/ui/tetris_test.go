package ui

import "testing"

// Al fijarse una pieza sale un haz por cada una de sus columnas, desde la
// celda más alta de la pieza en esa columna; los haces ya apagados se quitan.
func TestBlocksBeams(t *testing.T) {
	g := &tetris{hold: -1}
	g.piece, g.rot, g.px, g.py = pieceT, 0, 3, 10 // T: arriba (4,10), abajo (3..5,11)
	g.beams = []tetBeam{{x: 0, top: 5, at: 0}}
	g.addBeams(1, false)
	if len(g.beams) != 3 {
		t.Fatalf("got %d beams, want 3 (one per column, old one gone)", len(g.beams))
	}
	want := map[int]int{3: 11, 4: 10, 5: 11}
	for _, b := range g.beams {
		if want[b.x] != b.top || b.piece != pieceT || b.at != 1 {
			t.Errorf("beam %+v, want top %d for column %d", b, want[b.x], b.x)
		}
	}
}

// Soltar la pieza de golpe da haces largos; si cae sola, cortos.
func TestBlocksHardDropBeams(t *testing.T) {
	a := &App{}
	a.tetris = &tetris{hold: -1, queue: []int{1, 2, 3, 4}}
	a.tetris.spawn(pieceO)
	a.t = 5
	a.hardDrop()
	for _, b := range a.tetris.beams {
		if !b.hard {
			t.Errorf("hard drop gave a short beam: %+v", b)
		}
	}
	g := a.tetris
	for g.down() {
	}
	g.lock(6, 9)
	for _, b := range g.beams {
		if b.at == 6 && b.hard {
			t.Errorf("a piece that fell by itself gave a long beam: %+v", b)
		}
	}
}

// Con el teclado, Bloques se juega como los juegos modernos y su ayuda nombra
// esas teclas; con mando, los botones de siempre.
func TestBlocksKeyboardHints(t *testing.T) {
	a := &App{}
	if !a.tetKeyboard() {
		t.Fatal("no gamepad and no touch should be the keyboard")
	}
	a.screen = Blocks
	own, all := a.guideRows()
	if a.btn(own[2].btn) != "SPACE" || a.btn(all[3].btn) != "P" {
		t.Errorf("keyboard guide: drop %q, music %q; want SPACE and P", a.btn(own[2].btn), a.btn(all[3].btn))
	}
	a.input.usingPad = true
	if a.tetKeyboard() {
		t.Error("with a gamepad Blocks still uses the keyboard keys")
	}
}

// fillRows llena las filas de abajo menos una columna, para que una pieza
// las complete.
func fillRows(g *tetris, rows, hole int) {
	for y := tetRows - rows; y < tetRows; y++ {
		for x := 0; x < tetW; x++ {
			if x != hole {
				g.board[y][x] = 1
			}
		}
	}
}

// El locutor dice la jugada: dos líneas son DOUBLE y cuatro TETRA; un TETRA
// tras otro, BACK TO BACK; subir de nivel, LEVEL UP. Y cada jugada suena.
func TestBlocksNarrator(t *testing.T) {
	said := func(g *tetris, want string) {
		t.Helper()
		for _, s := range g.sounds {
			if s == "voice:"+want {
				g.sounds = g.sounds[:0]
				return
			}
		}
		t.Errorf("sounds %v, want the narrator to say %q", g.sounds, want)
		g.sounds = g.sounds[:0]
	}
	g := &tetris{hold: -1, queue: []int{0, 0, 0, 0, 0, 0}}
	fillRows(g, 2, 0)
	g.piece, g.rot, g.px, g.py = pieceI, 1, -2, tetRows-4 // la I de pie en la columna 0
	g.lock(1, 9)
	said(g, "double")
	if g.call != "DOUBLE" {
		t.Errorf("call %q, want DOUBLE", g.call)
	}
	g.finishClear()
	g.sounds = g.sounds[:0]

	g.lines = 3 // dos TETRA seguidos: el segundo es BACK TO BACK y sube de nivel
	for i, want := range []string{"tetra", "b2b+tetra"} {
		fillRows(g, 4, 0)
		g.piece, g.rot, g.px, g.py = pieceI, 1, -2, tetRows-4
		g.lock(float64(2+i), 9)
		said(g, want)
		g.finishClear()
	}
	said(g, "level")
}
