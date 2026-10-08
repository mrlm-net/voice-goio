---
title: Voice Assignment
description: A voice per controller position, per controller and per crew, kept for the session.
order: 13
section: guides
---

The [`speaker`](speaker.md) does this itself. An application that assigns voices on its own uses a `voices.Pool`:

```go
pool := voices.NewPool(man, voices.PoolOptions{Dir: voices.Dir(), FemaleShare: 1.0 / 9})
twr := pool.Assign("LKPR", voicegoio.Tower)          // the same voice all session
gnd := pool.Assign("LKPR", speaker.KindOf("ground")) // never the tower's speaker
crew := pool.AssignCrew("CSA123")                    // never a controller's voice
```

## The rules

- **A position keeps its voice.** A controller position at an airport keeps its voice, and two positions at the same airport never share one. The speaker hands a position over to a new voice every 30 to 60 minutes (a shift change).
- **One controller, one voice on every frequency** (v0.13.0). Setting `Utterance.Controller` keys the voice by the person rather than the position: ground and tower worked by one controller sound like one person, with the radio sound of the `Position` each call is made on.
- **Crews never get a controller's voice** (v0.13.1). `Pool.AssignCrew` keeps a crew's voice for the session, with a cockpit's radio sound, and keeps controller and crew voices apart while either side has a free one. The speaker uses it for every pilot call.
- **Regional.** Assignment is region-weighted; `Assign` records the accent it chose on `VoiceProfile.Accent`. Controller accents in the speaker are opt-in (`speaker.Options.Accents`, off by default).

## The ATIS

The ATIS gets one fixed machine voice. It is not assigned from the regional pool and does not consume an airport's voices: an ATIS is a recording played on a loop, so it is the same flat voice everywhere, and giving Gatwick a different ATIS reader from Heathrow would be wrong rather than varied.

## Unaudited speakers

Speaker-level audit (`quality`, `pass` in the manifest) is a human listening pass, so the manifest ships those fields unset rather than fabricated. Until a model is audited its speakers are only assignable with `PoolOptions.AllowUnaudited`. `voicecheck audit -model <name>` renders the audit text in every speaker of a model for that pass.
