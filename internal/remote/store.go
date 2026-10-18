package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/config"
)

// scheme es el prefijo de las rutas remotas.
const scheme = "subsonic://"

// SongPath es la ruta de una canción remota.
func SongPath(server, id string) string { return scheme + Slug(server) + "/" + id }

// CoverPath es la ruta de una portada remota.
func CoverPath(server, coverArt string) string {
	return scheme + Slug(server) + "/cover/" + coverArt
}

// IsRemote dice si path es remota.
func IsRemote(path string) bool { return strings.HasPrefix(path, scheme) }

// Parse separa una ruta remota: el servidor, si es portada y el id.
func Parse(path string) (server string, cover bool, id string, ok bool) {
	rest, found := strings.CutPrefix(path, scheme)
	if !found {
		return "", false, "", false
	}
	server, id, found = strings.Cut(rest, "/")
	if !found || id == "" {
		return "", false, "", false
	}
	if c, isCover := strings.CutPrefix(id, "cover/"); isCover {
		return server, true, c, true
	}
	return server, false, id, true
}

// Slug es el nombre de un servidor en rutas y carpetas: minúsculas, letras,
// números y guiones.
func Slug(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "-"):
			b.WriteByte('-')
		}
	}
	if s := strings.Trim(b.String(), "-"); s != "" {
		return s
	}
	return "server"
}

// Meta es lo que se sabe de una canción remota sin bajarla: para que el
// servicio (que no tiene el catálogo) diga qué suena.
type Meta struct {
	Title  string `json:"title"`
	Artist string `json:"artist"`
	Album  string `json:"album"`
}

// Store es la caché de lo remoto, la misma para la interfaz y el servicio
// (data/cache/<servidor>): canciones, portadas y el índice de etiquetas.
type Store struct {
	data    string
	servers func() []config.Server

	mu       sync.Mutex
	inflight map[string]bool  // canciones bajándose
	failed   map[string]error // la última que falló de cada una
	meta     map[string]map[string]Meta
	metaAt   map[string]time.Time
	// Los clientes con su sesión, y la configuración con que se crearon.
	clients   map[string]Backend
	clientCfg map[string]config.Server
}

// NewStore prepara la caché en dataDir; servers da la configuración actual
// (se vuelve a pedir: puede cambiar mientras corre).
func NewStore(dataDir string, servers func() []config.Server) *Store {
	return &Store{data: dataDir, servers: servers, inflight: map[string]bool{}, failed: map[string]error{},
		meta: map[string]map[string]Meta{}, metaAt: map[string]time.Time{},
		clients: map[string]Backend{}, clientCfg: map[string]config.Server{}}
}

// dir es la caché de un servidor.
func (s *Store) dir(server string) string { return filepath.Join(s.data, "cache", server) }

// client es el cliente del servidor con ese slug, ya con la sesión iniciada
// (Jellyfin la necesita para las URL). Se guarda mientras su configuración
// no cambie.
func (s *Store) client(server string) (Backend, config.Server, error) {
	for _, srv := range s.servers() {
		if Slug(srv.Name) != server {
			continue
		}
		s.mu.Lock()
		c, ok := s.clients[server]
		same := ok && s.clientCfg[server] == srv
		s.mu.Unlock()
		if same {
			return c, srv, nil
		}
		c = New(srv)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		err := c.Ping(ctx)
		cancel()
		if err != nil {
			return nil, srv, err
		}
		s.mu.Lock()
		s.clients[server], s.clientCfg[server] = c, srv
		s.mu.Unlock()
		return c, srv, nil
	}
	return nil, config.Server{}, fmt.Errorf("remote: unknown server %q", server)
}

// cached es el archivo de la canción si ya está en la caché.
func (s *Store) cached(server, id string) string {
	m, _ := filepath.Glob(filepath.Join(s.dir(server), "songs", id+".*"))
	for _, f := range m {
		if !strings.HasSuffix(f, ".part") {
			return f
		}
	}
	return ""
}

// Local es el archivo de la canción remota path: si ya está (ready), su
// ruta; si no, empieza a bajarla en segundo plano y vuelve enseguida (sin
// esperar: el servicio lo pregunta desde el hilo del audio). err es el fallo
// de la última vez que se intentó.
func (s *Store) Local(path string) (local string, ready bool, err error) {
	server, cover, id, ok := Parse(path)
	if !ok || cover {
		return "", false, fmt.Errorf("remote: bad path %q", path)
	}
	if f := s.cached(server, id); f != "" {
		now := time.Now()
		_ = os.Chtimes(f, now, now) // usada: la última en salir de la caché
		return f, true, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.failed[path]; err != nil {
		delete(s.failed, path) // la próxima vez se reintenta
		return "", false, err
	}
	if !s.inflight[path] {
		s.inflight[path] = true
		go s.fetch(server, id, path)
	}
	return "", false, nil
}

// Prefetch empieza a bajar path si es remota y no está (la siguiente de la
// cola, para que al pasar no haya que esperar).
func (s *Store) Prefetch(path string) {
	if IsRemote(path) {
		_, _, _ = s.Local(path)
	}
}

// exts traduce el tipo de lo que manda el servidor a una extensión que
// decode reconozca.
var exts = map[string]string{
	"audio/mpeg": "mp3", "audio/mp3": "mp3", "audio/flac": "flac", "audio/x-flac": "flac",
	"audio/ogg": "ogg", "audio/opus": "opus", "audio/mp4": "m4a", "audio/x-m4a": "m4a",
	"audio/aac": "m4a", "audio/wav": "wav", "audio/x-wav": "wav", "audio/aiff": "aiff",
}

// fetch baja la canción a la caché.
func (s *Store) fetch(server, id, path string) {
	err := s.download(server, id)
	s.mu.Lock()
	delete(s.inflight, path)
	if err != nil {
		s.failed[path] = err
		delete(s.clients, server) // por si caducó la sesión: la próxima vez, otra
	}
	s.mu.Unlock()
}

func (s *Store) download(server, id string) error {
	c, srv, err := s.client(server)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	resp, err := c.fetch(ctx, c.StreamURL(id))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	ext := exts[strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0])]
	if ext == "" {
		ext = "mp3"
	}
	dir := filepath.Join(s.dir(server), "songs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(dir, id+".part")
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return err
	}
	f.Close()
	if err := os.Rename(tmp, filepath.Join(dir, id+"."+ext)); err != nil {
		return err
	}
	limit := srv.CacheMB
	if limit <= 0 {
		limit = 400
	}
	prune(dir, int64(limit)<<20)
	return nil
}

// prune deja la caché de canciones por debajo de limit bytes, quitando las
// usadas hace más tiempo.
func prune(dir string, limit int64) {
	entries, _ := os.ReadDir(dir)
	type file struct {
		path string
		size int64
		at   time.Time
	}
	var files []file
	var total int64
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || e.IsDir() || strings.HasSuffix(e.Name(), ".part") {
			continue
		}
		files = append(files, file{filepath.Join(dir, e.Name()), info.Size(), info.ModTime()})
		total += info.Size()
	}
	sort.Slice(files, func(i, j int) bool { return files[i].at.Before(files[j].at) })
	for _, f := range files {
		if total <= limit {
			return
		}
		_ = os.Remove(f.path)
		total -= f.size
	}
}

// Cover es la portada remota path a size píxeles (JPEG o PNG), de la caché o
// del servidor.
func (s *Store) Cover(ctx context.Context, path string, size int) ([]byte, error) {
	server, cover, id, ok := Parse(path)
	if !ok || !cover {
		return nil, fmt.Errorf("remote: bad cover %q", path)
	}
	file := filepath.Join(s.dir(server), "covers", fmt.Sprintf("%s-%d", id, size))
	if b, err := os.ReadFile(file); err == nil {
		return b, nil
	}
	c, _, err := s.client(server)
	if err != nil {
		return nil, err
	}
	resp, err := c.fetch(ctx, c.CoverURL(id, size))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err == nil {
		_ = os.WriteFile(file, b, 0o644)
	}
	return b, nil
}

// metaFile es el índice de etiquetas de un servidor.
func (s *Store) metaFile(server string) string { return filepath.Join(s.dir(server), "meta.json") }

// SaveMeta escribe el índice de etiquetas de un servidor (lo hace la
// interfaz al cargar el catálogo).
func (s *Store) SaveMeta(server string, m map[string]Meta) error {
	if err := os.MkdirAll(s.dir(Slug(server)), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	f := s.metaFile(Slug(server))
	if err := os.WriteFile(f+".tmp", b, 0o644); err != nil {
		return err
	}
	return os.Rename(f+".tmp", f)
}

// Track son las etiquetas de la canción remota path, del índice (que se
// relee si cambió).
func (s *Store) Track(path string) (Meta, bool) {
	server, cover, id, ok := Parse(path)
	if !ok || cover {
		return Meta{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.metaFile(server)
	if info, err := os.Stat(f); err == nil && info.ModTime() != s.metaAt[server] {
		if b, err := os.ReadFile(f); err == nil {
			m := map[string]Meta{}
			if json.Unmarshal(b, &m) == nil {
				s.meta[server], s.metaAt[server] = m, info.ModTime()
			}
		}
	}
	m, ok := s.meta[server][id]
	return m, ok
}

// Download guarda la canción remota path (el archivo original, con sus
// etiquetas y portada) en musicDir/Artista/Álbum/NN Título.ext. Si ya estaba,
// no la vuelve a bajar. progress recibe los bytes. Devuelve la ruta.
func (s *Store) Download(ctx context.Context, song Song, server, musicDir string, progress func(done, total int64)) (string, error) {
	c, _, err := s.client(Slug(server))
	if err != nil {
		return "", err
	}
	artist := song.AlbumArtist
	if artist == "" {
		artist = song.Artist
	}
	album := song.Album
	if album == "" {
		album = "Singles"
	}
	name := song.Title
	if song.Track > 0 {
		name = fmt.Sprintf("%02d %s", song.Track, song.Title)
	}
	ext := song.Suffix
	if ext == "" {
		ext = "mp3"
	}
	dir := filepath.Join(musicDir, safeName(artist), safeName(album))
	path := filepath.Join(dir, safeName(name)+"."+ext)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	resp, err := c.fetch(ctx, c.DownloadURL(song.ID))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	tmp := path + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	total := resp.ContentLength
	var done int64
	buf := make([]byte, 64<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				f.Close()
				_ = os.Remove(tmp)
				return "", err
			}
			done += int64(n)
			if progress != nil {
				progress(done, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			_ = os.Remove(tmp)
			return "", rerr
		}
	}
	f.Close()
	return path, os.Rename(tmp, path)
}

// safeName es un nombre de archivo que vale en exFAT (como --windows-filenames
// de yt-dlp: los caracteres prohibidos, por sus versiones de ancho completo).
func safeName(s string) string {
	r := strings.NewReplacer("/", "⧸", "\\", "⧹", ":", "：", "?", "？", "\"", "＂", "*", "＊", "|", "｜", "<", "＜", ">", "＞")
	s = strings.TrimRight(strings.TrimSpace(r.Replace(s)), ". ")
	if s == "" {
		return "_"
	}
	return s
}
