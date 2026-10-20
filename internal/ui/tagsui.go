package ui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // las portadas que llegan (para saber su tamaño)
	_ "image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/juanvillacortac/audiofilus/internal/decode"
	"github.com/juanvillacortac/audiofilus/internal/library"
	"github.com/juanvillacortac/audiofilus/internal/loudness"
	"github.com/juanvillacortac/audiofilus/internal/musicbrainz"
	"github.com/juanvillacortac/audiofilus/internal/tagwrite"
	"github.com/juanvillacortac/audiofilus/internal/ytm"
)

// Las herramientas de las etiquetas, en el menú (Start) de una canción, un
// álbum o un artista de la biblioteca de la tarjeta (no en los de un
// servidor, la radio o YouTube Music):
//
//   - Editar etiquetas: de una canción, todos sus campos; de un álbum, los
//     que comparten sus canciones (álbum, artista del álbum, género, año).
//   - Buscar portada: en MusicBrainz (Cover Art Archive) y, si no hay, en
//     iTunes; se guarda como cover.jpg en la carpeta del álbum, dentro de
//     los archivos, o las dos cosas.
//   - Buscar en MusicBrainz: la edición del álbum; se emparejan sus pistas
//     con las canciones y se escriben las etiquetas de las que casan.
//   - Calcular ReplayGain: la sonoridad EBU R128 de cada canción (y la del
//     álbum), en las etiquetas; el servicio la lee al poner la canción.
//
// Lo largo (escribir, medir) va como un trabajo más de Descargas, con su
// progreso; lo que busca en la red, en segundo plano, y lo que vuelve se
// atiende en el hilo de la interfaz (pollMeta). Tras escribir, la
// biblioteca se pone al día sola: solo se releen los archivos cambiados.

// metaState es lo de las herramientas de las etiquetas.
type metaState struct {
	done chan func() // lo que vuelve de las goroutines, para el hilo de la interfaz
	mb   *musicbrainz.Client
}

// metaInit prepara el estado (la primera vez).
func (a *App) metaInit() {
	if a.meta.done == nil {
		a.meta.done = make(chan func(), 8)
		a.meta.mb = musicbrainz.New()
	}
}

// metaLater hace f en el hilo de la interfaz (desde una goroutine).
func (a *App) metaLater(f func()) {
	a.metaInit()
	a.meta.done <- f
	ebiten.ScheduleFrame()
}

// pollMeta atiende lo que volvió de las goroutines (va en cada Update).
func (a *App) pollMeta() {
	if a.meta.done == nil {
		return
	}
	for {
		select {
		case f := <-a.meta.done:
			f()
		default:
			return
		}
	}
}

// localTaggable son las canciones de songs que se pueden etiquetar: archivos
// de la tarjeta (no pistas de un CUE ni música de consola) de un formato
// que tagwrite sabe escribir.
func localTaggable(songs []Song) []Song {
	var out []Song
	for _, s := range songs {
		if decode.IsVirtualPath(s.Path) || strings.Contains(s.Path, "://") || !tagwrite.Supported(s.Path) {
			continue
		}
		out = append(out, s)
	}
	return out
}

// addMetaItems añade las herramientas al menú del elemento elegido (lo
// llama openItemMenu al terminar).
func (a *App) addMetaItems(v *view) {
	if a.menu == nil || a.source != 0 || a.mobile() || v.count() == 0 {
		return
	}
	var song []Song // una canción
	var al *Album   // un álbum
	var all []Song  // todas (para ReplayGain)
	switch v.kind {
	case vAlbums:
		al = v.albums[v.sel]
		all = al.Songs
	case vArtists:
		all = v.artists[v.sel].AllSongs()
	case vAlbum, vSongs, vPlaylist, vFolder:
		songs := a.itemSongs(v)
		if len(songs) != 1 {
			all = songs
			break
		}
		song, all = songs, songs
		if v.kind == vAlbum {
			al = v.album
		}
	default:
		return
	}
	var items []menuItem
	if s := localTaggable(song); len(s) == 1 {
		items = append(items, menuItem{label: T("tg_edit"), icon: "pencil", do: func() { a.openTagEditor(s, false) }})
	}
	if al != nil {
		if s := localTaggable(al.Songs); len(s) > 0 {
			items = append(items,
				menuItem{label: T("tg_edit_album"), icon: "pencil", do: func() { a.openTagEditor(s, true) }},
				menuItem{label: T("tg_cover"), icon: "disc", do: func() { a.findCover(al, s) }},
				menuItem{label: T("tg_mb"), icon: "search", do: func() { a.mbLookup(al, s) }})
		}
	}
	if s := localTaggable(all); len(s) > 0 {
		items = append(items,
			menuItem{label: T("tg_rg"), icon: "speaker", do: func() { a.computeGain(s, al != nil || v.kind == vArtists, false) }},
			menuItem{label: T("tg_rg_again"), icon: "speaker", do: func() { a.computeGain(s, al != nil || v.kind == vArtists, true) }})
	}
	a.menu.items = append(a.menu.items, items...)
}

// metaJob pone un trabajo largo en Descargas (con su progreso); after se
// hace al terminar bien, en el hilo de la interfaz.
func (a *App) metaJob(title, sub string, run func(ctx context.Context, progress func(done, total int64)) error, after func()) {
	j := &ytJob{track: ytm.Track{ID: "meta:" + title + ":" + time.Now().String(), Title: title, Artists: []string{sub}}, podcast: true, state: jobWaiting}
	j.run = func(ctx context.Context, progress func(ytm.Progress)) (string, error) {
		progress(ytm.Progress{Stage: "resolve"})
		err := run(ctx, func(done, total int64) {
			progress(ytm.Progress{Stage: "download", Done: done, Total: total})
		})
		return "", err
	}
	j.after = func(string) {
		// El trabajo avisa "listo" al terminar: lo de after va después, para
		// que se vea su propio aviso.
		a.metaLater(func() {
			if a.yt.bgScan == nil {
				a.yt.bgScan = StartScan(a.paths, a.cfg.MusicDirs) // relee solo lo cambiado
			}
			if after != nil {
				after()
			}
		})
	}
	a.addJob(j)
	a.yt.queue <- j
	a.flash(T("ytm_queued", title))
}

// Los campos que se editan, en orden: los de una canción y los de un álbum.
var (
	tagFieldsSong  = []string{"title", "artist", "album", "albumartist", "genre", "year", "track", "disc"}
	tagFieldsAlbum = []string{"album", "albumartist", "genre", "year"}
)

// tagFieldName es el nombre de un campo en la interfaz.
func tagFieldName(f string) string {
	if f == "disc" {
		return T("tg_disc")
	}
	return T("smf_" + f)
}

// tagEdit es lo que se está editando.
type tagEdit struct {
	songs  []Song
	album  bool
	fields []string
	orig   map[string]string
	vals   map[string]string
}

// tagValues son los campos de unas etiquetas como texto.
func tagValues(t tagwrite.Tags) map[string]string {
	num := func(n int) string {
		if n <= 0 {
			return ""
		}
		return strconv.Itoa(n)
	}
	return map[string]string{"title": t.Title, "artist": t.Artist, "album": t.Album, "albumartist": t.AlbumArtist,
		"genre": t.Genre, "year": num(t.Year), "track": num(t.Track), "disc": num(t.Disc)}
}

// openTagEditor abre el editor: los campos con su valor (los de la primera
// canción, en un álbum); elegir uno pide el nuevo.
func (a *App) openTagEditor(songs []Song, album bool) {
	t, err := tagwrite.Read(songs[0].Path)
	if err != nil && !errors.Is(err, tagwrite.ErrUnsupported) {
		a.flash(T("tg_failed", err))
		return
	}
	ed := &tagEdit{songs: songs, album: album, fields: tagFieldsSong, orig: tagValues(t), vals: tagValues(t)}
	if album {
		ed.fields = tagFieldsAlbum
	}
	a.tagMenu(ed)
}

// tagMenu es el menú del editor.
func (a *App) tagMenu(ed *tagEdit) {
	var items []menuItem
	for _, f := range ed.fields {
		items = append(items, menuItem{label: tagFieldName(f), icon: "pencil",
			value: func() string { return orDefault(ed.vals[f], "-") },
			do: func() {
				a.openRawPrompt(tagFieldName(f), ed.vals[f], false, func(v string) {
					ed.vals[f] = strings.TrimSpace(v)
					a.tagMenu(ed)
				})
			}})
	}
	items = append(items, menuItem{label: T("tg_save"), icon: "check", close: true, do: func() { a.saveTags(ed) }})
	title := ed.songs[0].Title
	if ed.album {
		title = ed.orig["album"]
	}
	a.menu = &menu{title: T("tg_edit"), subtitle: title, items: items}
}

// saveTags escribe lo cambiado en cada canción del editor.
func (a *App) saveTags(ed *tagEdit) {
	var t tagwrite.Tags
	changed := false
	for _, f := range ed.fields {
		v := ed.vals[f]
		if v == ed.orig[f] {
			continue
		}
		changed = true
		if v == "" {
			t.Clear = append(t.Clear, f)
			continue
		}
		n, _ := strconv.Atoi(v)
		switch f {
		case "title":
			t.Title = v
		case "artist":
			t.Artist = v
		case "album":
			t.Album = v
		case "albumartist":
			t.AlbumArtist = v
		case "genre":
			t.Genre = v
		case "year":
			t.Year = n
		case "track":
			t.Track = n
		case "disc":
			t.Disc = n
		}
	}
	if !changed {
		return
	}
	songs := ed.songs
	a.metaJob(T("tg_job_tags"), orDefault(ed.orig["album"], songs[0].Title), func(ctx context.Context, progress func(done, total int64)) error {
		return writeEach(ctx, songs, progress, func(Song) tagwrite.Tags { return t })
	}, func() { a.flash(T("tg_saved", len(songs))) })
}

// writeEach escribe en cada canción las etiquetas que le toquen, con el
// progreso por canciones. Sigue con las demás si una falla y devuelve el
// primer error.
func writeEach(ctx context.Context, songs []Song, progress func(done, total int64), tags func(Song) tagwrite.Tags) error {
	var first error
	for i, s := range songs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := tagwrite.Write(s.Path, tags(s)); err != nil && first == nil {
			first = fmt.Errorf("%s: %w", filepath.Base(s.Path), err)
		}
		progress(int64(i+1), int64(len(songs)))
	}
	return first
}

// coverFound es una portada encontrada.
type coverFound struct {
	data   []byte
	mime   string
	source string
	w, h   int
}

// findCover busca la portada de un álbum (MusicBrainz y, si no, iTunes) y
// ofrece dónde guardarla.
func (a *App) findCover(al *Album, songs []Song) {
	a.metaInit()
	mb := a.meta.mb
	artist, album := al.Artist, al.Title
	a.flash(T("tk_searching"))
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		var c coverFound
		var err error
		if rels, serr := mb.SearchRelease(ctx, artist, album, len(al.Songs)); serr == nil && len(rels) > 0 {
			c.data, c.mime, err = mb.FrontCover(ctx, rels[0].ID, rels[0].GroupID, 1200)
			c.source = "MusicBrainz"
		}
		if len(c.data) == 0 {
			c.data, c.mime, err = mb.ITunesCover(ctx, artist, album, 1200)
			c.source = "iTunes"
		}
		if err == nil {
			if cfg, _, derr := image.DecodeConfig(bytes.NewReader(c.data)); derr == nil {
				c.w, c.h = cfg.Width, cfg.Height
			}
		}
		a.metaLater(func() {
			if err != nil || len(c.data) == 0 {
				a.flash(T("tg_cover_none"))
				return
			}
			a.coverMenu(al, songs, c)
		})
	}()
}

// coverMenu ofrece dónde guardar la portada encontrada.
func (a *App) coverMenu(al *Album, songs []Song, c coverFound) {
	dir := filepath.Dir(songs[0].Path)
	folder := func() error { return os.WriteFile(filepath.Join(dir, "cover"+coverExt(c.mime)), c.data, 0o644) }
	embed := func(ctx context.Context, progress func(done, total int64)) error {
		cover := &tagwrite.Cover{MIME: c.mime, Data: c.data}
		return writeEach(ctx, songs, progress, func(Song) tagwrite.Tags { return tagwrite.Tags{Cover: cover} })
	}
	done := func() { a.flash(T("tg_cover_saved", al.Title)) }
	a.menu = &menu{title: T("tg_cover_found", c.source), subtitle: fmt.Sprintf("%s · %d×%d · %d KB", al.Title, c.w, c.h, len(c.data)/1024), album: al,
		items: []menuItem{
			{label: T("tg_cover_folder"), icon: "folder", do: func() {
				if err := folder(); err != nil {
					a.flash(T("tg_failed", err))
					return
				}
				a.yt.bgScan = StartScan(a.paths, a.cfg.MusicDirs)
				done()
			}},
			{label: T("tg_cover_embed"), icon: "disc", do: func() { a.metaJob(T("tg_job_cover"), al.Title, embed, done) }},
			{label: T("tg_cover_both"), icon: "check", do: func() {
				if err := folder(); err != nil {
					a.flash(T("tg_failed", err))
					return
				}
				a.metaJob(T("tg_job_cover"), al.Title, embed, done)
			}},
		}}
}

// coverExt es la extensión de una imagen por su tipo.
func coverExt(mime string) string {
	if strings.Contains(mime, "png") {
		return ".png"
	}
	return ".jpg"
}

// folderCover busca una portada suelta en la carpeta de un álbum
// (cover.jpg, folder.jpg...): la de los álbumes sin portada en los archivos.
func folderCover(dir string) string {
	for _, n := range []string{"cover", "folder", "front", "Cover", "Folder", "Front", "album", "AlbumArt"} {
		for _, ext := range []string{".jpg", ".jpeg", ".png"} {
			p := filepath.Join(dir, n+ext)
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}

// mbLookup busca el álbum en MusicBrainz y ofrece las ediciones encontradas.
func (a *App) mbLookup(al *Album, songs []Song) {
	a.metaInit()
	mb := a.meta.mb
	artist, album, n := al.Artist, al.Title, len(al.Songs)
	a.flash(T("tk_searching"))
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		rels, err := mb.SearchRelease(ctx, artist, album, n)
		a.metaLater(func() {
			if err != nil || len(rels) == 0 {
				a.flash(T("tg_mb_none"))
				return
			}
			var items []menuItem
			for _, r := range rels[:min(len(rels), 10)] {
				parts := []string{}
				if r.Year > 0 {
					parts = append(parts, strconv.Itoa(r.Year))
				}
				if r.Country != "" {
					parts = append(parts, r.Country)
				}
				parts = append(parts, T("tg_tracks", r.TrackCount))
				if r.Label != "" {
					parts = append(parts, r.Label)
				}
				items = append(items, menuItem{label: r.Title + " · " + strings.Join(parts, " · "), icon: "disc", do: func() { a.mbRelease(al, songs, r.ID) }})
			}
			a.menu = &menu{title: T("tg_mb"), subtitle: artist + " · " + album, album: al, items: items}
		})
	}()
}

// mbRelease lee una edición, la empareja con las canciones y ofrece
// aplicarla.
func (a *App) mbRelease(al *Album, songs []Song, id string) {
	mb := a.meta.mb
	a.flash(T("tk_searching"))
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		rel, err := mb.GetRelease(ctx, id)
		if err != nil {
			a.metaLater(func() { a.flash(T("tg_failed", err)) })
			return
		}
		local := make([]musicbrainz.LocalTrack, len(songs))
		for i, s := range songs {
			lt := musicbrainz.LocalTrack{Path: s.Path, Title: s.Title, Number: s.Track % 1000, Disc: s.Track / 1000}
			if src, err := decode.Open(s.Path); err == nil {
				if r := src.SampleRate(); r > 0 && src.Length() > 0 {
					lt.Duration = float64(src.Length()) / float64(r)
				}
				src.Close()
			}
			local[i] = lt
		}
		matches, conf := musicbrainz.MatchAlbum(local, rel)
		a.metaLater(func() { a.mbApplyMenu(al, songs, rel, matches, conf) })
	}()
}

// mbApplyMenu dice cuántas canciones casan y ofrece escribirlas (con la
// portada o sin ella).
func (a *App) mbApplyMenu(al *Album, songs []Song, rel musicbrainz.Release, matches []musicbrainz.TrackMatch, conf float64) {
	byPath := map[string]*musicbrainz.Track{}
	n := 0
	for _, m := range matches {
		if m.Track != nil {
			byPath[songs[m.Local].Path] = m.Track
			n++
		}
	}
	if n == 0 {
		a.flash(T("tg_mb_none"))
		return
	}
	genre := ""
	if len(rel.Genres) > 0 {
		genre = rel.Genres[0]
	}
	discs := len(rel.Media)
	apply := func(withCover bool) {
		mb := a.meta.mb
		a.metaJob(T("tg_job_mb"), rel.Title, func(ctx context.Context, progress func(done, total int64)) error {
			var cover *tagwrite.Cover
			if withCover {
				if data, mime, err := mb.FrontCover(ctx, rel.ID, rel.GroupID, 1200); err == nil {
					cover = &tagwrite.Cover{MIME: mime, Data: data}
				}
			}
			var todo []Song
			for _, s := range songs {
				if byPath[s.Path] != nil {
					todo = append(todo, s)
				}
			}
			return writeEach(ctx, todo, progress, func(s Song) tagwrite.Tags {
				tr := byPath[s.Path]
				t := tagwrite.Tags{Title: tr.Title, Artist: orDefault(tr.Artist, rel.Artist), Album: rel.Title,
					AlbumArtist: rel.Artist, Genre: genre, Year: rel.Year, Track: tr.Number, Cover: cover}
				if discs > 1 {
					t.Disc, t.DiscTotal = tr.Disc, discs
				}
				return t
			})
		}, func() { a.flash(T("tg_saved", n)) })
	}
	a.menu = &menu{title: rel.Title, subtitle: T("tg_mb_match", n, len(songs), int(conf*100+0.5)), album: al, items: []menuItem{
		{label: T("tg_apply"), icon: "check", do: func() { apply(false) }},
		{label: T("tg_apply_cover"), icon: "disc", do: func() { apply(true) }},
	}}
}

// computeGain mide la sonoridad (EBU R128) de las canciones y escribe su
// ReplayGain; con album, también la del álbum (por álbumes, si son las de un
// artista). Sin again, salta las que ya lo tienen.
func (a *App) computeGain(songs []Song, album, again bool) {
	if !again {
		var todo []Song
		for _, s := range songs {
			if g, ok := library.ReadGain(s.Path); !ok || (album && !g.HasAlbum) {
				todo = append(todo, s)
			}
		}
		if len(todo) == 0 {
			a.flash(T("tg_rg_have"))
			return
		}
		// Un álbum se mide entero (la ganancia del álbum es de todas).
		if !album {
			songs = todo
		}
	}
	// Por álbumes, en el orden en que llegan.
	var groups [][]Song
	idx := map[*Album]int{}
	for _, s := range songs {
		al, _ := a.lib.AlbumOf(s.Path)
		if !album || al == nil {
			groups = append(groups, []Song{s})
			continue
		}
		i, ok := idx[al]
		if !ok {
			i = len(groups)
			idx[al] = i
			groups = append(groups, nil)
		}
		groups[i] = append(groups[i], s)
	}
	total := int64(len(songs)) * 1000
	a.metaJob(T("tg_job_rg"), N("n_songs", len(songs)), func(ctx context.Context, progress func(done, total int64)) error {
		var done int64
		var first error
		for _, g := range groups {
			res := make([]*loudness.Analyzer, len(g))
			for i, s := range g {
				an, err := loudness.File(ctx, s.Path, loudness.Options{}, func(d, t int64) {
					if t > 0 {
						progress(done+d*1000/t, total)
					}
				})
				done += 1000
				progress(done, total)
				if err != nil {
					if errors.Is(err, context.Canceled) {
						return err
					}
					if first == nil {
						first = fmt.Errorf("%s: %w", filepath.Base(s.Path), err)
					}
					continue
				}
				res[i] = an
			}
			var albumRes loudness.Result
			var ok []*loudness.Analyzer
			for _, r := range res {
				if r != nil {
					ok = append(ok, r)
				}
			}
			if len(g) > 1 && len(ok) > 0 {
				albumRes = loudness.Album(ok...)
			}
			for i, s := range g {
				if res[i] == nil {
					continue
				}
				r := res[i].Result()
				gain := &tagwrite.Gain{TrackGain: r.Gain, TrackPeak: r.Peak}
				if len(g) > 1 {
					gain.AlbumGain, gain.AlbumPeak, gain.HasAlbum = albumRes.Gain, albumRes.Peak, true
				}
				if err := tagwrite.Write(s.Path, tagwrite.Tags{ReplayGain: gain}); err != nil && first == nil {
					first = fmt.Errorf("%s: %w", filepath.Base(s.Path), err)
				}
			}
		}
		return first
	}, func() { a.flash(T("tg_rg_done", len(songs))) })
}
