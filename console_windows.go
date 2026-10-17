//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// createNoWindow keeps a console program from flashing a window.
const createNoWindow = 0x08000000

// hideConsole runs a console program without showing a console window.
func hideConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
}
