package dlna

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// GENA son los eventos de UPnP: un cliente se suscribe a un servicio
// (SUBSCRIBE con su CALLBACK) y el servicio le manda NOTIFY con los cambios.
// Muchos mandos de DLNA (BubbleUPnP, el de Windows) no preguntan por el
// estado: esperan el LastChange del AVTransport para enterarse de que la
// canción empezó o se paró.

// subscriber es una suscripción.
type subscriber struct {
	sid      string
	callback []string // las URL de su CALLBACK
	expires  time.Time
	seq      uint32
}

// events son las suscripciones de un servicio.
type events struct {
	mu      sync.Mutex
	subs    map[string]*subscriber
	initial func() string // las propiedades que se mandan al suscribirse (un propertyset)
	client  *http.Client
}

func newEvents(initial func() string) *events {
	return &events{subs: map[string]*subscriber{}, initial: initial, client: &http.Client{Timeout: 3 * time.Second}}
}

// newSID es un id de suscripción.
func newSID() string { return "uuid:" + newUUID() }

// newUUID es un UUID al azar (versión 4).
func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// timeoutOf lee un TIMEOUT ("Second-1800"; "infinite" o nada: 30 min).
func timeoutOf(h string) time.Duration {
	if s, ok := strings.CutPrefix(strings.ToLower(h), "second-"); ok {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return time.Duration(min(n, 7200)) * time.Second
		}
	}
	return 1800 * time.Second
}

// serve atiende SUBSCRIBE y UNSUBSCRIBE.
func (e *events) serve(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "SUBSCRIBE":
		timeout := timeoutOf(r.Header.Get("TIMEOUT"))
		if sid := r.Header.Get("SID"); sid != "" {
			// Una renovación.
			e.mu.Lock()
			s, ok := e.subs[sid]
			if ok {
				s.expires = time.Now().Add(timeout)
			}
			e.mu.Unlock()
			if !ok {
				http.Error(w, "precondition failed", http.StatusPreconditionFailed)
				return
			}
			w.Header().Set("SID", sid)
			w.Header().Set("TIMEOUT", fmt.Sprintf("Second-%d", int(timeout.Seconds())))
			return
		}
		cb := parseCallback(r.Header.Get("CALLBACK"))
		if len(cb) == 0 || r.Header.Get("NT") != "upnp:event" {
			http.Error(w, "precondition failed", http.StatusPreconditionFailed)
			return
		}
		s := &subscriber{sid: newSID(), callback: cb, expires: time.Now().Add(timeout)}
		e.mu.Lock()
		e.prune()
		e.subs[s.sid] = s
		e.mu.Unlock()
		w.Header().Set("SID", s.sid)
		w.Header().Set("TIMEOUT", fmt.Sprintf("Second-%d", int(timeout.Seconds())))
		w.Header().Set("Server", serverHeader)
		w.WriteHeader(http.StatusOK)
		// El estado inicial, después de la respuesta (el cliente aún no sabe
		// su SID hasta que la recibe).
		go func() {
			time.Sleep(100 * time.Millisecond)
			e.send(s, e.initial())
		}()
	case "UNSUBSCRIBE":
		e.mu.Lock()
		delete(e.subs, r.Header.Get("SID"))
		e.mu.Unlock()
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// parseCallback lee las URL de un CALLBACK ("<http://a/><http://b/>").
func parseCallback(h string) []string {
	var out []string
	for _, p := range strings.Split(h, ">") {
		if i := strings.Index(p, "<"); i >= 0 {
			if u := strings.TrimSpace(p[i+1:]); strings.HasPrefix(u, "http://") {
				out = append(out, u)
			}
		}
	}
	return out
}

// prune olvida las suscripciones caducadas (con e.mu).
func (e *events) prune() {
	for k, s := range e.subs {
		if time.Now().After(s.expires) {
			delete(e.subs, k)
		}
	}
}

// notify manda body (un propertyset) a todos los suscritos.
func (e *events) notify(body string) {
	e.mu.Lock()
	e.prune()
	subs := make([]*subscriber, 0, len(e.subs))
	for _, s := range e.subs {
		subs = append(subs, s)
	}
	e.mu.Unlock()
	for _, s := range subs {
		go e.send(s, body)
	}
}

// send manda un NOTIFY a s (al primer CALLBACK que conteste).
func (e *events) send(s *subscriber, body string) {
	e.mu.Lock()
	seq := s.seq
	s.seq++
	e.mu.Unlock()
	for _, u := range s.callback {
		req, err := http.NewRequest("NOTIFY", u, bytes.NewBufferString(body))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
		req.Header.Set("NT", "upnp:event")
		req.Header.Set("NTS", "upnp:propchange")
		req.Header.Set("SID", s.sid)
		req.Header.Set("SEQ", strconv.FormatUint(uint64(seq), 10))
		resp, err := e.client.Do(req)
		if err != nil {
			continue
		}
		resp.Body.Close()
		return
	}
}

// count es cuántas suscripciones hay.
func (e *events) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.prune()
	return len(e.subs)
}

// propertySet arma un propertyset con estas propiedades (nombre, valor).
func propertySet(props ...[2]string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?><e:propertyset xmlns:e="urn:schemas-upnp-org:event-1-0">`)
	for _, p := range props {
		fmt.Fprintf(&b, "<e:property><%s>%s</%s></e:property>", p[0], xmlEscape(p[1]), p[0])
	}
	b.WriteString("</e:propertyset>")
	return b.String()
}
