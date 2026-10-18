//go:build !windows

package proc

import "os/exec"

// Hidden no hace nada fuera de Windows.
func Hidden(cmd *exec.Cmd) *exec.Cmd { return cmd }
