package ui

import (
	"fmt"
	"strings"

	"github.com/juanvillacortac/audiofilus/internal/cast"
	"github.com/juanvillacortac/audiofilus/internal/mpd"
)

// La biblioteca de otro audiofilus (o de un mpd): mientras se le manda la
// música, Reproduciendo → Start → "Biblioteca de <aparato>" muestra sus
// álbumes (list album group albumartist) y sus canciones; elegir una pone
// ese álbum en el aparato desde esa canción. Sus canciones van en la cola
// del envío como "mpdlib://<host>/<uri>" y el aparato las toca por su URI
// (cast.Item.RemoteURI): no se sirve nada desde aquí. Al dejar de enviar,
// eso no sigue aquí (no está en este equipo).

// mpdLibScheme es el prefijo de las canciones de la biblioteca de un
// servidor MPD.
const mpdLibScheme = "mpdlib://"

// mpdLibPath es la ruta de la canción uri del servidor host.
func mpdLibPath(host, uri string) string { return mpdLibScheme + host + "/" + uri }

// splitMPDLib separa una ruta de mpdLibPath.
func splitMPDLib(p string) (host, uri string, ok bool) {
	rest, ok := strings.CutPrefix(p, mpdLibScheme)
	if !ok {
		return "", "", false
	}
	return strings.Cut(rest, "/")
}

// mpdRemoteSongs son las canciones vistas de bibliotecas remotas (por
// ruta), para poner sus nombres en Reproduciendo.
type mpdRemoteSongs map[string]mpd.Song

// mpdCastItem prepara una canción de la biblioteca de un servidor MPD.
func (a *App) mpdCastItem(path string) cast.Item {
	_, uri, _ := splitMPDLib(path)
	it := cast.Item{RemoteURI: uri}
	if s, ok := a.mpdSongs[path]; ok {
		it.Title, it.Artist, it.Album, it.Duration = s.Tags.Title, s.Tags.Artist, s.Tags.Album, s.Duration
	}
	if it.Title == "" {
		it.Title = strings.TrimSuffix(uri[strings.LastIndex(uri, "/")+1:], "."+lastExt(uri))
	}
	return it
}

// lastExt es la extensión de uri, sin el punto.
func lastExt(uri string) string {
	if i := strings.LastIndex(uri, "."); i >= 0 && !strings.Contains(uri[i:], "/") {
		return uri[i+1:]
	}
	return ""
}

// mpdQuery se conecta al aparato del envío, hace f y vuelve a la interfaz
// con done (o con el error).
func (a *App) mpdQuery(f func(c *mpd.Client) (any, error), done func(any)) {
	d := a.cast.dev
	a.flash(T("cast_searching_lib"))
	go func() {
		c, err := mpd.Dial(d.Host, d.Password)
		var res any
		if err == nil {
			res, err = f(c)
			c.Close()
		}
		a.acctCh <- func() {
			if err != nil {
				a.flash(T("cast_failed", err))
				return
			}
			done(res)
		}
	}()
}

// mpdAlbum es un álbum de la biblioteca remota.
type mpdAlbum struct{ artist, title string }

// openMPDLibrary abre la lista de álbumes del aparato al que se manda.
func (a *App) openMPDLibrary() {
	from := a.menu
	name, host := a.cast.dev.Name, a.cast.dev.Host
	setMPDPassword(host, a.cast.dev.Password)
	a.mpdQuery(func(c *mpd.Client) (any, error) {
		pairs, err := c.Cmd("list", "album", "group", "albumartist")
		if err != nil {
			return nil, err
		}
		var out []mpdAlbum
		artist := ""
		for _, p := range pairs {
			switch p.Key {
			case "AlbumArtist":
				artist = p.Value
			case "Album":
				out = append(out, mpdAlbum{artist, p.Value})
			}
		}
		return out, nil
	}, func(res any) {
		albums := res.([]mpdAlbum)
		m := &menu{title: T("mpd_library", name), subtitle: N("n_albums", len(albums)), cols: 2, parent: from}
		for _, al := range albums {
			m.items = append(m.items, menuItem{label: al.title, icon: "disc", value: func() string { return al.artist },
				cover: a.mpdAlbumOf(host, al), do: func() { a.openMPDAlbum(al) }})
		}
		if len(albums) == 0 {
			m.items = append(m.items, menuItem{label: T("mpd_empty"), icon: "disc", do: func() {}})
		}
		a.menu = m
	})
}

// mpdAlbumOf es el álbum remoto al como Album, para su portada (el mismo
// cada vez, para que el cargador no la vuelva a pedir).
func (a *App) mpdAlbumOf(host string, al mpdAlbum) *Album {
	key := mpdAlbumCover(host, al)
	if b := a.mpdAlbums[key]; b != nil {
		return b
	}
	if a.mpdAlbums == nil {
		a.mpdAlbums = map[string]*Album{}
	}
	b := &Album{Title: al.title, Artist: al.artist, coverPath: key, stray: true, scaled: map[int]*coverImage{}}
	a.mpdAlbums[key] = b
	return b
}

// openMPDAlbum abre las canciones de un álbum remoto.
func (a *App) openMPDAlbum(al mpdAlbum) {
	from := a.menu
	host := a.cast.dev.Host
	a.mpdQuery(func(c *mpd.Client) (any, error) {
		pairs, err := c.Cmd("find", fmt.Sprintf("((Album == %s) AND (AlbumArtist == %s))", filterQuote(al.title), filterQuote(al.artist)))
		if err == nil && len(pairs) == 0 {
			// Un mpd sin AlbumArtist en sus etiquetas.
			pairs, err = c.Cmd("find", "album", al.title)
		}
		return mpd.Songs(pairs), err
	}, func(res any) {
		songs := res.([]mpd.Song)
		album := a.mpdAlbumOf(host, al)
		var paths []string
		for _, s := range songs {
			p := mpdLibPath(host, s.File)
			a.mpdSongs[p] = s
			a.mpdAlbums[p] = album // Reproduciendo, con su portada
			paths = append(paths, p)
		}
		play := func(i int) {
			a.menu = nil
			a.send("load", append([]string{fmt.Sprint(i)}, paths...)...)
		}
		m := &menu{title: al.title, subtitle: al.artist, cols: 2, parent: from, album: a.mpdAlbumOf(host, al)}
		m.items = append(m.items, menuItem{label: T("mpd_play_all"), icon: "play", close: true, do: func() { play(0) }})
		for i, s := range songs {
			label := s.Tags.Title
			if label == "" {
				label = s.File
			}
			m.items = append(m.items, menuItem{label: label, icon: "note", close: true, do: func() { play(i) }})
		}
		a.menu = m
	})
}

// filterQuote escribe un valor para un filtro de MPD (entre comillas
// simples, con \ de escape).
func filterQuote(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}
