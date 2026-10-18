package ui

import (
	"image"

	"github.com/hajimehoshi/ebiten/v2"
)

// coverFade es lo que tarda una portada en aparecer cuando llega.
const coverFade = 0.18

// maxCovers es cuántas portadas reducidas se guardan a la vez en la GPU. Una de
// 52×52 son ~11 KB y una de 128×128, 64 KB; con este límite no pasan de unos
// pocos MB. Cuando se supera, se libera la que lleva más tiempo sin verse.
const maxCovers = 192

// coverImage es una portada lista para dibujar.
type coverImage struct {
	img      *ebiten.Image
	ready    float64 // instante (App.t) en que llegó, para el fundido
	lastUsed float64 // último instante en que se dibujó
}

type coverJob struct {
	al   *Album
	size int
}

type coverResult struct {
	coverJob
	rgba *image.RGBA // nil si el álbum no tiene portada
}

// covers prepara las portadas en segundo plano: leer la imagen del MP3,
// decodificarla, reducirla y tramarla tarda varios milisegundos, y hacerlo en
// el hilo que dibuja daba un tirón al entrar en una pantalla con muchas. El hilo
// de la interfaz solo sube a la GPU las que ya están listas (Update).
type covers struct {
	jobs    chan coverJob
	results chan coverResult
	pending map[coverJob]bool
	loaded  map[coverJob]*coverImage // las que están en la GPU, para el límite
}

func newCovers(workers int) *covers {
	c := &covers{
		jobs:    make(chan coverJob, 512),
		results: make(chan coverResult, 512),
		pending: map[coverJob]bool{},
		loaded:  map[coverJob]*coverImage{},
	}
	for range workers {
		go func() {
			for j := range c.jobs {
				var rgba *image.RGBA
				if src := j.al.decodeCover(); src != nil {
					rgba = scaleCover(src, j.size)
				}
				c.results <- coverResult{j, rgba}
			}
		}()
	}
	return c
}

// request pide una portada si no está ya pedida. Si la cola está llena, se
// vuelve a pedir en el siguiente frame.
func (c *covers) request(al *Album, size int) {
	j := coverJob{al, size}
	if c.pending[j] {
		return
	}
	select {
	case c.jobs <- j:
		c.pending[j] = true
	default:
	}
}

// collect sube a la GPU las portadas que llegaron. Va en Update, en el hilo de
// la interfaz.
func (c *covers) collect(now float64) {
	for {
		select {
		case r := <-c.results:
			delete(c.pending, r.coverJob)
			if r.rgba == nil {
				r.al.missing = true
				continue
			}
			ci := &coverImage{img: ebiten.NewImageFromImage(r.rgba), ready: now, lastUsed: now}
			if r.al.scaled == nil {
				r.al.scaled = map[int]*coverImage{} // un álbum armado a mano (stray)
			}
			r.al.scaled[r.size] = ci
			c.loaded[r.coverJob] = ci
			c.evict()
		default:
			return
		}
	}
}

// evict libera las portadas que llevan más tiempo sin verse hasta quedar en
// maxCovers.
func (c *covers) evict() {
	for len(c.loaded) > maxCovers {
		var oldest coverJob
		first := true
		for j, ci := range c.loaded {
			if first || ci.lastUsed < c.loaded[oldest].lastUsed {
				oldest, first = j, false
			}
		}
		c.loaded[oldest].img.Deallocate()
		delete(oldest.al.scaled, oldest.size)
		delete(c.loaded, oldest)
	}
}

// cover devuelve la portada de al a size×size y su opacidad (para el
// fundido), o nil mientras se prepara o si no tiene.
func (a *App) cover(al *Album, size int) (*ebiten.Image, float32) {
	if ci, ok := al.scaled[size]; ok {
		ci.lastUsed = a.t
		return ci.img, float32(min(1, (a.t-ci.ready)/coverFade))
	}
	if al.HasCover() && !al.missing {
		a.covers.request(al, size)
	}
	return nil, 0
}
