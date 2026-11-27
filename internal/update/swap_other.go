//go:build !darwin || ios

package update

import "errors"

// swap solo hace falta para la app de macOS (placeApp), que Detect solo
// reconoce allí.
func swap(a, b string) error { return errors.New("only on macOS") }
