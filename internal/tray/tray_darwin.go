//go:build darwin && !ios

package tray

import (
	"bytes"
	"errors"
	"image/png"
	"os"
	"os/exec"
	"runtime"
	"sync/atomic"
	"syscall"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	"github.com/juanvillacortac/audiofilus/internal/engine"
)

// On macOS the tray is an NSStatusItem in the menu bar. AppKit only runs on
// the main thread, so the service runs in a goroutine (RunWithMain) and the
// main thread waits here for the tray to start [NSApp run].

func init() { runtime.LockOSThread() } // keeps main() on the main thread

var (
	mainq     = make(chan func())
	mainReady atomic.Bool
	cur       *Tray // the one tray, for the Objective-C callbacks
)

// RunWithMain runs the service (run) in a goroutine and serves AppKit on the
// main thread; the process exits when run returns.
func RunWithMain(run func()) {
	mainReady.Store(true)
	go func() {
		run()
		os.Exit(0)
	}()
	for f := range mainq {
		f()
	}
}

// Tray is the menu bar icon.
type Tray struct {
	base
}

// Start puts the icon in the menu bar. It needs RunWithMain (the service).
func Start(eng *engine.Engine, opt Options) (*Tray, error) {
	if !mainReady.Load() {
		return nil, errors.New("no AppKit main thread")
	}
	t := &Tray{base: base{eng: eng, opt: opt}}
	ready := make(chan error, 1)
	mainq <- func() {
		err := t.setup()
		ready <- err
		if err == nil {
			objc.ID(objc.GetClass("NSApplication")).Send(sel("sharedApplication")).Send(sel("run")) // never returns
		}
	}
	return t, <-ready
}

// Update does nothing: the menu reads the state each time it opens.
func (t *Tray) Update() {}

// Close does nothing: the icon goes away with the process.
func (t *Tray) Close() {}

// detach separa la interfaz del servicio: en su propia sesión.
func detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }

func sel(name string) objc.SEL { return objc.RegisterName(name) }

// nsString is s as an autoreleased NSString.
func nsString(s string) objc.ID {
	b := append([]byte(s), 0)
	return objc.ID(objc.GetClass("NSString")).Send(sel("stringWithUTF8String:"), &b[0])
}

type nsSize struct{ W, H float64 }

// setup creates the status item and its menu, on the main thread.
func (t *Tray) setup() error {
	if _, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
		return err
	}
	cls, err := objc.RegisterClass("AudiofilusTray", objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{
		{Cmd: sel("menuNeedsUpdate:"), Fn: menuNeedsUpdate},
		{Cmd: sel("itemClicked:"), Fn: itemClicked},
		// Opening Audiofilus.app while only the service runs reaches the
		// service (same bundle): it opens the interface.
		{Cmd: sel("applicationShouldHandleReopen:hasVisibleWindows:"), Fn: reopen},
	})
	if err != nil {
		return err
	}
	cur = t
	target := objc.ID(cls).Send(sel("new"))
	app := objc.ID(objc.GetClass("NSApplication")).Send(sel("sharedApplication"))
	app.Send(sel("setActivationPolicy:"), 1) // accessory: no Dock icon
	app.Send(sel("setDelegate:"), target)

	item := objc.ID(objc.GetClass("NSStatusBar")).Send(sel("systemStatusBar")).Send(sel("statusItemWithLength:"), float64(-1))
	item.Send(sel("retain"))
	var buf bytes.Buffer
	if err := png.Encode(&buf, monoIcon(36)); err != nil {
		return err
	}
	b := buf.Bytes()
	data := objc.ID(objc.GetClass("NSData")).Send(sel("dataWithBytes:length:"), unsafe.Pointer(&b[0]), len(b))
	runtime.KeepAlive(b)
	img := objc.ID(objc.GetClass("NSImage")).Send(sel("alloc")).Send(sel("initWithData:"), data)
	img.Send(sel("setSize:"), nsSize{18, 18}) // 36 px for Retina
	img.Send(sel("setTemplate:"), true)       // tinted by macOS for light and dark bars
	item.Send(sel("button")).Send(sel("setImage:"), img)

	menu := objc.ID(objc.GetClass("NSMenu")).Send(sel("alloc")).Send(sel("initWithTitle:"), nsString("Audiofilus"))
	menu.Send(sel("setAutoenablesItems:"), false)
	menu.Send(sel("setDelegate:"), target)
	for id := idNow; id <= idLast; id++ {
		var mi objc.ID
		if separator(id) {
			mi = objc.ID(objc.GetClass("NSMenuItem")).Send(sel("separatorItem"))
		} else {
			mi = objc.ID(objc.GetClass("NSMenuItem")).Send(sel("alloc")).Send(sel("initWithTitle:action:keyEquivalent:"),
				nsString(t.label(id)), sel("itemClicked:"), nsString(""))
			mi.Send(sel("setTarget:"), target)
			mi.Send(sel("setTag:"), id)
			mi.Send(sel("setEnabled:"), id != idNow)
		}
		menu.Send(sel("addItem:"), mi)
	}
	item.Send(sel("setMenu:"), menu)
	return nil
}

// menuNeedsUpdate refreshes the labels and the Shuffle check before the menu opens.
func menuNeedsUpdate(_ objc.ID, _ objc.SEL, menu objc.ID) {
	for id := idNow; id <= idLast; id++ {
		if separator(id) {
			continue
		}
		mi := menu.Send(sel("itemWithTag:"), id)
		mi.Send(sel("setTitle:"), nsString(cur.label(id)))
		state := 0
		if cur.checked(id) {
			state = 1
		}
		mi.Send(sel("setState:"), state)
	}
}

// itemClicked does what the menu item says, off the main thread.
func itemClicked(_ objc.ID, _ objc.SEL, sender objc.ID) {
	id := objc.Send[int](sender, sel("tag"))
	go cur.do(id)
}

// reopen opens the interface when Audiofilus.app is opened again.
func reopen(_ objc.ID, _ objc.SEL, _ objc.ID, _ bool) bool {
	go cur.open()
	return true
}
