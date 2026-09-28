// Package jsonl writes the JSON-lines protocol the piper sidecar reads on
// stdin: one compact object per line, flushed immediately so the child starts
// synthesising without waiting for a buffer to fill.
package jsonl

import (
	"bufio"
	"encoding/json"
	"io"
	"sync"
)

// Line is one synthesis request. Fields are pointers so that a model which does
// not accept per-line prosody simply never sees the keys; piper ignores
// unknown keys but rejects a null speaker_id on a single speaker model.
type Line struct {
	Text        string   `json:"text"`
	SpeakerID   *int     `json:"speaker_id,omitempty"`
	LengthScale *float32 `json:"length_scale,omitempty"`
	NoiseScale  *float32 `json:"noise_scale,omitempty"`
	NoiseW      *float32 `json:"noise_w,omitempty"`
	OutputFile  string   `json:"output_file,omitempty"`
}

// Writer serialises concurrent writers onto one child's stdin.
type Writer struct {
	mu  sync.Mutex
	bw  *bufio.Writer
	enc *json.Encoder
}

// NewWriter wraps w. The caller keeps ownership of w.
func NewWriter(w io.Writer) *Writer {
	bw := bufio.NewWriter(w)
	enc := json.NewEncoder(bw) // json.Encoder already appends the newline
	return &Writer{bw: bw, enc: enc}
}

// Encode writes one line and flushes it.
func (w *Writer) Encode(l Line) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.enc.Encode(l); err != nil {
		return err
	}
	return w.bw.Flush()
}

// Int and Float32 are helpers for the optional fields.
func Int(v int) *int             { return &v }
func Float32(v float32) *float32 { return &v }
