//go:build !windows

package piper

import "os/exec"

// hideConsole does nothing: only Windows opens a console for a child process.
func hideConsole(*exec.Cmd) {}
