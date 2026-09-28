// Package tts selects a synthesis backend.
//
// The application should not have to know which backends exist on which
// platform; it asks for a TTS and gets the best one available. On Windows that
// is always piper. On macOS it is piper if the binary and voices have been
// downloaded, otherwise `say`, so the pipeline can be heard before anything has
// been installed. The deterministic generator is the last resort and is what CI
// runs against.
package tts

import (
	"fmt"
	"runtime"

	voicegoio "github.com/mrlm-net/voice-goio"
	"github.com/mrlm-net/voice-goio/tts/fake"
	"github.com/mrlm-net/voice-goio/tts/piper"
	"github.com/mrlm-net/voice-goio/tts/say"
)

// Backend names, as reported by Open and accepted by Options.Prefer.
const (
	BackendPiper = "piper"
	BackendSay   = "say"
	BackendFake  = "fake"
	BackendAuto  = ""
)

// Options configures backend selection.
type Options struct {
	// Prefer forces one backend; empty tries them in order.
	Prefer string
	// Piper configures the piper backend when it is chosen.
	Piper piper.Options
	// Say configures the macOS development backend.
	Say say.Options
	// Fake configures the deterministic generator.
	Fake fake.Options
}

// Open returns a backend and the name of the one that was chosen.
func Open(opt Options) (voicegoio.TTS, string, error) {
	try := func(name string) (voicegoio.TTS, error) {
		switch name {
		case BackendPiper:
			p, err := piper.New(opt.Piper)
			if err != nil {
				return nil, err
			}
			if err := p.Available(); err != nil {
				p.Close()
				return nil, err
			}
			return p, nil
		case BackendSay:
			return say.New(opt.Say)
		case BackendFake:
			return fake.New(opt.Fake), nil
		default:
			return nil, fmt.Errorf("tts: unknown backend %q", name)
		}
	}

	if opt.Prefer != BackendAuto {
		t, err := try(opt.Prefer)
		return t, opt.Prefer, err
	}

	order := []string{BackendPiper, BackendFake}
	if runtime.GOOS == "darwin" {
		order = []string{BackendPiper, BackendSay, BackendFake}
	}
	var errs []error
	for _, name := range order {
		t, err := try(name)
		if err == nil {
			return t, name, nil
		}
		errs = append(errs, fmt.Errorf("%s: %w", name, err))
	}
	return nil, "", fmt.Errorf("tts: no backend available: %v", errs)
}
