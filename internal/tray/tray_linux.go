// Package tray pone audiofilus en la bandeja del sistema del escritorio Linux,
// con el estándar StatusNotifierItem por D-Bus (el que entienden la barra de
// Omarchy, Waybar, KDE y GNOME con AppIndicator): el icono del CD; clic,
// abrir la interfaz; clic central, pausar o seguir; y un menú con lo que
// suena, reproducir/pausar, siguiente, anterior, abrir y salir. Vive en el
// servicio, así sigue ahí con la ventana cerrada y la música en segundo plano.
package tray

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"

	"github.com/juanvillacortac/audiofilus/internal/desktop"
	"github.com/juanvillacortac/audiofilus/internal/engine"
)

const (
	itemPath    = "/StatusNotifierItem"
	menuPath    = "/MenuBar"
	itemIface   = "org.kde.StatusNotifierItem"
	menuIface   = "com.canonical.dbusmenu"
	watcherName = "org.kde.StatusNotifierWatcher"
)

// Options es lo que la bandeja necesita del servicio.
type Options struct {
	Home   string      // AUDIOFILUS_HOME, para abrir la interfaz con la misma carpeta
	UIOpen func() bool // si hay una interfaz conectada
	Quit   func()      // salir del todo (como "Salir" en la interfaz)
	Lang   string      // "es" o "en"
}

// Tray es el icono publicado.
type Tray struct {
	eng   *engine.Engine
	opt   Options
	conn  *dbus.Conn
	name  string
	props *prop.Properties

	mu  sync.Mutex
	rev uint32
}

// pixmap es un icono para D-Bus: ARGB32 en orden de red.
type pixmap struct {
	W, H int32
	Data []byte
}

// tooltip es el texto que sale al pasar el ratón.
type tooltip struct {
	IconName string
	Icon     []pixmap
	Title    string
	Text     string
}

// Start publica el icono en el bus de sesión y lo registra en la bandeja (y
// de nuevo cada vez que la bandeja aparece: si la barra se reinicia). Sin bus
// de sesión, error y el servicio sigue sin bandeja.
func Start(eng *engine.Engine, opt Options) (*Tray, error) {
	if os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" && os.Getenv("XDG_RUNTIME_DIR") == "" {
		return nil, fmt.Errorf("no D-Bus session bus")
	}
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, err
	}
	t := &Tray{eng: eng, opt: opt, conn: conn, name: fmt.Sprintf("org.kde.StatusNotifierItem-%d-1", os.Getpid())}
	if reply, err := conn.RequestName(t.name, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		conn.Close()
		return nil, fmt.Errorf("D-Bus name %s is taken", t.name)
	}
	if err := t.export(); err != nil {
		conn.Close()
		return nil, err
	}
	// La bandeja puede llegar después (o reiniciarse): se registra cada vez.
	_ = conn.AddMatchSignal(dbus.WithMatchInterface("org.freedesktop.DBus"), dbus.WithMatchMember("NameOwnerChanged"),
		dbus.WithMatchArg(0, watcherName))
	sig := make(chan *dbus.Signal, 8)
	conn.Signal(sig)
	go func() {
		for s := range sig {
			if s.Name == "org.freedesktop.DBus.NameOwnerChanged" && len(s.Body) == 3 && s.Body[2] != "" {
				t.register()
			}
		}
	}()
	t.register()
	return t, nil
}

// register le pide a la bandeja que muestre el icono.
func (t *Tray) register() {
	call := t.conn.Object(watcherName, "/StatusNotifierWatcher").Call(watcherName+".RegisterStatusNotifierItem", 0, t.name)
	if call.Err != nil {
		log.Printf("tray: no system tray yet (%v)", call.Err)
	}
}

// Close quita el icono.
func (t *Tray) Close() { t.conn.Close() }

// export publica el icono y el menú, con sus propiedades y su introspección.
func (t *Tray) export() error {
	icons := iconPixmaps()
	var err error
	t.props, err = prop.Export(t.conn, itemPath, prop.Map{itemIface: {
		"Category":      {Value: "ApplicationStatus", Emit: prop.EmitConst},
		"Id":            {Value: "audiofilus", Emit: prop.EmitConst},
		"Title":         {Value: "Audiofilus", Emit: prop.EmitConst},
		"Status":        {Value: "Active", Emit: prop.EmitTrue},
		"WindowId":      {Value: int32(0), Emit: prop.EmitConst},
		"IconName":      {Value: "audiofilus", Emit: prop.EmitConst},
		"IconThemePath": {Value: "", Emit: prop.EmitConst},
		"IconPixmap":    {Value: icons, Emit: prop.EmitConst},
		"ItemIsMenu":    {Value: false, Emit: prop.EmitConst},
		"Menu":          {Value: dbus.ObjectPath(menuPath), Emit: prop.EmitConst},
		"ToolTip":       {Value: t.tooltip(), Emit: prop.EmitTrue},
	}})
	if err != nil {
		return err
	}
	if err := t.conn.Export(item{t}, itemPath, itemIface); err != nil {
		return err
	}
	menuProps, err := prop.Export(t.conn, menuPath, prop.Map{menuIface: {
		"Version":       {Value: uint32(3), Emit: prop.EmitConst},
		"TextDirection": {Value: "ltr", Emit: prop.EmitConst},
		"Status":        {Value: "normal", Emit: prop.EmitConst},
		"IconThemePath": {Value: []string{}, Emit: prop.EmitConst},
	}})
	if err != nil {
		return err
	}
	if err := t.conn.Export(menu{t}, menuPath, menuIface); err != nil {
		return err
	}
	for _, o := range []struct {
		path  string
		iface string
		obj   any
		props *prop.Properties
	}{{itemPath, itemIface, item{t}, t.props}, {menuPath, menuIface, menu{t}, menuProps}} {
		node := &introspect.Node{Name: o.path, Interfaces: []introspect.Interface{
			introspect.IntrospectData, prop.IntrospectData,
			{Name: o.iface, Methods: introspect.Methods(o.obj), Properties: o.props.Introspection(o.iface)},
		}}
		if err := t.conn.Export(introspect.NewIntrospectable(node), dbus.ObjectPath(o.path), "org.freedesktop.DBus.Introspectable"); err != nil {
			return err
		}
	}
	return nil
}

// Update rehace el texto y el menú (otra canción, pausa o play).
func (t *Tray) Update() {
	t.mu.Lock()
	t.rev++
	rev := t.rev
	t.mu.Unlock()
	t.props.SetMust(itemIface, "ToolTip", t.tooltip())
	_ = t.conn.Emit(itemPath, itemIface+".NewToolTip")
	_ = t.conn.Emit(menuPath, menuIface+".LayoutUpdated", rev, int32(0))
}

// tooltip es lo que suena.
func (t *Tray) tooltip() tooltip {
	return tooltip{IconName: "audiofilus", Title: "Audiofilus", Text: t.nowPlaying()}
}

// nowPlaying es "Artista — Título" (o el nombre del archivo), con "(en
// pausa)" si lo está.
func (t *Tray) nowPlaying() string {
	st := t.eng.Status()
	if st.Track.Path == "" {
		return t.tr("Nada sonando", "Nothing playing")
	}
	s := st.Track.Title
	if s == "" {
		s = st.Track.Path[strings.LastIndex(st.Track.Path, "/")+1:]
	}
	if st.Track.Artist != "" {
		s = st.Track.Artist + " — " + s
	}
	if r := []rune(s); len(r) > 60 {
		s = string(r[:59]) + "…"
	}
	if st.State != "playing" {
		s += t.tr(" (en pausa)", " (paused)")
	}
	return s
}

// tr elige el texto del idioma de la interfaz.
func (t *Tray) tr(es, en string) string {
	if t.opt.Lang == "en" || t.opt.Lang == "" && !strings.HasPrefix(os.Getenv("LANG"), "es") {
		return en
	}
	return es
}

// open abre la interfaz (este mismo binario, sin argumentos), si no está ya
// abierta, en su propia sesión: es del usuario, no del servicio.
func (t *Tray) open() {
	if t.opt.UIOpen() {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		log.Printf("tray: %v", err)
		return
	}
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), "AUDIOFILUS_HOME="+t.opt.Home)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		log.Printf("tray: opening the interface: %v", err)
		return
	}
	go func() { _ = cmd.Wait() }()
}

// iconPixmaps es el icono de la app (el CD de 256×256) en los tamaños de una
// bandeja, reducido sin suavizar (es pixel art). Por si el tema de iconos no
// tiene "audiofilus" (sin `audiofilus desktop install`).
func iconPixmaps() []pixmap {
	img, err := png.Decode(bytes.NewReader(desktop.Icon))
	if err != nil {
		return nil
	}
	var out []pixmap
	for _, size := range []int{32, 64} {
		out = append(out, scaled(img, size))
	}
	return out
}

// scaled es img reducida a size×size tomando un píxel de cada bloque.
func scaled(img image.Image, size int) pixmap {
	b := img.Bounds()
	p := pixmap{W: int32(size), H: int32(size), Data: make([]byte, 0, size*size*4)}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			r, g, bl, a := img.At(b.Min.X+x*b.Dx()/size+b.Dx()/size/2, b.Min.Y+y*b.Dy()/size+b.Dy()/size/2).RGBA()
			p.Data = append(p.Data, byte(a>>8), byte(r>>8), byte(g>>8), byte(bl>>8))
		}
	}
	return p
}

// item son los métodos del icono.
type item struct{ t *Tray }

// Activate es el clic: abrir la interfaz.
func (i item) Activate(x, y int32) *dbus.Error { i.t.open(); return nil }

// SecondaryActivate es el clic central: pausar o seguir.
func (i item) SecondaryActivate(x, y int32) *dbus.Error {
	i.t.eng.Toggle()
	return nil
}

// ContextMenu no hace falta: la bandeja muestra el menú (Menu).
func (i item) ContextMenu(x, y int32) *dbus.Error { return nil }

// Scroll no hace nada.
func (i item) Scroll(delta int32, orientation string) *dbus.Error { return nil }

// Los elementos del menú.
const (
	idNow = iota + 1
	idToggle
	idNext
	idPrev
	idSep1
	idOpen
	idSep2
	idQuit
)

// menuLayout es un elemento del menú y sus hijos, como lo pide dbusmenu.
type menuLayout struct {
	ID       int32
	Props    map[string]dbus.Variant
	Children []dbus.Variant
}

// itemProps son las propiedades de cada elemento.
func (t *Tray) itemProps(id int32) map[string]dbus.Variant {
	label := func(s string) map[string]dbus.Variant { return map[string]dbus.Variant{"label": dbus.MakeVariant(s)} }
	switch id {
	case idNow:
		m := label(t.nowPlaying())
		m["enabled"] = dbus.MakeVariant(false)
		return m
	case idToggle:
		if t.eng.Status().State == "playing" {
			return label(t.tr("Pausar", "Pause"))
		}
		return label(t.tr("Reproducir", "Play"))
	case idNext:
		return label(t.tr("Siguiente", "Next"))
	case idPrev:
		return label(t.tr("Anterior", "Previous"))
	case idOpen:
		return label(t.tr("Abrir Audiofilus", "Open Audiofilus"))
	case idQuit:
		return label(t.tr("Salir", "Quit"))
	case idSep1, idSep2:
		return map[string]dbus.Variant{"type": dbus.MakeVariant("separator")}
	}
	return map[string]dbus.Variant{"children-display": dbus.MakeVariant("submenu")}
}

// menu son los métodos de dbusmenu.
type menu struct{ t *Tray }

// GetLayout da el menú entero (es plano: todo cuelga de la raíz, 0).
func (m menu) GetLayout(parent, depth int32, names []string) (uint32, menuLayout, *dbus.Error) {
	m.t.mu.Lock()
	rev := m.t.rev
	m.t.mu.Unlock()
	if parent != 0 {
		return rev, menuLayout{ID: parent, Props: m.t.itemProps(parent), Children: []dbus.Variant{}}, nil
	}
	root := menuLayout{ID: 0, Props: m.t.itemProps(0)}
	for id := int32(idNow); id <= idQuit; id++ {
		root.Children = append(root.Children, dbus.MakeVariant(menuLayout{ID: id, Props: m.t.itemProps(id), Children: []dbus.Variant{}}))
	}
	return rev, root, nil
}

// groupProps es un elemento con sus propiedades.
type groupProps struct {
	ID    int32
	Props map[string]dbus.Variant
}

// GetGroupProperties da las propiedades de varios elementos.
func (m menu) GetGroupProperties(ids []int32, names []string) ([]groupProps, *dbus.Error) {
	if len(ids) == 0 {
		for id := int32(idNow); id <= idQuit; id++ {
			ids = append(ids, id)
		}
	}
	out := make([]groupProps, 0, len(ids))
	for _, id := range ids {
		out = append(out, groupProps{id, m.t.itemProps(id)})
	}
	return out, nil
}

// GetProperty da una propiedad de un elemento.
func (m menu) GetProperty(id int32, name string) (dbus.Variant, *dbus.Error) {
	if v, ok := m.t.itemProps(id)[name]; ok {
		return v, nil
	}
	return dbus.MakeVariant(""), nil
}

// Event atiende un clic en el menú.
func (m menu) Event(id int32, event string, data dbus.Variant, ts uint32) *dbus.Error {
	if event != "clicked" {
		return nil
	}
	t := m.t
	switch id {
	case idToggle:
		t.eng.Toggle()
	case idNext:
		t.eng.Next()
	case idPrev:
		t.eng.Prev()
	case idOpen:
		t.open()
	case idQuit:
		t.opt.Quit()
	}
	return nil
}

// menuEvent es un evento de EventGroup.
type menuEvent struct {
	ID    int32
	Event string
	Data  dbus.Variant
	Time  uint32
}

// EventGroup atiende varios eventos.
func (m menu) EventGroup(events []menuEvent) ([]int32, *dbus.Error) {
	for _, e := range events {
		_ = m.Event(e.ID, e.Event, e.Data, e.Time)
	}
	return []int32{}, nil
}

// AboutToShow: el menú está al día (se rehace en cada GetLayout).
func (m menu) AboutToShow(id int32) (bool, *dbus.Error) { return false, nil }

// AboutToShowGroup es AboutToShow para varios.
func (m menu) AboutToShowGroup(ids []int32) ([]int32, []int32, *dbus.Error) {
	return []int32{}, []int32{}, nil
}
