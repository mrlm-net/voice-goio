// Command voicecheck is the regression and audit tool for voice-goio.
//
//	voicecheck synth    --out ./wav              synthesise the audit text in every voice and radio profile
//	voicecheck audit    --model de_DE-thorsten-medium   swap-trick audit for one non English model
//	voicecheck recog    --backend fake|sapi --script testdata/stt/readbacks.jsonl
//	voicecheck devices                            list input and output devices
//	voicecheck voices                             report pool size, accents and what is missing
//	voicecheck download --model <name>            fetch a model from the voice repository
//
// synth and audit produce WAVs for a human to listen to; recog and voices are
// machine checkable and exit non-zero on failure, which is what makes them
// usable from CI and from a pre-release script.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	voicegoio "github.com/mrlm-net/voice-goio"
	"github.com/mrlm-net/voice-goio/audio"
	"github.com/mrlm-net/voice-goio/audio/radio"
	"github.com/mrlm-net/voice-goio/internal/wav"
	"github.com/mrlm-net/voice-goio/stt/fake"
	"github.com/mrlm-net/voice-goio/stt/sapi"
	"github.com/mrlm-net/voice-goio/tts"
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
	engine, name, err := tts.Open(tts.Options{Prefer: *backend})
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
	engine, name, err := tts.Open(tts.Options{Prefer: *backend})
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
		backend = fs.String("backend", "fake", "fake or sapi")
		script  = fs.String("script", filepath.Join("testdata", "stt", "readbacks.jsonl"), "corpus")
		minPct  = fs.Float64("min", 95, "minimum percentage of exactly tagged phrases")
		wavDir  = fs.String("wav", "", "render each phrase to a WAV here and feed the file to the engine, "+
			"instead of waiting for someone to say it into a microphone (sapi only)")
		ttsName = fs.String("tts", "", "tts backend used to render the phrases")
	)
	fs.Parse(args)

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
			return recogSAPIFiles(lines, *minPct, *wavDir, *ttsName)
		}
		return recogSAPI(lines, *minPct)
	default:
		return fmt.Errorf("unknown backend %q", *backend)
	}
}

// recogFake runs the corpus through the Go parser, which is the reference
// implementation of the grammar.
func recogFake(lines []fake.ScriptLine, minPct float64) error {
	p := fake.NewParser()
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
	rec, err := sapi.New(sapi.Options{})
	if err != nil {
		return err
	}
	defer rec.Close()

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
		fmt.Printf("\nSay: \x1b[1m%s\x1b[0m\n", l.Text)
		if err := rec.Start(); err != nil {
			return err
		}
		time.Sleep(6 * time.Second) // the operator speaks
		if err := rec.Stop(); err != nil {
			return err
		}
		got := <-rec.Results()
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

// recogSAPIFiles is the unattended form of the corpus run: every phrase is
// rendered to a WAV and played into the engine through SpFileStream, so the
// same expected-tags file validates the Windows backend without a person
// saying forty phrases into a headset (SPEC.md 4.5).
func recogSAPIFiles(lines []fake.ScriptLine, minPct float64, wavDir, ttsName string) error {
	if err := os.MkdirAll(wavDir, 0o755); err != nil {
		return err
	}
	engine, name, err := tts.Open(tts.Options{Prefer: ttsName})
	if err != nil {
		return err
	}
	defer engine.Close()

	rec, err := sapi.New(sapi.Options{})
	if err != nil {
		return err
	}
	defer rec.Close()

	pool, err := loadPool("", "", 7)
	if err != nil {
		return err
	}
	// One voice for the whole corpus: this measures the grammar, not the
	// engine's tolerance for different speakers. Vary the voice deliberately
	// with a second run if that is what you want to measure.
	voice := pool.Assign("EGLL", voicegoio.Approach)

	fmt.Printf("rendering with %s, feeding files to the engine\n", name)
	var total, exact int
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
		path := filepath.Join(wavDir, fmt.Sprintf("%03d.wav", i))
		pcm, err := engine.Synthesize(context.Background(), voice, l.Text)
		if err != nil {
			return fmt.Errorf("render %q: %w", l.Text, err)
		}
		if err := wav.WriteFile(path, pcm, engine.SampleRate(voice)); err != nil {
			return err
		}
		got, err := rec.RecognizeFile(path)
		if err != nil {
			return fmt.Errorf("recognise %s: %w", path, err)
		}
		ok := tagsMatch(got.Tags, l.Tags)
		if ok {
			exact++
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

// ---- download --------------------------------------------------------------

func cmdDownload(args []string) error {
	fs := flag.NewFlagSet("download", flag.ExitOnError)
	var (
		manifest = fs.String("voices", "", "manifest path")
		dir      = fs.String("dir", "", "destination directory")
		model    = fs.String("model", "", "model name, or \"all\"")
		base     = fs.String("base", voices.DefaultBaseURL, "base URL or LAN mirror")
		write    = fs.String("write", "", "write the manifest back here with the hashes filled in")
	)
	fs.Parse(args)
	if *model == "" {
		return fmt.Errorf("download: -model is required (or -model all)")
	}
	pool, err := loadPool(*manifest, *dir, 7)
	if err != nil {
		return err
	}
	man := pool.Manifest()

	targets := man.Models
	if *model != "all" {
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
