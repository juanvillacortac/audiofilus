//go:build darwin && !ios

package platform

import "testing"

// Only the built-in speakers count as unplugged: the headphone jack, Bluetooth
// and USB don't.
func TestSpeakers(t *testing.T) {
	const bluetooth, usb = 0x626c7565, 0x75736220 // 'blue', 'usb '
	for _, c := range []struct {
		transport, source uint32
		want              bool
	}{
		{caBuiltIn, 0x6973706b, true}, // 'ispk'
		{caBuiltIn, 0, true},
		{caBuiltIn, caHeadphones, false},
		{bluetooth, 0, false},
		{usb, 0, false},
	} {
		if got := speakers(c.transport, c.source); got != c.want {
			t.Errorf("speakers(%x, %x) = %v, want %v", c.transport, c.source, got, c.want)
		}
	}
}
