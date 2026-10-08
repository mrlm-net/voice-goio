---
title: Design Spec (historic)
description: The original design brief, kept for its reasoning and the acceptance criteria the docs refer to.
order: 40
section: reference
---

# voice-goio — Offline Voice I/O Library for an MSFS ATC App (Agent Brief / SPEC.md)

> [!NOTE]
> **Historic: the original design spec.** It is kept for its reasoning and the
> acceptance criteria the README refers to (§4, §9). The repository layout and
> the version plan below are outdated (packages such as `speaker`, voice packs
> and `piper.Install` came later). The [README](../README.md) and the
> [CHANGELOG](../CHANGELOG.md) are current.

## 0. Goal

`voice-goio` is a standalone Go library providing voice input (pilot → ATC) and voice output (ATC → pilot) for an existing Go application that controls traffic in MSFS 2020/2024. The app imports it as a versioned module; the library knows nothing about MSFS or SimConnect.

Hard constraints:

- **No third-party services.** Everything runs on the user's PC, offline. A test with the network adapter disabled must pass.
- **No external Go dependencies.** `go.mod` has zero `require` lines. Stdlib only. Native platform APIs via `syscall` (Windows) or subprocesses. **`CGO_ENABLED=0` for every build.**
- **English only**, but controllers must have varied accents (US regional, UK regional, Australian/NZ/Indian/South African, and non-native: German, Dutch, Polish, Czech, French, Spanish, Italian).
- **Runtime target: Windows 10/11 x64** (MSFS is Windows-only). **Development happens on macOS first**; Windows-only code is cross-compiled from macOS on every commit so it never rots.
- Quality bar: match or exceed BeyondATC's free "Basic" voice tier (~100 local neural voices with accents).

## 1. Repository layout

Module path: `github.com/mrlm-net/voice-goio`. Package name: `voicegoio` (hyphens are not legal in Go identifiers; the import path keeps the hyphen).

```
voice-goio/
├── go.mod                    # zero requires
├── voicegoio.go               # public API: interfaces + types (§2)
├── normalise/                # text → spoken form (icao|faa); telephony.csv via go:embed
├── tts/
│   └── piper/                # sidecar process pool, JSON-lines stdin, PCM stdout
├── stt/
│   ├── sapi/                 # //go:build windows — SAPI5 via syscall COM vtables (§4.4)
│   └── fake/                 # all platforms: stdin text / WAV+expected-tags harness (§4.5)
├── audio/
│   ├── radio/                # DSP: biquads, soft clip, noise, squelch, profiles (§4.6)
│   ├── out.go                # Player interface + device enumeration types
│   ├── out_windows.go        # //go:build windows — winmm.dll waveOut via syscall, device selectable
│   ├── out_darwin.go         # //go:build darwin — afplay subprocess, default device only (dev)
│   └── out_other.go          # //go:build !windows && !darwin — writes WAV to a file
├── voices/                   # manifest (voices.json), downloader (net/http), audit runner
├── grammar/atc.grxml         # SRGS grammar (§4.4)
├── data/telephony.csv        # ICAO 3-letter → telephony designator
├── internal/wav/             # WAV reader/writer
├── internal/jsonl/           # JSON-lines encoder for the piper sidecar
├── cmd/voicecheck/           # synth + recognise regression CLI (§6)
├── testdata/                 # ATC test phrases, expected normalisations, expected recognition tags
├── SPEC.md                   # this file
├── README.md
└── .github/workflows/ci.yml  # §7
```

Rules:

- Windows-only files carry `//go:build windows`; macOS-only `//go:build darwin`. Every platform-specific file has a counterpart so `go build ./...` succeeds on darwin, windows and linux.
- Windows-only implementations are created as **compiling stubs first** (return `voicegoio.ErrNotImplemented`) so the app can depend on the module before the Windows work is done.
- No cgo anywhere. If a task appears to need it, stop and report — do not add it.

## 2. Public API (`voicegoio.go`)

```go
package voicegoio

var ErrNotImplemented = errors.New("voicegoio: not implemented on this platform")

type Phraseology string
const (
    ICAO Phraseology = "icao"
    FAA  Phraseology = "faa"
)

// Transmission is what the app sends to be spoken. Text uses the app's normal tokens
// (BAW123, FL350, 127.45, RWY 27L); the library normalises it.
type Transmission struct {
    Frequency    string      // "127.450"
    ControllerID string      // "LKPR_TWR"
    Phraseology  Phraseology
    Text         string
}

// Callsign describes an aircraft on frequency. Spoken is filled by normalise if empty.
type Callsign struct {
    ICAO   string // "BAW123", "OK-ABC"
    Spoken string // "Speedbird one two three"
}

type Recognition struct {
    Text       string            // raw recognised text
    Confidence float32           // 0..1
    Tags       map[string]string // semantic properties: intent, callsign, value, ...
}

type VoiceProfile struct {
    Model       string  // "en_GB-vctk-medium"
    SpeakerID   int
    LengthScale float32 // 0.80–0.95
    NoiseScale  float32 // 0.5–0.7
    NoiseW      float32 // 0.6–0.9
    Radio       string  // radio profile name: tower|ground|approach|center|atis
}

type TTS interface {
    // Synthesize returns 16-bit mono PCM at SampleRate(profile) for one transmission.
    Synthesize(ctx context.Context, profile VoiceProfile, spokenText string) ([]int16, error)
    SampleRate(profile VoiceProfile) int
    Close() error
}

type STT interface {
    SetCallsigns(cs []Callsign) error // rebuild dynamic callsign rule; call on every roster change
    Start() error                     // PTT pressed
    Stop() error                      // PTT released; result arrives on Results()
    Results() <-chan Recognition
    Close() error
}

type PlaybackEvent struct {
    Frequency    string
    ControllerID string
    Started      bool // true = started, false = finished
}

// Player owns the output device and per-frequency queues.
type Player interface {
    Devices() ([]Device, error)
    SetDevice(id string) error
    Play(t Transmission, pcm []int16, sampleRate int) error // queued per frequency
    Events() <-chan PlaybackEvent
    Close() error
}
```

## 3. Integration contract (app ↔ voice-goio)

| Direction | What | Shape |
|---|---|---|
| App → TTS | one ATC transmission | `Transmission` — raw tokens; library normalises, synthesises, applies radio chain, queues playback |
| App → STT | who is on frequency | `[]Callsign`; resend on every roster change |
| App → STT | PTT state | `Start()` on press, `Stop()` on release |
| STT → App | one recognition per PTT cycle | `Recognition{Tags: {intent, callsign, value}}` or `intent=say_again` |
| Player → App | playback events | `Started`/`Finished` per transmission so the app holds the next call until the frequency is clear |

The app never sees audio, PCM, grammars or voice files. Text in, tags out.

## 4. Components

### 4.1 Normaliser (`normalise/`) — build first

Converts ATC message text into spoken form. Pure Go, unit-tested against `testdata/normalise/*.txt`.

| Input | ICAO output | FAA output |
|---|---|---|
| `127.45` | one two seven decimal four five | one two seven point four five |
| digits `3`, `5`, `9`, `0` | tree, fife, niner, zero | three, five, niner, zero |
| `FL350` | flight level tree fife zero | flight level three five zero |
| `5000 ft` | fife thousand | five thousand |
| `HDG 270` | heading two seven zero | heading two seven zero |
| `RWY 27L` | runway two seven left | runway two seven left |
| `QNH 1013` | Q N H one zero one tree | altimeter two niner niner two (inHg conversion) |
| `SQK 4321` | squawk four tree two one | squawk four three two one |
| `TWY A3` | taxiway alpha tree | taxiway alpha three |
| `BAW123` | Speedbird one two tree | Speedbird one two three |
| `OK-ABC` (GA) | oscar kilo alpha bravo charlie | oscar kilo alpha bravo charlie |

- Telephony table `data/telephony.csv` (`BAW,Speedbird`; `CSA,CSA`; `TVS,Skytravel`; …) embedded via `go:embed`. Fallback: spell letters with NATO alphabet.
- Never emit bare digits or abbreviations — TTS only sees words.
- Exposes `SpokenCallsign(icao string) string`, used by STT to build the grammar.

### 4.2 TTS sidecar (`tts/piper/`)

- Binary: `piper` from `github.com/rhasspy/piper` releases — `piper_windows_amd64.zip` and `piper_macos_aarch64.tar.gz`; both ship `espeak-ng-data/`. Library locates it via `PiperPath` option; default `bin/piper/piper[.exe]` next to the executable.
- Voices: `.onnx` + `.onnx.json` pairs from `huggingface.co/rhasspy/piper-voices`. Stored under a user data dir (`%LOCALAPPDATA%/voice-goio/voices` on Windows, `~/Library/Application Support/voice-goio/voices` on macOS). Downloaded on first run (§4.3); never bundled.
- One long-lived process **per model file** (not per speaker). `--json-input` lets `speaker_id` change per line for multi-speaker models. Pool size default 4, LRU eviction.
- Launch:

  ```
  piper --model <voice>.onnx --config <voice>.onnx.json --output-raw --json-input --sentence_silence 0
  ```
  stdin: one JSON object per line `{"text": "...", "speaker_id": 17}`
  stdout: raw 16-bit signed LE mono PCM; sample rate from `.onnx.json` → `audio.sample_rate` (22050 for `medium`).
- Verify against the shipped binary's `--help` whether `length_scale` / `noise_scale` / `noise_w` are accepted per JSON line. If not: one process per (model, scale-set).
- Utterance framing: piper writes all samples for a line then waits. Detect end-of-utterance by a sentinel line synthesised after the real one (a fixed short word) and cut at the known sentinel sample count, or fall back to `--output_dir` mode (one WAV per utterance, read on file completion). Correctness first, then optimise.
- Timeout 5 s per utterance; on failure kill, restart, retry once.

### 4.3 Voice pool (`voices/`)

Initial pool, all `medium`:

| Model | Speakers | Accent coverage |
|---|---|---|
| `en_GB-vctk-medium` | 109 | UK regional, Irish, Scottish, Welsh, Australian, NZ, Indian, South African, Canadian |
| `en_US-libritts_r-medium` | 904 | US variety — curate ~30 by listening |
| `en_US-l2arctic-medium` | 24 | non-native: Arabic, Mandarin, Hindi, Korean, Spanish, Vietnamese |
| `en_US-arctic-medium` | 18 | clean US / Canadian / Scottish / Indian |
| `en_GB-aru-medium`, `en_GB-semaine-medium` | 12, 4 | more UK |
| `en_US-ryan`, `en_US-joe`, `en_US-lessac`, `en_US-hfc_male`, `en_US-hfc_female` | 1 each | US anchors for busy sectors |
| `en_GB-alan`, `en_GB-alba`, `en_GB-northern_english_male`, `en_GB-southern_english_female` | 1 each | UK anchors |
| `de_DE-thorsten`, `de_DE-mls`, `nl_NL-mls`, `pl_PL-mls`, `fr_FR-mls`, `cs_CZ-jirka`, `it_IT-riccardo`, `es_ES-mls` | 1–20 each | non-native controllers via the swap trick |

**Swap trick:** copy the model's `.onnx.json`, set `"espeak": {"voice": "en-us"}`, pass the copy as `--config`. Phonemes missing from `phoneme_id_map` are dropped (piper warns). The audit runner synthesises `testdata/audit/clearance.txt` per voice; a human listens once and marks pass/fail in `voices.json`. Never feed English through a non-English phonemizer.

`voices.json`:

```json
{
  "models": [
    {
      "name": "en_GB-vctk-medium",
      "onnx": "en/en_GB/vctk/medium/en_GB-vctk-medium.onnx",
      "config": "en/en_GB/vctk/medium/en_GB-vctk-medium.onnx.json",
      "sha256": "...",
      "sample_rate": 22050,
      "license": "CC-BY-4.0",
      "espeak_override": null,
      "speakers": [
        {"id": 42, "label": "p225", "accent": "en-GB-scottish", "quality": 4, "pass": true}
      ]
    }
  ],
  "regions": {
    "LK": ["en-CZ", "en-GB", "en-DE"],
    "K":  ["en-US"],
    "EG": ["en-GB", "en-IE"]
  }
}
```

- Downloader: `net/http` from HuggingFace, sha256 verify, resumable, progress callback. Base URL configurable (mirror / LAN cache).
- `Assign(icaoPrefix, controllerKind) VoiceProfile` picks by region weighting, avoids reusing a speaker within one airport, and is deterministic given a session seed.
- Licensing: `license` recorded per model from its MODEL_CARD. Build tag/flag `commercial=true` filters to CC-BY / public-domain voices.

### 4.4 STT — SAPI5 grammar mode (`stt/sapi/`, Windows only)

Stdlib only: `syscall.NewLazyDLL("ole32.dll")` for `CoInitializeEx`/`CoCreateInstance`, then direct COM vtable calls with `syscall.SyscallN`. **Do not use IDispatch/automation and do not implement a COM event sink.**

- Engine: `CLSID_SpInprocRecognizer` → `ISpRecognizer`. InProc, not Shared (no WSR UI, owns the mic).
- Prerequisite: Windows speech pack for `en-US`/`en-GB`. Detect via `ISpRecognizer::GetRecognizer` / token enumeration; return a typed error with the Settings path if missing.
- Audio input: enumerate `SpObjectTokenCategory` `AudioInput`, `ISpRecognizer::SetInput(token)`.
- Context: `ISpRecognizer::CreateRecoContext` → `ISpRecoContext`. Notification: `ISpRecoContext::SetNotifyWin32Event`, then a goroutine loops `WaitForNotifyEvent(timeout)` → `GetEvents` → for `SPEI_RECOGNITION` take `ISpRecoResult`: `GetText` for text, `GetPhrase` → `SPPHRASE` for rule name, confidence and semantic properties (`pProperties` linked list → `Tags`). `SPEI_FALSE_RECOGNITION` → `intent=say_again`.
- Grammar: `ISpRecoContext::CreateGrammar` → `ISpRecoGrammar`. Static rules from `grammar/atc.grxml` via `LoadCmdFromFile(..., SPLO_DYNAMIC)`. Root rule `transmission` = `callsign` + (`readback` | `request` | `report` | `checkin`), semantic `<tag>` properties for `intent`, `callsign`, `value`.
- Dynamic rule `callsign`: `GetRule("callsign", 0, SPRAF_TopLevel|SPRAF_Dynamic, TRUE)`, `ClearRule`, one `AddWordTransition` per spoken callsign (with the ICAO code as the property value), `Commit(0)`. Rebuild on every `SetCallsigns`. Restricting to aircraft on frequency is the main accuracy lever.
- Numbers sub-rule accepts ICAO and FAA forms (`three|tree`, `five|fife`, `nine|niner`, `point|decimal`).
- Coverage v1: check-in; readbacks (altitude, heading, speed, frequency, squawk, runway, taxi route, clearance); requests (climb/descent, direct, approach change, pushback, taxi, takeoff, landing); reports (ready, established, runway vacated, going around); `say again`, `standby`, `roger`, `wilco`.
- PTT: `Start()` → `SetRuleState("transmission", SPRS_ACTIVE)` and `ISpRecoContext::Resume`; `Stop()` → `SetRuleState(..., SPRS_INACTIVE)` and wait up to 800 ms for the final result, else emit `say_again`.
- Confidence threshold configurable (default 0.5); below → `say_again`.
- Known risk: this is the deprecated Windows Speech Recognition engine (deprecated Dec 2023, still shipped in Windows 11, no removal date). It stays isolated behind `voicegoio.STT`.

### 4.5 STT fake / harness (`stt/fake/`, all platforms)

Used on macOS for development and everywhere for regression:

- `fake.FromStdin()` — each line typed is treated as a recognised transmission; tags derived by matching against the same grammar rules re-implemented as a tiny Go parser over `atc.grxml` (or a simplified table), so `intent`/`callsign`/`value` extraction can be unit-tested without an engine.
- `fake.FromScript(path)` — `testdata/stt/*.jsonl` lines `{"text": "...", "tags": {...}}`; `Start()`/`Stop()` pops the next line.
- The Windows `sapi` backend is tested against the same script via `cmd/voicecheck` playing the WAVs into the recognizer (`ISpRecognizer::SetInput` with a `SpFileStream` token), so both backends share one expected-tags corpus.

### 4.6 Radio chain (`audio/radio/`)

Pure Go on `[]float32`, applied per transmission, parameterised by profile:

| Stage | Default | Notes |
|---|---|---|
| Band-pass | 300–3400 Hz, 2× 2nd-order Butterworth biquads | core "radio" sound |
| Presence EQ | +3 dB peak ~2 kHz, Q 1.0 | intelligibility |
| Soft clip | `tanh(x*drive)`, drive 1.5–3.0 | compression / overmodulation |
| Noise floor | white or pink, −45 dBFS, under speech only | |
| Squelch | 60–100 ms static burst + click at start and end | |
| Dropouts | 0–2 per transmission, 20–40 ms, `center` only | low probability |
| Gain | normalise to −6 dBFS peak | |
| Resample | to device rate (48 kHz), linear or windowed-sinc | last |

Profiles `tower`, `ground`, `approach`, `center`, `atis` in `radio.json`.

### 4.7 Playback (`audio/out_*.go`)

| Platform | Implementation | Device selection |
|---|---|---|
| Windows | `winmm.dll`: `waveOutGetNumDevs`/`waveOutGetDevCapsW` for enumeration, `waveOutOpen(deviceID)`, `waveOutPrepareHeader`, `waveOutWrite`, `waveOutUnprepareHeader`, `waveOutClose`; double-buffered | yes — this is why users can route ATC to a headset |
| macOS (dev) | write WAV to temp, `exec.Command("afplay", path)` | no — `SetDevice` returns nil and is documented as a no-op |
| other | write WAV to `VOICEGOIO_OUT_DIR` | n/a |

Queue semantics: one transmission at a time per frequency; other frequencies are silent (the app decides which frequency is monitored). Emits `PlaybackEvent`.

## 5. Build order

Steps 1–5 on macOS; 6–7 on Windows.

1. `normalise` + tests.
2. `tts/piper`: process pool, JSON input, PCM reader; one voice → WAV.
3. `audio/radio` + `audio/out_darwin.go`; A/B WAVs for `tower` vs `center`.
4. `stt/fake` + tag parser + `testdata/stt` corpus.
5. `voices`: manifest, downloader, audit runner, region weighting, `Assign`. Windows stubs for `stt/sapi` and `out_windows.go`. Tag `v0.1.0`.
6. Windows: `out_windows.go` (winmm), then `stt/sapi` bring-up: recognizer → static grammar → PTT → dynamic callsigns. `cmd/voicecheck` against a real mic and the WAV corpus. Tag `v0.2.0`.
7. App integration (outside this repo): consume `v0.2.0`, settings UI (mic, output device, PTT key, phraseology, voices dir).

## 6. `cmd/voicecheck`

```
voicecheck synth   --voices voices.json --text testdata/audit/clearance.txt --out ./wav   # every pass=true voice × every radio profile
voicecheck audit   --voices voices.json --model de_DE-thorsten-medium                    # swap-trick audit for one model
voicecheck recog   --backend sapi|fake --script testdata/stt/readbacks.jsonl             # prints tags + confidence, exit 1 on mismatch
voicecheck devices                                                                       # list input/output devices
```

## 7. CI (`.github/workflows/ci.yml`)

- Matrix: `macos-latest`, `windows-latest`, `ubuntu-latest`.
- Every job: `CGO_ENABLED=0 go vet ./... && go test ./...`, plus cross builds `GOOS=windows GOARCH=amd64`, `GOOS=darwin GOARCH=arm64`, `GOOS=linux GOARCH=amd64` with `CGO_ENABLED=0`.
- Guard: fail if `go.mod` contains any `require` line or `go list -deps ./...` shows a non-stdlib package.
- Piper binary and voices are not fetched in CI; sidecar tests use a fake piper (`testdata/fakepiper.go` built at test time) that echoes a known PCM pattern.

## 8. Development workflow (macOS first, Windows later)

1. `go mod init github.com/mrlm-net/voice-goio`; commit layout with all stubs compiling on darwin, windows, linux.
2. Work through build order 1–5 on macOS. Run `GOOS=windows CGO_ENABLED=0 go build ./...` locally before each push.
3. Tag `v0.1.0`. In the app: `go get github.com/mrlm-net/voice-goio@v0.1.0`. While both change at once use `replace github.com/mrlm-net/voice-goio => ../voice-goio` in the app's `go.mod` or a `go.work`; remove before release.
4. Private repo → `GOPRIVATE=github.com/mrlm-net` on both machines.
5. On Windows: build order 6, tag `v0.2.0`, bump in app.
6. Versioning: semver; API in `voicegoio.go` is the compatibility surface. Breaking changes → `v2` module path.

## 9. Acceptance

- Network disabled: full flow works on Windows (PTT → recognition → ATC reply → audio).
- `go.mod` has zero requires; `CGO_ENABLED=0 go build ./...` passes for darwin/arm64, windows/amd64, linux/amd64.
- TTS: text → first audio sample < 300 ms with a warm process; ≥ 100 `pass=true` voices; ≥ 10 distinct accents.
- STT: ≥ 95 % correct intent + callsign on the scripted corpus (20 phrases × 3 speakers, 5 callsigns on frequency); off-grammar input yields `say_again`.
- Radio profiles audibly distinct; speech intelligible through `center`.
- Windows output device selection works; macOS documented as default-device only.
- Normaliser tests cover every row in §4.1.

## 10. Open decisions (owner: Martin)

- Distribution: personal vs. shipped → decides `commercial=true` licence filtering.
- Later upgrade path: in-process `sherpa-onnx-c-api.dll` loaded via `syscall.NewLazyDLL` (still no cgo) for Kokoro voices and no process pool; macOS STT via a Swift `SFSpeechRecognizer` sidecar if ever needed.
