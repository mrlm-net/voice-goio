//go:build !windows

package whisper

import voicegoio "github.com/mrlm-net/voice-goio"

// InputDevices lists the microphones; only on Windows, where whisper
// records itself. Elsewhere the recorder tool picks its input.
func InputDevices() ([]voicegoio.Device, error) { return nil, voicegoio.ErrNotImplemented }

// MicRecorder is the platform's recorder; device is ignored away from
// Windows (arecord and sox record the default input).
func MicRecorder(device string) Recorder { return defaultRecorder() }
