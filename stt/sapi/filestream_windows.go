//go:build windows

package sapi

// Recognition from a WAV file instead of a microphone.
//
// SPEC.md 4.5 wants one corpus of expected tags validated against both
// backends: the pure Go parser in `go test`, and the real engine by playing
// rendered WAVs into it. Without this the SAPI half of that corpus needs a
// person sitting at a headset saying forty phrases, which is not a regression
// test — it is an afternoon.
//
// SAPI supports it directly: ISpRecognizer::SetInput takes any object that
// implements the stream interfaces, and SpFileStream is one bound to a file.
// The engine then reads its audio from there and raises the same events it
// would for a microphone, so nothing else in this backend changes.

import (
	"fmt"
	"runtime"
	"unsafe"

	voicegoio "github.com/mrlm-net/voice-goio"
)

// SetInputFile points the recogniser at a WAV file. Every subsequent Start
// and Stop cycle reads from the file rather than from the microphone.
//
// The file must be 16 bit PCM. The engine is fussier about format than the
// playback side is: a rate it does not accept is rejected at BindToFile rather
// than producing silence, which is the failure mode to expect here.
func (r *Recognizer) SetInputFile(path string) error {
	return r.do(func() error {
		stream, err := coCreateInstance(&clsidSpFileStream, &iidISpStream)
		if err != nil {
			return fmt.Errorf("sapi: create file stream: %w", err)
		}
		p, keep := utf16(path)
		err = stream.call("ISpStream::BindToFile", spsBindToFile,
			up(p), spfmOpenReadOnly, 0, 0, 0)
		runtime.KeepAlive(keep)
		if err != nil {
			stream.release()
			return fmt.Errorf("sapi: bind %s: %w", path, err)
		}
		if err := r.rec.call("ISpRecognizer::SetInput", recSetInput, obj(stream), 1); err != nil {
			stream.release()
			return err
		}
		// The recognizer holds its own reference now.
		if r.input != nil {
			r.input.release()
		}
		r.input = stream
		return nil
	})
}

// SetInputMicrophone restores the live audio input after SetInputFile.
func (r *Recognizer) SetInputMicrophone() error {
	return r.do(func() error {
		if err := r.selectAudioInput(); err != nil {
			return err
		}
		if r.input != nil {
			r.input.release()
			r.input = nil
		}
		return nil
	})
}

// RecognizeFile is the whole cycle for one file: point the engine at it, run a
// push to talk cycle, and return the single result.
//
// The wait is generous because the engine reads the file as fast as it likes
// and raises SPEI_RECOGNITION when it has finished with it, which for a short
// transmission is quicker than real time but is not instant.
func (r *Recognizer) RecognizeFile(path string) (voicegoio.Recognition, error) {
	if err := r.SetInputFile(path); err != nil {
		return voicegoio.Recognition{}, err
	}
	if err := r.Start(); err != nil {
		return voicegoio.Recognition{}, err
	}
	if err := r.Stop(); err != nil {
		return voicegoio.Recognition{}, err
	}
	select {
	case rec := <-r.Results():
		return rec, nil
	case <-r.done:
		return voicegoio.Recognition{}, voicegoio.ErrClosed
	}
}

// interface assertion: the file input path uses the same unsafe helpers as the
// rest of the backend.
var _ = unsafe.Sizeof(0)
