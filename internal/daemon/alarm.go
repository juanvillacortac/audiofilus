package daemon

import (
	"encoding/json"
	"errors"
	"log"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/library"
	"github.com/juanvillacortac/audiofilus/internal/platform"
)

// La alarma: a una hora (todos los días, algunos o una vez) empieza a sonar
// lo que se eligió (lo último que sonaba, una lista, una carpeta o una
// emisora), con el volumen subiendo poco a poco. Vive en el servicio, así que
// suena con la interfaz cerrada; en las portátiles además se programa el reloj
// de la consola (RTC) para que la despierte si está suspendida. Se guarda en
// data/alarm.json.

// Alarm es la alarma configurada; Next, cuándo sonará (lo calcula el servicio).
type Alarm struct {
	On   bool `json:"on"`
	Hour int  `json:"hour"`
	Min  int  `json:"min"`
	// Days son los días (0 domingo ... 6 sábado); vacío, todos. Once: solo
	// la próxima vez (luego se apaga).
	Days []int `json:"days,omitempty"`
	Once bool  `json:"once,omitempty"`
	// Source es qué suena: "" lo último que sonaba; si no, una lista (.m3u),
	// una carpeta, un archivo o una emisora ("radio://...").
	Source string `json:"source,omitempty"`
	// Fade son los segundos en que el volumen llega a su nivel (0: 60).
	Fade int `json:"fade,omitempty"`

	Next time.Time `json:"next,omitzero"`
}

// alarmEvery es cada cuánto se mira la hora: con el reloj de verdad (no un
// temporizador, que no cuenta mientras la consola está suspendida).
const alarmEvery = 5 * time.Second

// next es la próxima vez que suena después de now.
func (a Alarm) next(now time.Time) time.Time {
	if !a.On {
		return time.Time{}
	}
	t := time.Date(now.Year(), now.Month(), now.Day(), a.Hour, a.Min, 0, 0, now.Location())
	for i := 0; i < 8; i++ {
		if t.After(now) && (len(a.Days) == 0 || a.Once || slices.Contains(a.Days, int(t.Weekday()))) {
			return t
		}
		t = t.AddDate(0, 0, 1)
	}
	return time.Time{}
}

func (d *Daemon) alarmFile() string { return filepath.Join(d.paths.Data, "alarm.json") }

// loadAlarm lee la alarma guardada.
func (d *Daemon) loadAlarm() {
	var a Alarm
	if b, err := os.ReadFile(d.alarmFile()); err == nil {
		_ = json.Unmarshal(b, &a)
	}
	d.setAlarm(a, false)
}

// setAlarm la cambia (y la guarda si save): calcula la próxima vez y
// programa el despertador de la consola.
func (d *Daemon) setAlarm(a Alarm, save bool) {
	a.Hour, a.Min = max(0, min(23, a.Hour)), max(0, min(59, a.Min))
	a.Next = a.next(time.Now())
	d.mu.Lock()
	d.alarm = a
	d.mu.Unlock()
	if save {
		if b, err := json.Marshal(a); err == nil {
			_ = os.MkdirAll(d.paths.Data, 0o755)
			_ = os.WriteFile(d.alarmFile(), b, 0o644)
		}
	}
	// El despertador de la consola: un poco antes, para que dé tiempo a
	// arrancar el sonido. En el PC no se toca.
	if d.plat.Name() != "desktop" {
		wake := time.Time{}
		if !a.Next.IsZero() {
			wake = a.Next.Add(-30 * time.Second)
		}
		if err := platform.SetWakeAlarm(wake); err != nil {
			log.Printf("alarm: wake alarm: %v", err)
		}
	}
	if !a.Next.IsZero() {
		log.Printf("alarm: next at %s", a.Next.Format(time.RFC3339))
	}
}

// watchAlarm mira la hora y hace sonar la alarma cuando toca.
func (d *Daemon) watchAlarm(stop <-chan struct{}) {
	tick := time.NewTicker(alarmEvery)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
		}
		d.mu.Lock()
		a := d.alarm
		d.mu.Unlock()
		if a.Next.IsZero() || time.Now().Before(a.Next) {
			continue
		}
		// Si la consola se despertó tarde (estaba apagada), no suena horas
		// después: solo dentro de los diez minutos siguientes.
		if time.Since(a.Next) < 10*time.Minute {
			d.ringAlarm(a)
		}
		if a.Once {
			a.On = false
		}
		d.setAlarm(a, a.Once)
	}
}

// ringAlarm pone a sonar lo de la alarma con el volumen subiendo.
func (d *Daemon) ringAlarm(a Alarm) {
	e := d.eng
	target := e.Status().Volume
	if target < 0.2 {
		target = 0.5 // si estaba casi en silencio, no despertaría
	}
	e.SetVolume(0.02)
	switch src := strings.TrimSpace(a.Source); {
	case src == "":
		e.Play()
	case strings.Contains(src, "://"):
		e.Load([]string{src}, 0, 0, true)
	default:
		files, err := library.Expand([]string{src})
		if err != nil || len(files) == 0 {
			log.Printf("alarm: %s: %v (playing the last queue)", src, err)
			e.Play()
		} else {
			e.Load(files, 0, 0, true)
		}
	}
	log.Printf("alarm: ringing (%q)", a.Source)
	fade := a.Fade
	if fade <= 0 {
		fade = 60
	}
	go func() {
		set := 0.02 // lo último que puso la alarma
		for i := 1; i <= fade; i++ {
			time.Sleep(time.Second)
			st := e.Status()
			if st.State != "playing" {
				// La pausaron a medio subir: el volumen vuelve a lo que
				// era (si no, lo siguiente sonaría casi en silencio).
				e.SetVolume(target)
				return
			}
			if math.Abs(st.Volume-set) > 0.01 {
				return // lo movieron a mano: manda lo suyo
			}
			set = target * float64(i) / float64(fade)
			e.SetVolume(set)
		}
	}()
}

// alarmCommand atiende "alarm": sin argumentos devuelve la alarma; "set
// <json>" la cambia, "off" la apaga y "test" la hace sonar ya.
func (d *Daemon) alarmCommand(args []string) (any, error) {
	d.mu.Lock()
	cur := d.alarm
	d.mu.Unlock()
	if len(args) == 0 {
		return cur, nil
	}
	switch args[0] {
	case "set":
		if len(args) != 2 {
			return nil, errors.New(`usage: alarm set '{"on":true,"hour":7,"min":30}'`)
		}
		var a Alarm
		if err := json.Unmarshal([]byte(args[1]), &a); err != nil {
			return nil, err
		}
		d.setAlarm(a, true)
	case "off":
		cur.On = false
		d.setAlarm(cur, true)
	case "test":
		d.ringAlarm(cur)
	default:
		return nil, errors.New("usage: alarm [set <json>|off|test]")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.alarm, nil
}
