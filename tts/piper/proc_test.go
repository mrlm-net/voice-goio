package piper

import "testing"

// TestSentences: split after . ! ? and a space, not inside numbers.
func TestSentences(t *testing.T) {
	got := sentences("Ladies and gentlemen, welcome aboard. Our flight time is 1.5 hours!  Please fasten your seatbelts? Thank you")
	want := []string{"Ladies and gentlemen, welcome aboard.", "Our flight time is 1.5 hours!", "Please fasten your seatbelts?", "Thank you"}
	if len(got) != len(want) {
		t.Fatalf("%q", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d: %q, want %q", i, got[i], want[i])
		}
	}
	if s := sentences("CSA1, contact Ruzyne Radar 118.31"); len(s) != 1 {
		t.Errorf("%q", s)
	}
}
