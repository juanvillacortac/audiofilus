package daemon

import (
	"errors"
	"strconv"

	"github.com/juanvillacortac/audiofilus/internal/engine"
)

// dspCommand aplica a d un comando del sonido:
//
//	replaygain off|track|album|auto
//	preamp <dB>
//	eq <preajuste>            (flat, bass, rock...; ver engine.EQPresets)
//	eq <10 ganancias en dB>   (de 31 Hz a 16 kHz; queda como "custom")
//	crossfade <segundos>      (0 lo apaga)
func dspCommand(d engine.DSP, cmd string, args []string) (engine.DSP, error) {
	num := func(s string) (float64, error) { return strconv.ParseFloat(s, 64) }
	switch cmd {
	case "replaygain":
		if len(args) != 1 {
			return d, errors.New("usage: replaygain off|track|album|auto")
		}
		switch args[0] {
		case "off", "track", "album", "auto":
			d.ReplayGain = args[0]
		default:
			return d, errors.New("usage: replaygain off|track|album|auto")
		}
	case "preamp":
		if len(args) != 1 {
			return d, errors.New("usage: preamp <dB>")
		}
		v, err := num(args[0])
		if err != nil {
			return d, errors.New("usage: preamp <dB>")
		}
		d.Preamp = v
	case "eq":
		switch {
		case len(args) == 1 && engine.PresetGains(args[0]) != nil:
			d.EQ, d.EQPreset = engine.PresetGains(args[0]), args[0]
		case len(args) == len(engine.EQBands):
			gains := make([]float64, len(args))
			for i, a := range args {
				v, err := num(a)
				if err != nil {
					return d, errors.New("usage: eq <preset> | eq <10 gains in dB>")
				}
				gains[i] = v
			}
			d.EQ, d.EQPreset = gains, "custom"
		default:
			return d, errors.New("usage: eq <preset> | eq <10 gains in dB>")
		}
	case "crossfade":
		if len(args) != 1 {
			return d, errors.New("usage: crossfade <seconds>")
		}
		v, err := num(args[0])
		if err != nil {
			return d, errors.New("usage: crossfade <seconds>")
		}
		d.Crossfade = v
	}
	return d.Normalize(), nil
}
