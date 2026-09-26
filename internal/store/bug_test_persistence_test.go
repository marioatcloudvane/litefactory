package store

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
)

// Child persistence runs against real SQLite and real approval/repair allocation.
// Completed repair and process observations are upstream fixtures, not proof of
// executed fixer work, host isolation, or actual check commands.
func testChildRequest(t *testing.T, db *DB) BugTestRequest {
	t.Helper()
	identity, analysis := repairBoundaryApproved(t, db)
	repair, _ := repairBoundaryRequest(t, db, identity, analysis, 0, "initial")
	if _, err := db.ReserveBugRepair(t.Context(), repair, repairBoundaryRevision(t, db, identity.SessionID)); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`UPDATE bug_repairs SET state='completed' WHERE session_id=?`,
		`DELETE FROM bug_work_guards WHERE session_id=?`,
	} {
		if _, err := db.sql.ExecContext(t.Context(), statement, identity.SessionID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.sql.ExecContext(t.Context(), `INSERT INTO bug_repair_completions(session_id,request,result_id,result_revision,evidence,source_commit,source_tree,save_operation) VALUES(?,?,1,1,'{}',?,?,'saved-fixture')`, identity.SessionID, repair.Request, repair.SourceCommit, repair.SourceTree); err != nil {
		t.Fatal(err)
	}
	var defs []GraphDefinition
	for n, path := range []string{"bug.dot", "bug_tests.dot"} {
		d, err := db.CreateGraphDefinition(t.Context(), GraphDefinition{Team: "bug", Workflow: "bug", ProfileVersion: 1, Path: path, Digest: strings.Repeat(fmt.Sprint(n+1), 64), TeamSnapshotDigest: strings.Repeat("c", 64), CreatedAt: graphTestTime()})
		if err != nil {
			t.Fatal(err)
		}
		defs = append(defs, d)
	}
	parent, err := db.CreateGraphInstance(t.Context(), GraphInstance{SessionID: identity.SessionID, DefinitionID: defs[0].ID, CurrentNode: "test", StartedAt: graphTestTime(), NextDueAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	attempt := graphTestAttempt(t, db, parent, 20)
	return BugTestRequest{SessionID: identity.SessionID, Operation: "child-1", ParentInstance: parent.ID, ParentAttempt: attempt.ID, ChildDefinition: defs[1].ID, DefinitionDigest: defs[1].Digest, Binding: BugCheckBinding{SessionID: identity.SessionID, Repair: repair.Request, SourceCommit: repair.SourceCommit, SourceTree: repair.SourceTree, TargetCommit: strings.Repeat("d", 40), TargetRepository: "acme/api", TargetBranch: "main", RecipeDigest: config.BugRecipeDigest([]byte(checkPersistenceRecipe)), EnvironmentRevision: "env-1", PermissionRevision: "permission-1"}}
}
func testChildCreate(t *testing.T, db *DB, r BugTestRequest) BugTestCall {
	t.Helper()
	c, e := db.CreateBugTestCall(t.Context(), r, graphTestTime())
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func testChildFinishPhase(t *testing.T, db *DB, c BugTestCall, phase, outcome string) {
	t.Helper()
	b := c.PhaseBinding(phase)
	checkPersistenceEnsure(t, db, b)
	count := 1
	if phase == "regression" {
		count = 2
	}
	for n := 0; n < count; n++ {
		ok, e := db.ClaimBugCheckCommand(t.Context(), b.SessionID, b.Operation, n)
		if e != nil || !ok {
			t.Fatalf("launch %d: %v %v", n, ok, e)
		}
		obs := checkPersistencePassed()
		obs.Outcome = outcome
		if outcome != "passed" {
			code := 2
			if outcome == "failed" {
				code = 1
			}
			obs.Exit = &code
			obs.Reason = "controlled check obstacle"
		}
		if e = db.ObserveBugCheckCommand(t.Context(), b.SessionID, b.Operation, n, true, obs); e != nil {
			t.Fatal(e)
		}
		if outcome != "passed" {
			break
		}
	}
	if e := db.FinishBugCheckPhase(t.Context(), b.SessionID, b.Operation, outcome, "controlled observation", true); e != nil {
		t.Fatal(e)
	}
	testChildRelease(t, db, b)
}
func testChildRelease(t *testing.T, db *DB, b BugCheckBinding) {
	t.Helper()
	if e := db.ReleaseBugWork(t.Context(), b.SessionID, b.Operation); e != nil {
		t.Fatal(e)
	}
}
func testChildConsume(t *testing.T, db *DB, c BugTestCall, outcome string) error {
	t.Helper()
	return db.CompleteGraphNodeAttempt(t.Context(), c.Request.ParentAttempt, GraphCompletion{Status: GraphSucceeded, Outcome: outcome, FinishedAt: graphTestTime(), Transition: &GraphTransition{InstanceID: c.Request.ParentInstance, FromNode: "test", ToNode: "after", OnOutcome: outcome, At: graphTestTime()}})
}
func TestBugTestPersistenceConcurrentReplayReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "child.db")
	db, e := Open(t.Context(), path)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = db.Close() }()
	if e = db.Migrate(t.Context()); e != nil {
		t.Fatal(e)
	}
	r := testChildRequest(t, db)
	var wg sync.WaitGroup
	results := make(chan BugTestCall, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, e := db.CreateBugTestCall(t.Context(), r, graphTestTime())
			if e != nil {
				t.Error(e)
			}
			results <- c
		}()
	}
	wg.Wait()
	close(results)
	var first BugTestCall
	for c := range results {
		if first.ID == 0 {
			first = c
		}
		if c.ID != first.ID || c.ChildInstance != first.ChildInstance {
			t.Fatal("duplicate child")
		}
	}
	p, e := db.GetGraphInstance(t.Context(), r.ParentInstance)
	if e != nil || p.Status != GraphRunning || !p.NextDueAt.IsZero() {
		t.Fatalf("parent not suspended: %+v %v", p, e)
	}
	if e = testChildConsume(t, db, first, "passed"); e == nil {
		t.Fatal("unknown child consumed")
	}
	changed := r
	changed.Binding.TargetCommit = strings.Repeat("e", 40)
	if _, e = db.CreateBugTestCall(t.Context(), changed, graphTestTime()); e == nil {
		t.Fatal("conflicting replay accepted")
	}
	if e = db.Close(); e != nil {
		t.Fatal(e)
	}
	db, e = Open(t.Context(), path)
	if e != nil {
		t.Fatal(e)
	}
	replay := testChildCreate(t, db, r)
	if replay.ID != first.ID || replay.ChildInstance != first.ChildInstance {
		t.Fatal("restart replaced child")
	}
	byParent, e := db.BugTestParentCall(t.Context(), r.ParentInstance)
	if e != nil || byParent.ID != first.ID {
		t.Fatal("lost parent wait", e)
	}
	byChild, e := db.BugTestChildCall(t.Context(), first.ChildInstance)
	if e != nil || byChild.ID != first.ID {
		t.Fatal("lost child correlation", e)
	}
	if ok, e := db.ClaimBugWork(t.Context(), r.SessionID, "foreign", "repair"); e == nil && ok {
		t.Fatal("child lost exclusion")
	}
}
func TestBugTestPersistenceThreePhasesAndConsumption(t *testing.T) {
	db := newTestDB(t)
	r := testChildRequest(t, db)
	c := testChildCreate(t, db, r)
	for n, p := range []string{"regression", "focused", "full"} {
		testChildFinishPhase(t, db, c, p, "passed")
		got, e := db.SettleBugTestCall(t.Context(), c.ID, graphTestTime())
		if e != nil {
			t.Fatal(e)
		}
		want := "pending"
		if n == 2 {
			want = "passed"
		}
		if got.Outcome != want {
			t.Fatalf("after %s: %s", p, got.Outcome)
		}
	}
	if e := testChildConsume(t, db, c, "failed"); e == nil {
		t.Fatal("wrong outcome consumed")
	}
	if e := testChildConsume(t, db, c, "passed"); e != nil {
		t.Fatal(e)
	}
	got, e := db.GetBugTestCall(t.Context(), c.ID)
	if e != nil || !got.Consumed {
		t.Fatalf("consumption not durable: %+v %v", got, e)
	}
	if e = db.CancelBugTestCall(t.Context(), c.ID); e != nil {
		t.Fatal(e)
	}
	got, e = db.GetBugTestCall(t.Context(), c.ID)
	if e != nil || got.Cancelled {
		t.Fatal("historical consumption cancelled", e)
	}
	state, _, _, e := db.CurrentBugAnalysis(t.Context(), r.SessionID)
	if e != nil || state.AnalysisUsed != 1 {
		t.Fatal("testing changed analysis budget", e)
	}
	var count int
	if e = db.sql.QueryRowContext(t.Context(), `SELECT count(*) FROM bug_repairs WHERE session_id=?`, r.SessionID).Scan(&count); e != nil || count != 1 {
		t.Fatal("testing changed repair budget", count, e)
	}
}
func TestBugTestPersistenceCancellationWaitsForCleanup(t *testing.T) {
	db := newTestDB(t)
	c := testChildCreate(t, db, testChildRequest(t, db))
	b := c.PhaseBinding("regression")
	checkPersistenceEnsure(t, db, b)
	if ok, e := db.ClaimBugCheckCommand(t.Context(), b.SessionID, b.Operation, 0); e != nil || !ok {
		t.Fatal(ok, e)
	}
	if e := db.CancelBugTestCall(t.Context(), c.ID); e != nil {
		t.Fatal(e)
	}
	got, e := db.SettleBugTestCall(t.Context(), c.ID, graphTestTime())
	if e != nil || got.Outcome != "pending" {
		t.Fatal("cancel request certified cleanup", got, e)
	}
	if ok, e := db.ClaimBugCheckCommand(t.Context(), b.SessionID, b.Operation, 1); e == nil && ok {
		t.Fatal("cancelled command launched")
	}
	obs := checkPersistencePassed()
	obs.Outcome = "cancelled"
	obs.Reason = "termination observed"
	if e = db.ObserveBugCheckCommand(t.Context(), b.SessionID, b.Operation, 0, true, obs); e != nil {
		t.Fatal(e)
	}
	if e = db.FinishBugCheckPhase(t.Context(), b.SessionID, b.Operation, "cancelled", "owner cancelled", true); e != nil {
		t.Fatal(e)
	}
	testChildRelease(t, db, b)
	got, e = db.SettleBugTestCall(t.Context(), c.ID, graphTestTime())
	if e != nil || got.Outcome != "cancelled" {
		t.Fatal("confirmed termination not settled", got, e)
	}
	if e = testChildConsume(t, db, c, "passed"); e == nil {
		t.Fatal("cancelled child passed")
	}
	if e = testChildConsume(t, db, c, "cancelled"); e != nil {
		t.Fatal(e)
	}
}
func TestBugTestPersistenceGenerationEligibility(t *testing.T) {
	for _, outcome := range []string{"blocked", "failed"} {
		t.Run(outcome, func(t *testing.T) {
			db := newTestDB(t)
			r := testChildRequest(t, db)
			c := testChildCreate(t, db, r)
			testChildFinishPhase(t, db, c, "regression", outcome)
			if _, e := db.SettleBugTestCall(t.Context(), c.ID, graphTestTime()); e != nil {
				t.Fatal(e)
			}
			if e := testChildConsume(t, db, c, outcome); e != nil {
				t.Fatal(e)
			}
			// A later factory continuation enters the same bounded parent checkpoint.
			if _, e := db.sql.ExecContext(t.Context(), `UPDATE graph_instances SET current_node='test' WHERE id=?`, r.ParentInstance); e != nil {
				t.Fatal(e)
			}
			a, e := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: r.ParentInstance, Node: "test", StartedAt: graphTestTime(), Limit: 20})
			if e != nil {
				t.Fatal(e)
			}
			next := r
			next.Operation = "child-2"
			next.ParentAttempt = a.ID
			next.PreviousCall = c.ID
			if _, e = db.CreateBugTestCall(t.Context(), next, graphTestTime()); e == nil {
				t.Fatal("generation without resolution admitted")
			}
			next.Resolution = "infrastructure restored; fixture evidence retained"
			got, e := db.CreateBugTestCall(t.Context(), next, graphTestTime())
			if outcome == "failed" {
				if e == nil {
					t.Fatal("test failure used infrastructure retry")
				}
				return
			}
			if e != nil || got.Generation != 2 {
				t.Fatal(got, e)
			}
			replay := testChildCreate(t, db, next)
			if replay.ID != got.ID {
				t.Fatal("duplicate generation")
			}
		})
	}
}

func TestBugTestPersistenceAtomicAllocationFailure(t *testing.T) {
	db := newTestDB(t)
	r := testChildRequest(t, db)
	var before int
	if e := db.sql.QueryRowContext(t.Context(), `SELECT count(*) FROM graph_instances`).Scan(&before); e != nil {
		t.Fatal(e)
	}
	if _, e := db.sql.ExecContext(t.Context(), `CREATE TRIGGER reject_child_dispatch BEFORE INSERT ON graph_dispatches BEGIN SELECT RAISE(ABORT,'injected dispatch failure'); END`); e != nil {
		t.Fatal(e)
	}
	if _, e := db.CreateBugTestCall(t.Context(), r, graphTestTime()); e == nil {
		t.Fatal("dispatch failure reported success")
	}
	var after int
	if e := db.sql.QueryRowContext(t.Context(), `SELECT count(*) FROM graph_instances`).Scan(&after); e != nil || after != before {
		t.Fatal("partial child survived", after, before, e)
	}
	if _, e := db.BugTestParentCall(t.Context(), r.ParentInstance); !errors.Is(e, ErrNotFound) {
		t.Fatal("partial parent wait survived", e)
	}
	if _, e := db.sql.ExecContext(t.Context(), `DROP TRIGGER reject_child_dispatch`); e != nil {
		t.Fatal(e)
	}
	c := testChildCreate(t, db, r)
	if c.Generation != 1 {
		t.Fatal("failed allocation spent generation")
	}
}
func TestBugTestPersistenceLateCancellationAndDrift(t *testing.T) {
	for _, change := range []string{"cancel", "drift", "foreign-evidence"} {
		t.Run(change, func(t *testing.T) {
			db := newTestDB(t)
			c := testChildCreate(t, db, testChildRequest(t, db))
			for _, phase := range []string{"regression", "focused", "full"} {
				testChildFinishPhase(t, db, c, phase, "passed")
			}
			if _, e := db.SettleBugTestCall(t.Context(), c.ID, graphTestTime()); e != nil {
				t.Fatal(e)
			}
			want := "blocked"
			switch change {
			case "cancel":
				if e := db.CancelBugTestCall(t.Context(), c.ID); e != nil {
					t.Fatal(e)
				}
				want = "cancelled"
			case "drift":
				if e := db.InvalidateBugTestCall(t.Context(), c.ID, "target changed"); e != nil {
					t.Fatal(e)
				}
			case "foreign-evidence":
				// Corrupt the persisted correlation to exercise fail-closed consumption.
				if _, e := db.sql.ExecContext(t.Context(), `UPDATE bug_check_phases SET binding=json_set(binding,'$.generation','other-child') WHERE operation=?`, c.PhaseBinding("full").Operation); e != nil {
					t.Fatal(e)
				}
			}
			if e := testChildConsume(t, db, c, "passed"); e == nil {
				t.Fatal("changed evidence consumed as passed")
			}
			if change != "foreign-evidence" {
				if e := testChildConsume(t, db, c, want); e != nil {
					t.Fatal(e)
				}
			}
		})
	}
}

// BugChildExecutionFixture exports test setup only to the external store tests,
// which exercise the turn driver without creating an import cycle.
func BugChildExecutionFixture(t *testing.T, db *DB, digest string) BugTestCall {
	t.Helper()
	r := testChildRequest(t, db)
	// Replace the fixture pin with the actual shipped graph digest before admission.
	if _, e := db.sql.ExecContext(t.Context(), `UPDATE graph_definitions SET digest=? WHERE id=?`, digest, r.ChildDefinition); e != nil {
		t.Fatal(e)
	}
	r.DefinitionDigest = digest
	return testChildCreate(t, db, r)
}
func BugChildRetainedPhaseFixture(t *testing.T, db *DB, c BugTestCall, phase string) {
	t.Helper()
	testChildFinishPhase(t, db, c, phase, "passed")
}
