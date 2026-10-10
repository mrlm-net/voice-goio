package audio

import "testing"

// TestPlayerGain: 1 by default, clamped to 0…1; scaled leaves full volume
// untouched and scales the rest.
func TestPlayerGain(t *testing.T) {
	p, err := NewPlayer(Options{Silent: true})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if p.Gain() != 1 {
		t.Errorf("default %v", p.Gain())
	}
	p.SetGain(2)
	if p.Gain() != 1 {
		t.Errorf("clamped to %v", p.Gain())
	}
	p.SetGain(0.5)
	pcm := []int16{1000, -2000}
	if out := scaled(pcm, p.Gain()); out[0] != 500 || out[1] != -1000 || pcm[0] != 1000 {
		t.Errorf("scaled %v (in %v)", out, pcm)
	}
	if out := scaled(pcm, 1); &out[0] != &pcm[0] {
		t.Error("full volume copied")
	}
}
