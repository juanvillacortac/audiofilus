//go:build !ios

package decode

import "errors"

// newPlatformAAC es el decodificador de AAC del sistema: fuera de iOS no
// hay (se usa libfdk-aac).
func newPlatformAAC(config []byte) (aacCodec, error) {
	return nil, errors.New("no system AAC decoder")
}
