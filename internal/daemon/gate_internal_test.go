package daemon

import (
	"context"
	"errors"
	"github.com/cloudvane/litefactory/internal/testutil"
	"sync"
	"testing"
	"time"
)

// gateTestTimeout bounds the rendezvous below. Every failure this file looks
// for presents as a goroutine that never wakes up, and "panic: test timed out
// after 10m" names neither the test nor the line. Failing on our own clock is
// the difference between a diagnosis and a stack dump.
const gateTestTimeout = testutil.Wait

// TestCloneGateExcludesOneClone is AC-PAR-3.1's core: concurrent holders of one
// clone are serialised.
//
// The shared counter is deliberately unguarded. If the gate ever granted two
// holders at once, the increment and the read below would be a data race and
// -race would say so by name — which is a better failure than a flaky count,
// because it points at the two goroutines that overlapped. The explicit maximum
// is the second half, for the case where the overlap happens not to be caught.
func TestCloneGateExcludesOneClone(t *testing.T) {
	t.Parallel()

	const (
		clone   = "/clones/acme-api"
		holders = 8
		rounds  = 50
	)

	gate := newCloneGate()
	held, maxHeld, entries := 0, 0, 0

	var wg sync.WaitGroup
	for range holders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range rounds {
				release, err := gate.Acquire(t.Context(), clone)
				if err != nil {
					t.Errorf("Acquire: %v", err)
					return
				}
				held++
				entries++
				if held > maxHeld {
					maxHeld = held
				}
				held--
				release()
			}
		}()
	}
	wg.Wait()

	if maxHeld != 1 {
		t.Errorf("the gate was held by %d callers at once, want 1", maxHeld)
	}
	if entries != holders*rounds {
		t.Errorf("the gate was entered %d times, want %d", entries, holders*rounds)
	}
}

// TestCloneGateDoesNotSerialiseDistinctClones is the other half of AC-PAR-3.1:
// the gate is per clone, not global.
//
// Two holders of two different clones are made to wait for each other. A single
// global lock — the shape this would collapse into if the map key were dropped
// or the map mutex were held across the acquisition — cannot get both of them
// past the barrier, and the test fails on its own timeout rather than hanging.
func TestCloneGateDoesNotSerialiseDistinctClones(t *testing.T) {
	t.Parallel()

	gate := newCloneGate()
	inside := make(chan string, 2)
	proceed := make(chan struct{})

	var wg sync.WaitGroup
	for _, clone := range []string{"/clones/acme-api", "/clones/acme-web"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := gate.Acquire(t.Context(), clone)
			if err != nil {
				t.Errorf("Acquire %s: %v", clone, err)
				return
			}
			defer release()
			inside <- clone
			<-proceed
		}()
	}

	for i := range 2 {
		select {
		case <-inside:
		case <-time.After(gateTestTimeout):
			close(proceed)
			wg.Wait()
			t.Fatalf("only %d of 2 clones could be held at once; the gate is global, not per clone", i)
		}
	}
	close(proceed)
	wg.Wait()
}

// TestCloneGateAcquireIsCancellable is the reason this is a channel and not a
// sync.Mutex, and it is the single most likely thing to be "simplified" away.
//
// sync.Mutex.Lock cannot be abandoned. A worker queued behind a sibling's
// 60-second fetch would sit inside Lock with no way to be told the daemon is
// shutting down, and [Daemon.Quiesce] would wait for it past the grace budget.
// A select on ctx.Done() is the same exclusion with an exit; this test is that
// exit. If it ever fails by timing out, someone swapped the channel for a
// mutex.
func TestCloneGateAcquireIsCancellable(t *testing.T) {
	t.Parallel()

	const clone = "/clones/acme-api"

	gate := newCloneGate()
	release, err := gate.Acquire(t.Context(), clone)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer release()

	ctx, cancel := context.WithCancel(t.Context())
	waited := make(chan error, 1)
	go func() {
		secondRelease, secondErr := gate.Acquire(ctx, clone)
		if secondErr == nil {
			secondRelease()
		}
		waited <- secondErr
	}()

	cancel()
	select {
	case err := <-waited:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Acquire on a cancelled context = %v, want context.Canceled", err)
		}
	case <-time.After(gateTestTimeout):
		t.Fatalf("a queued Acquire did not give up %v after its context was cancelled; "+
			"the wait is uninterruptible, which is what a sync.Mutex would do", gateTestTimeout)
	}
}

// TestCloneGateCancelledAcquireLeavesTheGateUsable checks the abandonment does
// not corrupt the gate: giving up a wait must not consume the slot, or the
// clone would be wedged for the life of the process by one shutdown.
func TestCloneGateCancelledAcquireLeavesTheGateUsable(t *testing.T) {
	t.Parallel()

	const clone = "/clones/acme-api"

	gate := newCloneGate()
	release, err := gate.Acquire(t.Context(), clone)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := gate.Acquire(cancelled, clone); !errors.Is(err, context.Canceled) {
		t.Fatalf("Acquire on a cancelled context = %v, want context.Canceled", err)
	}

	release()
	second, err := gate.Acquire(t.Context(), clone)
	if err != nil {
		t.Fatalf("Acquire after an abandoned wait: %v", err)
	}
	second()
}

// TestCloneGateReleaseIsIdempotent pins the sync.Once inside Acquire.
//
// A duplicated defer is the way a call site is likely to get this wrong, and
// without the Once the second call would hand the gate to another turn while
// the first was still inside its critical section — mutual exclusion broken by
// a copy-pasted line, with no error anywhere. Here the second release must be
// a no-op, so the gate is still held afterwards.
func TestCloneGateReleaseIsIdempotent(t *testing.T) {
	t.Parallel()

	const clone = "/clones/acme-api"

	gate := newCloneGate()
	first, err := gate.Acquire(t.Context(), clone)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	first()
	first()

	second, err := gate.Acquire(t.Context(), clone)
	if err != nil {
		t.Fatalf("Acquire after a doubled release: %v", err)
	}
	defer second()

	// The doubled release must not have left a spare slot behind: with the
	// gate held, a third acquisition has to block until it is given back.
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := gate.Acquire(cancelled, clone); !errors.Is(err, context.Canceled) {
		t.Errorf("Acquire while the gate is held = %v, want it to block and then report context.Canceled; "+
			"a doubled release let a second holder in", err)
	}
}

// TestCloneGateKeysAreExactPaths records that the key is store.Repo.Path as
// stored, compared byte for byte.
//
// That is safe because the stored path is git's own answer for the clone's top
// level, already absolute and symlink-resolved (internal/git/clone.go), so two
// registrations of one clone cannot disagree about its spelling. It is written
// down because the alternative — normalising here — would put a second,
// divergent idea of a clone's identity into the tree.
func TestCloneGateKeysAreExactPaths(t *testing.T) {
	t.Parallel()

	gate := newCloneGate()
	release, err := gate.Acquire(t.Context(), "/clones/acme-api")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer release()

	other, err := gate.Acquire(t.Context(), "/clones/acme-api/")
	if err != nil {
		t.Fatalf("Acquire a differently spelled path: %v", err)
	}
	other()

	if len(gate.gates) != 2 {
		t.Errorf("gates = %d, want 2: keys are compared as stored, not normalised", len(gate.gates))
	}
}
