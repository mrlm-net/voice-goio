package wav_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/mrlm-net/voice-goio/internal/wav"
)

func TestRoundTrip(t *testing.T) {
	in := []int16{0, 1, -1, 32767, -32768, 1234}
	var buf bytes.Buffer
	if err := wav.Write(&buf, in, 22050); err != nil {
		t.Fatal(err)
	}
	out, rate, err := wav.Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if rate != 22050 {
		t.Errorf("rate = %d, want 22050", rate)
	}
	if len(out) != len(in) {
		t.Fatalf("read %d samples, want %d", len(out), len(in))
	}
	for i := range in {
		if out[i] != in[i] {
			t.Errorf("sample %d = %d, want %d", i, out[i], in[i])
		}
	}
}

// macOS `say` writes a JUNK chunk before fmt. A reader that assumes fmt comes
// first would fail on every file the development TTS backend produces.
func TestSkipsUnknownChunks(t *testing.T) {
	var body bytes.Buffer
	body.WriteString("JUNK")
	binary.Write(&body, binary.LittleEndian, uint32(4))
	body.Write([]byte{0, 0, 0, 0})

	var full bytes.Buffer
	pcm := []int16{10, 20, 30, 40}
	wav.Write(&full, pcm, 16000)
	raw := full.Bytes()

	spliced := append([]byte{}, raw[:12]...)
	spliced = append(spliced, body.Bytes()...)
	spliced = append(spliced, raw[12:]...)
	// Fix up the RIFF size so the file stays well formed.
	binary.LittleEndian.PutUint32(spliced[4:], uint32(len(spliced)-8))

	out, rate, err := wav.Read(bytes.NewReader(spliced))
	if err != nil {
		t.Fatal(err)
	}
	if rate != 16000 || len(out) != len(pcm) {
		t.Errorf("read %d samples at %d Hz, want %d at 16000", len(out), rate, len(pcm))
	}
}

func TestRejectsNonWAVE(t *testing.T) {
	if _, _, err := wav.Read(bytes.NewReader([]byte("not a wav file at all"))); err == nil {
		t.Error("expected an error for a non-RIFF stream")
	}
}
