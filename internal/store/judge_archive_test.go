package store

import (
	"errors"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

func TestJudgeArchivedPlanRecreationGetsNewSourceIdentity(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	session := archiveTestSession(t, db, "acme", 815)
	state := planbrief.PlanState{SharedRevision: "shared", Stories: []planbrief.PlanStoryState{{Key: "selected", Draft: planbrief.PlanDraft{Key: "selected", Name: "Saved Plan", UserStory: planbrief.UserStoryContent{Want: "inspect saved work"}}}}}
	if _, err := db.SavePlanWorkflowState(ctx, session.TaskRef, 0, state); err != nil {
		t.Fatal(err)
	}
	before, err := db.ResolveJudgeSource(ctx, session.TaskRef, "")
	if err != nil {
		t.Fatal(err)
	}
	family := ArchiveFamily{SessionID: session.ID, RepoID: session.RepoID, TaskRef: session.TaskRef}
	if _, err = db.DeleteArchivedFamily(ctx, family, archiveTestTrailer(t, db, family), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	// Archive leaves the global incarnation ledger unchanged. Availability comes
	// from the retained source join, so this reference is already unavailable.
	var currentSequence int64
	if err = db.sql.QueryRowContext(ctx, `SELECT sequence FROM judge_source_commits WHERE kind='plan' AND source_id=? AND active=1`, session.TaskRef).Scan(&currentSequence); err != nil || currentSequence != before.Sequence {
		t.Fatalf("archive changed global save ledger: %d %v", currentSequence, err)
	}
	if _, err = db.ResolveJudgeSource(ctx, session.TaskRef, before.Reference); !errors.Is(err, ErrJudgeSourceUnavailable) {
		t.Fatalf("archived source remained available: %v", err)
	}
	if _, err = db.SavePlanWorkflowState(ctx, session.TaskRef, 0, state); err != nil {
		t.Fatal(err)
	}
	after, err := db.ResolveJudgeSource(ctx, session.TaskRef, "")
	if err != nil {
		t.Fatal(err)
	}
	if after.ID != before.ID || after.Revision != before.Revision || after.Digest != before.Digest {
		t.Fatal("fixture failed to recreate identical local source identity")
	}
	if after.Sequence <= before.Sequence || after.Reference == before.Reference {
		t.Fatalf("archive substituted old save identity: before=%+v after=%+v", before, after)
	}
	if _, err = db.ResolveJudgeSource(ctx, session.TaskRef, before.Reference); !errors.Is(err, ErrJudgeSourceUnavailable) {
		t.Fatalf("archived canonical reference silently substituted: %v", err)
	}
	var retired int
	if err = db.sql.QueryRowContext(ctx, `SELECT count(*) FROM judge_source_commits WHERE kind='plan' AND source_id=? AND active=0`, session.TaskRef).Scan(&retired); err != nil || retired != 1 {
		t.Fatalf("retired incarnation %d %v", retired, err)
	}
}
