//go:build windows

package whisper

import (
	"os"
	"os/exec"
	"syscall"
)

// hideWindow keeps whisper-cli's console window from showing
// (CREATE_NO_WINDOW), as piper's.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
}

// defaultRecorder records the default microphone with winmm.
func defaultRecorder() Recorder { return &WaveInRecorder{} }

// interrupt ends a recorder process (no SIGINT on Windows).
func interrupt(p *os.Process) error { return p.Kill() }
