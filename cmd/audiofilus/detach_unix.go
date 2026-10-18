//go:build !windows

package main

import "syscall"

// detachAttr pone el daemon en una sesión nueva (setsid).
func detachAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }
