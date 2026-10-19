// La bandeja en Linux: StatusNotifierItem por D-Bus y su menú (dbusmenu).

package tray

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"

	"github.com/juanvillacortac/audiofilus/internal/engine"
)

const (
	itemPath    = "/StatusNotifierItem"
	menuPath    = "/MenuBar"
	itemIface   = "org.kde.StatusNotifierItem"
	menuIface   = "com.canonical.dbusmenu"
	watcherName = "org.kde.StatusNotifierWatcher"
)

// Tray es el icono publicado.
type Tray struct {
	base
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
	t := &Tray{base: base{eng: eng, opt: opt}, conn: conn, name: fmt.Sprintf("org.kde.StatusNotifierItem-%d-1", os.Getpid())}
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

// iconPixmaps es el icono en los tamaños de una bandeja, en ARGB32 (por si
// el tema de iconos no tiene "audiofilus", sin `audiofilus desktop install`).
func iconPixmaps() []pixmap {
	var out []pixmap
	for _, size := range []int{32, 64} {
		img := iconAt(size)
		p := pixmap{W: int32(size), H: int32(size), Data: make([]byte, 0, size*size*4)}
		for i := 0; i < len(img.Pix); i += 4 {
			p.Data = append(p.Data, img.Pix[i+3], img.Pix[i], img.Pix[i+1], img.Pix[i+2])
		}
		out = append(out, p)
	}
	return out
}

// detach separa la interfaz del servicio: en su propia sesión.
func detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }

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

// menuLayout es un elemento del menú y sus hijos, como lo pide dbusmenu.
type menuLayout struct {
	ID       int32
	Props    map[string]dbus.Variant
	Children []dbus.Variant
}

// itemProps son las propiedades de cada elemento (0 es la raíz).
func (t *Tray) itemProps(id int32) map[string]dbus.Variant {
	switch {
	case id == 0:
		return map[string]dbus.Variant{"children-display": dbus.MakeVariant("submenu")}
	case separator(int(id)):
		return map[string]dbus.Variant{"type": dbus.MakeVariant("separator")}
	}
	m := map[string]dbus.Variant{"label": dbus.MakeVariant(t.label(int(id)))}
	switch id {
	case idNow:
		m["enabled"] = dbus.MakeVariant(false)
	case idShuffle:
		m["toggle-type"] = dbus.MakeVariant("checkmark")
		on := int32(0)
		if t.checked(idShuffle) {
			on = 1
		}
		m["toggle-state"] = dbus.MakeVariant(on)
	}
	return m
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
	for id := int32(idNow); id <= idLast; id++ {
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
		for id := int32(idNow); id <= idLast; id++ {
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
	if event == "clicked" && m.t.do(int(id)) {
		m.t.Update() // la casilla
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
