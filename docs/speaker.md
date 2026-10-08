---
title: The Speaker
description: The radio, heard — one frequency, the queue, the ATIS — plus the intercom, the cabin PA and chimes.
order: 11
section: guides
---

`speaker` is the whole output side with the rules the simconnect airport map uses, so every application sounds the same: piper with the default pool (no Czech voice reading English, one female voice in nine), a voice per controller position with shifts, a voice per crew, one frequency followed, one call at a time with 1 to 5 s between calls, nothing said more than 60 s late, and the ATIS as a looping broadcast joined mid-sentence.

Both current consumers (the MyCrew app and the simconnect airport map) use it this way. The snippets compile against v0.13.1.

## The radio

```go
sp := speaker.New(speaker.Options{PiperPath: piperExe, VoicesDir: voicesDir})
sp.Set(true, "118.105")                 // on, following one frequency
sp.SetATIS("122.155", "LKPR", atisText) // the broadcast on its frequency
sp.Hear(speaker.Utterance{Airport: "LKPR", Position: speaker.PosTower,
    Callsign: "CSA123", Frequency: "118.105", Text: "CSA123, runway 24, cleared to land"})
fmt.Println(sp.State().Status)          // "on (piper)", or why it is silent
```

## Speaking

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

## Channels

- **Radio** (`ChannelRadio`) follows one frequency through the radio chain.
- **Intercom** (`ChannelIntercom`): the crew and the cabin talk here. The same voices, without the radio chain (no band-pass, noise or squelch), and not tied to the frequency followed — it speaks with the radio off, on another frequency or none, on its own queue beside the radio.
- **Cabin PA** (`ChannelPA`): through the cabin speaker chain (`PAChain`), on its own queue.

Each channel can have its own output device (`SetDeviceFor`; `""` follows `SetDevice`). One voice says one line at a time across channels.

```go
man, _ := voices.LoadDefault()
copilot := voices.Installed(man, voices.Dir())[0].Profile(0) // model + speaker the player chose
sp.SayIntercom("Before start checklist complete", copilot)
sp.Hear(speaker.Utterance{Intercom: true, Position: "purser", Text: "Cabin secure", Voice: &purser})
```

`Utterance.Voice` (a `*voicegoio.VoiceProfile`) forces a voice and wins over the pool's pick on every channel.

## Chimes

Chimes are generated in code: `Chime(c)` queues one in order on its channel, `Utterance.Chime` does the same through `Hear`, and `ChimePCM(c)` at `ChimeRate` exports the sound.

## Audio for other clients

`sp.Clip(u)` renders an utterance in memory without a player (for a client that plays on its own device), and `speaker.WAV(pcm, rate)` makes it a WAV file, which is how the airport map serves audio to its browser clients.

## What sits beside the application

The package documentation (`go doc ./speaker`) has the mapping from simconnect's `traffic.Transmission` and what has to sit beside the application at runtime: piper's folder (`bin/piper/piper.exe` next to the executable by default, with its DLLs, `espeak-ng-data` and `libtashkeel_model.ort`) and the voice models (`%LOCALAPPDATA%\voice-goio\voices` by default). The speaker writes nothing on Windows, so both can live in a read-only install folder. See [Piper and voices](piper-and-voices.md).

## Small helpers

- `speaker.SpokenEnd(text)`: ends a call with a full stop, so piper does not cut its last syllable.
- `speaker.KindOf(position)` maps a position string to a `voicegoio.ControllerKind` (its voice and radio sound).
- `(*audio.Player).Recording()`: the path and length of the session recording, when one is being made.
