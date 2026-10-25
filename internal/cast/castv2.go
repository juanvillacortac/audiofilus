package cast

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Chromecast: el protocolo CASTV2, por TLS al puerto 8009. Cada mensaje es un
// CastMessage de protobuf (escrito a mano: son cinco campos) precedido de su
// largo en 4 bytes. Se abre una conexión virtual con el receptor, se lanza el
// Default Media Receiver (CC1AD845), se conecta con su transporte y se le
// manda LOAD con la dirección de la canción; luego PLAY, PAUSE, SEEK... con el
// mediaSessionId que devuelve. Un PING cada 5 s mantiene viva la conexión.

const (
	nsConnection = "urn:x-cast:com.google.cast.tp.connection"
	nsHeartbeat  = "urn:x-cast:com.google.cast.tp.heartbeat"
	nsReceiver   = "urn:x-cast:com.google.cast.receiver"
	nsMedia      = "urn:x-cast:com.google.cast.media"

	defaultMediaReceiver = "CC1AD845"
	senderID             = "sender-0"
	receiverID           = "receiver-0"
)

// castMsg es un CastMessage (solo con carga de texto, la que se usa).
type castMsg struct {
	Source, Dest, Namespace, Payload string
}

// Los campos del CastMessage: protocol_version (1, varint), source_id (2),
// destination_id (3), namespace (4), payload_type (5, varint),
// payload_utf8 (6).
func encodeCastMessage(m castMsg) []byte {
	var b []byte
	b = appendVarintField(b, 1, 0) // CASTV2_1_0
	b = appendStringField(b, 2, m.Source)
	b = appendStringField(b, 3, m.Dest)
	b = appendStringField(b, 4, m.Namespace)
	b = appendVarintField(b, 5, 0) // STRING
	b = appendStringField(b, 6, m.Payload)
	return b
}

func appendVarintField(b []byte, field int, v uint64) []byte {
	b = binary.AppendUvarint(b, uint64(field<<3|0))
	return binary.AppendUvarint(b, v)
}

func appendStringField(b []byte, field int, s string) []byte {
	b = binary.AppendUvarint(b, uint64(field<<3|2))
	b = binary.AppendUvarint(b, uint64(len(s)))
	return append(b, s...)
}

var errProto = errors.New("cast: bad cast message")

// decodeCastMessage lee un CastMessage (ignorando los campos que no usa).
func decodeCastMessage(b []byte) (castMsg, error) {
	var m castMsg
	for len(b) > 0 {
		key, n := binary.Uvarint(b)
		if n <= 0 {
			return m, errProto
		}
		b = b[n:]
		field, wire := int(key>>3), key&7
		switch wire {
		case 0:
			_, n := binary.Uvarint(b)
			if n <= 0 {
				return m, errProto
			}
			b = b[n:]
		case 2:
			l, n := binary.Uvarint(b)
			if n <= 0 || uint64(len(b)-n) < l {
				return m, errProto
			}
			v := string(b[n : n+int(l)])
			b = b[n+int(l):]
			switch field {
			case 2:
				m.Source = v
			case 3:
				m.Dest = v
			case 4:
				m.Namespace = v
			case 6:
				m.Payload = v
			}
		case 5:
			if len(b) < 4 {
				return m, errProto
			}
			b = b[4:]
		case 1:
			if len(b) < 8 {
				return m, errProto
			}
			b = b[8:]
		default:
			return m, errProto
		}
	}
	return m, nil
}

// writeFrame escribe un mensaje con su largo delante.
func writeFrame(w io.Writer, m castMsg) error {
	body := encodeCastMessage(m)
	frame := binary.BigEndian.AppendUint32(nil, uint32(len(body)))
	_, err := w.Write(append(frame, body...))
	return err
}

// readFrame lee un mensaje.
func readFrame(r io.Reader) (castMsg, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return castMsg{}, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n > 1<<20 {
		return castMsg{}, errProto
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return castMsg{}, err
	}
	return decodeCastMessage(body)
}

// castPlayer controla un Chromecast.
type castPlayer struct {
	dev Device

	mu        sync.Mutex
	conn      net.Conn
	done      chan struct{}
	waiters   map[int]chan map[string]any
	transport string // el destino de la app lanzada
	session   int    // mediaSessionId
	media     map[string]any
	volume    float64
	reqID     atomic.Int64
	dial      func(ctx context.Context) (net.Conn, error) // los tests la cambian
}

func newCastPlayer(d Device) *castPlayer {
	p := &castPlayer{dev: d, volume: -1}
	p.dial = func(ctx context.Context) (net.Conn, error) {
		dl := &tls.Dialer{NetDialer: &net.Dialer{Timeout: 5 * time.Second}, Config: &tls.Config{InsecureSkipVerify: true}}
		return dl.DialContext(ctx, "tcp", d.Host)
	}
	return p
}

// ensure abre la conexión si no está (o se cayó).
func (p *castPlayer) ensure(ctx context.Context) error {
	p.mu.Lock()
	if p.conn != nil {
		select {
		case <-p.done:
			p.conn, p.transport, p.session = nil, "", 0 // se cayó: de nuevo
		default:
			p.mu.Unlock()
			return nil
		}
	}
	p.mu.Unlock()
	conn, err := p.dial(ctx)
	if err != nil {
		return err
	}
	done := make(chan struct{})
	p.mu.Lock()
	p.conn, p.done, p.waiters = conn, done, map[int]chan map[string]any{}
	p.mu.Unlock()
	go p.read(conn, done)
	go p.heartbeat(conn, done)
	return p.send(nsConnection, receiverID, map[string]any{"type": "CONNECT"})
}

// send manda un mensaje JSON a dest.
func (p *castPlayer) send(ns, dest string, payload map[string]any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	p.mu.Lock()
	conn := p.conn
	p.mu.Unlock()
	if conn == nil {
		return errors.New("cast: not connected")
	}
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return writeFrame(conn, castMsg{Source: senderID, Dest: dest, Namespace: ns, Payload: string(b)})
}

// request manda un mensaje con requestId y espera su respuesta.
func (p *castPlayer) request(ctx context.Context, ns, dest string, payload map[string]any) (map[string]any, error) {
	id := int(p.reqID.Add(1))
	payload["requestId"] = id
	ch := make(chan map[string]any, 1)
	p.mu.Lock()
	if p.waiters == nil {
		p.mu.Unlock()
		return nil, errors.New("cast: not connected")
	}
	p.waiters[id] = ch
	done := p.done
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.waiters, id)
		p.mu.Unlock()
	}()
	if err := p.send(ns, dest, payload); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	select {
	case r := <-ch:
		switch r["type"] {
		case "LOAD_FAILED", "LOAD_CANCELLED", "INVALID_REQUEST", "LAUNCH_ERROR":
			return r, fmt.Errorf("cast: %v (%v)", r["type"], r["reason"])
		}
		return r, nil
	case <-done:
		return nil, errors.New("cast: connection closed")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// read atiende lo que llega: PING, respuestas y estados.
func (p *castPlayer) read(conn net.Conn, done chan struct{}) {
	defer close(done)
	defer conn.Close()
	for {
		_ = conn.SetReadDeadline(time.Now().Add(20 * time.Second))
		m, err := readFrame(conn)
		if err != nil {
			return
		}
		var pl map[string]any
		if json.Unmarshal([]byte(m.Payload), &pl) != nil {
			continue
		}
		switch pl["type"] {
		case "PING":
			_ = p.send(nsHeartbeat, m.Source, map[string]any{"type": "PONG"})
			continue
		case "CLOSE":
			if m.Source != receiverID {
				p.mu.Lock()
				p.transport, p.session = "", 0 // la app se cerró
				p.mu.Unlock()
			}
		case "MEDIA_STATUS":
			p.noteMedia(pl)
		case "RECEIVER_STATUS":
			p.noteReceiver(pl)
		}
		if id, ok := pl["requestId"].(float64); ok && id > 0 {
			p.mu.Lock()
			ch := p.waiters[int(id)]
			p.mu.Unlock()
			if ch != nil {
				select {
				case ch <- pl:
				default:
				}
			}
		}
	}
}

// heartbeat manda PING cada 5 s.
func (p *castPlayer) heartbeat(conn net.Conn, done chan struct{}) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
			if p.send(nsHeartbeat, receiverID, map[string]any{"type": "PING"}) != nil {
				conn.Close()
				return
			}
		}
	}
}

// noteMedia guarda el último estado de la canción.
func (p *castPlayer) noteMedia(pl map[string]any) {
	list, _ := pl["status"].([]any)
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(list) == 0 {
		p.media = nil
		return
	}
	st, _ := list[0].(map[string]any)
	if id, ok := st["mediaSessionId"].(float64); ok {
		p.session = int(id)
	}
	p.media = st
}

// noteReceiver guarda el volumen y el transporte de la app.
func (p *castPlayer) noteReceiver(pl map[string]any) {
	st, _ := pl["status"].(map[string]any)
	p.mu.Lock()
	defer p.mu.Unlock()
	if vol, ok := st["volume"].(map[string]any); ok {
		if l, ok := vol["level"].(float64); ok {
			p.volume = l
		}
	}
	apps, _ := st["applications"].([]any)
	for _, a := range apps {
		app, _ := a.(map[string]any)
		if app["appId"] == defaultMediaReceiver {
			if t, ok := app["transportId"].(string); ok {
				p.transport = t
			}
		}
	}
}

// launch abre el Default Media Receiver (si no estaba) y se conecta a él.
func (p *castPlayer) launch(ctx context.Context) (string, error) {
	p.mu.Lock()
	tr := p.transport
	p.mu.Unlock()
	if tr == "" {
		if _, err := p.request(ctx, nsReceiver, receiverID, map[string]any{"type": "LAUNCH", "appId": defaultMediaReceiver}); err != nil {
			return "", err
		}
		p.mu.Lock()
		tr = p.transport
		p.mu.Unlock()
		if tr == "" {
			return "", errors.New("cast: the media receiver didn't start")
		}
	}
	if err := p.send(nsConnection, tr, map[string]any{"type": "CONNECT"}); err != nil {
		return "", err
	}
	return tr, nil
}

func (p *castPlayer) Load(ctx context.Context, m media, start float64) error {
	if err := p.ensure(ctx); err != nil {
		return err
	}
	tr, err := p.launch(ctx)
	if err != nil {
		return err
	}
	meta := map[string]any{"metadataType": 3, "title": orStr(m.Title, "Audiofilus")}
	if m.Artist != "" {
		meta["artist"] = m.Artist
	}
	if m.Album != "" {
		meta["albumName"] = m.Album
	}
	if m.Cover != "" {
		meta["images"] = []any{map[string]any{"url": m.Cover}}
	}
	med := map[string]any{"contentId": m.URL, "contentType": orStr(m.MIME, "audio/mpeg"), "streamType": "BUFFERED", "metadata": meta}
	if m.Duration > 0 {
		med["duration"] = m.Duration
	}
	_, err = p.request(ctx, nsMedia, tr, map[string]any{"type": "LOAD", "media": med, "autoplay": true, "currentTime": start})
	return err
}

// mediaCmd manda una orden a la canción cargada.
func (p *castPlayer) mediaCmd(ctx context.Context, typ string, extra map[string]any) error {
	if err := p.ensure(ctx); err != nil {
		return err
	}
	p.mu.Lock()
	tr, sess := p.transport, p.session
	p.mu.Unlock()
	if tr == "" || sess == 0 {
		return errors.New("cast: nothing loaded")
	}
	payload := map[string]any{"type": typ, "mediaSessionId": sess}
	for k, v := range extra {
		payload[k] = v
	}
	_, err := p.request(ctx, nsMedia, tr, payload)
	return err
}

func (p *castPlayer) Play(ctx context.Context) error  { return p.mediaCmd(ctx, "PLAY", nil) }
func (p *castPlayer) Pause(ctx context.Context) error { return p.mediaCmd(ctx, "PAUSE", nil) }
func (p *castPlayer) Stop(ctx context.Context) error  { return p.mediaCmd(ctx, "STOP", nil) }
func (p *castPlayer) Seek(ctx context.Context, sec float64) error {
	return p.mediaCmd(ctx, "SEEK", map[string]any{"currentTime": sec})
}

func (p *castPlayer) SetVolume(ctx context.Context, v float64) error {
	if err := p.ensure(ctx); err != nil {
		return err
	}
	v = max(0, min(1, v))
	_, err := p.request(ctx, nsReceiver, receiverID, map[string]any{"type": "SET_VOLUME", "volume": map[string]any{"level": v}})
	return err
}

// SetNext no se usa en Chromecast: la sesión pone la siguiente al terminar.
func (p *castPlayer) SetNext(context.Context, media) error { return ErrUnsupported }

func (p *castPlayer) Status(ctx context.Context) (Status, error) {
	if err := p.ensure(ctx); err != nil {
		return Status{}, err
	}
	p.mu.Lock()
	tr := p.transport
	p.mu.Unlock()
	if tr != "" {
		_, _ = p.request(ctx, nsMedia, tr, map[string]any{"type": "GET_STATUS"})
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	st := Status{State: Stopped, Volume: p.volume}
	if p.media == nil {
		return st, nil
	}
	switch p.media["playerState"] {
	case "PLAYING":
		st.State = Playing
	case "PAUSED":
		st.State = Paused
	case "BUFFERING", "LOADING":
		st.State = Loading
	}
	st.Position, _ = p.media["currentTime"].(float64)
	if med, ok := p.media["media"].(map[string]any); ok {
		st.Duration, _ = med["duration"].(float64)
	}
	return st, nil
}

func (p *castPlayer) Close() error {
	p.mu.Lock()
	conn, tr := p.conn, p.transport
	p.mu.Unlock()
	if conn == nil {
		return nil
	}
	if tr != "" {
		_ = p.send(nsConnection, tr, map[string]any{"type": "CLOSE"})
	}
	_ = p.send(nsConnection, receiverID, map[string]any{"type": "CLOSE"})
	return conn.Close()
}
