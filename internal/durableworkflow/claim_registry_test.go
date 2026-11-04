package durableworkflow

import (
	"context"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
)

type kindCoordinator struct {
	kind     string
	deadline time.Duration
	busy     atomic.Bool
	executed atomic.Int64
	recovers atomic.Int64
	claimed  store.WorkflowAttempt
	db       *store.DB
	now      func() time.Time
}

func (c *kindCoordinator) Kind() string            { return c.kind }
func (c *kindCoordinator) Deadline() time.Duration { return c.deadline }
func (c *kindCoordinator) Idle() bool              { return !c.busy.Load() }
func (c *kindCoordinator) ExecuteClaim(ctx context.Context, a store.WorkflowAttempt, _ store.WorkflowJob) error {
	c.executed.Add(1)
	c.claimed = a
	return c.db.FinishWorkflowAttempt(ctx, a, store.WorkflowFinish{State: "succeeded", Result: `{"answer":"ok"}`, Questions: `[]`, Summary: "ok"}, c.now())
}
func (c *kindCoordinator) RecoverClaim(ctx context.Context, a store.WorkflowAttempt) error {
	c.recovers.Add(1)
	return c.db.ReconcileWorkflowAttempt(ctx, a, store.WorkflowFinish{State: "uncertain", Result: `{}`, Questions: `[]`, Summary: "uncertain"}, c.now())
}

func admitRegistryAdvisory(t *testing.T, s *Service, p owneraction.Principal, n int) store.AdvisoryAdmission {
	t.Helper()
	id := strconv.Itoa(n)
	owner, err := strconv.ParseInt(p.OwnerID, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	scope := store.DiscussionScope{OwnerID: owner, OwnerGeneration: p.OwnerGeneration, RepositoryRef: "github:acme/api", TaskRef: "github:acme/api#" + id}
	c, err := s.db.OpenDiscussion(t.Context(), store.DiscussionConversation{DiscussionScope: scope, ID: "registry-conversation-" + id, ClientID: "client", RequestKey: "open-" + id, CreatedAt: s.now()})
	if err != nil {
		t.Fatal(err)
	}
	a := store.AdvisoryAdmission{DiscussionScope: scope, ID: "registry-admission-" + id, ConversationID: c.ID, Operation: "ask", RequestKey: "ask-" + id, Digest: "digest-" + id, Basis: `{"stage":"research"}`, Context: `[]`, Question: "Why?", GrantID: "grant", ConsentRevision: "1", JobID: "registry-job-" + id, ExecutionID: "registry-execution-" + id, CreatedAt: s.now()}
	if a, err = s.db.AdmitAdvisory(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestRegisterRefusesReservedEmptyAndDuplicateKinds(t *testing.T) {
	s, _, _ := workerFixture(t, &workerAdapter{})
	for _, kind := range []string{"", ConfirmedWorkflowKind} {
		if err := s.Register(&kindCoordinator{kind: kind, deadline: time.Minute}); err == nil {
			t.Fatalf("kind %q accepted", kind)
		}
	}
	if err := s.Register(&kindCoordinator{kind: "brainstorm_scan"}); err == nil {
		t.Fatal("zero deadline accepted")
	}
	if err := s.Register(&kindCoordinator{kind: "brainstorm_scan", deadline: time.Minute}); err != nil {
		t.Fatal(err)
	}
	if err := s.Register(&kindCoordinator{kind: "brainstorm_scan", deadline: time.Minute}); err == nil {
		t.Fatal("duplicate kind accepted")
	}
	if err := s.BindAdvisory(&kindCoordinator{kind: "brainstorm_scan", deadline: time.Minute}); err == nil {
		t.Fatal("advisory binding accepted another kind")
	}
	if err := s.Register(nil); err == nil {
		t.Fatal("nil coordinator accepted")
	}
}

func TestRunOnceDispatchesByKindWithLongestDeadlineAndSharedIdleGate(t *testing.T) {
	s, p, _ := workerFixture(t, &workerAdapter{})
	advisory := &kindCoordinator{kind: "advisory", deadline: time.Minute, db: s.db, now: s.now}
	scan := &kindCoordinator{kind: "brainstorm_scan", deadline: 3 * time.Hour, db: s.db, now: s.now}
	for _, c := range []*kindCoordinator{advisory, scan} {
		if err := s.Register(c); err != nil {
			t.Fatal(err)
		}
	}
	a := admitRegistryAdvisory(t, s, p, 1)
	scan.busy.Store(true)
	if ran, err := s.RunOnce(t.Context()); err != nil || ran || advisory.executed.Load() != 0 {
		t.Fatalf("claimed while another kind was busy: %v %v", ran, err)
	}
	scan.busy.Store(false)
	if ran, err := s.RunOnce(t.Context()); err != nil || !ran {
		t.Fatalf("run = %v %v", ran, err)
	}
	if advisory.executed.Load() != 1 || scan.executed.Load() != 0 {
		t.Fatalf("dispatch advisory=%d scan=%d", advisory.executed.Load(), scan.executed.Load())
	}
	if advisory.claimed.JobID != a.JobID || !advisory.claimed.Deadline.Equal(advisory.claimed.StartedAt.Add(3*time.Hour)) {
		t.Fatalf("claim limit = %+v", advisory.claimed)
	}
}

func TestUnregisteredKindIsSettledWithoutRunning(t *testing.T) {
	s, p, _ := workerFixture(t, &workerAdapter{})
	scan := &kindCoordinator{kind: "brainstorm_scan", deadline: time.Minute, db: s.db, now: s.now}
	if err := s.Register(scan); err != nil {
		t.Fatal(err)
	}
	a := admitRegistryAdvisory(t, s, p, 1)
	if ran, err := s.RunOnce(t.Context()); err != nil || !ran {
		t.Fatalf("run = %v %v", ran, err)
	}
	j, err := s.db.ReadWorkflowJob(t.Context(), a.JobID)
	if err != nil || j.State != "failed" || scan.executed.Load() != 0 {
		t.Fatalf("job = %+v %v executed=%d", j, err, scan.executed.Load())
	}
	if _, err = s.db.RunningWorkflowAttempt(t.Context()); err == nil {
		t.Fatal("refused claim kept the shared slot")
	}
}

func TestRecoveryDispatchesByKindAndSettlesUnregisteredKindAsUncertain(t *testing.T) {
	s, p, _ := workerFixture(t, &workerAdapter{})
	admitRegistryAdvisory(t, s, p, 1)
	a, _, err := s.db.ClaimWorkflowAttempt(t.Context(), s.now(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RecoverRunning(t.Context()); err != nil {
		t.Fatal(err)
	}
	attempts, err := s.db.WorkflowAttempts(t.Context(), a.JobID)
	if err != nil || attempts[0].State != "uncertain" {
		t.Fatalf("unregistered recovery = %+v %v", attempts, err)
	}
	advisory := &kindCoordinator{kind: "advisory", deadline: time.Minute, db: s.db, now: s.now}
	if err = s.Register(advisory); err != nil {
		t.Fatal(err)
	}
	admitRegistryAdvisory(t, s, p, 2)
	if _, _, err = s.db.ClaimWorkflowAttempt(t.Context(), s.now(), time.Minute); err != nil {
		t.Fatal(err)
	}
	if err = s.RecoverRunning(t.Context()); err != nil || advisory.recovers.Load() != 1 || advisory.executed.Load() != 0 {
		t.Fatalf("recover = %v recovers=%d executed=%d", err, advisory.recovers.Load(), advisory.executed.Load())
	}
}
