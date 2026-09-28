// Package userdir resolves the per-user directory voice models are stored in.
//
// It exists so the voices package and the piper backend cannot disagree about
// where models live. They previously each had their own idea, and an empty
// Dir in one of them meant "the current working directory", which quietly
// downloaded sixty megabytes of model into whatever directory the tool
// happened to be run from.
package userdir

import (
	"os"
	"path/filepath"
	"runtime"
)

// App is the directory name used under the platform's data directory.
const App = "voice-goio"

// Voices is where downloaded piper models live:
//
//	%LOCALAPPDATA%\voice-goio\voices              on Windows
//	~/Library/Application Support/voice-goio/voices on macOS
//	$XDG_DATA_HOME/voice-goio/voices              elsewhere
func Voices() string {
	switch runtime.GOOS {
	case "windows":
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			return filepath.Join(d, App, "voices")
		}
	case "darwin":
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, "Library", "Application Support", App, "voices")
		}
	default:
		if d := os.Getenv("XDG_DATA_HOME"); d != "" {
			return filepath.Join(d, App, "voices")
		}
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, ".local", "share", App, "voices")
		}
	}
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, App, "voices")
	}
	return filepath.Join(".", "voices")
}
