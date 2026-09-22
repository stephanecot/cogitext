//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// Détache le fetch de fond du groupe de processus du hook, pour qu'il survive à la
// fin du hook et ne lui renvoie jamais de signal.
func detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
