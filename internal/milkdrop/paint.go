package milkdrop

import "math"

// Lo que un preset pinta encima de la realimentación, en este orden (el de
// MilkDrop): los vectores de movimiento, las formas propias, las ondas
// propias, la onda principal y los bordes. Las posiciones de MilkDrop van de
// 0 a 1 con y hacia arriba; aquí pasan a píxeles con y hacia abajo.

// paint pinta lo de in con el alfa multiplicado por k (la mezcla).
func (r *Renderer) paint(in *instance, k float64) {
	if k <= 0.002 {
		return
	}
	v := &in.vals
	r.motionVectors(v, k)
	for _, sh := range in.shapes {
		if sh.enabled {
			r.shape(in, sh, k)
		}
	}
	for _, w := range in.waves {
		if w.enabled {
			r.customWave(in, w, k)
		}
	}
	r.mainWave(v, k)
	r.borders(v, k)
}

// px pasa una posición de MilkDrop a píxeles.
func (r *Renderer) px(x, y float64) (float32, float32) {
	return float32(x * float64(r.w)), float32((1 - y) * float64(r.h))
}

// color32 es un color de MilkDrop (que puede salirse de 0..1) con el alfa
// multiplicado por k.
func color32(cr, cg, cb, ca, k float64) rgba {
	return rgba{float32(clamp(cr, 0, 1)), float32(clamp(cg, 0, 1)), float32(clamp(cb, 0, 1)), float32(clamp(ca*k, 0, 1))}
}

// waveColor es el color de la onda principal: con wave_brighten, el canal más
// fuerte sube a 1; con modwavealphabyvolume, el alfa sigue al volumen.
func (r *Renderer) waveColor(v *frameVals, k float64) rgba {
	cr, cg, cb, ca := clamp(v[oWaveR], 0, 1), clamp(v[oWaveG], 0, 1), clamp(v[oWaveB], 0, 1), v[oWaveA]
	if v[oWaveBright] != 0 {
		if hi := math.Max(cr, math.Max(cg, cb)); hi > 0.01 {
			cr, cg, cb = cr/hi, cg/hi, cb/hi
		}
	}
	if v[oModAlpha] != 0 {
		vol := (r.e.bass + r.e.mid + r.e.treb) / 3
		lo, hi := v[oModAlphaStart], v[oModAlphaEnd]
		if hi-lo > 1e-4 {
			ca *= clamp((vol-lo)/(hi-lo), 0, 1)
		} else if vol < lo {
			ca = 0
		}
	}
	return color32(cr, cg, cb, ca, k)
}

// mainWave es la onda principal, con los ocho modos de MilkDrop 1
// (aproximados: la onda es sintetizada).
func (r *Renderer) mainWave(v *frameVals, k float64) {
	c := r.waveColor(v, k)
	mode := int(math.Floor(v[oWaveMode])) % 8
	if mode < 0 {
		mode += 8
	}
	if mode == 3 {
		c.a *= float32(clamp(r.e.trebAtt*r.e.trebAtt*0.6, 0, 1))
	}
	if c.a <= 0.002 {
		return
	}
	n := r.opt.WavePoints
	L, R := &r.wave[0], &r.wave[1]
	at := func(ch *[wavePoints]float64, i int) float64 { return ch[(i%n+n)%n*wavePoints/n] }
	scale := v[oWaveScale]
	myst := v[oWaveMystery]
	cx, cy := v[oWaveX], v[oWaveY]
	asp := float64(r.h) / float64(r.w) // para que los círculos sean redondos
	thick := float32(1)
	if v[oWaveThick] != 0 {
		thick = 2
	}
	dots, add := v[oWaveDots] != 0, v[oWaveAdd] != 0
	t := r.time
	var px, py float32
	closed := false
	for i := 0; i < n; i++ {
		var x, y float64
		f := float64(i) / float64(n-1)
		switch mode {
		case 0: // un círculo cuyo radio es la onda
			ang := f*2*math.Pi + t*0.2
			rad := (0.5 + 0.4*at(R, i)*scale + myst) * 0.5
			x, y = cx+rad*math.Cos(ang)*asp, cy+rad*math.Sin(ang)
			closed = true
		case 1: // osciloscopio x-y en espiral
			rad := (0.53 + 0.43*at(R, i)*scale + myst) * 0.5
			ang := at(L, i+32)*1.57 + t*2.3
			x, y = cx+rad*math.Cos(ang)*asp, cy+rad*math.Sin(ang)
		case 2, 3: // osciloscopio x-y centrado
			x, y = cx+at(L, i)*scale*0.5*asp, cy+at(R, i+32)*scale*0.5
		case 4: // horizontal, con la derivada
			d := at(L, i) - at(L, i-1)
			x = f*1.1 - 0.05 + myst*0.1
			y = cy + at(L, i)*0.25*scale + d*0.1
		case 5: // la "explosión": producto de canales, girando
			l, rr := at(L, i), at(R, i+32)
			x0, y0 := (l*l-rr*rr)*scale*0.6, 2*l*rr*scale*0.6
			ca, sa := math.Cos(t*0.3), math.Sin(t*0.3)
			x, y = cx+(x0*ca-y0*sa)*asp, cy+x0*sa+y0*ca
		case 6: // una línea en el ángulo que dice wave_mystery
			ang := myst * math.Pi / 2
			s := f - 0.5
			off := at(L, i) * 0.25 * scale
			x = cx + s*math.Cos(ang)*1.4 - off*math.Sin(ang)*asp
			y = cy + s*math.Sin(ang)*1.4 + off*math.Cos(ang)
		case 7: // dos líneas, una por canal, separadas por wave_mystery
			sep := (myst + 1) * 0.15
			x = f
			if i%2 == 0 {
				y = cy + sep + at(L, i)*0.2*scale
			} else {
				y = cy - sep + at(R, i)*0.2*scale
			}
		}
		qx, qy := r.px(x, y)
		switch {
		case dots:
			r.geo.dot(qx, qy, thick+0.5, c, add)
		case mode == 7 && i >= 2:
			// Cada canal, con su punto de dos antes.
			r.geo.line(r.wavePrev[i%2][0], r.wavePrev[i%2][1], qx, qy, thick, c, c, add)
		case mode != 7 && i > 0:
			r.geo.line(px, py, qx, qy, thick, c, c, add)
		}
		if mode == 7 {
			r.wavePrev[i%2] = [2]float32{qx, qy}
		}
		if i == 0 {
			r.waveFirst = [2]float32{qx, qy}
		}
		px, py = qx, qy
	}
	if closed && !dots {
		r.geo.line(px, py, r.waveFirst[0], r.waveFirst[1], thick, c, c, add)
	}
}

// customWave es una onda propia: su código por fotograma y el de cada punto
// deciden dónde va y de qué color.
func (r *Renderer) customWave(in *instance, w *waveInst, k float64) {
	n := w.runFrame(in, &r.e)
	if n < 2 {
		return
	}
	thick := float32(1)
	if !w.thin && n <= 128 {
		thick = 1.5
	}
	sepPts := int(clamp(w.sep, 0, wavePoints/2))
	var px, py float32
	var pc rgba
	for i := 0; i < n; i++ {
		s := float64(i) / float64(n-1)
		var v1, v2 float64
		if w.spectrum {
			v1 = bandAtF(&r.bands, s) * w.scaling * 0.5
			v2 = bandAtF(&r.bands, math.Min(1, s+float64(sepPts)/wavePoints)) * w.scaling * 0.5
		} else {
			j := i * (wavePoints - sepPts) / n
			v1 = r.wave[0][j] * w.scaling * 0.5
			v2 = r.wave[1][min(wavePoints-1, j+sepPts)] * w.scaling * 0.5
		}
		x, y, cr, cg, cb, ca := w.point(s, v1, v2)
		c := color32(cr, cg, cb, ca, k)
		qx, qy := r.px(x, y)
		if w.dots {
			r.geo.dot(qx, qy, thick+0.5, c, w.add)
		} else if i > 0 && (c.a > 0.002 || pc.a > 0.002) {
			r.geo.line(px, py, qx, qy, thick, pc, c, w.add)
		}
		px, py, pc = qx, qy, c
	}
}

// bandAtF interpola el espectro en f (0..1).
func bandAtF(b *[nBands]float64, f float64) float64 {
	pos := clamp(f, 0, 1) * (nBands - 1)
	i := int(pos)
	v := b[i]
	if i+1 < nBands {
		v += (b[i+1] - v) * (pos - float64(i))
	}
	return v
}

// shape es una forma propia (un polígono con el centro de un color y el borde
// de otro, con textura o sin ella), num_inst veces.
func (r *Renderer) shape(in *instance, sh *shapeInst, k float64) {
	for inst := 0; inst < sh.instances(); inst++ {
		sh.runInstance(in, &r.e, inst)
		v := &sh.vals
		sides := int(clamp(v[sSides], 3, 100))
		c1 := color32(v[sR], v[sG], v[sB], v[sA], k)
		c2 := color32(v[sR2], v[sG2], v[sB2], v[sA2], k)
		cb := color32(v[sBorderR], v[sBorderG], v[sBorderB], v[sBorderA], k)
		textured := v[sTextured] != 0
		add := v[sAdditive] != 0
		if c1.a <= 0.002 && c2.a <= 0.002 && cb.a <= 0.002 && !textured {
			continue
		}
		cx, cy := r.px(v[sX], v[sY])
		rad := v[sRad] * float64(r.h) / 2
		texZoom := nonZero(v[sTexZoom], 1)
		asp := float64(r.h) / float64(r.w)
		g := &r.geo
		if c1.a > 0.002 || c2.a > 0.002 || textured {
			if textured {
				// Con textura, el color es un tinte (a 1 si el preset no da
				// uno: MilkDrop multiplica por él).
				if c1.r+c1.g+c1.b == 0 {
					c1.r, c1.g, c1.b = 1, 1, 1
				}
				if c2.r+c2.g+c2.b == 0 {
					c2.r, c2.g, c2.b = 1, 1, 1
				}
			}
			base := g.begin(textured, add, sides+1)
			g.tvertOr(textured, cx, cy, float32(0.5*float64(r.w)), float32(0.5*float64(r.h)), c1)
			for s := 0; s < sides; s++ {
				ang := float64(s)/float64(sides)*2*math.Pi + v[sAng] + math.Pi/4
				x := float64(cx) + rad*math.Cos(ang)
				y := float64(cy) - rad*math.Sin(ang)
				ta := ang + v[sTexAng]
				tu := (0.5 + 0.5*math.Cos(ta)/texZoom*asp) * float64(r.w)
				tv := (0.5 - 0.5*math.Sin(ta)/texZoom) * float64(r.h)
				g.tvertOr(textured, float32(x), float32(y), float32(tu), float32(tv), c2)
				g.tri(base, 0, uint16(1+s), uint16(1+(s+1)%sides))
			}
		}
		if cb.a > 0.002 {
			thick := float32(1)
			if v[sThick] != 0 {
				thick = 2
			}
			var lx, ly float32
			for s := 0; s <= sides; s++ {
				ang := float64(s%sides)/float64(sides)*2*math.Pi + v[sAng] + math.Pi/4
				x := cx + float32(rad*math.Cos(ang))
				y := cy - float32(rad*math.Sin(ang))
				if s > 0 {
					g.line(lx, ly, x, y, thick, cb, cb, add)
				}
				lx, ly = x, y
			}
		}
	}
}

// tvertOr añade un vértice con textura o sin ella.
func (b *batch) tvertOr(textured bool, x, y, sx, sy float32, c rgba) {
	if textured {
		b.tvert(x, y, sx, sy, c)
	} else {
		b.vert(x, y, c)
	}
}

// borders son los bordes de fuera y de dentro.
func (r *Renderer) borders(v *frameVals, k float64) {
	w, h := float32(r.w), float32(r.h)
	edge := func(off, size float64, c rgba) float32 {
		if c.a <= 0.002 || size <= 0 {
			return float32(off)
		}
		ox, oy := float32(off)*w, float32(off)*h
		tx, ty := max(1, float32(size)*w), max(1, float32(size)*h)
		g := &r.geo
		g.rect(ox, oy, w-2*ox, ty, c, false)              // arriba
		g.rect(ox, h-oy-ty, w-2*ox, ty, c, false)         // abajo
		g.rect(ox, oy+ty, tx, h-2*oy-2*ty, c, false)      // izquierda
		g.rect(w-ox-tx, oy+ty, tx, h-2*oy-2*ty, c, false) // derecha
		return float32(off + size)
	}
	obSize, ibSize := clamp(v[oOBSize], 0, 0.5), clamp(v[oIBSize], 0, 0.5)
	edge(0, obSize, color32(v[oOBR], v[oOBG], v[oOBB], v[oOBA], k))
	edge(obSize, ibSize, color32(v[oIBR], v[oIBG], v[oIBB], v[oIBA], k))
}

// motionVectors son las rayitas de MilkDrop que enseñan hacia dónde se mueve
// la imagen: una rejilla de mv_x × mv_y, cada una de su punto al lugar de
// donde la malla toma el color.
func (r *Renderer) motionVectors(v *frameVals, k float64) {
	c := color32(v[oMVR], v[oMVG], v[oMVB], v[oMVA], k)
	if c.a <= 0.002 {
		return
	}
	nx, ny := int(clamp(v[oMVX], 1, 64)), int(clamp(v[oMVY], 1, 48))
	for j := 0; j < ny; j++ {
		for i := 0; i < nx; i++ {
			x := (float64(i) + 0.25 + v[oMVDX]) / float64(nx)
			y := (float64(j) + 0.25 + v[oMVDY]) / float64(ny)
			if x < 0 || x > 1 || y < 0 || y > 1 {
				continue
			}
			u, vv := r.uvAt(x, y)
			l := v[oMVL] * 6
			x1, y1 := x-(u-x)*l, y-(vv-y)*l
			r.geo.line(float32(x*float64(r.w)), float32(y*float64(r.h)), float32(x1*float64(r.w)), float32(y1*float64(r.h)), 1, c, c, false)
		}
	}
}

// uvAt interpola la malla en (x, y) (de 0 a 1, y hacia abajo).
func (r *Renderer) uvAt(x, y float64) (float64, float64) {
	fx, fy := x*float64(r.gx), y*float64(r.gy)
	i, j := min(int(fx), r.gx-1), min(int(fy), r.gy-1)
	ax, ay := fx-float64(i), fy-float64(j)
	at := func(ii, jj, c int) float64 { return r.uvCur[2*(jj*(r.gx+1)+ii)+c] }
	var out [2]float64
	for c := range out {
		top := at(i, j, c) + (at(i+1, j, c)-at(i, j, c))*ax
		bot := at(i, j+1, c) + (at(i+1, j+1, c)-at(i, j+1, c))*ax
		out[c] = top + (bot-top)*ay
	}
	return out[0], out[1]
}
