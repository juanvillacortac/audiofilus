package ui

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/decode"
	"github.com/juanvillacortac/audiofilus/internal/musicbrainz"
	"github.com/juanvillacortac/audiofilus/internal/tagwrite"
)

// Buscar los datos de una canción suelta en internet (MusicBrainz): para las
// que no tienen álbum, tienen el título mal (el nombre del archivo, "01 -
// Artista - Título") o no tienen portada. Se ofrecen las grabaciones que
// casan, con la edición en que salen, y se escriben su título, su artista, el
// álbum y el año, con la portada de esa edición si se pide. Solo se tocan
// esos campos: lo vacío no borra nada.

// trackPrefix es el número de pista delante del nombre de un archivo ("01 ",
// "01. ", "01 - ", "1-02 ").
var trackPrefix = regexp.MustCompile(`^\s*(\d{1,2}[-.])?\d{1,3}\s*([.)_-]\s*)?\s+`)

// guessArtistTitle saca de una canción el artista y el título para buscarla:
// los suyos, o, si su título es el nombre del archivo, lo que se lea en él
// ("Artista - Título", sin el número de pista).
func guessArtistTitle(s Song) (artist, title string) {
	artist, title = strings.TrimSpace(s.Artist), strings.TrimSpace(s.Title)
	title = trackPrefix.ReplaceAllString(title, "")
	title = strings.NewReplacer("_", " ").Replace(title)
	if artist == "" {
		if a, t, ok := strings.Cut(title, " - "); ok {
			artist, title = strings.TrimSpace(a), strings.TrimSpace(t)
		}
	}
	return artist, title
}

// bestRelease es la edición de una grabación para sus etiquetas: un álbum
// antes que un sencillo o un recopilatorio, y la más antigua (la original).
func bestRelease(rec musicbrainz.Recording) (musicbrainz.ReleaseRef, bool) {
	var best musicbrainz.ReleaseRef
	found := false
	rank := func(r musicbrainz.ReleaseRef) int {
		if strings.EqualFold(r.Type, "Album") {
			return 0
		}
		return 1
	}
	for _, r := range rec.Releases {
		switch {
		case !found, rank(r) < rank(best),
			rank(r) == rank(best) && r.Date != "" && (best.Date == "" || r.Date < best.Date):
			best, found = r, true
		}
	}
	return best, found
}

// releaseYear es el año de una fecha de MusicBrainz ("1997-05-21", "1997").
func releaseYear(date string) int {
	if len(date) >= 4 {
		if y, err := strconv.Atoi(date[:4]); err == nil {
			return y
		}
	}
	return 0
}

// songLookup busca la canción en MusicBrainz y ofrece lo que encontró.
func (a *App) songLookup(s Song) {
	a.metaInit()
	mb := a.meta.mb
	artist, title := guessArtistTitle(s)
	a.flash(T("tk_searching"))
	go func() {
		secs := 0
		if src, err := decode.Open(s.Path); err == nil {
			if r := src.SampleRate(); r > 0 && src.Length() > 0 {
				secs = int(src.Length() / int64(r))
			}
			src.Close()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		recs, err := mb.LookupRecording(ctx, artist, title, secs)
		a.metaLater(func() {
			if err != nil || len(recs) == 0 {
				a.flash(T("tg_lookup_none"))
				return
			}
			var items []menuItem
			for _, rec := range recs[:min(len(recs), 10)] {
				rel, ok := bestRelease(rec)
				label := rec.Title + " · " + rec.Artist
				if ok {
					label += " · " + rel.Title
					if y := releaseYear(rel.Date); y > 0 {
						label += " (" + strconv.Itoa(y) + ")"
					}
				}
				items = append(items, menuItem{label: label, icon: "disc", do: func() { a.songLookupApply(s, rec, rel, ok) }})
			}
			a.menu = &menu{title: T("tg_lookup"), subtitle: strings.Trim(artist+" · "+title, " ·"), items: items}
		})
	}()
}

// songLookupApply ofrece escribir lo encontrado, con la portada de su
// edición o sin ella.
func (a *App) songLookupApply(s Song, rec musicbrainz.Recording, rel musicbrainz.ReleaseRef, hasRel bool) {
	tags := tagwrite.Tags{Title: rec.Title, Artist: rec.Artist}
	if hasRel {
		tags.Album, tags.Year = rel.Title, releaseYear(rel.Date)
	}
	apply := func(withCover bool) {
		mb := a.meta.mb
		a.metaJob(T("tg_job_tags"), rec.Title, func(ctx context.Context, progress func(done, total int64)) error {
			t := tags
			if withCover && hasRel {
				if data, mime, err := mb.Cover(ctx, rel.ID, rel.GroupID, rec.Artist, rel.Title, 1200); err == nil && len(data) > 0 {
					t.Cover = &tagwrite.Cover{MIME: mime, Data: data}
				}
			}
			return writeEach(ctx, []Song{s}, progress, func(Song) tagwrite.Tags { return t })
		}, func() { a.flash(T("tg_saved", 1)) })
	}
	sub := rec.Artist
	if hasRel {
		sub += " · " + rel.Title
	}
	items := []menuItem{{label: T("tg_apply"), icon: "check", do: func() { apply(false) }}}
	if hasRel {
		items = append(items, menuItem{label: T("tg_apply_cover"), icon: "disc", do: func() { apply(true) }})
	}
	a.menu = &menu{title: rec.Title, subtitle: sub, items: items}
}
