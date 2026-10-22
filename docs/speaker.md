---
title: The Speaker
description: The radio, heard — one frequency, the queue, the ATIS — plus the intercom, the cabin PA and chimes.
order: 11
section: guides
---

`speaker` is the whole output side with the rules the simconnect airport map uses, so every application sounds the same: piper with the default pool (no Czech voice reading English, one female voice in nine), a voice per controller position with shifts, a voice per crew, one frequency followed, one call at a time with 1 to 5 s between calls, nothing said more than 60 s late, and the ATIS as a looping broadcast joined mid-sentence.

Both current consumers (the MyCrew app and the simconnect airport map) use it this way. The snippets compile against v0.16.

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

### One channel at a time

By default each channel plays on its own, as real radios and cabins do (a PA over the radio). A player hears that as too many voices at once, so `Options.OneAtATime` has the radio, the intercom and the PA take turns: one utterance plays and the others wait for it to end, never cutting it. Among those waiting the radio goes first, then the intercom, then the PA; the ATIS broadcast waits for them all. A chime is followed by its own PA or answer.

```go
sp := speaker.New(speaker.Options{OneAtATime: true})
```

## Volume per channel

`SetGain(ch, gain)` sets a channel's volume from 0 (muted) to 1 (full), clamped: the cockpit's audio panel, where a receive key switched off is 0. It applies to what is already playing on the channel within about 0.1 s, chimes included, and to the channel's player when it opens later. `Gain(ch)` reads it back (1 when never set).

```go
sp.SetGain(speaker.ChannelRadio, 0.8)    // VHF volume knob
sp.SetGain(speaker.ChannelIntercom, 1)
sp.SetGain(speaker.ChannelPA, 0)         // PA receive key off
fmt.Println(sp.Gain(speaker.ChannelPA)) // 0
```

The same control exists one level down, on `audio.Player`: `SetGain(g)` and `Gain()`, applied as the audio goes to the device (the next part, about 0.1 s on Windows). A session recording keeps the audio as given, without the gain.

## Speaking rate

`Utterance.Tempo` is how fast a call is said against the voice's own pace: 1 (or 0) as the voice speaks, above 1 faster (a busy frequency, an urgent call), below 1 slower. It is kept within `speaker.MinTempo` (0.8) and `speaker.MaxTempo` (1.35), since faster than that is no longer clear on the radio, and it scales the voice's `LengthScale`.

## Saying something once

`SayOnce(u)` says an utterance now, whatever frequency is followed and even while the speaker is off: an airport panel's ATIS button, for example. It returns false when the voice is unavailable (`State().Status` says why). An `Intercom` utterance is queued on the intercom, as `SayIntercom` does.

## Chimes

Chimes are generated in code: `Chime(c)` queues one in order on its channel, `Utterance.Chime` does the same through `Hear`, and `ChimePCM(c)` at `ChimeRate` exports the sound.

## Audio for other clients

`sp.Clip(u)` renders an utterance in memory without a player (for a client that plays on its own device), and `speaker.WAV(pcm, rate)` makes it a WAV file, which is how the airport map serves audio to its browser clients.

## What sits beside the application

The package documentation (`go doc ./speaker`) has the mapping from simconnect's `traffic.Transmission` and what has to sit beside the application at runtime: piper's folder (`bin/piper/piper.exe` next to the executable by default, with its DLLs, `espeak-ng-data` and `libtashkeel_model.ort`) and the voice models (`%LOCALAPPDATA%\voice-goio\voices` by default). The speaker writes nothing on Windows, so both can live in a read-only install folder. See [Piper and voices](piper-and-voices.md).

## Small helpers

- `speaker.SpokenEnd(text)`: ends a call with a full stop, so piper does not cut its last syllable.
- `speaker.Pad(pcm, rate)`: adds `speaker.TailPad` (150 ms) of silence after a synthesised call, before the radio effect, as the speaker does itself.
- `speaker.KindOf(position)` maps a position string to a `voicegoio.ControllerKind` (its voice and radio sound).
- `(*audio.Player).Recording()`: the path and length of the session recording, when one is being made.
