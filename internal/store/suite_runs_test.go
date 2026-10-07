package store

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var suiteSHA = strings.Repeat("a", 40)

func suiteBegin(t *testing.T, db *DB, instance int64) SuiteRun {
	t.Helper()
	run, err := db.BeginSuiteRun(t.Context(), SuiteRun{InstanceID: instance, Node: "suite", Commit: suiteSHA, RecipeDigest: "r", TreeBefore: "t", StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func TestSuiteRunLifecycleAndLookup(t *testing.T) {
	db := newTestDB(t)
	run := suiteBegin(t, db, 1)
	if run.Attempt != 1 || run.State != SuiteRunning {
		t.Fatalf("run %+v", run)
	}
	if got, err := db.RunningSuiteRun(t.Context()); err != nil || got.ID != run.ID {
		t.Fatalf("running %+v %v", got, err)
	}
	if _, err := db.BeginSuiteRun(t.Context(), SuiteRun{InstanceID: 2, Node: "suite", Commit: suiteSHA, StartedAt: graphTestTime()}); !errors.Is(err, ErrSuiteRunning) {
		t.Fatalf("second running run: %v", err)
	}
	if err := db.FinishSuiteRun(t.Context(), run.ID, SuitePassed, "t", "", graphTestTime()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RunningSuiteRun(t.Context()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("running after finish: %v", err)
	}
	got, err := db.LatestSuiteRun(t.Context(), 1, suiteSHA)
	if err != nil || got.State != SuitePassed || got.TreeAfter != "t" || got.FinishedAt.IsZero() {
		t.Fatalf("latest %+v %v", got, err)
	}
	if _, err := db.LatestSuiteRun(t.Context(), 1, strings.Repeat("b", 40)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other commit: %v", err)
	}
	if next := suiteBegin(t, db, 1); next.Attempt != 2 {
		t.Fatalf("attempt %d", next.Attempt)
	}
}

func TestSuiteFinishedRunIsImmutable(t *testing.T) { // AC-007
	db := newTestDB(t)
	run := suiteBegin(t, db, 1)
	if err := db.FinishSuiteRun(t.Context(), run.ID, SuiteFailed, "t", "boom", graphTestTime()); err != nil {
		t.Fatal(err)
	}
	if err := db.FinishSuiteRun(t.Context(), run.ID, SuiteFailed, "t", "boom", graphTestTime().Add(time.Hour)); err != nil {
		t.Fatalf("same content again: %v", err)
	}
	if err := db.FinishSuiteRun(t.Context(), run.ID, SuitePassed, "t", "", graphTestTime()); !errors.Is(err, ErrSuiteFinished) {
		t.Fatalf("different content: %v", err)
	}
	// The table itself refuses a change that bypasses the store methods.
	if _, err := db.sql.ExecContext(t.Context(), `UPDATE suite_runs SET state='passed' WHERE id=?`, run.ID); err == nil {
		t.Fatal("direct update of a finished run succeeded")
	}
	if _, err := db.sql.ExecContext(t.Context(), `DELETE FROM suite_runs WHERE id=?`, run.ID); err == nil {
		t.Fatal("delete of a finished run succeeded")
	}
}

func TestSuiteCommandClaimThenFinish(t *testing.T) {
	db := newTestDB(t)
	run := suiteBegin(t, db, 1)
	c := SuiteCommand{RunID: run.ID, Position: 0, Argv: []string{"go", "test"}, StartedAt: graphTestTime(), ProcessIdentity: "id"}
	if err := db.ClaimSuiteCommand(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	got, _ := db.SuiteCommands(t.Context(), run.ID)
	if len(got) != 1 || got[0].State != SuiteRunning || got[0].ProcessIdentity != "id" {
		t.Fatalf("claim %+v", got)
	}
	zero := 0
	c.State, c.Exit, c.FinishedAt, c.CleanupConfirmed, c.OutputTail, c.OutputDigest = SuitePassed, &zero, graphTestTime(), true, "ok", "d"
	if err := db.FinishSuiteCommand(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	if err := db.FinishSuiteCommand(t.Context(), c); err != nil {
		t.Fatalf("same content again: %v", err)
	}
	changed := c
	changed.OutputDigest = "other"
	if err := db.FinishSuiteCommand(t.Context(), changed); !errors.Is(err, ErrSuiteFinished) {
		t.Fatalf("different content: %v", err)
	}
	got, _ = db.SuiteCommands(t.Context(), run.ID)
	if got[0].State != SuitePassed || got[0].Exit == nil || *got[0].Exit != 0 || got[0].OutputTail != "ok" || got[0].ProcessIdentity != "id" {
		t.Fatalf("finished %+v", got[0])
	}
}

func TestSuiteCommandRefusesUnconfirmedPassAndOversizedTail(t *testing.T) { // AC-007
	db := newTestDB(t)
	run := suiteBegin(t, db, 1)
	base := SuiteCommand{RunID: run.ID, Argv: []string{"x"}, StartedAt: graphTestTime(), FinishedAt: graphTestTime(), State: SuitePassed}
	if err := db.FinishSuiteCommand(t.Context(), base); err == nil {
		t.Fatal("a pass without confirmed cleanup was accepted")
	}
	base.State, base.OutputTail = SuiteFailed, strings.Repeat("x", MaxSuiteOutputTail+1)
	if err := db.FinishSuiteCommand(t.Context(), base); err == nil {
		t.Fatal("an unbounded tail was accepted")
	}
}

func TestSuiteInterruptMarksRunAndCommands(t *testing.T) {
	db := newTestDB(t)
	run := suiteBegin(t, db, 1)
	if err := db.ClaimSuiteCommand(t.Context(), SuiteCommand{RunID: run.ID, Argv: []string{"x"}, StartedAt: graphTestTime(), ProcessIdentity: "id"}); err != nil {
		t.Fatal(err)
	}
	if err := db.InterruptSuiteRun(t.Context(), run.ID, graphTestTime()); err != nil {
		t.Fatal(err)
	}
	got, _ := db.GetSuiteRun(t.Context(), run.ID)
	cmds, _ := db.SuiteCommands(t.Context(), run.ID)
	if got.State != SuiteInterrupted || cmds[0].State != SuiteInterrupted {
		t.Fatalf("run %+v commands %+v", got, cmds)
	}
	if _, err := db.BeginSuiteRun(t.Context(), SuiteRun{InstanceID: 1, Node: "suite", Commit: suiteSHA, RecipeDigest: "r", StartedAt: graphTestTime()}); err != nil {
		t.Fatalf("a new attempt after the interruption: %v", err)
	}
}

func TestLockSuiteIsExclusivePerHome(t *testing.T) { // AC-004
	db := newTestDB(t)
	dir := t.TempDir()
	unlock, err := db.LockSuite(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.LockSuite(t.Context(), dir); !errors.Is(err, ErrSuiteBusy) {
		t.Fatalf("second lock: %v", err)
	}
	if err := unlock(); err != nil {
		t.Fatal(err)
	}
	again, err := db.LockSuite(t.Context(), dir)
	if err != nil {
		t.Fatalf("lock after release: %v", err)
	}
	_ = again()
}

func TestReconcileWorkflowGraphsCoversValidate(t *testing.T) { // AC-006
	db := newTestDB(t)
	s := testSession(t, db, 316)
	d, err := db.CreateGraphDefinition(t.Context(), GraphDefinition{Team: "core", Mode: "validate", ProfileVersion: 1, Path: "core/validate.dot", Digest: strings.Repeat("b", 64), CreatedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	i, err := db.CreateGraphInstance(t.Context(), GraphInstance{SessionID: s.ID, DefinitionID: d.ID, CurrentNode: "suite", StartedAt: graphTestTime(), NextDueAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	graphTestAttempt(t, db, i, 3)
	n, err := db.ReconcileWorkflowGraphs(t.Context(), "validate", graphTestTime())
	if err != nil || n != 1 {
		t.Fatalf("reconciled %d %v", n, err)
	}
	got, err := db.GetGraphInstance(t.Context(), i.ID)
	if err != nil || got.Status != GraphFailed {
		t.Fatalf("instance %+v %v", got, err)
	}
}
