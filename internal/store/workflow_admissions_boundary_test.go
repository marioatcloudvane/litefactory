package store

import (
	"context"
	"errors"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestWorkflowBoundaryCompetingAdmissionReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "admission.db")
	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	s := testSession(t, db, 389)
	const count = 24
	results := make(chan TaskWorkflowAdmission, count)
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for n := range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a := TaskWorkflowAdmission{SessionID: s.ID, Version: 1, Family: "standalone", Workflow: "bug", SourceTrigger: int64(n + 1)}
			if n%2 == 0 {
				a.Family = "mode"
				a.Workflow = "plan"
			}
			got, e := db.AdmitWorkflow(t.Context(), a)
			results <- got
			errs <- e
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	winner, err := db.GetTaskWorkflowAdmission(t.Context(), s.ID)
	if err != nil {
		t.Fatal(err)
	}
	for got := range results {
		if got != winner {
			t.Fatalf("competing writer replaced winner: %+v != %+v", got, winner)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	got, err := db.AdmitWorkflow(t.Context(), TaskWorkflowAdmission{SessionID: s.ID, Version: 1, Family: "standalone", Workflow: "bug", SourceTrigger: 999})
	if err != nil || got != winner {
		t.Fatalf("reopened replay=%+v err=%v winner=%+v", got, err, winner)
	}
	other := testSession(t, db, 390)
	if _, err := db.GetTaskWorkflowAdmission(t.Context(), other.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other task inherited admission: %v", err)
	}
}

func TestWorkflowBoundaryPriorEvidence(t *testing.T) {
	for _, kind := range []string{"discovered", "run", "selection", "graph", "feature"} {
		t.Run(kind, func(t *testing.T) {
			db := newTestDB(t)
			s := testSession(t, db, 389)
			want := "bug"
			switch kind {
			case "run":
				testRun(t, db, s.ID, 12)
				want = "spike"
			case "selection":
				_, err := db.GetOrCreateModeSelection(t.Context(), ModeSelection{SessionID: s.ID, TriggerCommentID: 12, Mode: "plan", Label: "Mode: Plan"})
				if err != nil {
					t.Fatal(err)
				}
				want = "plan"
			case "graph":
				d := graphTestDefinition(t, db)
				_, err := db.CreateGraphInstance(t.Context(), GraphInstance{SessionID: s.ID, DefinitionID: d.ID, CurrentNode: "work", StartedAt: graphTestTime(), NextDueAt: graphTestTime()})
				if err != nil {
					t.Fatal(err)
				}
				want = "implement"
			case "feature":
				_, err := db.sql.ExecContext(t.Context(), "INSERT INTO managed_features(parent_task,generation,state) VALUES(?,1,'planning')", s.TaskRef)
				if err != nil {
					t.Fatal(err)
				}
				want = "plan"
			}
			got, err := db.AdmitWorkflow(t.Context(), TaskWorkflowAdmission{SessionID: s.ID, Version: 1, Family: "standalone", Workflow: "bug"})
			if err != nil || got.Workflow != want {
				t.Fatalf("prior %s: got=%+v err=%v want=%s", kind, got, err, want)
			}
			if kind == "selection" {
				if err := db.DeleteModeSelection(t.Context(), s.ID, 12); err != nil {
					t.Fatal(err)
				}
				again, err := db.GetTaskWorkflowAdmission(t.Context(), s.ID)
				if err != nil || again != got {
					t.Fatalf("journal deletion changed admission: %+v %v", again, err)
				}
			}
		})
	}
}

func TestWorkflowBoundaryCancellationAndWriteFailure(t *testing.T) {
	db := newTestDB(t)
	s := testSession(t, db, 389)
	a := TaskWorkflowAdmission{SessionID: s.ID, Version: 1, Family: "standalone", Workflow: "bug"}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := db.AdmitWorkflow(ctx, a); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled admission: %v", err)
	}
	if _, err := db.GetTaskWorkflowAdmission(t.Context(), s.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cancelled admission persisted: %v", err)
	}
	if _, err := db.sql.ExecContext(t.Context(), `CREATE TRIGGER fail_admission BEFORE INSERT ON workflow_admissions BEGIN SELECT RAISE(ABORT,'injected write failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AdmitWorkflow(t.Context(), a); err == nil {
		t.Fatal("write failure admitted route")
	}
	if _, err := db.GetTaskWorkflowAdmission(t.Context(), s.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("failed write persisted: %v", err)
	}
	if _, err := db.sql.ExecContext(t.Context(), "DROP TRIGGER fail_admission"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AdmitWorkflow(t.Context(), a); err != nil {
		t.Fatal(err)
	}
}

func TestWorkflowBoundaryAmbiguousPriorEvidenceStops(t *testing.T) {
	db := newTestDB(t)
	s := testSession(t, db, 389)
	run := testRun(t, db, s.ID, 12)
	// Historical legacy runs may have no recorded mode; absence is not proof of
	// a genuinely new task and cannot authorize conversion to Bug.
	if _, err := db.sql.ExecContext(t.Context(), "UPDATE runs SET mode='' WHERE id=?", run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AdmitWorkflow(t.Context(), TaskWorkflowAdmission{SessionID: s.ID, Version: 1, Family: "standalone", Workflow: "bug"}); err == nil {
		t.Fatal("ambiguous prior execution converted to Bug")
	}
	if _, err := db.GetTaskWorkflowAdmission(t.Context(), s.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ambiguous evidence persisted admission: %v", err)
	}
}

func TestWorkflowBoundaryWorktreeCleanupRetainsAdmission(t *testing.T) {
	fixture := gittest.New(t)
	binary, err := git.Resolve(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	clone, err := binary.Open(t.Context(), fixture.Dir)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(filepath.Dir(fixture.Dir), "bug-worktree")
	_, err = git.EnsureWorktree(t.Context(), clone, git.EnsureWorktreeOptions{Dir: dir, Branch: "lf/389-bug", BaseBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	db := newTestDB(t)
	repo, err := db.UpsertRepo(t.Context(), Repo{Path: fixture.Dir, Forge: ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: "git@github.com:acme/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: filepath.Dir(dir)})
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(t.Context(), repo, 389)
	if err != nil {
		t.Fatal(err)
	}
	want, err := db.AdmitWorkflow(t.Context(), TaskWorkflowAdmission{SessionID: session.ID, Version: 1, Family: "standalone", Workflow: "bug", SourceTrigger: 501})
	if err != nil {
		t.Fatal(err)
	}
	if err := clone.WorktreeRemove(t.Context(), dir, git.WorktreeRemoveOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("worktree not removed: %v", err)
	}
	got, err := db.AdmitWorkflow(t.Context(), TaskWorkflowAdmission{SessionID: session.ID, Version: 1, Family: "mode", Workflow: "plan", SourceTrigger: 502})
	if err != nil || got != want {
		t.Fatalf("cleanup changed retained route: %+v %v", got, err)
	}
}
