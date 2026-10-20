package ui

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/history"
	"github.com/juanvillacortac/audiofilus/internal/remote"
)

// Favoritas, valoraciones, el temporizador, y las listas que salen solas
// (Me gusta, Recientes, Más escuchadas y las estadísticas, en Listas). Las
// marcas se guardan en data/favorites.json (internal/history); las de una
// canción de un servidor se mandan además a él (star, setRating), y al
// cargar su catálogo se suman las suyas. El historial lo escribe el
// servicio (data/history.jsonl) y aquí solo se lee.

// marks son las favoritas y valoraciones (se cargan la primera vez).
func (a *App) marksStore() *history.Marks {
	if a.marks == nil {
		a.marks = history.LoadMarks(a.paths.Data)
	}
	return a.marks
}

// isFavSong dice si la canción path es favorita.
func (a *App) isFavSong(path string) bool {
	return path != "" && a.marksStore().Get(path).Fav
}

// setMark cambia la favorita o la valoración de s (y en su servidor).
func (a *App) setMark(s Song, fav bool, rating int) {
	ms := a.marksStore()
	mk := ms.Get(s.Path)
	favChanged, ratingChanged := mk.Fav != fav, mk.Rating != rating
	mk.Fav, mk.Rating = fav, rating
	mk.Title, mk.Artist, mk.Album = s.Title, s.Artist, s.Album
	if err := ms.Set(s.Path, mk); err != nil {
		a.flash(err.Error())
		return
	}
	if favChanged {
		if fav {
			a.flash(T("fav_on"))
		} else {
			a.flash(T("fav_off"))
		}
	}
	a.histAt = time.Time{} // las listas solas se rehacen
	if remote.IsRemote(s.Path) && a.store != nil {
		store, path := a.store, s.Path
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			var err error
			if favChanged {
				err = store.SetFavorite(ctx, path, fav)
			}
			if ratingChanged && err == nil {
				err = store.SetRating(ctx, path, rating)
			}
			if err != nil {
				a.flash(T("srv_error", "", err))
			}
		}()
	}
}

// markItems son las opciones de favorita y valoración de una canción (para
// los menús de Reproduciendo y de cada canción).
func (a *App) markItems(s Song) []menuItem {
	if s.Path == "" {
		return nil
	}
	return []menuItem{
		{label: T("fav"), icon: "heart",
			do:     func() { mk := a.marksStore().Get(s.Path); a.setMark(s, !mk.Fav, mk.Rating) },
			active: func() bool { return a.isFavSong(s.Path) },
			value:  func() string { return onOffLabel(a.isFavSong(s.Path)) }},
		{label: T("rating"), icon: "star",
			do: func() {
				mk := a.marksStore().Get(s.Path)
				a.setMark(s, mk.Fav, (mk.Rating+1)%6)
			},
			active: func() bool { return a.marksStore().Get(s.Path).Rating > 0 },
			value:  func() string { return ratingLabel(a.marksStore().Get(s.Path).Rating) }},
	}
}

// ratingLabel es una valoración legible: "4/5", o "-" sin valorar.
func ratingLabel(r int) string {
	if r == 0 {
		return "-"
	}
	return fmt.Sprintf("%d/5", r)
}

// timerChoices son los tiempos del temporizador, en minutos (-1: al acabar
// la canción; 0: apagado).
var timerChoices = []int{0, 15, 30, 45, 60, 90, -1}

// timerItem es la opción del temporizador del menú de Reproduciendo: cada
// A pasa al siguiente tiempo.
func (a *App) timerItem() menuItem {
	return menuItem{label: T("timer"), icon: "moon",
		do: func() {
			a.timerChoice = (a.timerChoice + 1) % len(timerChoices)
			switch m := timerChoices[a.timerChoice]; m {
			case 0:
				a.send("timer", "off")
				a.status.TimerLeft, a.status.StopAfter = 0, false
			case -1:
				a.send("timer", "track")
				a.status.TimerLeft, a.status.StopAfter = 0, true
			default:
				a.send("timer", fmt.Sprint(m))
				a.status.TimerLeft, a.status.StopAfter = float64(m*60), false
			}
		},
		active: func() bool { return a.status.TimerLeft > 0 || a.status.StopAfter },
		value:  a.timerLabel}
}

// timerLabel es lo que le queda al temporizador: "29 min", "Fin de la
// canción" o "No".
func (a *App) timerLabel() string {
	switch {
	case a.status.StopAfter:
		return T("timer_track")
	case a.status.TimerLeft > 0:
		return fmt.Sprintf("%d min", int(a.status.TimerLeft/60)+1)
	}
	return onOffLabel(false)
}

// listens es el historial, leído de nuevo solo si cambió.
func (a *App) listens() []history.Listen {
	st, err := os.Stat(history.File(a.paths.Data))
	if err != nil {
		return nil
	}
	if !st.ModTime().Equal(a.histAt) {
		a.hist, a.histAt = history.Load(a.paths.Data), st.ModTime()
	}
	return a.hist
}

// songFor es la canción de path para las listas solas: la de la
// biblioteca (o de su servidor) si está, si no con los datos guardados.
func (a *App) songFor(path, title, artist, album string) Song {
	if a.local != nil {
		if s, ok := a.local.Song(path); ok {
			return s
		}
	}
	for _, r := range a.remotes {
		if r.lib != nil {
			if s, ok := r.lib.Song(path); ok {
				return s
			}
		}
	}
	return Song{Path: path, Title: orDefault(title, path), Artist: artist, Album: album}
}

// autoPlaylists son las listas que salen solas, delante de las demás:
// Me gusta, Recientes, Más escuchadas y Estadísticas (las que tienen algo).
func (a *App) autoPlaylists() []*Playlist {
	var out []*Playlist
	if favs := a.marksStore().Favorites(); len(favs) > 0 {
		pl := &Playlist{Name: T("pl_favs"), Remote: true, icon: "heart"}
		for _, p := range favs {
			mk := a.marksStore().Get(p)
			pl.Songs = append(pl.Songs, a.songFor(p, mk.Title, mk.Artist, mk.Album))
		}
		out = append(out, pl)
	}
	ls := a.listens()
	if len(ls) == 0 {
		return append(out, a.smartPlaylists()...)
	}
	recent := &Playlist{Name: T("pl_recent"), Remote: true, icon: "clock"}
	for _, l := range history.Recent(ls, 100) {
		recent.Songs = append(recent.Songs, a.songFor(l.Path, l.Title, l.Artist, l.Album))
	}
	top := &Playlist{Name: T("pl_top"), Remote: true, icon: "star"}
	stats := history.Compute(ls, time.Time{}, 100)
	for _, c := range stats.TopSongs {
		top.Songs = append(top.Songs, a.songFor(c.Path, c.Name, c.Sub, ""))
	}
	out = append(out, recent, top, &Playlist{Name: T("pl_stats"), Remote: true, icon: "stats", stats: true})
	return append(out, a.smartPlaylists()...)
}

// statRow es una fila de las estadísticas; path, si está, es la canción
// que suena al elegirla.
type statRow struct {
	row  listRow
	path string
}

// statsView es la vista de las estadísticas: los totales y lo más
// escuchado (artistas, discos y canciones), como filas de una lista.
func (a *App) statsView() *view {
	ls := a.listens()
	s := history.Compute(ls, time.Time{}, 10)
	month := history.Compute(ls, time.Now().AddDate(0, -1, 0), 0)
	var rows []statRow
	add := func(icon, title, sub string, path string) {
		rows = append(rows, statRow{listRow{icon, title, sub, false}, path})
	}
	add("stats", N("n_plays", s.Plays), T("stats_hours", int(s.Hours+0.5), s.Since.Format("2006-01-02")), "")
	add("clock", T("stats_month"), N("n_plays", month.Plays), "")
	add("note", N("n_songs", s.Songs), N("n_artists", s.Artists), "")
	section := func(icon, name string, cs []history.Count, songs bool) {
		if len(cs) == 0 {
			return
		}
		add(icon, name, "", "")
		for i, c := range cs {
			sub := N("n_plays", c.Plays)
			if c.Sub != "" {
				sub = c.Sub + " · " + sub
			}
			p := ""
			if songs {
				p = c.Path
			}
			add("", fmt.Sprintf("%d. %s", i+1, c.Name), sub, p)
		}
	}
	section("user", T("stats_artists"), s.TopArtists, false)
	section("disc", T("stats_albums"), s.TopAlbums, false)
	section("note", T("stats_songs"), s.TopSongs, true)
	return &view{kind: vStats, title: T("pl_stats"), stats: rows}
}

// mergeServerMarks suma las favoritas y valoraciones de un catálogo remoto.
func (a *App) mergeServerMarks(server string, songs []remote.Song) {
	var rs []history.Remote
	for _, s := range songs {
		if s.Starred != "" || s.Rating > 0 {
			rs = append(rs, history.Remote{Path: remote.SongPath(server, s.ID), Fav: s.Starred != "", Rating: s.Rating,
				Title: s.Title, Artist: s.Artist, Album: s.Album})
		}
	}
	if len(rs) > 0 {
		ms := a.marksStore()
		go ms.Merge(rs)
	}
}
