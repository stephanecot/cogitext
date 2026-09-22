//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// Équivalent Windows : le processus de fond ne doit ni ouvrir de console ni être tué
// avec le hook.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008 | 0x08000000}
}
