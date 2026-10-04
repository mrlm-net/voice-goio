package piper_test

import (
	"context"
	"os"
	"testing"
	"time"

	voicegoio "github.com/mrlm-net/voice-goio"
	"github.com/mrlm-net/voice-goio/tts/piper"
)

// TestLiveMultiSentence, with VOICEGOIO_PIPER set to a real piper (and
// VOICEGOIO_VOICES to its models, else the default folder): a text of
// several sentences is said whole, not only its first sentence.
func TestLiveMultiSentence(t *testing.T) {
	bin := os.Getenv("VOICEGOIO_PIPER")
	if bin == "" {
		t.Skip("VOICEGOIO_PIPER not set")
	}
	tts, err := piper.New(piper.Options{PiperPath: bin, VoicesDir: os.Getenv("VOICEGOIO_VOICES"), Timeout: 30 * time.Second, IdleGap: 150 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer tts.Close()
	p := voicegoio.VoiceProfile{Model: "en_GB-vctk-medium", SpeakerID: 10}
	one, err := tts.Synthesize(context.Background(), p, "Ladies and gentlemen, welcome aboard.")
	if err != nil {
		t.Fatal(err)
	}
	all, err := tts.Synthesize(context.Background(), p, "Ladies and gentlemen, welcome aboard. This is your captain speaking. We are now cruising at flight level three seven zero. The weather in Hurghada is sunny. Please keep your seatbelts fastened while seated.")
	if err != nil {
		t.Fatal(err)
	}
	rate := tts.SampleRate(p)
	t.Logf("first sentence %.1f s, all five %.1f s", float64(len(one))/float64(rate), float64(len(all))/float64(rate))
	if len(all) < 4*len(one) { // about five times
		t.Errorf("five sentences %d samples, the first alone %d: cut short", len(all), len(one))
	}
}
