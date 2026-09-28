package wav

// Streaming writer, for recording a whole session to one file.
//
// The length of a RIFF stream lives in its header, which is at the front, so a
// writer that does not know how long the recording will be has to go back and
// patch the sizes when it closes. That is why this is a file rather than an
// io.Writer: it needs to seek.

import (
	"encoding/binary"
	"fmt"
	"os"
	"sync"
)

// Writer appends 16 bit mono PCM to a WAVE file and fixes up the header on
// Close. It is safe for concurrent use.
type Writer struct {
	mu      sync.Mutex
	f       *os.File
	rate    int
	samples int
	closed  bool
}

// Create starts a new recording.
func Create(path string, sampleRate int) (*Writer, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	// A placeholder header, rewritten by Close once the length is known.
	if _, err := f.Write(Header(0, sampleRate, 1)); err != nil {
		f.Close()
		return nil, err
	}
	return &Writer{f: f, rate: sampleRate}, nil
}

// Append writes samples to the end of the recording.
func (w *Writer) Append(pcm []int16) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return fmt.Errorf("wav: recording is closed")
	}
	buf := make([]byte, len(pcm)*2)
	for i, s := range pcm {
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(s))
	}
	if _, err := w.f.Write(buf); err != nil {
		return err
	}
	w.samples += len(pcm)
	return nil
}

// AppendSilence writes a gap, which is what keeps consecutive transmissions
// from running into each other in the recording.
func (w *Writer) AppendSilence(d int) error {
	if d <= 0 {
		return nil
	}
	return w.Append(make([]int16, d))
}

// Samples reports how much has been recorded.
func (w *Writer) Samples() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.samples
}

// Duration reports the recording length in seconds.
func (w *Writer) Duration() float64 {
	if w.rate == 0 {
		return 0
	}
	return float64(w.Samples()) / float64(w.rate)
}

// Close patches the header with the real lengths and closes the file.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	if _, err := w.f.Seek(0, 0); err != nil {
		w.f.Close()
		return err
	}
	if _, err := w.f.Write(Header(w.samples, w.rate, 1)); err != nil {
		w.f.Close()
		return err
	}
	return w.f.Close()
}
