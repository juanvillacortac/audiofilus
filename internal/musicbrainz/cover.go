package musicbrainz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// coverLimit es el tamaño máximo de una portada que se baja.
const coverLimit = 16 << 20

// FrontCover baja la portada de una edición del Cover Art Archive, de size
// píxeles (250, 500 o 1200; 0 la original). Si la edición no tiene, prueba
// con la del álbum (groupID; puede ir vacío). Devuelve la imagen y su tipo
// ("image/jpeg"...).
func (c *Client) FrontCover(ctx context.Context, releaseID, groupID string, size int) ([]byte, string, error) {
	name := "front"
	switch size {
	case 250, 500, 1200:
		name = fmt.Sprintf("front-%d", size)
	}
	try := []string{}
	if releaseID != "" {
		try = append(try, c.CAA+"/release/"+url.PathEscape(releaseID)+"/"+name)
	}
	if groupID != "" {
		try = append(try, c.CAA+"/release-group/"+url.PathEscape(groupID)+"/"+name)
	}
	for _, u := range try {
		b, mime, err := c.get(ctx, u, false, coverLimit)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, "", err
		}
		return b, imageType(b, mime), nil
	}
	return nil, "", ErrNotFound
}

// ITunesCover busca la portada de un álbum en el buscador de iTunes (sin
// clave), para cuando MusicBrainz no la tiene: la del primer resultado cuyo
// artista coincide, a size píxeles (las de iTunes se piden de cualquier
// tamaño cambiando su dirección).
func (c *Client) ITunesCover(ctx context.Context, artist, album string, size int) ([]byte, string, error) {
	if size <= 0 {
		size = 600
	}
	u := c.ITunes + "/search?media=music&entity=album&limit=10&term=" + url.QueryEscape(strings.TrimSpace(artist+" "+album))
	b, _, err := c.get(ctx, u, false, 1<<20)
	if err != nil {
		return nil, "", err
	}
	var res struct {
		Results []struct {
			Artist string `json:"artistName"`
			Album  string `json:"collectionName"`
			Art    string `json:"artworkUrl100"`
		} `json:"results"`
	}
	if err := json.Unmarshal(b, &res); err != nil {
		return nil, "", err
	}
	for _, r := range res.Results {
		if r.Art == "" || (artist != "" && similarity(r.Artist, artist) < 0.6) {
			continue
		}
		art := strings.Replace(r.Art, "100x100", fmt.Sprintf("%dx%d", size, size), 1)
		img, mime, err := c.get(ctx, art, false, coverLimit)
		if err != nil {
			return nil, "", err
		}
		return img, imageType(img, mime), nil
	}
	return nil, "", ErrNotFound
}

// Cover busca la portada de un álbum como se pueda: la de su edición o su
// álbum en el Cover Art Archive y, si no hay, la de iTunes.
func (c *Client) Cover(ctx context.Context, releaseID, groupID, artist, album string, size int) ([]byte, string, error) {
	if releaseID != "" || groupID != "" {
		b, mime, err := c.FrontCover(ctx, releaseID, groupID, size)
		if err == nil || !errors.Is(err, ErrNotFound) {
			return b, mime, err
		}
	}
	return c.ITunesCover(ctx, artist, album, size)
}

// imageType es el tipo de una imagen: el que dice el servidor si es de
// imagen, si no el que se adivina por sus primeros bytes.
func imageType(b []byte, mime string) string {
	if strings.HasPrefix(mime, "image/") {
		return strings.TrimSpace(strings.Split(mime, ";")[0])
	}
	return http.DetectContentType(b)
}
