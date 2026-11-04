package ui

import (
	"fmt"
	"image/color"
	"io"
	"log"
	"math"
	"os/exec"
	"strconv"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
)

// Los vídeos de los juegos (el <video> del gamelist) en la ficha: los
// decodifica ffmpeg (el de Knulli) a poca resolución, y se pintan pixelados
// como las carátulas. Su sonido lo saca el mismo ffmpeg por PulseAudio (o
// ALSA): mientras suena, la música se pausa (la ficha la vuelve a poner al
// salir). Sin ffmpeg no hay vídeo.

// El tamaño de los cuadros del vídeo en la ficha y cuántos por segundo. A
// pantalla completa va a la resolución de la pantalla (gmVideoFullSize), a
// gmVideoFullFPS.
const (
	gmVideoW, gmVideoH = 176, 132
	gmVideoFPS         = 20
	gmVideoFullFPS     = 30
	// gmVideoMaxW es el ancho mayor a pantalla completa (con HDMI): más no
	// se nota en los vídeos de los juegos y le cuesta a la CPU.
	gmVideoMaxW = 960
)

// gmVideo es el vídeo que se está viendo.
type gmVideo struct {
	path   string
	w, h   int // el tamaño de los cuadros
	cmd    *exec.Cmd
	frames chan []byte
	img    *ebiten.Image
	shown  bool // ya llegó algún cuadro
	failed bool // ffmpeg no está o no pudo
}

// play empieza (o sigue) el vídeo de path, en bucle, en cuadros de w×h a
// fps por segundo (si cambia el tamaño, vuelve a empezar).
func (v *gmVideo) play(path string, w, h, fps int) {
	if v.path == path && v.cmd != nil && v.w == w && v.h == h {
		return
	}
	v.stop()
	v.path, v.shown, v.failed = path, false, false
	if v.w != w || v.h != h {
		if v.img != nil {
			v.img.Deallocate()
		}
		v.img, v.w, v.h = nil, w, h
	}
	bin, err := exec.LookPath("ffmpeg")
	if err != nil {
		v.failed = true
		return
	}
	// Reducido con "area" (como las carátulas, sin brillos); agrandado (a
	// pantalla completa), con bicúbico.
	flags := "area"
	if w > gmVideoW {
		flags = "bicubic"
	}
	vf := fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease:flags=%s,format=rgba,pad=%d:%d:(ow-iw)/2:(oh-ih)/2:color=0x00000000", w, h, flags, w, h)
	args := []string{"-nostdin", "-loglevel", "error", "-stream_loop", "-1", "-re", "-i", path,
		"-map", "0:v:0", "-vf", vf, "-r", strconv.Itoa(fps), "-f", "rawvideo", "-pix_fmt", "rgba", "-"}
	if out := audioOut(bin); out != "" {
		// El sonido, si tiene, a la vez (con -re va al ritmo del vídeo).
		args = append(args, "-map", "0:a:0?", "-f", out, "default")
	}
	cmd := exec.Command(bin, args...)
	dieWithParent(cmd)
	out, err := cmd.StdoutPipe()
	if err != nil {
		v.failed = true
		return
	}
	if err := cmd.Start(); err != nil {
		log.Printf("game mode: video: %v", err)
		v.failed = true
		return
	}
	v.cmd = cmd
	frames := make(chan []byte, 1)
	v.frames = frames
	go func() {
		size := w * h * 4
		for {
			buf := make([]byte, size)
			if _, err := io.ReadFull(out, buf); err != nil {
				close(frames)
				return
			}
			select { // solo el último: si la interfaz va lenta, se saltan
			case <-frames:
			default:
			}
			frames <- buf
		}
	}()
	go func() { _ = cmd.Wait() }()
}

// update sube el último cuadro que llegó (en el hilo de la interfaz).
func (v *gmVideo) update() {
	if v.frames == nil {
		return
	}
	select {
	case f, ok := <-v.frames:
		if !ok {
			v.frames = nil
			if !v.shown {
				v.failed = true // no salió ni un cuadro: no se pudo leer
			}
			return
		}
		if v.img == nil {
			v.img = ebiten.NewImage(v.w, v.h)
		}
		v.img.WritePixels(f)
		v.shown = true
	default:
	}
}

// stop para el vídeo.
func (v *gmVideo) stop() {
	if v.cmd != nil && v.cmd.Process != nil {
		_ = v.cmd.Process.Kill()
	}
	v.cmd, v.frames, v.path = nil, nil, ""
}

// audioOutName es la salida de sonido de ffmpeg (pulse o alsa; "" sin
// ninguna), mirada una vez.
var audioOutName *string

// audioOut es por dónde saca ffmpeg el sonido de los vídeos: PulseAudio (lo
// que usa el servicio de música; en Knulli lo da PipeWire) o ALSA.
func audioOut(bin string) string {
	if audioOutName != nil {
		return *audioOutName
	}
	name := ""
	if out, err := exec.Command(bin, "-hide_banner", "-devices").Output(); err == nil {
		for _, l := range strings.Split(string(out), "\n") {
			f := strings.Fields(l)
			if len(f) >= 2 && strings.Contains(f[0], "E") {
				if f[1] == "pulse" {
					name = "pulse"
					break
				}
				if f[1] == "alsa" {
					name = "alsa"
				}
			}
		}
	}
	audioOutName = &name
	return name
}

// gmVideoFullSize es el tamaño de los cuadros a pantalla completa: el de la
// pantalla de verdad (no el lienzo lógico, que es la mitad), hasta
// gmVideoMaxW de ancho; pares, como le gustan a ffmpeg.
func (a *App) gmVideoFullSize() (int, int) {
	w, h := a.finalW, a.finalH
	if w <= 0 || h <= 0 {
		w, h = a.w*2, a.h*2
	}
	if w > gmVideoMaxW {
		h, w = h*gmVideoMaxW/w, gmVideoMaxW
	}
	return w &^ 1, h &^ 1
}

// gmVideoNative dice si el vídeo a pantalla completa se pinta en la pantalla
// de verdad (DrawFinalScreen), a su resolución: ya llegó, no hay un empuje
// en marcha y la interfaz no está girada. Entonces el lienzo deja su sitio
// transparente (encima van el volumen y los avisos).
func (a *App) gmVideoNative() bool {
	g := &a.gm
	v := &g.info.video
	return g.on && a.mode == modeMain && !a.sleeping && g.screen == gmVideoFull && g.trans.at == 0 &&
		a.input.rot == 0 && v.shown && !v.failed && v.img != nil
}

// drawNativeVideo pinta el vídeo a pantalla completa en screen, lo más
// grande que quepa y sobre negro.
func (a *App) drawNativeVideo(screen *ebiten.Image) {
	img := a.gm.info.video.img
	sw, sh := float64(screen.Bounds().Dx()), float64(screen.Bounds().Dy())
	iw, ih := float64(img.Bounds().Dx()), float64(img.Bounds().Dy())
	sc := min(sw/iw, sh/ih)
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Scale(sc, sc)
	op.GeoM.Translate(math.Floor((sw-iw*sc)/2), math.Floor((sh-ih*sc)/2))
	op.Filter = ebiten.FilterLinear
	screen.Fill(color.Black)
	screen.DrawImage(img, op)
}
