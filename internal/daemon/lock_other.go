//go:build !unix

package daemon

// tryLock no tiene candado fuera de Unix: queda solo la pregunta al socket
// (Run).
func tryLock(string) (unlock func(), ok bool, err error) { return func() {}, true, nil }
