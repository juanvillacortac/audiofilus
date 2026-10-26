package ui

import "testing"

// La guía se reparte en columnas a cualquier tamaño, con y sin los gestos
// del sleep: en la PC (sin sleep) y con la ventana estrecha se caía al
// intentar juntar una segunda columna que no había.
func TestGuideColumns(t *testing.T) {
	own, all := []hint{{"A", "g_pause"}}, []hint{{"SEL", "g_screens"}}
	for _, sleep := range []bool{false, true} {
		for _, sz := range [][2]int{{240, 320}, {240, 1000}, {320, 240}, {640, 480}} {
			cols := guideColumns(own, all, sleep, sz[0], sz[1])
			if len(cols) == 0 || len(cols[0]) < 2 || cols[0][0].head != "g_here" {
				t.Errorf("sleep %v, %dx%d: %v", sleep, sz[0], sz[1], cols)
			}
		}
	}
	if cols := guideColumns(own, all, true, 240, 1000); len(cols) != 1 || len(cols[0]) != 4 {
		t.Errorf("tall and narrow: want one column with all four sections, got %v", cols)
	}
}
