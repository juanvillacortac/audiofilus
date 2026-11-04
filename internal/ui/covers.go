package ui

import (
	"image"
	"sync"
	"time"

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
	content  image.Rectangle // la parte con imagen (en los logos, el resto es transparente)
	ready    float64         // instante (App.t) en que llegó, para el fundido
	lastUsed float64         // último instante en que se dibujó
}

type coverJob struct {
	al   *Album
	size int
}

type coverResult struct {
	coverJob
	rgba    *image.RGBA // nil si el álbum no tiene portada
	content image.Rectangle
	skipped bool // ya no la pedía nadie (Album.passing): no se preparó
}

// coverStale es desde cuándo una imagen que se pide mientras está a la vista
// (Album.passing) ya no se prepara si nadie la volvió a pedir: pasando
// rápido por una lista, la cola se llenaba de imágenes que ya no se veían, y
// prepararlas todas le quitaba la CPU a la interfaz durante segundos.
const coverStale = 250 * time.Millisecond

// covers prepara las portadas en segundo plano: leer la imagen del MP3,
// decodificarla, reducirla y tramarla tarda varios milisegundos, y hacerlo en
// el hilo que dibuja daba un tirón al entrar en una pantalla con muchas. El hilo
// de la interfaz solo sube a la GPU las que ya están listas (Update).
type covers struct {
	jobs    chan coverJob
	results chan coverResult
	pending map[coverJob]bool
	loaded  map[coverJob]*coverImage // las que están en la GPU, para el límite
	// asked es cuándo se pidió por última vez cada imagen de las que se
	// piden mientras están a la vista (Album.passing); lo miran los hilos.
	askMu sync.Mutex
	asked map[coverJob]time.Time
}

func newCovers(workers int) *covers {
	c := &covers{
		jobs:    make(chan coverJob, 512),
		results: make(chan coverResult, 512),
		pending: map[coverJob]bool{},
		loaded:  map[coverJob]*coverImage{},
		asked:   map[coverJob]time.Time{},
	}
	for range workers {
		go func() {
			for j := range c.jobs {
				if c.stale(j) {
					c.results <- coverResult{coverJob: j, skipped: true}
					continue
				}
				var rgba *image.RGBA
				if src := j.al.decodeCover(); src != nil && j.al.logo {
					rgba = scaleLogoBg(src, j.size, !j.al.clear)
				} else if src != nil {
					rgba = scaleCover(src, j.size)
				}
				var content image.Rectangle
				if rgba != nil {
					content = opaqueBounds(rgba)
				}
				c.results <- coverResult{coverJob: j, rgba: rgba, content: content}
			}
		}()
	}
	return c
}

// request pide una portada si no está ya pedida. Si la cola está llena, se
// vuelve a pedir en el siguiente frame.
func (c *covers) request(al *Album, size int) {
	j := coverJob{al, size}
	if al.passing {
		c.askMu.Lock()
		c.asked[j] = time.Now()
		c.askMu.Unlock()
	}
	if c.pending[j] {
		return
	}
	select {
	case c.jobs <- j:
		c.pending[j] = true
	default:
	}
}

// stale dice si la imagen j ya no hace falta: se pide mientras está a la
// vista y hace más de coverStale que nadie la pide.
func (c *covers) stale(j coverJob) bool {
	if !j.al.passing {
		return false
	}
	c.askMu.Lock()
	at, ok := c.asked[j]
	c.askMu.Unlock()
	return ok && time.Since(at) > coverStale
}

// collect sube a la GPU las portadas que llegaron. Va en Update, en el hilo de
// la interfaz.
func (c *covers) collect(now float64) {
	for {
		select {
		case r := <-c.results:
			delete(c.pending, r.coverJob)
			if r.al.passing {
				c.askMu.Lock()
				delete(c.asked, r.coverJob)
				c.askMu.Unlock()
			}
			if r.skipped {
				continue // si vuelve a verse, se vuelve a pedir
			}
			if r.rgba == nil {
				r.al.missing = true
				continue
			}
			ci := &coverImage{img: ebiten.NewImageFromImage(r.rgba), content: r.content, ready: now, lastUsed: now}
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

// coverContent es como cover, con la parte de la imagen que no es
// transparente (las cajas de los juegos vienen centradas en un cuadrado).
func (a *App) coverContent(al *Album, size int) (*ebiten.Image, float32, image.Rectangle) {
	img, alpha := a.cover(al, size)
	if img == nil {
		return nil, 0, image.Rectangle{}
	}
	return img, alpha, al.scaled[size].content
}

// opaqueBounds es el rectángulo de los píxeles no transparentes de img (o
// todo, si lo es entero).
func opaqueBounds(img *image.RGBA) image.Rectangle {
	b := img.Bounds()
	r := image.Rectangle{Min: b.Max, Max: b.Min}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		row := img.Pix[(y-b.Min.Y)*img.Stride:]
		for x := b.Min.X; x < b.Max.X; x++ {
			if row[(x-b.Min.X)*4+3] > 0x80 {
				r.Min.X, r.Min.Y = min(r.Min.X, x), min(r.Min.Y, y)
				r.Max.X, r.Max.Y = max(r.Max.X, x+1), max(r.Max.Y, y+1)
			}
		}
	}
	if r.Empty() {
		return b
	}
	return r
}

// prefetchCovers es cuántas canciones de la cola se miran por delante.
const prefetchAhead = 3

// prefetchCovers pide, a size, las portadas de las próximas canciones de la
// cola que son de otro disco que cur: al pasar a la siguiente, la suya ya está
// lista y sale sin la de relleno (antes tardaba hasta un segundo). Se marcan
// como vistas, así que no las libera evict mientras siguen por delante.
func (a *App) prefetchCovers(cur *Album, size int) {
	q := a.queue()
	i := a.status.Index
	// La cola solo se pide en su pantalla: aquí, si falta o es de otra
	// canción (otro largo, o cambió lo que suena), se pide (como mucho una
	// vez por segundo); mientras llega, se mira la que hay.
	if (len(q) != a.status.Count || a.prefetchFor != a.playingSong.Path) && a.t-a.queueAt > 1 {
		a.queueAt = a.t
		a.prefetchFor = a.playingSong.Path
		a.send("queue")
	}
	if i < 0 || i >= len(q) {
		return
	}
	for _, p := range q[i+1 : min(len(q), i+1+prefetchAhead)] {
		if al, _ := a.albumOf(p); al != nil && al != cur {
			a.cover(al, size)
		}
	}
}
