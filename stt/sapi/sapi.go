// Package sapi is the Windows speech recognition backend.
//
// It drives SAPI 5's in-process recognizer (CLSID_SpInprocRecognizer) through
// raw COM vtable calls made with syscall.SyscallN. There is no cgo, no
// automation layer and no COM event sink: notification is a Win32 event handle
// that a goroutine waits on, which keeps the whole backend free of callbacks
// into Go from foreign threads.
//
// In-process rather than shared is deliberate. The shared recognizer is the one
// behind Windows Speech Recognition: it shows UI, it is trained per user, and
// it competes with other applications for the microphone. An in-process
// recognizer owns its own audio input and answers only to this grammar.
//
// Known risk, recorded in SPEC.md 4.4: this engine was deprecated in December
// 2023. It still ships in Windows 11 and has no announced removal date. It is
// isolated behind voicegoio.STT precisely so it can be replaced.
package sapi

import (
	"errors"
	"fmt"
)

// ErrNoSpeechPack is returned when Windows has no recognizer for an English
// locale installed. This is the single most common setup failure, so it is a
// typed error carrying the Settings path the user needs.
var ErrNoSpeechPack = errors.New(
	"sapi: no English speech recognition engine is installed; " +
		"add one under Settings > Time & language > Speech > Manage voices " +
		"(or Language & region > add English (United States) with the Speech feature)")

// ErrNoAudioInput is returned when SAPI reports no audio input token.
var ErrNoAudioInput = errors.New("sapi: no audio input device is available to the speech engine")

// Options configures the recogniser.
type Options struct {
	// GrammarPath is an SRGS XML file. Empty means the grammar embedded in the
	// grammar package, written to a temporary file (SAPI compiles XML only
	// from a file).
	GrammarPath string
	// Locale selects the recognizer token, e.g. "en-US" or "en-GB". Empty
	// accepts whichever English engine Windows offers.
	Locale string
	// AudioInputID selects a microphone by SAPI token id. Empty uses the
	// default input.
	AudioInputID string
	// MinConfidence below which a result is reported as say_again. 0 means
	// 0.5, per SPEC.md 4.4.
	MinConfidence float32
	// FinalResultTimeoutMS is how long Stop waits for the engine to deliver
	// the final result before giving up and emitting say_again. 0 means 800.
	FinalResultTimeoutMS int
}

func (o *Options) withDefaults() {
	if o.MinConfidence == 0 {
		o.MinConfidence = 0.5
	}
	if o.FinalResultTimeoutMS == 0 {
		o.FinalResultTimeoutMS = 800
	}
}

// hresultError carries a COM failure with the call that produced it.
type hresultError struct {
	call string
	hr   uintptr
}

func (e *hresultError) Error() string {
	return fmt.Sprintf("sapi: %s failed: HRESULT 0x%08X", e.call, uint32(e.hr))
}

// HRESULT returns the raw status code, so callers can special case
// SPERR_NOT_FOUND and friends.
func (e *hresultError) HRESULT() uint32 { return uint32(e.hr) }
