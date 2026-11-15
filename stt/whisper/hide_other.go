//go:build !windows

package whisper

import (
	"os"
	"os/exec"
	"runtime"
)

func hideWindow(*exec.Cmd) {}

// defaultRecorder records with arecord on Linux and sox's rec on macOS,
// 16 kHz mono 16 bit, as whisper takes it.
func defaultRecorder() Recorder {
	switch runtime.GOOS {
	case "linux":
		return &CommandRecorder{Name: "arecord", Args: []string{"-q", "-f", "S16_LE", "-r", "16000", "-c", "1", "-t", "wav"}}
	case "darwin":
		return &CommandRecorder{Name: "rec", Args: []string{"-q", "-r", "16000", "-c", "1", "-b", "16"}}
	}
	return nil
}

// interrupt ends a recorder process the way Ctrl-C does, so it finishes
// the WAV header.
func interrupt(p *os.Process) error { return p.Signal(os.Interrupt) }
