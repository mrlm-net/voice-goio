// Command demo runs the whole voice-goio pipeline end to end so it can be
// heard, not just tested.
//
// It stands in for the MSFS application: it owns a frequency, a roster of
// aircraft and a trivial controller that answers pilot transmissions. What it
// demonstrates is the integration contract from SPEC.md 3 — text in, tags out,
// audio handled entirely inside the library:
//
//	app text -> normalise -> TTS -> radio chain -> player -> playback events
//	pilot speech/typing -> STT -> Recognition{intent, callsign, value} -> app
//
// Modes:
//
//	demo                 a scripted arrival, played out loud
//	demo -mode arrival   the full sequence: radar, approach, tower, ground
//	demo -mode accents   the same clearance in many assigned voices
//	demo -mode profiles  one clearance through each radio profile
//	demo -mode emergency a mayday with a second aircraft on the frequency
//	demo -mode live      type pilot transmissions and get answers
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	voicegoio "github.com/mrlm-net/voice-goio"
	"github.com/mrlm-net/voice-goio/audio"
	"github.com/mrlm-net/voice-goio/audio/radio"
	"github.com/mrlm-net/voice-goio/normalise"
	"github.com/mrlm-net/voice-goio/stt/fake"
	"github.com/mrlm-net/voice-goio/tts"
	"github.com/mrlm-net/voice-goio/voices"
)

func main() {
	var (
		mode     = flag.String("mode", "session", "session | arrival | accents | profiles | emergency | live")
		backend  = flag.String("tts", "", "tts backend: piper, say, fake (default: best available)")
		airport  = flag.String("airport", "EGLL", "ICAO code of the airport, which selects the accents used")
		phrase   = flag.String("phrase", "", "override the phrase used by -mode accents and profiles")
		count    = flag.Int("n", 8, "number of voices for -mode accents")
		ph       = flag.String("phraseology", "icao", "icao or faa")
		seed     = flag.Int64("seed", 7, "session seed; the same seed gives the same voices")
		outDir   = flag.String("out", "", "write one WAV per transmission here instead of playing them")
		record   = flag.String("record", "", "also record the whole session to this single WAV file")
		silent   = flag.Bool("silent", false, "render without playing anything out loud")
		deviceID = flag.String("device", "", "output device id (see voicecheck devices)")
	)
	flag.Parse()

	if err := run(*mode, opts{
		backend: *backend, airport: *airport, phrase: *phrase, count: *count,
		phraseology: voicegoio.Phraseology(*ph), seed: *seed, outDir: *outDir,
		record: *record, silent: *silent, device: *deviceID,
	}); err != nil {
		fmt.Fprintln(os.Stderr, "demo:", err)
		os.Exit(1)
	}
}

type opts struct {
	backend     string
	airport     string
	phrase      string
	count       int
	phraseology voicegoio.Phraseology
	seed        int64
	outDir      string
	record      string
	silent      bool
	device      string
}

// station is one controller position: an identity, a frequency, and the voice
// the pool assigned to it.
type station struct {
	id    string
	freq  string
	kind  voicegoio.ControllerKind
	voice voicegoio.VoiceProfile
}

// rig is the assembled pipeline. The application only ever touches these four
// objects; everything else in the library is reached through them.
type rig struct {
	norm   *normalise.Normaliser
	tts    voicegoio.TTS
	chain  *radio.Set
	player *audio.Player
	pool   *voices.Pool
	name   string
	ph     voicegoio.Phraseology
}

func setup(o opts) (*rig, error) {
	engine, name, err := tts.Open(tts.Options{Prefer: o.backend})
	if err != nil {
		return nil, err
	}
	man, err := voices.LoadDefault()
	if err != nil {
		return nil, err
	}
	// A -out directory swaps the platform player for the WAV writer, which is
	// how a headless or CI run produces something to listen to later.
	player, err := audio.NewPlayer(audio.Options{
		DeviceID:   o.device,
		WAVDir:     o.outDir,
		RecordPath: o.record,
		Silent:     o.silent,
	})
	if err != nil {
		engine.Close()
		return nil, err
	}
	return &rig{
		norm:   normalise.New(),
		tts:    engine,
		chain:  radio.Default(),
		player: player,
		pool:   voices.NewPool(man, voices.PoolOptions{Seed: o.seed, AllowUnaudited: true}),
		name:   name,
		ph:     o.phraseology,
	}, nil
}

func (r *rig) close() {
	r.player.Close()
	r.tts.Close()
}

// transmit is the one function that matters: it is exactly what the MSFS
// application would call for every ATC message.
func (r *rig) transmit(ctx context.Context, s station, text string) error {
	t := voicegoio.Transmission{
		Frequency:    s.freq,
		ControllerID: s.id,
		Phraseology:  r.ph,
		Text:         text,
	}
	return r.say(ctx, s, t, r.norm.Spoken(t.Text, t.Phraseology))
}

// say is transmit with the spoken form already decided, so a pilot call that
// is written the way it is said can be played without being normalised twice.
func (r *rig) say(ctx context.Context, s station, t voicegoio.Transmission, spoken string) error {
	fmt.Printf("  \x1b[36m%-12s\x1b[0m %s\n", s.id, t.Text)
	fmt.Printf("  %-12s \x1b[2m%s\x1b[0m\n", "", spoken)

	start := time.Now()
	pcm, err := r.tts.Synthesize(ctx, s.voice, spoken)
	if err != nil {
		return fmt.Errorf("synthesize: %w", err)
	}
	synth := time.Since(start)

	rate := r.tts.SampleRate(s.voice)
	out := r.chain.Apply(pcm, rate, s.voice.Radio, r.player.SampleRate(), r.pool.Seed()+int64(len(t.Text)))
	fmt.Printf("  %-12s \x1b[2m%s speaker %d | %s | radio %s | %d ms audio | synth %v\x1b[0m\n",
		"", s.voice.Model, s.voice.SpeakerID, orDefault(s.voice.Accent, "?"), s.voice.Radio,
		len(out)*1000/r.player.SampleRate(), synth.Round(time.Millisecond))

	return r.player.Play(t, out, r.player.SampleRate())
}

// waitIdle drains playback events until every transmission has finished, which
// is how the application knows the frequency is clear for the next call.
func (r *rig) waitIdle(n int) {
	for range n {
		<-r.player.Events() // started
		<-r.player.Events() // finished
	}
}

func run(mode string, o opts) error {
	r, err := setup(o)
	if err != nil {
		return err
	}
	defer r.close()

	fmt.Printf("\n\x1b[1mvoice-goio demo\x1b[0m  tts=%s  airport=%s  phraseology=%s  seed=%d\n",
		r.name, o.airport, r.ph, o.seed)
	if r.name == "fake" {
		fmt.Println("\x1b[33mnote: no piper binary and no macOS say available, so the audio is a\n" +
			"      synthetic buzz. The pipeline is real; only the voice is not.\x1b[0m")
	}
	fmt.Println()

	defer func() {
		if path, secs, ok := r.player.Recording(); ok {
			fmt.Printf("\n\x1b[1mrecorded\x1b[0m %s \x1b[2m(%.0f s)\x1b[0m\n", path, secs)
		}
	}()

	switch mode {
	case "session":
		return r.session(o)
	case "accents":
		return r.accents(o)
	case "profiles":
		return r.profiles(o)
	case "arrival":
		return r.arrival(o)
	case "emergency":
		return r.emergency(o)
	case "live":
		return r.live(o)
	default:
		return fmt.Errorf("unknown mode %q", mode)
	}
}

// ---- scripted session ------------------------------------------------------

func (r *rig) session(o opts) error {
	ctx := context.Background()
	gnd := r.station(o.airport, voicegoio.Ground, "_GND", "121.905")
	twr := r.station(o.airport, voicegoio.Tower, "_TWR", "118.100")
	app := r.station(o.airport, voicegoio.Approach, "_APP", "119.710")
	ctr := r.station(o.airport, voicegoio.Center, "_CTR", "127.450")

	atis := r.station(o.airport, voicegoio.ATIS, "_ATIS", "128.075")

	script := []struct {
		s    station
		text string
	}{
		{atis, o.airport + " information ALPHA, time 1150, RWY 27L in use, wind 250 degrees 8 kt, " +
			"10KM BR, BKN020, temperature 15, dewpoint 9, QNH 1013, NOSIG"},
		{ctr, "BAW123 London Control, radar contact, descend FL100, QNH 1013"},
		{app, "BAW123 turn left HDG 270, cleared ILS approach RWY 27L, report established"},
		{twr, "BAW123 wind 250 degrees 8 kt, RWY 27L cleared to land"},
		// A taxi clearance says where to, then how to get there. Without the
		// destination it is not a clearance at all.
		{gnd, "OK-ABC taxi to RWY 27L at A1 via TWY A3 and B"},
		{twr, "OK-ABC hold short RWY 27L, traffic on short final"},
		// The weather goes off, which is what a new ATIS letter is for.
		{twr, "BAW123 wind 270 degrees 18 kt gusting 28 kt, BKN008, caution wind shear reported on short final"},
		{atis, o.airport + " information BRAVO, time 1220, RWY 27L in use, wind 270 degrees 18 kt gusting 28 kt, " +
			"4KM RA, BKN008, temperature 14, dewpoint 12, QNH 1008, TEMPO 2KM TSRA"},
	}
	fmt.Println("\x1b[1mArrival, with the weather going off and a new ATIS\x1b[0m")
	fmt.Print("\x1b[2mthe ATIS is one fixed machine voice at every airport; the controllers are not\x1b[0m\n\n")
	for _, l := range script {
		if err := r.transmit(ctx, l.s, l.text); err != nil {
			return err
		}
		r.waitIdle(1)
		fmt.Println()
	}
	return nil
}

// ---- accent tour -----------------------------------------------------------

// accents plays one clearance through many assigned voices. This is the
// BeyondATC "Basic" comparison: the bar is roughly a hundred local neural
// voices with real accent variety, and the point of the tour is that you can
// hear the variety rather than count rows in a manifest.
func (r *rig) accents(o opts) error {
	ctx := context.Background()
	text := o.phrase
	if text == "" {
		text = "BAW123 descend FL100, QNH 1013, contact Praha Radar on 127.45"
	}
	total := r.pool.Count()
	fmt.Printf("\x1b[1mAccent tour: %d of %d assignable voices, %d accents in the pool\x1b[0m\n",
		o.count, total, len(r.pool.Accents()))
	fmt.Printf("\x1b[2mpool accents: %s\x1b[0m\n", strings.Join(r.pool.Accents(), " "))
	if r.name == tts.BackendSay {
		fmt.Print("\x1b[33mnote: this backend is English voices only. Native accents (en-GB, en-US,\n" +
			"      en-IE, en-AU, en-IN, en-ZA) are real; the non-native ones are labelled\n" +
			"      but spoken by an English voice, because feeding English to a Czech or\n" +
			"      German voice mispronounces it rather than accenting it. Only piper\n" +
			"      renders those, via the espeak swap trick.\x1b[0m\n")
	}
	fmt.Println()

	// Assigning across different airports is what pulls in different regions.
	airports := []string{"LKPR", "EGLL", "KJFK", "EDDF", "LFPG", "EHAM", "LIRF", "EPWA",
		"YSSY", "VIDP", "FAOR", "LEMD", "EIDW", "KDEN", "EGPH", "CYYZ"}
	for i := range o.count {
		icao := airports[i%len(airports)]
		s := r.station(icao, voicegoio.Center, "_CTR", "127.450")
		fmt.Printf("\x1b[1m%2d. %s\x1b[0m\n", i+1, icao)
		if err := r.transmit(ctx, s, text); err != nil {
			return err
		}
		r.waitIdle(1)
		fmt.Println()
	}
	return nil
}

// ---- radio profile A/B -----------------------------------------------------

func (r *rig) profiles(o opts) error {
	ctx := context.Background()
	text := o.phrase
	if text == "" {
		text = "BAW123 descend FL100, QNH 1013"
	}
	base := r.station(o.airport, voicegoio.Tower, "_TWR", "118.100")
	fmt.Println("\x1b[1mRadio profiles, same voice and same words\x1b[0m")
	fmt.Print("\x1b[2mlisten for: bandwidth, noise floor, squelch length, and dropouts on center\x1b[0m\n\n")

	for _, name := range r.chain.Names() {
		s := base
		s.voice.Radio = name
		s.id = strings.ToUpper(name)
		if err := r.transmit(ctx, s, text); err != nil {
			return err
		}
		r.waitIdle(1)
		fmt.Println()
	}
	return nil
}

// ---- interactive -----------------------------------------------------------

// live closes the loop: what you type is treated as a pilot transmission, the
// recogniser tags it, and the controller answers out loud.
func (r *rig) live(o opts) error {
	ctx := context.Background()
	roster := []voicegoio.Callsign{
		{ICAO: "BAW123"}, {ICAO: "CSA1234"}, {ICAO: "OK-ABC"}, {ICAO: "DLH4EK"}, {ICAO: "RYR89AB"},
	}
	rec := fake.FromStdin()
	defer rec.Close()
	if err := rec.SetCallsigns(roster); err != nil {
		return err
	}
	twr := r.station(o.airport, voicegoio.Tower, "_TWR", "118.100")

	fmt.Println("\x1b[1mOn frequency:\x1b[0m")
	for _, c := range roster {
		fmt.Printf("  %-8s \x1b[2m%s\x1b[0m\n", c.ICAO, normalise.SpokenCallsign(c.ICAO))
	}
	fmt.Println("\nType a transmission the way a pilot says it:")
	fmt.Println("  \x1b[2mSpeedbird one two three descending flight level one zero zero\x1b[0m")
	fmt.Println("  \x1b[2moscar kilo alpha bravo charlie request pushback\x1b[0m")
	fmt.Println("\nThe identifier and plain digits work too, which is quicker to type.")
	fmt.Println("The real recogniser only ever hears words, so this is a harness")
	fmt.Println("convenience and nothing the Windows backend has to cope with:")
	fmt.Println("  \x1b[2mCSA1234 with you, 15000\x1b[0m")
	fmt.Println("  \x1b[2mDLH4EK request climb FL370\x1b[0m")
	fmt.Println("\nCtrl-D to quit.")

	for {
		fmt.Print("\n\x1b[1mPTT>\x1b[0m ")
		got, ok := <-rec.Results()
		if !ok {
			return nil
		}
		fmt.Printf("  \x1b[35mrecognised\x1b[0m  intent=%s callsign=%s value=%s confidence=%.2f\n",
			got.Tags[voicegoio.TagIntent], got.Tags[voicegoio.TagCallsign],
			got.Tags[voicegoio.TagValue], got.Confidence)
		if err := r.transmit(ctx, twr, reply(got)); err != nil {
			return err
		}
		r.waitIdle(1)
	}
}

// reply is the stand-in for the application's ATC logic. The library has no
// opinion about it: it only turns the text into audio.
func reply(rec voicegoio.Recognition) string {
	cs := rec.Tags[voicegoio.TagCallsign]
	v := rec.Tags[voicegoio.TagValue]

	// A failed recognition is answered according to how it failed. Replying
	// "station calling, say again your callsign" to an aircraft whose callsign
	// is sitting in the tags is the single most obviously robotic thing a
	// controller can do.
	if rec.Tags[voicegoio.TagIntent] == voicegoio.IntentSayAgain {
		switch rec.Tags[voicegoio.TagReason] {
		case voicegoio.ReasonNoCallsign:
			return "Station calling, say again your callsign"
		case voicegoio.ReasonOffGrammar:
			return cs + " say again, standard phraseology"
		case voicegoio.ReasonLowConfidence:
			return cs + " you are unreadable, say again"
		default:
			// No reason: the pilot said "say again", so repeat the clearance.
			return cs + " I say again, descend FL100, QNH 1013"
		}
	}
	if cs == "" {
		return "Station calling, say again your callsign"
	}

	switch rec.Tags[voicegoio.TagIntent] {
	case "mayday":
		return cs + " roger MAYDAY, SQK 7700, RWY 27L cleared to land, " +
			"emergency services on standby, when able say souls on board and fuel remaining"
	case "pan_pan":
		return cs + " roger PAN PAN, descend 3000 ft, vectors RWY 27L, " +
			"when able say intentions and fuel remaining"
	case "readback_altitude":
		if q := rec.Tags["qnh"]; q != "" {
			return cs + " readback correct, QNH " + q
		}
		return cs + " readback correct, maintain " + spokenLevel(v)
	case "readback_qnh":
		return cs + " QNH " + v + " correct"
	case "readback_speed":
		return cs + " speed " + v + " knots correct"
	case "readback_heading":
		return cs + " heading " + v + " approved"
	case "readback_frequency":
		return cs + " correct, good day"
	case "readback_squawk":
		return cs + " squawk " + v + " observed, radar contact"
	case "readback_runway":
		return cs + " correct, RWY " + v
	case "readback_taxi":
		return cs + " correct, hold short RWY 27L"
	case "request_climb":
		return cs + " climb " + spokenLevel(orDefault(v, "FL350"))
	case "request_descent":
		return cs + " descend FL100, QNH 1013"
	case "request_direct":
		return cs + " cleared direct " + v
	case "request_approach":
		return cs + " expect ILS approach RWY 27L, descend 3000 ft"
	case "report_in_sight":
		return cs + " roger, cleared visual approach RWY 27L"
	case "negative":
		return cs + " roger, standby for further"
	case "request_pushback":
		return cs + " pushback approved, face north, report ready to taxi"
	case "request_taxi":
		return cs + " taxi to RWY 27L at A1 via TWY A3 and B"
	case "request_takeoff":
		return cs + " RWY 27L at A1 cleared for takeoff, wind 250 degrees 8 kt"
	case "request_landing":
		return cs + " RWY 27L cleared to land"
	case "request_clearance":
		return cs + " cleared to destination via SID, climb FL80, SQK 4321"
	case "report_ready":
		return cs + " line up and wait RWY 27L"
	case "report_established":
		return cs + " contact tower on 118.100"
	case "report_vacated":
		return cs + " taxi to stand 42 via TWY B, contact ground on 121.905"
	case "report_going_around":
		return cs + " roger, climb 3000 ft, HDG 240, contact approach on 119.710"
	case "checkin":
		return cs + " identified, descend FL100, QNH 1013"
	case "ident":
		return cs + " ident observed, radar contact, descend FL100"
	case "wilco", "roger", "standby", "affirm":
		return cs + " roger"
	case "request_deviation":
		return cs + " deviation " + orDefault(v, "as requested") + " approved, " +
			"report back on course"
	case "report_souls":
		return cs + " roger, " + v + " souls on board"
	default:
		// Anything the parser tagged but this stand-in controller has no
		// answer for. Saying "standby" to a readback sounds correct while
		// meaning nothing, so name the gap instead: this is demo logic, and
		// the real application owns these responses.
		return cs + " roger"
	}
}

func spokenLevel(v string) string {
	if v == "" {
		return "present level"
	}
	if strings.HasPrefix(v, "FL") {
		return v
	}
	return v + " ft"
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// station assigns a voice to a controller position at an airport.
func (r *rig) station(icao string, kind voicegoio.ControllerKind, suffix, freq string) station {
	return station{
		id:    icao + suffix,
		freq:  freq,
		kind:  kind,
		voice: r.pool.Assign(icao, kind),
	}
}
