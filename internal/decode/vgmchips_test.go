package decode

import (
	"encoding/binary"
	"strings"
	"testing"
)

// Un VGM de chips que libgme no emula (Out Run: YM2151 y SegaPCM) da un
// error en vez de sonar en silencio; con un chip de la Mega Drive, no.
func TestVGMChips(t *testing.T) {
	h := make([]byte, 0x100)
	copy(h, "Vgm ")
	binary.LittleEndian.PutUint32(h[0x08:], 0x151)
	binary.LittleEndian.PutUint32(h[0x34:], 0xCC) // datos en 0x100
	binary.LittleEndian.PutUint32(h[0x30:], 4000000)
	binary.LittleEndian.PutUint32(h[0x38:], 4000000)
	err := vgmCheckHeader(h)
	if err == nil || !strings.Contains(err.Error(), "YM2151") || !strings.Contains(err.Error(), "SegaPCM") {
		t.Errorf("arcade VGM: %v", err)
	}
	binary.LittleEndian.PutUint32(h[0x2C:], 7670453) // y un YM2612
	if err := vgmCheckHeader(h); err != nil {
		t.Errorf("VGM with a YM2612: %v", err)
	}
}
