package durableworkflow

import (
	"context"
	"errors"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
)

// characterizationCoordinator is a fake non-confirmed coordinator. By default it
// settles a claim at once; a hook may hold the claim to observe the shared slot.
type characterizationCoordinator struct {
	db       *store.DB
	now      func() time.Time
	busy     atomic.Bool
	executes atomic.Int64
	recovers atomic.Int64
	hook     func(context.Context, store.WorkflowAttempt, store.WorkflowJob) error
}

func (c *characterizationCoordinator) Kind() string            { return "advisory" }
func (c *characterizationCoordinator) Deadline() time.Duration { return time.Minute }
func (c *characterizationCoordinator) Idle() bool              { return !c.busy.Load() }
func (c *characterizationCoordinator) ExecuteClaim(ctx context.Context, a store.WorkflowAttempt, j store.WorkflowJob) error {
	c.executes.Add(1)
	if c.hook != nil {
		return c.hook(ctx, a, j)
	}
	return c.settle(ctx, a, "succeeded", false)
}
func (c *characterizationCoordinator) RecoverClaim(ctx context.Context, a store.WorkflowAttempt) error {
	c.recovers.Add(1)
	return c.settle(ctx, a, "uncertain", true)
}
func (c *characterizationCoordinator) settle(ctx context.Context, a store.WorkflowAttempt, state string, recovery bool) error {
	f := store.WorkflowFinish{State: state, Result: `{"answer":"fixture answer"}`, Questions: `[]`, Summary: "fixture " + state}
	if recovery {
		return c.db.ReconcileWorkflowAttempt(ctx, a, f, c.now())
	}
	return c.db.FinishWorkflowAttempt(ctx, a, f, c.now())
}

// bindCharacterizationCoordinator is the single place the fake is installed.
func bindCharacterizationCoordinator(t testing.TB, s *Service, c *characterizationCoordinator) {
	t.Helper()
	if err := s.BindAdvisory(c); err != nil {
		t.Fatal(err)
	}
}

type characterizationFixture struct {
	s           *Service
	p           owneraction.Principal
	adapter     *workerAdapter
	coordinator *characterizationCoordinator
	queue       func(string) Proposal
	admitted    int
}

func newCharacterizationFixture(t *testing.T, a *workerAdapter) *characterizationFixture {
	t.Helper()
	s, p, queue := workerFixture(t, a)
	f := &characterizationFixture{s: s, p: p, adapter: a, queue: queue}
	f.coordinator = &characterizationCoordinator{db: s.db, now: func() time.Time { return s.now() }}
	bindCharacterizationCoordinator(t, s, f.coordinator)
	return f
}

// admitAdvisory queues one advisory job on its own task conversation.
func (f *characterizationFixture) admitAdvisory(t *testing.T) store.AdvisoryAdmission {
	t.Helper()
	f.admitted++
	n := strconv.Itoa(f.admitted)
	owner, err := strconv.ParseInt(f.p.OwnerID, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	scope := store.DiscussionScope{OwnerID: owner, OwnerGeneration: f.p.OwnerGeneration, RepositoryRef: "github:acme/api", TaskRef: "github:acme/api#" + n}
	c, err := f.s.db.OpenDiscussion(t.Context(), store.DiscussionConversation{DiscussionScope: scope, ID: "conversation-" + n, ClientID: "client", RequestKey: "open-" + n, CreatedAt: f.s.now()})
	if err != nil {
		t.Fatal(err)
	}
	a := store.AdvisoryAdmission{DiscussionScope: scope, ID: "admission-" + n, ConversationID: c.ID, Operation: "ask", RequestKey: "ask-" + n, Digest: "digest-" + n, Basis: `{"stage":"research"}`, Context: `[]`, Question: "Why?", GrantID: "grant", ConsentRevision: "1", JobID: "advisory-job-" + n, ExecutionID: "advisory-execution-" + n, CreatedAt: f.s.now()}
	if a, err = f.s.db.AdmitAdvisory(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	return a
}

func characterizationJobState(t *testing.T, s *Service, id string) string {
	t.Helper()
	j, err := s.db.ReadWorkflowJob(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return j.State
}

func characterizationAttemptState(t *testing.T, s *Service, job string) string {
	t.Helper()
	attempts, err := s.db.WorkflowAttempts(t.Context(), job)
	if err != nil || len(attempts) == 0 {
		t.Fatalf("attempts for %s: %+v %v", job, attempts, err)
	}
	return attempts[len(attempts)-1].State
}

func characterizationSlotFree(t *testing.T, s *Service) bool {
	t.Helper()
	_, err := s.db.RunningWorkflowAttempt(t.Context())
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	return errors.Is(err, store.ErrNotFound)
}

func TestCharacterizationConfirmedAndAdvisoryClaimsShareOneSlot(t *testing.T) {
	var active, peak atomic.Int64
	enter := func() {
		n := active.Add(1)
		for {
			old := peak.Load()
			if n <= old || peak.CompareAndSwap(old, n) {
				return
			}
		}
	}
	started, release := make(chan struct{}), make(chan struct{})
	a := &workerAdapter{execute: func(context.Context) (Output, error) {
		enter()
		defer active.Add(-1)
		close(started)
		<-release
		return Output{State: "succeeded", Summary: "workflow done"}, nil
	}}
	f := newCharacterizationFixture(t, a)
	advStarted, advRelease := make(chan struct{}), make(chan struct{})
	f.coordinator.hook = func(ctx context.Context, at store.WorkflowAttempt, _ store.WorkflowJob) error {
		enter()
		defer active.Add(-1)
		close(advStarted)
		<-advRelease
		return f.coordinator.settle(ctx, at, "succeeded", false)
	}
	proposal := f.queue("shared-slot")
	advisory := f.admitAdvisory(t)

	done := make(chan error, 1)
	go func() { _, err := f.s.RunOnce(t.Context()); done <- err }()
	<-started
	// While the confirmed workflow holds the slot, nothing else may claim it.
	if worked, err := f.s.RunOnce(t.Context()); err != nil || worked {
		t.Fatalf("second runner claimed while busy: %v %v", worked, err)
	}
	if _, _, err := f.s.db.ClaimWorkflowAttempt(t.Context(), f.s.now(), time.Minute); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("durable slot granted twice: %v", err)
	}
	if f.coordinator.executes.Load() != 0 || characterizationJobState(t, f.s, advisory.JobID) != "queued" {
		t.Fatal("advisory claim ran beside the confirmed workflow")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if state := characterizationJobState(t, f.s, proposal.Job); state != "succeeded" {
		t.Fatalf("confirmed workflow state %q", state)
	}

	// Queued after the advisory admission, so the advisory claim is next.
	second := f.queue("behind-advisory")
	go func() { _, err := f.s.RunOnce(t.Context()); done <- err }()
	<-advStarted
	if characterizationJobState(t, f.s, advisory.JobID) != "running" || characterizationSlotFree(t, f.s) {
		t.Fatal("advisory claim does not hold the shared slot")
	}
	if _, _, err := f.s.db.ClaimWorkflowAttempt(t.Context(), f.s.now(), time.Minute); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("durable slot granted beside advisory: %v", err)
	}
	if a.calls.Load() != 1 || characterizationJobState(t, f.s, second.Job) != "queued" {
		t.Fatal("confirmed workflow ran beside the advisory claim")
	}
	close(advRelease)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if state := characterizationJobState(t, f.s, advisory.JobID); state != "succeeded" {
		t.Fatalf("advisory state %q", state)
	}
	if !characterizationSlotFree(t, f.s) || peak.Load() != 1 {
		t.Fatalf("slot free=%v peak concurrency=%d", characterizationSlotFree(t, f.s), peak.Load())
	}
}

func TestCharacterizationRunOnceWaitsWhileCoordinatorBusy(t *testing.T) {
	a := &workerAdapter{execute: func(context.Context) (Output, error) {
		return Output{State: "succeeded", Summary: "workflow done"}, nil
	}}
	f := newCharacterizationFixture(t, a)
	proposal := f.queue("busy")
	advisory := f.admitAdvisory(t)
	f.coordinator.busy.Store(true)
	for range 3 {
		if worked, err := f.s.RunOnce(t.Context()); err != nil || worked {
			t.Fatalf("claimed while coordinator busy: %v %v", worked, err)
		}
	}
	if a.calls.Load() != 0 || f.coordinator.executes.Load() != 0 || !characterizationSlotFree(t, f.s) {
		t.Fatal("work started while coordinator busy")
	}
	if characterizationJobState(t, f.s, proposal.Job) != "queued" || characterizationJobState(t, f.s, advisory.JobID) != "queued" {
		t.Fatal("queued jobs changed while coordinator busy")
	}
	f.coordinator.busy.Store(false)
	if worked, err := f.s.RunOnce(t.Context()); err != nil || !worked || a.calls.Load() != 1 {
		t.Fatalf("idle coordinator did not release the queue: %v %v", worked, err)
	}
	if worked, err := f.s.RunOnce(t.Context()); err != nil || !worked || f.coordinator.executes.Load() != 1 {
		t.Fatalf("advisory did not run next: %v %v", worked, err)
	}
}

func TestCharacterizationCancelQueuedAndRunningJobs(t *testing.T) {
	f := newCharacterizationFixture(t, &workerAdapter{})
	queued := f.admitAdvisory(t)
	cancelled, won, err := f.s.db.CancelAdvisory(t.Context(), queued.DiscussionScope, queued.JobID, f.s.now())
	if err != nil || won || cancelled.Job.State != "cancelled" {
		t.Fatalf("queued cancel %+v won=%v %v", cancelled.Job, won, err)
	}
	if characterizationJobState(t, f.s, queued.JobID) != "cancelled" || characterizationAttemptState(t, f.s, queued.JobID) != "cancelled" {
		t.Fatal("queued cancel not durable")
	}
	if worked, err := f.s.RunOnce(t.Context()); err != nil || worked || f.coordinator.executes.Load() != 0 {
		t.Fatalf("cancelled job was claimed: %v %v", worked, err)
	}

	running := f.admitAdvisory(t)
	started, release := make(chan struct{}), make(chan struct{})
	f.coordinator.hook = func(ctx context.Context, a store.WorkflowAttempt, _ store.WorkflowJob) error {
		close(started)
		<-release
		if err := f.s.db.HeartbeatWorkflowAttempt(ctx, a, f.s.now()); err != nil {
			return err
		}
		return f.coordinator.settle(ctx, a, "cancelled", false)
	}
	done := make(chan error, 1)
	go func() { _, err := f.s.RunOnce(t.Context()); done <- err }()
	<-started
	stopping, won, err := f.s.db.CancelAdvisory(t.Context(), running.DiscussionScope, running.JobID, f.s.now())
	if err != nil || won || stopping.Job.State != "stop_requested" {
		t.Fatalf("running cancel %+v won=%v %v", stopping.Job, won, err)
	}
	if characterizationAttemptState(t, f.s, running.JobID) != "stop_requested" || characterizationSlotFree(t, f.s) {
		t.Fatal("running cancel released the slot before the coordinator stopped")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if characterizationJobState(t, f.s, running.JobID) != "cancelled" || !characterizationSlotFree(t, f.s) {
		t.Fatal("stopped job did not settle as cancelled")
	}
}

func TestCharacterizationRecoverRunningWithoutReceiptIsUncertain(t *testing.T) {
	t.Run("confirmed workflow", func(t *testing.T) {
		a := &workerAdapter{found: false}
		f := newCharacterizationFixture(t, a)
		proposal := f.queue("recover")
		attempt, _, err := f.s.db.ClaimWorkflowAttempt(t.Context(), f.s.now(), time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if err = f.s.RecoverRunning(t.Context()); err != nil {
			t.Fatal(err)
		}
		if characterizationJobState(t, f.s, proposal.Job) != "uncertain" || a.calls.Load() != 0 || !characterizationSlotFree(t, f.s) {
			t.Fatalf("recovery relaunched or settled wrongly, calls=%d", a.calls.Load())
		}
		if worked, err := f.s.RunOnce(t.Context()); err != nil || worked || a.calls.Load() != 0 {
			t.Fatalf("recovered job relaunched: %v %v", worked, err)
		}
		err = f.s.db.FinishWorkflowAttempt(t.Context(), attempt, store.WorkflowFinish{State: "succeeded", Result: `{}`, Questions: `[]`, Revision: 99}, f.s.now())
		if !errors.Is(err, store.ErrWorkflowFenced) {
			t.Fatalf("stale fence accepted: %v", err)
		}
	})
	t.Run("advisory", func(t *testing.T) {
		f := newCharacterizationFixture(t, &workerAdapter{})
		advisory := f.admitAdvisory(t)
		if _, _, err := f.s.db.ClaimWorkflowAttempt(t.Context(), f.s.now(), time.Hour); err != nil {
			t.Fatal(err)
		}
		if err := f.s.RecoverRunning(t.Context()); err != nil {
			t.Fatal(err)
		}
		if f.coordinator.recovers.Load() != 1 || f.coordinator.executes.Load() != 0 {
			t.Fatalf("recover=%d execute=%d", f.coordinator.recovers.Load(), f.coordinator.executes.Load())
		}
		if characterizationJobState(t, f.s, advisory.JobID) != "uncertain" || !characterizationSlotFree(t, f.s) {
			t.Fatal("advisory recovery not uncertain")
		}
		if worked, err := f.s.RunOnce(t.Context()); err != nil || worked || f.coordinator.executes.Load() != 0 {
			t.Fatalf("recovered advisory relaunched: %v %v", worked, err)
		}
	})
}

func TestCharacterizationRecoverExpired(t *testing.T) {
	newClock := func(f *characterizationFixture) *time.Time {
		at := f.s.now()
		f.s.now = func() time.Time { return at }
		return &at
	}
	t.Run("confirmed workflow", func(t *testing.T) {
		a := &workerAdapter{found: false}
		f := newCharacterizationFixture(t, a)
		proposal := f.queue("expired")
		at := newClock(f)
		if _, _, err := f.s.db.ClaimWorkflowAttempt(t.Context(), f.s.now(), time.Minute); err != nil {
			t.Fatal(err)
		}
		if err := f.s.RecoverExpired(t.Context()); err != nil || characterizationJobState(t, f.s, proposal.Job) != "running" {
			t.Fatalf("unexpired claim reconciled: %v", err)
		}
		*at = at.Add(2 * time.Minute)
		f.coordinator.busy.Store(true)
		if err := f.s.RecoverExpired(t.Context()); err != nil || characterizationJobState(t, f.s, proposal.Job) != "running" {
			t.Fatalf("expiry reconciled while coordinator busy: %v", err)
		}
		f.coordinator.busy.Store(false)
		if err := f.s.RecoverExpired(t.Context()); err != nil {
			t.Fatal(err)
		}
		if characterizationJobState(t, f.s, proposal.Job) != "uncertain" || a.calls.Load() != 0 || !characterizationSlotFree(t, f.s) {
			t.Fatalf("expired claim not reconciled without dispatch, calls=%d", a.calls.Load())
		}
	})
	t.Run("advisory", func(t *testing.T) {
		f := newCharacterizationFixture(t, &workerAdapter{})
		advisory := f.admitAdvisory(t)
		at := newClock(f)
		if _, _, err := f.s.db.ClaimWorkflowAttempt(t.Context(), f.s.now(), time.Minute); err != nil {
			t.Fatal(err)
		}
		if err := f.s.RecoverExpired(t.Context()); err != nil || f.coordinator.recovers.Load() != 0 {
			t.Fatalf("unexpired advisory reconciled: %v", err)
		}
		*at = at.Add(2 * time.Minute)
		if err := f.s.RecoverExpired(t.Context()); err != nil {
			t.Fatal(err)
		}
		if f.coordinator.recovers.Load() != 1 || f.coordinator.executes.Load() != 0 || characterizationJobState(t, f.s, advisory.JobID) != "uncertain" {
			t.Fatalf("expired advisory recover=%d execute=%d", f.coordinator.recovers.Load(), f.coordinator.executes.Load())
		}
	})
}
