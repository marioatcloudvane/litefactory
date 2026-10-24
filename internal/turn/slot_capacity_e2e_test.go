package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

// slotStoryPool meters the parent and its reviewers together. Every mutation
// records the live cost, including unrelated work that creates contention.
type slotStoryPool struct {
	mu          sync.Mutex
	used, total int
	observed    []int
}

func (p *slotStoryPool) acquire(cost int) (func(), bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if cost <= 0 {
		return nil, false, fmt.Errorf("invalid fixture cost %d", cost)
	}
	if cost > p.total-p.used {
		return nil, false, nil
	}
	p.used += cost
	p.observed = append(p.observed, p.used)
	var once sync.Once
	return func() {
		once.Do(func() { p.mu.Lock(); defer p.mu.Unlock(); p.used -= cost; p.observed = append(p.observed, p.used) })
	}, true, nil
}
func (p *slotStoryPool) TryAcquire(ctx context.Context) (func(), bool, error) {
	return p.TryAcquireWeighted(ctx, 1)
}
func (p *slotStoryPool) TryAcquireWeighted(_ context.Context, cost int) (func(), bool, error) {
	return p.acquire(cost)
}
func (p *slotStoryPool) Capacity() (int, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.used, p.total
}

type slotStoryParent struct {
	pool    *slotStoryPool
	release func()
}

func (p *slotStoryParent) Replace(_ context.Context, cost int) (bool, error) {
	p.Release()
	release, ok, err := p.pool.acquire(cost)
	if ok {
		p.release = release
	}
	return ok, err
}
func (p *slotStoryParent) Release() {
	if p.release != nil {
		p.release()
		p.release = nil
	}
}
func (p *slotStoryParent) Capacity() (int, int) { return p.pool.Capacity() }

// AC-003 drives the real persisted Validate graph, suite and nested review
// activities through contention, a partial yield and startup reconciliation.
func TestSlotCapacityWorkflowWaitsAndResumesWithoutReplay(t *testing.T) {
	f := newFlowFixture(t)
	// Use a real admitted launch, including retained workflow bytes, so capacity
	// resolution exercises pinned authority at every dispatch and after resume.
	original, err := f.db.GetSession(t.Context(), f.instance.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := f.db.GetRepo(t.Context(), original.RepoID)
	if err != nil {
		t.Fatal(err)
	}
	session, err := f.db.GetOrCreateSession(t.Context(), repo, 498)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.AdmitWorkflowPath(t.Context(), session.ID, "graph"); err != nil {
		t.Fatal(err)
	}
	captured := captureValidate(t)
	raw, err := json.Marshal(captured.Files)
	if err != nil {
		t.Fatal(err)
	}
	topology, err := graphexec.SemanticDigest(f.profile)
	if err != nil {
		t.Fatal(err)
	}
	admitted, err := f.db.AdmitWorkflowLaunch(t.Context(), store.WorkflowLaunch{Mode: "validate", SessionID: session.ID, TriggerID: 0, DefinitionID: f.instance.DefinitionID, Start: f.profile.Start, ContentDigest: f.profile.Digest, TopologyDigest: topology, Files: raw})
	if err != nil {
		t.Fatal(err)
	}
	f.instance = admitted.Instance
	f.stepTo("review")
	pool := &slotStoryPool{total: 2}
	f.b.Slots = pool
	f.registry.handlers[graphexec.ValidateReviewAction] = ValidateReviewActivity{b: f.b}
	step := func() GraphNodeResult {
		t.Helper()
		f.clock = f.clock.Add(time.Second)
		parent := &slotStoryParent{pool: pool}
		defer parent.Release()
		res, err := ExecuteNodeAction(WithStepCapacity(t.Context(), parent), f.opts, GraphNodeRequest{Profile: f.profile, Activities: f.registry, InstanceID: f.instance.ID, At: f.clock})
		if err != nil {
			t.Fatalf("step: %v (%s)", err, res.Activity.Diagnostic)
		}
		return res
	}
	attempts := func() []store.GraphNodeAttempt {
		t.Helper()
		rows, err := f.db.ListGraphNodeAttempts(t.Context(), f.instance.ID)
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}
	repairs := func() []store.RepairRound {
		t.Helper()
		rows, err := f.db.ListRepairRounds(t.Context(), f.instance.ID)
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}
	before := attempts()
	beforeRepairs := repairs()
	suiteRuns := len(f.suiteCommands)
	busy, ok, err := pool.acquire(2)
	if err != nil || !ok {
		t.Fatal("cannot occupy fixture pool")
	}
	for range 2 {
		res := step()
		if !res.Deferred || res.AttemptID != 0 || f.currentNode() != "review" {
			t.Fatalf("busy step %+v", res)
		}
		if !reflect.DeepEqual(before, attempts()) || !reflect.DeepEqual(beforeRepairs, repairs()) {
			t.Fatal("capacity-only wait changed attempts or repair allowances")
		}
	}
	busy()
	// Exactly one child fits alongside its executing parent. Its accepted result
	// is retained before this dispatch yields the remaining nested work.
	first := step()
	if !first.Deferred || first.AttemptID == 0 || f.currentNode() != "review" {
		t.Fatalf("partial step %+v", first)
	}
	if got := strings.Join(f.reviewers.roles(), ","); got != "acceptance" {
		t.Fatalf("first starts %q", got)
	}
	rows := f.reviewRows()
	if rows["acceptance"].State != store.ReviewAccepted {
		t.Fatalf("acceptance not retained: %+v", rows)
	}
	// The attempt and exact pending dispatch survive together even if the
	// outer driver never reaches its separate park after this return.
	owed, err := f.db.LatestGraphDispatch(t.Context(), f.instance.ID)
	if err != nil || owed.Status != "pending" || owed.Node != "review" || owed.LaunchID != admitted.LaunchID || owed.Attempts != 0 {
		t.Fatalf("yield lost its atomic continuation: %+v err=%v", owed, err)
	}
	afterPartial := attempts()
	if len(afterPartial) != len(before)+1 {
		t.Fatalf("partial attempt count %d want %d", len(afterPartial), len(before)+1)
	}
	// A code review completed before a reply was retained. Startup recovery must
	// recover that result and run only the architecture reviewer still missing.
	codeAttempt := validateReviewAttempt(f.instance.ID, "code", f.base)
	if _, _, err := f.db.BeginValidateReview(t.Context(), store.ValidateReview{InstanceID: f.instance.ID, Role: "code", Commit: f.base, Attempt: codeAttempt, StartedAt: f.clock}); err != nil {
		t.Fatal(err)
	}
	f.reviewers.retained[codeAttempt] = goodReview(ValidateReviewRequest{Role: "code", Stories: f.stories, SnapshotID: validateSnapshotID(f.base)})
	if _, err := f.db.ReconcileWorkflowGraphs(t.Context(), "validate", f.clock.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	// Rebuild the review handler to model a new runtime over retained records.
	f.registry.handlers[graphexec.ValidateReviewAction] = ValidateReviewActivity{b: f.b}
	busy, ok, err = pool.acquire(2)
	if err != nil || !ok {
		t.Fatal("cannot reoccupy fixture pool")
	}
	resumedBusy := step()
	if !resumedBusy.Deferred || !reflect.DeepEqual(afterPartial, attempts()) {
		t.Fatal("resume bypassed admission or consumed an attempt")
	}
	busy()
	completed := step()
	if completed.Deferred || completed.Activity.Outcome != "done" || completed.AttemptID != first.AttemptID {
		t.Fatalf("resume %+v; original attempt %d", completed, first.AttemptID)
	}
	if got := strings.Join(f.reviewers.roles(), ","); got != "acceptance,architecture" {
		t.Fatalf("replayed completed work: %s", got)
	}
	if len(attempts()) != len(afterPartial) || !reflect.DeepEqual(beforeRepairs, repairs()) {
		t.Fatal("yield consumed attempts or repair allowances")
	}
	for role, row := range f.reviewRows() {
		if row.State != store.ReviewAccepted {
			t.Fatalf("%s state %s", role, row.State)
		}
	}
	if len(f.reviewers.recovered) == 0 || f.reviewers.recovered[0] != codeAttempt {
		t.Fatalf("lost reply not recovered: %v", f.reviewers.recovered)
	}
	f.runToEnd()
	if f.final() != "assessed" || len(f.suiteCommands) != suiteRuns {
		t.Fatalf("final=%s suite starts=%d want=%d", f.final(), len(f.suiteCommands), suiteRuns)
	}
	used, total := pool.Capacity()
	if used != 0 {
		t.Fatalf("leaked %d slots", used)
	}
	sawFull := false
	for _, live := range pool.observed {
		if live < 0 || live > total {
			t.Fatalf("live capacity %d exceeds bound %d", live, total)
		}
		sawFull = sawFull || live == total
	}
	if !sawFull {
		t.Fatal("fixture never exercised full capacity")
	}
}

var _ StepCapacity = (*slotStoryParent)(nil)
var _ WeightedValidateSlots = (*slotStoryPool)(nil)
