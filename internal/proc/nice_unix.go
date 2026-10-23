//go:build !windows

package proc

import "syscall"

// LowPriority baja la prioridad del proceso pid (nice 10): lo pesado de fondo
// (yt-dlp, ffmpeg) cede la CPU a la música y a la interfaz cuando la consola
// va justa. Lo que lance después lo hereda.
func LowPriority(pid int) {
	_ = syscall.Setpriority(syscall.PRIO_PROCESS, pid, 10)
}
