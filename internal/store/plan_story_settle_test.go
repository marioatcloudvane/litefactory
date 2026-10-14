package store

import (
	"path/filepath"
	"testing"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

// An uncertain write followed by a definitive conflict (recorded from a fresh
// remote read) is no longer unknown, so its debt may be settled; a request whose
// latest mutation outcome is still uncertain may not.
func TestSettlePlanStoryDebtUsesLatestMutationOutcome(t *testing.T) {
	ctx := t.Context()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "settle.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(ctx, testRepo(t, db, "owner", "repo"), 7)
	if err != nil {
		t.Fatal(err)
	}
	parent := session.TaskRef
	state := planbrief.PlanState{SharedRevision: "initial", Stories: []planbrief.PlanStoryState{{Key: "story-001"}}}
	if _, err = db.SavePlanWorkflowState(ctx, parent, 0, state); err != nil {
		t.Fatal(err)
	}
	for _, c := range []PlanStoryCandidate{{Parent: parent, Key: "story-001", RequestID: "settled", Candidate: "a", RequestJSON: `{"operation":"create"}`}, {Parent: parent, Key: "story-002", RequestID: "unknown", Candidate: "b", RequestJSON: `{"operation":"create"}`}} {
		if _, err = db.ReservePlanStory(ctx, parent, c.Key); err != nil {
			t.Fatal(err)
		}
		if err = db.PutPlanStoryCandidate(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	for _, outcome := range []string{"uncertain", "uncertain", "conflict"} {
		if err = db.AppendPlanStoryEffect(ctx, PlanStoryEffect{Parent: parent, RequestID: "settled", Phase: "update", Outcome: outcome}); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.AppendPlanStoryEffect(ctx, PlanStoryEffect{Parent: parent, RequestID: "unknown", Phase: "update", Outcome: "uncertain"}); err != nil {
		t.Fatal(err)
	}
	current, revision, err := db.GetPlanWorkflowState(ctx, parent)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.SettlePlanStoryDebt(ctx, parent, "unknown", current, revision, false); err == nil {
		t.Fatal("a request whose latest write is uncertain must not be superseded")
	}
	if err = db.SettlePlanStoryDebt(ctx, parent, "settled", current, revision, false); err != nil {
		t.Fatalf("uncertain then conflict must be settleable: %v", err)
	}
}

func TestRebasePlanStoryRevisionIsCompareAndSet(t *testing.T) {
	ctx := t.Context()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "rebase.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(ctx, testRepo(t, db, "owner", "repo"), 7)
	if err != nil {
		t.Fatal(err)
	}
	parent := session.TaskRef
	if _, err = db.SavePlanWorkflowState(ctx, parent, 0, planbrief.PlanState{SharedRevision: "initial"}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ReservePlanStory(ctx, parent, "story-001"); err != nil {
		t.Fatal(err)
	}
	if err = db.BindPlanStoryShell(ctx, parent, "story-001", 11, 12, "shell"); err != nil {
		t.Fatal(err)
	}
	if err = db.RebasePlanStoryRevision(ctx, parent, "story-001", "shell", "observed"); err != nil {
		t.Fatal(err)
	}
	if err = db.RebasePlanStoryRevision(ctx, parent, "story-001", "shell", "other"); err == nil {
		t.Fatal("a stale expected revision must not rebase")
	}
	d, err := db.GetPlanStoryDraft(ctx, parent, "story-001")
	if err != nil || d.Revision != "observed" {
		t.Fatalf("revision not rebased: %+v %v", d, err)
	}
}
