package main

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestFreezeAndThaw comprueba que un proceso elegido se congela y queda
// apuntado en frozenPath, y que thawLeftovers lo descongela (como tras una
// caída de Audiofilus a mitad de juego). Solo toca su propio proceso.
func TestFreezeAndThaw(t *testing.T) {
	if _, err := os.Stat(frozenPath); err == nil {
		t.Skip(frozenPath + " exists: a game is running")
	}
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skip(err)
	}
	pid := cmd.Process.Pid
	defer func() { _ = syscall.Kill(pid, syscall.SIGCONT); cmd.Process.Kill(); cmd.Wait() }()
	old := frozenComms
	frozenComms = map[string]bool{"sleep": true} // para que thawLeftovers lo reconozca
	defer func() { frozenComms = old; os.Remove(frozenPath) }()

	pids := freezeBackground(func(p int, _ string) bool { return p == pid })
	if len(pids) != 1 || pids[0] != pid {
		t.Fatalf("froze %v, want only %d", pids, pid)
	}
	if !waitState(pid, func(s string) bool { return s == "T" }) {
		t.Errorf("process state %q after freezing, want T", procState(pid))
	}
	thawLeftovers()
	if !waitState(pid, func(s string) bool { return s != "T" }) {
		t.Errorf("process still stopped after thawLeftovers")
	}
	if _, err := os.Stat(frozenPath); err == nil {
		t.Errorf("%s left behind", frozenPath)
	}
}

// waitState espera hasta un segundo a que el estado de pid cumpla ok.
func waitState(pid int, ok func(string) bool) bool {
	for i := 0; i < 100; i++ {
		if ok(procState(pid)) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// procState es el estado de pid en /proc (R, S, T...).
func procState(pid int) string {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return ""
	}
	s := string(b)
	f := strings.Fields(s[strings.LastIndexByte(s, ')')+1:])
	return f[0]
}

// TestThawWaitsForLauncher comprueba que, mientras el `launch` que congeló
// sigue vivo, thawLeftovers no descongela nada (un `audiofilus status` a
// mitad de juego descongelaba Syncthing), y que sí lo hace cuando ya no
// está.
func TestThawWaitsForLauncher(t *testing.T) {
	if _, err := os.Stat(frozenPath); err == nil {
		t.Skip(frozenPath + " exists: a game is running")
	}
	victim := exec.Command("sleep", "30")
	if err := victim.Start(); err != nil {
		t.Skip(err)
	}
	vpid := victim.Process.Pid
	defer func() { _ = syscall.Kill(vpid, syscall.SIGCONT); victim.Process.Kill(); victim.Wait() }()
	// Un "launch" de mentira: un script que espera, con "launch" de argumento.
	dir := t.TempDir()
	script := dir + "/fake"
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	fake := exec.Command(script, "launch")
	if err := fake.Start(); err != nil {
		t.Skip(err)
	}
	fpid := fake.Process.Pid
	defer func() { fake.Process.Kill(); fake.Wait() }()
	// Recién lanzado, el hijo puede no haber hecho aún el exec (su cmdline
	// es todavía la de este test): se espera a que sea el script.
	for i := 0; i < 200 && !launcherAlive(fpid); i++ {
		time.Sleep(5 * time.Millisecond)
	}
	if !launcherAlive(fpid) {
		t.Fatal("the fake launcher never showed up as `launch`")
	}

	old := frozenComms
	frozenComms = map[string]bool{"sleep": true}
	defer func() { frozenComms = old; os.Remove(frozenPath) }()
	content := "launcher " + strconv.Itoa(fpid) + "\n" + strconv.Itoa(vpid) + "\n"
	if err := os.WriteFile(frozenPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = syscall.Kill(vpid, syscall.SIGSTOP)
	if !waitState(vpid, func(s string) bool { return s == "T" }) {
		t.Fatal("could not stop the test process")
	}
	thawLeftovers()
	time.Sleep(50 * time.Millisecond)
	if procState(vpid) != "T" {
		t.Errorf("thawed while the launcher is alive")
	}
	if _, err := os.Stat(frozenPath); err != nil {
		t.Errorf("%s removed while the launcher is alive", frozenPath)
	}
	fake.Process.Kill()
	fake.Wait()
	thawLeftovers()
	if !waitState(vpid, func(s string) bool { return s != "T" }) {
		t.Errorf("not thawed after the launcher died")
	}
}
