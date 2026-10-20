package daemon

import (
	"testing"

	"github.com/juanvillacortac/audiofilus/internal/engine"
)

// Los comandos del paramétrico y del remuestreo cambian la configuración y
// rechazan lo que no vale.
func TestDSPCommandsPEQ(t *testing.T) {
	d := engine.DSP{}.Normalize()
	d, err := dspCommand(d, "eqmode", []string{"parametric"})
	if err != nil || d.EQMode != "parametric" {
		t.Fatalf("eqmode: %v %q", err, d.EQMode)
	}
	d, err = dspCommand(d, "peq", []string{`[{"type":"peak","freq":1000,"gain":3,"q":1},{"type":"highpass","freq":40,"q":0.707}]`})
	if err != nil || len(d.PEQ) != 2 || d.PEQ[1].Type != "highpass" {
		t.Fatalf("peq: %v %+v", err, d.PEQ)
	}
	d, err = dspCommand(d, "peqpreamp", []string{"-3"})
	if err != nil || d.PEQPreamp != -3 {
		t.Fatalf("peqpreamp: %v %v", err, d.PEQPreamp)
	}
	d, err = dspCommand(d, "resampler", []string{"quality"})
	if err != nil || d.Resampler != "quality" {
		t.Fatalf("resampler: %v %q", err, d.Resampler)
	}
	for _, bad := range []struct {
		cmd  string
		args []string
	}{
		{"eqmode", []string{"fancy"}},
		{"peq", []string{`[{"type":"notch","freq":1000}]`}},
		{"peq", []string{"not json"}},
		{"resampler", []string{"sinc"}},
		{"peqpreamp", []string{"loud"}},
	} {
		if _, err := dspCommand(d, bad.cmd, bad.args); err == nil {
			t.Errorf("%s %v accepted", bad.cmd, bad.args)
		}
	}
}
