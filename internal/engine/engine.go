// Package engine reproduce la cola con oto: un único player, que vive todo el
// rato, lee del engine, y el engine decodifica, remuestrea y avanza por la cola.
package engine

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"log"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ebitengine/oto/v3"

	"github.com/juanvillacortac/audiofilus/internal/decode"
	"github.com/juanvillacortac/audiofilus/internal/library"
	"github.com/juanvillacortac/audiofilus/internal/queue"
	"github.com/juanvillacortac/audiofilus/internal/resample"
)

const bytesPerFrame = 8 // float32 estéreo

// restartThreshold: "anterior" reinicia la canción si ya sonó más que esto
// (igual que Songo).
const restartThreshold = 3 * time.Second

// Status es una foto del estado del reproductor.
type Status struct {
	State    string        `json:"state"` // playing, paused o stopped
	Track    library.Track `json:"track"`
	Position float64       `json:"position"` // segundos
	Duration float64       `json:"duration"` // segundos; 0 si no se conoce
	Index    int           `json:"index"`
	Count    int           `json:"count"`
	Shuffle  bool          `json:"shuffle"`
	Repeat   string        `json:"repeat"`
	Volume   float64       `json:"volume"`
	// Loading: la canción es remota y aún se está bajando (suena silencio).
	Loading bool `json:"loading,omitempty"`
	// DSP es la configuración del sonido; Gain, la ganancia de ReplayGain
	// que lleva la canción (dB; 0 sin etiquetas o apagado).
	DSP  DSP     `json:"dsp"`
	Gain float64 `json:"gain_db,omitempty"`
	// StopAfter: se parará al acabar esta canción.
	StopAfter bool `json:"stop_after,omitempty"`
}

// Engine es seguro para uso concurrente. El hilo de audio de oto llama a Read;
// los comandos llegan desde el socket de control.
type Engine struct {
	ctx    *oto.Context
	player *oto.Player
	rate   int

	mu      sync.Mutex
	q       *queue.Queue
	src     decode.Source
	rs      *resample.Reader
	track   library.Track
	playing bool
	ended   bool // la cola se acabó; el player necesita un Seek para volver a arrancar
	pending func()
	fbuf    []float32
	boost   float32 // el refuerzo por encima del 100 % (1 = ninguno), con limitador
	limit   limiter
	// El sonido (dsp.go): la configuración, el ecualizador, la ganancia de
	// ReplayGain de lo que suena y sus etiquetas (para recalcularla).
	dsp      DSP
	eq       eqFilter
	gain     float32
	gainTags library.Gain
	gainOK   bool
	// El fundido entre canciones (crossfade.go): tail es la que se va
	// apagando mientras empieza la siguiente; xfLen y xfPos, cuánto dura el
	// fundido y cuánto lleva (en cuadros de salida).
	tail   *voice
	xfLen  int
	xfPos  int
	tbuf   []float32
	xfSkip string // la siguiente que se vio que no se funde (mismo disco)
	// stopAfter: al acabar la canción que suena, parar (el temporizador
	// "al terminar esta canción").
	stopAfter bool
	meter     meter // nivel y graves de lo que suena (meter.go)
	// waiting es la canción remota que se está esperando (sin src); waitAt,
	// cuándo se preguntó por última vez si ya llegó.
	waiting string
	waitAt  time.Time
	// El scratch (scratch.go): hist guarda lo último que se generó (histN
	// cuadros en total; vale desde histFrom) y scr es el scratch en curso.
	hist     []float32
	histN    int64
	histFrom int64
	scr      *scratch

	// OpenRemote abre una pista remota ("subsonic://...", "radio://..."):
	// ready si ya puede sonar; si no, empieza a prepararla (bajarla,
	// conectar) y vuelve enseguida (se llama desde el hilo del audio, no
	// puede esperar). Prefetch empieza a bajar una que vendrá. Los pone el
	// servicio (internal/remote, internal/radio).
	OpenRemote func(path string) (src decode.Source, ready bool, err error)
	Prefetch   func(path string)

	// El fundido de pausar y seguir va sobre el volumen del player, en
	// tiempo real (no sobre el audio que se genera, que suena hasta 250 ms
	// después): vol es el volumen elegido (0..1) y fadeGen cuenta los
	// fundidos, para que uno nuevo cancele al anterior.
	vol     float64
	fadeGen int
	latency time.Duration // lo que tarda en oírse lo que se manda (el búfer)

	// OnPlayingChanged se llama (fuera del lock) al empezar o parar la reproducción.
	OnPlayingChanged func(playing bool)
	// OnTrackChanged se llama (fuera del lock) al empezar una canción.
	OnTrackChanged func(library.Track)
	// OnStopAfter se llama cuando se paró al acabar la canción (StopAfter).
	OnStopAfter func()
	events      chan func()
	closed      bool
}

// New abre la salida de audio.
func New(sampleRate int, buffer time.Duration) (*Engine, error) {
	usePulseSocket()
	ctx, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate:      sampleRate,
		ChannelCount:    2,
		Format:          oto.FormatFloat32LE,
		BufferSize:      buffer,
		ApplicationName: "audiofilus",
	})
	if err != nil {
		return nil, err
	}
	<-ready
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e := &Engine{ctx: ctx, rate: sampleRate, q: queue.New(), events: make(chan func(), 16), boost: 1, vol: 1, gain: 1, latency: buffer, limit: newLimiter(sampleRate), meter: newMeter(sampleRate, 2*buffer)}
	e.dsp = DSP{}.Normalize()
	e.player = ctx.NewPlayer(e)
	e.player.SetBufferSize(int(buffer.Seconds() * float64(sampleRate) * bytesPerFrame))
	go e.dispatch()
	return e, nil
}

// usePulseSocket apunta los clientes de PulseAudio al socket del sistema cuando
// no hay uno por usuario, como en Knulli, donde todo corre como root. Sin esto,
// oto caería a ALSA y chocaría con PipeWire.
func usePulseSocket() {
	// En muOS el audio es PipeWire (por ALSA), y su socket está en /run.
	if _, err := os.Stat("/run/muos"); err == nil && os.Getenv("XDG_RUNTIME_DIR") == "" {
		os.Setenv("XDG_RUNTIME_DIR", "/run")
	}
	if os.Getenv("PULSE_SERVER") != "" {
		return
	}
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		if _, err := os.Stat(dir + "/pulse/native"); err == nil {
			return
		}
	}
	for _, p := range []string{"/var/run/pulse/native", "/run/pulse/native"} {
		if _, err := os.Stat(p); err == nil {
			os.Setenv("PULSE_SERVER", "unix:"+p)
			return
		}
	}
}

// dispatch ejecuta las retrollamadas en orden, fuera del lock y del hilo de audio.
func (e *Engine) dispatch() {
	for f := range e.events {
		f()
	}
}

// emit encola una retrollamada para que no corra con el lock tomado ni en el
// hilo de audio. Se llama con e.mu tomado.
func (e *Engine) emit(f func()) {
	if e.closed {
		return
	}
	select {
	case e.events <- f:
	default:
	}
}

// Read implementa io.Reader para el player de oto.
func (e *Engine) Read(p []byte) (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	frames := len(p) / bytesPerFrame
	if frames == 0 {
		return 0, nil
	}
	if e.waiting != "" && !e.ended {
		// Una canción remota que se está bajando: silencio hasta que llegue
		// (se pregunta cada 200 ms), y entonces suena desde el principio.
		if time.Since(e.waitAt) >= 200*time.Millisecond {
			e.waitAt = time.Now()
			src, ready, err := e.OpenRemote(e.waiting)
			switch {
			case err != nil:
				log.Printf("skipping %s: %v", e.waiting, err)
				e.waiting = ""
				if !e.advanceLocked(true) {
					e.ended = true
					e.setPlayingLocked(false)
				}
			case ready:
				e.setSourceLocked(e.waiting, "", src)
			}
		}
		if e.waiting != "" {
			clear(p[:frames*bytesPerFrame])
			return frames * bytesPerFrame, nil
		}
	}
	if e.ended || e.rs == nil {
		e.ended = true
		return 0, io.EOF
	}
	if cap(e.fbuf) < frames*2 {
		e.fbuf = make([]float32, frames*2)
	}
	buf := e.fbuf[:frames*2]
	n := 0
	if e.scr != nil {
		n = e.scratchRead(buf)
	}
	for n < len(buf) {
		// A trozos de como mucho xfChunk, para empezar el fundido a tiempo.
		end := len(buf)
		if e.dsp.Crossfade > 0 {
			e.maybeCrossfadeLocked()
			end = min(end, n+xfChunk*2)
		}
		got, err := e.rs.Read(buf[n:end])
		gainBuf(buf[n:n+got], e.gain)
		e.mixTailLocked(buf[n : n+got])
		n += got
		e.histWrite(buf[n-got : n])
		if err == nil {
			continue
		}
		if !errors.Is(err, io.EOF) {
			log.Printf("decode error in %s: %v", e.track.Path, err)
		}
		// Terminó la canción: se pasa a la siguiente sin silencio entre medias.
		if !e.advanceLocked(true) {
			e.ended = true
			e.setPlayingLocked(false)
			break
		}
	}
	e.eq.process(buf[:n])
	e.limit.process(buf[:n], e.boost, len(e.eq.bands) > 0 || e.gain > 1)
	e.meter.add(buf[:n], time.Now().Add(e.meter.latency), e.rate)
	for i := 0; i < n; i++ {
		binary.LittleEndian.PutUint32(p[i*4:], math.Float32bits(buf[i]))
	}
	if n == 0 {
		return 0, io.EOF
	}
	return n * 4, nil
}

// Seek implementa io.Seeker. oto lo llama desde Player.Seek cuando no hay ningún
// Read en curso; el engine lo aprovecha para cambiar de canción o saltar con el
// búfer de audio vaciado.
func (e *Engine) Seek(offset int64, whence int) (int64, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.pending != nil {
		e.pending()
		e.pending = nil
	}
	return 0, nil
}

// apply ejecuta op con el búfer de audio vaciado y luego sigue sonando si tocaba.
func (e *Engine) apply(op func()) {
	e.mu.Lock()
	e.pending = func() {
		e.histReset() // lo que sonó antes ya no vale para un scratch
		op()
	}
	e.mu.Unlock()
	if _, err := e.player.Seek(0, io.SeekStart); err != nil {
		log.Printf("player seek: %v", err)
	}
	e.mu.Lock()
	playing := e.playing
	e.mu.Unlock()
	if playing {
		_ = e.ctx.Resume()
		e.player.Play()
	}
}

// openLocked abre la canción actual de la cola, saltando los archivos que fallen.
func (e *Engine) openLocked() bool {
	for tries := 0; tries < e.q.Len(); tries++ {
		path := e.q.Current()
		if path == "" {
			return false
		}
		if e.OpenRemote != nil && strings.Contains(path, "://") {
			src, ready, err := e.OpenRemote(path)
			if err == nil && !ready {
				// Aún no está: se espera (en Read) con silencio.
				e.closeSourceLocked()
				e.waiting, e.waitAt, e.ended = path, time.Now(), false
				e.track = library.ReadTrack(path)
				e.announceLocked()
				return true
			}
			if err != nil {
				log.Printf("skipping %s: %v", path, err)
				if !e.q.Next(false) {
					return false
				}
				continue
			}
			e.setSourceLocked(path, "", src)
			return true
		}
		if e.openFileLocked(path, path) {
			return true
		}
		if !e.q.Next(false) {
			return false
		}
	}
	return false
}

// openFileLocked abre local, el archivo de la canción path (la misma, o el de
// la caché si es remota), y avisa del cambio de canción.
func (e *Engine) openFileLocked(path, local string) bool {
	src, err := decode.Open(local)
	if err != nil {
		log.Printf("skipping %v", err)
		e.waiting = ""
		return false
	}
	e.setSourceLocked(path, local, src)
	return true
}

// setSourceLocked pone src (ya abierta) como la canción path y avisa. local
// es su archivo, si lo hay (de él sale su ReplayGain).
func (e *Engine) setSourceLocked(path, local string, src decode.Source) {
	e.closeSourceLocked()
	e.src = src
	e.rs = resample.New(src, e.rate)
	e.ended, e.waiting = false, ""
	e.track = library.ReadTrack(path)
	e.gainTags, e.gainOK = library.Gain{}, false
	if f, _, _, ok := decode.SplitCuePath(local); ok {
		local = f // una pista de un CUE: la ganancia es la de su archivo
	}
	if local != "" {
		e.gainTags, e.gainOK = library.ReadGain(local)
	}
	e.gain = replayGain(e.dsp, e.gainTags, e.gainOK, e.q.Shuffled())
	e.announceLocked()
}

// announceLocked avisa del cambio de canción y empieza a bajar la siguiente
// si es remota.
func (e *Engine) announceLocked() {
	t := e.track
	next := e.q.Peek()
	e.emit(func() {
		if e.OnTrackChanged != nil {
			e.OnTrackChanged(t)
		}
		if e.Prefetch != nil && next != "" {
			e.Prefetch(next)
		}
	})
}

// closeSourceLocked cierra el archivo actual (y el que se estaba
// apagando en un fundido: un cambio a mano corta en seco).
func (e *Engine) closeSourceLocked() {
	if e.src != nil {
		e.src.Close()
	}
	e.src, e.rs = nil, nil
	e.stopCrossfadeLocked()
}

// advanceLocked pasa a la siguiente canción (ver queue.Next).
func (e *Engine) advanceLocked(auto bool) bool {
	if auto && e.stopAfter {
		e.stopAfter = false
		e.emit(func() {
			if e.OnStopAfter != nil {
				e.OnStopAfter()
			}
		})
		return false
	}
	if !e.q.Next(auto) {
		return false
	}
	if auto && e.q.Repeat == queue.RepeatOne && e.src != nil {
		e.seekLocked(0)
		return true
	}
	return e.openLocked()
}

// seekLocked salta a seconds en la canción actual.
func (e *Engine) seekLocked(seconds float64) {
	e.stopCrossfadeLocked()
	if e.src == nil {
		return
	}
	frame := int64(seconds * float64(e.src.SampleRate()))
	if err := e.src.SeekFrame(frame); err != nil {
		log.Printf("seek: %v", err)
	}
	e.rs.Reset()
	e.ended = false
	e.histReset()
}

// setPlayingLocked cambia el estado y avisa a OnPlayingChanged.
func (e *Engine) setPlayingLocked(on bool) {
	if e.playing == on {
		return
	}
	e.playing = on
	e.emit(func() {
		if e.OnPlayingChanged != nil {
			e.OnPlayingChanged(on)
		}
	})
}

// Load reemplaza la cola y se coloca en start, at segundos; suena si play.
func (e *Engine) Load(tracks []string, start int, at float64, play bool) {
	e.apply(func() {
		e.q.Set(tracks, start)
		e.closeSourceLocked()
		if !e.openLocked() {
			e.setPlayingLocked(false)
			return
		}
		if at > 0 {
			e.seekLocked(at)
		}
		e.setPlayingLocked(play)
	})
	if !play {
		e.player.Pause()
		return
	}
	// Si venía de una pausa, la salida está dormida, el player parado y su
	// volumen en 0 (el fundido de Pause): op espera en el hilo del audio, que
	// no corre, y sin esto quedaba "sonando" en silencio hasta pausar y
	// reanudar (pasaba al elegir un episodio o una radio con la música en
	// pausa). Despertarla, tocar y subir el volumen (fadeIn cancela además el
	// fundido de la pausa si aún no terminó).
	_ = e.ctx.Resume()
	e.player.Play()
	e.fadeIn()
}

// Append añade canciones al final de la cola.
func (e *Engine) Append(tracks []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.q.Append(tracks)
}

// PlayNext encola una canción justo después de la actual.
func (e *Engine) PlayNext(track string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.q.PlayNext(track)
}

// Play reanuda (o empieza) la reproducción.
func (e *Engine) Play() {
	e.mu.Lock()
	if e.src == nil && e.q.Current() != "" {
		e.mu.Unlock()
		e.apply(func() {
			if e.openLocked() {
				e.setPlayingLocked(true)
			}
		})
		e.fadeIn()
		return
	}
	if e.src == nil {
		e.mu.Unlock()
		return
	}
	ended := e.ended
	e.setPlayingLocked(true)
	e.mu.Unlock()
	_ = e.ctx.Resume()
	// Vuelve a subir: desde donde iba si se estaba apagando, y si no desde 0.
	defer e.fadeIn()
	if ended {
		e.apply(func() { e.seekLocked(0) })
		return
	}
	e.player.Play()
}

// Tiempos del fundido.
const (
	fadeOutSec = 0.3
	fadeInSec  = 0.15
)

// Pause baja el volumen en fadeOutSec y, cuando ya se oyó todo lo que había
// en el búfer (latency), para la salida y la deja dormir. El audio pendiente
// se conserva para reanudar sin cortes.
func (e *Engine) Pause() {
	e.mu.Lock()
	if !e.playing {
		e.mu.Unlock()
		return
	}
	e.setPlayingLocked(false)
	e.fadeGen++
	gen := e.fadeGen
	wait := e.latency + 60*time.Millisecond
	e.mu.Unlock()
	go func() {
		if !e.rampVolume(gen, e.player.Volume(), 0, fadeOutSec) {
			return
		}
		time.Sleep(wait)
		e.mu.Lock()
		still := gen == e.fadeGen && !e.playing
		e.mu.Unlock()
		if still {
			e.player.Pause()
			// En pausa se deja dormir el dispositivo de audio.
			_ = e.ctx.Suspend()
		}
	}()
}

// rampVolume lleva el volumen del player de from a to en sec segundos, por
// pasos. Devuelve false si otro fundido lo canceló.
func (e *Engine) rampVolume(gen int, from, to, sec float64) bool {
	const steps = 15
	for k := 1; k <= steps; k++ {
		time.Sleep(time.Duration(sec / steps * float64(time.Second)))
		e.mu.Lock()
		ok := gen == e.fadeGen
		e.mu.Unlock()
		if !ok {
			return false
		}
		e.player.SetVolume(from + (to-from)*float64(k)/steps)
	}
	return true
}

// fadeIn sube el volumen al elegido en fadeInSec, desde el que haya (0 si
// estaba en pausa del todo, o a medias si se estaba apagando).
func (e *Engine) fadeIn() {
	e.mu.Lock()
	e.fadeGen++
	gen, target := e.fadeGen, e.vol
	e.mu.Unlock()
	go e.rampVolume(gen, e.player.Volume(), target, fadeInSec)
}

// Toggle alterna entre sonar y pausa.
func (e *Engine) Toggle() {
	if e.Playing() {
		e.Pause()
	} else {
		e.Play()
	}
}

// Playing dice si está sonando música.
func (e *Engine) Playing() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.playing
}

// Next salta a la siguiente canción.
func (e *Engine) Next() {
	e.apply(func() {
		if !e.advanceLocked(false) {
			e.ended = true
			e.setPlayingLocked(false)
		}
	})
}

// Prev reinicia la canción, o va a la anterior si acaba de empezar.
func (e *Engine) Prev() {
	pos := e.Status().Position
	e.apply(func() {
		if time.Duration(pos*float64(time.Second)) > restartThreshold || !e.q.Prev() {
			e.seekLocked(0)
			return
		}
		e.openLocked()
	})
}

// Jump reproduce la posición i de la cola.
func (e *Engine) Jump(i int) {
	e.apply(func() {
		if e.q.Jump(i) && e.openLocked() {
			e.setPlayingLocked(true)
		}
	})
}

// SeekTo salta a seconds en la canción actual.
func (e *Engine) SeekTo(seconds float64) {
	e.apply(func() { e.seekLocked(math.Max(seconds, 0)) })
}

// SeekBy avanza (o retrocede) delta segundos.
func (e *Engine) SeekBy(delta float64) {
	e.SeekTo(e.Status().Position + delta)
}

// SetShuffle activa o desactiva el aleatorio.
func (e *Engine) SetShuffle(on bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.q.SetShuffle(on)
	// En "auto", ReplayGain va por canción en aleatorio y por disco en orden.
	e.gain = replayGain(e.dsp, e.gainTags, e.gainOK, on)
}

// SetRepeat cambia el modo de repetición.
func (e *Engine) SetRepeat(r queue.Repeat) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.q.Repeat = r
}

// SetVolume cambia el volumen de salida (1 = sin cambios, hasta 2). Hasta 1 lo
// aplica oto; por encima, Read amplifica con el limitador (se nota tras el
// búfer de audio, ~250 ms).
func (e *Engine) SetVolume(v float64) {
	v = math.Max(0, math.Min(v, 2))
	e.mu.Lock()
	e.vol = math.Min(v, 1)
	e.boost = float32(math.Max(v, 1))
	e.fadeGen++ // un cambio de volumen manda sobre un fundido a medias
	playing := e.playing
	e.mu.Unlock()
	if playing {
		e.player.SetVolume(math.Min(v, 1))
	}
}

// SetDSP cambia la configuración del sonido (se oye tras el búfer).
func (e *Engine) SetDSP(d DSP) {
	d = d.Normalize()
	e.mu.Lock()
	defer e.mu.Unlock()
	e.dsp = d
	e.eq.set(d.EQ, e.rate)
	e.gain = replayGain(d, e.gainTags, e.gainOK, e.q.Shuffled())
	if d.Crossfade == 0 {
		e.stopCrossfadeLocked()
	}
}

// SetStopAfter pide (o deja de pedir) parar al acabar la canción que suena.
func (e *Engine) SetStopAfter(on bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stopAfter = on
}

// DSPConfig es la configuración del sonido.
func (e *Engine) DSPConfig() DSP {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.dsp
}

// Queue devuelve las canciones en orden de reproducción.
func (e *Engine) Queue() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.q.Tracks()
}

// Status devuelve el estado. Position descuenta el audio que aún está en el búfer.
func (e *Engine) Status() Status {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := Status{
		Index:     e.q.Index(),
		Count:     e.q.Len(),
		Shuffle:   e.q.Shuffled(),
		Repeat:    e.q.Repeat.String(),
		Volume:    e.vol * float64(e.boost),
		Track:     e.track,
		DSP:       e.dsp,
		StopAfter: e.stopAfter,
	}
	if e.gain != 1 {
		s.Gain = math.Round(20*math.Log10(float64(e.gain))*10) / 10
	}
	s.Loading = e.waiting != ""
	if b, ok := e.src.(interface{ Buffering() bool }); ok && b.Buffering() {
		s.Loading = true // una radio sin datos: suena silencio
	}
	if strings.Contains(e.track.Path, "://") {
		// Lo remoto puede cambiar de título mientras suena (la radio).
		e.track = library.ReadTrack(e.track.Path)
		s.Track = e.track
	}
	switch {
	case e.playing:
		s.State = "playing"
	case (e.src != nil || e.waiting != "") && !e.ended:
		s.State = "paused"
	default:
		s.State = "stopped"
	}
	if e.src != nil {
		rate := float64(e.src.SampleRate())
		buffered := float64(e.player.BufferedSize()) / bytesPerFrame / float64(e.rate)
		s.Position = math.Max(float64(e.src.Position())/rate-buffered, 0)
		if l := e.src.Length(); l > 0 {
			s.Duration = float64(l) / rate
		}
	}
	return s
}

// Close para y libera el archivo actual.
func (e *Engine) Close() {
	e.player.Pause()
	e.mu.Lock()
	e.closeSourceLocked()
	e.closed = true
	e.mu.Unlock()
	close(e.events)
}

// Context es la salida de audio. En el móvil, servicio e interfaz van en el
// mismo proceso y oto admite un solo contexto: la interfaz suena por este
// (ver mobile y ui.SharedAudio).
func (e *Engine) Context() *oto.Context { return e.ctx }

// Idle duerme la salida si no suena música: lo llama la interfaz del móvil
// tras sus sonidos (con su propio contexto, lo dormía ella).
func (e *Engine) Idle() {
	e.mu.Lock()
	playing := e.playing
	e.mu.Unlock()
	if !playing {
		_ = e.ctx.Suspend()
	}
}

// PlaySFX suena un efecto (bytes float32 estéreo, ver internal/sfx) por
// encima de la música. Si la salida estaba parada (en pausa), se enciende
// para el efecto y se vuelve a parar al terminar si sigue en pausa.
func (e *Engine) PlaySFX(b []byte, d time.Duration) {
	if len(b) == 0 {
		return
	}
	_ = e.ctx.Resume()
	p := e.ctx.NewPlayer(bytes.NewReader(b))
	p.Play()
	time.AfterFunc(d+500*time.Millisecond, func() {
		_ = p.Close()
		e.mu.Lock()
		playing := e.playing
		e.mu.Unlock()
		if !playing {
			_ = e.ctx.Suspend()
		}
	})
}
