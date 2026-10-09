package speaker

import (
	"sync"
	"testing"
	"time"
)

// TestFloorOneAtATime: with the floor taken, the others wait; freed, the
// radio goes before the intercom and the intercom before the PA; never two
// at once.
func TestFloorOneAtATime(t *testing.T) {
	f := newFloor()
	done := make(chan struct{})
	if !f.take(done, prioPA) {
		t.Fatal("free floor not taken")
	}
	var mu sync.Mutex
	var order []int
	playing := 0
	var wg sync.WaitGroup
	for _, p := range []int{prioATIS, prioPA, prioIntercom, prioRadio} {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			if !f.take(done, p) {
				return
			}
			mu.Lock()
			playing++
			if playing > 1 {
				t.Error("two at once")
			}
			order = append(order, p)
			mu.Unlock()
			time.Sleep(10 * time.Millisecond)
			mu.Lock()
			playing--
			mu.Unlock()
			f.give(0)
		}(p)
	}
	time.Sleep(50 * time.Millisecond) // all four waiting
	f.give(0)
	wg.Wait()
	want := []int{prioRadio, prioIntercom, prioPA, prioATIS}
	for i := range want {
		if i >= len(order) || order[i] != want[i] {
			t.Fatalf("order %v, want %v", order, want)
		}
	}
}

// TestFloorChimeKeeps: after a chime its lane keeps the floor: its PA goes
// before a radio call already waiting; after the keep the radio may go.
func TestFloorChimeKeeps(t *testing.T) {
	f := newFloor()
	done := make(chan struct{})
	f.take(done, prioPA) // the chime
	got := make(chan int, 2)
	go func() {
		if f.take(done, prioRadio) {
			got <- prioRadio
			f.give(0)
		}
	}()
	time.Sleep(20 * time.Millisecond)
	f.give(200 * time.Millisecond) // the chime ends; its PA next
	if !f.take(done, prioPA) {
		t.Fatal("the PA after its chime not taken")
	}
	select {
	case p := <-got:
		t.Fatalf("%d went between the chime and its PA", p)
	case <-time.After(30 * time.Millisecond):
	}
	f.give(0)
	select {
	case <-got:
	case <-time.After(time.Second):
		t.Fatal("the radio never went after the PA")
	}
}

// TestFloorClosed: a waiter returns false once the speaker closes.
func TestFloorClosed(t *testing.T) {
	f := newFloor()
	done := make(chan struct{})
	f.take(done, prioRadio)
	res := make(chan bool)
	go func() { res <- f.take(done, prioPA) }()
	close(done)
	if <-res {
		t.Error("taken after close")
	}
}
