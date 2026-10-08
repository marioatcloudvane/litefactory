package daemon

import (
	"testing"
)

// The Validate reviewers take their slots from the same pool as dispatched
// turns, so they count against max_concurrent_runs.
func TestRunSlotsLendTheDaemonPool(t *testing.T) {
	slots := &runSlots{}
	if _, ok, err := slots.TryAcquire(t.Context()); ok || err == nil {
		t.Fatalf("an unbound pool lends nothing: ok=%v err=%v", ok, err)
	}
	p := newPool(2)
	slots.bind(p)
	// A dispatched turn holds one slot already.
	if !p.try() {
		t.Fatal("the turn takes a slot")
	}
	release, ok, err := slots.TryAcquire(t.Context())
	if err != nil || !ok {
		t.Fatalf("the reviewer takes the second slot: ok=%v err=%v", ok, err)
	}
	if _, ok, err := slots.TryAcquire(t.Context()); ok || err != nil {
		t.Fatalf("a full pool refuses without waiting: ok=%v err=%v", ok, err)
	}
	release()
	release() // a second release is harmless
	again, ok, err := slots.TryAcquire(t.Context())
	if err != nil || !ok {
		t.Fatalf("a released slot is free again: ok=%v err=%v", ok, err)
	}
	again()
	p.done()
	p.wait()
}
