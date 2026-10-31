//go:build windows

package piper

import (
	"os/exec"
	"syscall"
)

// createNoWindow is CREATE_NO_WINDOW: a console program started by a windowed
// application would otherwise open a console window of its own.
const createNoWindow = 0x08000000

// hideConsole keeps piper's console window from showing.
func hideConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
}
