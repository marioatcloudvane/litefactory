package store

import (
	"path/filepath"
	"testing"
)

func TestEngineeringMigrationPinsExistingFeaturesThroughRestartAndReplan(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	applyMigrationsThrough(t, db, 18)
	for _, state := range []string{"planning", "implementing", "completed"} {
		if _, err := db.sql.ExecContext(t.Context(), `INSERT INTO managed_features(parent_task,generation,state,ready) VALUES(?,1,?,1)`, state, state); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, parent := range []string{"planning", "implementing", "completed"} {
		f, err := db.GetManagedFeature(t.Context(), parent)
		if err != nil {
			t.Fatal(err)
		}
		if f.WorkflowVersion != WorkflowLegacy {
			t.Fatalf("%s changed workflow %+v", parent, f)
		}
	}
	if err := db.ReplanManagedFeature(t.Context(), "implementing", 1); err != nil {
		t.Fatal(err)
	}
	f, err := db.GetManagedFeature(t.Context(), "implementing")
	if err != nil {
		t.Fatal(err)
	}
	if f.Generation != 2 || f.WorkflowVersion != WorkflowLegacy {
		t.Fatalf("legacy replan changed version %+v", f)
	}
}

func TestEngineeringNewApprovalPinsVersionAndReplanRetainsIt(t *testing.T) {
	db, session, revisions := approvedFixture(t)
	if err := db.ApproveManagedStories(t.Context(), session.TaskRef, 1, revisions); err != nil {
		t.Fatal(err)
	}
	f, err := db.GetManagedFeature(t.Context(), session.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	if f.WorkflowVersion != WorkflowSeparated {
		t.Fatalf("new approval version %d", f.WorkflowVersion)
	}
	for _, s := range revisions {
		if err := db.DeliverStoryScopeNotice(t.Context(), session.TaskRef, 1, s.Key, "freeze"); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.ActivateManagedScope(t.Context(), session.TaskRef, 1); err != nil {
		t.Fatal(err)
	}
	if err := db.ReplanManagedFeature(t.Context(), session.TaskRef, 1); err != nil {
		t.Fatal(err)
	}
	f, err = db.GetManagedFeature(t.Context(), session.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	if f.WorkflowVersion != WorkflowSeparated {
		t.Fatal("new workflow lost on replan")
	}
}
