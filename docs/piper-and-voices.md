---
title: Piper and Voices
description: Installing the piper synthesiser, voice packs and models, accents and the commercial build tag.
order: 12
section: guides
---

Voice output needs two things on the user's machine that are fetched, never bundled: the piper binary and the voice models.

## Installing piper

`piper.Install` downloads the pinned Windows release (2023.11.14-2), checks its SHA-256 and unzips it to `dir/piper`. `piper.DefaultPiperPath()` is `bin/piper/piper.exe` next to the running executable, which is what `speaker.Options.PiperPath` defaults to, so `dir = <exe dir>/bin` lines the two up. Install does nothing when piper is already there.

```go
if _, err := os.Stat(piper.DefaultPiperPath()); err != nil {
    exe, _ := os.Executable()
    err = piper.Install(ctx, filepath.Join(filepath.Dir(exe), "bin"),
        func(done, total int64) { /* progress bar */ })
}
```

An application that keeps piper elsewhere (MyCrew uses its own per-user folder) installs there and passes that path as `PiperPath`. `(*piper.TTS).Available()` reports whether the binary is present.

`tts/piper` drives piper as a pool of warm sidecars: JSON lines in, raw PCM out.

## Voice packs

Models go into the per-user folder, `voices.Dir()` (`%LOCALAPPDATA%\voice-goio\voices` on Windows, never the working directory). Packs are the sets an installer offers: `voices.Packs()` is `core`, `en`, `all`; `voices.PackCore` is the default (the English voices the pool uses most plus one accent model per country). `InstallPack` checks each model's SHA-256, resumes broken downloads and skips what is installed, so it is safe to run again.

```go
man, _ := voices.LoadDefault()
for _, p := range voices.Packs() {
    size, _ := man.PackSize(p) // for a "download 714 MB?" prompt
}
err := voices.InstallPack(ctx, voices.PackCore, "", // "" is voices.Dir()
    func(done, total int64, model string) { /* progress */ })

installed := voices.Installed(man, voices.Dir()) // what is on disk: a voice picker
```

`man.Pack(name)` lists a pack's models (compare with `Installed` for what is missing). For a single model, `voices.Downloader{Dir, OnProgress}` and `Fetch(ctx, model)` download one, reporting `voices.Progress` (model, file, bytes downloaded, total). `Model.Profile(speakerID)` is one of a model's voices as a `VoiceProfile`.

`voices.Load(path)` and `(*voices.Manifest).Save(path)` read and write a manifest other than the embedded one.

## The voices

Voice models are never bundled: hundreds of megabytes, their own licences, and a user only needs their region. The manifest (`voices/voices.json`) lists 23 models covering 23 accents and 1145 assignable voices.

A model declares every accent its speakers cover, not just one. That matters for `en_GB-vctk-medium`, whose 109 speakers span the British Isles, Australia, New Zealand, India, South Africa and Canada: calling the whole model "en-GB" would leave a Sydney or Delhi controller with no regional voice at all.

```bash
go run ./cmd/voicecheck voices                             # pool report vs. the bar
go run ./cmd/voicecheck download -model en_GB-vctk-medium  # fetch one
go run ./cmd/voicecheck download -model all -write voices/voices.json
```

## Accents

Only piper renders non-native accents. The macOS development backend is English voices only: macOS ships Czech and German *language* voices, and handing them English text makes them mispronounce it rather than accent it. Native accents there (en-GB, en-US, en-IE, en-AU, en-IN, en-ZA) are genuine English voices.

Non-English models (German, Czech, Dutch, Polish, French, Italian, Spanish) produce non-native controllers via the **swap trick**: a copy of the model config with `espeak.voice` forced to `en-us`, so English text is phonemised in English and spoken by a model trained on another language. `voices` writes those config copies itself. It is never done the other way round.

## Licensing filter

`-tags commercial` restricts assignment to permissively licensed models. It is a build tag rather than a setting so a shipped binary cannot be configured into a licence breach.

## Do not verify piper on Apple Silicon

The `piper_macos_aarch64.tar.gz` asset of release 2023.11.14-2 contains an **x86_64** binary despite its name. Under Rosetta it starts and then hangs indefinitely, producing no output at all. `piper_windows_amd64.zip` is native on the target platform.
