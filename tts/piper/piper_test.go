package piper_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	voicegoio "github.com/mrlm-net/voice-goio"
	"github.com/mrlm-net/voice-goio/tts/piper"
)

const samplesPerWord = 1000 // must match testdata/fakepiper

// buildFakePiper compiles the stand-in sidecar. CI never downloads the real
// piper, so this is what keeps the JSON lines protocol under test.
func buildFakePiper(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "fakepiper")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, filepath.Join("..", "..", "testdata", "fakepiper", "main.go"))
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fakepiper: %v\n%s", err, out)
	}
	return bin
}

// fakeVoices writes the file pair the resolver expects. The .onnx is never
// read by the fake sidecar; the .onnx.json is, by SampleRate.
func fakeVoices(t *testing.T, models ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, m := range models {
		sub := filepath.Join(dir, "en", m)
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		onnx := filepath.Join(sub, m+".onnx")
		if err := os.WriteFile(onnx, []byte("not a real model"), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg := `{"audio":{"sample_rate":22050},"num_speakers":109}`
		if err := os.WriteFile(onnx+".json", []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func newTTS(t *testing.T, poolSize int, models ...string) *piper.TTS {
	t.Helper()
	tts, err := piper.New(piper.Options{
		PiperPath: buildFakePiper(t),
		VoicesDir: fakeVoices(t, models...),
		PoolSize:  poolSize,
		Timeout:   10 * time.Second,
		IdleGap:   150 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tts.Close() })
	return tts
}

// The sentinel trick is the subtle part of this backend: piper marks no
// utterance boundary, so we append a known word and cut its samples back off.
// If the trim is wrong, every transmission ends with a stray "dot".
func TestSynthesizeTrimsSentinel(t *testing.T) {
	tts := newTTS(t, 4, "en_GB-vctk-medium")
	p := voicegoio.VoiceProfile{Model: "en_GB-vctk-medium", SpeakerID: 42, LengthScale: 0.9, Radio: "tower"}

	pcm, err := tts.Synthesize(context.Background(), p, "Speedbird one two three descend flight level one zero zero")
	if err != nil {
		t.Fatal(err)
	}
	if want := 10 * samplesPerWord; len(pcm) != want { // 10 words in
		t.Errorf("got %d samples, want %d (sentinel not trimmed cleanly?)", len(pcm), want)
	}

	// A second utterance on the same warm process must not inherit the first.
	pcm2, err := tts.Synthesize(context.Background(), p, "roger")
	if err != nil {
		t.Fatal(err)
	}
	if want := samplesPerWord; len(pcm2) != want {
		t.Errorf("second utterance: got %d samples, want %d", len(pcm2), want)
	}
}

func TestSampleRateFromModelConfig(t *testing.T) {
	tts := newTTS(t, 4, "en_US-ryan-medium")
	if got := tts.SampleRate(voicegoio.VoiceProfile{Model: "en_US-ryan-medium"}); got != 22050 {
		t.Errorf("SampleRate = %d, want 22050", got)
	}
	if got := tts.SampleRate(voicegoio.VoiceProfile{Model: "nonexistent"}); got != piper.DefaultSampleRate {
		t.Errorf("unknown model SampleRate = %d, want %d", got, piper.DefaultSampleRate)
	}
}

// Concurrency matters: one controller talking on tower while another reads an
// ATIS must not interleave two utterances on one process.
func TestConcurrentSynthesis(t *testing.T) {
	tts := newTTS(t, 2, "en_GB-vctk-medium", "en_US-ryan-medium")
	models := []string{"en_GB-vctk-medium", "en_US-ryan-medium"}
	errs := make(chan error, 8)
	for i := range 8 {
		go func() {
			p := voicegoio.VoiceProfile{Model: models[i%2], SpeakerID: i}
			pcm, err := tts.Synthesize(context.Background(), p, "one two three")
			if err == nil && len(pcm) != 3*samplesPerWord {
				t.Errorf("got %d samples, want %d", len(pcm), 3*samplesPerWord)
			}
			errs <- err
		}()
	}
	for range 8 {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
}

func TestUnknownModelIsAnActionableError(t *testing.T) {
	tts := newTTS(t, 4, "en_GB-vctk-medium")
	_, err := tts.Synthesize(context.Background(), voicegoio.VoiceProfile{Model: "de_DE-thorsten-medium"}, "test")
	if err == nil {
		t.Fatal("want an error for a model that was never downloaded")
	}
	t.Logf("error: %v", err)
}

func TestClosedPoolRejects(t *testing.T) {
	tts := newTTS(t, 4, "en_GB-vctk-medium")
	tts.Close()
	if _, err := tts.Synthesize(context.Background(), voicegoio.VoiceProfile{Model: "en_GB-vctk-medium"}, "test"); err == nil {
		t.Fatal("want ErrClosed after Close")
	}
}
