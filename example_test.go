package voicegoio_test

// The wiring the consuming application does, compiled on every build so the
// README cannot drift away from the API.
//
// These are Example functions without an "Output:" comment, which the go tool
// compiles but does not run — they touch audio devices and subprocesses, so
// running them in CI would be a different kind of test.

import (
	"context"
	"fmt"
	"log"

	voicegoio "github.com/mrlm-net/voice-goio"
	"github.com/mrlm-net/voice-goio/audio"
	"github.com/mrlm-net/voice-goio/audio/radio"
	"github.com/mrlm-net/voice-goio/grammar"
	"github.com/mrlm-net/voice-goio/normalise"
	"github.com/mrlm-net/voice-goio/speaker"
	"github.com/mrlm-net/voice-goio/stt/fake"
	"github.com/mrlm-net/voice-goio/stt/sapi"
	"github.com/mrlm-net/voice-goio/tts"
	"github.com/mrlm-net/voice-goio/voices"
)

// Example_speak assembles the output half and speaks one transmission.
func Example_speak() {
	const sessionSeed = 20260928

	engine, backend, err := tts.Open(tts.Options{})
	if err != nil {
		log.Fatal(err)
	}
	defer engine.Close()
	fmt.Println("tts backend:", backend)

	man, err := voices.LoadDefault()
	if err != nil {
		log.Fatal(err)
	}
	pool := voices.NewPool(man, voices.PoolOptions{Seed: sessionSeed, AllowUnaudited: true})
	chain := radio.Default()
	norm := normalise.New()

	player, err := audio.NewPlayer(audio.Options{})
	if err != nil {
		log.Fatal(err)
	}
	defer player.Close()

	// The application must drain playback events: Finished is how it knows the
	// frequency is clear for the next call.
	go func() {
		for ev := range player.Events() {
			if !ev.Started {
				fmt.Printf("%s finished on %s\n", ev.ControllerID, ev.Frequency)
			}
		}
	}()

	// A controller keeps this voice for the whole session.
	voice := pool.Assign("EGLL", voicegoio.Tower)

	tx := voicegoio.Transmission{
		Frequency:    "118.500",
		ControllerID: "EGLL_TWR",
		Phraseology:  voicegoio.ICAO,
		Text:         "BAW123 RWY 27L cleared to land, wind 250 degrees 8 kt",
	}

	pcm, err := engine.Synthesize(context.Background(), voice, norm.Spoken(tx.Text, tx.Phraseology))
	if err != nil {
		log.Fatal(err)
	}
	out := chain.Apply(pcm, engine.SampleRate(voice), voice.Radio, player.SampleRate(), sessionSeed)
	if err := player.Play(tx, out, player.SampleRate()); err != nil {
		log.Fatal(err)
	}
}

// Example_listen assembles the input half. The Windows application swaps
// fake.FromStdin for sapi.New; nothing else changes, because both satisfy
// voicegoio.STT.
func Example_listen() {
	var rec voicegoio.STT = fake.FromStdin()
	defer rec.Close()

	// Resend on every roster change: restricting the grammar to the aircraft
	// on frequency is the single largest accuracy lever there is.
	if err := rec.SetCallsigns([]voicegoio.Callsign{
		{ICAO: "BAW123"},
		{ICAO: "OK-ABC"},
	}); err != nil {
		log.Fatal(err)
	}

	if err := rec.Start(); err != nil { // push to talk pressed
		log.Fatal(err)
	}
	if err := rec.Stop(); err != nil { // released
		log.Fatal(err)
	}

	got := <-rec.Results() // exactly one result per cycle
	switch got.Tags[voicegoio.TagIntent] {
	case "readback_altitude":
		fmt.Printf("%s read back %s\n", got.Tags[voicegoio.TagCallsign], got.Tags[voicegoio.TagValue])
	case "mayday", "pan_pan":
		fmt.Printf("%s declared an emergency: %s\n", got.Tags[voicegoio.TagCallsign], got.Tags[voicegoio.TagValue])
	case voicegoio.IntentSayAgain:
		fmt.Println("not understood; the controller should say again")
	}
}

// Example_devices is what a settings screen needs.
func Example_devices() {
	player, err := audio.NewPlayer(audio.Options{})
	if err != nil {
		log.Fatal(err)
	}
	defer player.Close()

	devices, err := player.Devices()
	if err != nil {
		log.Fatal(err)
	}
	for _, d := range devices {
		fmt.Printf("%-8s %s (default=%v)\n", d.ID, d.Name, d.Default)
	}
	// Selecting a device is a no-op on platforms that cannot route audio, so
	// this needs no platform switch.
	if len(devices) > 0 {
		if err := player.SetDevice(devices[0].ID); err != nil {
			log.Fatal(err)
		}
	}
}

// Example_intercom is the crew on the intercom, each in the voice the player
// chose: no radio chain, whatever frequency is followed, even with the radio
// off.
func Example_intercom() {
	man, err := voices.LoadDefault()
	if err != nil {
		log.Fatal(err)
	}
	// A voice picker lists what is installed in the per-user folder.
	installed := voices.Installed(man, voices.Dir())
	if len(installed) == 0 {
		log.Fatalf("no voices in %s", voices.Dir())
	}
	copilot := installed[0].Profile(0)

	sp := speaker.New(speaker.Options{})
	defer sp.Close()
	sp.SayIntercom("Before start checklist complete", copilot)

	// The same as an Utterance; Voice also picks the voice of a radio call.
	sp.Hear(speaker.Utterance{Intercom: true, Position: "purser", Text: "Cabin secure", Voice: &copilot})
}

// Example_commands listens for the application's own phrases instead of the
// ATC grammar.
func Example_commands() {
	cmds := []grammar.Command{
		{Intent: "request_taxi", Phrases: []string{"request taxi"}},
		{Intent: "gear_up", Phrases: []string{"gear up", "landing gear up"}},
		{Intent: "doors_closed", Phrases: []string{"doors closed"}},
	}
	g, err := grammar.Commands(cmds)
	if err != nil {
		log.Fatal(err)
	}
	rec, err := sapi.New(sapi.Options{Grammar: g}) // Windows
	if err != nil {
		log.Fatal(err)
	}
	defer rec.Close()
	rec.Start() // push to talk pressed
	rec.Stop()  // released

	got := <-rec.Results()                     // exactly one result per cycle
	fmt.Println(got.Tags[voicegoio.TagIntent]) // "gear_up", or say_again

	// Typed, or away from Windows: the same phrases, no engine.
	intent, ok := grammar.Match(cmds, "Gear up!")
	fmt.Println(intent, ok)
}
