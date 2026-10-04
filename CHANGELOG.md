# Changelog

All notable changes to voice-goio, an offline ATC voice library. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[semantic versioning](https://semver.org/) with `voicegoio.go` as the
compatibility surface.

## [Unreleased]

## [0.12.0] — 2026-10-04

### Added

- speaker: an output device per channel. `SetDeviceFor(ChannelRadio | ChannelIntercom | ChannelPA, id)` routes one channel; "" goes back to the main device (`SetDevice`), which stays the default. `State().DeviceFor` lists the channels routed elsewhere.
- speaker: a cabin PA channel. `SayPA(text, voice)` speaks on its own queue and player, in order with itself and independent of the intercom, so a PA and an intercom call can overlap. `Chime(ChimePA)` now plays on the PA. `Utterance.PA` queues on the PA too, and `Clip` renders a PA through the cabin chain.
- speaker: `PAChain`, a light cabin-speaker sound: band-limited to about 300 Hz–4 kHz, with two early reflections; no noise or squelch.

## [0.11.2] — 2026-10-04

### Added

- speaker: cabin chimes on the intercom, generated in code with no files. `Chime(ChimeCall | ChimePA | ChimeSeatbelt)` queues one in order with `SayIntercom`. `ChimeCall` is the two-tone call, `ChimePA` a single soft ding before a PA, `ChimeSeatbelt` the seat-belt bong. After a call chime the next intercom item waits a pickup (`PickupMin` + up to `PickupJitter`, 1.5–3 s). `Utterance.Chime` queues a chime the same way, `Clip` renders one, and `ChimePCM` / `ChimeRate` export the sound. A chime needs no voice model.

## [0.11.1] — 2026-10-04

### Fixed

- tts/piper: a text of several sentences is said whole. Piper writes each sentence's audio once it is computed, and the pause while it computes the next one is longer than the idle gap, so only the first sentence was returned (a passenger announcement cut after its first sentence). The text is now said a sentence at a time, joined with piper's 0.2 s sentence silence (#22).

## [0.11.0] — 2026-10-04

### Added

- voices: `InstallPack(ctx, pack, dir, progress)` downloads a pack's models, each checked against its SHA-256, resuming and skipping what is installed and valid; `Manifest.PackSize(pack)` its download size (core 714 MB, en 1031 MB, all 1542 MB); the manifest records every model's SHA-256 and `size`.
- tts/piper: `Install(ctx, dir, progress)` downloads piper 2023.11.14-2 for Windows (rhasspy/piper, MIT), checks its pinned SHA-256 and unzips it to dir/piper — the layout the speaker defaults to.

## [0.10.0] — 2026-10-04

### Added

- voices: `PackCore` ("core"), an installer's default: the English voices the pool mostly uses (en_GB-alan, en_GB-vctk, en_US-lessac, en_US-ryan) and one controller accent model per country (cs_CZ-jirka, de_DE-mls, fr_FR-mls, nl_NL-mls, pl_PL-mls, it_IT-riccardo, es_ES-mls): 11 models, 702 MB as a zip. "en" (15 models, 1.02 GB) and "all" (23, 1.52 GB) stay as optional downloads.

## [0.9.0] — 2026-10-04

### Changed

- speaker: the controller accents are opt-in, `Options.Accents` (off by default): English voices only unless asked, until the accent models are trained for English. `voices.AccentModels` lists them.

## [0.8.0] — 2026-10-04

### Added

- voices: voice packs (#17). `Manifest.Pack(name)`: `PackEnglish` ("en", every English model) and `PackAll` ("all", with the accent models); `Manifest.WritePack` zips a pack of the installed models with a manifest of just them, for an installer (unpacked into the voices folder). voicecheck: `download -pack en|all` and `pack -pack en|all -out file.zip`. Today: English 15 models, 1.02 GB; all 23, 1.52 GB.

## [0.7.1] — 2026-10-04

### Changed

- voices: every accent model (German thorsten and MLS, Dutch, Polish, French, Italian, Spanish) reads English through RP phonemes like the Czech one: the words as every English voice says them, the accent in the voice. At a German, Dutch or French airport the 20-speaker MLS models give every position an accent.

## [0.7.0] — 2026-10-04

### Changed

- voices: the Czech controller voice (cs_CZ-jirka-medium) reads English through RP phonemes (espeak en-gb-x-rp), the words pronounced as every English voice says them, a light Czech accent; assigned first at Czech airports (LK) again. More voices per country are added to the catalogue and assigned the same way.
- speaker: `Options.Exclude` leaves voices out; nothing is excluded by default (ExcludedVoice was).

## [0.6.0] — 2026-10-04

### Added

- speaker: the intercom (#11). `Utterance.Intercom` and `Speaker.SayIntercom(text, voice)`: the crew and the cabin speak without the radio chain (no band-pass, noise or squelch) and whatever the frequency followed, even with the radio off, on their own player queue (`IntercomKey`) one at a time with `IntercomGap` between; a change of frequency does not drop them. `Clip` of an intercom utterance is dry.
- speaker: `Utterance.Voice`, an explicit voice (model + speaker) that wins over the pool's pick, on the intercom and on the radio; its `Radio` left empty is the position's.
- voices: `Dir()`, the per-user models folder (`%LOCALAPPDATA%\voice-goio\voices` on Windows), `Installed(manifest, dir)`, the manifest's models present there, and `Model.Profile(speakerID)`, one of their voices as a `VoiceProfile`.
- grammar: `Commands`, an application's own SRGS grammar from phrases and intents ("gear up" → `gear_up`), and `Match`, the same matching for typed input; stt/sapi: `Options.Grammar` takes the grammar as text (`GrammarPath` still takes a file).

## [0.5.0] — 2026-10-04

### Added

- speaker: the radio, heard, as the simconnect airport map speaks it, for every application to share: `speaker.New`, `Set` (on/off and the one frequency followed), `Hear` an `Utterance`, `SetATIS` (or `Options.ATIS`) for the looping broadcast, `SayOnce`, `SetDevice`/`Devices`, `State`, `Clip`/`WAV` for a client playing on its own device. Voices per controller position with shifts and per crew, one call at a time with 1 to 5 s between, nothing said more than 60 s late, FAA or ICAO reading. The package doc lists the files that must sit beside the application (piper and the voice models) and what that means for packaging.
- voices: `Speaker.Gender` ("F", "M" or "") and `PoolOptions.FemaleShare`. A position gets a voice of its gender (the share of positions that get a female one, e.g. 1/9 for 1:8), the best of that gender in its accent tier, and any voice when none is free. The genders come from the sources: VCTK's 109 speakers from the corpus's own speaker-info.txt (version 0.92, Edinburgh DataShare), which also gives each speaker's accent in place of the round-robin placeholders (63 female, 46 male); lessac (Blizzard 2013 corpus page), alba (DataShare abstract) and ryan (RyanSpeech paper); hfc_male, hfc_female, northern_english_male and southern_english_female by their names. alan and joe stay without one: their sources do not say.

## [0.4.0] — 2026-10-02

### Changed

- License: new versions are under the Business Source License 1.1 instead of Apache-2.0. Non-commercial use (personal and hobby use, the flight-simulation community, education, research, non-profits) is allowed; commercial use, such as a paid add-on or product, a paid service or use inside a business, needs a separate licence. Each version becomes Apache-2.0 four years after it is published. Versions up to and including v0.3.1 stay under Apache-2.0.

### Fixed

- normalise: a lone A or I in a taxi route after "via" is the taxiway and is spelled: "via H, A" says "via hotel, alpha", not "via hotel, a".

## [0.3.1] — 2026-09-28

No Go API change: the types and functions are identical to 0.3.0. What changed
is one more recognised phrase, and the demo.

### Added

- `readback_continue`: "continue approach" is an instruction in its own right
  and its readback carries no number, so nothing matched it and a perfectly
  ordinary transmission came back as `say_again`. Applications switching on
  intent already need a default branch, so a new value is additive.

### Fixed

- The emergency demo had its geometry backwards, again. The emergency is the
  aircraft *behind*: number two at fifteen miles, five miles behind the one
  that already has a landing clearance at ten. The aircraft in front is sent
  around not because it is in the way at that moment, but because one that is
  slow to vacate would leave the runway occupied when the emergency arrives.
  The go-around now includes "fly runway heading", and the emergency aircraft
  checks in as ordinary traffic before anything goes wrong.
- Demo output folds at a fixed width with a hanging indent instead of wrapping
  at the window edge, which destroyed the alignment that makes a transcript
  readable at a glance.

## [0.3.0] — 2026-09-28

Everything that can be done before a Windows machine is involved. The two
Windows backends still have not run against hardware.

### Added

- `readback_clearance`: departure clearance readbacks, which SPEC.md 4.4 lists
  and which were missing from both the parser and the grammar. The squawk is
  the value, because it is the element a wrong readback most often turns on;
  the SID and the cleared level come back as secondary tags. It is tested
  before the altitude and squawk rules, since a clearance contains both.
- `Recognizer.SetInputFile`, `SetInputMicrophone` and `RecognizeFile` on the
  Windows backend, built on SAPI's `SpFileStream`. SPEC.md 4.5 wants one corpus
  validated against both backends, and without this the SAPI half needs a
  person saying every phrase into a headset.
  `voicecheck recog -backend sapi -wav <dir>` renders each phrase and feeds the
  file to the engine, so the corpus runs unattended.

### Fixed

- **Voice models downloaded into the working directory.** An empty
  `PoolOptions.Dir` resolved to a relative path, so `voicecheck download` wrote
  sixty megabytes of model into whatever directory it was run from — in
  testing, the source tree. The voices package and the piper backend now share
  one definition of the per-user data directory (`internal/userdir`) rather
  than each having their own idea of it.
- `piper.CheckFlags` could hang forever. It ran `--help` with no deadline, and
  a piper that cannot start does not fail, it hangs — which is exactly what the
  macOS build does on Apple Silicon. It now gives up after 20 seconds and says
  the binary is probably the wrong architecture.
- The emergency demo had its geometry backwards: an aircraft at eight miles
  final was described as being ahead of one already cleared to land, and then
  sent around for it. The cleared aircraft is now explicitly at ten miles, so
  the emergency really is ahead of it. The aftermath continues past the
  landing, through the fire service and the taxi to stand.

### Known blocker

piper still cannot be verified on an Apple Silicon Mac: the
`piper_macos_aarch64.tar.gz` asset of release 2023.11.14-2 contains an x86_64
binary, and under Rosetta it hangs producing no output at all. The Windows
binary is native on the target platform, so that verification moves to the
bring-up session.

## [0.2.0] — 2026-09-28

New public API — `voicegoio.TagReason` and the recording options on
`audio.Options` — so this is a minor bump rather than a patch. Nothing was
removed or changed shape, so upgrading from 0.1.2 needs no code changes.

### Added

- `voicegoio.TagReason` and the `ReasonNoCallsign` / `ReasonOffGrammar` /
  `ReasonLowConfidence` constants. When a transmission is not understood, the
  recogniser now says *how* it failed, and keeps the callsign when it has one.
  A `say_again` whose callsign was identified is now reported with 0.5
  confidence rather than 0, because it was partially recognised. Both backends
  set it.
- `audio.Options.RecordPath` records the whole frequency to one continuous WAV
  as it plays, and `audio.Options.Silent` accepts audio and plays nothing, so a
  render produces a file without shouting through the speakers for several
  minutes. `make demo-wav` renders every demo this way.
- Recognised `ident` / `squawk ident`, `request_deviation`
  ("request turn 20 degrees right due to weather"), and `report_souls`
  ("souls on board one four seven").
- `-mode emergency` is now two aircraft on one frequency and one runway: a
  mayday from one, a go-around for the other whose landing clearance is taken
  back, then souls on board and the landing. Three voices share the frequency.

### Changed

- A go-around is reported ahead of any readback inside it. "Going around,
  climb tree thousand" is a go-around, not an altitude readback: the aircraft
  is no longer doing what the controller last told it to.
- A transmission carrying a pressure alongside its primary instruction reports
  it as a secondary `qnh` tag instead of letting it compete to become the
  intent.

### Fixed

- **The macOS development backend spoke non-English languages.** Accent labels
  such as `en-CZ` and `en-DE` were mapped to Czech and German *language*
  voices, which phonemise English as Czech and German — mispronunciation, not
  an accent, and exactly what SPEC.md 4.3 forbids. Every voice that backend can
  select is now an English one, and a test enforces it. Native accents
  (en-GB, en-US, en-IE, en-AU, en-IN, en-ZA) are genuine and unaffected; the
  non-native ones are only real with piper.
- "I say again" was spoken "india say again": a lone capital I was treated as
  an identifier rather than as the pronoun.
- "descent to FL100" was not recognised as a descent readback, so a
  transmission carrying a QNH as well was filed as a QNH readback.
- A check-in that gave its level before the phrase identifying it
  ("BAW123 15000 with you") lost the level.

## [0.1.2] — 2026-09-28

No API change. Found by driving the live harness by hand.

### Added

- `ident` and `squawk ident` are recognised as an instruction in their own
  right, in the Go parser, in `grammar/atc.grxml` so the Windows backend
  agrees, and in the demo's canned controller.
- `stt/fake` accepts the identifier and plain digits as a typing convenience:
  `CSA1234 with you, 15000` and `DLH4EK request climb FL370` tag exactly as
  their spoken equivalents do. Speech never produces those forms, so no
  grammar rule was widened and the Windows backend is unaffected.

### Fixed

- A single-token callsign could never be matched. `matchCallsign` only tried
  windows of two or more tokens, because every spoken callsign is several
  words, so `CSA1234` failed regardless of what the roster held. The window
  now goes down to one token, and the roster registers the identifier itself
  alongside every spoken variant.
- The digit reader only understood number words, so a check-in written with
  digits lost its level.

The recognition corpus is now 41 phrases, all tagged exactly.

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
