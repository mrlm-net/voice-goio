---
title: Integrating
description: Adding voice-goio to an application, developing both side by side, and the versions so far.
order: 30
section: integration
---

## Adding it

```bash
go get github.com/mrlm-net/voice-goio@v0.13.1
```

Requires Go 1.27 or newer. The `go` directive in `go.mod` is a floor on the consumer's toolchain, not a target, so it is set to the version the library is actually developed and tested on. A machine on an older toolchain will fetch 1.27 automatically on the first build.

## Developing alongside an application

While the library and the application are changing together, point the app at the working copy rather than at a tag — a `go.work` beside both checkouts is the least intrusive way, because it leaves the app's `go.mod` alone:

```
go 1.27

use (
    ./mycrew-online/app // or ./your-app
    ./voice-goio
)
```

Remove it, or drop the `replace`, before a release build.

## What to wire first

1. **Playback.** `tts.Open` → `pool.Assign` → `norm.Spoken` → `chain.Apply` → `player.Play`, and drain `player.Events()` (see [The pipeline](pipeline.md)), or let the [speaker](speaker.md) do it. This works on macOS with no downloads, so the integration can be proven before any Windows work.
2. **Settings.** `player.Devices()` for the output device; on Windows, `sapi.InputDevices()` for the microphone and `sapi.Engines()` to show whether a speech pack is installed at all.
3. **Voices.** [Piper and a voice pack](piper-and-voices.md) from the app's first-run screen.
4. **Recognition.** `stt/fake` on the development machine, `stt/sapi` on Windows. Same interface, so the wiring is written once. See [Recognition](recognition.md).

## Versions

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

The full notes are in the [changelog](/docs/changelog).

## License

Business Source License 1.1 for versions after v0.3.1. Non-commercial use is free: personal and hobby use, the flight-simulation community, education, research and non-profits. Commercial use, such as a paid add-on or product, a paid service or use inside a business, needs a separate licence: write to [support@mrlm.net](mailto:support@mrlm.net). Each version becomes Apache-2.0 four years after it is published, and versions up to and including v0.3.1 remain under Apache-2.0.
