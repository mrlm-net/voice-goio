// Package speaker is the radio, heard: what is said on the frequency an
// application follows, spoken one transmission at a time with the rules the
// simconnect airport map has had since #419, so every application built on
// voice-goio sounds the same.
//
// The rules:
//
//   - Voices: piper (tts.Open) with the default voice pool (voices.json),
//     English voices; with Options.Accents (off by default) the airport's
//     accent first (at a Czech airport the Czech voice on RP phonemes: a
//     light accent, the words as every English voice says them),
//     Options.Exclude never, FemaleShare of
//     the positions with a female voice, the radio.Default chain and
//     normalise. A voice per controller position at an airport, handed over
//     every ShiftMin to ShiftMax (a new controller, a new voice: "LKPR",
//     "LKPR-2"); a voice per pilot call sign, kept for good.
//   - One frequency, as on a receiver: nothing is said without one, only
//     what is on it, and a change of frequency resets the player and drops
//     what was queued.
//   - One queue, one utterance at a time; anything not said within MaxLag
//     of being heard is dropped. A full queue (64) drops what comes.
//   - Gap plus up to GapJitter at random between two transmissions (a
//     clearance and its readback alike), synthesis included; halved while
//     more than two wait.
//   - The ATIS is a broadcast: synthesised once per text (its information
//     letter is in it) and looped from a fixed start with ATISGap between
//     runs, so tuning in joins it mid-sentence.
//   - The reading: Utterance.Phraseology FAA reads numbers and frequencies
//     the FAA way (a US airport), ICAO otherwise; each call ends with a full
//     stop (SpokenEnd) and TailPad of silence so neither piper nor the radio
//     fade takes the last syllable.
//
// # Use
//
//	sp := speaker.New(speaker.Options{PiperPath: piperExe, VoicesDir: voicesDir, Hint: "see the README"})
//	sp.Set(true, "118.105")                 // on, following Praha Tower
//	sp.SetATIS("122.155", "LKPR", atisText) // or Options.ATIS to be asked
//	sp.Hear(u)                              // every transmission; dropped unless on the frequency
//	st := sp.State()                        // On, Frequency, Status ("off", "on (piper)", ErrNoVoice …), Devices
//
// # From simconnect's traffic.Transmission
//
// The mapping is the application's (this module does not import
// simconnect):
//
//	func utterance(t traffic.Transmission) speaker.Utterance {
//		ph := voicegoio.ICAO
//		if t.Phraseology == traffic.PhraseologyFAA {
//			ph = voicegoio.FAA
//		}
//		return speaker.Utterance{
//			Airport:     t.Airport,
//			Position:    string(t.Position), // "tower", "ground", … as the Pos* constants
//			Callsign:    t.Callsign,
//			Pilot:       t.Pilot,
//			Frequency:   t.Frequency,
//			Text:        t.Text,
//			Phraseology: ph,
//		}
//	}
//
// An ATIS transmission (traffic.IntentATIS, Position "atis") is not taken by
// Hear: the application gives the current ATIS with SetATIS (or
// Options.ATIS) and the speaker broadcasts it while its frequency is
// followed. SayOnce says one now, even while off.
//
// # The intercom and chosen voices
//
// The crew and the cabin speak on the intercom, not the radio:
//
//	man, _ := voices.LoadDefault()
//	installed := voices.Installed(man, voices.Dir()) // a voice picker's list
//	copilot := installed[0].Profile(0)               // model + speaker the player chose
//	sp.SayIntercom("Before start checklist complete", copilot)
//	sp.Hear(speaker.Utterance{Intercom: true, Position: "purser", Text: "Cabin secure", Voice: &purser})
//
// An Intercom utterance is said without the radio chain (no band-pass,
// noise or squelch) and is not tied to the frequency: Hear queues it while
// the radio is off, on another frequency or none, and a change of frequency
// does not drop it. The intercom has its own player queue (IntercomKey) on
// the same output device, one utterance at a time with IntercomGap between,
// MaxLag as on the radio; it is heard beside the radio, not after it. The
// pool's rules (shifts, FemaleShare, Options.Exclude) do not apply to a voice
// given in Utterance.Voice, which wins on the radio as well.
//
// # What must be beside the application
//
// Piper: PiperPath, by default bin/piper/piper.exe next to the running
// executable (os.Executable), with the rest of piper_windows_amd64.zip in
// the same folder as it ships: espeak-ng.dll, espeak-ng-data/,
// onnxruntime.dll, onnxruntime_providers_shared.dll, piper_phonemize.dll and
// libtashkeel_model.ort. Without the executable tts.Open falls back to
// another backend and the speaker reports ErrNoVoice (with Options.Hint).
//
// Voice models: VoicesDir, by default the per-user data folder
// (%LOCALAPPDATA%\voice-goio\voices on Windows, never the working
// directory). Each model is <name>.onnx with its <name>.onnx.json beside
// it, anywhere under VoicesDir (piper's resolver walks the tree; the pool
// checks the manifest's path, e.g. en/en_GB/alan/medium/en_GB-alan-medium.onnx,
// which is how cmd/voicecheck download lays them out). The pool assigns only
// the voices.json models that are installed there; with none installed it
// assigns any and synthesis fails (logged), so at least one English model is
// needed: en_GB-alan-medium, en_US-ryan-medium and en_GB-vctk-medium are a
// good start; voices.json lists the rest.
//
// Packaging (e.g. MSIX): the speaker writes nothing on Windows. Piper is
// found relative to the executable and the models are only read, so both can
// sit in a read-only install folder: ship the models in the package and pass
// their folder as VoicesDir (an absolute path built from os.Executable), or
// leave VoicesDir empty to use models downloaded per user. The piper
// resolver used here never writes; voices.Pool.Resolve (the espeak-override
// copies) is not used by the speaker. Clip and WAV make audio in memory.
package speaker
