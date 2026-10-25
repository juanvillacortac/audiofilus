package mpd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// El cliente: así un audiofilus manda música a otro (o a cualquier MPD) y
// lo controla, y así se prueba el servidor.

// Pair es una línea "clave: valor" de una respuesta.
type Pair struct{ Key, Value string }

// Error es un ACK del servidor.
type Error struct {
	Code    int
	Command string
	Msg     string
}

func (e *Error) Error() string { return fmt.Sprintf("mpd: %s: %s (%d)", e.Command, e.Msg, e.Code) }

// IsNoExist dice si err es un "no existe" de MPD.
func IsNoExist(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == ackNoExist
}

// Client es una conexión a un servidor MPD. Es seguro usarlo desde varias
// goroutines (los comandos van de uno en uno).
type Client struct {
	nc      net.Conn
	r       *bufio.Reader
	mu      sync.Mutex
	Version string // la del protocolo que anunció el servidor
}

// clientTimeout es lo más que se espera una respuesta.
const clientTimeout = 10 * time.Second

// Dial se conecta a addr ("host:puerto") y, si hay, manda la contraseña.
func Dial(addr, password string) (*Client, error) {
	nc, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return nil, err
	}
	c := &Client{nc: nc, r: bufio.NewReader(nc)}
	_ = nc.SetReadDeadline(time.Now().Add(clientTimeout))
	hello, err := c.r.ReadString('\n')
	if err != nil {
		nc.Close()
		return nil, err
	}
	v, ok := strings.CutPrefix(strings.TrimSpace(hello), "OK MPD ")
	if !ok {
		nc.Close()
		return nil, fmt.Errorf("mpd: not an MPD server: %q", strings.TrimSpace(hello))
	}
	c.Version = v
	if password != "" {
		if _, err := c.Cmd("password", password); err != nil {
			nc.Close()
			return nil, err
		}
	}
	return c, nil
}

// Close cierra la conexión.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, _ = io.WriteString(c.nc, "close\n")
	return c.nc.Close()
}

// line escribe un comando con sus argumentos entre comillas.
func line(args []string) string {
	var b strings.Builder
	for i, a := range args {
		if i > 0 {
			b.WriteByte(' ')
			b.WriteString(quote(a))
		} else {
			b.WriteString(a)
		}
	}
	b.WriteByte('\n')
	return b.String()
}

// Cmd manda un comando y devuelve las líneas de la respuesta.
func (c *Client) Cmd(args ...string) ([]Pair, error) {
	pairs, _, err := c.cmd(args)
	return pairs, err
}

// cmd manda un comando y lee la respuesta, con los datos binarios si trae.
func (c *Client) cmd(args []string) ([]Pair, []byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.nc.SetDeadline(time.Now().Add(clientTimeout))
	if _, err := io.WriteString(c.nc, line(args)); err != nil {
		return nil, nil, err
	}
	var pairs []Pair
	var bin []byte
	for {
		l, err := c.r.ReadString('\n')
		if err != nil {
			return nil, nil, err
		}
		l = strings.TrimSuffix(l, "\n")
		if l == "OK" {
			return pairs, bin, nil
		}
		if strings.HasPrefix(l, "ACK ") {
			return nil, nil, parseAck(l)
		}
		k, v, ok := strings.Cut(l, ": ")
		if !ok {
			continue
		}
		if k == "binary" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				return nil, nil, fmt.Errorf("mpd: bad binary size %q", v)
			}
			bin = make([]byte, n+1) // y su salto de línea
			if _, err := io.ReadFull(c.r, bin); err != nil {
				return nil, nil, err
			}
			bin = bin[:n]
		}
		pairs = append(pairs, Pair{k, v})
	}
}

// parseAck lee "ACK [code@n] {cmd} msg".
func parseAck(l string) error {
	e := &Error{Msg: l}
	rest := strings.TrimPrefix(l, "ACK [")
	if codes, after, ok := strings.Cut(rest, "] {"); ok {
		code, _, _ := strings.Cut(codes, "@")
		e.Code, _ = strconv.Atoi(code)
		cmd, msg, _ := strings.Cut(after, "} ")
		e.Command, e.Msg = cmd, msg
	}
	return e
}

// Picture baja la portada de uri (albumart, a trozos; si no hay, la
// incrustada con readpicture). nil si no tiene.
func (c *Client) Picture(uri string) ([]byte, error) {
	for _, cmd := range []string{"albumart", "readpicture"} {
		var data []byte
		for {
			pairs, bin, err := c.cmd([]string{cmd, uri, strconv.Itoa(len(data))})
			if err != nil {
				if IsNoExist(err) {
					break
				}
				return nil, err
			}
			size := -1
			for _, p := range pairs {
				if p.Key == "size" {
					size, _ = strconv.Atoi(p.Value)
				}
			}
			if size <= 0 || len(bin) == 0 {
				break
			}
			data = append(data, bin...)
			if len(data) >= size {
				return data, nil
			}
		}
	}
	return nil, nil
}

// Song es una canción de una respuesta.
type Song struct {
	File     string
	Tags     Tags
	Duration float64
	Pos, ID  int
}

// Songs agrupa las líneas de una respuesta en canciones (cada una empieza
// en "file"); los directorios y listas que vengan se saltan.
func Songs(pairs []Pair) []Song {
	var out []Song
	var cur *Song
	for _, p := range pairs {
		switch p.Key {
		case "file":
			out = append(out, Song{File: p.Value, Pos: -1, ID: -1})
			cur = &out[len(out)-1]
			continue
		case "directory", "playlist":
			cur = nil
			continue
		}
		if cur == nil {
			continue
		}
		switch p.Key {
		case "duration":
			cur.Duration, _ = strconv.ParseFloat(p.Value, 64)
		case "Time":
			if cur.Duration == 0 {
				cur.Duration, _ = strconv.ParseFloat(p.Value, 64)
			}
		case "Pos":
			cur.Pos, _ = strconv.Atoi(p.Value)
		case "Id":
			cur.ID, _ = strconv.Atoi(p.Value)
		default:
			if t := canonTag(p.Key); t != "" {
				cur.Tags.set(t, p.Value)
			}
		}
	}
	return out
}

// Map pasa las líneas a un mapa (la primera de cada clave).
func Map(pairs []Pair) map[string]string {
	m := map[string]string{}
	for _, p := range pairs {
		if _, ok := m[p.Key]; !ok {
			m[p.Key] = p.Value
		}
	}
	return m
}
