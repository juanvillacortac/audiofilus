package daemon

import (
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/juanvillacortac/audiofilus/internal/engine"
)

// dspCommand aplica a d un comando del sonido:
//
//	replaygain off|track|album|auto
//	preamp <dB>
//	eq <preajuste>            (flat, bass, rock...; ver engine.EQPresets)
//	eq <10 ganancias en dB>   (de 31 Hz a 16 kHz; queda como "custom")
//	crossfade <segundos>      (0 lo apaga)
//	eqmode graphic|parametric (qué ecualizador suena)
//	peq <JSON>                (las bandas del paramétrico: [{"type":"peak","freq":1000,"gain":3,"q":1}, ...])
//	peqpreamp <dB>            (lo que se baja o sube antes del paramétrico)
//	resampler auto|linear|quality (desde la canción siguiente)
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
	case "eqmode":
		if len(args) != 1 || (args[0] != "graphic" && args[0] != "parametric") {
			return d, errors.New("usage: eqmode graphic|parametric")
		}
		d.EQMode = args[0]
	case "peq":
		var bands []engine.PEQBand
		if len(args) != 1 || json.Unmarshal([]byte(args[0]), &bands) != nil {
			return d, errors.New(`usage: peq '[{"type":"peak","freq":1000,"gain":3,"q":1}]'`)
		}
		if len(bands) > engine.PEQMax {
			return d, errors.New("peq: at most 8 bands")
		}
		for _, b := range bands {
			if !slices.Contains(engine.PEQTypes, b.Type) {
				return d, errors.New("peq: type must be peak, lowshelf, highshelf, lowpass or highpass")
			}
		}
		d.PEQ = bands
	case "peqpreamp":
		if len(args) != 1 {
			return d, errors.New("usage: peqpreamp <dB>")
		}
		v, err := num(args[0])
		if err != nil {
			return d, errors.New("usage: peqpreamp <dB>")
		}
		d.PEQPreamp = v
	case "crossfeed":
		if len(args) != 1 || !slices.Contains(engine.CrossfeedLevels, strings.TrimPrefix(args[0], "off")) {
			return d, errors.New("usage: crossfeed off|light|medium|strong")
		}
		d.Crossfeed = strings.TrimPrefix(args[0], "off")
	case "resampler":
		if len(args) != 1 || (args[0] != "auto" && args[0] != "linear" && args[0] != "quality") {
			return d, errors.New("usage: resampler auto|linear|quality")
		}
		d.Resampler = args[0]
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
