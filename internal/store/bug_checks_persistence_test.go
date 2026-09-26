package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
)

const checkPersistenceRecipe = "version: 1\nphases:\n  regression:\n    commands:\n      - argv: [echo, first]\n      - argv: [echo, second]\n  focused:\n    commands:\n      - argv: [echo, focused]\n  full:\n    commands:\n      - argv: [echo, full]\n"

func checkPersistenceBinding(t *testing.T, db *DB) BugCheckBinding {
	t.Helper()
	id := bugBoundaryIdentity(t, db, 493)
	return BugCheckBinding{SessionID: id.SessionID, Operation: "checks-1", Generation: "generation-1", Repair: "repair-1", Phase: "regression", SourceCommit: strings.Repeat("a", 40), SourceTree: strings.Repeat("b", 40), TargetCommit: strings.Repeat("c", 40), TargetRepository: "acme/api", TargetBranch: "main", RecipeDigest: config.BugRecipeDigest([]byte(checkPersistenceRecipe)), EnvironmentRevision: "env-1", PermissionRevision: "permission-1"}
}
func checkPersistenceEnsure(t *testing.T, db *DB, b BugCheckBinding) {
	t.Helper()
	if _, err := db.EnsureBugCheckPhase(t.Context(), b, []byte(checkPersistenceRecipe)); err != nil {
		t.Fatal(err)
	}
	if ok, err := db.ClaimBugWork(t.Context(), b.SessionID, b.Operation, "checks"); err != nil || !ok {
		t.Fatalf("work claim: %v %v", ok, err)
	}
}
func checkPersistencePassed() BugCheckObservation {
	zero := 0
	return BugCheckObservation{Executable: "/bin/echo", ExecutableDigest: strings.Repeat("d", 64), CWD: "/fixture", Started: time.Date(2026, 9, 25, 1, 0, 0, 0, time.UTC), Ended: time.Date(2026, 9, 25, 1, 0, 1, 0, time.UTC), Exit: &zero, Outcome: "passed", CaptureComplete: true, CleanupConfirmed: true, ProcessID: 123, ProcessIdentity: "fixture-process-start"}
}
func TestBugChecksPersistenceConcurrentClaimsAndOrderedCompletion(t *testing.T) {
	db := newTestDB(t)
	b := checkPersistenceBinding(t, db)
	checkPersistenceEnsure(t, db, b)
	if ok, err := db.ClaimBugCheckCommand(t.Context(), b.SessionID, b.Operation, 1); err != nil || ok {
		t.Fatalf("out-of-order claim: %v %v", ok, err)
	}
	var winners atomic.Int32
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := db.ClaimBugCheckCommand(t.Context(), b.SessionID, b.Operation, 0)
			if err != nil {
				t.Error(err)
			}
			if ok {
				winners.Add(1)
			}
		}()
	}
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatalf("launch claims=%d", winners.Load())
	}
	if ok, err := db.ClaimBugWork(t.Context(), b.SessionID, "fixer", "repair"); err != nil || ok {
		t.Fatalf("overlapping work: %v %v", ok, err)
	}
	observation := checkPersistencePassed()
	if err := db.ObserveBugCheckCommand(t.Context(), b.SessionID, b.Operation, 0, true, observation); err != nil {
		t.Fatal(err)
	}
	if err := db.ObserveBugCheckCommand(t.Context(), b.SessionID, b.Operation, 0, true, observation); err != nil {
		t.Fatal(err)
	}
	changed := observation
	changed.Output = "conflicting completion"
	if err := db.ObserveBugCheckCommand(t.Context(), b.SessionID, b.Operation, 0, true, changed); err == nil {
		t.Fatal("replaced completed command")
	}
	if err := db.FinishBugCheckPhase(t.Context(), b.SessionID, b.Operation, "passed", "", true); err == nil {
		t.Fatal("incomplete phase passed")
	}
	if ok, err := db.ClaimBugCheckCommand(t.Context(), b.SessionID, b.Operation, 1); err != nil || !ok {
		t.Fatalf("second claim: %v %v", ok, err)
	}
	if err := db.ObserveBugCheckCommand(t.Context(), b.SessionID, b.Operation, 1, true, observation); err != nil {
		t.Fatal(err)
	}
	if err := db.FinishBugCheckPhase(t.Context(), b.SessionID, b.Operation, "passed", "", false); err == nil {
		t.Fatal("stale phase passed")
	}
	if err := db.FinishBugCheckPhase(t.Context(), b.SessionID, b.Operation, "passed", "", true); err != nil {
		t.Fatal(err)
	}
	if err := db.InvalidateBugCheckPhase(t.Context(), b.SessionID, b.Operation, "target moved"); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetBugCheckPhase(t.Context(), b.SessionID, b.Operation)
	if err != nil {
		t.Fatal(err)
	}
	if got.Current || got.Outcome != "passed" || len(got.Commands) != 2 {
		t.Fatalf("lost history/invalidation: %+v", got)
	}
}
func TestBugChecksPersistenceRestartCancellationAndIsolation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "checks.db")
	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	b := checkPersistenceBinding(t, db)
	checkPersistenceEnsure(t, db, b)
	if ok, err := db.ClaimBugCheckCommand(t.Context(), b.SessionID, b.Operation, 0); err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	if err = db.CancelBugCheckPhase(t.Context(), b.SessionID, b.Operation); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.EnsureBugCheckPhase(t.Context(), b, []byte(checkPersistenceRecipe)); err != nil {
		t.Fatal(err)
	}
	if ok, err := db.ClaimBugCheckCommand(t.Context(), b.SessionID, b.Operation, 0); err != nil || ok {
		t.Fatalf("uncertain/cancelled launch repeated: %v %v", ok, err)
	}
	if err = db.FinishBugCheckPhase(t.Context(), b.SessionID, b.Operation, "cancelled", "owner cancelled", true); err == nil {
		t.Fatal("unconfirmed cleanup became cancelled")
	}
	conflict := b
	conflict.Generation = "different"
	if _, err = db.EnsureBugCheckPhase(t.Context(), conflict, []byte(checkPersistenceRecipe)); err == nil {
		t.Fatal("conflicting replay accepted")
	}
	other := bugBoundaryIdentity(t, db, 494)
	if _, err = db.GetBugCheckPhase(t.Context(), other.SessionID, b.Operation); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-task record leaked: %v", err)
	}
	observation := checkPersistencePassed()
	observation.Outcome = "cancelled"
	observation.Reason = "termination observed"
	if err = db.ObserveBugCheckCommand(t.Context(), b.SessionID, b.Operation, 0, true, observation); err != nil {
		t.Fatal(err)
	}
	if err = db.FinishBugCheckPhase(t.Context(), b.SessionID, b.Operation, "cancelled", "owner cancelled", true); err != nil {
		t.Fatal(err)
	}
	if ok, err := db.ClaimBugCheckCommand(t.Context(), b.SessionID, b.Operation, 1); err != nil || ok {
		t.Fatalf("launched after cancellation: %v %v", ok, err)
	}
	state, _, _, err := db.CurrentBugAnalysis(t.Context(), b.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if state.AnalysisUsed != 0 {
		t.Fatal("checks consumed analysis budget")
	}
}
func TestBugChecksPersistenceReliabilitySurvivesGeneration(t *testing.T) {
	db := newTestDB(t)
	b := checkPersistenceBinding(t, db)
	checkPersistenceEnsure(t, db, b)
	if err := db.FlagBugCheckUnreliable(t.Context(), b.SessionID, b.Operation, "known flaky"); err != nil {
		t.Fatal(err)
	}
	next := b
	next.Operation = "checks-2"
	next.Generation = "generation-2"
	got, err := db.EnsureBugCheckPhase(t.Context(), next, []byte(checkPersistenceRecipe))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Unreliable {
		t.Fatal("new generation erased known flakiness")
	}
	if ok, err := db.ClaimBugCheckCommand(t.Context(), next.SessionID, next.Operation, 0); err != nil || ok {
		t.Fatalf("flaky launch admitted: %v %v", ok, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = db.EnsureBugCheckPhase(ctx, next, []byte(checkPersistenceRecipe)); err == nil {
		t.Fatal("cancelled persistence succeeded")
	}
}

func TestBugChecksPersistenceRejectsIncompleteSuccessAndStopsFailure(t *testing.T) {
	db := newTestDB(t)
	b := checkPersistenceBinding(t, db)
	checkPersistenceEnsure(t, db, b)
	if ok, err := db.ClaimBugCheckCommand(t.Context(), b.SessionID, b.Operation, 0); err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	for _, field := range []string{"exit", "capture", "cleanup", "executable", "process", "start", "end"} {
		observation := checkPersistencePassed()
		switch field {
		case "exit":
			observation.Exit = nil
		case "capture":
			observation.CaptureComplete = false
		case "cleanup":
			observation.CleanupConfirmed = false
		case "executable":
			observation.ExecutableDigest = ""
		case "process":
			observation.ProcessIdentity = ""
		case "start":
			observation.Started = time.Time{}
		case "end":
			observation.Ended = observation.Started.Add(-time.Second)
		}
		if err := db.ObserveBugCheckCommand(t.Context(), b.SessionID, b.Operation, 0, true, observation); err == nil {
			t.Errorf("missing %s accepted as success", field)
		}
	}
	observation := checkPersistencePassed()
	one := 1
	observation.Exit = &one
	observation.Outcome = "failed"
	observation.Reason = "assertion failed"
	if err := db.ObserveBugCheckCommand(t.Context(), b.SessionID, b.Operation, 0, true, observation); err != nil {
		t.Fatal(err)
	}
	if ok, err := db.ClaimBugCheckCommand(t.Context(), b.SessionID, b.Operation, 1); err != nil || ok {
		t.Fatalf("launch after failure: %v %v", ok, err)
	}
	if err := db.FinishBugCheckPhase(t.Context(), b.SessionID, b.Operation, "passed", "", true); err == nil {
		t.Fatal("failed command certified passing")
	}
	if err := db.FinishBugCheckPhase(t.Context(), b.SessionID, b.Operation, "failed", "assertion failed", true); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetBugCheckPhase(t.Context(), b.SessionID, b.Operation)
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != "failed" || len(got.Commands) != 1 {
		t.Fatalf("failure evidence changed: %+v", got)
	}
}
