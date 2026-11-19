package daemon

import (
	"log"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/control"
	"github.com/juanvillacortac/audiofilus/internal/platform"
)

// La salida de audio: en automático (Ajustes > Salida de audio, lo de
// siempre) la música pasa sola a un DAC USB al conectarlo y vuelve a la de
// antes al quitarlo, en pausa, como al desenchufar los audífonos. Elegir una
// salida la fija: se usa siempre que esté conectada. Con PipeWire/PulseAudio
// el cambio es al momento; con ALSA directo es reiniciar el servicio con la
// otra tarjeta (sigue sonando si sonaba). Ver platform.Outputs.

// softVolEvery es cada cuánto se lee el volumen del sistema mientras suena
// por un DAC con ALSA directo: los botones de la consola mueven el control
// de la tarjeta interna, y ese valor se aplica por software.
const softVolEvery = time.Second

// startOutputs abre la elección de salida y la vigila. alsaOut es la tarjeta
// que se eligió al arrancar (PickALSAOutput), "" si no es ALSA.
func (d *Daemon) startOutputs(alsaOut string) {
	d.outs = platform.OpenOutputs()
	if d.outs == nil {
		return
	}
	d.outStop = make(chan struct{})
	if alsaOut != "" && alsaOut != platform.OutputBuiltin {
		log.Printf("audio output: %s (software volume)", alsaOut)
		d.softVol = true
		d.applySoftVolume(d.plat.SystemVolume())
		go d.watchSoftVolume(d.outStop)
	}
	d.outKnown, d.outCur = known(d.outs.List()), d.outs.Current()
	if !d.outs.Restart() {
		// Al arrancar con el DAC ya conectado (Knulli pone la de la
		// consola al encender), se pasa a él como si se acabara de conectar.
		d.applyOutputs(nil, "")
	}
	go d.watchOutputs(d.outStop)
}

// stopOutputs suelta la conexión al salir.
func (d *Daemon) stopOutputs() {
	if d.outs == nil {
		return
	}
	close(d.outStop)
	d.outs.Close()
}

// known es el conjunto de IDs de una lista de salidas.
func known(list []platform.Output) map[string]bool {
	m := make(map[string]bool, len(list))
	for _, o := range list {
		m[o.ID] = true
	}
	return m
}

// watchOutputs aplica la elección cada vez que cambian las salidas.
func (d *Daemon) watchOutputs(stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		case <-d.outs.Changed():
		}
		d.mu.Lock()
		before, was := d.outKnown, d.outCur
		d.mu.Unlock()
		d.applyOutputs(before, was)
		control.Notify()
	}
}

// applyOutputs decide y hace el cambio de salida. before son las salidas que
// había antes (nil al arrancar o al cambiar la elección: todo cuenta como
// recién conectado) y was la que sonaba entonces.
func (d *Daemon) applyOutputs(before map[string]bool, was string) {
	list, cur := d.outs.List(), d.outs.Current()
	d.mu.Lock()
	pref, prev := d.cfg.AudioOutput, d.outPrev
	d.outKnown, d.outCur = known(list), cur
	d.mu.Unlock()
	if d.outs.Restart() {
		d.applyALSAOutput(pref, list, cur)
		return
	}
	// Se desconectó por donde sonaba: el servidor ya mandó la música a
	// otra (cur), quizá el altavoz.
	gone := before != nil && was != "" && !known(list)[was]
	if gone && d.eng.Playing() {
		log.Printf("audio output %q gone: pause", was)
		d.eng.PauseNow()
	}
	target := outputTarget(pref, list, cur, prev, before, gone)
	if target == "" || target == cur {
		return
	}
	log.Printf("audio output: %q -> %q", cur, target)
	if err := d.outs.Set(target); err != nil {
		log.Printf("audio output: %v", err)
		return
	}
	d.mu.Lock()
	d.outCur = target
	switch {
	case isUSB(list, target) && !isUSB(list, cur) && !gone:
		d.outPrev = cur // para volver a ella al quitar el DAC
	case !isUSB(list, target):
		d.outPrev = ""
	}
	d.mu.Unlock()
}

// applyALSAOutput reinicia el servicio con otra tarjeta si la que debe sonar
// (la elegida si está, si no un DAC USB en automático, si no la de la
// consola) no es la abierta. Si se fue la tarjeta abierta, el nuevo arranca
// en pausa.
func (d *Daemon) applyALSAOutput(pref string, list []platform.Output, cur string) {
	want := platform.OutputBuiltin
	for _, o := range list {
		if o.ID == pref || (pref == "" && o.USB && want == platform.OutputBuiltin) {
			want = o.ID
		}
	}
	if want == cur {
		return
	}
	gone := !known(list)[cur]
	log.Printf("audio output: %q -> %q (restart)", cur, want)
	if gone {
		d.eng.PauseNow()
	}
	_ = d.outs.Set(want)
	d.mu.Lock()
	d.restarting, d.restartPaused = true, gone
	d.mu.Unlock()
	d.quitOnce.Do(func() { close(d.quit) })
}

// outputTarget es adónde cambiar con PipeWire/PulseAudio ("" para nada):
//
//   - con una salida elegida (pref), a ella si está;
//   - en automático, a un DAC USB recién conectado (o ya conectado al
//     arrancar: before nil), salvo que ya suene por uno;
//   - y si se fue la que sonaba (gone), de vuelta a la de antes del DAC
//     (prev), no a la que eligió el servidor.
func outputTarget(pref string, list []platform.Output, cur, prev string, before map[string]bool, gone bool) string {
	has := known(list)
	if pref != "" {
		if has[pref] {
			return pref
		}
		return ""
	}
	if has[cur] && isUSB(list, cur) {
		return ""
	}
	for _, o := range list {
		if o.USB && !before[o.ID] {
			return o.ID
		}
	}
	if gone && has[prev] {
		return prev
	}
	return ""
}

// isUSB dice si la salida id de list es un DAC USB.
func isUSB(list []platform.Output, id string) bool {
	for _, o := range list {
		if o.ID == id {
			return o.USB
		}
	}
	return false
}

// chooseOutput es la elección de Ajustes (id, o "auto"). La interfaz ya la
// guardó en audiofilus.json; aquí se aplica.
func (d *Daemon) chooseOutput(id string) {
	if id == "auto" {
		id = ""
	}
	d.mu.Lock()
	d.cfg.AudioOutput = id
	d.mu.Unlock()
	if d.outs == nil {
		return
	}
	if d.outs.Restart() {
		// Como el resto de cambios: si la elegida no está, sigue la de ahora.
		d.applyALSAOutput(id, d.outs.List(), d.outs.Current())
		return
	}
	d.applyOutputs(nil, "")
}

// outputsStatus es lo que ve la interfaz: las salidas, la que suena y la
// elegida.
func (d *Daemon) outputsStatus() ([]platform.Output, string, string) {
	if d.outs == nil {
		return nil, "", ""
	}
	d.mu.Lock()
	pref := d.cfg.AudioOutput
	d.mu.Unlock()
	return d.outs.List(), d.outs.Current(), pref
}

// watchSoftVolume aplica el volumen del sistema por software mientras suena
// por un DAC con ALSA directo: lo lee cada softVolEvery mientras suena (en
// pausa, nada).
func (d *Daemon) watchSoftVolume(stop <-chan struct{}) {
	t := time.NewTicker(softVolEvery)
	defer t.Stop()
	last := -2
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		if !d.eng.Playing() {
			continue
		}
		d.mu.Lock()
		recent := time.Since(d.sysSetAt) < 2*time.Second
		d.mu.Unlock()
		if recent {
			continue // lo puso volumeStep, que ya lo aplicó
		}
		if v := d.plat.SystemVolume(); v != last {
			last = v
			d.applySoftVolume(v)
		}
	}
}

// applySoftVolume pone el volumen del sistema v (0-100, o -1 si no se sabe)
// como volumen de salida del engine, si suena por un DAC con ALSA directo.
func (d *Daemon) applySoftVolume(v int) {
	if !d.softVol || v < 0 {
		return
	}
	d.eng.SetMaster(float64(v) / 100)
}
