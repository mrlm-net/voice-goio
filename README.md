# voice-goio

Offline voice input and output for an MSFS ATC application, as a Go library.

**v0.1.0** — the platform-independent half is complete and tested; the two
Windows backends are written and cross-compile but have not yet run against
real hardware. See [Platform status](#platform-status) before wiring it in.

- **No services.** Everything runs on the user's PC. Works with the network adapter disabled.
- **No dependencies.** `go.mod` has zero `require` lines. Standard library only, `CGO_ENABLED=0` everywhere.
- **Windows is the runtime target**, macOS is the development machine. Windows-only code is cross-compiled on every commit so it cannot rot.

The application deals in **text and semantic tags**. It never sees PCM, grammars or voice model files.

```
app text ─▶ normalise ─▶ TTS ─▶ radio chain ─▶ player ─▶ PlaybackEvent
pilot speech ─▶ STT ─▶ Recognition{intent, callsign, value} ─▶ app
```

## Quick start

```bash
make demo-arrival   # the full sequence: radar → approach → tower → ground
make demo           # one arrival, with the weather going off and a new ATIS
make demo-accents   # one clearance in many assigned voices
make demo-profiles  # one clearance through each radio profile
make demo-live      # type pilot calls, hear the controller answer
go run ./cmd/demo -mode emergency   # a mayday and a pan pan, heard and answered
```

`demo-arrival` is the one to run first. It is the whole library in one pass:
four controller positions, four frequencies, three handoffs, and all twelve
pilot calls put through the recogniser with their tags printed next to the
audio that produced them.

On macOS with no piper installed the demo falls back to the built-in `say`
backend, so the whole pipeline is audible before anything has been downloaded.
On Windows it uses piper.

```bash
make bars      # the two machine-checkable quality bars
make all       # fmt, vet, test, and the three cross builds
make deps      # prove go.mod is empty and nothing outside stdlib is reachable
```

## Using it from the application

The snippets below are compiled on every build — they live in
[`example_test.go`](example_test.go), so they cannot drift from the API.

```go
import (
    voicegoio "github.com/mrlm-net/voice-goio"
    "github.com/mrlm-net/voice-goio/audio"
    "github.com/mrlm-net/voice-goio/audio/radio"
    "github.com/mrlm-net/voice-goio/normalise"
    "github.com/mrlm-net/voice-goio/tts"
    "github.com/mrlm-net/voice-goio/voices"
)

engine, _, _ := tts.Open(tts.Options{})          // piper on Windows
man, _       := voices.LoadDefault()
pool         := voices.NewPool(man, voices.PoolOptions{Seed: sessionSeed})
chain        := radio.Default()
player, _    := audio.NewPlayer(audio.Options{DeviceID: settings.OutputDevice})
norm         := normalise.New()

// One ATC transmission.
voice := pool.Assign("LKPR", voicegoio.Tower)              // stable for the session
tx := voicegoio.Transmission{
    Frequency: "118.100", ControllerID: "LKPR_TWR",
    Phraseology: voicegoio.ICAO,
    Text: "BAW123 RWY 24 cleared to land, wind 250 degrees 8 kt",
}
pcm, _ := engine.Synthesize(ctx, voice, norm.Spoken(tx.Text, tx.Phraseology))
out := chain.Apply(pcm, engine.SampleRate(voice), voice.Radio, player.SampleRate(), seed)
player.Play(tx, out, player.SampleRate())

// Playback events tell you when the frequency is clear again.
for ev := range player.Events() { ... }
```

Recognition, on Windows:

```go
rec, err := sapi.New(sapi.Options{MinConfidence: 0.5})
rec.SetCallsigns(aircraftOnFrequency)   // on every roster change
rec.Start()                             // PTT pressed
rec.Stop()                              // PTT released
r := <-rec.Results()                    // exactly one result per cycle
switch r.Tags[voicegoio.TagIntent] { ... }
```

On macOS use `stt/fake` (typed input or a script) — the same interface.

As a typing convenience, `stt/fake` also accepts the identifier and plain
digits: `CSA1234 with you, 15000` and `DLH4EK request climb FL370` tag exactly
as their spoken equivalents do. Speech never produces those forms, so nothing
in the Windows backend has to cope with them.

## What is where

| Path | What it does |
|---|---|
| `voicegoio.go` | The whole public API: interfaces and value types. This file is the compatibility surface. |
| `normalise/` | `BAW123 climb FL350` → `Speedbird one two tree, climb flight level tree fife zero`. ICAO and FAA, plus emergency signals, METAR shorthand and the pauses. |
| `data/`, `grammar/` | Embedded telephony table and the SRGS grammar (leaf packages, because `go:embed` cannot reach out of its own directory). |
| `tts/piper/` | The shipping synthesiser: a pool of warm piper sidecars, JSON lines in, raw PCM out. |
| `tts/say/` | macOS development synthesiser. Real accents, no downloads. Not a shipping backend. |
| `tts/fake/` | Deterministic tone generator for CI and regression. |
| `stt/sapi/` | Windows SAPI 5 in-process recognizer over raw COM vtables. |
| `stt/fake/` | The tag parser plus stdin and script recognisers, for development and regression. |
| `audio/radio/` | The radio chain: band pass, presence, soft clip, noise, squelch, dropouts, level, resample. |
| `audio/` | Per-frequency queues and playback events; `winmm` on Windows, `afplay` on macOS, WAV files elsewhere. |
| `voices/` | Manifest, downloader, region-weighted voice assignment. |
| `cmd/voicecheck` | Regression and audit CLI. |
| `cmd/demo` | The whole pipeline, assembled and audible. `-mode arrival` is the full one: four positions, four frequencies, three handoffs, every pilot call recognised. |

## Platform status

| Component | macOS | Windows | Linux |
|---|---|---|---|
| normalise, radio chain, voices, grammar parser | ✅ | ✅ | ✅ |
| TTS | ✅ `say` (dev) + piper | ✅ piper | ✅ piper |
| Playback | ✅ afplay, default device only | ✅ `winmm`, device selectable | WAV files |
| Recognition | ✅ `stt/fake` | ✅ `stt/sapi` — **written, not yet bring-up tested on hardware** | `stt/fake` |

Two things carry real risk and are called out rather than buried:

- **`stt/sapi` has never run against a live engine.** It is complete — COM
  creation, audio input selection, grammar load, dynamic callsign rule, PTT,
  event pump, semantic property extraction — and it compiles and vets clean for
  `windows/amd64`. What it has not done is talk to SAPI. The struct layouts are
  pinned by `layout_windows_test.go`, which runs in CI on `windows-latest`, and
  every failure path returns a typed error. Expect to spend a session on
  bring-up (SPEC.md build order step 6).
- **`tts/piper` has never run against the real piper binary**, only against
  `testdata/fakepiper`, which speaks the same protocol. The part to verify
  first is utterance framing (below) and whether the shipped binary accepts
  `length_scale` per JSON line (`piper.CheckFlags` reports this).

## Prosody, emergencies and the ATIS

**Pauses are the normaliser's job.** Both piper (through espeak) and macOS `say`
take their pauses from punctuation, so inserting commas at semantic boundaries
is the only lever there is. The normaliser adds one after a callsign, between
the elements of a taxi route (`TWY A3 and B` → "taxiway alpha tree, bravo"),
after a distress signal, and between an information unit and the instruction
that follows it. That last rule keys off having just finished a keyword group
rather than off the verb alone, which is what stops `radar contact` becoming
"radar, contact".

**Distress and urgency.** `MAYDAY` and `PAN PAN` are recognised by both
backends and outrank every other intent, including `say_again`: a transmission
containing "mayday" is a mayday whatever else is in it. The callsign is matched
*anywhere* in the transmission, because a distress call puts the signal first,
and it is reported confidently even when the aircraft is not on the roster — an
aircraft in trouble may have just arrived on the frequency. `Tags[value]`
carries the nature (`engine_failure`, `low_fuel`, `medical`, …) on a best
effort basis; an unrecognised nature never downgrades the intent.

The library pronounces what the application writes and does not rewrite
phraseology, so the application sends the real thing —
`MAYDAY MAYDAY MAYDAY` or `PAN PAN, PAN PAN, PAN PAN`. It does know that
"mayday" is a one-word signal and "pan pan" a two-word one, so the pauses land
between the repeats instead of inside them.

**The ATIS gets one fixed machine voice.** It is not assigned from the regional
pool and does not consume an airport's voices: an ATIS is a recording played on
a loop, so it is the same flat voice everywhere, and giving Gatwick a different
ATIS reader from Heathrow would be wrong rather than varied. The normaliser
also speaks METAR shorthand, so a broadcast can be written the way the
application already holds it — `BKN020` → "broken two thousand", `10KM BR` →
"one zero kilometres mist", `NOSIG` → "no significant change".

## Three decisions worth knowing about

**Utterance framing.** In `--output-raw` mode piper writes the samples for a
line and then waits. There is no length prefix and no end marker. The backend
appends a fixed sentinel word after the real text, reads until stdout has been
quiet for 150 ms, and cuts the sentinel's samples back off (its length is
measured once per voice shape). If the trim is wrong, every transmission ends
with a stray "dot" — `TestSynthesizeTrimsSentinel` is what guards it.

**The dynamic callsign rule.** Restricting recognition to the aircraft actually
on frequency is worth more than any acoustic tuning. `SetCallsigns` clears and
rebuilds the grammar's `callsign` rule, adding one word transition per
phraseology variant, so a pilot may say "tree" or "three" for the same aircraft.

**Loudness, not peaks.** The radio chain compresses hard and normalises to
−15 dBFS RMS under a −3 dBFS soft ceiling. Normalising peaks instead — which is
what SPEC.md §4.6 asks for — leaves speech around −20 dBFS RMS because of its
15 dB crest factor, and it sounds distant and hard to follow. Compression is
also what real transmitters do, and it is the single biggest intelligibility
win in the chain. `internal/dsp` has the stage-by-stage level test.

## Known deviations from SPEC.md

**Radio chain order (§4.6).** Levelling happens on the speech, before the noise
floor and squelch bursts, rather than as the last stage before resampling, and
it targets RMS with a soft ceiling rather than a peak. Two reasons: a long
squelch tail otherwise drags the speech level down with it, and peak
normalisation does not control how loud speech actually sounds. A side effect
worth having is that the per-profile noise figures now mean something absolute
— −45 dBFS of noise under speech at −15 dBFS RMS is a 30 dB signal-to-noise
ratio on every profile, whatever the voice did.

**Phraseology (§4.1).** SPEC.md §4.1 shows `127.45` spoken as `one two seven decimal four **five**`
under ICAO, which contradicts the digit table two rows above it in the same
table (ICAO speaks 5 as "fife", as the `FL350` and `SQK 4321` rows do). The
digit table is the general rule, so it wins: the frequency reads
`one two seven decimal four fife`. If the literal spec text is wanted instead,
that is a one-line change in `normalise.digits` and one line in
`testdata/normalise/icao.txt`.

## Voices

Voice models are never bundled: hundreds of megabytes, their own licences, and
a user only needs their region. The manifest (`voices/voices.json`) lists 23
models covering 23 accents and 1145 assignable voices, which is past the
BeyondATC "Basic" bar of roughly 100 local neural voices.

A model declares every accent its speakers cover, not just one. That matters
for `en_GB-vctk-medium`, whose 109 speakers span the British Isles, Australia,
New Zealand, India, South Africa and Canada: calling the whole model "en-GB"
would leave a Sydney or Delhi controller with no regional voice at all.
`Assign` records the accent it chose on `VoiceProfile.Accent`.

```bash
go run ./cmd/voicecheck voices                             # pool report vs. the bar
go run ./cmd/voicecheck download -model en_GB-vctk-medium  # fetch one
go run ./cmd/voicecheck download -model all -write voices/voices.json
```

Non-English models (German, Czech, Dutch, Polish, French, Italian, Spanish)
produce non-native controllers via the **swap trick**: a copy of the model
config with `espeak.voice` forced to `en-us`, so English text is phonemised in
English and spoken by a model trained on another language. `voices` writes
those config copies itself. It is never done the other way round.

Speaker-level audit (`quality`, `pass` in the manifest) is a human listening
pass, so the manifest ships those fields unset rather than fabricated. Until a
model is audited its speakers are only assignable with
`PoolOptions.AllowUnaudited`. `voicecheck audit -model <name>` renders the
audit text in every speaker of a model for that pass.

## Licensing filter

`-tags commercial` restricts assignment to permissively licensed models. It is
a build tag rather than a setting so a shipped binary cannot be configured into
a licence breach. SPEC.md §10 leaves the distribution decision open; flipping
the tag is the whole change.

## Acceptance status (SPEC.md §9)

| Criterion | Status |
|---|---|
| `go.mod` zero requires; `CGO_ENABLED=0` builds for darwin/arm64, windows/amd64, linux/amd64 | ✅ enforced by test and CI |
| Normaliser covers every row of §4.1 | ✅ `testdata/normalise/*.txt`, one deviation documented above |
| ≥ 100 voices, ≥ 10 accents | ✅ 1145 voices, 23 accents (`voicecheck voices`) |
| ≥ 95 % intent + callsign on the corpus; off-grammar yields `say_again` | ✅ 34/34 on `stt/fake` (`voicecheck recog`) |
| Radio profiles audibly distinct; speech intelligible through `center` | ✅ asserted in `audio/radio`, audible via `demo -mode profiles` |
| macOS output documented as default-device only | ✅ `SetDevice` is a no-op there and says so |
| Windows output device selection | ⏳ implemented (`winmm`), untested on hardware |
| Network disabled: full flow on Windows | ⏳ needs SAPI bring-up and a piper install |
| TTS first sample < 300 ms warm | ⏳ needs the real piper binary to measure |

## Consuming this from the sim application

The module is private, so both machines need:

```bash
go env -w GOPRIVATE=github.com/mrlm-net
```

While the library and the application are changing together, point the app at
the working copy rather than at a tag — a `go.work` beside both checkouts is
the least intrusive way, because it leaves the app's `go.mod` alone:

```
go 1.24

use (
    ./msfs-atc-app
    ./voice-goio
)
```

Remove it, or drop the `replace`, before a release build. When the library
settles:

```bash
go get github.com/mrlm-net/voice-goio@v0.1.0
```

### What to wire first

1. **Playback.** `tts.Open` → `pool.Assign` → `norm.Spoken` → `chain.Apply` →
   `player.Play`, and drain `player.Events()`. This works today on macOS with
   no downloads, so the integration can be proven before any Windows work.
2. **Settings.** `player.Devices()` for the output device; on Windows,
   `sapi.InputDevices()` for the microphone and `sapi.Engines()` to show
   whether a speech pack is installed at all.
3. **Voices.** `voicecheck download -model all` once, or call
   `voices.Downloader` from the app's first-run screen. `pool.Missing()`
   reports what has not been fetched.
4. **Recognition.** `stt/fake` on the development machine, `stt/sapi` on
   Windows. Same interface, so the wiring is written once.

### The compatibility surface

`voicegoio.go` is the only file the application should depend on the shape of.
Everything else is an implementation behind one of its interfaces. Breaking
changes to it mean a `/v2` module path; anything else is additive.

## Versioning

Requires Go 1.27 or newer. The `go` directive in `go.mod` is a floor on the
consumer's toolchain, not a target, so it is set to the version the library is
actually developed and tested on rather than to the oldest one that happens to
compile. A machine on an older toolchain will fetch 1.27 automatically on the
first build.

| Tag | Contents |
|---|---|
| `v0.1.0` | Everything platform-independent, complete and tested. Windows backends compile and are wired, but have not run against hardware. |
| `v0.2.0` | After Windows bring-up: `stt/sapi` against a real engine, `winmm` against a real device, piper against the real binary. |
