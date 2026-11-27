//go:build darwin && !ios

package platform

import (
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// On macOS "plugged" means the sound goes somewhere other than the built-in
// speakers (headphones, Bluetooth, a USB DAC, HDMI): when the Mac falls back
// to them while playing, daemon/jack.go pauses, as Music does.

// CoreAudio's four-character codes.
const (
	caSystem     = 1          // kAudioObjectSystemObject
	caDefaultOut = 0x644f7574 // 'dOut'
	caTransport  = 0x7472616e // 'tran'
	caSource     = 0x73737263 // 'ssrc'
	caGlobal     = 0x676c6f62 // 'glob'
	caOutput     = 0x6f757470 // 'outp'
	caBuiltIn    = 0x626c746e // 'bltn'
	caHeadphones = 0x6864706e // 'hdpn'
)

var coreAudio struct {
	once sync.Once
	get  func(obj uint32, addr *[3]uint32, qualSize uint32, qual unsafe.Pointer, size *uint32, out unsafe.Pointer) int32
}

// caProp reads a 32-bit property of a CoreAudio object.
func caProp(obj, sel, scope uint32) (uint32, bool) {
	var v uint32
	n := uint32(4)
	addr := [3]uint32{sel, scope, 0}
	return v, coreAudio.get(obj, &addr, 0, nil, &n, unsafe.Pointer(&v)) == 0
}

// WiredJack says whether the default output is something other than the
// built-in speakers. Reading it is cheap (in process).
func WiredJack() (plugged, known, cheap bool) {
	coreAudio.once.Do(func() {
		if lib, err := purego.Dlopen("/System/Library/Frameworks/CoreAudio.framework/CoreAudio", purego.RTLD_NOW); err == nil {
			purego.RegisterLibFunc(&coreAudio.get, lib, "AudioObjectGetPropertyData")
		}
	})
	if coreAudio.get == nil {
		return false, false, false
	}
	dev, ok := caProp(caSystem, caDefaultOut, caGlobal)
	if !ok || dev == 0 {
		return false, false, false
	}
	transport, ok := caProp(dev, caTransport, caGlobal)
	if !ok {
		return false, false, false
	}
	source, _ := caProp(dev, caSource, caOutput)
	return !speakers(transport, source), true, true
}

// speakers says whether a device is the built-in speakers: built in and not
// the headphone jack (its own device on Apple silicon, a data source of the
// built-in output on Intel; both report 'hdpn').
func speakers(transport, source uint32) bool {
	return transport == caBuiltIn && source != caHeadphones
}
