package ui

import (
	"encoding/json"
	"fmt"
	"math"
	"math/cmplx"
	"slices"
	"strconv"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/juanvillacortac/audiofilus/internal/engine"
)

// El ecualizador paramétrico (engine.DSP.PEQ): hasta ocho bandas, cada una
// con su forma (pico, estante grave o agudo, paso bajo o alto), frecuencia,
// ganancia y Q, y un preamp. El editor es una lista: ↑↓ eligen la banda (la
// primera fila es el preamp), ←→ el campo y A mantenida con ↑↓ lo cambia;
// arriba se dibuja la curva que resulta. X añade una banda, B+A quita la
// elegida, START cierra. Cada cambio se manda al servicio al momento.

// peqEditor es el editor abierto.
type peqEditor struct {
	bands  []engine.PEQBand
	preamp float64
	row    int // 0 el preamp, 1.. las bandas
	col    int // el campo: 0 forma, 1 frecuencia, 2 ganancia, 3 Q
}

// openEQMenu es lo que abre la baldosa del ecualizador: elegir el gráfico
// (diez bandas, el de siempre) o el paramétrico, y editar el que se use.
func (a *App) openEQMenu() {
	a.menu = &menu{title: T("set_eq"), items: []menuItem{
		{label: T("eq_graphic"), icon: "eq", active: func() bool { return orDefault(a.dspOf().EQMode, "graphic") == "graphic" },
			close: true, do: func() {
				a.send("eqmode", "graphic")
				a.status.DSP.EQMode = "graphic"
				a.openEQ()
			}},
		{label: T("eq_parametric"), icon: "eq", active: func() bool { return a.dspOf().EQMode == "parametric" },
			close: true, do: func() {
				a.send("eqmode", "parametric")
				a.status.DSP.EQMode = "parametric"
				a.openPEQ()
			}},
		// El perfil de los audífonos, de AutoEq (autoequi.go).
		{label: "AutoEQ", icon: "headphones", do: a.openAutoEQ},
	}}
}

// openPEQ abre el editor con lo que tiene el servicio (sin bandas, una de
// pico a 1 kHz para empezar).
func (a *App) openPEQ() {
	d := a.dspOf()
	bands := slices.Clone(d.PEQ)
	if len(bands) == 0 {
		bands = []engine.PEQBand{{Type: "peak", Freq: 1000, Q: 1}}
	}
	a.peqEdit = &peqEditor{bands: bands, preamp: d.PEQPreamp, row: 1}
	a.sendPEQ()
}

// sendPEQ manda las bandas y el preamp al servicio.
func (a *App) sendPEQ() {
	e := a.peqEdit
	if b, err := json.Marshal(e.bands); err == nil {
		a.send("peq", string(b))
	}
	a.send("peqpreamp", strconv.FormatFloat(e.preamp, 'f', 1, 64))
	a.status.DSP.PEQ, a.status.DSP.PEQPreamp = slices.Clone(e.bands), e.preamp
}

// peqStep cambia el campo elegido d pasos (A+↑↓ uno; L1/R1 seis).
func (a *App) peqStep(d float64) {
	e := a.peqEdit
	if e.row == 0 {
		e.preamp = math.Max(-24, math.Min(12, math.Round((e.preamp+d*0.5)*2)/2))
		a.sendPEQ()
		return
	}
	b := &e.bands[e.row-1]
	switch e.col {
	case 0:
		i := slices.Index(engine.PEQTypes, b.Type)
		n := len(engine.PEQTypes)
		b.Type = engine.PEQTypes[((i+int(math.Copysign(1, d)))%n+n)%n]
	case 1:
		// La frecuencia, en pasos de 1/12 de octava (más con L1/R1).
		b.Freq = math.Max(20, math.Min(20000, math.Round(b.Freq*math.Pow(2, d/12))))
	case 2:
		b.Gain = math.Max(-15, math.Min(15, math.Round((b.Gain+d*0.5)*2)/2))
	case 3:
		b.Q = math.Max(0.3, math.Min(10, math.Round(b.Q*math.Pow(2, d/8)*100)/100))
	}
	a.sendPEQ()
}

// updatePEQ atiende el editor: ↑↓ la fila, ←→ el campo, A mantenida con
// ↑↓ cambia el valor (L1/R1 de a mucho), X añade una banda, Y quita la
// elegida, B o START cierran (los cambios ya están).
func (a *App) updatePEQ() {
	e, in := a.peqEdit, &a.input
	aHeld := in.duration(ActConfirm) > 0
	rows := len(e.bands) + 1
	switch {
	case in.Pressed(ActMenu), in.Pressed(ActBack):
		a.peqEdit = nil
	case in.Pressed(ActPause) && e.row > 0:
		e.bands = slices.Delete(e.bands, e.row-1, e.row)
		e.row = min(e.row, len(e.bands))
		a.sendPEQ()
	case in.Pressed(ActShuffle) && len(e.bands) < engine.PEQMax:
		f := 1000.0
		if len(e.bands) > 0 {
			f = math.Min(16000, e.bands[len(e.bands)-1].Freq*2)
		}
		e.bands = append(e.bands, engine.PEQBand{Type: "peak", Freq: f, Q: 1})
		e.row = len(e.bands)
		a.sendPEQ()
	case in.Pressed(ActPrev):
		a.peqStep(-6)
	case in.Pressed(ActNext):
		a.peqStep(6)
	case aHeld && in.Repeat(ActUp):
		a.peqStep(1)
	case aHeld && in.Repeat(ActDown):
		a.peqStep(-1)
	case in.Repeat(ActUp):
		e.row = (e.row - 1 + rows) % rows
	case in.Repeat(ActDown):
		e.row = (e.row + 1) % rows
	case in.Repeat(ActLeft):
		e.col = (e.col + 3) % 4
	case in.Repeat(ActRight):
		e.col = (e.col + 1) % 4
	}
}

// peqResponse es la ganancia (dB) de las bandas en f, a 48 kHz, con los
// biquads RBJ que usa el motor.
func peqResponse(bands []engine.PEQBand, preamp, f float64) float64 {
	const fs = 48000.0
	db := preamp
	z := cmplx.Exp(complex(0, -2*math.Pi*f/fs))
	for _, b := range bands {
		w0 := 2 * math.Pi * b.Freq / fs
		alpha := math.Sin(w0) / (2 * math.Max(0.3, b.Q))
		A := math.Pow(10, b.Gain/40)
		cs := math.Cos(w0)
		var b0, b1, b2, a0, a1, a2 float64
		switch b.Type {
		case "lowshelf":
			sq := 2 * math.Sqrt(A) * alpha
			b0, b1, b2 = A*((A+1)-(A-1)*cs+sq), 2*A*((A-1)-(A+1)*cs), A*((A+1)-(A-1)*cs-sq)
			a0, a1, a2 = (A+1)+(A-1)*cs+sq, -2*((A-1)+(A+1)*cs), (A+1)+(A-1)*cs-sq
		case "highshelf":
			sq := 2 * math.Sqrt(A) * alpha
			b0, b1, b2 = A*((A+1)+(A-1)*cs+sq), -2*A*((A-1)+(A+1)*cs), A*((A+1)+(A-1)*cs-sq)
			a0, a1, a2 = (A+1)-(A-1)*cs+sq, 2*((A-1)-(A+1)*cs), (A+1)-(A-1)*cs-sq
		case "lowpass":
			b0, b1, b2 = (1-cs)/2, 1-cs, (1-cs)/2
			a0, a1, a2 = 1+alpha, -2*cs, 1-alpha
		case "highpass":
			b0, b1, b2 = (1+cs)/2, -(1 + cs), (1+cs)/2
			a0, a1, a2 = 1+alpha, -2*cs, 1-alpha
		default: // peak
			b0, b1, b2 = 1+alpha*A, -2*cs, 1-alpha*A
			a0, a1, a2 = 1+alpha/A, -2*cs, 1-alpha/A
		}
		h := (complex(b0, 0) + complex(b1, 0)*z + complex(b2, 0)*z*z) / (complex(a0, 0) + complex(a1, 0)*z + complex(a2, 0)*z*z)
		db += 20 * math.Log10(math.Max(1e-6, cmplx.Abs(h)))
	}
	return db
}

// freqLabel es una frecuencia corta: "80", "1.2k".
func freqLabel(f float64) string {
	if f >= 1000 {
		return strconv.FormatFloat(f/1000, 'f', -1, 64)[:min(4, len(strconv.FormatFloat(f/1000, 'f', -1, 64)))] + "k"
	}
	return fmt.Sprintf("%.0f", f)
}

// drawPEQ pinta el editor: la curva arriba y la lista de bandas.
func (a *App) drawPEQ(dst *ebiten.Image) {
	e := a.peqEdit
	if e == nil {
		return
	}
	th := a.theme
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	rect(dst, 0, 0, w, h, withAlpha(th.Shadow, 0xb0))
	pw, ph := min(w-2*margin, 360), min(h-topH-bottomH-12, 210)
	px, py := (w-pw)/2, topH+(h-topH-bottomH-ph)/2
	panel(dst, px, py, pw, ph, th.Bg2)
	rect(dst, px+1, py, pw-2, 1, th.Accent)
	drawIcon(dst, "eq", px+8, py+7, th.Accent)
	drawText(dst, T("eq_parametric"), SmallFace, px+20, py+8, th.Text)
	// La curva: de 20 Hz a 20 kHz en escala logarítmica, ±15 dB. Con muchas
	// bandas (las diez de un perfil de AutoEQ) se encoge para que quepan.
	const rowH = 11
	cx, cy, cw := px+8, py+20, pw-16
	chh := max(30, min(56, ph-20-8-(len(e.bands)+1)*rowH-4))
	panel(dst, cx, cy, cw, chh, withAlpha(th.Bg, 0x90))
	zero := cy + chh/2
	rect(dst, cx, zero, cw, 1, withAlpha(th.Dim, 0x60))
	prev := zero
	for x := 0; x < cw; x++ {
		f := 20 * math.Pow(1000, float64(x)/float64(cw-1))
		db := math.Max(-15, math.Min(15, peqResponse(e.bands, e.preamp, f)))
		y := zero - int(math.Round(db/15*float64(chh/2-2)))
		lo, hi := min(prev, y), max(prev, y)
		if x == 0 {
			lo, hi = y, y
		}
		rect(dst, cx+x, lo, 1, hi-lo+1, th.Accent)
		prev = y
	}
	// Las bandas: una fila cada una (la primera, el preamp).
	y := cy + chh + 8
	pre := fmt.Sprintf("%s  %+.1f dB", T("eq_preamp"), e.preamp)
	if e.row == 0 {
		panel(dst, px+6, y-2, pw-12, rowH, mix(th.Bg2, th.Accent, 0.35))
	}
	drawText(dst, pre, SmallFace, px+10, y, th.Text)
	y += rowH
	colX := []int{px + 22, px + 22 + (pw-30)*30/100, px + 22 + (pw-30)*55/100, px + 22 + (pw-30)*80/100}
	for i, b := range e.bands {
		ry := y + i*rowH
		sel := e.row == i+1
		if sel {
			panel(dst, px+6, ry-2, pw-12, rowH, mix(th.Bg2, th.Accent, 0.25))
		}
		drawText(dst, fmt.Sprint(i+1), SmallFace, px+10, ry, th.Dim)
		vals := []string{T("peq_" + b.Type), freqLabel(b.Freq) + " Hz", fmt.Sprintf("%+.1f dB", b.Gain), fmt.Sprintf("Q %.2f", b.Q)}
		for c, v := range vals {
			col := th.Text
			if (b.Type == "lowpass" || b.Type == "highpass") && c == 2 {
				col = withAlpha(th.Dim, 0x80) // la ganancia no cuenta
			}
			if sel && c == e.col {
				vw := textWidth(v, SmallFace)
				rect(dst, colX[c]-1, ry-2, vw+2, rowH, th.Accent)
				col = th.Bg
			}
			drawText(dst, v, SmallFace, colX[c], ry, col)
		}
	}
	// La ayuda, sobre la de la pantalla de debajo (que no se transparente).
	rect(dst, 0, h-bottomH, w, bottomH, th.Bg)
	a.drawHints(dst, []hint{{"A+^v", T("tk_h_edit")}, {"X", T("peq_h_add")}, {"Y", T("peq_h_del")}, {"B", T("h_ok")}})
}
