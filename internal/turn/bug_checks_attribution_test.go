package turn

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/bugchecks"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/store"
)

const attributionRerunRecipe = "version: 1\nphases:\n  regression:\n    commands:\n      - argv: [go, test, ./...]\n        rerun:\n          failures: go-test\n          argv: [go, test, -count=1, -run, '{tests}', '{packages}']\n          timeout_seconds: 60\n  focused:\n    commands:\n      - argv: [echo, focused]\n  full:\n    commands:\n      - argv: [echo, full]\n"

const attributionPlainRecipe = "version: 1\nphases:\n  regression:\n    commands:\n      - argv: [go, test, ./...]\n  focused:\n    commands:\n      - argv: [echo, focused]\n  full:\n    commands:\n      - argv: [echo, full]\n"

// attributionCall is one command the fake isolation was asked to run.
type attributionCall struct {
	root string
	argv []string
}

// attributionFixture binds a real task worktree and store to a scripted
// isolation runner; respond answers each call with an exit code and output.
type attributionFixture struct {
	db      *store.DB
	in      BugChecksInput
	h       *BugChecksActivity
	calls   []attributionCall
	bases   []string
	cleaned []string
}

func newAttributionFixture(t *testing.T, recipe string, respond func(call int, argv []string) (int, string)) *attributionFixture {
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
	repoRow, err := db.UpsertRepo(t.Context(), store.Repo{Path: root, Forge: store.ForgeGitHub, Owner: "example", Name: "bugs", RemoteURL: "git@github.com:example/bugs.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: filepath.Join(root, "worktrees")})
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(t.Context(), repoRow, 493)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.AdmitWorkflow(t.Context(), store.TaskWorkflowAdmission{SessionID: session.ID, Version: 1, Family: "standalone", Workflow: "bug"}); err != nil {
		t.Fatal(err)
	}
	if err = db.EnsureBugRecord(t.Context(), store.BugIdentity{SessionID: session.ID, Repository: "example/bugs", IssueID: 10493, IssueNumber: 493, OwnerID: 42}); err != nil {
		t.Fatal(err)
	}
	repo := gittest.New(t)
	w := newSyncWorktree(t, repo, 493, "attribution")
	commit := repo.GitIn(w.Path(), "rev-parse", "HEAD")
	tree := repo.GitIn(w.Path(), "rev-parse", "HEAD^{tree}")
	launcher := filepath.Join(root, "launcher")
	if err = os.WriteFile(launcher, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	b := store.BugCheckBinding{SessionID: session.ID, Operation: "phase", Generation: "generation", Repair: "repair", Phase: "regression", SourceCommit: commit, SourceTree: tree, TargetCommit: repo.Head(), TargetRepository: "example/bugs", TargetBranch: "main", RecipeDigest: config.BugRecipeDigest([]byte(recipe)), EnvironmentRevision: "environment", PermissionRevision: "permission"}
	if _, err = db.EnsureBugCheckPhase(t.Context(), b, []byte(recipe)); err != nil {
		t.Fatal(err)
	}
	f := &attributionFixture{db: db}
	binary, err := git.Resolve(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	clone, err := binary.Open(t.Context(), repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	f.in = BugChecksInput{Binding: b, Worktree: w, Isolation: bugchecks.Isolation{Binary: launcher, PATH: "/usr/bin:/bin", Home: root, Revision: "permission"}, Observe: func(context.Context, store.BugCheckBinding) error { return nil },
		Base: func(ctx context.Context, target string) (*git.Worktree, func(context.Context) error, error) {
			dir := filepath.Join(t.TempDir(), "target")
			base, e := clone.WorktreeAdd(ctx, dir, git.WorktreeAddOptions{Branch: target, Detach: true})
			if e != nil {
				return nil, nil, e
			}
			f.bases = append(f.bases, base.Path())
			return base, func(ctx context.Context) error {
				f.cleaned = append(f.cleaned, base.Path())
				return clone.WorktreeRemove(ctx, base.Path(), git.WorktreeRemoveOptions{Force: true})
			}, nil
		}}
	f.h = &BugChecksActivity{run: func(_ context.Context, root string, c config.BugCheckCommand, started func(bugchecks.CommandResult) error) bugchecks.CommandResult {
		f.calls = append(f.calls, attributionCall{root, append([]string(nil), c.Argv...)})
		begin := time.Now().UTC()
		out := bugchecks.CommandResult{ExecutableDigest: strings.Repeat("d", 64), Executable: "/usr/bin/" + c.Argv[0], CWD: root, Started: begin, ProcessID: 100 + len(f.calls), ProcessIdentity: "fixture-process"}
		if err := started(out); err != nil {
			t.Errorf("launch observation refused: %v", err)
		}
		exit, output := respond(len(f.calls)-1, c.Argv)
		out.Ended, out.Exit, out.Output, out.CaptureComplete, out.CleanupConfirmed = begin.Add(time.Second), &exit, output, true, true
		switch exit {
		case 0:
			out.Outcome = "passed"
		case 1:
			out.Outcome, out.Reason = "failed", "project check reported test failure"
		default:
			out.Outcome, out.Reason = "blocked", "check infrastructure failure or signal"
		}
		return out
	}}
	return f
}

func (f *attributionFixture) runPhase(t *testing.T) (ActivityResult, store.BugCheckPhaseRecord) {
	t.Helper()
	result, err := f.h.RunPhase(t.Context(), f.db, f.in)
	if err != nil {
		t.Fatal(err)
	}
	record, err := f.db.GetBugCheckPhase(t.Context(), f.in.Binding.SessionID, f.in.Binding.Operation)
	if err != nil {
		t.Fatal(err)
	}
	var out BugChecksOutput
	if err = json.Unmarshal(result.Output, &out); err != nil {
		t.Fatal(err)
	}
	if out.Reason != record.Reason || result.Outcome != record.Outcome {
		t.Fatalf("activity %s %q disagrees with retained phase %s %q", result.Outcome, out.Reason, record.Outcome, record.Reason)
	}
	if ok, err := f.db.ClaimBugWork(t.Context(), f.in.Binding.SessionID, "after", "repair"); err != nil || !ok {
		t.Fatalf("check guard not released: %v %v", ok, err)
	}
	return result, record
}

const (
	attributionFailAB = "--- FAIL: TestA (0.01s)\n--- FAIL: TestB (0.00s)\n    --- FAIL: TestB/sub (0.00s)\nFAIL\nFAIL\texample.com/p\t0.1s\nok  \texample.com/q\t0.1s\nFAIL\n"
	attributionFailB  = "--- FAIL: TestB (0.00s)\nFAIL\nFAIL\texample.com/p\t0.1s\nFAIL\n"
	attributionPassP  = "ok  \texample.com/p\t0.1s\n"
)

func TestBugCheckAttributionFlakyOnlyPasses(t *testing.T) {
	f := newAttributionFixture(t, attributionRerunRecipe, func(call int, _ []string) (int, string) {
		if call == 0 {
			return 1, attributionFailAB
		}
		return 0, attributionPassP
	})
	result, record := f.runPhase(t)
	want := BugKnownFailuresPrefix + "flaky TestA (example.com/p), TestB (example.com/p)"
	if result.Outcome != "passed" || record.Reason != want || !record.Current {
		t.Fatalf("phase = %s %q, want passed %q", result.Outcome, record.Reason, want)
	}
	if len(f.calls) != 2 || len(f.bases) != 0 {
		t.Fatalf("calls %+v bases %v; want one repair rerun and no target", f.calls, f.bases)
	}
	if got := strings.Join(f.calls[1].argv, " "); got != "go test -count=1 -run ^(TestA|TestB)$ example.com/p" || f.calls[1].root != f.in.Worktree.Path() {
		t.Fatalf("repair rerun = %q in %s", got, f.calls[1].root)
	}
	c := record.Commands[0]
	if c.Observation.Outcome != "failed" || c.Observation.Output != attributionFailAB || *c.Observation.Exit != 1 {
		t.Fatalf("original failure not retained: %+v", c.Observation)
	}
	if !c.Passed() || c.Attribution == nil || len(c.Attribution.Reruns) != 1 || c.Attribution.Reruns[0].Target != "repair" || c.Attribution.Reruns[0].Observation.Output != attributionPassP {
		t.Fatalf("attribution evidence = %+v", c.Attribution)
	}
	if row := bugCheckRow("regression", record); !strings.Contains(row, "| regression | passed with known failures: flaky TestA (example.com/p), TestB (example.com/p) | 1 |") {
		t.Fatalf("review row hides known failures: %s", row)
	}
}

func TestBugCheckAttributionPreExistingPasses(t *testing.T) {
	f := newAttributionFixture(t, attributionRerunRecipe, func(call int, _ []string) (int, string) {
		if call == 0 {
			return 1, attributionFailAB
		}
		return 1, attributionFailB
	})
	result, record := f.runPhase(t)
	want := BugKnownFailuresPrefix + "flaky TestA (example.com/p); pre-existing TestB (example.com/p)"
	if result.Outcome != "passed" || record.Reason != want {
		t.Fatalf("phase = %s %q, want passed %q", result.Outcome, record.Reason, want)
	}
	if len(f.calls) != 3 || len(f.bases) != 1 || f.calls[2].root != f.bases[0] || strings.Join(f.calls[2].argv, " ") != "go test -count=1 -run ^(TestB)$ example.com/p" {
		t.Fatalf("target rerun calls %+v bases %v", f.calls, f.bases)
	}
	if len(f.cleaned) != 1 {
		t.Fatalf("target worktree not cleaned up: %v", f.cleaned)
	}
	if _, err := os.Stat(f.bases[0]); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target worktree survived: %v", err)
	}
	a := record.Commands[0].Attribution
	if a == nil || len(a.Reruns) != 2 || a.Reruns[1].Target != "target" || len(a.PreExisting) != 1 || len(a.Flaky) != 1 || len(a.Repair) != 0 {
		t.Fatalf("attribution = %+v", a)
	}
	e := store.BugDeliveryEvidence{}
	if lead := bugDeliveryLead(7, e, "regression"); !strings.Contains(lead, "with known failures the repair did not cause") || !strings.Contains(lead, "regression passed with known failures") {
		t.Fatalf("delivery lead hides known failures:\n%s", lead)
	}
}

func TestBugCheckAttributionRepairFailureFails(t *testing.T) {
	f := newAttributionFixture(t, attributionRerunRecipe, func(call int, _ []string) (int, string) {
		switch call {
		case 0:
			return 1, attributionFailAB
		case 1:
			return 1, attributionFailB
		}
		return 0, attributionPassP
	})
	result, record := f.runPhase(t)
	if result.Outcome != "failed" || !strings.Contains(record.Reason, "failing with the repair but passing on main: TestB (example.com/p)") || !strings.Contains(record.Reason, "also flaky TestA (example.com/p)") {
		t.Fatalf("phase = %s %q", result.Outcome, record.Reason)
	}
	if c := record.Commands[0]; c.Passed() || c.Attribution.Outcome != "failed" || len(c.Attribution.Repair) != 1 {
		t.Fatalf("repair failure counted as passing: %+v", c.Attribution)
	}
	if len(f.cleaned) != 1 {
		t.Fatalf("target worktree not cleaned up: %v", f.cleaned)
	}
}

func TestBugCheckAttributionUnattributableFailsAsBefore(t *testing.T) {
	for name, output := range map[string]string{
		"build failure": "# example.com/p\np.go:1:1: expected 'package'\nFAIL\texample.com/p [build failed]\nFAIL\n",
		"timeout":       "panic: test timed out after 10m0s\n--- FAIL: TestA (600.00s)\nFAIL\texample.com/p\t600.1s\n",
		"unnamed":       "FAIL\texample.com/p\t0.1s\n",
	} {
		t.Run(name, func(t *testing.T) {
			f := newAttributionFixture(t, attributionRerunRecipe, func(int, []string) (int, string) { return 1, output })
			result, record := f.runPhase(t)
			if result.Outcome != "failed" || !strings.HasPrefix(record.Reason, "project check reported test failure (not attributed to named tests: ") || len(f.calls) != 1 {
				t.Fatalf("phase = %s %q after %d calls", result.Outcome, record.Reason, len(f.calls))
			}
			if c := record.Commands[0]; c.Observation.Output != output || c.Passed() {
				t.Fatalf("unattributed failure changed: %+v", c)
			}
		})
	}
	t.Run("rerun names nothing", func(t *testing.T) {
		f := newAttributionFixture(t, attributionRerunRecipe, func(call int, _ []string) (int, string) {
			if call == 0 {
				return 1, attributionFailB
			}
			return 0, "ok  \texample.com/p\t0.1s [no tests to run]\n"
		})
		result, record := f.runPhase(t)
		if result.Outcome != "failed" || !strings.Contains(record.Reason, "ran no tests") {
			t.Fatalf("phase = %s %q", result.Outcome, record.Reason)
		}
	})
	t.Run("blocked command never reruns", func(t *testing.T) {
		f := newAttributionFixture(t, attributionRerunRecipe, func(int, []string) (int, string) { return 2, attributionFailB })
		result, record := f.runPhase(t)
		if result.Outcome != "blocked" || record.Reason != "check infrastructure failure or signal" || len(f.calls) != 1 || record.Commands[0].Attribution != nil {
			t.Fatalf("phase = %s %q after %d calls", result.Outcome, record.Reason, len(f.calls))
		}
	})
}

// An interrupted rerun keeps the guard until its own process is observed
// stopped; recovery then ends it and settles the command's original failure.
func TestBugCheckAttributionInterruptedRerunRecovers(t *testing.T) {
	f := newAttributionFixture(t, attributionRerunRecipe, nil)
	db, b, ctx := f.db, f.in.Binding, t.Context()
	if ok, err := db.ClaimBugWork(ctx, b.SessionID, b.Operation, "checks"); err != nil || !ok {
		t.Fatalf("work claim: %v %v", ok, err)
	}
	if ok, err := db.ClaimBugCheckCommand(ctx, b.SessionID, b.Operation, 0); err != nil || !ok {
		t.Fatalf("command claim: %v %v", ok, err)
	}
	exit := 1
	failed := store.BugCheckObservation{ExecutableDigest: strings.Repeat("d", 64), Executable: "/usr/bin/go", CWD: "/fixture", Started: time.Now().UTC().Add(-time.Minute), Ended: time.Now().UTC(), Exit: &exit, Outcome: "failed", Reason: "project check reported test failure", Output: attributionFailB, CaptureComplete: true, CleanupConfirmed: true, ProcessID: 10, ProcessIdentity: "command-process"}
	for _, finished := range []bool{false, true} {
		if err := db.ObserveBugCheckCommand(ctx, b.SessionID, b.Operation, 0, finished, failed); err != nil {
			t.Fatal(err)
		}
	}
	if ok, err := db.ClaimBugCheckRerun(ctx, b.SessionID, b.Operation, 0, "target", []store.BugCheckTest{{Package: "example.com/p", Name: "TestB"}}); err != nil || ok {
		t.Fatalf("target rerun claimed before the repair rerun: %v %v", ok, err)
	}
	if ok, err := db.ClaimBugCheckRerun(ctx, b.SessionID, b.Operation, 0, "repair", []store.BugCheckTest{{Package: "example.com/p", Name: "TestB"}}); err != nil || !ok {
		t.Fatalf("rerun claim: %v %v", ok, err)
	}
	if err := db.ObserveBugCheckRerun(ctx, b.SessionID, b.Operation, 0, false, store.BugCheckObservation{Started: time.Now().UTC(), ProcessID: 11, ProcessIdentity: "rerun-process"}); err != nil {
		t.Fatal(err)
	}
	var observed []string
	f.h.ObserveStopped = func(_ context.Context, identity string) (bool, error) {
		observed = append(observed, identity)
		return true, nil
	}
	phase, quiescent, err := f.h.Reconcile(ctx, db, b.SessionID, b.Operation)
	if err != nil || !quiescent {
		t.Fatalf("reconcile: %v %v", quiescent, err)
	}
	if len(observed) != 1 || observed[0] != "rerun-process" {
		t.Fatalf("recovery observed %v, want the rerun's process", observed)
	}
	c := phase.Commands[0]
	if phase.Outcome != "failed" || phase.Reason != failed.Reason || c.Observation.Output != attributionFailB || c.RerunActive() || c.Attribution.Reruns[0].Observation.Outcome != "blocked" {
		t.Fatalf("recovered phase = %s %q %+v", phase.Outcome, phase.Reason, c)
	}
}

// Cancellation during a rerun ends the phase cancelled, with the rerun still
// observed to its end and no target rerun launched.
func TestBugCheckAttributionCancelledRerun(t *testing.T) {
	var f *attributionFixture
	f = newAttributionFixture(t, attributionRerunRecipe, func(call int, _ []string) (int, string) {
		if call == 0 {
			return 1, attributionFailB
		}
		if err := f.h.Cancel(t.Context(), f.db, f.in.Binding.SessionID, f.in.Binding.Operation); err != nil {
			t.Error(err)
		}
		return 0, attributionPassP
	})
	inner := f.h.run
	f.h.run = func(ctx context.Context, root string, c config.BugCheckCommand, started func(bugchecks.CommandResult) error) bugchecks.CommandResult {
		out := inner(ctx, root, c, started)
		if ctx.Err() != nil {
			out.Outcome, out.Reason = "cancelled", ctx.Err().Error()
		}
		return out
	}
	result, record := f.runPhase(t)
	if result.Outcome != "cancelled" || len(f.calls) != 2 || len(f.bases) != 0 {
		t.Fatalf("phase = %s %q after %d calls", result.Outcome, record.Reason, len(f.calls))
	}
	if c := record.Commands[0]; c.RerunActive() || c.Passed() || c.Attribution.Reruns[0].Observation.Outcome != "cancelled" {
		t.Fatalf("cancelled rerun evidence = %+v", c.Attribution)
	}
}

func TestBugCheckAttributionAbsentWithoutRerun(t *testing.T) {
	f := newAttributionFixture(t, attributionPlainRecipe, func(int, []string) (int, string) { return 1, attributionFailB })
	result, record := f.runPhase(t)
	if result.Outcome != "failed" || record.Reason != "project check reported test failure" || len(f.calls) != 1 || record.Commands[0].Attribution != nil {
		t.Fatalf("phase = %s %q after %d calls: %+v", result.Outcome, record.Reason, len(f.calls), record.Commands[0])
	}
	pass := newAttributionFixture(t, attributionPlainRecipe, func(int, []string) (int, string) { return 0, attributionPassP })
	if result, record = pass.runPhase(t); result.Outcome != "passed" || record.Reason != "" {
		t.Fatalf("passing phase = %s %q", result.Outcome, record.Reason)
	}
}
