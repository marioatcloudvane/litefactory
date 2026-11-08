package store

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

func TestPlanWorkflowStore_AdmissionBackfillAndIsolation(t *testing.T) {
	ctx := t.Context()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.ensureMigrationTable(ctx); err != nil {
		t.Fatal(err)
	}
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range migrations {
		if m.version < 58 {
			if err = db.applyMigration(ctx, m); err != nil {
				t.Fatal(err)
			}
		}
	}
	legacy := testSession(t, db, 362)
	historicalRun(t, db, legacy.ID, 10, "plan")
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	path, err := db.AdmitPlanPath(ctx, legacy.ID)
	if err != nil || path != "legacy" {
		t.Fatalf("legacy route migrated: %q %v", path, err)
	}
	fresh := testSession(t, db, 363)
	if _, err = db.AdmitWorkflowPath(ctx, fresh.ID, "legacy"); err != nil {
		t.Fatal(err)
	}
	path, err = db.PlanExecutionPath(ctx, fresh.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatal("fresh session already has a Plan route", path, err)
	}
	other := testSession(t, db, 364)
	if path, err = db.AdmitPlanPath(ctx, other.ID); err != nil || path != "graph" {
		t.Fatal("new sessions must default to graph Plan", path, err)
	}
	path, err = db.AdmitPlanWorkflowPath(ctx, fresh.ID, "graph")
	if err != nil || path != "graph" {
		t.Fatal(path, err)
	}
	if path, err = db.AdmitPlanPath(ctx, fresh.ID); err != nil || path != "graph" {
		t.Fatal("an admitted graph route must survive admission", path, err)
	}
	research, err := db.AdmitWorkflowPath(ctx, fresh.ID, "graph")
	if err != nil || research != "legacy" {
		t.Fatal("Plan route changed Research", research, err)
	}
	pin, err := db.CreateGraphDefinition(ctx, GraphDefinition{Team: "builtin", Mode: "plan", ProfileVersion: 1, Path: "plan/plan.dot", Digest: strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	launch := WorkflowLaunch{Mode: "plan", SessionID: fresh.ID, TriggerID: 22, DefinitionID: pin.ID, Start: "select_story", ContentDigest: strings.Repeat("b", 64), TopologyDigest: strings.Repeat("c", 64), Files: []byte(`{"plan.dot":"eA=="}`)}
	first, err := db.AdmitWorkflowLaunch(ctx, launch)
	if err != nil {
		t.Fatal(err)
	}
	again, err := db.AdmitWorkflowLaunch(ctx, launch)
	if err != nil || first.Instance.ID != again.Instance.ID {
		t.Fatal("Plan launch identity lost", again, err)
	}
}

func TestPlanWorkflowStore_CASHistoryAndRestart(t *testing.T) {
	ctx := t.Context()
	name := filepath.Join(t.TempDir(), "restart.db")
	db, err := Open(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	session := testSession(t, db, 362)
	state := planbrief.PlanState{SharedRevision: "shared", ConsumedReplies: []int64{123}, Stories: []planbrief.PlanStoryState{{Key: "first", Stage: "story", Candidate: "candidate", Draft: planbrief.PlanDraft{Key: "first"}, Wait: &planbrief.PlanWait{ID: "wait", Stage: "story", Candidate: "candidate", Revision: "exact-ref", Kind: "review", IssueID: 400}}}}
	rev, err := db.SavePlanWorkflowState(ctx, session.TaskRef, 0, state)
	if err != nil || rev != 1 {
		t.Fatal(rev, err)
	}
	changed := state
	changed.SharedRevision = "next"
	if _, err = db.SavePlanWorkflowState(ctx, session.TaskRef, 0, changed); err == nil {
		t.Fatal("stale CAS replaced current state")
	}
	rev, err = db.SavePlanWorkflowState(ctx, session.TaskRef, 1, changed)
	if err != nil || rev != 2 {
		t.Fatal(rev, err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	got, rev, err := db.GetPlanWorkflowState(ctx, session.TaskRef)
	if err != nil || rev != 2 || !reflect.DeepEqual(got, changed) {
		t.Fatal("restart lost current wait or consumed reply", got, rev, err)
	}
	var rows int
	if err = db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM plan_workflow_history WHERE parent=?`, session.TaskRef).Scan(&rows); err != nil || rows != 0 {
		t.Fatal("state saves must not add journal rows", rows, err)
	}
	foreign, rev, err := db.GetPlanWorkflowState(ctx, "github:other/repo#362")
	if err != nil || rev != 0 || foreign.SharedRevision != "" {
		t.Fatal("parent isolation failed")
	}
}

func TestPlanWorkflowStore_ExplicitAdmissionIsImmutable(t *testing.T) {
	db := newTestDB(t)
	legacy := testSession(t, db, 362)
	fresh := testSession(t, db, 363)
	for _, tc := range []struct {
		id             int64
		selected, want string
	}{
		{legacy.ID, "legacy", "legacy"}, {legacy.ID, "graph", "legacy"},
		{fresh.ID, "graph", "graph"}, {fresh.ID, "legacy", "graph"},
	} {
		got, err := db.AdmitPlanWorkflowPath(t.Context(), tc.id, tc.selected)
		if err != nil || got != tc.want {
			t.Fatalf("admission %+v: got %q err %v", tc, got, err)
		}
	}
	for _, tc := range []struct {
		id       int64
		selected string
	}{{0, "legacy"}, {legacy.ID, "foreign"}} {
		if _, err := db.AdmitPlanWorkflowPath(t.Context(), tc.id, tc.selected); err == nil {
			t.Fatalf("invalid admission accepted: %+v", tc)
		}
	}
}
