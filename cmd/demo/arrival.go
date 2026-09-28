package main

// The arrival sequence: one flight from the en route sector to the stand,
// through every controller position and every frequency change.
//
// This is the mode that exercises the whole library rather than one corner of
// it. Four positions means four voices and four radio profiles; three handoffs
// mean four frequencies, so the player's per-frequency queues and playback
// events are what keeps the transmissions in order; and every pilot call goes
// through the recogniser, so the tags the application would act on are printed
// next to the audio that produced them.

import (
	"context"
	"fmt"
	"strings"

	voicegoio "github.com/mrlm-net/voice-goio"
	"github.com/mrlm-net/voice-goio/stt/fake"
)

// exchange is one push to talk cycle in either direction. A line may carry a
// pilot call, a controller call, or both — the pilot's first, as on the radio.
type exchange struct {
	pilot string // spoken form, as the pilot says it
	ctl   string // application tokens, normalised by the library
	note  string // what this step is demonstrating
}

// leg is everything that happens on one frequency.
type leg struct {
	title string
	kind  voicegoio.ControllerKind
	id    string
	freq  string
	steps []exchange
}

// arrivalLegs is a complete EGLL arrival for a single aircraft.
//
// The phraseology is the application's responsibility, not the library's, so
// it is written out here the way it would actually be said — including the
// pilot's readbacks, which are what the recogniser has to cope with.
func arrivalLegs() []leg {
	return []leg{{
		title: "En route — radar control",
		kind:  voicegoio.Center,
		id:    "_CTR",
		freq:  "127.450",
		steps: []exchange{
			{pilot: "London Control, Speedbird one two three, flight level tree six zero",
				note: "check-in on a new frequency"},
			{ctl: "BAW123 London Control, radar contact, descend FL100, QNH 1013"},
			{pilot: "descending flight level one zero zero, QNH one zero one tree, Speedbird one two three",
				note: "readback of a descent"},
			{ctl: "BAW123 contact London Director on 119.720"},
			{pilot: "one one niner decimal seven two zero, Speedbird one two three",
				note: "a bare frequency readback, with no \"contact\" in front of it"},
		},
	}, {
		title: "Radar vectors — approach",
		kind:  voicegoio.Approach,
		id:    "_APP",
		freq:  "119.720",
		steps: []exchange{
			{pilot: "London Director, Speedbird one two three, descending flight level one zero zero"},
			{ctl: "BAW123 London Director, descend 3000 ft, turn left HDG 270, reduce speed 180 kt"},
			{pilot: "descend tree thousand feet, heading two seven zero, speed one eight zero knots, Speedbird one two three",
				note: "three instructions, one readback"},
			{ctl: "BAW123 cleared ILS approach RWY 27L, report established"},
			{pilot: "cleared ILS approach runway two seven left, Speedbird one two three"},
			{pilot: "Speedbird one two three, established", note: "a position report"},
			{ctl: "BAW123 contact Tower on 118.500"},
		},
	}, {
		title: "Final and landing — tower",
		kind:  voicegoio.Tower,
		id:    "_TWR",
		freq:  "118.500",
		steps: []exchange{
			{pilot: "Tower, Speedbird one two three, established"},
			{ctl: "BAW123 wind 250 degrees 8 kt, RWY 27L cleared to land"},
			{pilot: "cleared to land runway two seven left, Speedbird one two three"},
			{ctl: "BAW123 vacate left via TWY A4, contact Ground on 121.905"},
			{pilot: "Speedbird one two three, runway vacated", note: "runway vacated"},
		},
	}, {
		title: "Taxi in — ground",
		kind:  voicegoio.Ground,
		id:    "_GND",
		freq:  "121.905",
		steps: []exchange{
			{pilot: "Ground, Speedbird one two three, runway vacated on alpha four"},
			{ctl: "BAW123 taxi to stand 42 via TWY B and A, cross RWY 27R at B3"},
			{pilot: "taxi to stand four two via bravo and alpha, Speedbird one two three",
				note: "taxi route read back"},
		},
	}}
}

// arrival plays the sequence.
func (r *rig) arrival(o opts) error {
	ctx := context.Background()
	legs := arrivalLegs()

	// Every pilot call in order, so one recogniser can be driven through the
	// whole arrival the way a real push to talk key would drive it.
	roster := []string{"BAW123", "CSA1234", "OK-ABC"}
	lines := []fake.ScriptLine{{Callsigns: roster}}
	for _, l := range legs {
		for _, st := range l.steps {
			if st.pilot != "" {
				lines = append(lines, fake.ScriptLine{Text: st.pilot})
			}
		}
	}
	rec := fake.FromLines(lines)
	defer rec.Close()

	// The cockpit is assigned as a position at this airport so the pool's
	// uniqueness rule guarantees it a voice no controller is using.
	cockpit := station{id: "BAW123", kind: voicegoio.Approach,
		voice: r.pool.Assign(o.airport, voicegoio.ControllerKind("cockpit"))}
	cockpit.voice.Radio = "approach"

	fmt.Printf("\x1b[1mArrival: %s, radar to stand\x1b[0m\n", o.airport)
	fmt.Print("\x1b[2mfour positions, four frequencies, three handoffs; every pilot call is recognised\x1b[0m\n")

	var pilotCalls, ctlCalls int
	for _, l := range legs {
		ctl := r.station(o.airport, l.kind, l.id, l.freq)
		fmt.Printf("\n\x1b[1m── %s \x1b[0m\x1b[2m%s on %s\x1b[0m\n", l.title, ctl.id, l.freq)

		cockpit.freq = l.freq // the aircraft follows the handoff
		// On the ground the aircraft is on the same kind of link as the
		// ground station; airborne it is not.
		if l.kind == voicegoio.Ground {
			cockpit.voice.Radio = "ground"
		} else {
			cockpit.voice.Radio = "approach"
		}
		for _, st := range l.steps {
			if st.note != "" {
				fmt.Printf("  \x1b[2m· %s\x1b[0m\n", st.note)
			}
			if st.pilot != "" {
				pilotCalls++
				if err := r.transmit(ctx, cockpit, st.pilot); err != nil {
					return err
				}
				r.waitIdle(1)
				if err := rec.Start(); err != nil {
					return err
				}
				if err := rec.Stop(); err != nil {
					return err
				}
				got := <-rec.Results()
				fmt.Printf("  %-12s \x1b[35m→ intent=%s callsign=%s value=%s conf=%.2f\x1b[0m\n",
					"", got.Tags[voicegoio.TagIntent], orDefault(got.Tags[voicegoio.TagCallsign], "-"),
					orDefault(got.Tags[voicegoio.TagValue], "-"), got.Confidence)
			}
			if st.ctl != "" {
				ctlCalls++
				if err := r.transmit(ctx, ctl, st.ctl); err != nil {
					return err
				}
				r.waitIdle(1)
			}
		}
	}
	fmt.Printf("\n\x1b[2mon stand. %s\x1b[0m\n", strings.Join([]string{
		fmt.Sprintf("%d transmissions", pilotCalls+ctlCalls),
		fmt.Sprintf("%d pilot calls recognised", pilotCalls),
		fmt.Sprintf("%d positions", len(legs)),
		fmt.Sprintf("%d frequencies", len(legs)),
	}, " · "))
	return nil
}
