# voice-goio

[![Go Reference](https://pkg.go.dev/badge/github.com/mrlm-net/voice-goio.svg)](https://pkg.go.dev/github.com/mrlm-net/voice-goio)
[![Docs](https://img.shields.io/badge/docs-goio.mrlm.net-8a6418)](https://goio.mrlm.net/)
[![License: BSL 1.1 · non-commercial](https://img.shields.io/badge/license-BSL%201.1%20%C2%B7%20non--commercial-6c7480)](LICENSE)

Offline ATC voice input and output, as a Go library.

Controller speech out, pilot speech in, for any application that needs a radio:
a flight simulator add-on, a training tool, a controller trainer. The library
is aviation-specific — the normaliser, the grammar, the radio chain and the
voice pool are all built around ATC phraseology — but it knows nothing about
any particular simulator. There is no SimConnect here, no MSFS, no assumption
about where the transmissions come from. An application hands it text and gets
audio; it hands the application tags.

> [!WARNING]
> **Under active development, and this README is as much a working notebook as
> it is documentation.** The API is pre-1.0 and versions come fast. The output
> side (`tts/piper`, `speaker`, `audio` on `winmm`) runs on Windows hardware in
> two applications; recognition (`stt/sapi`) is not exercised by current
> consumers and has never run against a live engine. Much of what follows is
> design reasoning rather than a stable contract.

**v0.13.1** — the output side is in use on Windows: piper, voice packs, the
`speaker` with its radio, intercom, PA and chime channels. Recognition on
Windows is written but untried. See [Platform status](#platform-status) and
[Using it in an app](#using-it-in-an-app).

- **No services.** Everything runs on the user's PC. Works with the network adapter disabled.
- **No dependencies.** `go.mod` has zero `require` lines. Standard library only, `CGO_ENABLED=0` everywhere.
- **Windows is the runtime target** (because the first consumer is an MSFS add-on, and MSFS is Windows-only), macOS is the development machine. Windows-only code is cross-compiled on every commit so it cannot rot.

The application deals in **text and semantic tags**. It never sees PCM, grammars or voice model files.

```
app text ─▶ normalise ─▶ TTS ─▶ radio chain ─▶ player ─▶ PlaybackEvent
pilot speech ─▶ STT ─▶ Recognition{intent, callsign, value} ─▶ app
```

## Quick start

```bash
make demo-arrival   # the full sequence: radar → approach → tower → ground
make demo-emergency # a mayday with a second aircraft on the same runway
make demo           # one arrival, with the weather going off and a new ATIS
make demo-accents   # one clearance in many assigned voices
make demo-profiles  # one clearance through each radio profile
make demo-live      # type pilot calls, hear the controller answer
make demo-wav       # render every demo to one WAV each, silently
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

The ATC grammar is the default, not the only one. An application listening
for its own phrases (cockpit commands: "request taxi", "gear up", "doors
closed") builds a grammar from them and hands it to SAPI; each phrase comes
back as its intent. `grammar.Match` is the same matching for typed input and
away from Windows. A hand-written SRGS file works too (`Options.GrammarPath`):
its public root rule must be `transmission`, its tags set `out.intent`, and a
`callsign` rule is what `SetCallsigns` rebuilds.

```go
cmds := []grammar.Command{
    {Intent: "gear_up", Phrases: []string{"gear up", "landing gear up"}},
    {Intent: "doors_closed", Phrases: []string{"doors closed"}},
}
g, _ := grammar.Commands(cmds)
rec, err := sapi.New(sapi.Options{Grammar: g}) // r.Tags["intent"] == "gear_up"
intent, ok := grammar.Match(cmds, "Gear up")  // typed: "gear_up", true
```

As a typing convenience, `stt/fake` also accepts the identifier and plain
digits: `CSA1234 with you, 15000` and `DLH4EK request climb FL370` tag exactly
as their spoken equivalents do. Speech never produces those forms, so nothing
in the Windows backend has to cope with them.

### The radio, heard: `speaker`

`speaker` is the whole output side with the rules the simconnect airport map
uses, so every application sounds the same: piper with the default pool (no
Czech voice reading English, one female voice in nine), a voice per
controller position with shifts, a voice per crew, one frequency followed,
one call at a time with 1 to 5 s between calls, nothing said more than 60 s
late, and the ATIS as a looping broadcast joined mid-sentence.

```go
sp := speaker.New(speaker.Options{PiperPath: piperExe, VoicesDir: voicesDir})
sp.Set(true, "118.105")                 // on, following one frequency
sp.SetATIS("122.155", "LKPR", atisText) // the broadcast on its frequency
sp.Hear(speaker.Utterance{Airport: "LKPR", Position: speaker.PosTower,
    Callsign: "CSA123", Frequency: "118.105", Text: "CSA123, runway 24, cleared to land"})
fmt.Println(sp.State().Status)          // "on (piper)", or why it is silent
```

The crew and the cabin talk on the **intercom**: the same voices, without the
radio chain (no band-pass, noise or squelch), and not tied to the frequency
followed — it speaks with the radio off, on another frequency or none, on its
own queue beside the radio. `Utterance.Voice` gives the voice explicitly (each
crew member in the voice the player chose) and wins over the pool's pick, on
the intercom and on the radio. `voices.Dir()` is the per-user models folder
and `voices.Installed` lists what is in it, for a voice picker.

```go
man, _ := voices.LoadDefault()
copilot := voices.Installed(man, voices.Dir())[0].Profile(0) // model + speaker the player chose
sp.SayIntercom("Before start checklist complete", copilot)
sp.Hear(speaker.Utterance{Intercom: true, Position: "purser", Text: "Cabin secure", Voice: &purser})
```

The package documentation (`go doc ./speaker`) has the mapping from
simconnect's `traffic.Transmission` and what has to sit beside the
application at runtime: piper's folder (`bin/piper/piper.exe` next to the
executable by default, with its DLLs, `espeak-ng-data` and
`libtashkeel_model.ort`) and the voice models (`%LOCALAPPDATA%\voice-goio\voices`
by default). The speaker writes nothing on Windows, so both can live in a
read-only install folder.

## Using it in an app

What an application needs to ship voice output: piper, the voice models, a
voice per station and per crew, and the `speaker`. This is how both current
consumers (the MyCrew app and the simconnect airport map) use it. The snippets
compile against v0.13.1.

### Installing piper

Piper is fetched, not bundled. `piper.Install` downloads the pinned Windows
release (2023.11.14-2), checks its SHA-256 and unzips it to `dir/piper`.
`piper.DefaultPiperPath()` is `bin/piper/piper.exe` next to the running
executable, which is what `speaker.Options.PiperPath` defaults to, so
`dir = <exe dir>/bin` lines the two up. Install does nothing when piper is
already there.

```go
if _, err := os.Stat(piper.DefaultPiperPath()); err != nil {
    exe, _ := os.Executable()
    err = piper.Install(ctx, filepath.Join(filepath.Dir(exe), "bin"),
        func(done, total int64) { /* progress bar */ })
}
```

An application that keeps piper elsewhere (MyCrew uses its own per-user
folder) installs there and passes that path as `PiperPath`.
`(*piper.TTS).Available()` reports whether the binary is present.

### Voice packs

Models go into the per-user folder, `voices.Dir()`
(`%LOCALAPPDATA%\voice-goio\voices` on Windows). Packs are the sets an
installer offers: `voices.Packs()` is `core`, `en`, `all`; `voices.PackCore` is
the default (the English voices the pool uses most plus one accent model per
country). `InstallPack` checks each model's SHA-256, resumes broken downloads
and skips what is installed, so it is safe to run again.

```go
man, _ := voices.LoadDefault()
for _, p := range voices.Packs() {
    size, _ := man.PackSize(p) // for a "download 714 MB?" prompt
}
err := voices.InstallPack(ctx, voices.PackCore, "", // "" is voices.Dir()
    func(done, total int64, model string) { /* progress */ })

installed := voices.Installed(man, voices.Dir()) // what is on disk: a voice picker
```

`man.Pack(name)` lists a pack's models (compare with `Installed` for what is
missing). For a single model, `voices.Downloader{Dir, OnProgress}` and
`Fetch(ctx, model)` download one, reporting `voices.Progress` (model, file,
bytes downloaded, total).

### Assigning voices

The `speaker` does this itself. An application that assigns voices on its own
uses a `voices.Pool`:

```go
pool := voices.NewPool(man, voices.PoolOptions{Dir: voices.Dir(), FemaleShare: 1.0 / 9})
twr := pool.Assign("LKPR", voicegoio.Tower)          // the same voice all session
gnd := pool.Assign("LKPR", speaker.KindOf("ground")) // never the tower's speaker
crew := pool.AssignCrew("CSA123")                    // never a controller's voice
```

- A controller position at an airport keeps its voice, and two positions at
  the same airport never share one. The speaker hands a position over to a new
  voice every 30 to 60 minutes (a shift change).
- **One controller, one voice on every frequency (v0.13.0).** Setting
  `Utterance.Controller` keys the voice by the person rather than the position:
  ground and tower worked by one controller sound like one person, with the
  radio sound of the `Position` each call is made on.
- **Crews never get a controller's voice (v0.13.1).** `Pool.AssignCrew` keeps
  a crew's voice for the session, with a cockpit's radio sound, and keeps
  controller and crew voices apart while either side has a free one. The
  speaker uses it for every pilot call.
- `speaker.KindOf(position)` maps a position string to a
  `voicegoio.ControllerKind` (its voice and radio sound).

### Speaking

```go
sp := speaker.New(speaker.Options{
    PiperPath: piperPath, // "" bin/piper/piper.exe next to the executable
    VoicesDir: "",        // "" voices.Dir()
    Hint:      "install the voices in Settings",
})
defer sp.Close()

sp.Set(true, "118.105") // on, following one frequency
sp.Hear(speaker.Utterance{Airport: "LKPR", Position: speaker.PosTower, Controller: "LKPR_TWR",
    Callsign: "CSA123", Frequency: "118.105", Text: "CSA123, runway 24, cleared to land",
    Phraseology: voicegoio.ICAO}) // voicegoio.FAA at a US airport
sp.Hear(speaker.Utterance{Airport: "LKPR", Position: speaker.PosTower, Callsign: "CSA123",
    Pilot: true, Frequency: "118.105", Text: "Cleared to land runway 24, CSA123"})

captain := installed[0].Profile(0)  // a voice the player chose
sp.SayIntercom("Before start checklist complete", captain)
sp.Chime(speaker.ChimePA)           // ChimeCall, ChimePA, ChimeSeatbelt
sp.SayPA("Ladies and gentlemen, welcome aboard", captain)
sp.SetDeviceFor(speaker.ChannelPA, deviceID) // ChannelRadio, ChannelIntercom, ChannelPA

fmt.Println(sp.State().Status) // "on (piper)", "off", or why it is silent
```

- **Channels.** The radio (`ChannelRadio`) follows one frequency through the
  radio chain. The intercom (`ChannelIntercom`) and the cabin PA (`ChannelPA`)
  are dry or through the cabin speaker chain (`PAChain`), not tied to the
  frequency, each with its own queue. Each channel can have its own output
  device (`SetDeviceFor`; `""` follows `SetDevice`). One voice says one line at
  a time across channels.
- **Chimes** are generated in code: `Chime(c)` queues one in order on its
  channel, `Utterance.Chime` does the same through `Hear`, and `ChimePCM(c)` at
  `ChimeRate` exports the sound.
- **WAV.** `sp.Clip(u)` renders an utterance in memory without a player (for a
  client that plays on its own device), and `speaker.WAV(pcm, rate)` makes it a
  WAV file, which is how the airport map serves audio to its browser clients.
- `Utterance.Voice` (a `*voicegoio.VoiceProfile`) forces a voice and wins over
  the pool on every channel.

### Small helpers

- `speaker.SpokenEnd(text)`: ends a call with a full stop, so piper does not
  cut its last syllable.
- `normalise.SpokenVariants(spoken)`: every phraseology variant of a spoken
  callsign ("three" and "tree"), for a recogniser's callsign list.
- `radio.PeakDBFS(pcm)` / `radio.RMSdBFS(pcm)`: a signal's peak and RMS level in
  dBFS.
- `(*audio.Player).Recording()`: the path and length of the session recording,
  when one is being made.
- `voices.Load(path)` / `(*voices.Manifest).Save(path)`: read and write a
  manifest other than the embedded one; `radio.Load(b)` reads radio profiles.

## What is where

| Path | What it does |
|---|---|
| `voicegoio.go` | The whole public API: interfaces and value types. This file is the compatibility surface. |
| `normalise/` | `BAW123 climb FL350` → `Speedbird one two tree, climb flight level tree fife zero`. ICAO and FAA, plus emergency signals, METAR shorthand and the pauses. |
| `data/`, `grammar/` | Embedded telephony table and the SRGS grammar, and `grammar.Commands` for an application's own phrases (leaf packages, because `go:embed` cannot reach out of its own directory). |
| `tts/piper/` | The shipping synthesiser: a pool of warm piper sidecars, JSON lines in, raw PCM out. |
| `tts/say/` | macOS development synthesiser. English voices only, no downloads. Not a shipping backend. |
| `tts/fake/` | Deterministic tone generator for CI and regression. |
| `stt/sapi/` | Windows SAPI 5 in-process recognizer over raw COM vtables. |
| `stt/fake/` | The tag parser plus stdin and script recognisers, for development and regression. |
| `audio/radio/` | The radio chain: band pass, presence, soft clip, noise, squelch, dropouts, level, resample. |
| `speaker/` | The radio, heard: voices per position, controller and crew, one frequency, the queue, the gaps, the ATIS broadcast — the rules applications share. Beside it the intercom (dry), the cabin PA (`PAChain`), code-generated chimes (`chime.go`), an output device per channel, and `WAV`/`Clip` for in-memory audio. |
| `audio/` | Per-frequency queues and playback events; `winmm` on Windows, `afplay` on macOS, WAV files elsewhere, a silent sink for rendering. |
| `voices/` | Manifest, packs (`core`, `en`, `all`) and `InstallPack`, downloader, `Installed`/`Dir`, region-weighted voice assignment (`Pool.Assign`, `Pool.AssignCrew`). |
| `tts/piper/install.go` | `piper.Install`: the pinned piper release for Windows, SHA-256 checked. |
| `internal/dsp` | Signal processing primitives the radio chain is built from: biquads, resampling, level helpers. |
| `internal/wav` | Read and write 16-bit PCM WAV. |
| `internal/userdir` | The per-user data folder, so `voices` and `tts/piper` agree on where models live. |
| `internal/jsonl` | The JSON-lines protocol written to piper's stdin. |
| `cmd/voicecheck` | Regression and audit CLI. |
| `cmd/demo` | The whole pipeline, assembled and audible. `-mode arrival` is the full one: four positions, four frequencies, three handoffs, every pilot call recognised. |

## Platform status

| Component | macOS | Windows | Linux |
|---|---|---|---|
| normalise, radio chain, voices, grammar parser | ✅ | ✅ | ✅ |
| TTS | ✅ `say` (dev) + piper | ✅ piper, in use in both consumers | ✅ piper |
| Playback (`audio`, `speaker`) | ✅ afplay, default device only | ✅ `winmm`, device per channel, in use in both consumers | WAV files |
| Recognition | ✅ `stt/fake` | ⏳ `stt/sapi` — written, **not exercised by current consumers**, never run against a live engine | `stt/fake` |

The consumers are the MyCrew app (`mycrew-online/app`, `internal/voice`) and
the simconnect airport map (`cmd/airport-map`); both run piper, the speaker
and `winmm` playback on Windows hardware. Neither imports `stt/...`.

- **`stt/sapi` has never run against a live engine.** It is complete — COM
  creation, audio input selection, grammar load, dynamic callsign rule, PTT,
  event pump, semantic property extraction — and it compiles and vets clean for
  `windows/amd64`. What it has not done is talk to SAPI. The struct layouts are
  pinned by `layout_windows_test.go`, which runs in CI on `windows-latest`, and
  every failure path returns a typed error. Expect to spend a session on
  bring-up (SPEC.md build order step 6).
- **`tts/piper` runs against the real piper binary** on Windows in both
  consumers (sentence-at-a-time synthesis since v0.11.1). The tests still run
  against `testdata/fakepiper`, which speaks the same protocol.

  Do not verify piper on an Apple Silicon Mac. The
  `piper_macos_aarch64.tar.gz` asset of release 2023.11.14-2 contains an
  **x86_64** binary despite its name (`file` says so, and `lipo -archs` agrees).
  Under Rosetta it starts and then hangs indefinitely, producing no output at
  all — not for `--help`, and not for a one-sentence synthesis with a model
  that downloads and verifies correctly. `piper_windows_amd64.zip` is native on
  the target platform, so the bring-up session is the place to do this.

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

Models are downloaded to and read from the per-user folder, `voices.Dir()`
(`%LOCALAPPDATA%\voice-goio\voices` on Windows, never the working directory);
`voices.Installed(manifest, dir)` lists the manifest's models present in a
folder, and `Model.Profile(speakerID)` is one of their voices as a
`VoiceProfile`.

```bash
go run ./cmd/voicecheck voices                             # pool report vs. the bar
go run ./cmd/voicecheck download -model en_GB-vctk-medium  # fetch one
go run ./cmd/voicecheck download -model all -write voices/voices.json
```

Only piper renders non-native accents. The macOS development backend is
English voices only: macOS ships Czech and German *language* voices, and
handing them English text makes them mispronounce it rather than accent it,
which is exactly what SPEC.md §4.3 forbids. Native accents there (en-GB,
en-US, en-IE, en-AU, en-IN, en-ZA) are genuine English voices and are real.

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
| Windows output device selection | ✅ `winmm`, per channel (`SetDeviceFor`), in use in both consumers |
| Network disabled: full flow on Windows | ⏳ output side runs offline once piper and the models are installed; recognition needs SAPI bring-up (not exercised by current consumers) |
| TTS first sample < 300 ms warm | ⏳ not measured |

## Consuming this from an application

The module is private, so both machines need:

```bash
go env -w GOPRIVATE=github.com/mrlm-net
```

While the library and the application are changing together, point the app at
the working copy rather than at a tag — a `go.work` beside both checkouts is
the least intrusive way, because it leaves the app's `go.mod` alone:

```
go 1.27

use (
    ./mycrew-online/app // or ./your-app
    ./voice-goio
)
```

Remove it, or drop the `replace`, before a release build. Against a tag:

```bash
go get github.com/mrlm-net/voice-goio@v0.13.1
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
| `v0.1.x` | Everything platform-independent, complete and tested. Windows backends compile and are wired, but have not run against hardware. |
| `v0.2.0` | Recognition failure reasons (`TagReason`), session recording, emergency and deviation phraseology. |
| `v0.3.0` | Departure clearance readbacks, WAV input for the Windows recogniser so the corpus runs unattended, and a fix for models downloading into the working directory. |
| `v0.4.0` | Business Source License 1.1 (non-commercial; Apache-2.0 four years after each release). Taxiway letters after "via" are spelled. |
| `v0.5.0` | `speaker`: the radio as applications speak it (voices per position and crew, one frequency, the queue, gaps, the ATIS broadcast). Female share in the voice pool; piper sentinel cut at its silence. |
| `v0.6.0` | The intercom, explicit voices (`Utterance.Voice`), `voices.Dir`/`Installed`, application grammars (`grammar.Commands`). |
| `v0.7.0` | The Czech controller voice through RP phonemes; `speaker.Options.Exclude`. |
| `v0.7.1` | Every accent model reads English through RP phonemes. |
| `v0.8.0` | Voice packs (`en`, `all`) and `Manifest.WritePack`. |
| `v0.9.0` | Controller accents opt-in (`speaker.Options.Accents`, off by default). |
| `v0.10.0` | `PackCore`, an installer's default pack. |
| `v0.11.0` | `voices.InstallPack`, `Manifest.PackSize`, `piper.Install`. |
| `v0.11.1` | Fix: piper says a text of several sentences whole. |
| `v0.11.2` | Cabin chimes on the intercom. |
| `v0.12.0` | An output device per channel; the cabin PA channel and `PAChain`. |
| `v0.12.1` | Fix: one voice says one line at a time across channels. |
| `v0.13.0` | One controller, one voice on every frequency it works (`Utterance.Controller`). |
| `v0.13.1` | `Pool.AssignCrew`: crews never speak in a controller's voice. |
| `v0.14.0` | `Utterance.Tempo`: speaking rate per utterance (a busy frequency, an urgent call), within `MinTempo`…`MaxTempo`. |
| `v0.15.0` | `Options.OneAtATime`: the radio, intercom and PA take turns, one utterance at a time (radio first, ATIS last). |

Versions describe what changed, not what is planned. SPEC.md §5 earmarked
`v0.2.0` for the Windows bring-up; that number went to an earlier release
because it added API, and pre-assigning numbers to milestones turned out to be
a way of being wrong twice. The bring-up will land in whatever version follows
it.

## License

Business Source License 1.1, see [LICENSE](LICENSE), for versions after v0.3.1. Non-commercial use is free: personal and hobby use, the flight-simulation community, education, research and non-profits. Commercial use, such as a paid add-on or product, a paid service or use inside a business, needs a separate licence; [open an issue](https://github.com/mrlm-net/voice-goio/issues) to ask. Each version becomes Apache-2.0 four years after it is published, and versions up to and including v0.3.1 remain under Apache-2.0.
