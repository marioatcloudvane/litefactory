package turn

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/store"
)

func checkRecoveryFixture(t *testing.T) (*store.DB, store.BugCheckBinding) {
	t.Helper()
	root := t.TempDir()
	db, err := store.Open(t.Context(), filepath.Join(root, "checks.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: root, Forge: store.ForgeGitHub, Owner: "example", Name: "bugs", RemoteURL: "git@github.com:example/bugs.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: filepath.Join(root, "worktrees")})
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(t.Context(), repo, 493)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.AdmitWorkflow(t.Context(), store.TaskWorkflowAdmission{SessionID: session.ID, Version: 1, Family: "standalone", Workflow: "bug"}); err != nil {
		t.Fatal(err)
	}
	if err = db.EnsureBugRecord(t.Context(), store.BugIdentity{SessionID: session.ID, Repository: "example/bugs", IssueID: 10493, IssueNumber: 493, OwnerID: 42}); err != nil {
		t.Fatal(err)
	}
	recipe := []byte("version: 1\nphases:\n  regression:\n    commands:\n      - argv: [echo, regression]\n  focused:\n    commands:\n      - argv: [echo, focused]\n  full:\n    commands:\n      - argv: [echo, full]\n")
	b := store.BugCheckBinding{SessionID: session.ID, Operation: "phase", Generation: "generation", Repair: "repair", Phase: "regression", SourceCommit: strings.Repeat("a", 40), SourceTree: strings.Repeat("b", 40), TargetCommit: strings.Repeat("c", 40), TargetRepository: "example/bugs", TargetBranch: "main", RecipeDigest: config.BugRecipeDigest(recipe), EnvironmentRevision: "environment", PermissionRevision: "permission"}
	if _, err = db.EnsureBugCheckPhase(t.Context(), b, recipe); err != nil {
		t.Fatal(err)
	}
	if ok, err := db.ClaimBugWork(t.Context(), session.ID, b.Operation, "checks"); err != nil || !ok {
		t.Fatalf("work claim: %v %v", ok, err)
	}
	if ok, err := db.ClaimBugCheckCommand(t.Context(), session.ID, b.Operation, 0); err != nil || !ok {
		t.Fatalf("command claim: %v %v", ok, err)
	}
	return db, b
}
func checkRecoveryLaunch(t *testing.T, db *store.DB, b store.BugCheckBinding) store.BugCheckObservation {
	t.Helper()
	o := store.BugCheckObservation{Executable: "/fixture/command", ExecutableDigest: strings.Repeat("d", 64), CWD: "/fixture", Started: time.Now().UTC().Add(-time.Minute), ProcessID: 123, ProcessIdentity: "controlled-process-start"}
	if err := db.ObserveBugCheckCommand(t.Context(), b.SessionID, b.Operation, 0, false, o); err != nil {
		t.Fatal(err)
	}
	return o
}
func TestBugCheckRecoveryUnknownLaunchAndObserverFailureRemainUnresolved(t *testing.T) {
	for _, scenario := range []string{"unknown", "running", "observer-error", "active"} {
		t.Run(scenario, func(t *testing.T) {
			db, b := checkRecoveryFixture(t)
			calls := 0
			probeErr := errors.New("process state unreadable")
			h := BugChecksActivity{ObserveStopped: func(context.Context, string) (bool, error) {
				calls++
				if scenario == "observer-error" {
					return false, probeErr
				}
				return false, nil
			}}
			if scenario != "unknown" {
				checkRecoveryLaunch(t, db, b)
			}
			if scenario == "active" {
				h.active = map[string]context.CancelFunc{bugCheckKey(b.SessionID, b.Operation): func() {}}
			}
			got, quiescent, err := h.Reconcile(t.Context(), db, b.SessionID, b.Operation)
			if scenario == "observer-error" {
				if !errors.Is(err, probeErr) {
					t.Fatalf("lost observer cause: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if quiescent || got.Cleanup || got.Outcome != "pending" {
				t.Fatalf("unknown execution progressed: %+v %v", got, quiescent)
			}
			if (scenario == "unknown" || scenario == "active") && calls != 0 {
				t.Fatal("observer called without recoverable launch")
			}
			if ok, err := db.ClaimBugWork(t.Context(), b.SessionID, "fixer", "repair"); err != nil || ok {
				t.Fatalf("unresolved guard released: %v %v", ok, err)
			}
			if ok, err := db.ClaimBugCheckCommand(t.Context(), b.SessionID, b.Operation, 0); err != nil || ok {
				t.Fatalf("uncertain command relaunched: %v %v", ok, err)
			}
		})
	}
}
func TestBugCheckRecoveryStoppedInterruptionAndCancellation(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		name := "interrupted"
		if cancelled {
			name = "cancelled"
		}
		t.Run(name, func(t *testing.T) {
			db, b := checkRecoveryFixture(t)
			o := checkRecoveryLaunch(t, db, b)
			if cancelled {
				if err := db.CancelBugCheckPhase(t.Context(), b.SessionID, b.Operation); err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			h := BugChecksActivity{ObserveStopped: func(_ context.Context, identity string) (bool, error) {
				calls++
				if identity != o.ProcessIdentity {
					t.Fatalf("wrong process identity: %q", identity)
				}
				return true, nil
			}}
			want := "blocked"
			if cancelled {
				want = "cancelled"
			}
			for range 2 {
				got, quiescent, err := h.Reconcile(t.Context(), db, b.SessionID, b.Operation)
				if err != nil {
					t.Fatal(err)
				}
				if !quiescent || !got.Cleanup || got.Outcome != want || got.Commands[0].Observation.Exit != nil {
					t.Fatalf("incorrect recovery: %+v %v", got, quiescent)
				}
			}
			if calls != 1 {
				t.Fatalf("repeated cleanup observation: %d", calls)
			}
			if ok, err := db.ClaimBugWork(t.Context(), b.SessionID, "new-work", "repair"); err != nil || !ok {
				t.Fatalf("quiescent guard not released: %v %v", ok, err)
			}
			if _, _, err := h.Reconcile(t.Context(), db, b.SessionID, b.Operation); err != nil {
				t.Fatal(err)
			}
			if ok, err := db.ClaimBugWork(t.Context(), b.SessionID, "other-work", "checks"); err != nil || ok {
				t.Fatalf("replay removed another owner: %v %v", ok, err)
			}
		})
	}
}
func TestBugCheckRecoveryCompletedReplayRequiresFreshPassingGuards(t *testing.T) {
	db, b := checkRecoveryFixture(t)
	o := checkRecoveryLaunch(t, db, b)
	zero := 0
	o.Exit = &zero
	o.Ended = time.Now().UTC()
	o.Outcome = "passed"
	o.CaptureComplete = true
	o.CleanupConfirmed = true
	if err := db.ObserveBugCheckCommand(t.Context(), b.SessionID, b.Operation, 0, true, o); err != nil {
		t.Fatal(err)
	}
	h := BugChecksActivity{ObserveStopped: func(context.Context, string) (bool, error) {
		t.Fatal("completed process probed again")
		return false, nil
	}}
	for range 2 {
		got, quiescent, err := h.Reconcile(t.Context(), db, b.SessionID, b.Operation)
		if err != nil {
			t.Fatal(err)
		}
		if !quiescent || got.Outcome != "pending" || len(got.Commands) != 1 || got.Commands[0].Observation.Outcome != "passed" {
			t.Fatalf("recovery inferred phase pass or lost completion: %+v", got)
		}
	}
	if err := db.FinishBugCheckPhase(t.Context(), b.SessionID, b.Operation, "passed", "", true); err != nil {
		t.Fatal(err)
	}
	got, quiescent, err := h.Reconcile(t.Context(), db, b.SessionID, b.Operation)
	if err != nil || !quiescent || got.Outcome != "passed" {
		t.Fatalf("terminal replay: %+v %v %v", got, quiescent, err)
	}
}
