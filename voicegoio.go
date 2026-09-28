// Package voicegoio is an offline voice input/output library for an MSFS ATC
// application.
//
// It is the compatibility surface of the module: this file declares the
// interfaces and value types the consuming application sees. Everything else in
// the module is an implementation of one of these interfaces, in a subpackage
// that imports this one. Nothing in this package imports a subpackage, so the
// application can compile against the API long before a given backend exists.
//
// Design constraints (see docs/SPEC.md):
//   - no third party Go modules: go.mod has zero requires, stdlib only;
//   - no cgo: every build is CGO_ENABLED=0, native APIs are reached through
//     syscall (Windows) or subprocesses;
//   - runtime target is Windows 10/11 x64, development happens on macOS.
//
// The application deals in text and semantic tags only. It never sees PCM,
// grammars or voice model files.
package voicegoio

import (
	"context"
	"errors"
)

// ErrNotImplemented is returned by a backend that exists on this platform only
// as a compiling stub. Callers should treat it as "pick another backend", not
// as a failure of the transmission.
var ErrNotImplemented = errors.New("voicegoio: not implemented on this platform")

// ErrClosed is returned once a component has been closed.
var ErrClosed = errors.New("voicegoio: component is closed")

// Phraseology selects the spoken conventions used by the normaliser and
// accepted by the recogniser.
type Phraseology string

const (
	// ICAO phraseology: "tree", "fife", "niner", "decimal", QNH in hectopascals.
	ICAO Phraseology = "icao"
	// FAA phraseology: "three", "five", "niner", "point", altimeter in inHg.
	FAA Phraseology = "faa"
)

// ControllerKind identifies the position a controller works, which selects a
// radio profile and weights voice assignment.
type ControllerKind string

const (
	Tower    ControllerKind = "tower"
	Ground   ControllerKind = "ground"
	Approach ControllerKind = "approach"
	Center   ControllerKind = "center"
	ATIS     ControllerKind = "atis"
)

// Transmission is one ATC message handed to the library by the application.
//
// Text carries the application's normal tokens ("BAW123 climb FL350, QNH 1013")
// and is normalised by the library before synthesis. Frequency and ControllerID
// are opaque to the library except as a queue key and as the identity reported
// back on PlaybackEvent.
type Transmission struct {
	Frequency    string // "127.450"
	ControllerID string // "LKPR_TWR"
	Phraseology  Phraseology
	Text         string
}

// Callsign describes one aircraft on frequency. Spoken is the phonetic form fed
// to the recogniser's dynamic rule; when empty the library fills it from the
// telephony table.
type Callsign struct {
	ICAO   string // "BAW123", "OK-ABC"
	Spoken string // "Speedbird one two three"
}

// Recognition is one result per push-to-talk cycle.
//
// Tags carries the semantic properties extracted by the grammar. Well known
// keys are "intent", "callsign" and "value"; an unrecognised or low confidence
// transmission is reported as intent "say_again".
type Recognition struct {
	Text       string
	Confidence float32
	Tags       map[string]string
}

// Well known tag keys and the intent used for unusable input.
const (
	TagIntent   = "intent"
	TagCallsign = "callsign"
	TagValue    = "value"
	TagUnit     = "unit"

	IntentSayAgain = "say_again"
)

// VoiceProfile identifies one controller voice: a piper model, a speaker inside
// that model, its prosody, and the radio profile applied after synthesis.
type VoiceProfile struct {
	Model       string  // "en_GB-vctk-medium"
	SpeakerID   int     // multi speaker models only
	LengthScale float32 // 0.80-0.95, lower is faster
	NoiseScale  float32 // 0.5-0.7
	NoiseW      float32 // 0.6-0.9
	Radio       string  // radio profile name: tower|ground|approach|center|atis
	// Accent is the accent this voice was assigned for, such as "en-GB" or
	// "en-IN". It is informational for the application, and backends that do
	// not use piper models use it to choose an equivalent local voice.
	Accent string
}

// TTS synthesises already normalised text into mono 16 bit PCM.
//
// Implementations are safe for concurrent use; the piper backend multiplexes
// onto a pool of sidecar processes.
type TTS interface {
	// Synthesize returns 16 bit mono PCM at SampleRate(profile).
	Synthesize(ctx context.Context, profile VoiceProfile, spokenText string) ([]int16, error)
	// SampleRate reports the native rate of the model behind profile.
	SampleRate(profile VoiceProfile) int
	Close() error
}

// STT is a push-to-talk recogniser restricted to the aircraft on frequency.
type STT interface {
	// SetCallsigns rebuilds the dynamic callsign rule. Call on every roster
	// change; restricting the grammar is the main accuracy lever.
	SetCallsigns(cs []Callsign) error
	// Start is called when PTT is pressed.
	Start() error
	// Stop is called when PTT is released. Exactly one Recognition follows on
	// Results, possibly with intent "say_again".
	Stop() error
	Results() <-chan Recognition
	Close() error
}

// Device is one audio endpoint reported by the platform.
type Device struct {
	ID      string
	Name    string
	Default bool
}

// PlaybackEvent brackets one transmission so the application can hold the next
// call until the frequency is clear.
type PlaybackEvent struct {
	Frequency    string
	ControllerID string
	Started      bool // true = started, false = finished
}

// Player owns the output device and one queue per frequency.
type Player interface {
	Devices() ([]Device, error)
	SetDevice(id string) error
	// Play enqueues pcm behind anything already queued for t.Frequency.
	Play(t Transmission, pcm []int16, sampleRate int) error
	Events() <-chan PlaybackEvent
	Close() error
}

// Normaliser converts application text into spoken form. The concrete
// implementation lives in normalise; the interface is here so the pipeline can
// be assembled from this package alone.
type Normaliser interface {
	Spoken(text string, ph Phraseology) string
	SpokenCallsign(icao string) string
}
