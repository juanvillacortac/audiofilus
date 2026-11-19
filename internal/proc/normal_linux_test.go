package proc

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// TestOwnSession comprueba que OwnSession pide una sesión nueva sin perder
// lo que ya traía el comando en SysProcAttr.
func TestOwnSession(t *testing.T) {
	c := OwnSession(exec.Command("true"))
	if c.SysProcAttr == nil || !c.SysProcAttr.Setsid {
		t.Fatalf("Setsid not set: %+v", c.SysProcAttr)
	}
	c = exec.Command("true")
	c.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
	OwnSession(c)
	if !c.SysProcAttr.Setsid || c.SysProcAttr.Pdeathsig != syscall.SIGTERM {
		t.Errorf("existing attributes lost: %+v", c.SysProcAttr)
	}
	// Y de verdad arranca en otra sesión: su sid es su propio pid.
	c = OwnSession(exec.Command("sleep", "1"))
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Process.Kill(); _ = c.Wait() }()
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", c.Process.Pid))
	if err != nil {
		t.Skip(err)
	}
	// "pid (comm) estado ppid pgrp sesión ...": tras el paréntesis final.
	f := strings.Fields(string(b[strings.LastIndexByte(string(b), ')')+1:]))
	if want := strconv.Itoa(c.Process.Pid); len(f) < 4 || f[3] != want {
		t.Errorf("child session %v, want its own pid %s", f, want)
	}
}
