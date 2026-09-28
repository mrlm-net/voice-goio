package audio_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	voicegoio "github.com/mrlm-net/voice-goio"
	"github.com/mrlm-net/voice-goio/audio"
)

// newPlayer forces the WAV sink so the tests never touch a real device, which
// is what lets them run identically on macOS, Windows and CI.
func newPlayer(t *testing.T) (*audio.Player, string) {
	t.Helper()
	dir := t.TempDir()
	p, err := audio.NewPlayer(audio.Options{WAVDir: dir, SampleRate: 22050})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	return p, dir
}

func tone(n int) []int16 {
	out := make([]int16, n)
	for i := range out {
		out[i] = int16(i % 1000)
	}
	return out
}

// Every transmission is bracketed by a Started and a Finished event. The
// application relies on Finished to know the frequency is clear before it
// sends the next call, so a missing event means overlapping controllers.
func TestPlaybackEventsBracketEachTransmission(t *testing.T) {
	p, _ := newPlayer(t)
	for i := range 3 {
		tx := voicegoio.Transmission{Frequency: "118.100", ControllerID: "LKPR_TWR", Text: "test"}
		if err := p.Play(tx, tone(2205), 22050); err != nil {
			t.Fatalf("play %d: %v", i, err)
		}
	}
	for i := range 3 {
		started := waitEvent(t, p)
		if !started.Started || started.Frequency != "118.100" {
			t.Errorf("transmission %d: expected a Started event, got %+v", i, started)
		}
		finished := waitEvent(t, p)
		if finished.Started || finished.ControllerID != "LKPR_TWR" {
			t.Errorf("transmission %d: expected a Finished event, got %+v", i, finished)
		}
	}
}

// Each frequency has its own queue, so a busy tower never delays ground.
func TestFrequenciesQueueIndependently(t *testing.T) {
	p, dir := newPlayer(t)
	for _, f := range []string{"118.100", "121.905", "118.100"} {
		tx := voicegoio.Transmission{Frequency: f, ControllerID: "X", Text: "t"}
		if err := p.Play(tx, tone(441), 22050); err != nil {
			t.Fatal(err)
		}
	}
	for range 6 {
		waitEvent(t, p)
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.wav"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Errorf("wrote %d files, want 3", len(files))
	}
}

// A frequency that is saturated reports congestion rather than growing without
// bound: the application decides what to drop, not the library.
func TestCongestedFrequencyReportsError(t *testing.T) {
	dir := t.TempDir()
	p, err := audio.NewPlayer(audio.Options{WAVDir: dir, SampleRate: 22050, QueueDepth: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	tx := voicegoio.Transmission{Frequency: "127.450", ControllerID: "CTR", Text: "t"}
	var lastErr error
	for range 20 {
		if err := p.Play(tx, tone(22050), 22050); err != nil {
			lastErr = err
			break
		}
	}
	if lastErr == nil {
		t.Error("expected a congestion error once the queue filled")
	} else {
		t.Logf("congestion reported: %v", lastErr)
	}
}

// Playback at a different rate than the device is resampled rather than
// played at the wrong pitch.
func TestRateConversion(t *testing.T) {
	p, dir := newPlayer(t)
	tx := voicegoio.Transmission{Frequency: "118.100", ControllerID: "TWR", Text: "t"}
	if err := p.Play(tx, tone(4800), 48000); err != nil {
		t.Fatal(err)
	}
	waitEvent(t, p)
	waitEvent(t, p)

	files, _ := filepath.Glob(filepath.Join(dir, "*.wav"))
	if len(files) != 1 {
		t.Fatalf("wrote %d files, want 1", len(files))
	}
	st, err := os.Stat(files[0])
	if err != nil {
		t.Fatal(err)
	}
	// 4800 samples at 48 kHz is 100 ms, which is 2205 samples at 22.05 kHz.
	if got := (st.Size() - 44) / 2; got < 2100 || got > 2300 {
		t.Errorf("wrote %d samples, want about 2205 after resampling", got)
	}
}

func TestClosedPlayerRejects(t *testing.T) {
	p, _ := newPlayer(t)
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	tx := voicegoio.Transmission{Frequency: "118.100"}
	if err := p.Play(tx, tone(100), 22050); err != voicegoio.ErrClosed {
		t.Errorf("Play after Close = %v, want ErrClosed", err)
	}
	if err := p.Close(); err != nil {
		t.Errorf("second Close = %v, want nil", err)
	}
}

// SetDevice is a no-op on platforms that cannot route audio, so settings code
// needs no platform switch.
func TestDeviceEnumeration(t *testing.T) {
	p, _ := newPlayer(t)
	devs, err := p.Devices()
	if err != nil {
		t.Fatal(err)
	}
	if len(devs) == 0 {
		t.Error("no output devices reported")
	}
	if err := p.SetDevice(devs[0].ID); err != nil {
		t.Errorf("SetDevice(%q) = %v", devs[0].ID, err)
	}
}

func waitEvent(t *testing.T, p *audio.Player) voicegoio.PlaybackEvent {
	t.Helper()
	select {
	case ev := <-p.Events():
		return ev
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a playback event")
		return voicegoio.PlaybackEvent{}
	}
}
