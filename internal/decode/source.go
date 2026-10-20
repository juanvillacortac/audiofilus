// Package decode convierte archivos de audio en un flujo común: muestras
// float32 estéreo intercaladas, a la frecuencia de muestreo del propio archivo.
package decode

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Source es un flujo de audio decodificado. Un frame es un par estéreo; las
// posiciones y longitudes se cuentan en frames, a SampleRate.
type Source interface {
	// Read llena dst con muestras estéreo intercaladas (I, D, I, D, ...).
	// Devuelve cuántas muestras escribió, siempre un múltiplo de 2, e io.EOF
	// cuando el flujo se acaba.
	Read(dst []float32) (int, error)
	SampleRate() int
	// Length es el total de frames, o -1 si no se conoce.
	Length() int64
	// Position es el próximo frame que devolverá Read.
	Position() int64
	// SeekFrame salta a frame, acotado a los límites del flujo.
	SeekFrame(frame int64) error
	Close() error
}

// ErrUnsupported se devuelve para archivos cuyo formato o códec no se puede
// decodificar (por ejemplo, Opus dentro de un .ogg).
var ErrUnsupported = errors.New("unsupported audio format")

// Extensions son las extensiones de archivo que entiende Open.
// Los que dependen de una biblioteca en C (Opus, AAC, módulos) se listan
// igual: si falta la biblioteca, Open falla con ErrUnsupported y se salta.
var Extensions = append(append([]string{".mp3", ".ogg", ".oga", ".opus", ".flac", ".wav", ".aif", ".aiff", ".aifc", ".m4a", ".m4b", ".mp4", ".aac", ".dsf", ".dff"}, ModuleExtensions...), GMEExtensions...)

// IsAudioFile dice si path tiene una extensión soportada.
func IsAudioFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	for _, e := range Extensions {
		if ext == e {
			return true
		}
	}
	return false
}

// Open decodifica el archivo path, eligiendo el decodificador por su contenido
// (los primeros bytes), no por la extensión.
func Open(path string) (Source, error) {
	// Una pista de un CUE: un trozo de su archivo (segment.go).
	if IsCuePath(path) {
		return OpenSegment(path)
	}
	// Una canción de un archivo de libgme (gme.go).
	if file, song, ok := SplitGMEPath(path); ok {
		return newGME(file, song)
	}
	if IsGMEFile(path) {
		return newGME(path, 1)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	var magic [36]byte
	n, _ := io.ReadFull(f, magic[:])
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		f.Close()
		return nil, err
	}

	var src Source
	switch {
	case n >= 4 && string(magic[:4]) == "fLaC":
		src, err = newFLAC(f)
	case n >= 36 && string(magic[:4]) == "OggS" && string(magic[28:36]) == "OpusHead":
		src, err = newOpus(f, path)
	case n >= 4 && string(magic[:4]) == "OggS":
		src, err = newVorbis(f)
	case n >= 12 && string(magic[:4]) == "RIFF" && string(magic[8:12]) == "WAVE":
		src, err = newWAV(f)
	case n >= 12 && string(magic[:4]) == "FORM" && (string(magic[8:12]) == "AIFF" || string(magic[8:12]) == "AIFC"):
		src, err = newAIFF(f)
	case n >= 4 && string(magic[:4]) == "DSD ":
		src, err = newDSF(f)
	case n >= 16 && string(magic[:4]) == "FRM8" && string(magic[12:16]) == "DSD ":
		src, err = newDFF(f)
	case isMP4(magic[:n]):
		src, err = newM4A(f)
	case isModule(path):
		src, err = newXMP(f, path)
	case strings.EqualFold(filepath.Ext(path), ".mp3") || isMP3Header(magic[:n]):
		// libmpg123 si está (mucho más eficiente); si no, go-mp3.
		if src, err = newMPG123(f); errors.Is(err, ErrUnsupported) {
			src, err = newMP3(f)
		}
	default:
		err = ErrUnsupported
	}
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return src, nil
}

// isMP3Header reconoce una etiqueta ID3 o la palabra de sincronía de un frame MP3.
func isMP3Header(b []byte) bool {
	if len(b) >= 3 && string(b[:3]) == "ID3" {
		return true
	}
	return len(b) >= 2 && b[0] == 0xFF && b[1]&0xE0 == 0xE0
}

// clampFrame acota frame a [0, length]; length < 0 significa desconocida.
func clampFrame(frame, length int64) int64 {
	if frame < 0 {
		return 0
	}
	if length >= 0 && frame > length {
		return length
	}
	return frame
}
