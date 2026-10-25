package ui

import (
	"bytes"
	"image"
	"strings"
	"sync"

	"github.com/juanvillacortac/audiofilus/internal/mpd"
)

// Las portadas de la biblioteca de un servidor MPD (la de otro audiofilus o
// un mpd, "Biblioteca de <aparato>"): se le piden con albumart (la suelta)
// y readpicture (la incrustada), por la URI de una canción. Un álbum de la
// lista no trae canciones: su portada es la de la primera que encuentre
// (find … window 0:1).

// mpdAlbumMark separa, en la ruta de la portada de un álbum remoto, el
// artista del título (no puede ir en un nombre de archivo).
const mpdAlbumMark = "\x01"

// mpdAlbumCover es la ruta de la portada del álbum remoto al del servidor
// host (el Album.coverPath de su botón).
func mpdAlbumCover(host string, al mpdAlbum) string {
	return mpdLibPath(host, mpdAlbumMark+al.artist+mpdAlbumMark+al.title)
}

// mpdCovers guarda lo bajado (también que no hay: nil), y las contraseñas
// de los servidores (las que se dieron al mandarles música).
var mpdCovers struct {
	sync.Mutex
	data     map[string][]byte
	password map[string]string
}

// maxMPDCovers es cuántas portadas bajadas se guardan (las reducidas las
// guarda el cargador; estas son para no volver a pedirlas a cada tamaño).
const maxMPDCovers = 64

// setMPDPassword apunta la contraseña del servidor host.
func setMPDPassword(host, password string) {
	mpdCovers.Lock()
	defer mpdCovers.Unlock()
	if mpdCovers.password == nil {
		mpdCovers.password = map[string]string{}
	}
	mpdCovers.password[host] = password
}

// decodeMPDCover baja y decodifica la portada de coverPath (de mpdLibPath o
// mpdAlbumCover). Corre en los trabajadores del cargador.
func decodeMPDCover(coverPath string) image.Image {
	mpdCovers.Lock()
	b, ok := mpdCovers.data[coverPath]
	mpdCovers.Unlock()
	if !ok {
		b = fetchMPDCover(coverPath)
		mpdCovers.Lock()
		if mpdCovers.data == nil || len(mpdCovers.data) >= maxMPDCovers {
			mpdCovers.data = map[string][]byte{}
		}
		mpdCovers.data[coverPath] = b
		mpdCovers.Unlock()
	}
	if b == nil {
		return nil
	}
	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return nil
	}
	return img
}

// fetchMPDCover pide la portada al servidor (nil si no tiene o no se pudo).
func fetchMPDCover(coverPath string) []byte {
	host, uri, ok := splitMPDLib(coverPath)
	if !ok {
		return nil
	}
	mpdCovers.Lock()
	password := mpdCovers.password[host]
	mpdCovers.Unlock()
	c, err := mpd.Dial(host, password)
	if err != nil {
		return nil
	}
	defer c.Close()
	if rest, isAlbum := strings.CutPrefix(uri, mpdAlbumMark); isAlbum {
		artist, title, _ := strings.Cut(rest, mpdAlbumMark)
		pairs, err := c.Cmd("find", "((Album == "+filterQuote(title)+") AND (AlbumArtist == "+filterQuote(artist)+"))", "window", "0:1")
		if err == nil && len(pairs) == 0 {
			pairs, err = c.Cmd("find", "album", title, "window", "0:1")
		}
		songs := mpd.Songs(pairs)
		if err != nil || len(songs) == 0 {
			return nil
		}
		uri = songs[0].File
	}
	b, err := c.Picture(uri)
	if err != nil {
		return nil
	}
	return b
}
