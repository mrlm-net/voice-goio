package whisper

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// Recorder captures the microphone between Start and Stop into a WAV file
// (16 kHz mono 16 bit), which Stop returns; the recogniser deletes it.
type Recorder interface {
	Start() error
	Stop() (wav string, err error)
}

// CommandRecorder records with a command line tool that writes a WAV to
// the file named last on its command line until interrupted: arecord,
// sox's rec.
type CommandRecorder struct {
	Name string
	Args []string

	mu   sync.Mutex
	cmd  *exec.Cmd
	path string
	done chan error
}

// Start runs the tool into a new temporary file.
func (c *CommandRecorder) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cmd != nil {
		return errors.New("whisper: already recording")
	}
	bin, err := exec.LookPath(c.Name)
	if err != nil {
		return fmt.Errorf("whisper: recorder %s: %w", c.Name, err)
	}
	f, err := os.CreateTemp("", "voicegoio-ptt-*.wav")
	if err != nil {
		return err
	}
	f.Close()
	cmd := exec.Command(bin, append(append([]string(nil), c.Args...), f.Name())...)
	hideWindow(cmd)
	if err := cmd.Start(); err != nil {
		os.Remove(f.Name())
		return fmt.Errorf("whisper: recorder: %w", err)
	}
	c.cmd, c.path, c.done = cmd, f.Name(), make(chan error, 1)
	go func() { c.done <- cmd.Wait() }()
	return nil
}

// Stop interrupts the tool and returns its file once it has finished
// writing (killed after 2 s).
func (c *CommandRecorder) Stop() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cmd == nil {
		return "", errors.New("whisper: not recording")
	}
	cmd, path, done := c.cmd, c.path, c.done
	c.cmd = nil
	_ = interrupt(cmd.Process)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		cmd.Process.Kill()
		<-done
	}
	if st, err := os.Stat(path); err != nil || st.Size() <= 44 {
		os.Remove(path)
		return "", fmt.Errorf("whisper: %s recorded nothing", filepath.Base(c.Name))
	}
	return path, nil
}
