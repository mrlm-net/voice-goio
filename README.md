# voice-goio

[![Go Reference](https://pkg.go.dev/badge/github.com/mrlm-net/voice-goio.svg)](https://pkg.go.dev/github.com/mrlm-net/voice-goio)
[![Docs](https://img.shields.io/badge/docs-goio.mrlm.net-8a6418)](https://goio.mrlm.net/)
[![License: BSL 1.1 · non-commercial](https://img.shields.io/badge/license-BSL%201.1%20%C2%B7%20non--commercial-6c7480)](LICENSE)

Offline ATC voice input and output, as a Go library.

Controller speech out, pilot speech in, for any application that needs a radio:
a flight simulator add-on, a training tool, a controller trainer. The library
is aviation-specific — the normaliser, the grammar, the radio chain and the
voice pool are all built around ATC phraseology — but it knows nothing about
any particular simulator. An application hands it text and gets audio; it
hands the application tags.

- **No services.** Everything runs on the user's PC. Works with the network adapter disabled.
- **No dependencies.** `go.mod` has zero `require` lines. Standard library only, `CGO_ENABLED=0` everywhere.
- **Windows is the runtime target**, macOS the development machine. Windows-only code is cross-compiled on every commit so it cannot rot.

```
app text ─▶ normalise ─▶ TTS ─▶ radio chain ─▶ player ─▶ PlaybackEvent
pilot speech ─▶ STT ─▶ Recognition{intent, callsign, value} ─▶ app
```

> [!NOTE]
> Pre-1.0: the API changes between minor versions. The output side
> (`tts/piper`, `speaker`, `audio` on `winmm`) runs on Windows hardware in two
> applications; recognition (`stt/sapi`) has never run against a live engine.
> See [Platform status](https://goio.mrlm.net/docs/platform-status).

## Install

Requires Go 1.27 or newer.

```bash
go get github.com/mrlm-net/voice-goio@latest
```

## Quick start

Most applications only need the `speaker`, plus piper and a voice pack on the
user's machine:

```go
sp := speaker.New(speaker.Options{})    // piper next to the executable, voices in the per-user folder
defer sp.Close()

sp.Set(true, "118.105")                 // on, following one frequency
sp.Hear(speaker.Utterance{Airport: "LKPR", Position: speaker.PosTower,
    Callsign: "CSA123", Frequency: "118.105", Text: "CSA123, runway 24, cleared to land"})
fmt.Println(sp.State().Status)          // "on (piper)", or why it is silent
```

To hear the whole pipeline without writing any code:

```bash
make demo-arrival   # radar → approach → tower → ground, every pilot call recognised
make all            # fmt, vet, test, and the three cross builds
```

On macOS with no piper installed the demos fall back to the built-in `say`
backend, so the pipeline is audible before anything has been downloaded.

## Documentation

Full documentation is at **[goio.mrlm.net](https://goio.mrlm.net/)**:

- [Getting started](https://goio.mrlm.net/docs/getting-started) — install, the demos, build and test
- [The speaker](https://goio.mrlm.net/docs/speaker) — radio, intercom, cabin PA, chimes, volume per channel
- [Piper and voices](https://goio.mrlm.net/docs/piper-and-voices) — installing the synthesiser, voice packs and accents
- [Voice assignment](https://goio.mrlm.net/docs/voice-assignment) — a voice per position, controller and crew
- [Recognition](https://goio.mrlm.net/docs/recognition) — SAPI, the fake backend and grammars of your own
- [The pipeline](https://goio.mrlm.net/docs/pipeline) — the lower-level pieces, wired by hand
- [Packages](https://goio.mrlm.net/docs/packages) and the [voicecheck CLI](https://goio.mrlm.net/docs/voicecheck)
- [Integrating](https://goio.mrlm.net/docs/integrating) — developing alongside an application, the versions so far
- [Changelog](CHANGELOG.md)

API reference: [pkg.go.dev](https://pkg.go.dev/github.com/mrlm-net/voice-goio).
The source of the docs is [`docs/`](docs/).

## License

Business Source License 1.1, see [LICENSE](LICENSE), for versions after v0.3.1. Non-commercial use is free: personal and hobby use, the flight-simulation community, education, research and non-profits. Commercial use, such as a paid add-on or product, a paid service or use inside a business, needs a separate licence; [open an issue](https://github.com/mrlm-net/voice-goio/issues) to ask. Each version becomes Apache-2.0 four years after it is published, and versions up to and including v0.3.1 remain under Apache-2.0.
