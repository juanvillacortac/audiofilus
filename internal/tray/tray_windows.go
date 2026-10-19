// La bandeja en Windows: el área de notificación (Shell_NotifyIcon), con una
// ventana invisible que recibe sus clics y un menú emergente, en Go puro.
// Windows exige que la ventana y su bucle de mensajes vivan en un mismo hilo:
// una goroutine fijada a el suyo.

package tray

import (
	"bytes"
	"errors"
	"image/png"
	"os/exec"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/juanvillacortac/audiofilus/internal/engine"
)

var (
	user32  = windows.NewLazySystemDLL("user32.dll")
	shell32 = windows.NewLazySystemDLL("shell32.dll")

	pRegisterClassEx       = user32.NewProc("RegisterClassExW")
	pCreateWindowEx        = user32.NewProc("CreateWindowExW")
	pDefWindowProc         = user32.NewProc("DefWindowProcW")
	pGetMessage            = user32.NewProc("GetMessageW")
	pTranslateMessage      = user32.NewProc("TranslateMessage")
	pDispatchMessage       = user32.NewProc("DispatchMessageW")
	pPostMessage           = user32.NewProc("PostMessageW")
	pDestroyWindow         = user32.NewProc("DestroyWindow")
	pPostQuitMessage       = user32.NewProc("PostQuitMessage")
	pCreatePopupMenu       = user32.NewProc("CreatePopupMenu")
	pAppendMenu            = user32.NewProc("AppendMenuW")
	pTrackPopupMenu        = user32.NewProc("TrackPopupMenu")
	pDestroyMenu           = user32.NewProc("DestroyMenu")
	pGetCursorPos          = user32.NewProc("GetCursorPos")
	pSetForegroundWindow   = user32.NewProc("SetForegroundWindow")
	pRegisterWindowMessage = user32.NewProc("RegisterWindowMessageW")
	pCreateIconFromRes     = user32.NewProc("CreateIconFromResourceEx")
	pDestroyIcon           = user32.NewProc("DestroyIcon")
	pGetSystemMetrics      = user32.NewProc("GetSystemMetrics")
	pShellNotifyIcon       = shell32.NewProc("Shell_NotifyIconW")
)

const (
	nimAdd, nimModify, nimDelete   = 0, 1, 2
	nifMessage, nifIcon, nifTip    = 1, 2, 4
	wmNull, wmDestroy, wmClose     = 0x0000, 0x0002, 0x0010
	wmLButtonUp, wmRButtonUp       = 0x0202, 0x0205
	wmMButtonUp                    = 0x0208
	wmTray                         = 0x8000 + 1 // WM_APP+1: los clics en el icono
	mfString, mfGrayed, mfChecked  = 0x0000, 0x0001, 0x0008
	mfSeparator                    = 0x0800
	tpmReturnCmd, tpmNoNotify      = 0x0100, 0x0080
	tpmRightButton, tpmBottomAlign = 0x0002, 0x0020
	smCxSmIcon                     = 49
	lrDefaultColor, iconResVersion = 0, 0x00030000
	createNewProcessGroup          = 0x00000200
	trayIconID                     = 1
)

// notifyIconData es NOTIFYICONDATAW (la de Vista en adelante).
type notifyIconData struct {
	Size            uint32
	Wnd             windows.Handle
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            windows.Handle
	Tip             [128]uint16
	State           uint32
	StateMask       uint32
	Info            [256]uint16
	Version         uint32
	InfoTitle       [64]uint16
	InfoFlags       uint32
	GUIDItem        windows.GUID
	BalloonIcon     windows.Handle
}

// wndClassEx es WNDCLASSEXW.
type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   windows.Handle
	Icon       windows.Handle
	Cursor     windows.Handle
	Background windows.Handle
	MenuName   *uint16
	ClassName  *uint16
	IconSm     windows.Handle
}

// msg es MSG.
type msg struct {
	Wnd     windows.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
	Private uint32
}

// Tray es el icono del área de notificación.
type Tray struct {
	base
	wnd            windows.Handle
	icon           windows.Handle
	taskbarCreated uint32 // el mensaje que llega cuando el Explorador se reinicia
}

// current es la bandeja (solo hay una): la ventana la necesita en wndProc.
var current *Tray

// Start crea el icono, con su ventana y su bucle de mensajes en un hilo propio.
func Start(eng *engine.Engine, opt Options) (*Tray, error) {
	if current != nil {
		return nil, errors.New("the tray is already running")
	}
	t := &Tray{base: base{eng: eng, opt: opt}}
	ready := make(chan error, 1)
	go t.loop(ready)
	if err := <-ready; err != nil {
		return nil, err
	}
	return t, nil
}

// loop crea la ventana y el icono y atiende sus mensajes hasta Close.
func (t *Tray) loop(ready chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var inst windows.Handle
	if err := windows.GetModuleHandleEx(0, nil, &inst); err != nil {
		ready <- err
		return
	}
	class, _ := windows.UTF16PtrFromString("audiofilus-tray")
	wc := wndClassEx{WndProc: windows.NewCallback(wndProc), Instance: inst, ClassName: class}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if r, _, err := pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		ready <- err
		return
	}
	title, _ := windows.UTF16PtrFromString("Audiofilus")
	// Una ventana normal pero nunca visible (no "solo mensajes"): así le llega
	// TaskbarCreated si el Explorador se reinicia, y se vuelve a poner el icono.
	wnd, _, err := pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)), 0,
		0, 0, 0, 0, 0, 0, uintptr(inst), 0)
	if wnd == 0 {
		ready <- err
		return
	}
	t.wnd = windows.Handle(wnd)
	tc, _ := windows.UTF16PtrFromString("TaskbarCreated")
	r, _, _ := pRegisterWindowMessage.Call(uintptr(unsafe.Pointer(tc)))
	t.taskbarCreated = uint32(r)
	t.icon = makeIcon()
	current = t
	if !t.notify(nimAdd) {
		ready <- errors.New("Shell_NotifyIcon failed")
		return
	}
	ready <- nil
	var m msg
	for {
		if r, _, _ := pGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0); int32(r) <= 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
	if t.icon != 0 {
		pDestroyIcon.Call(uintptr(t.icon))
	}
}

// notify pone, cambia o quita el icono (op: nimAdd, nimModify, nimDelete).
func (t *Tray) notify(op uintptr) bool {
	d := notifyIconData{Wnd: t.wnd, ID: trayIconID, Flags: nifMessage | nifIcon | nifTip, CallbackMessage: wmTray, Icon: t.icon}
	d.Size = uint32(unsafe.Sizeof(d))
	tip, _ := windows.UTF16FromString("Audiofilus\n" + t.nowPlaying())
	if len(tip) > len(d.Tip) {
		tip = append(tip[:len(d.Tip)-1], 0)
	}
	copy(d.Tip[:], tip)
	r, _, _ := pShellNotifyIcon.Call(op, uintptr(unsafe.Pointer(&d)))
	return r != 0
}

// Update rehace el texto del icono (otra canción, pausa o play); el menú se
// arma al abrirlo.
func (t *Tray) Update() { t.notify(nimModify) }

// Close quita el icono y termina su hilo.
func (t *Tray) Close() { pPostMessage.Call(uintptr(t.wnd), wmClose, 0, 0) }

// wndProc atiende los mensajes de la ventana invisible.
func wndProc(wnd windows.Handle, m uint32, wp, lp uintptr) uintptr {
	t := current
	switch {
	case t == nil:
	case m == wmTray:
		switch lp & 0xffff {
		case wmLButtonUp:
			t.do(idOpen)
		case wmMButtonUp:
			t.do(idToggle)
		case wmRButtonUp:
			t.showMenu()
		}
		return 0
	case m == t.taskbarCreated && m != 0:
		t.notify(nimAdd)
		return 0
	case m == wmClose:
		t.notify(nimDelete)
		pDestroyWindow.Call(uintptr(wnd))
		return 0
	case m == wmDestroy:
		current = nil
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProc.Call(uintptr(wnd), uintptr(m), wp, lp)
	return r
}

// showMenu abre el menú junto al cursor y hace lo elegido.
func (t *Tray) showMenu() {
	menu, _, _ := pCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer pDestroyMenu.Call(menu)
	for id := idNow; id <= idLast; id++ {
		if separator(id) {
			pAppendMenu.Call(menu, mfSeparator, 0, 0)
			continue
		}
		flags := uintptr(mfString)
		if id == idNow {
			flags |= mfGrayed
		}
		if t.checked(id) {
			flags |= mfChecked
		}
		text, _ := windows.UTF16PtrFromString(t.label(id))
		pAppendMenu.Call(menu, flags, uintptr(id), uintptr(unsafe.Pointer(text)))
	}
	var pt struct{ X, Y int32 }
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	// Sin esto el menú no se cierra al hacer clic fuera (lo documenta
	// TrackPopupMenu).
	pSetForegroundWindow.Call(uintptr(t.wnd))
	id, _, _ := pTrackPopupMenu.Call(menu, tpmReturnCmd|tpmNoNotify|tpmRightButton|tpmBottomAlign,
		uintptr(pt.X), uintptr(pt.Y), 0, uintptr(t.wnd), 0)
	pPostMessage.Call(uintptr(t.wnd), wmNull, 0, 0)
	if id != 0 {
		t.do(int(id))
		t.Update()
	}
}

// makeIcon es el CD del logo al tamaño de los iconos pequeños del sistema
// (16 a 100 % de escala, 24 a 150 %...), reducido sin suavizar.
func makeIcon() windows.Handle {
	size, _, _ := pGetSystemMetrics.Call(smCxSmIcon)
	if size == 0 {
		size = 16
	}
	var b bytes.Buffer
	if err := png.Encode(&b, iconAt(int(size))); err != nil {
		return 0
	}
	data := b.Bytes()
	h, _, _ := pCreateIconFromRes.Call(uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)), 1, iconResVersion,
		size, size, lrDefaultColor)
	return windows.Handle(h)
}

// detach separa la interfaz del servicio (su propio grupo de procesos), sin
// esconder su ventana.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
}
