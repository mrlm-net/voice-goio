//go:build !windows

package sapi

import (
	voicegoio "github.com/mrlm-net/voice-goio"
)

// Recognizer is the non Windows stub.
//
// SPEC.md 1 requires Windows-only implementations to exist as compiling stubs
// on every platform, so the application can depend on the module and write the
// wiring before the Windows work is done. New fails immediately rather than
// returning a recogniser that goes quiet at run time.
type Recognizer struct{}

// New always returns voicegoio.ErrNotImplemented away from Windows. Use
// stt/fake on the development machine.
func New(Options) (*Recognizer, error) { return nil, voicegoio.ErrNotImplemented }

func (r *Recognizer) SetCallsigns([]voicegoio.Callsign) error { return voicegoio.ErrNotImplemented }
func (r *Recognizer) Start() error                            { return voicegoio.ErrNotImplemented }
func (r *Recognizer) Stop() error                             { return voicegoio.ErrNotImplemented }
func (r *Recognizer) Results() <-chan voicegoio.Recognition   { return nil }
func (r *Recognizer) Close() error                            { return nil }

// InputDevices reports the microphones SAPI can use. Empty away from Windows.
func InputDevices() ([]voicegoio.Device, error) { return nil, voicegoio.ErrNotImplemented }

var _ voicegoio.STT = (*Recognizer)(nil)
