# Changelog

All notable changes to voice-goio. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[semantic versioning](https://semver.org/) with `voicegoio.go` as the
compatibility surface.

## [0.1.1] — 2026-09-28

Toolchain and CI only; no change to the library itself. **Use this rather than
v0.1.0**, whose workflow was misconfigured.

### Changed

- Requires Go 1.27 or newer, up from 1.24. The `go` directive is a floor on
  the consumer's toolchain, so this is a narrowing, taken because the library
  is developed and tested on 1.27 and CI was previously exercising a toolchain
  nobody runs.

### Fixed

- The race step ran with `CGO_ENABLED=0`, which the race detector cannot do,
  so it never ran at all. It now opts back into cgo for that one step; the
  shipped build is still `CGO_ENABLED=0` everywhere.
- On Windows the checkout converted the tree to CRLF and `gofmt -l` then
  reported every file as unformatted. A `.gitattributes` pins the working tree
  to LF on all platforms.
- `make release-check` builds, vets, tests and cross-compiles a clean clone of
  `HEAD`. This is the check that a file is in the repository rather than only
  on the developer's disk — an over-broad `.gitignore` pattern had excluded
  `internal/wav` from the first release commit, which built locally and failed
  every CI job on a fresh checkout.

## [0.1.0] — 2026-09-28

First tagged release. The platform-independent half of SPEC.md is complete and
tested; the Windows backends are written and cross-compile on every commit but
have not yet run against real hardware. Enough to integrate against, not yet
enough to fly with.

### Added

- **Public API** (`voicegoio.go`) — `TTS`, `STT`, `Player` and `Normaliser`
  interfaces plus the value types. Nothing in the root package imports a
  subpackage, so the application can compile against the API before a given
  backend exists.
- **Normaliser** (`normalise/`) — application tokens to spoken form under ICAO
  or FAA phraseology: callsigns via an embedded telephony table, flight levels,
  headings, runways, taxi routes, squawks, QNH (with hPa to inHg conversion),
  frequencies and altitudes. Also emergency signals, METAR/ATIS shorthand, and
  the punctuation that becomes prosody: pauses after a callsign, between taxi
  route elements, after a distress signal, and between an information unit and
  the instruction that follows it.
- **Radio chain** (`audio/radio/`, `internal/dsp/`) — band pass, presence EQ,
  compressor, soft clip, loudness normalisation under a soft ceiling, noise
  floor, squelch bursts, dropouts and sample rate conversion. Five profiles
  (`tower`, `ground`, `approach`, `center`, `atis`) in an editable
  `radio.json`.
- **Playback** (`audio/`) — per-frequency queues with `Started`/`Finished`
  events. `winmm` through `syscall` on Windows with device selection, `afplay`
  on macOS, WAV files elsewhere and on demand anywhere.
- **TTS** — `tts/piper` (a pool of warm sidecars, JSON lines in, raw PCM out,
  LRU eviction, sentinel-based utterance framing), `tts/say` (macOS
  development backend, real accents with nothing to download) and `tts/fake`
  (deterministic, for CI). `tts.Open` picks the best available.
- **Recognition** — `stt/fake` with a tag parser that mirrors
  `grammar/atc.grxml`, driven from stdin or a JSON-lines script;
  `stt/sapi` for Windows, built on raw COM vtable calls with no cgo, no
  automation layer and no event sink.
- **Voice pool** (`voices/`) — 23 models covering 23 accents and 1145
  assignable voices, region-weighted deterministic assignment that keeps a
  controller's voice stable for a session and unique within an airport, a
  resumable sha256-verified downloader, the espeak swap trick for non-native
  controllers, and a fixed machine voice for the ATIS.
- **Tools** — `cmd/voicecheck` (synth, audit, recog, devices, voices,
  download) and `cmd/demo` with six modes, of which `-mode arrival` runs a
  complete arrival across four positions and four frequencies.
- **CI** — macOS, Windows and Linux; `gofmt`, `go vet`, tests and race;
  cross builds for all three platforms plus the `commercial` build tag; a
  guard proving `go.mod` is empty and nothing outside the standard library is
  reachable; and both quality bars enforced.

### Known limitations

- `stt/sapi` has never run against a live speech engine. Struct layouts are
  pinned by a test that runs on `windows-latest` in CI and every failure path
  returns a typed error, but bring-up is still ahead.
- `tts/piper` has only run against `testdata/fakepiper`, which speaks the same
  protocol. Utterance framing and whether the shipped binary accepts
  `length_scale` per JSON line are the two things to verify first
  (`piper.CheckFlags` reports the latter).
- Speaker-level audit flags in `voices.json` are unset rather than fabricated,
  so multi-speaker models are only assignable with
  `PoolOptions.AllowUnaudited` until someone has listened to them.

### Deviations from SPEC.md

- The radio chain levels the speech before adding noise and squelch, and
  targets RMS under a soft ceiling rather than a peak. Peak normalisation
  leaves speech around −20 dBFS RMS and sounding distant.
- ICAO frequency digits follow the phraseology digit table (`four fife`)
  rather than §4.1's frequency row (`four five`), which contradicts it.

Both are documented with their reasoning in the README.
