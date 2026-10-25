package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestInvocationContextUsesResolvedDeadline(t *testing.T) {
	deadline := time.Now().Add(time.Hour)
	worker, stop := WithWorkerDeadline(t.Context(), deadline)
	defer stop()
	invocation, cancel := InvocationContext(worker, time.Nanosecond)
	defer cancel()
	got, ok := invocation.Deadline()
	if !ok || !got.Equal(deadline) {
		t.Fatalf("deadline = %s, want %s", got, deadline)
	}
	if invocation.Err() != nil {
		t.Fatalf("short fallback expired worker: %v", invocation.Err())
	}
}

func TestInvocationContextFallbackAndCancellation(t *testing.T) {
	parent, cancel := context.WithCancel(t.Context())
	invocation, stop := InvocationContext(parent, time.Hour)
	defer stop()
	if _, ok := invocation.Deadline(); !ok {
		t.Fatal("unmarked invocation has no fallback deadline")
	}
	cancel()
	if !errors.Is(invocation.Err(), context.Canceled) {
		t.Fatalf("cancellation = %v", invocation.Err())
	}
}

func TestEvidenceContextSharesExpiredWorkerAllowance(t *testing.T) {
	worker, stop := WithWorkerDeadline(t.Context(), time.Now().Add(-time.Minute))
	defer stop()
	before := time.Now()
	first, cancel := EvidenceContext(worker)
	defer cancel()
	deadline, _ := first.Deadline()
	if first.Err() != nil || deadline.Before(before.Add(30*time.Second)) || deadline.After(time.Now().Add(30*time.Second)) {
		t.Fatalf("late collection deadline=%s error=%v", deadline, first.Err())
	}
	var collectors sync.WaitGroup
	for range 8 {
		collectors.Go(func() {
			evidence, cancel := EvidenceContext(worker)
			defer cancel()
			got, _ := evidence.Deadline()
			if !got.Equal(deadline) || evidence.Err() != nil {
				t.Errorf("shared deadline=%s want=%s error=%v", got, deadline, evidence.Err())
			}
		})
	}
	collectors.Wait()
}

func TestEvidenceContextDoesNotRenewExpiredCleanup(t *testing.T) {
	worker, stop := WithWorkerDeadline(t.Context(), time.Now().Add(-time.Minute))
	defer stop()
	state, ok := worker.Value(workerDeadlineKey{}).(*workerDeadlineState)
	if !ok {
		t.Fatal("worker context lacks shared cleanup state")
	}
	deadline := time.Now().Add(-time.Second)
	state.evidenceOnce.Do(func() { state.evidenceDeadline = deadline })
	evidence, cancel := EvidenceContext(worker)
	defer cancel()
	got, _ := evidence.Deadline()
	if !got.Equal(deadline) || !errors.Is(evidence.Err(), context.DeadlineExceeded) {
		t.Fatalf("expired cleanup renewed: deadline=%s error=%v", got, evidence.Err())
	}
}

func TestEvidenceContextSurvivesParentCancellation(t *testing.T) {
	parent, cancel := context.WithCancel(t.Context())
	cancel()
	before := time.Now()
	evidence, stop := EvidenceContext(parent)
	defer stop()
	deadline, _ := evidence.Deadline()
	if evidence.Err() != nil || deadline.After(time.Now().Add(30*time.Second)) || deadline.Before(before.Add(30*time.Second)) {
		t.Fatalf("evidence deadline=%s error=%v", deadline, evidence.Err())
	}
}

func TestEvidenceContextLiveCollectionDoesNotSpendCleanupAllowance(t *testing.T) {
	workerDeadline := time.Now().Add(time.Hour)
	worker, stop := WithWorkerDeadline(t.Context(), workerDeadline)
	defer stop()
	state, ok := worker.Value(workerDeadlineKey{}).(*workerDeadlineState)
	if !ok {
		t.Fatal("worker context lacks shared cleanup state")
	}
	live, cancelLive := EvidenceContext(worker)
	defer cancelLive()
	if live.Err() != nil || !state.evidenceDeadline.IsZero() {
		t.Fatalf("live collection spent independent cleanup allowance: %s", state.evidenceDeadline)
	}
	// Reuse the logical attempt's state with an expired context to model expiry
	// without waiting for an ambient clock.
	expired, stopExpired := context.WithDeadline(t.Context(), time.Now().Add(-time.Minute))
	defer stopExpired()
	expired = context.WithValue(expired, workerDeadlineKey{}, state)
	before := time.Now()
	cleanup, stopCleanup := EvidenceContext(expired)
	defer stopCleanup()
	deadline, _ := cleanup.Deadline()
	if cleanup.Err() != nil || deadline.Before(before.Add(30*time.Second)) {
		t.Fatalf("expiry lost independent allowance: deadline=%s error=%v", deadline, cleanup.Err())
	}
	stop()
	if !errors.Is(live.Err(), context.Canceled) {
		t.Fatalf("live collection ignored worker cancellation: %v", live.Err())
	}
}

func TestEvidenceContextMarkedCancellationKeepsIndependentAllowance(t *testing.T) {
	worker, stop := WithWorkerDeadline(t.Context(), time.Now().Add(time.Hour))
	stop()
	evidence, cancel := EvidenceContext(worker)
	defer cancel()
	if evidence.Err() != nil {
		t.Fatalf("cancelled worker blocked independent evidence: %v", evidence.Err())
	}
}
