//go:build windows

package piper

import (
	"os/exec"
	"testing"
)

// TestHideConsole: piper is started without a console window.
func TestHideConsole(t *testing.T) {
	cmd := exec.Command("piper.exe")
	hideConsole(cmd)
	if cmd.SysProcAttr == nil || cmd.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Errorf("SysProcAttr %+v: no CREATE_NO_WINDOW", cmd.SysProcAttr)
	}
}
