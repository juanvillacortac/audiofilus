//go:build unix

package daemon

import (
	"os"
	"syscall"
)

// tryLock toma el candado de path (flock, exclusivo, sin esperar): ok es
// false si lo tiene otro proceso. unlock lo suelta; si el proceso muere, lo
// suelta el sistema.
func tryLock(path string) (unlock func(), ok bool, err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if err == syscall.EWOULDBLOCK {
			return nil, false, nil
		}
		return nil, false, err
	}
	return func() { f.Close() }, true, nil
}
