package ytm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/juanvillacortac/audiofilus/internal/proc"
	"github.com/juanvillacortac/audiofilus/internal/tagwrite"
)

// Find busca el yt-dlp que el usuario puso junto a audiofilus (en base): la
// versión desempaquetada en base/yt-dlp/ (arranca en ~3 s en vez de ~11 en la
// consola), un solo archivo base/yt-dlp, o uno del PATH. "" si no hay.
//
// Si lo que hay es el zip de la versión desempaquetada, primero lo
// desempaqueta en base/yt-dlp/ (unpackZip).
func Find(base string) string {
	unpackZip(base)
	if fi, err := os.Stat(filepath.Join(base, "yt-dlp")); err == nil {
		if !fi.IsDir() {
			return filepath.Join(base, "yt-dlp")
		}
		if p := unpacked(filepath.Join(base, "yt-dlp")); p != "" {
			return p
		}
	}
	if p, err := exec.LookPath("yt-dlp"); err == nil {
		return p
	}
	return ""
}

// clients son los clientes de YouTube que se prueban, en orden. Medido con
// ytm-gym: sin cuenta, "visionos" y el de por defecto pasan; los demás daban
// 403 a menudo.
var clients = []string{"visionos", ""}

// ErrLogin es cuando YouTube pide iniciar sesión para ese video (con
// restricción de edad, o solo para miembros): sin cuenta, se prueba otra
// subida de la misma canción (withAlternates); con cookies.txt, yt-dlp entra
// como el usuario.
var ErrLogin = errors.New("YouTube asks to sign in for this video")

// ErrBot es cuando YouTube pide "confirmar que no eres un bot": no es del
// video sino de la red (la IP hizo muchas peticiones) y se pasa en unas
// horas. Otra subida no sirve; cookies.txt sí.
var ErrBot = errors.New("YouTube is asking this network to confirm it's not a bot")

// loginKind clasifica un error de yt-dlp: ErrBot, ErrLogin o nil.
func loginKind(msg string) error {
	m := strings.ToLower(msg)
	if strings.Contains(m, "not a bot") {
		return ErrBot
	}
	for _, k := range []string{"sign in", "log in", "login", "age-restricted", "confirm your age", "inappropriate", "members-only", "join this channel"} {
		if strings.Contains(m, k) {
			return ErrLogin
		}
	}
	return nil
}

// cookiesFile es el cookies.txt que el usuario puso para yt-dlp (exportado
// de su navegador, en formato Netscape): en la carpeta de yt-dlp o una más
// arriba (junto a audiofilus). "" si no hay.
func cookiesFile(bin string) string {
	dir := filepath.Dir(bin)
	for _, p := range []string{filepath.Join(dir, "cookies.txt"), filepath.Join(filepath.Dir(dir), "cookies.txt")} {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}

// Progress es cómo va un trabajo de yt-dlp.
type Progress struct {
	Stage       string // "resolve" (sacando la URL), "download" o "convert"
	Done, Total int64  // bytes bajados y totales (en "download")
}

// progressTag marca las líneas de progreso que pide run a yt-dlp.
const progressTag = "AFPROG "

// run ejecuta yt-dlp con args, probando cada cliente, y devuelve lo que
// imprime (sin las líneas de progreso). progress, si no es nil, recibe cómo
// va: primero "resolve", luego "download" con los bytes y, al acabar de bajar,
// "convert" (lo que yt-dlp haga después: pasar a MP3, la portada...).
func run(ctx context.Context, bin string, progress func(Progress), args ...string) ([]byte, error) {
	report := func(p Progress) {
		if progress != nil {
			progress(p)
		}
	}
	var last error
	cookies := cookiesFile(bin)
	try := clients
	if cookies != "" {
		try = []string{"", "visionos"} // con sesión, primero el cliente web, que la usa
	}
	for _, c := range try {
		// IPv4: la URL del audio va atada a la IP que la pidió, y si la
		// consola sale por IPv6 a la hora de bajarlo, YouTube da 403 (o yt-dlp
		// reintenta despacio: 33 s en vez de 16 en la consola).
		full := []string{"--no-warnings", "--no-playlist", "--force-ipv4",
			"--newline", "--progress", "--progress-template",
			"download:" + progressTag + "%(progress.downloaded_bytes)s %(progress.total_bytes,progress.total_bytes_estimate)s"}
		if c != "" {
			full = append(full, "--extractor-args", "youtube:player_client="+c)
		}
		if cookies != "" {
			full = append(full, "--cookies", cookies)
		}
		full = append(full, args...)
		report(Progress{Stage: "resolve"})
		cmd := proc.Hidden(exec.CommandContext(ctx, bin, full...))
		cmd.Dir = os.TempDir()
		stdout, _ := cmd.StdoutPipe()
		stderr, _ := cmd.StderrPipe()
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		proc.LowPriority(cmd.Process.Pid) // que la música y la interfaz vayan antes
		var mu sync.Mutex
		var out bytes.Buffer
		var errLines []string
		read := func(r io.Reader, keep bool) {
			sc := bufio.NewScanner(r)
			for sc.Scan() {
				line := sc.Text()
				if rest, ok := strings.CutPrefix(line, progressTag); ok {
					var p Progress
					fmt.Sscan(rest, &p.Done, &p.Total)
					p.Stage = "download"
					if p.Total > 0 && p.Done >= p.Total {
						p.Stage = "convert"
					}
					report(p)
					continue
				}
				mu.Lock()
				if keep {
					out.WriteString(line + "\n")
				} else if strings.TrimSpace(line) != "" {
					errLines = append(errLines, line)
				}
				mu.Unlock()
			}
		}
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); read(stdout, true) }()
		go func() { defer wg.Done(); read(stderr, false) }()
		wg.Wait()
		err := cmd.Wait()
		if err == nil {
			return out.Bytes(), nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		msg := ""
		for k := len(errLines) - 1; k >= 0; k-- {
			if strings.Contains(errLines[k], "ERROR") {
				msg = errLines[k]
				break
			}
		}
		if msg == "" && len(errLines) > 0 {
			msg = errLines[len(errLines)-1]
		}
		if kind := loginKind(msg); kind != nil {
			last = fmt.Errorf("yt-dlp: %w: %s", kind, msg)
			if kind == ErrBot {
				break // es de la red: otro cliente da lo mismo
			}
		} else {
			last = fmt.Errorf("yt-dlp: %v: %s", err, msg)
		}
	}
	return nil, last
}

// lastLine es la última línea no vacía de s (el error de yt-dlp).
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

func watchURL(id string) string { return "https://music.youtube.com/watch?v=" + id }

// Fetch baja el audio de t (AAC en .m4a, sin recodificar) a dir para
// escucharlo, junto con sus etiquetas en un archivo aparte (el .m4a de YouTube
// no trae; library.ReadTrack las lee de ahí). Si ya estaba, no lo vuelve a
// bajar. En dir se quedan como mucho keep canciones.
func Fetch(ctx context.Context, bin string, t Track, dir string, keep int, progress func(Progress)) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, t.ID+".m4a")
	if _, err := os.Stat(path); err != nil {
		tmp := filepath.Join(dir, t.ID+".part.m4a")
		_ = os.Remove(tmp)
		err := withAlternates(ctx, bin, t, func(id string) error {
			_ = os.Remove(tmp)
			_, err := run(ctx, bin, progress, "-f", "140/bestaudio[ext=m4a]", "--fixup", "never", "--no-part", "-o", tmp, watchURL(id))
			return err
		})
		if err != nil {
			_ = os.Remove(tmp)
			return "", err
		}
		if err := os.Rename(tmp, path); err != nil {
			return "", err
		}
	}
	if t.Thumb != "" {
		_ = fetchThumb(ctx, t.Thumb, path+".jpg")
	}
	tags, _ := json.Marshal(map[string]string{"title": t.Title, "artist": t.Artist(), "album": t.Album})
	_ = os.WriteFile(path+".tags.json", tags, 0o644)
	_ = os.Chtimes(path, time.Now(), time.Now())
	prune(dir, keep)
	return path, nil
}

// prune deja en dir las keep canciones usadas más recientemente.
func prune(dir string, keep int) {
	files, _ := filepath.Glob(filepath.Join(dir, "*.m4a"))
	if len(files) <= keep {
		return
	}
	mt := func(p string) time.Time {
		fi, err := os.Stat(p)
		if err != nil {
			return time.Time{}
		}
		return fi.ModTime()
	}
	sort.Slice(files, func(i, j int) bool { return mt(files[i]).After(mt(files[j])) })
	for _, f := range files[keep:] {
		_ = os.Remove(f)
		_ = os.Remove(f + ".tags.json")
		_ = os.Remove(f + ".jpg")
	}
}

// Download guarda t en la biblioteca (musicDir) en Artista/Álbum (o
// Singles)/Título, con portada cuadrada y etiquetas limpias, y apunta su id en
// .library.json para que ni ytm-gym ni audiofilus la vuelvan a bajar.
// Devuelve la ruta.
//
// Sin mp3, el audio de YouTube tal cual (AAC en .m4a, sin recodificar): se
// baja con Fetch a la caché de escuchar (cacheDir; si ya se escuchó, no se
// vuelve a pedir) y las etiquetas y la portada se escriben en Go (saveM4A).
// Sin ffmpeg, la mitad de memoria y nada de CPU para convertir, y sin perder
// calidad (pasar de AAC a MP3 es comprimir dos veces).
//
// Con mp3, como ytm-gym: MP3 a 192 kbps con TXXX:youtube_id, con yt-dlp y
// ffmpeg (o solo ffmpeg, desde la caché: fromCache). Sin ffmpeg no se puede:
// se guarda el .m4a.
func Download(ctx context.Context, bin string, t Track, musicDir, cacheDir string, mp3 bool, progress func(Progress)) (string, error) {
	lib := LoadLibrary(musicDir)
	if rel, ok := lib[t.ID]; ok {
		if _, err := os.Stat(filepath.Join(musicDir, rel)); err == nil {
			return filepath.Join(musicDir, rel), nil
		}
	}
	if _, err := exec.LookPath("ffmpeg"); !mp3 || err != nil {
		if cacheDir == "" {
			return "", errors.New("no cache folder for the download")
		}
		cached, err := Fetch(ctx, bin, t, cacheDir, cacheKeep, progress)
		if err != nil {
			return "", err
		}
		path, err := saveM4A(t, cached, musicDir, progress)
		if err != nil {
			return "", err
		}
		if rel, err := filepath.Rel(musicDir, path); err == nil {
			lib[t.ID] = rel
			_ = SaveLibrary(musicDir, lib)
		}
		return path, nil
	}
	if cached := filepath.Join(cacheDir, t.ID+".m4a"); cacheDir != "" {
		if _, err := os.Stat(cached); err == nil {
			if _, err := exec.LookPath("ffmpeg"); err == nil {
				path, err := fromCache(ctx, t, cached, musicDir, progress)
				if err != nil {
					return "", err
				}
				if rel, err := filepath.Rel(musicDir, path); err == nil {
					lib[t.ID] = rel
					_ = SaveLibrary(musicDir, lib)
				}
				return path, nil
			}
		}
	}
	args := []string{
		"-P", musicDir,
		"-o", "%(meta_album_artist)S/%(meta_album|Singles)S/%(meta_title)S.%(ext)s",
		"--windows-filenames", "--trim-filenames", "120", "--no-overwrites",
		"--print", "after_move:filepath",
		// Las etiquetas, como CleanTags de ytm-gym.
		"--parse-metadata", "%(track,title)s:%(meta_title)s",
		"--parse-metadata", "%(artists)l:%(meta_artist)s",
		"--parse-metadata", "%(artists.0,artist,uploader)s:%(meta_album_artist)s",
		"--parse-metadata", "%(album|)s:%(meta_album)s",
		"--parse-metadata", "id:%(meta_youtube_id)s",
		"--parse-metadata", ":(?P<meta_description>)",
		"--parse-metadata", ":(?P<meta_synopsis>)",
		"--parse-metadata", ":(?P<meta_comment>)",
		"--parse-metadata", ":(?P<meta_purl>)",
		"--parse-metadata", ":(?P<meta_genre>)",
	}
	if _, err := exec.LookPath("ffmpeg"); err == nil {
		args = append(args, "-f", "bestaudio/best",
			"-x", "--audio-format", "mp3", "--audio-quality", "192K",
			"--embed-metadata", "--embed-thumbnail", "--convert-thumbnails", "jpg",
			// La portada, cuadrada: las miniaturas de YouTube son 16:9. yt-dlp
			// parte este valor como una shell: las comillas dobles hacen que
			// ffmpeg reciba las simples (sin ellas, las comas de min()
			// separarían filtros: "Filter not found").
			"--ppa", `ThumbnailsConvertor+FFmpeg_o:-vf "crop='min(iw,ih)':'min(iw,ih)'"`)
	} else {
		args = append(args, "-f", "140/bestaudio[ext=m4a]/bestaudio", "--fixup", "never")
	}
	out, err := run(ctx, bin, progress, append(args, watchURL(t.ID))...)
	if errors.Is(err, ErrLogin) && cacheDir != "" {
		// Pide sesión: el audio de otra subida de la misma canción (Fetch ya
		// la busca), con las etiquetas y la portada de la elegida.
		if _, ferr := exec.LookPath("ffmpeg"); ferr == nil {
			cached, ferr := Fetch(ctx, bin, t, cacheDir, 15, progress)
			if ferr != nil {
				return "", ferr
			}
			path, ferr := fromCache(ctx, t, cached, musicDir, progress)
			if ferr != nil {
				return "", ferr
			}
			if rel, err := filepath.Rel(musicDir, path); err == nil {
				lib[t.ID] = rel
				_ = SaveLibrary(musicDir, lib)
			}
			return path, nil
		}
	}
	if err != nil {
		return "", err
	}
	path := lastLine(string(out))
	if path == "" {
		return "", errors.New("yt-dlp: no file")
	}
	if rel, err := filepath.Rel(musicDir, path); err == nil {
		lib[t.ID] = rel
		_ = SaveLibrary(musicDir, lib)
	}
	return path, nil
}

// LoadLibrary lee .library.json de ytm-gym: id de YouTube → ruta relativa.
func LoadLibrary(musicDir string) map[string]string {
	lib := map[string]string{}
	if b, err := os.ReadFile(filepath.Join(musicDir, ".library.json")); err == nil {
		_ = json.Unmarshal(b, &lib)
	}
	return lib
}

// SaveLibrary escribe .library.json como ytm-gym (con sangría de un espacio).
func SaveLibrary(musicDir string, lib map[string]string) error {
	b, err := json.MarshalIndent(lib, "", " ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(musicDir, ".library.json.tmp")
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(musicDir, ".library.json"))
}

// thumbSize pide la miniatura de YouTube Music a 544×544: la búsqueda la da a
// 60 o 120, que en la pantalla del reproductor se vería borrosa.
var thumbSize = regexp.MustCompile(`=w\d+-h\d+`)

// fetchThumb baja la miniatura (cuadrada, en JPEG) a path, si no estaba.
func fetchThumb(ctx context.Context, url, path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	url = thumbSize.ReplaceAllString(url, "=w544-h544")
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("thumbnail: %s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// CacheDir es la caché de escuchar dentro de data/: en la tarjeta, no en
// /tmp, que en las portátiles está en la RAM (15 canciones, unos 60 MB que la
// consola no tenía para lo demás).
func CacheDir(data string) string { return filepath.Join(data, "cache", "ytm") }

// CacheKeep son las canciones que se guardan en la caché de escuchar.
const CacheKeep = 15

const cacheKeep = CacheKeep

// saveM4A copia a la biblioteca el .m4a de la caché (de Fetch), con la misma
// ruta que Download pero .m4a, y le escribe las etiquetas y la portada en Go
// (tagwrite: sin ffmpeg). La portada, cuadrada (las miniaturas de los videos
// son 16:9).
func saveM4A(t Track, cached, musicDir string, progress func(Progress)) (string, error) {
	if progress != nil {
		progress(Progress{Stage: "convert"})
	}
	dir, first := libraryDir(t, musicDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, safeName(t.Title)+".m4a")
	if _, err := os.Stat(path); err == nil {
		return path, nil // ya estaba
	}
	tmp := path + ".part.m4a"
	if err := copyFile(cached, tmp); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	tags := tagwrite.Tags{Title: t.Title, Artist: t.Artist(), Album: t.Album, AlbumArtist: first}
	if b, err := os.ReadFile(cached + ".jpg"); err == nil {
		tags.Cover = &tagwrite.Cover{MIME: "image/jpeg", Data: squareJPEG(b)}
	}
	if err := tagwrite.Write(tmp, tags); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return path, os.Rename(tmp, path)
}

// libraryDir es la carpeta de t en la biblioteca (Artista/Álbum o Singles) y
// su primer artista.
func libraryDir(t Track, musicDir string) (dir, first string) {
	album := t.Album
	if album == "" {
		album = "Singles"
	}
	first = "Desconocido"
	if len(t.Artists) > 0 {
		first = t.Artists[0]
	}
	return filepath.Join(musicDir, safeName(first), safeName(album)), first
}

// copyFile copia src a dst.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// squareJPEG recorta al centro una portada que no es cuadrada (si no se
// puede leer, la deja como está).
func squareJPEG(b []byte) []byte {
	img, err := jpeg.Decode(bytes.NewReader(b))
	if err != nil {
		return b
	}
	r := img.Bounds()
	w, h := r.Dx(), r.Dy()
	if w == h {
		return b
	}
	s := min(w, h)
	x, y := r.Min.X+(w-s)/2, r.Min.Y+(h-s)/2
	sub, ok := img.(interface {
		SubImage(image.Rectangle) image.Image
	})
	if !ok {
		return b
	}
	var out bytes.Buffer
	if jpeg.Encode(&out, sub.SubImage(image.Rect(x, y, x+s, y+s)), &jpeg.Options{Quality: 90}) != nil {
		return b
	}
	return out.Bytes()
}

// fromCache convierte a MP3 lo que ya se bajó para escuchar (el .m4a de la
// caché y su miniatura), con la misma ruta, etiquetas y portada que Download.
func fromCache(ctx context.Context, t Track, cached, musicDir string, progress func(Progress)) (string, error) {
	if progress != nil {
		progress(Progress{Stage: "convert"})
	}
	dir, first := libraryDir(t, musicDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, safeName(t.Title)+".mp3")
	if _, err := os.Stat(path); err == nil {
		return path, nil // ya estaba (--no-overwrites)
	}
	tmp := path + ".part.mp3"
	args := []string{"-y", "-v", "error", "-i", cached}
	thumb := cached + ".jpg"
	_, err := os.Stat(thumb)
	hasThumb := err == nil
	if hasThumb {
		args = append(args, "-i", thumb, "-map", "0:a", "-map", "1:v", "-c:v", "mjpeg",
			"-metadata:s:v", "title=Album cover", "-metadata:s:v", "comment=Cover (front)", "-disposition:v", "attached_pic")
	}
	// Sin las etiquetas del contenedor de YouTube (major_brand=dash...).
	args = append(args, "-map_metadata", "-1", "-c:a", "libmp3lame", "-b:a", "192k", "-id3v2_version", "3",
		"-metadata", "title="+t.Title,
		"-metadata", "artist="+t.Artist(),
		"-metadata", "album_artist="+first,
		"-metadata", "album="+t.Album,
		"-metadata", "youtube_id="+t.ID,
		"-f", "mp3", tmp)
	cmd := proc.Hidden(exec.CommandContext(ctx, "ffmpeg", args...))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Start()
	if err == nil {
		proc.LowPriority(cmd.Process.Pid)
		err = cmd.Wait()
	}
	if err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("ffmpeg: %v: %s", err, lastLine(stderr.String()+" "))
	}
	return path, os.Rename(tmp, path)
}

// safeName es un nombre de archivo que vale en exFAT, como lo deja yt-dlp con
// --windows-filenames (los caracteres prohibidos, por sus versiones de ancho
// completo) y recortado a 120 bytes como --trim-filenames.
func safeName(s string) string {
	r := strings.NewReplacer("/", "⧸", "\\", "⧹", ":", "：", "?", "？", "\"", "＂", "*", "＊", "|", "｜", "<", "＜", ">", "＞")
	s = strings.TrimRight(strings.TrimSpace(r.Replace(s)), ". ")
	if s == "" {
		s = "_"
	}
	for len(s) > 120 {
		_, size := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-size]
	}
	return s
}

// withAlternates hace do con el video de t y, si YouTube pide sesión, con
// otras subidas de la misma canción (alternates) hasta que una funcione.
// Si ninguna, devuelve el error del original.
func withAlternates(ctx context.Context, bin string, t Track, do func(id string) error) error {
	err := do(t.ID)
	if !errors.Is(err, ErrLogin) {
		return err
	}
	for _, id := range alternates(ctx, bin, t) {
		if do(id) == nil {
			return nil
		}
		if ctx.Err() != nil {
			break
		}
	}
	return err
}

// alternates busca en YouTube (con yt-dlp: el buscador normal, que trae el
// video oficial, los de la letra y otros canales) otras subidas de la misma
// canción: con el título dentro del del video y, si se sabe, una duración
// parecida (±15 %; así no se cuela un directo o un remix largo).
func alternates(ctx context.Context, bin string, t Track) []string {
	out, err := run(ctx, bin, nil, "--flat-playlist", "--print", "%(id)s\t%(duration)s\t%(title)s", "ytsearch8:"+t.Artist()+" "+t.Title)
	if err != nil {
		return nil
	}
	want := normTitle(t.Title)
	dur := parseClock(t.Duration)
	var ids []string
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.SplitN(line, "\t", 3)
		if len(f) < 3 || f[0] == t.ID || f[0] == "" {
			continue
		}
		if want != "" && !strings.Contains(normTitle(f[2]), want) {
			continue
		}
		var d float64
		fmt.Sscan(f[1], &d)
		if dur > 0 && d > 0 && (d < dur*0.85 || d > dur*1.15) {
			continue
		}
		ids = append(ids, f[0])
	}
	return ids
}

// normTitle deja un título para comparar: en minúsculas, sin lo que va entre
// paréntesis o corchetes y solo con letras y números.
func normTitle(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range strings.ToLower(s) {
		switch {
		case r == '(' || r == '[':
			depth++
		case r == ')' || r == ']':
			depth = max(0, depth-1)
		case depth == 0 && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			b.WriteRune(r)
		}
	}
	return b.String()
}

// parseClock lee "3:59" o "1:02:03" en segundos (0 si no se puede).
func parseClock(s string) float64 {
	var total float64
	for _, p := range strings.Split(s, ":") {
		var v float64
		if _, err := fmt.Sscan(p, &v); err != nil {
			return 0
		}
		total = total*60 + v
	}
	return total
}
