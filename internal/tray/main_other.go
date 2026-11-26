//go:build !darwin || ios

package tray

// RunWithMain runs the service: only macOS needs the main thread for the tray.
func RunWithMain(run func()) { run() }

// OpenDocuments does nothing: elsewhere opened files arrive as arguments.
func OpenDocuments(func(urls []string)) {}
