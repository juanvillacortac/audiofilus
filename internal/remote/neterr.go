package remote

import (
	"errors"
	"fmt"
	"net/url"
)

// netErr quita la URL de un error de red de net/http ("Get "<url>": EOF")
// y deja solo la causa, con op delante ("jellyfin: Items: EOF"). La URL es
// tan larga que en la pantalla de la consola la causa no se llegaba a ver
// (#13), y en Subsonic lleva el token y la sal de la contraseña, que no deben
// salir en pantalla ni en el log. El error de dentro se conserva (%w), así que
// errors.Is(err, context.DeadlineExceeded) sigue sirviendo.
func netErr(op string, err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("%s: %w", op, ue.Err)
	}
	return err
}
