//go:build !windows

package main

import "os/exec"

// hideConsole does nothing where processes never get a console window.
func hideConsole(*exec.Cmd) {}
