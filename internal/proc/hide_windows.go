package proc

import (
	"os/exec"
	"syscall"
)

// Hidden hace que cmd no abra una ventana de consola (yt-dlp, ffmpeg o el
// servicio son programas de consola y la interfaz es una ventana).
func Hidden(cmd *exec.Cmd) *exec.Cmd {
	const createNoWindow = 0x08000000
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= createNoWindow
	cmd.SysProcAttr.HideWindow = true
	return cmd
}
