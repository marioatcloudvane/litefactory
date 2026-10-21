package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func analystScheduleFixture(t *testing.T, db *DB) AnalystSchedule {
	t.Helper()
	repo, e := db.UpsertRepo(t.Context(), Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "app", BaseBranch: "main"})
	if e != nil {
		t.Fatal(e)
	}
	a, e := db.CreateAnalystSchedule(t.Context(), AnalystSchedule{ID: "schedule", CreationKey: "create", RequestDigest: "digest", Name: "review", Kind: "logs", RepositoryID: repo.ID, Config: json.RawMessage(`{}`), IntervalSeconds: 3600, Operator: "owner"})
	if e != nil {
		t.Fatal(e)
	}
	return a
}
func TestAnalystScheduleVersionDisableAndFencing(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	a := analystScheduleFixture(t, db)
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	if a.Enabled {
		t.Fatal("creation opted in")
	}
	yes, no := true, false
	a, e := db.MutateAnalystSchedule(ctx, a.ID, a.Version, AnalystScheduleMutation{Enabled: &yes, Operator: "owner"}, now)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.MutateAnalystSchedule(ctx, a.ID, 1, AnalystScheduleMutation{Enabled: &no, Operator: "owner"}, now); !errors.Is(e, ErrAnalystScheduleConflict) {
		t.Fatalf("stale mutation: %v", e)
	}
	o, e := db.ReserveAnalystOccurrence(ctx, a.ID, a.Version, now.Add(3*time.Hour), time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	if o.MissedSlots != 3 || !o.LastDue.Equal(now.Add(3*time.Hour)) {
		t.Fatalf("coalescing: %#v", o)
	}
	old, e := db.GetAnalystScheduleVersion(ctx, a.ID, a.Version)
	if e != nil || !old.NextDue.Equal(now) {
		t.Fatalf("saved config changed: %#v %v", old, e)
	}
	_, e = db.MutateAnalystSchedule(ctx, a.ID, a.Version, AnalystScheduleMutation{Enabled: &no, Operator: "owner"}, now)
	if e != nil {
		t.Fatal(e)
	}
	launchCtx := WithAnalystOccurrence(ctx, o.ID, o.Fence, o.Cutoff)
	e = db.standardsWrite(launchCtx, func(tx *sql.Tx) error { return attachAnalystAllocation(launchCtx, tx, "logs", "analysis", o.StartKey) })
	if !errors.Is(e, ErrAnalystScheduleConflict) {
		t.Fatalf("disabled launch: %v", e)
	}
	recovered, e := db.RecoverAnalystOccurrence(ctx, o.ID, o.LeaseUntil.Add(time.Second), time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	o.State = "interrupted"
	if e = db.FinalizeAnalystOccurrence(ctx, o, nil, recovered.Cutoff); !errors.Is(e, ErrAnalystScheduleConflict) {
		t.Fatalf("stale writer: %v", e)
	}
	recovered.State = "interrupted"
	if e = db.FinalizeAnalystOccurrence(ctx, recovered, nil, o.LeaseUntil.Add(2*time.Second)); e != nil {
		t.Fatal(e)
	}
}

func TestAnalystSourcesKeepLateCompletionAndExcludePriorMembership(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	a := analystScheduleFixture(t, db)
	repo, e := db.GetRepo(ctx, a.RepositoryID)
	if e != nil {
		t.Fatal(e)
	}
	session, e := db.GetOrCreateSession(ctx, repo, 99)
	if e != nil {
		t.Fatal(e)
	}
	older := testRun(t, db, session.ID, 1)
	// Another task finishes while the older task is still active.
	session2, e := db.GetOrCreateSession(ctx, repo, 100)
	if e != nil {
		t.Fatal(e)
	}
	newer := testRun(t, db, session2.ID, 2)
	if _, e = db.FinishRun(ctx, newer.ID, RunOutcome{Status: RunFailed}); e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC().Add(time.Minute)
	yes := true
	a, e = db.MutateAnalystSchedule(ctx, a.ID, a.Version, AnalystScheduleMutation{Enabled: &yes, Operator: "owner"}, now)
	if e != nil {
		t.Fatal(e)
	}
	o, e := db.ReserveAnalystOccurrence(ctx, a.ID, a.Version, now, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	o, e = db.SelectAnalystSources(ctx, o.ID, o.Fence, AnalystSourceQuery{After: now.Add(-24 * time.Hour), Limit: 1}, now)
	if e != nil {
		t.Fatal(e)
	}
	if len(o.Works) != 1 || o.Works[0].RunID != newer.ID {
		t.Fatalf("selected %#v", o.Works)
	}
	o.State = "completed"
	o.Disposition = "insufficient_evidence"
	if e = db.FinalizeAnalystOccurrence(ctx, o, nil, now); e != nil {
		t.Fatal(e)
	}
	if _, e = db.FinishRun(ctx, older.ID, RunOutcome{Status: RunSucceeded}); e != nil {
		t.Fatal(e)
	}
	now = now.Add(time.Hour)
	o, e = db.ReserveAnalystOccurrence(ctx, a.ID, a.Version, now, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	o, e = db.SelectAnalystSources(ctx, o.ID, o.Fence, AnalystSourceQuery{After: now.Add(-24 * time.Hour), Limit: 1}, now)
	if e != nil {
		t.Fatal(e)
	}
	if len(o.Works) != 1 || o.Works[0].RunID != older.ID {
		t.Fatalf("late completion lost or prior source repeated: %#v", o.Works)
	}
}

func independentAnalystStore(t *testing.T, db *DB) *DB {
	t.Helper()
	other, e := Open(t.Context(), db.Path())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := other.Close(); e != nil {
			t.Error(e)
		}
	})
	return other
}

func TestAnalystIndependentConnectionDisableAndFence(t *testing.T) {
	db := newTestDB(t)
	other := independentAnalystStore(t, db)
	ctx := t.Context()
	a := analystScheduleFixture(t, db)
	now := time.Now().UTC()
	yes, no := true, false
	a, e := db.MutateAnalystSchedule(ctx, a.ID, a.Version, AnalystScheduleMutation{Enabled: &yes, Operator: "owner"}, now)
	if e != nil {
		t.Fatal(e)
	}
	o, e := db.ReserveAnalystOccurrence(ctx, a.ID, a.Version, now, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	_, e = other.MutateAnalystSchedule(ctx, a.ID, a.Version, AnalystScheduleMutation{Enabled: &no, Operator: "owner"}, now.Add(time.Second))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.MutateAnalystSchedule(ctx, a.ID, a.Version, AnalystScheduleMutation{Name: "stale", Operator: "owner"}, now); !errors.Is(e, ErrAnalystScheduleConflict) {
		t.Fatalf("independent mutation not visible: %v", e)
	}
	launch := WithAnalystOccurrence(ctx, o.ID, o.Fence, now.Add(2*time.Second))
	e = db.standardsWrite(launch, func(tx *sql.Tx) error { return attachAnalystAllocation(launch, tx, "logs", "forbidden", o.StartKey) })
	if !errors.Is(e, ErrAnalystScheduleConflict) {
		t.Fatalf("disable lost to allocation: %v", e)
	}
	recovered, e := other.RecoverAnalystOccurrence(ctx, o.ID, now.Add(2*time.Minute), time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	o.State = "interrupted"
	if e = db.FinalizeAnalystOccurrence(ctx, o, nil, now.Add(2*time.Minute)); !errors.Is(e, ErrAnalystScheduleConflict) {
		t.Fatalf("cross-connection stale fence accepted: %v", e)
	}
	recovered.State = "interrupted"
	if e = other.FinalizeAnalystOccurrence(ctx, recovered, nil, now.Add(2*time.Minute)); e != nil {
		t.Fatal(e)
	}
	saved, e := db.GetAnalystOccurrence(ctx, o.ID)
	if e != nil || saved.Fence != recovered.Fence || saved.State != "interrupted" {
		t.Fatalf("retained recovery %#v %v", saved, e)
	}
}

func TestAnalystConcurrentRepositoryReservationAndBackwardClock(t *testing.T) {
	db := newTestDB(t)
	other := independentAnalystStore(t, db)
	ctx := t.Context()
	first := analystScheduleFixture(t, db)
	second := first
	second.ID = "second"
	second.CreationKey = "second-create"
	second.RequestDigest = "second-digest"
	second, e := db.CreateAnalystSchedule(ctx, second)
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	yes := true
	first, e = db.MutateAnalystSchedule(ctx, first.ID, first.Version, AnalystScheduleMutation{Enabled: &yes, Operator: "owner"}, now)
	if e != nil {
		t.Fatal(e)
	}
	second, e = other.MutateAnalystSchedule(ctx, second.ID, second.Version, AnalystScheduleMutation{Enabled: &yes, Operator: "owner"}, now)
	if e != nil {
		t.Fatal(e)
	}
	type reservation struct {
		o AnalystOccurrence
		e error
	}
	results := make(chan reservation, 2)
	start := make(chan struct{})
	go func() {
		<-start
		o, e := db.ReserveAnalystOccurrence(ctx, first.ID, first.Version, now, time.Minute)
		results <- reservation{o, e}
	}()
	go func() {
		<-start
		o, e := other.ReserveAnalystOccurrence(ctx, second.ID, second.Version, now, time.Minute)
		results <- reservation{o, e}
	}()
	close(start)
	accepted := 0
	var winner AnalystOccurrence
	for range 2 {
		r := <-results
		if r.e == nil {
			accepted++
			winner = r.o
		}
	}
	if accepted != 1 {
		t.Fatalf("repository capacity admitted %d", accepted)
	}
	winner.State = "no_new_evidence"
	if e = db.FinalizeAnalystOccurrence(ctx, winner, nil, now); e != nil {
		t.Fatal(e)
	}
	if _, e = other.ReserveAnalystOccurrence(ctx, winner.ScheduleID, winner.Version, now.Add(-time.Hour), time.Minute); !errors.Is(e, ErrAnalystScheduleConflict) {
		t.Fatalf("backward clock recreated slot: %v", e)
	}
	if _, e = db.ReserveAnalystOccurrence(ctx, winner.ScheduleID, winner.Version, now, time.Minute); !errors.Is(e, ErrAnalystScheduleConflict) {
		t.Fatalf("same slot recreated: %v", e)
	}
	next, e := other.ReserveAnalystOccurrence(ctx, winner.ScheduleID, winner.Version, now.Add(time.Hour), time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	if !next.FirstDue.Equal(now.Add(time.Hour)) || next.MissedSlots != 0 {
		t.Fatalf("next anchor slot %#v", next)
	}
}

func TestAnalystExplicitReanalysisRestrictsPriorSources(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	a := analystScheduleFixture(t, db)
	repo, e := db.GetRepo(ctx, a.RepositoryID)
	if e != nil {
		t.Fatal(e)
	}
	for issue := 1; issue <= 2; issue++ {
		s, e := db.GetOrCreateSession(ctx, repo, issue)
		if e != nil {
			t.Fatal(e)
		}
		r := testRun(t, db, s.ID, int64(issue))
		if _, e = db.FinishRun(ctx, r.ID, RunOutcome{Status: RunSucceeded}); e != nil {
			t.Fatal(e)
		}
	}
	now := time.Now().UTC().Add(time.Minute)
	yes := true
	a, e = db.MutateAnalystSchedule(ctx, a.ID, a.Version, AnalystScheduleMutation{Enabled: &yes, Operator: "owner"}, now)
	if e != nil {
		t.Fatal(e)
	}
	q := AnalystSourceQuery{After: now.Add(-24 * time.Hour), Limit: 1}
	var originals []AnalystOccurrence
	for range 2 {
		o, e := db.ReserveAnalystOccurrence(ctx, a.ID, a.Version, now, time.Minute)
		if e != nil {
			t.Fatal(e)
		}
		o, e = db.SelectAnalystSources(ctx, o.ID, o.Fence, q, now)
		if e != nil {
			t.Fatal(e)
		}
		if len(o.Works) != 1 {
			t.Fatalf("initial membership %#v", o.Works)
		}
		o.State = "completed"
		o.Disposition = "insufficient_evidence"
		if e = db.FinalizeAnalystOccurrence(ctx, o, nil, now); e != nil {
			t.Fatal(e)
		}
		originals = append(originals, o)
		now = now.Add(time.Hour)
	}
	a, e = db.MutateAnalystSchedule(ctx, a.ID, a.Version, AnalystScheduleMutation{ReanalysisOf: originals[0].ID, Reason: "operator requested review", Operator: "owner"}, now)
	if e != nil {
		t.Fatal(e)
	}
	o, e := db.ReserveAnalystOccurrence(ctx, a.ID, a.Version, now, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	q.Limit = 5
	o, e = db.SelectAnalystSources(ctx, o.ID, o.Fence, q, now)
	if e != nil {
		t.Fatal(e)
	}
	if len(o.Works) != 1 || o.Works[0].SourceKey != originals[0].Works[0].SourceKey || o.Generation != 2 {
		t.Fatalf("reanalysis expanded scope %#v", o)
	}
	o.State = "completed"
	o.Disposition = "insufficient_evidence"
	if e = db.FinalizeAnalystOccurrence(ctx, o, nil, now); e != nil {
		t.Fatal(e)
	}
	now = now.Add(time.Hour)
	o, e = db.ReserveAnalystOccurrence(ctx, a.ID, a.Version, now, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	o, e = db.SelectAnalystSources(ctx, o.ID, o.Fence, q, now)
	if e != nil {
		t.Fatal(e)
	}
	if len(o.Works) != 0 || o.ReanalysisOf != 0 {
		t.Fatalf("ordinary pass repeated old generations %#v", o)
	}
}

func TestAnalystSourcesSelectSeparatedBugWorkflow(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	a := analystScheduleFixture(t, db)
	repo, e := db.GetRepo(ctx, a.RepositoryID)
	if e != nil {
		t.Fatal(e)
	}
	session, e := db.GetOrCreateSession(ctx, repo, 101)
	if e != nil {
		t.Fatal(e)
	}
	bugRun := testRun(t, db, session.ID, 1)
	// Modern Bug graph runs retain workflow=bug with an empty mode.
	if _, e = db.sql.ExecContext(ctx, `UPDATE runs SET workflow='bug',mode='' WHERE id=?`, bugRun.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = db.FinishRun(ctx, bugRun.ID, RunOutcome{Status: RunFailed}); e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC().Add(time.Minute)
	yes := true
	a, e = db.MutateAnalystSchedule(ctx, a.ID, a.Version, AnalystScheduleMutation{Enabled: &yes, Operator: "owner"}, now)
	if e != nil {
		t.Fatal(e)
	}
	o, e := db.ReserveAnalystOccurrence(ctx, a.ID, a.Version, now, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	o, e = db.SelectAnalystSources(ctx, o.ID, o.Fence, AnalystSourceQuery{After: now.Add(-time.Hour), Modes: []string{"bug"}, Limit: 1}, now)
	if e != nil {
		t.Fatal(e)
	}
	if len(o.Works) != 1 || o.Works[0].RunID != bugRun.ID || o.Works[0].IssueNumber != 101 {
		t.Fatalf("separated Bug workflow was lost: %+v", o.Works)
	}
}
