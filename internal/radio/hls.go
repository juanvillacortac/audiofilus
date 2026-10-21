package radio

import (
	"bufio"
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/decode"
)

// Las emisoras HLS (las de muchas radios públicas y de los agregadores): en
// vez de un flujo continuo dan una lista (.m3u8) de trozos de unos segundos
// que se va renovando. Se lee la lista, se bajan los trozos nuevos en orden y
// su audio va al mismo búfer que el de una emisora normal, así que el resto
// (decodificar, esperar datos, reconectar) es igual.
//
// Lo que se entiende: la lista maestra (se elige una variante de audio, o la
// pista de audio aparte si la hay), trozos MPEG-TS con AAC (ADTS) o MP3 y
// trozos de audio "empaquetado" (.aac/.mp3 con una etiqueta ID3 delante),
// cifrados o no con AES-128, y trozos fMP4 (#EXT-X-MAP) con AAC: el trozo
// inicial dice cómo es el audio y a cada frame se le pone su cabecera ADTS.

// hlsMaxBandwidth es el máximo que se elige de una lista maestra: las radios
// dan variantes de 32 a 320 kbps; por encima suele ser vídeo.
const hlsMaxBandwidth = 400_000

// hlsVariant es una entrada de una lista maestra.
type hlsVariant struct {
	uri       string
	bandwidth int
	codecs    string
}

// hlsSegment es un trozo de una lista de medios.
type hlsSegment struct {
	uri   string
	rng   hlsRange // #EXT-X-BYTERANGE: solo un trozo de uri
	init  string   // el trozo inicial fMP4 (#EXT-X-MAP), si lo hay
	initR hlsRange
	seq   int
	title string
	key   *hlsKey
}

// hlsRange es un rango de bytes de una dirección (#EXT-X-BYTERANGE: algunas
// listas dan todos los trozos dentro de un solo archivo). n == 0: entera.
type hlsRange struct{ off, n int64 }

// parseRange lee "n@o" (sin @o, empieza donde acabó el anterior: prevEnd).
func parseRange(v string, prevEnd int64) hlsRange {
	ns, offs, ok := strings.Cut(strings.TrimSpace(v), "@")
	n, _ := strconv.ParseInt(ns, 10, 64)
	off := prevEnd
	if ok {
		off, _ = strconv.ParseInt(offs, 10, 64)
	}
	if n <= 0 || off < 0 {
		return hlsRange{}
	}
	return hlsRange{off, n}
}

// hlsKey es el cifrado de un trozo (#EXT-X-KEY, solo AES-128).
type hlsKey struct {
	uri string
	iv  []byte // nil: el número de secuencia
}

// hlsMedia es una lista de medios leída.
type hlsMedia struct {
	target   time.Duration
	segments []hlsSegment
	ended    bool
}

// hlsAttrs lee los atributos de una etiqueta (BANDWIDTH=128000,CODECS="mp4a.40.2").
func hlsAttrs(s string) map[string]string {
	m := map[string]string{}
	for s != "" {
		eq := strings.IndexByte(s, '=')
		if eq < 0 {
			break
		}
		k := strings.TrimSpace(s[:eq])
		s = s[eq+1:]
		var v string
		if strings.HasPrefix(s, `"`) {
			end := strings.IndexByte(s[1:], '"')
			if end < 0 {
				v, s = s[1:], ""
			} else {
				v, s = s[1:end+1], s[end+2:]
			}
		} else if c := strings.IndexByte(s, ','); c >= 0 {
			v, s = s[:c], s[c:]
		} else {
			v, s = s, ""
		}
		m[strings.ToUpper(k)] = v
		s = strings.TrimPrefix(s, ",")
	}
	return m
}

// hlsMaster elige de una lista maestra la dirección que hay que seguir: la
// pista de audio aparte (#EXT-X-MEDIA TYPE=AUDIO, la de por defecto) o la
// variante de más calidad que no pase de hlsMaxBandwidth (si todas pasan, la
// más ligera). "" si no es una lista maestra.
func hlsMaster(body []byte) string {
	var variants []hlsVariant
	audio, audioDefault := "", ""
	sc := bufio.NewScanner(bytes.NewReader(body))
	pending := (*hlsVariant)(nil)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(l, "#EXT-X-MEDIA:"):
			a := hlsAttrs(l[len("#EXT-X-MEDIA:"):])
			if strings.EqualFold(a["TYPE"], "AUDIO") && a["URI"] != "" {
				if audio == "" {
					audio = a["URI"]
				}
				if strings.EqualFold(a["DEFAULT"], "YES") && audioDefault == "" {
					audioDefault = a["URI"]
				}
			}
		case strings.HasPrefix(l, "#EXT-X-STREAM-INF:"):
			a := hlsAttrs(l[len("#EXT-X-STREAM-INF:"):])
			bw, _ := strconv.Atoi(a["BANDWIDTH"])
			pending = &hlsVariant{bandwidth: bw, codecs: a["CODECS"]}
		case l != "" && !strings.HasPrefix(l, "#") && pending != nil:
			pending.uri = l
			variants = append(variants, *pending)
			pending = nil
		}
	}
	if audioDefault != "" {
		return audioDefault
	}
	if audio != "" {
		return audio
	}
	if len(variants) == 0 {
		return ""
	}
	best := -1
	for i, v := range variants {
		if v.bandwidth <= hlsMaxBandwidth && (best < 0 || v.bandwidth > variants[best].bandwidth) {
			best = i
		}
	}
	if best < 0 {
		best = 0
		for i, v := range variants {
			if v.bandwidth < variants[best].bandwidth {
				best = i
			}
		}
	}
	return variants[best].uri
}

// parseHLSMedia lee una lista de medios.
func parseHLSMedia(body []byte) hlsMedia {
	m := hlsMedia{target: 6 * time.Second}
	seq := 0
	title := ""
	var key *hlsKey
	init := ""
	var initR, rng hlsRange
	var end int64 // dónde acabó el rango anterior
	sc := bufio.NewScanner(bytes.NewReader(body))
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(l, "#EXT-X-TARGETDURATION:"):
			if n, err := strconv.ParseFloat(l[len("#EXT-X-TARGETDURATION:"):], 64); err == nil && n > 0 {
				m.target = time.Duration(n * float64(time.Second))
			}
		case strings.HasPrefix(l, "#EXT-X-MEDIA-SEQUENCE:"):
			seq, _ = strconv.Atoi(strings.TrimSpace(l[len("#EXT-X-MEDIA-SEQUENCE:"):]))
		case strings.HasPrefix(l, "#EXT-X-KEY:"):
			a := hlsAttrs(l[len("#EXT-X-KEY:"):])
			key = nil
			if strings.EqualFold(a["METHOD"], "AES-128") {
				key = &hlsKey{uri: a["URI"]}
				if iv := strings.TrimPrefix(strings.TrimPrefix(a["IV"], "0x"), "0X"); iv != "" {
					key.iv, _ = hex.DecodeString(iv)
				}
			}
		case strings.HasPrefix(l, "#EXT-X-MAP:"):
			a := hlsAttrs(l[len("#EXT-X-MAP:"):])
			init, initR = a["URI"], hlsRange{}
			if a["BYTERANGE"] != "" {
				initR = parseRange(a["BYTERANGE"], 0)
			}
		case strings.HasPrefix(l, "#EXT-X-BYTERANGE:"):
			rng = parseRange(l[len("#EXT-X-BYTERANGE:"):], end)
		case strings.HasPrefix(l, "#EXT-X-ENDLIST"):
			m.ended = true
		case strings.HasPrefix(l, "#EXTINF:"):
			title = ""
			if _, t, ok := strings.Cut(l, ","); ok {
				title = strings.TrimSpace(t)
			}
		case l != "" && !strings.HasPrefix(l, "#"):
			m.segments = append(m.segments, hlsSegment{uri: l, rng: rng, seq: seq, title: title, key: key, init: init, initR: initR})
			end, rng = rng.off+rng.n, hlsRange{}
			seq++
			title = ""
		}
	}
	return m
}

// hlsGet baja una dirección (con un límite, por si no es lo que parece).
func hlsGet(ctx context.Context, u string, limit int64) ([]byte, error) {
	return hlsGetRange(ctx, u, hlsRange{}, limit)
}

// hlsGetRange baja un rango de una dirección (entera si r.n == 0). Si el
// servidor no entiende rangos y la da entera, se recorta aquí.
func hlsGetRange(ctx context.Context, u string, r hlsRange, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	if r.n > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", r.off, r.off+r.n-1))
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusPartialContent && r.n > 0:
		return io.ReadAll(io.LimitReader(resp.Body, min(limit, r.n)))
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("hls: %s: %s", u, resp.Status)
	case r.n > 0:
		if _, err := io.CopyN(io.Discard, resp.Body, r.off); err != nil {
			return nil, err
		}
		return io.ReadAll(io.LimitReader(resp.Body, min(limit, r.n)))
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// resolveURL resuelve ref respecto a base (las listas usan direcciones
// relativas).
func resolveURL(base, ref string) string {
	b, err := url.Parse(base)
	if err != nil {
		return ref
	}
	r, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return b.ResolveReference(r).String()
}

// hls toca una emisora HLS: listURL es la lista que se leyó (body). Vuelve
// solo al cortarse o si no se puede (s.fatal); run reconecta.
func (s *Stream) hls(ctx context.Context, listURL string, body []byte) error {
	// Una lista maestra lleva a la de medios (y puede haber otra maestra
	// dentro, en algunas: hasta tres saltos).
	for hop := 0; hop < 3; hop++ {
		next := hlsMaster(body)
		if next == "" {
			break
		}
		listURL = resolveURL(listURL, next)
		var err error
		if body, err = hlsGet(ctx, listURL, 1<<20); err != nil {
			return err
		}
	}
	last := -1 // el último trozo puesto
	fails := 0 // trozos seguidos que no se pudieron bajar
	keys := map[string][]byte{}
	var demux tsDemuxer
	inits := map[string]*decode.FMP4Track{} // los trozos iniciales fMP4 leídos
	for {
		m := parseHLSMedia(body)
		segs := m.segments
		// Al empezar, solo los tres últimos: es en directo (y así suena
		// enseguida, con unos segundos de margen).
		if last < 0 && !m.ended && len(segs) > 3 {
			segs = segs[len(segs)-3:]
		}
		fresh := 0
		for _, seg := range segs {
			if seg.seq <= last {
				continue
			}
			data, err := hlsGetRange(ctx, resolveURL(listURL, seg.uri), seg.rng, 16<<20)
			if err != nil {
				// Un trozo que falla suelto (un 404 o un corte del CDN) se
				// reintenta con la lista siguiente, sin perder el sitio;
				// varios seguidos, se reconecta desde el principio.
				if fails++; fails > 5 || ctx.Err() != nil {
					return err
				}
				break
			}
			fails = 0
			if seg.key != nil {
				if data, err = s.hlsDecrypt(ctx, listURL, seg, data, keys); err != nil {
					return err
				}
			}
			var audio []byte
			var codec string
			if seg.init != "" {
				audio, codec, err = s.hlsFMP4(ctx, resolveURL(listURL, seg.init), seg.initR, data, inits)
			} else {
				audio, codec, err = demux.audio(data)
			}
			if err != nil {
				if ctx.Err() == nil && !errors.Is(err, decode.ErrUnsupported) {
					return err // el trozo inicial no se pudo bajar: reconectar
				}
				return s.setFatal(err)
			}
			s.mu.Lock()
			if s.codec == "" {
				s.codec = codec
			}
			if seg.title != "" && !isNumber(seg.title) {
				s.title = seg.title
			}
			s.mu.Unlock()
			s.push(ctx, audio)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			last = seg.seq
			fresh++
		}
		if m.ended {
			return errors.New("hls: stream ended")
		}
		// La lista se renueva cada trozo: se vuelve a pedir tras uno (o tras
		// medio, si no había nada nuevo).
		wait := m.target
		if fresh == 0 {
			wait /= 2
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
		var err error
		if body, err = hlsGet(ctx, listURL, 1<<20); err != nil {
			return err
		}
	}
}

// hlsFMP4 pasa un trozo fMP4 a AAC en ADTS: lee (una vez) su trozo inicial
// para saber la pista y su configuración, saca las muestras de los moof y
// le pone a cada una su cabecera.
func (s *Stream) hlsFMP4(ctx context.Context, initURL string, initR hlsRange, data []byte, inits map[string]*decode.FMP4Track) ([]byte, string, error) {
	id := fmt.Sprintf("%s@%d+%d", initURL, initR.off, initR.n)
	t, ok := inits[id]
	if !ok {
		b, err := hlsGetRange(ctx, initURL, initR, 1<<20)
		if err != nil {
			return nil, "", err
		}
		if t, err = decode.ParseFMP4Init(b); err != nil {
			return nil, "", err
		}
		inits[id] = t
	}
	samples, err := decode.FMP4Samples(t, data)
	if err != nil {
		return nil, "", err
	}
	var out []byte
	for _, smp := range samples {
		h, err := decode.ADTSHeader(t.Config, len(smp))
		if err != nil {
			return nil, "", err
		}
		out = append(append(out, h...), smp...)
	}
	return out, "aac", nil
}

// setFatal apunta un error que no se arregla reconectando.
func (s *Stream) setFatal(err error) error {
	s.mu.Lock()
	s.fatal = err
	s.mu.Unlock()
	return err
}

// isNumber dice si un título de #EXTINF es solo un número (algunas emisoras
// ponen el del trozo, que no sirve como título).
func isNumber(t string) bool {
	_, err := strconv.ParseFloat(t, 64)
	return err == nil
}

// hlsDecrypt descifra un trozo con AES-128 (CBC, relleno PKCS#7). La clave se
// pide una vez por dirección.
func (s *Stream) hlsDecrypt(ctx context.Context, listURL string, seg hlsSegment, data []byte, keys map[string][]byte) ([]byte, error) {
	ku := resolveURL(listURL, seg.key.uri)
	key, ok := keys[ku]
	if !ok {
		var err error
		if key, err = hlsGet(ctx, ku, 64); err != nil {
			return nil, err
		}
		keys[ku] = key
	}
	if len(key) != 16 || len(data)%aes.BlockSize != 0 || len(data) == 0 {
		return nil, errors.New("hls: bad AES-128 key or segment size")
	}
	iv := seg.key.iv
	if len(iv) != 16 {
		iv = make([]byte, 16)
		for i, n := 15, seg.seq; i >= 8; i, n = i-1, n>>8 {
			iv[i] = byte(n)
		}
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(data))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, data)
	if p := int(out[len(out)-1]); p >= 1 && p <= aes.BlockSize && p <= len(out) {
		out = out[:len(out)-p]
	}
	return out, nil
}

// tsDemuxer saca el audio de trozos MPEG-TS (o de audio empaquetado). Guarda
// la pista de audio de un trozo al siguiente, por si alguno no trae las
// tablas al principio.
type tsDemuxer struct {
	pmtPID   int
	audioPID int
	codec    string
	latm     bool // hay AAC en LATM (no se entiende: el decodificador come ADTS)
}

// Tipos de flujo de una PMT (ISO 13818-1).
const (
	tsMPEG1Audio = 0x03
	tsMPEG2Audio = 0x04
	tsAACADTS    = 0x0F
	tsAACLATM    = 0x11
	tsPacket     = 188
)

// audio devuelve el audio de un trozo, listo para el decodificador ("aac" en
// ADTS o "mp3"), y su códec.
func (d *tsDemuxer) audio(data []byte) ([]byte, string, error) {
	if off := tsSync(data); off >= 0 {
		return d.ts(data[off:])
	}
	// Audio empaquetado: una o más etiquetas ID3 y luego ADTS o MP3.
	for len(data) >= 10 && string(data[:3]) == "ID3" {
		size := int(data[6]&0x7f)<<21 | int(data[7]&0x7f)<<14 | int(data[8]&0x7f)<<7 | int(data[9]&0x7f)
		end := 10 + size
		if data[5]&0x10 != 0 {
			end += 10 // pie de la etiqueta
		}
		if end > len(data) {
			return nil, "", errors.New("hls: truncated ID3 tag")
		}
		data = data[end:]
	}
	if len(data) >= 2 && data[0] == 0xFF && data[1]&0xF0 == 0xF0 {
		// ADTS tiene la capa a 00; MP3 (MPEG-1/2 capa III), 01.
		if data[1]&0x06 == 0 {
			return data, "aac", nil
		}
		return data, "mp3", nil
	}
	return nil, "", fmt.Errorf("%w (HLS segment format)", decode.ErrUnsupported)
}

// tsSync es dónde empiezan los paquetes MPEG-TS (el byte 0x47 cada 188), o
// -1 si no lo parece. Algunos servidores dejan algo delante.
func tsSync(data []byte) int {
	for off := 0; off < tsPacket && off+tsPacket < len(data); off++ {
		if data[off] == 0x47 && data[off+tsPacket] == 0x47 && (off+2*tsPacket >= len(data) || data[off+2*tsPacket] == 0x47) {
			return off
		}
	}
	if len(data) >= tsPacket && len(data) < 2*tsPacket && data[0] == 0x47 {
		return 0
	}
	return -1
}

// ts saca el flujo de audio de un trozo MPEG-TS: lee la PAT para saber dónde
// está la PMT, la PMT para saber qué paquetes son de audio, y junta la carga
// de esos PES (sin sus cabeceras).
func (d *tsDemuxer) ts(data []byte) ([]byte, string, error) {
	var out []byte
	for off := 0; off+tsPacket <= len(data); off += tsPacket {
		p := data[off : off+tsPacket]
		if p[0] != 0x47 {
			continue
		}
		start := p[1]&0x40 != 0
		pid := int(p[1]&0x1f)<<8 | int(p[2])
		afc := (p[3] >> 4) & 3
		payload := p[4:]
		if afc&2 != 0 { // campo de adaptación
			if len(payload) == 0 || int(payload[0])+1 > len(payload) {
				continue
			}
			payload = payload[int(payload[0])+1:]
		}
		if afc&1 == 0 || len(payload) == 0 {
			continue
		}
		switch {
		case pid == 0 && start:
			d.pat(payload)
		case pid == d.pmtPID && d.pmtPID != 0 && start:
			d.pmt(payload)
		case pid == d.audioPID && d.audioPID != 0:
			if start {
				// Cabecera PES: 00 00 01, id, longitud (6 bytes), dos de
				// banderas y la longitud del resto.
				if len(payload) < 9 || payload[0] != 0 || payload[1] != 0 || payload[2] != 1 {
					continue
				}
				h := 9 + int(payload[8])
				if h > len(payload) {
					continue
				}
				// Si el PES dice su longitud y cabe en este paquete, lo que
				// sobra es relleno.
				if n := 6 + (int(payload[4])<<8 | int(payload[5])); n > 6 && n >= h && n < len(payload) {
					payload = payload[:n]
				}
				payload = payload[h:]
			}
			out = append(out, payload...)
		}
	}
	if d.audioPID == 0 {
		if d.latm {
			return nil, "", fmt.Errorf("%w (AAC in LATM inside HLS segments)", decode.ErrUnsupported)
		}
		return nil, "", fmt.Errorf("%w (HLS segment without AAC or MP3 audio)", decode.ErrUnsupported)
	}
	return out, d.codec, nil
}

// section devuelve una sección PSI sin el puntero ni la cabecera común (8
// bytes) ni el CRC, o nil si no cabe.
func section(payload []byte) []byte {
	ptr := int(payload[0])
	if 1+ptr+3 > len(payload) {
		return nil
	}
	sec := payload[1+ptr:]
	length := int(sec[1]&0x0f)<<8 | int(sec[2])
	if 3+length > len(sec) || length < 9 {
		return nil
	}
	return sec[8 : 3+length-4]
}

// pat lee la PAT: el PID de la PMT del primer programa.
func (d *tsDemuxer) pat(payload []byte) {
	body := section(payload)
	for i := 0; i+4 <= len(body); i += 4 {
		prog := int(body[i])<<8 | int(body[i+1])
		if prog != 0 {
			d.pmtPID = int(body[i+2]&0x1f)<<8 | int(body[i+3])
			return
		}
	}
}

// pmt lee la PMT: el primer flujo de audio AAC (ADTS) o MP3.
func (d *tsDemuxer) pmt(payload []byte) {
	body := section(payload)
	if len(body) < 4 {
		return
	}
	info := int(body[2]&0x0f)<<8 | int(body[3])
	for i := 4 + info; i+5 <= len(body); {
		typ := body[i]
		pid := int(body[i+1]&0x1f)<<8 | int(body[i+2])
		esInfo := int(body[i+3]&0x0f)<<8 | int(body[i+4])
		switch typ {
		case tsAACADTS:
			d.audioPID, d.codec = pid, "aac"
			return
		case tsMPEG1Audio, tsMPEG2Audio:
			d.audioPID, d.codec = pid, "mp3"
			return
		case tsAACLATM:
			d.latm = true
		}
		i += 5 + esInfo
	}
}
