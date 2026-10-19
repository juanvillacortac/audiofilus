// Package control es el protocolo local entre el daemon y sus clientes (hoy la
// CLI, luego la interfaz en Ebitengine): una petición y una respuesta JSON por
// conexión, sobre un socket Unix.
package control

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"time"
)

// Request es un comando con sus argumentos, p. ej. {"cmd":"seek","args":["+10"]}.
type Request struct {
	Cmd  string   `json:"cmd"`
	Args []string `json:"args,omitempty"`
}

// Response lleva el resultado; Data depende del comando.
type Response struct {
	OK    bool            `json:"ok"`
	Error string          `json:"error,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
}

// Handler ejecuta una petición y devuelve los datos a codificar, o un error.
type Handler func(Request) (any, error)

// Serve escucha en socket hasta que se cierre el listener.
func Serve(socket string, h Handler) (net.Listener, error) {
	_ = os.Remove(socket) // socket viejo de un cierre inesperado
	ln, err := net.Listen("unix", socket)
	if err != nil {
		return nil, err
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handle(conn, h)
		}
	}()
	return ln, nil
}

// handle atiende una conexión: lee una petición y escribe una respuesta.
func handle(conn net.Conn, h Handler) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	var req Request
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&req); err != nil {
		_ = json.NewEncoder(conn).Encode(Response{Error: "bad request: " + err.Error()})
		return
	}
	if req.Cmd == "attach" {
		attach(conn, h, req)
		return
	}
	resp := Response{OK: true}
	data, err := h(req)
	if err != nil {
		resp = Response{Error: err.Error()}
	} else if data != nil {
		b, err := json.Marshal(data)
		if err != nil {
			resp = Response{Error: err.Error()}
		} else {
			resp.Data = b
		}
	}
	_ = json.NewEncoder(conn).Encode(resp)
}

// attach atiende una conexión que se queda abierta mientras viva el cliente (la
// interfaz). Cuando el cliente se cierra, aunque muera de golpe, el sistema
// cierra el socket y el servicio recibe "detached".
func attach(conn net.Conn, h Handler, req Request) {
	if _, err := h(req); err != nil {
		_ = json.NewEncoder(conn).Encode(Response{Error: err.Error()})
		return
	}
	_ = json.NewEncoder(conn).Encode(Response{OK: true})
	_ = conn.SetDeadline(time.Time{})
	attached.Lock()
	attached.conns[conn] = true
	attached.Unlock()
	_, _ = io.Copy(io.Discard, conn)
	attached.Lock()
	delete(attached.conns, conn)
	attached.Unlock()
	_, _ = h(Request{Cmd: "detached"})
}

// attached son las conexiones de attach abiertas, para avisarles con Notify.
var attached = struct {
	sync.Mutex
	conns map[net.Conn]bool
}{conns: map[net.Conn]bool{}}

// Notify avisa a las interfaces conectadas (por attach) de que el estado
// cambió, con un salto de línea: así preguntan el estado enseguida en vez de
// esperar a su próxima consulta (p. ej. al tocar los botones de volumen de la
// consola, que no pasan por la interfaz).
func Notify() {
	attached.Lock()
	defer attached.Unlock()
	for c := range attached.conns {
		_ = c.SetWriteDeadline(time.Now().Add(100 * time.Millisecond))
		_, _ = c.Write([]byte("\n"))
	}
}

// NotifyQuit avisa a las interfaces conectadas de que el servicio se cierra
// del todo (por "quit": la interfaz, la línea de comandos, la bandeja,
// MPRIS): que se cierren también, en vez de volver a arrancarlo.
func NotifyQuit() {
	attached.Lock()
	defer attached.Unlock()
	for c := range attached.conns {
		_ = c.SetWriteDeadline(time.Now().Add(100 * time.Millisecond))
		_, _ = c.Write([]byte("q"))
	}
}

// Attach abre la conexión que dice "la interfaz sigue viva". Hay que mantenerla
// abierta (y cerrarla al salir).
func Attach(socket string) (net.Conn, error) {
	conn, err := net.DialTimeout("unix", socket, 2*time.Second)
	if err != nil {
		return nil, ErrNotRunning
	}
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if err := json.NewEncoder(conn).Encode(Request{Cmd: "attach"}); err != nil {
		conn.Close()
		return nil, err
	}
	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil || !resp.OK {
		conn.Close()
		return nil, errors.New("attach refused")
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

// ErrNotRunning significa que nadie escucha en el socket.
var ErrNotRunning = errors.New("audiofilus daemon is not running")

// Call envía una petición y espera la respuesta.
func Call(socket string, req Request) (Response, error) {
	conn, err := net.DialTimeout("unix", socket, 2*time.Second)
	if err != nil {
		return Response{}, ErrNotRunning
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return Response{}, err
	}
	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return Response{}, err
	}
	if !resp.OK {
		return resp, errors.New(resp.Error)
	}
	return resp, nil
}
