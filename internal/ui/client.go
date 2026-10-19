package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
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
	if _, err := control.Call(c.paths.Socket, control.Request{Cmd: "status"}); errors.Is(err, control.ErrNotRunning) {
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
			c.mu.Unlock()
			// Bloquea hasta que se cierre. Cada byte que llega es un aviso de que
			// el estado cambió (control.Notify): se pregunta ya.
			buf := make([]byte, 16)
			for {
				if _, err := conn.Read(buf); err != nil {
					break
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

// Background le dice al servicio que siga sonando cuando la interfaz se cierre.
func (c *Client) Background() {
	_, _ = control.Call(c.paths.Socket, control.Request{Cmd: "background"})
}

// daemonPath busca el binario del servicio: junto al de la interfaz (en el
// port de PortMaster, bin/<arquitectura>/, con la configuración una carpeta
// más arriba), en la carpeta de audiofilus o en el PATH.
func (c *Client) daemonPath() (string, error) {
	if exe, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		p := filepath.Join(filepath.Dir(exe), proc.Exe("audiofilus"))
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	p := filepath.Join(c.paths.Base, proc.Exe("audiofilus"))
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}
	if p, err := exec.LookPath("audiofilus"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("audiofilus (the audio service) not found next to the UI in %s", c.paths.Base)
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
	if errors.Is(err, control.ErrNotRunning) {
		// El servicio murió (o lo pararon): se vuelve a arrancar.
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
