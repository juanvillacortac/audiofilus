package ui

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/config"
	"github.com/juanvillacortac/audiofilus/internal/control"
	"github.com/juanvillacortac/audiofilus/internal/daemon"
	"github.com/juanvillacortac/audiofilus/internal/proc"
)

const (
	// statusEvery es cada cuánto se pregunta el estado al servicio. Entre medias
	// la posición avanza sola en la interfaz.
	statusEvery = 250 * time.Millisecond
	// sleepStatusEvery es lo mismo con la pantalla apagada: solo hace falta
	// enterarse de que el servicio la despertó (por un gesto).
	sleepStatusEvery = time.Second
	// awakeEvery y awakeLease: mientras la interfaz está abierta renueva cada
	// tanto el permiso para que la consola no se suspenda con música sonando; si
	// la interfaz muere, el permiso caduca solo.
	awakeEvery = 10 * time.Second
	awakeLease = "30"
)

// Client habla con el servicio de audio. Los comandos se mandan en segundo
// plano, para que la interfaz nunca espere al socket.
type Client struct {
	paths config.Paths

	mu     sync.Mutex
	status daemon.FullStatus
	got    time.Time // cuándo llegó status
	err    error
	queue  []string // la última cola pedida con "queue"

	attachConn net.Conn // la conexión de "attach", para cerrarla al salir
	// WasRunning dice si el servicio ya corría al abrir la interfaz (la
	// música seguía en segundo plano): entonces no hay splash.
	WasRunning bool
	// keepPlaying: al cerrarse la interfaz, la música sigue (Ajustes). Se le
	// dice al servicio al cambiar y en cada attach (que lo reinicia).
	keepPlaying bool
	// quit: el servicio avisó de que se cierra del todo (Salir en la bandeja,
	// `audiofilus quit`...): la interfaz se cierra y no lo vuelve a arrancar.
	quit atomic.Bool

	// OnWake se llama (desde otro hilo) cuando el servicio sale del sleep: la
	// interfaz no dibuja nada en sleep y necesita que la despierten.
	OnWake func()

	cmds chan control.Request
	stop chan struct{}
	done chan struct{}
}

// NewClient prepara el cliente; Start arranca el servicio si hace falta.
func NewClient(paths config.Paths) *Client {
	return &Client{paths: paths, cmds: make(chan control.Request, 32), stop: make(chan struct{}), done: make(chan struct{})}
}

// Start se asegura de que el servicio corre y empieza a seguir su estado.
func (c *Client) Start() error {
	_, err := control.Call(c.paths.Socket, control.Request{Cmd: "status"})
	c.WasRunning = err == nil
	if errors.Is(err, control.ErrNotRunning) {
		if err := c.startDaemon(); err != nil {
			return err
		}
	}
	go c.loop()
	go c.attach()
	return nil
}

// attach mantiene abierta la conexión que le dice al servicio que la interfaz
// sigue viva: si la interfaz muere de golpe, el sistema la cierra y el
// servicio para la música (salvo que se haya pedido segundo plano). Si el
// servicio se reinicia, se vuelve a conectar.
func (c *Client) attach() {
	for {
		conn, err := control.Attach(c.paths.Socket)
		if err == nil {
			c.mu.Lock()
			c.attachConn = conn
			keep := c.keepPlaying
			c.mu.Unlock()
			if keep {
				c.Background()
			}
			// Bloquea hasta que se cierre. Cada byte que llega es un aviso de que
			// el estado cambió (control.Notify): se pregunta ya.
			buf := make([]byte, 16)
			for {
				n, err := conn.Read(buf)
				if err != nil {
					break
				}
				if bytes.IndexByte(buf[:n], 'q') >= 0 {
					c.quit.Store(true)
					if c.OnWake != nil {
						c.OnWake() // para que la interfaz lo vea aunque esté quieta
					}
					return
				}
				c.Send("status")
			}
		}
		select {
		case <-c.stop:
			return
		case <-time.After(time.Second):
		}
	}
}

// QuitRequested dice si el servicio avisó de que se cierra del todo.
func (c *Client) QuitRequested() bool { return c.quit.Load() }

// Background le dice al servicio que siga sonando cuando la interfaz se cierre.
func (c *Client) Background() {
	_, _ = control.Call(c.paths.Socket, control.Request{Cmd: "background"})
}

// SetKeepPlaying elige si la música sigue al cerrarse la interfaz de cualquier
// forma (cerrar la ventana, o que muera): se lo dice al servicio ya y tras
// cada attach.
func (c *Client) SetKeepPlaying(on bool) {
	c.mu.Lock()
	changed := c.keepPlaying != on
	c.keepPlaying = on
	c.mu.Unlock()
	if !changed {
		return
	}
	if on {
		c.Send("background")
	} else {
		c.Send("background", "off")
	}
}

// daemonPath es el binario del servicio: este mismo (audiofilus es un solo
// binario: la interfaz, el servicio y la línea de comandos).
func (c *Client) daemonPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("finding the audiofilus binary: %w", err)
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return exe, nil
}

// startDaemon lanza `audiofilus daemon -detach`, que se separa en su propia
// sesión: así sigue sonando cuando se cierra la interfaz.
func (c *Client) startDaemon() error {
	bin, err := c.daemonPath()
	if err != nil {
		return err
	}
	cmd := proc.Hidden(exec.Command(bin, "daemon", "-detach"))
	cmd.Env = append(os.Environ(), "AUDIOFILUS_HOME="+c.paths.Base)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("starting the audio service: %v: %s", err, out)
	}
	log.Print("audio service started")
	return nil
}

// loop manda los comandos, pregunta el estado y renueva el permiso de awake.
func (c *Client) loop() {
	defer close(c.done)
	tick := time.NewTicker(statusEvery)
	defer tick.Stop()
	every := statusEvery
	lastAwake := time.Time{}
	for {
		select {
		case <-c.stop:
			_, _ = control.Call(c.paths.Socket, control.Request{Cmd: "awake", Args: []string{"off"}})
			return
		case req := <-c.cmds:
			c.do(req)
		case <-tick.C:
			if time.Since(lastAwake) > awakeEvery {
				lastAwake = time.Now()
				c.do(control.Request{Cmd: "awake", Args: []string{"on", awakeLease}})
			}
			c.do(control.Request{Cmd: "status"})
			// En sleep se pregunta menos.
			c.mu.Lock()
			want := statusEvery
			if c.status.Sleep {
				want = sleepStatusEvery
			}
			c.mu.Unlock()
			if want != every {
				every = want
				tick.Reset(every)
			}
		}
	}
}

// do ejecuta una petición y guarda el estado que devuelve.
func (c *Client) do(req control.Request) {
	resp, err := control.Call(c.paths.Socket, req)
	if errors.Is(err, control.ErrNotRunning) && !c.quit.Load() {
		// El servicio murió: se vuelve a arrancar (si lo cerraron a
		// propósito, avisó antes y la interfaz se cierra).
		if err2 := c.startDaemon(); err2 == nil {
			resp, err = control.Call(c.paths.Socket, req)
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.err = err
	if err != nil || len(resp.Data) == 0 {
		return
	}
	if req.Cmd == "queue" {
		var q []string
		if json.Unmarshal(resp.Data, &q) == nil {
			c.queue = q
		}
		return
	}
	var st daemon.FullStatus
	if json.Unmarshal(resp.Data, &st) == nil && st.Platform != "" {
		woke := c.status.Sleep && !st.Sleep
		c.status, c.got = st, time.Now()
		if woke && c.OnWake != nil {
			c.OnWake() // después de guardar el estado: el fotograma ya lo ve
		}
	}
}

// Queue devuelve la última cola recibida (Send("queue") la pide).
func (c *Client) Queue() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.queue
}

// Send manda un comando sin esperar.
func (c *Client) Send(cmd string, args ...string) {
	select {
	case c.cmds <- control.Request{Cmd: cmd, Args: args}:
	default:
	}
}

// Status devuelve el último estado conocido, con la posición adelantada por el
// tiempo transcurrido si está sonando, y el último error.
func (c *Client) Status() (daemon.FullStatus, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.status
	if st.State == "playing" && !c.got.IsZero() {
		st.Position = min(st.Position+time.Since(c.got).Seconds(), max(st.Duration, st.Position))
	}
	return st, c.err
}

// StopService para el servicio de audio (y la música), esperando la respuesta.
func (c *Client) StopService() {
	_, _ = control.Call(c.paths.Socket, control.Request{Cmd: "quit"})
}

// Close deja de seguir al servicio y retira el permiso de awake. Al cerrarse la
// conexión de "attach", el servicio para la música, salvo que antes se haya
// llamado a Background.
func (c *Client) Close() {
	close(c.stop)
	c.mu.Lock()
	if c.attachConn != nil {
		c.attachConn.Close()
	}
	c.mu.Unlock()
	select {
	case <-c.done:
	case <-time.After(2 * time.Second):
	}
}
