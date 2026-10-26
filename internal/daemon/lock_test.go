//go:build unix

package daemon

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/config"
)

// Un solo servicio por carpeta: mientras uno tiene el candado, otro que
// arranca espera (por si se está cerrando) y se va con ErrAlreadyRunning;
// cuando lo suelta, el siguiente se lo queda.
func TestLockService(t *testing.T) {
	old := lockWait
	lockWait = 300 * time.Millisecond
	defer func() { lockWait = old }()
	paths := config.Paths{Socket: filepath.Join(t.TempDir(), "s.sock")}
	unlock, err := lockService(paths)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := lockService(paths); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second service: %v", err)
	}
	if time.Since(start) < lockWait {
		t.Error("didn't wait for a service that may be closing")
	}
	unlock()
	unlock2, err := lockService(paths)
	if err != nil {
		t.Fatalf("after unlocking: %v", err)
	}
	unlock2()
}
