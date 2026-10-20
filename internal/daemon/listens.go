package daemon

import (
	"context"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/config"
	"github.com/juanvillacortac/audiofilus/internal/history"
	"github.com/juanvillacortac/audiofilus/internal/radio"
	"github.com/juanvillacortac/audiofilus/internal/remote"
	"github.com/juanvillacortac/audiofilus/internal/scrobble"
)

// Las escuchas: el servicio mira cada segundo qué suena y cuánto lleva
// sonando de verdad (sin contar pausas ni saltos). Una canción cuenta como
// escuchada cuando sonó la mitad o cuatro minutos (si dura al menos 30 s,
// la regla de Last.fm y ListenBrainz): entonces va al historial
// (data/history.jsonl, del que salen Recientes, Más escuchadas y las
// estadísticas), a Last.fm y ListenBrainz si hay cuenta, y a su servidor si
// es remota. A los pocos segundos de empezar se manda además el "sonando
// ahora". Las radios no cuentan, ni los episodios de podcast fuera del
// historial.

// listening es la canción que se está siguiendo.
type listening struct {
	path    string
	started time.Time
	played  float64 // segundos sonados de verdad
	lastPos float64
	counted bool // ya se contó
	nowSent bool // ya se mandó el "sonando ahora"
}

// trackListens sigue lo que suena hasta que se cierra el servicio.
func (d *Daemon) trackListens(store *remote.Store, stop <-chan struct{}) {
	q := scrobble.NewQueue(d.paths.Data)
	podcasts := filepath.Join(d.paths.Data, "podcasts") + string(filepath.Separator)
	var cur listening
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	retry := time.Now().Add(time.Minute)
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
		}
		st := d.eng.Status()
		path := st.Track.Path
		if path != cur.path {
			cur = listening{path: path, started: time.Now(), lastPos: st.Position}
		}
		if path == "" || radio.IsRadio(path) || st.State != "playing" {
			cur.lastPos = st.Position
			continue
		}
		// Solo lo que avanzó como sonando (un salto no cuenta).
		if dp := st.Position - cur.lastPos; dp > 0 && dp < 3 {
			cur.played += dp
		}
		cur.lastPos = st.Position
		podcast := strings.HasPrefix(path, podcasts)
		t := scrobble.Track{Title: st.Track.Title, Artist: st.Track.Artist, Album: st.Track.Album, Duration: st.Duration, At: cur.started}
		if !cur.nowSent && cur.played >= 5 && !podcast {
			cur.nowSent = true
			go d.nowPlaying(t)
		}
		need := 240.0
		if st.Duration > 0 {
			need = min(st.Duration/2, 240)
		}
		if cur.counted || cur.played < need || (st.Duration > 0 && st.Duration < 30) {
			if time.Now().After(retry) && q.Pending() > 0 {
				retry = time.Now().Add(2 * time.Minute)
				go d.submit(q, store, "", nil) // lo que esperaba, sin conexión
			}
			continue
		}
		cur.counted = true
		if err := history.Append(d.paths.Data, history.Listen{At: cur.started.Unix(), Path: path,
			Title: t.Title, Artist: t.Artist, Album: t.Album, Duration: st.Duration}); err != nil {
			log.Printf("history: %v", err)
		}
		if !podcast && t.Title != "" && t.Artist != "" {
			go d.submit(q, store, path, &t)
		}
	}
}

// scrobblers son los servicios con cuenta (leídos de audiofilus.json en cada
// uso: la interfaz cambia las cuentas sin reiniciar el servicio).
func (d *Daemon) scrobblers() ([]scrobble.Service, config.Scrobble) {
	cfg, err := config.Load(d.paths)
	if err != nil {
		return nil, config.Scrobble{}
	}
	s := cfg.Scrobble
	var out []scrobble.Service
	if s.ListenBrainzToken != "" {
		out = append(out, &scrobble.ListenBrainz{Token: s.ListenBrainzToken})
	}
	if s.LastFMSession != "" {
		key, secret := s.LastFMKey, s.LastFMSecret
		if key == "" {
			key, secret = scrobble.DefaultLastFMKey, scrobble.DefaultLastFMSecret
		}
		if key != "" {
			out = append(out, &scrobble.LastFM{Key: key, Secret: secret, Session: s.LastFMSession})
		}
	}
	return out, s
}

// nowPlaying manda el "sonando ahora" a los servicios (sin reintentar: es
// efímero).
func (d *Daemon) nowPlaying(t scrobble.Track) {
	svcs, _ := d.scrobblers()
	for _, s := range svcs {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		if err := s.NowPlaying(ctx, t); err != nil {
			log.Printf("now playing on %s: %v", s.Name(), err)
		}
		cancel()
	}
}

// submit manda una escucha (t; nil solo reintenta las que esperaban) a los
// servicios y, si path es remota, a su servidor.
func (d *Daemon) submit(q *scrobble.Queue, store *remote.Store, path string, t *scrobble.Track) {
	svcs, cfg := d.scrobblers()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, err := range q.Submit(ctx, svcs, t) {
		log.Printf("scrobble: %v", err)
	}
	if t != nil && store != nil && remote.IsRemote(path) && !cfg.NoServer {
		if err := store.Scrobble(ctx, path, t.At); err != nil {
			log.Printf("scrobble on the server: %v", err)
		}
	}
}
