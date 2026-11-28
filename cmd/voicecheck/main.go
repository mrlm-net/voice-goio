// Command voicecheck is the regression and audit tool for voice-goio.
//
//	voicecheck synth    --out ./wav              synthesise the audit text in every voice and radio profile
//	voicecheck audit    --model de_DE-thorsten-medium   swap-trick audit for one non English model
//	voicecheck recog    --backend fake|sapi|whisper [--grammar controller] --script testdata/stt/readbacks.jsonl
//	voicecheck whisper  --dir bin --model base.en   install whisper-cli and a model
//	voicecheck devices                            list input and output devices
//	voicecheck voices                             report pool size, accents and what is missing
//	voicecheck download --model <name>            fetch a model from the voice repository
//
// synth and audit produce WAVs for a human to listen to; recog and voices are
// machine checkable and exit non-zero on failure, which is what makes them
// usable from CI and from a pre-release script.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	voicegoio "github.com/mrlm-net/voice-goio"
	"github.com/mrlm-net/voice-goio/audio"
	"github.com/mrlm-net/voice-goio/audio/radio"
	"github.com/mrlm-net/voice-goio/grammar"
	"github.com/mrlm-net/voice-goio/internal/wav"
	"github.com/mrlm-net/voice-goio/normalise"
	"github.com/mrlm-net/voice-goio/stt/fake"
	"github.com/mrlm-net/voice-goio/stt/sapi"
	"github.com/mrlm-net/voice-goio/stt/whisper"
	"github.com/mrlm-net/voice-goio/tts"
	"github.com/mrlm-net/voice-goio/tts/piper"
	"github.com/mrlm-net/voice-goio/voices"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "synth":
		err = cmdSynth(os.Args[2:])
	case "audit":
		err = cmdAudit(os.Args[2:])
	case "recog":
		err = cmdRecog(os.Args[2:])
	case "devices":
		err = cmdDevices(os.Args[2:])
	case "voices":
		err = cmdVoices(os.Args[2:])
	case "pack":
		err = cmdPack(os.Args[2:])
	case "whisper":
		err = cmdWhisper(os.Args[2:])
	case "download":
		err = cmdDownload(os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "voicecheck:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `voicecheck - regression and audit tool for voice-goio

  synth     synthesise the audit text in every voice and radio profile
  audit     swap-trick audit for one non English model
  recog     replay a recognition corpus and compare the tags
  devices   list audio input and output devices
  voices    report pool size, accent coverage and missing models
  download  fetch a model from the voice repository
  whisper   install whisper-cli and a model (-dir bin -model base.en) for -backend whisper
  pack      zip a voice pack (-pack en|all -out file.zip) of the installed models for an installer

Run "voicecheck <command> -h" for the flags of each.
`)
}

// loadPool builds the voice pool from a manifest path, or the embedded one.
func loadPool(path, dir string, seed int64) (*voices.Pool, error) {
	var (
		m   *voices.Manifest
		err error
	)
	if path == "" {
		m, err = voices.LoadDefault()
	} else {
		m, err = voices.Load(path)
	}
	if err != nil {
		return nil, err
	}
	return voices.NewPool(m, voices.PoolOptions{Dir: dir, Seed: seed, AllowUnaudited: true}), nil
}

// ---- synth -----------------------------------------------------------------

func cmdSynth(args []string) error {
	fs := flag.NewFlagSet("synth", flag.ExitOnError)
	var (
		manifest = fs.String("voices", "", "manifest path (default: the embedded voices.json)")
		dir      = fs.String("dir", "", "voice model directory")
		textPath = fs.String("text", filepath.Join("testdata", "audit", "clearance.txt"), "text to speak")
		out      = fs.String("out", "wav", "output directory")
		backend  = fs.String("tts", "", "tts backend: piper, say, fake")
		limit    = fs.Int("limit", 12, "maximum number of voices (0 = no limit)")
		profiles = fs.String("profiles", "", "comma separated radio profiles (default: all)")
	)
	fs.Parse(args)

	body, err := os.ReadFile(*textPath)
	if err != nil {
		return err
	}
	text := strings.TrimSpace(string(body))

	pool, err := loadPool(*manifest, *dir, 7)
	if err != nil {
		return err
	}
	engine, name, err := tts.Open(tts.Options{Prefer: *backend, Piper: piper.Options{Resolver: pool}})
	if err != nil {
		return err
	}
	defer engine.Close()

	chain := radio.Default()
	profs := chain.Names()
	if *profiles != "" {
		profs = strings.Split(*profiles, ",")
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}

	airports := []string{"LKPR", "EGLL", "KJFK", "EDDF", "LFPG", "EHAM", "LIRF", "EPWA",
		"YSSY", "VIDP", "FAOR", "LEMD", "EIDW", "KDEN", "EGPH", "CYYZ"}
	n := *limit
	if n <= 0 || n > len(airports) {
		n = len(airports)
	}
	fmt.Printf("backend=%s voices=%d profiles=%v out=%s\n", name, n, profs, *out)

	for i := range n {
		v := pool.Assign(airports[i], voicegoio.Center)
		pcm, err := engine.Synthesize(context.Background(), v, text)
		if err != nil {
			fmt.Printf("  %-28s SKIP %v\n", v.Model, err)
			continue
		}
		rate := engine.SampleRate(v)
		for _, p := range profs {
			processed := chain.Apply(pcm, rate, p, 48000, int64(i))
			file := filepath.Join(*out, fmt.Sprintf("%02d_%s_s%d_%s.wav", i, v.Model, v.SpeakerID, p))
			if err := wav.WriteFile(file, processed, 48000); err != nil {
				return err
			}
			fmt.Printf("  %-44s %6.1f dBFS rms  %6.1f peak  %s\n",
				filepath.Base(file), radio.RMSdBFS(processed), radio.PeakDBFS(processed), airports[i])
		}
	}
	return nil
}

// ---- audit -----------------------------------------------------------------

func cmdAudit(args []string) error {
	fs := flag.NewFlagSet("audit", flag.ExitOnError)
	var (
		manifest = fs.String("voices", "", "manifest path")
		dir      = fs.String("dir", "", "voice model directory")
		model    = fs.String("model", "", "model to audit (required)")
		textPath = fs.String("text", filepath.Join("testdata", "audit", "clearance.txt"), "text to speak")
		out      = fs.String("out", "wav-audit", "output directory")
		backend  = fs.String("tts", "", "tts backend")
	)
	fs.Parse(args)
	if *model == "" {
		return fmt.Errorf("audit: -model is required")
	}

	body, err := os.ReadFile(*textPath)
	if err != nil {
		return err
	}
	pool, err := loadPool(*manifest, *dir, 7)
	if err != nil {
		return err
	}
	m, ok := pool.Manifest().Model(*model)
	if !ok {
		return fmt.Errorf("audit: %q is not in the manifest", *model)
	}
	engine, name, err := tts.Open(tts.Options{Prefer: *backend, Piper: piper.Options{Resolver: pool}})
	if err != nil {
		return err
	}
	defer engine.Close()
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}

	fmt.Printf("auditing %s (%s, licence %s) with backend %s\n", m.Name, m.Accent, m.License, name)
	if m.EspeakOverride != "" {
		fmt.Printf("espeak override: %s -- English text through a non English model.\n", m.EspeakOverride)
		fmt.Println("Phonemes the model never learned are dropped by piper; listen for")
		fmt.Println("missing consonants and for numbers that come out wrong.")
	}

	chain := radio.Default()
	speakers := max(m.SpeakerCount, 1)
	for id := range speakers {
		v := voicegoio.VoiceProfile{Model: m.Name, SpeakerID: id, LengthScale: 0.9, Radio: "center"}
		pcm, err := engine.Synthesize(context.Background(), v, strings.TrimSpace(string(body)))
		if err != nil {
			return err
		}
		processed := chain.Apply(pcm, engine.SampleRate(v), v.Radio, 48000, int64(id))
		file := filepath.Join(*out, fmt.Sprintf("%s_s%03d.wav", m.Name, id))
		if err := wav.WriteFile(file, processed, 48000); err != nil {
			return err
		}
		fmt.Printf("  %s  %6.1f dBFS rms  %6.1f peak\n",
			filepath.Base(file), radio.RMSdBFS(processed), radio.PeakDBFS(processed))
		if speakers > 24 && id >= 23 {
			fmt.Printf("  ... stopping at 24 of %d speakers; audit the rest in batches\n", speakers)
			break
		}
	}
	fmt.Println("\nListen, then set quality and pass for each speaker in voices.json.")
	return nil
}

// ---- recog -----------------------------------------------------------------

func cmdRecog(args []string) error {
	fs := flag.NewFlagSet("recog", flag.ExitOnError)
	var (
		backend = fs.String("backend", "fake", "fake, sapi or whisper")
		script  = fs.String("script", filepath.Join("testdata", "stt", "readbacks.jsonl"), "corpus")
		minPct  = fs.Float64("min", 95, "minimum percentage of exactly tagged phrases")
		wavDir  = fs.String("wav", "", "render each phrase to a WAV here and feed the file to the engine, "+
			"instead of waiting for someone to say it into a microphone (sapi; whisper always)")
		ttsName = fs.String("tts", "", "tts backend used to render the phrases")
		side    = fs.String("grammar", "atc", "atc (pilot speech) or controller (TWR and GND instructions)")
		model   = fs.String("model", "", "whisper: ggml model file (whisper.InstallModel)")
		bin     = fs.String("bin", "", "whisper: whisper-cli (default: beside the model, else PATH)")
		mic     = fs.Bool("mic", false, "whisper: say each phrase into the microphone (Enter to start and to stop)")
		device  = fs.String("device", "", "whisper -mic: microphone, an index or part of its name (voicecheck devices)")
	)
	fs.StringVar(&oneVoice, "voice", "", "-wav: render every phrase with this voice, model[#speaker] (voicecheck voices)")
	fs.IntVar(&speakers, "speakers", 0, "-wav: render the phrases in turn with this many installed English voices and accents")
	fs.Parse(args)
	switch *side {
	case "atc":
	case "controller":
		controller = true
		if *script == filepath.Join("testdata", "stt", "readbacks.jsonl") {
			*script = filepath.Join("testdata", "stt", "controller.jsonl")
		}
	default:
		return fmt.Errorf("unknown grammar %q", *side)
	}

	f, err := os.Open(*script)
	if err != nil {
		return err
	}
	lines, err := fake.ReadScript(f)
	f.Close()
	if err != nil {
		return err
	}

	switch *backend {
	case "fake":
		return recogFake(lines, *minPct)
	case "sapi":
		if *wavDir != "" {
			rec, err := sapi.New(sapiOptions())
			if err != nil {
				return err
			}
			return recogFiles(lines, *minPct, *wavDir, *ttsName, rec, 0)
		}
		return recogSAPI(lines, *minPct)
	case "whisper":
		opt := whisper.Options{Bin: *bin, Model: *model, Controller: controller}
		if *mic {
			opt.Recorder = whisper.MicRecorder(*device)
		}
		rec, err := whisper.New(opt)
		if err != nil {
			return err
		}
		if *mic {
			if err := rec.Warm(); err != nil {
				return err
			}
			return recogLive(rec, lines, *minPct, true)
		}
		if *wavDir == "" {
			*wavDir = filepath.Join(os.TempDir(), "voicecheck-whisper")
		}
		return recogFiles(lines, *minPct, *wavDir, *ttsName, whisperFiles{rec}, 16000)
	default:
		return fmt.Errorf("unknown backend %q", *backend)
	}
}

// recogFake runs the corpus through the Go parser, which is the reference
// implementation of the grammar.
func recogFake(lines []fake.ScriptLine, minPct float64) error {
	p := fake.NewParser()
	if controller {
		p = fake.NewControllerParser()
	}
	var total, exact int
	for _, l := range lines {
		if len(l.Callsigns) > 0 {
			cs := make([]voicegoio.Callsign, 0, len(l.Callsigns))
			for _, id := range l.Callsigns {
				cs = append(cs, voicegoio.Callsign{ICAO: id})
			}
			p.SetCallsigns(cs)
			fmt.Printf("on frequency: %s\n", strings.Join(l.Callsigns, " "))
			continue
		}
		total++
		got := p.Parse(l.Text)
		ok := tagsMatch(got.Tags, l.Tags)
		if ok {
			exact++
		}
		mark := "\x1b[31mFAIL\x1b[0m"
		if ok {
			mark = "\x1b[32m ok \x1b[0m"
		}
		fmt.Printf("%s %-58.58s intent=%-20s callsign=%-8s value=%-8s conf=%.2f\n",
			mark, l.Text, got.Tags[voicegoio.TagIntent], got.Tags[voicegoio.TagCallsign],
			got.Tags[voicegoio.TagValue], got.Confidence)
		if !ok {
			fmt.Printf("     want intent=%s callsign=%s value=%s\n",
				l.Tags[voicegoio.TagIntent], l.Tags[voicegoio.TagCallsign], l.Tags[voicegoio.TagValue])
		}
	}
	return report(exact, total, minPct)
}

// recogSAPI drives the real Windows engine. SPEC.md 4.5 runs the same corpus
// through both backends so one expected-tags file covers them.
func recogSAPI(lines []fake.ScriptLine, minPct float64) error {
	rec, err := sapi.New(sapiOptions())
	if err != nil {
		return err
	}
	return recogLive(rec, lines, minPct, false)
}

// recogLive has the operator say each phrase into the microphone: for 6 s,
// or with enter between Enter presses.
func recogLive(rec voicegoio.STT, lines []fake.ScriptLine, minPct float64, enter bool) error {
	defer rec.Close()
	in := bufio.NewReader(os.Stdin)

	var total, exact int
	for _, l := range lines {
		if len(l.Callsigns) > 0 {
			cs := make([]voicegoio.Callsign, 0, len(l.Callsigns))
			for _, id := range l.Callsigns {
				cs = append(cs, voicegoio.Callsign{ICAO: id})
			}
			if err := rec.SetCallsigns(cs); err != nil {
				return err
			}
			fmt.Printf("on frequency: %s\n", strings.Join(l.Callsigns, " "))
			continue
		}
		total++
		say := l.Text
		if strings.ContainsAny(say, "0123456789") {
			say = normalise.Spoken(say, voicegoio.ICAO)
		}
		fmt.Printf("\nSay: \x1b[1m%s\x1b[0m\n", say)
		if enter {
			fmt.Print("  Enter, speak, Enter ")
			in.ReadString('\n')
		}
		if err := rec.Start(); err != nil {
			return err
		}
		if enter {
			fmt.Print("  listening... ")
			in.ReadString('\n')
		} else {
			time.Sleep(6 * time.Second) // the operator speaks
		}
		t0 := time.Now()
		if err := rec.Stop(); err != nil {
			return err
		}
		got := <-rec.Results()
		fmt.Printf("  %d ms after release\n", time.Since(t0).Milliseconds())
		ok := tagsMatch(got.Tags, l.Tags)
		if ok {
			exact++
		}
		fmt.Printf("  heard %q -> intent=%s callsign=%s value=%s conf=%.2f\n",
			got.Text, got.Tags[voicegoio.TagIntent], got.Tags[voicegoio.TagCallsign],
			got.Tags[voicegoio.TagValue], got.Confidence)
	}
	return report(exact, total, minPct)
}

// fileRecognizer is a backend that takes a WAV file: SAPI (through
// SpFileStream) and whisper.
type fileRecognizer interface {
	SetCallsigns([]voicegoio.Callsign) error
	RecognizeFile(path string) (voicegoio.Recognition, error)
	Close() error
}

// whisperFiles is a whisper.Recognizer as a fileRecognizer.
type whisperFiles struct{ *whisper.Recognizer }

func (w whisperFiles) RecognizeFile(path string) (voicegoio.Recognition, error) {
	return w.Recognizer.RecognizeFile(path), nil
}

// recogFiles is the unattended form of the corpus run: every phrase is
// rendered to a WAV (at rate, 0: the voice's) and fed to the engine, so the
// same expected-tags file validates a backend without a person saying
// forty phrases into a headset (SPEC.md 4.5).
func recogFiles(lines []fake.ScriptLine, minPct float64, wavDir, ttsName string, rec fileRecognizer, rate int) error {
	defer rec.Close()
	if err := os.MkdirAll(wavDir, 0o755); err != nil {
		return err
	}
	pool, err := loadPool("", "", 7)
	if err != nil {
		return err
	}
	engine, name, err := tts.Open(tts.Options{Prefer: ttsName, Piper: piper.Options{Resolver: pool}})
	if err != nil {
		return err
	}
	defer engine.Close()
	// One voice for the whole corpus measures the grammar, not the engine's
	// tolerance for different speakers; -speakers N measures that, the
	// phrases said in turn by N voices and accents.
	vs, err := recogVoices(pool)
	if err != nil {
		return err
	}

	fmt.Printf("rendering with %s, %d voice(s), feeding files to the engine\n", name, len(vs))
	var total, exact int
	perVoice := make([][2]int, len(vs))
	for i, l := range lines {
		if len(l.Callsigns) > 0 {
			cs := make([]voicegoio.Callsign, 0, len(l.Callsigns))
			for _, id := range l.Callsigns {
				cs = append(cs, voicegoio.Callsign{ICAO: id})
			}
			if err := rec.SetCallsigns(cs); err != nil {
				return err
			}
			fmt.Printf("on frequency: %s\n", strings.Join(l.Callsigns, " "))
			continue
		}
		total++
		vi := (total - 1) % len(vs)
		voice := vs[vi]
		perVoice[vi][1]++
		path := filepath.Join(wavDir, fmt.Sprintf("%03d.wav", i))
		// A line written for the typing harness ("CSA1234 ident") is
		// rendered as a pilot says it ("CSA Lines one two tree four,
		// ident"), not as piper reads the digits; a line already spoken
		// has no digit and is left as it is.
		say := l.Text
		if strings.ContainsAny(say, "0123456789") {
			say = normalise.Spoken(say, voicegoio.ICAO)
		}
		pcm, err := engine.Synthesize(context.Background(), voice, say)
		if err != nil {
			return fmt.Errorf("render %q: %w", l.Text, err)
		}
		sr := engine.SampleRate(voice)
		if rate != 0 && rate != sr {
			pcm, sr = resample(pcm, sr, rate), rate
		}
		if err := wav.WriteFile(path, pcm, sr); err != nil {
			return err
		}
		got, err := rec.RecognizeFile(path)
		if err != nil {
			return fmt.Errorf("recognise %s: %w", path, err)
		}
		ok := tagsMatch(got.Tags, l.Tags)
		if ok {
			exact++
			perVoice[vi][0]++
		}
		mark := "\x1b[31mFAIL\x1b[0m"
		if ok {
			mark = "\x1b[32m ok \x1b[0m"
		}
		fmt.Printf("%s %-52.52s intent=%-20s callsign=%-8s value=%-8s conf=%.2f\n",
			mark, l.Text, got.Tags[voicegoio.TagIntent], got.Tags[voicegoio.TagCallsign],
			got.Tags[voicegoio.TagValue], got.Confidence)
		if !ok {
			fmt.Printf("     want intent=%s callsign=%s value=%s\n",
				l.Tags[voicegoio.TagIntent], l.Tags[voicegoio.TagCallsign], l.Tags[voicegoio.TagValue])
			if got.Text != "" {
				fmt.Printf("     heard %q\n", got.Text)
			}
			if len(vs) > 1 {
				fmt.Printf("     voice %s\n", voiceName(voice))
			}
		}
	}
	if len(vs) > 1 {
		fmt.Println("\nper voice:")
		for i, v := range vs {
			fmt.Printf("  %2d/%-2d %s\n", perVoice[i][0], perVoice[i][1], voiceName(v))
		}
	}
	return report(exact, total, minPct)
}

func tagsMatch(got, want map[string]string) bool {
	for _, k := range []string{voicegoio.TagIntent, voicegoio.TagCallsign, voicegoio.TagValue} {
		if got[k] != want[k] {
			return false
		}
	}
	return true
}

func report(exact, total int, minPct float64) error {
	if total == 0 {
		return fmt.Errorf("corpus is empty")
	}
	pct := float64(exact) / float64(total) * 100
	fmt.Printf("\n%d/%d exactly tagged (%.1f%%), threshold %.1f%%\n", exact, total, pct, minPct)
	if pct < minPct {
		return fmt.Errorf("below threshold")
	}
	return nil
}

// ---- devices ---------------------------------------------------------------

func cmdDevices(args []string) error {
	fs := flag.NewFlagSet("devices", flag.ExitOnError)
	fs.Parse(args)

	p, err := audio.NewPlayer(audio.Options{})
	if err != nil {
		return err
	}
	defer p.Close()
	outs, err := p.Devices()
	if err != nil {
		return err
	}
	fmt.Println("output devices:")
	for _, d := range outs {
		star := " "
		if d.Default {
			star = "*"
		}
		fmt.Printf(" %s %-6s %s\n", star, d.ID, d.Name)
	}

	fmt.Println("\ninput devices (speech engine):")
	ins, err := sapi.InputDevices()
	if err != nil {
		fmt.Printf("   unavailable: %v\n", err)
		return nil
	}
	for _, d := range ins {
		star := " "
		if d.Default {
			star = "*"
		}
		fmt.Printf(" %s %s\n     %s\n", star, d.Name, d.ID)
	}

	fmt.Println("\ninput devices (whisper -mic -device):")
	ins, err = whisper.InputDevices()
	if err != nil {
		fmt.Printf("   unavailable: %v\n", err)
		return nil
	}
	for _, d := range ins {
		star := " "
		if d.Default {
			star = "*"
		}
		fmt.Printf(" %s %-6s %s\n", star, d.ID, d.Name)
	}
	return nil
}

// ---- voices ----------------------------------------------------------------

func cmdVoices(args []string) error {
	fs := flag.NewFlagSet("voices", flag.ExitOnError)
	var (
		manifest = fs.String("voices", "", "manifest path")
		dir      = fs.String("dir", "", "voice model directory")
		minCount = fs.Int("min", 100, "minimum assignable voices (the BeyondATC Basic bar)")
		minAcc   = fs.Int("min-accents", 10, "minimum distinct accents")
	)
	fs.Parse(args)

	pool, err := loadPool(*manifest, *dir, 7)
	if err != nil {
		return err
	}
	accents := pool.Accents()
	fmt.Printf("models      %d\n", len(pool.Manifest().Models))
	fmt.Printf("voices      %d (bar: %d)\n", pool.Count(), *minCount)
	fmt.Printf("accents     %d (bar: %d) %s\n", len(accents), *minAcc, strings.Join(accents, " "))

	if missing := pool.Missing(); len(missing) > 0 {
		fmt.Printf("\nnot downloaded (%d): %s\n", len(missing), strings.Join(missing, " "))
		fmt.Println("run: voicecheck download -model <name>")
	}
	if pool.Count() < *minCount {
		return fmt.Errorf("pool has %d voices, below the %d bar", pool.Count(), *minCount)
	}
	if len(accents) < *minAcc {
		return fmt.Errorf("pool has %d accents, below the %d bar", len(accents), *minAcc)
	}
	return nil
}

// ---- pack ------------------------------------------------------------------

func cmdPack(args []string) error {
	fs := flag.NewFlagSet("pack", flag.ExitOnError)
	var (
		manifest = fs.String("voices", "", "manifest path")
		dir      = fs.String("dir", "", "the installed models")
		pack     = fs.String("pack", voices.PackAll, "en (English only) or all")
		out      = fs.String("out", "", "the zip to write")
	)
	fs.Parse(args)
	if *out == "" {
		return fmt.Errorf("pack: -out is required")
	}
	pool, err := loadPool(*manifest, *dir, 7)
	if err != nil {
		return err
	}
	man := pool.Manifest()
	models, err := man.Pack(*pack)
	if err != nil {
		return err
	}
	f, err := os.Create(*out)
	if err != nil {
		return err
	}
	if err := man.WritePack(f, models, pool.Dir()); err != nil {
		f.Close()
		os.Remove(*out)
		return err
	}
	fmt.Println(*out+":", len(models), "models")
	return f.Close()
}

// ---- download --------------------------------------------------------------

func cmdDownload(args []string) error {
	fs := flag.NewFlagSet("download", flag.ExitOnError)
	var (
		manifest = fs.String("voices", "", "manifest path")
		dir      = fs.String("dir", "", "destination directory")
		model    = fs.String("model", "", "model name, or \"all\"")
		pack     = fs.String("pack", "", "a voice pack: en (English only) or all")
		base     = fs.String("base", voices.DefaultBaseURL, "base URL or LAN mirror")
		write    = fs.String("write", "", "write the manifest back here with the hashes filled in")
	)
	fs.Parse(args)
	if *model == "" && *pack == "" {
		return fmt.Errorf("download: -model or -pack is required (-model all, -pack en)")
	}
	pool, err := loadPool(*manifest, *dir, 7)
	if err != nil {
		return err
	}
	man := pool.Manifest()

	targets := man.Models
	if *pack != "" {
		if targets, err = man.Pack(*pack); err != nil {
			return err
		}
	} else if *model != "all" {
		m, ok := man.Model(*model)
		if !ok {
			return fmt.Errorf("download: %q is not in the manifest", *model)
		}
		targets = []voices.Model{*m}
	}

	d := &voices.Downloader{
		BaseURL: *base,
		Dir:     pool.Dir(),
		OnProgress: func(p voices.Progress) {
			if p.Total > 0 {
				fmt.Printf("\r  %-50.50s %5.1f%%", p.File, float64(p.Downloaded)/float64(p.Total)*100)
			} else {
				fmt.Printf("\r  %-50.50s %d bytes", p.File, p.Downloaded)
			}
		},
	}
	for i := range targets {
		t := targets[i]
		fmt.Printf("%s\n", t.Name)
		sum, err := d.Fetch(context.Background(), &t)
		fmt.Println()
		if err != nil {
			return err
		}
		if mm, ok := man.Model(t.Name); ok && mm.SHA256 == "" {
			mm.SHA256 = sum
			fmt.Printf("  recorded sha256 %s\n", sum)
		}
	}
	if *write != "" {
		return man.Save(*write)
	}
	return nil
}

// controller: recog runs the controller grammar (-grammar controller).
var controller bool

// speakers: recog -wav renders the phrases in turn with this many installed
// voices that speak English (-speakers), 0 or 1 for one voice.
var speakers int

// oneVoice: recog -wav renders every phrase with this voice (-voice),
// model[#speaker].
var oneVoice string

// recogVoices are the voices recog -wav renders with: the pool's one, or
// with -speakers the installed models that speak English (native, or a
// foreign model reading English with its accent), one speaker of each,
// spread over the multi speaker models.
func recogVoices(pool *voices.Pool) ([]voicegoio.VoiceProfile, error) {
	if oneVoice != "" {
		name, idStr, _ := strings.Cut(oneVoice, "#")
		m, ok := pool.Manifest().Model(name)
		if !ok {
			return nil, fmt.Errorf("-voice: %q is not in the manifest", name)
		}
		id := 0
		if idStr != "" {
			var err error
			if id, err = strconv.Atoi(idStr); err != nil {
				return nil, fmt.Errorf("-voice: speaker %q: %w", idStr, err)
			}
		}
		return []voicegoio.VoiceProfile{m.Profile(id)}, nil
	}
	one := []voicegoio.VoiceProfile{pool.Assign("EGLL", voicegoio.Approach)}
	if speakers <= 1 {
		return one, nil
	}
	var out []voicegoio.VoiceProfile
	for round := 0; len(out) < speakers && round < 8; round++ {
		added := false
		for _, m := range voices.Installed(pool.Manifest(), "") {
			if !strings.HasPrefix(m.Accent, "en") && m.EspeakOverride == "" {
				continue
			}
			n := max(m.SpeakerCount, 1)
			if round >= n || len(out) >= speakers {
				continue
			}
			id := round * n / 8
			if len(m.Speakers) > 0 {
				id = m.Speakers[(round*len(m.Speakers)/8)%len(m.Speakers)].ID
			}
			v := m.Profile(id)
			if v.Accent == "" || !strings.HasPrefix(v.Accent, "en") {
				v.Accent = "en-" + strings.ToUpper(strings.SplitN(m.Name, "_", 2)[0])
			}
			out = append(out, v)
			added = true
		}
		if !added {
			break
		}
	}
	if len(out) == 0 {
		return one, nil
	}
	return out, nil
}

func voiceName(v voicegoio.VoiceProfile) string {
	return fmt.Sprintf("%s#%d (%s)", v.Model, v.SpeakerID, v.Accent)
}

// sapiOptions loads the grammar recog runs.
func sapiOptions() sapi.Options {
	if controller {
		return sapi.Options{Grammar: grammar.Controller}
	}
	return sapi.Options{}
}

// resample converts pcm from rate from to rate to, linearly: enough for a
// recogniser's input (whisper takes 16 kHz).
func resample(pcm []int16, from, to int) []int16 {
	if len(pcm) == 0 || from == to {
		return pcm
	}
	n := int(int64(len(pcm)) * int64(to) / int64(from))
	out := make([]int16, n)
	for i := range out {
		x := float64(i) * float64(from) / float64(to)
		j := int(x)
		if j+1 >= len(pcm) {
			out[i] = pcm[len(pcm)-1]
			continue
		}
		f := x - float64(j)
		out[i] = int16(float64(pcm[j])*(1-f) + float64(pcm[j+1])*f)
	}
	return out
}

// cmdWhisper installs whisper-cli and a model for -backend whisper.
func cmdWhisper(args []string) error {
	fs := flag.NewFlagSet("whisper", flag.ExitOnError)
	dir := fs.String("dir", "bin", "folder: whisper-cli in <dir>/whisper, the model in <dir>")
	model := fs.String("model", "base.en", "tiny.en, base.en or small.en")
	fs.Parse(args)
	progress := func(done, total int64) {
		if total > 0 {
			fmt.Fprintf(os.Stderr, "\r%3d%%", done*100/total)
		}
	}
	bin, err := whisper.Install(context.Background(), *dir, progress)
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr)
	m, err := whisper.InstallModel(context.Background(), *dir, *model, progress)
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr)
	fmt.Printf("whisper-cli %s\nmodel       %s\nvoicecheck recog -backend whisper -bin %s -model %s\n", bin, m, bin, m)
	return nil
}
