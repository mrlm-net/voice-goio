package main

// Two aircraft on one frequency, one runway, and an emergency between them.
//
// The single-aircraft version of this demo proved the distress path worked.
// This one is about what a frequency actually is: a shared, serialised
// resource that three parties take turns on, where the controller has to break
// an approach clearance already given to one aircraft in order to make room
// for another. The library's part is that nobody ever talks over anybody —
// every transmission is queued on the frequency and bracketed by playback
// events — and that each speaker is a distinguishable voice.

import (
	"context"
	"fmt"

	voicegoio "github.com/mrlm-net/voice-goio"
	"github.com/mrlm-net/voice-goio/stt/fake"
)

// speaker identifies who is transmitting in a scripted exchange.
type speaker int

const (
	fromTower speaker = iota
	fromBAW           // the emergency aircraft
	fromDLH           // the aircraft that has to give way
)

type call struct {
	who  speaker
	text string
	note string
}

// emergencyScript is an engine failure on final at an airport with another
// aircraft ahead of it, already cleared to land on the same runway.
//
// The geometry matters and is easy to get wrong. The emergency is the aircraft
// *behind*: number two at fifteen miles, five miles behind the aircraft that
// already has a landing clearance. The one in front is sent around not because
// it is in the way at that moment, but because a landing aircraft that is slow
// to vacate would leave the runway occupied when the emergency arrives, and
// that is not a risk anybody takes with an engine failure.
//
// The phraseology is the application's to get right, not the library's, so it
// is written here the way it would be said.
func emergencyScript() []call {
	return []call{
		// Routine first, so there is something to interrupt. DLH is number
		// one at ten miles.
		{fromTower, "DLH4EK number one, 10 miles final, RWY 27L cleared to land, wind 250 degrees 8 kt",
			"routine, before anything goes wrong"},
		{fromDLH, "cleared to land runway two seven left, Lufthansa four echo kilo", ""},

		// BAW checks in normally, five miles behind DLH. Nothing is wrong yet.
		{fromBAW, "Tower, Speedbird one two three, established ILS two seven left, one five miles final",
			"the emergency aircraft checks in as ordinary traffic, number two"},
		{fromTower, "BAW123 Tower, number two, continue approach, expect landing clearance shortly", ""},
		{fromBAW, "continue approach, Speedbird one two three", ""},

		// And then it is not ordinary traffic.
		{fromBAW, "mayday mayday mayday, Speedbird one two three, engine failure, " +
			"one five miles final, request immediate landing runway two seven left",
			"distress call: it outranks everything, roster or no roster"},
		{fromTower, "BAW123 roger MAYDAY, RWY 27L cleared to land, wind 250 degrees 8 kt, " +
			"emergency services on standby", ""},
		{fromBAW, "cleared to land runway two seven left, Speedbird one two three", ""},

		// The aircraft in front is sent around. Not because it is in the way
		// now, but because the runway has to be guaranteed clear when the
		// emergency arrives five miles behind it.
		{fromTower, "DLH4EK go around, I say again go around, climb 3000 ft, fly runway heading, " +
			"clearing the runway for an emergency aircraft 5 miles behind you",
			"the earlier landing clearance is taken back to guarantee a clear runway"},
		{fromDLH, "going around, climb tree thousand feet, runway heading, Lufthansa four echo kilo",
			"a go-around outranks the altitude readback inside it"},
		{fromTower, "DLH4EK contact Director on 119.720, expect vectors for a second approach", ""},
		{fromDLH, "one one niner decimal seven two zero, Lufthansa four echo kilo", ""},

		// With the frequency clear again, the question every emergency ends with.
		{fromTower, "BAW123 when able say souls on board and fuel remaining", ""},
		{fromBAW, "Speedbird one two three, souls on board one four seven, fuel remaining fife thousand kilos",
			"the answer, with the count as the tag value"},
		{fromTower, "BAW123 roger, one four seven souls, runway is clear, wind 250 degrees 8 kt", ""},

		// Down, and the part that does not end when the wheels stop.
		{fromBAW, "Speedbird one two three, runway vacated", ""},
		{fromTower, "BAW123 stop straight ahead on TWY A4, fire service is with you, " +
			"do not shut down engines until advised", "the emergency does not end at touchdown"},
		{fromBAW, "stopping on taxiway alpha four, Speedbird one two three", ""},
		{fromTower, "BAW123 fire service reports no external fire, no smoke, " +
			"you may shut down number two engine", ""},
		{fromBAW, "shutting down number two, Speedbird one two three, " +
			"request to remain on stand for engineering", ""},
		{fromTower, "BAW123 roger, when ready follow the leader vehicle to stand 42, " +
			"contact Ground on 121.905", ""},
		{fromBAW, "following the leader vehicle to stand four two, " +
			"one two one decimal niner zero fife, Speedbird one two three", ""},
	}
}

// emergency plays the scenario.
func (r *rig) emergency(o opts) error {
	ctx := context.Background()
	script := emergencyScript()

	roster := []string{"BAW123", "DLH4EK", "CSA1234"}
	lines := []fake.ScriptLine{{Callsigns: roster}}
	for _, c := range script {
		if c.who != fromTower {
			lines = append(lines, fake.ScriptLine{Text: c.text})
		}
	}
	rec := fake.FromLines(lines)
	defer rec.Close()

	const freq = "118.500"
	tower := r.station(o.airport, voicegoio.Tower, "_TWR", freq)

	// Both cockpits are assigned as positions at this airport, which is what
	// makes the pool give three different voices to the three parties on the
	// frequency. Two aircraft that sound alike would defeat the whole point.
	baw := station{id: "BAW123", freq: freq, kind: voicegoio.Approach,
		voice: r.pool.Assign(o.airport, voicegoio.ControllerKind("cockpit-baw"))}
	dlh := station{id: "DLH4EK", freq: freq, kind: voicegoio.Approach,
		voice: r.pool.Assign(o.airport, voicegoio.ControllerKind("cockpit-dlh"))}
	baw.voice.Radio, dlh.voice.Radio = "approach", "approach"
	// An aircraft in trouble transmits deliberately, and so does the controller
	// working it. LengthScale above 1.0 is slower than routine traffic.
	baw.voice.LengthScale = 1.12

	stationFor := map[speaker]*station{fromTower: &tower, fromBAW: &baw, fromDLH: &dlh}

	fmt.Printf("\x1b[1mEmergency: two aircraft, one frequency, one runway\x1b[0m\n")
	fmt.Printf("\x1b[2m%s on %s — DLH4EK number one at 10 miles, BAW123 number two at 15 miles\x1b[0m\n",
		tower.id, freq)
	fmt.Printf("\x1b[2mthree voices share the frequency; nobody transmits over anybody\x1b[0m\n")

	var pilotCalls int
	for _, c := range script {
		fmt.Println()
		if c.note != "" {
			fmt.Printf("  \x1b[2m· %s\x1b[0m\n", c.note)
		}
		s := stationFor[c.who]
		if err := r.transmit(ctx, *s, c.text); err != nil {
			return err
		}
		r.waitIdle(1)

		if c.who == fromTower {
			continue
		}
		pilotCalls++
		if err := rec.Start(); err != nil {
			return err
		}
		if err := rec.Stop(); err != nil {
			return err
		}
		got := <-rec.Results()
		fmt.Printf("  %-12s \x1b[35m→ %s · %s · %s · %.2f\x1b[0m\n", "",
			got.Tags[voicegoio.TagIntent], orDefault(got.Tags[voicegoio.TagCallsign], "-"),
			orDefault(got.Tags[voicegoio.TagValue], "-"), got.Confidence)
	}

	fmt.Printf("\n\x1b[2m%d transmissions on one frequency · %d pilot calls recognised · 3 voices\x1b[0m\n",
		len(script), pilotCalls)
	return nil
}
