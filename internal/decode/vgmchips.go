package decode

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// libgme solo emula los chips de VGM de la Mega Drive y la Master System
// (SN76489, YM2612, YM2413): un VGM de otra placa (las arcade de Sega con
// YM2151 y SegaPCM, QSound de Capcom, los de Konami...) "suena" en silencio
// absoluto. vgmCheck lo mira en la cabecera antes de abrirlo y, si no tiene
// ninguno de los que se emulan, da un error que dice cuáles tiene.

// vgmClocks son los relojes de la cabecera de un VGM: dónde está cada uno y
// el nombre del chip. Un reloj distinto de cero es que el chip se usa.
var vgmClocks = []struct {
	off  int
	name string
	ok   bool
}{
	{0x0C, "SN76489", true}, {0x10, "YM2413", true}, {0x2C, "YM2612", true},
	{0x30, "YM2151", false}, {0x38, "SegaPCM", false}, {0x40, "RF5C68", false}, {0x44, "YM2203", false},
	{0x48, "YM2608", false}, {0x4C, "YM2610", false}, {0x50, "YM3812", false}, {0x54, "YM3526", false},
	{0x58, "Y8950", false}, {0x5C, "YMF262", false}, {0x60, "YMF278B", false}, {0x64, "YMF271", false},
	{0x68, "YMZ280B", false}, {0x6C, "RF5C164", false}, {0x70, "PWM", false}, {0x74, "AY8910", false},
	{0x80, "Game Boy", false}, {0x84, "NES APU", false}, {0x88, "MultiPCM", false}, {0x8C, "uPD7759", false},
	{0x90, "OKIM6258", false}, {0x98, "OKIM6295", false}, {0x9C, "K051649", false}, {0xA0, "K054539", false},
	{0xA4, "HuC6280", false}, {0xA8, "C140", false}, {0xAC, "K053260", false}, {0xB0, "Pokey", false},
	{0xB4, "QSound", false},
}

// vgmCheck devuelve un error si el VGM (o VGZ) file no usa ningún chip que
// libgme emule. Si no puede leer la cabecera, no dice nada (que lo intente
// libgme).
func vgmCheck(file string) error {
	ext := strings.ToLower(filepath.Ext(file))
	if ext != ".vgm" && ext != ".vgz" {
		return nil
	}
	f, err := os.Open(file)
	if err != nil {
		return nil
	}
	defer f.Close()
	head := make([]byte, 0x100)
	n, _ := io.ReadFull(f, head)
	head = head[:n]
	if len(head) >= 2 && head[0] == 0x1F && head[1] == 0x8B {
		zr, err := gzip.NewReader(io.MultiReader(bytes.NewReader(head), f))
		if err != nil {
			return nil
		}
		head = make([]byte, 0x100)
		n, _ = io.ReadFull(zr, head)
		head = head[:n]
	}
	return vgmCheckHeader(head)
}

// vgmCheckHeader mira los chips de una cabecera de VGM.
func vgmCheckHeader(h []byte) error {
	if len(h) < 0x40 || string(h[:4]) != "Vgm " {
		return nil
	}
	u32 := func(o int) uint32 { return binary.LittleEndian.Uint32(h[o:]) }
	// Lo que mide la cabecera: hasta donde empiezan los datos (desde la
	// 1.50); antes, 0x40. Los campos de más allá no son de la cabecera.
	end := 0x40
	if u32(0x08) >= 0x150 {
		if d := u32(0x34); d != 0 {
			end = 0x34 + int(d)
		}
	}
	end = min(end, len(h))
	var used []string
	for _, c := range vgmClocks {
		if c.off+4 > end || u32(c.off)&0x3FFFFFFF == 0 {
			continue
		}
		if c.ok {
			return nil // tiene alguno que se emula: que suene lo que se pueda
		}
		used = append(used, c.name)
	}
	if len(used) == 0 {
		return nil
	}
	return fmt.Errorf("%w: VGM chips not emulated (%s)", ErrUnsupported, strings.Join(used, ", "))
}
