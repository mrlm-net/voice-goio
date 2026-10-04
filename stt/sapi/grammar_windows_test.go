//go:build windows

package sapi

import (
	"errors"
	"testing"

	voicegoio "github.com/mrlm-net/voice-goio"
	"github.com/mrlm-net/voice-goio/grammar"
)

// An application's own grammar (grammar.Commands) compiles in SAPI and takes
// a callsign roster. Skipped where Windows has no English recogniser or no
// audio input (CI runners).
func TestApplicationGrammar(t *testing.T) {
	if testing.Short() {
		t.Skip("starts the speech engine")
	}
	g, err := grammar.Commands([]grammar.Command{
		{Intent: "request_taxi", Phrases: []string{"request taxi"}},
		{Intent: "gear_up", Phrases: []string{"gear up", "landing gear up"}},
		{Intent: "doors_closed", Phrases: []string{"doors closed"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	r, err := New(Options{Grammar: g})
	if errors.Is(err, ErrNoSpeechPack) || errors.Is(err, ErrNoAudioInput) {
		t.Skip(err)
	}
	if err != nil {
		t.Fatalf("grammar not loaded: %v", err)
	}
	defer r.Close()
	if err := r.SetCallsigns([]voicegoio.Callsign{{ICAO: "CSA123"}}); err != nil {
		t.Errorf("SetCallsigns: %v", err)
	}
}
