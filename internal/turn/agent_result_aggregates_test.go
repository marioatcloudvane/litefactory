package turn

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	ar "github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/store"
)

type aggregateTurnData struct {
	fixture     *fixture
	source      *SubmissionSession
	options     SubmissionSessionOptions
	worktree    *git.Worktree
	binding     store.ResultBinding
	base        ar.Context
	selected    []store.ResultStoryRevision
	checkpoints []ResultEngineeringCheckpoint
	capture     agent.AggregateCapture
}

func executedAggregateFixture(t *testing.T, workflow int) aggregateTurnData {
	t.Helper()
	f, _, source, o := submissionSessionFixture(t, workflow)
	binary, err := git.Resolve(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	clone, err := binary.Open(t.Context(), f.repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	w, err := clone.WorktreeAdd(t.Context(), filepath.Join(filepath.Dir(f.repo.Dir), "aggregate-worktree"), git.WorktreeAddOptions{Branch: "lf/aggregate-fixture", CreateFrom: "HEAD"})
	if err != nil {
		t.Fatal(err)
	}
	v := aggregateTurnData{fixture: f, source: source, options: o, worktree: w, base: ar.Context{SchemaVersion: 1, WorkflowVersion: workflow, Mode: "implement", Stories: map[string]agent.ContributionStory{}}}
	files := []agent.StorySnapshotFile{}
	for _, key := range []string{"US-1", "US-2"} {
		stage := "engineering"
		if workflow == 1 {
			stage = "legacy"
		}
		c := capturedContribution(t, o.Worktree, key, stage)
		response := callContribution(t, source, "submit", c)
		var receipt store.SubmissionReceipt
		if err := json.Unmarshal(response.Result, &receipt); err != nil {
			t.Fatal(err)
		}
		if receipt.EngineeringState != "recorded" {
			t.Fatalf("prior engineering not recorded: %+v", receipt)
		}
		v.checkpoints = append(v.checkpoints, ResultEngineeringCheckpoint{Story: key, RunID: o.RunID, Sequence: receipt.Sequence})
		v.base.Stories[key] = source.scope.Stories[key]
		v.selected = append(v.selected, store.ResultStoryRevision{Story: key, Revision: source.scope.Stories[key].Revision})
		evidence, err := c.Capture.SnapshotEvidence()
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range evidence.Files {
			if err := os.WriteFile(filepath.Join(w.Path(), file.Path), file.Content, 0600); err != nil {
				t.Fatal(err)
			}
			files = append(files, file)
		}
	}
	for path, content := range map[string]string{"go.mod": "module aggregatefixture\n\ngo 1.26\n", "calc.go": "package sample\nfunc Sum(a,b int)int{return a+b}\n", "calc_test.go": "package sample\nimport \"testing\"\nfunc TestSum(t *testing.T){if Sum(2,3)!=5{t.Fatal(\"wrong sum\")}}\n"} {
		data := []byte(content)
		if err := os.WriteFile(filepath.Join(w.Path(), path), data, 0600); err != nil {
			t.Fatal(err)
		}
		files = append(files, agent.StorySnapshotFile{Path: path, Content: data, SHA256: agent.StoryDigest(data)})
	}
	slices.SortFunc(files, func(a, b agent.StorySnapshotFile) int { return strings.Compare(a.Path, b.Path) })
	v.binding, err = f.db.BindResultAttempt(t.Context(), store.ResultAttempt{Task: f.session.TaskRef, Mode: "implement", WorkContext: "aggregate-work", RunID: o.RunID, Attempt: "aggregate-attempt", SchemaVersion: 1, WorkflowVersion: workflow, SubmissionVersion: 2, FrozenStories: v.selected})
	if err != nil {
		t.Fatal(err)
	}
	output := runAggregateFixtureCheck(t, w)
	v.capture, err = agent.NewAggregateCapture(files, []agent.AggregateCheckClaim{{ID: "aggregate-check-1", ExecutionID: "aggregate-execution-1", Scope: "aggregate", Name: "go test -count=1 ./...", Executed: true, Passed: true, Evidence: output, ImplementationDigest: agent.SnapshotDigest(files)}})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("executed fixture aggregate: run=%d attempt=%s selected=%v digest=%s output=%s", v.binding.RunID, v.binding.Attempt, v.selected, v.capture.ImplementationDigest, output)
	return v
}
func runAggregateFixtureCheck(t *testing.T, w *git.Worktree) string {
	t.Helper()
	command := exec.CommandContext(t.Context(), "go", "test", "-count=1", "./...")
	command.Dir = w.Path()
	command.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("actual aggregate fixture check: %v\n%s", err, output)
	}
	return strings.TrimSpace(string(output))
}
func aggregateOnlyResult(t *testing.T, reference string, checkIDs []string) ar.Result {
	t.Helper()
	ref, err := agent.AggregateReference(reference, checkIDs)
	if err != nil {
		t.Fatal(err)
	}
	return ar.Result{Outcome: "completed", Comment: "Selected engineering and final aggregate checks are retained.", Payload: ar.Payload{Implement: &ar.Implement{AggregateChecks: &ref}}}
}
func TestResultAggregateWorktree_CompletionAndRecovery(t *testing.T) {
	for _, workflow := range []int{1, 2} {
		t.Run(map[int]string{1: "legacy", 2: "separated"}[workflow], func(t *testing.T) {
			v := executedAggregateFixture(t, workflow)
			db := v.fixture.db
			earlier := []store.StoryProgressEvent{}
			for _, ref := range v.checkpoints {
				e, err := db.GetStoryProgressEvent(t.Context(), ref.RunID, ref.Sequence)
				if err != nil {
					t.Fatal(err)
				}
				earlier = append(earlier, e)
			}
			effectsBefore, err := db.ListStoryProgressEffects(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			bytesBefore, err := db.SubmissionRetainedBytes(t.Context(), v.options.RunID)
			if err != nil {
				t.Fatal(err)
			}
			if err := RetainAgentResultAggregate(t.Context(), db, v.binding, v.worktree, "aggregate-1", v.selected, v.capture); err != nil {
				t.Fatal(err)
			}
			result := aggregateOnlyResult(t, "aggregate-1", []string{"aggregate-check-1"})
			d := ValidateAgentResultCompletion(t.Context(), db, v.binding, v.worktree, result, v.base, v.checkpoints)
			if !d.Valid {
				t.Fatalf("final engineering rejected: %+v", d)
			}
			priorAggregate, err := db.GetResultAggregate(t.Context(), v.binding, "aggregate-1")
			if err != nil {
				t.Fatal(err)
			}
			// A fresh database handle and run recover only aggregate proof. Earlier
			// immutable story checkpoints remain the successful scoped prerequisites.
			reopened, err := store.Open(t.Context(), db.Path())
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := reopened.Close(); err != nil {
					t.Error(err)
				}
			}()
			nextAttempt := agentResultRun(t, reopened, v.fixture.session, "implement", "aggregate-recovery", 2)
			nextAttempt.WorkflowVersion = workflow
			nextAttempt.WorkContext = "aggregate-work"
			nextAttempt.FrozenStories = v.selected
			fresh, err := reopened.BindResultAttempt(t.Context(), nextAttempt)
			if err != nil {
				t.Fatal(err)
			}
			if fresh.ResultID != v.binding.ResultID {
				t.Fatal("aggregate recovery changed result identity")
			}
			files, err := v.capture.SnapshotFiles()
			if err != nil {
				t.Fatal(err)
			}
			output := runAggregateFixtureCheck(t, v.worktree)
			recovery, err := agent.NewAggregateCapture(files, []agent.AggregateCheckClaim{{ID: "aggregate-check-2", ExecutionID: "aggregate-execution-2", Scope: "aggregate", Name: "go test -count=1 ./...", Executed: true, Passed: true, Evidence: output, ImplementationDigest: v.capture.ImplementationDigest}})
			if err != nil {
				t.Fatal(err)
			}
			if err := RetainAgentResultAggregate(t.Context(), reopened, fresh, v.worktree, "aggregate-2", v.selected, recovery); err != nil {
				t.Fatal(err)
			}
			result = aggregateOnlyResult(t, "aggregate-2", []string{"aggregate-check-2"})
			if d := ValidateAgentResultCompletion(t.Context(), reopened, fresh, v.worktree, result, v.base, v.checkpoints); !d.Valid {
				t.Fatalf("aggregate-only recovery rejected: %+v", d)
			}
			for i, ref := range v.checkpoints {
				e, err := reopened.GetStoryProgressEvent(t.Context(), ref.RunID, ref.Sequence)
				if err != nil || !reflect.DeepEqual(earlier[i], e) {
					t.Fatal("earlier checkpoint changed", err)
				}
			}
			after, err := reopened.GetResultAggregate(t.Context(), fresh, "aggregate-1")
			if err != nil || !reflect.DeepEqual(priorAggregate, after) {
				t.Fatal("prior aggregate overwritten")
			}
			if _, err := reopened.GetStoryProgressEvent(t.Context(), fresh.RunID, 1); !errors.Is(err, store.ErrNotFound) {
				t.Fatal("recovery manufactured a story event")
			}
			effectsAfter, err := reopened.ListStoryProgressEffects(t.Context())
			if err != nil || !reflect.DeepEqual(effectsBefore, effectsAfter) {
				t.Fatal("recovery created story effects")
			}
			bytesAfter, err := reopened.SubmissionRetainedBytes(t.Context(), v.options.RunID)
			if err != nil || bytesBefore != bytesAfter {
				t.Fatal("recovery rewrote prior story proof budget")
			}
			t.Logf("executed aggregate-only recovery: run=%d attempt=%s digest=%s output=%s; no new story events/effects", fresh.RunID, fresh.Attempt, recovery.ImplementationDigest, output)
		})
	}
}
// writeAggregateFixtureFile writes a worktree file, creating its directories,
// and removes it again when the test ends.
func writeAggregateFixtureFile(t *testing.T, w *git.Worktree, rel string, content []byte) {
	t.Helper()
	path := filepath.Join(w.Path(), filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

// removeAggregateFixturePath removes a worktree path written by a sub-test.
func removeAggregateFixturePath(t *testing.T, w *git.Worktree, rel string) {
	t.Helper()
	if err := os.RemoveAll(filepath.Join(w.Path(), filepath.FromSlash(rel))); err != nil {
		t.Error(err)
	}
}

// requireAggregateStatusPath proves git reports the path, so an exemption is
// never vacuously satisfied by an ignored or unseen file.
func requireAggregateStatusPath(t *testing.T, w *git.Worktree, rel string) {
	t.Helper()
	changes, err := w.Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range changes {
		if c.Path == rel {
			return
		}
	}
	t.Fatalf("git status does not report %s: %+v", rel, changes)
}

// worktreeDiagnostic returns the code=worktree diagnostic on $.snapshot_id.
func worktreeDiagnostic(d ar.Diagnostics) (ar.Diagnostic, bool) {
	for _, diag := range d.Diagnostics {
		if diag.Path == "$.snapshot_id" && diag.Code == "worktree" {
			return diag, true
		}
	}
	return ar.Diagnostic{}, false
}

// TestResultAggregateWorktree_SessionFilesExempt is #431. Session bookkeeping
// under the top-level .aiteam/ directory that changes after aggregate capture
// does not invalidate the aggregate (BC-1); every other path keeps the full
// coverage and byte-identity checks and names the path that differed (BC-2,
// BC-3); and only the top-level .aiteam/ directory is exempt (BC-4).
func TestResultAggregateWorktree_SessionFilesExempt(t *testing.T) {
	v := executedAggregateFixture(t, 2)
	db := v.fixture.db
	if err := RetainAgentResultAggregate(t.Context(), db, v.binding, v.worktree, "aggregate-1", v.selected, v.capture); err != nil {
		t.Fatal(err)
	}
	ref, err := agent.AggregateReference("aggregate-1", []string{"aggregate-check-1"})
	if err != nil {
		t.Fatal(err)
	}
	validator := ResultAggregateValidator(t.Context(), db, v.binding, v.worktree, v.selected)
	if d := validator(ref); !d.Valid {
		t.Fatalf("baseline aggregate rejected: %+v", d)
	}

	t.Run("BC-1 session file added after capture", func(t *testing.T) {
		const manifest = ".aiteam/lf-aggregate-fixture/2026-10-01/fixture/MANIFEST.md"
		writeAggregateFixtureFile(t, v.worktree, manifest, []byte("# Manifest\n\nrequests: ee21b769, 382be94c\n"))
		defer removeAggregateFixturePath(t, v.worktree, ".aiteam")
		requireAggregateStatusPath(t, v.worktree, manifest)
		d := validator(ref)
		if diag, found := worktreeDiagnostic(d); found || !d.Valid {
			t.Fatalf("session file absent from the snapshot invalidated the aggregate: %+v %+v", diag, d)
		}
	})

	t.Run("BC-1 session file snapshotted with older bytes", func(t *testing.T) {
		const notes = ".aiteam/lf-aggregate-fixture/2026-10-01/fixture/NOTES.md"
		older := []byte("notes before the final result\n")
		writeAggregateFixtureFile(t, v.worktree, notes, older)
		defer removeAggregateFixturePath(t, v.worktree, ".aiteam")
		files, err := v.capture.SnapshotFiles()
		if err != nil {
			t.Fatal(err)
		}
		files = append(slices.Clone(files), agent.StorySnapshotFile{Path: notes, Content: older, SHA256: agent.StoryDigest(older)})
		slices.SortFunc(files, func(a, b agent.StorySnapshotFile) int { return strings.Compare(a.Path, b.Path) })
		digest := agent.SnapshotDigest(files)
		capture, err := agent.NewAggregateCapture(files, []agent.AggregateCheckClaim{{ID: "aggregate-check-session", ExecutionID: "aggregate-execution-session", Scope: "aggregate", Name: "go test -count=1 ./...", Executed: true, Passed: true, Evidence: v.capture.Checks[0].Evidence, ImplementationDigest: digest}})
		if err != nil {
			t.Fatal(err)
		}
		if err := RetainAgentResultAggregate(t.Context(), db, v.binding, v.worktree, "aggregate-session", v.selected, capture); err != nil {
			t.Fatal(err)
		}
		sessionRef, err := agent.AggregateReference("aggregate-session", []string{"aggregate-check-session"})
		if err != nil {
			t.Fatal(err)
		}
		if d := validator(sessionRef); !d.Valid {
			t.Fatalf("session aggregate rejected before any change: %+v", d)
		}
		writeAggregateFixtureFile(t, v.worktree, notes, []byte("notes edited after the final result\n"))
		d := validator(sessionRef)
		if diag, found := worktreeDiagnostic(d); found || !d.Valid {
			t.Fatalf("session file with older snapshot bytes invalidated the aggregate: %+v %+v", diag, d)
		}
	})

	t.Run("BC-2 BC-3 changed non-session file", func(t *testing.T) {
		path := filepath.Join(v.worktree.Path(), "calc.go")
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(bytes.Clone(before), []byte("// changed after capture\n")...), 0o600); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := os.WriteFile(path, before, 0o600); err != nil {
				t.Error(err)
			}
		}()
		d := validator(ref)
		diag, found := worktreeDiagnostic(d)
		if d.Valid || !found {
			t.Fatalf("changed non-session file accepted: %+v", d)
		}
		if !strings.Contains(diag.Message, "calc.go") || !strings.Contains(diag.Actual, "calc.go") {
			t.Fatalf("worktree rejection does not name the file that differed: %+v", diag)
		}
	})

	t.Run("BC-2 BC-3 uncaptured non-session file", func(t *testing.T) {
		writeAggregateFixtureFile(t, v.worktree, "late.go", []byte("package sample\n"))
		defer removeAggregateFixturePath(t, v.worktree, "late.go")
		d := validator(ref)
		diag, found := worktreeDiagnostic(d)
		if d.Valid || !found {
			t.Fatalf("uncaptured non-session file accepted: %+v", d)
		}
		if !strings.Contains(diag.Message, "late.go") || !strings.Contains(diag.Actual, "late.go") {
			t.Fatalf("worktree rejection does not name the uncaptured file: %+v", diag)
		}
	})

	for _, rel := range []string{"src/.aiteam/x", ".aiteamx/y"} {
		t.Run("BC-4 "+rel, func(t *testing.T) {
			writeAggregateFixtureFile(t, v.worktree, rel, []byte("not a session file\n"))
			defer removeAggregateFixturePath(t, v.worktree, strings.SplitN(rel, "/", 2)[0])
			requireAggregateStatusPath(t, v.worktree, rel)
			d := validator(ref)
			diag, found := worktreeDiagnostic(d)
			if d.Valid || !found {
				t.Fatalf("%s was treated as a session file: %+v", rel, d)
			}
			if !strings.Contains(diag.Actual, rel) {
				t.Fatalf("worktree rejection does not name %s: %+v", rel, diag)
			}
		})
	}

	if d := validator(ref); !d.Valid {
		t.Fatalf("fixture not restored after sub-tests: %+v", d)
	}
}

func TestResultAggregateWorktree_RejectionsAndValidateReadOnly(t *testing.T) {
	v := executedAggregateFixture(t, 2)
	db := v.fixture.db
	if err := RetainAgentResultAggregate(t.Context(), db, v.binding, v.worktree, "aggregate-1", v.selected, v.capture); err != nil {
		t.Fatal(err)
	}
	ref, err := agent.AggregateReference("aggregate-1", []string{"aggregate-check-1"})
	if err != nil {
		t.Fatal(err)
	}
	validator := ResultAggregateValidator(t.Context(), db, v.binding, v.worktree, v.selected)
	for _, fault := range []string{"missing snapshot", "missing check", "wrong revision", "wrong set", "failed", "unexecuted", "stale bytes", "omitted final file", "symlink"} {
		t.Run(fault, func(t *testing.T) {
			reference := ref
			check := validator
			switch fault {
			case "missing snapshot":
				reference.SnapshotID = "absent"
			case "missing check":
				reference.CheckIDs = []string{"story-check"}
			case "wrong revision":
				selected := slices.Clone(v.selected)
				selected[0].Revision = agent.StoryDigest([]byte("other"))
				check = ResultAggregateValidator(t.Context(), db, v.binding, v.worktree, selected)
			case "wrong set":
				check = ResultAggregateValidator(t.Context(), db, v.binding, v.worktree, v.selected[:1])
			case "failed", "unexecuted":
				capture := v.capture
				capture.Checks = slices.Clone(capture.Checks)
				capture.Checks[0].ID = "negative-" + fault
				capture.Checks[0].ExecutionID = "execution-" + fault
				capture.Checks[0].Passed = false
				capture.Checks[0].Executed = fault == "failed"
				capture.Checks[0].Evidence = "Synthetic negative factual-outcome fixture"
				if err := RetainAgentResultAggregate(t.Context(), db, v.binding, v.worktree, fault, v.selected, capture); err != nil {
					t.Fatal(err)
				}
				reference.SnapshotID = fault
				reference.CheckIDs = []string{capture.Checks[0].ID}
			case "stale bytes":
				path := filepath.Join(v.worktree.Path(), "calc.go")
				before, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, append(bytes.Clone(before), []byte("// changed after execution\n")...), 0600); err != nil {
					t.Fatal(err)
				}
				defer func() {
					if err := os.WriteFile(path, before, 0600); err != nil {
						t.Error(err)
					}
				}()
			case "omitted final file":
				path := filepath.Join(v.worktree.Path(), "late.go")
				if err := os.WriteFile(path, []byte("package sample\n"), 0600); err != nil {
					t.Fatal(err)
				}
				defer func() {
					if err := os.Remove(path); err != nil {
						t.Error(err)
					}
				}()
			case "symlink":
				path := filepath.Join(v.worktree.Path(), "calc.go")
				before, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				outside := filepath.Join(t.TempDir(), "same.go")
				if err := os.WriteFile(outside, before, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, path); err != nil {
					t.Fatal(err)
				}
				defer func() {
					if err := os.Remove(path); err != nil {
						t.Error(err)
					}
					if err := os.WriteFile(path, before, 0600); err != nil {
						t.Error(err)
					}
				}()
			}
			if d := check(reference); d.Valid {
				t.Fatal("invalid aggregate completion accepted")
			}
		})
	}
	result := aggregateOnlyResult(t, "aggregate-1", []string{"aggregate-check-1"})
	if d := ValidateAgentResultCompletion(t.Context(), db, v.binding, v.worktree, result, v.base, nil); d.Valid {
		t.Fatal("missing selected engineering accepted")
	}
	for _, outcome := range []string{"incomplete", "blocked"} {
		unfinished := ar.Result{Outcome: outcome, Reason: "Engineering remains", Comment: "No invented successful proof", Payload: ar.Payload{Implement: &ar.Implement{}}}
		if d := ValidateAgentResultCompletion(t.Context(), db, v.binding, v.worktree, unfinished, v.base, nil); !d.Valid {
			t.Fatalf("unfinished exit demanded proof: %+v", d)
		}
	}
	// Revoking a previously recorded story proof cannot be hidden by a valid
	// aggregate snapshot. This changes only the isolated fixture's trusted state.
	first := v.checkpoints[0]
	if err := db.ConflictStoryProgress(t.Context(), first.RunID, first.Sequence); err != nil {
		t.Fatal(err)
	}
	if d := ValidateAgentResultCompletion(t.Context(), db, v.binding, v.worktree, result, v.base, v.checkpoints); d.Valid {
		t.Fatal("conflicted engineering checkpoint allowed completion")
	}
	attempt := agentResultRun(t, db, v.fixture.session, "validate", "read-only-validation", 2)
	attempt.FrozenStories = v.selected
	reader, err := db.BindResultAttempt(t.Context(), attempt)
	if err != nil {
		t.Fatal(err)
	}
	if err := RetainAgentResultAggregate(t.Context(), db, reader, v.worktree, "forbidden", v.selected, v.capture); err == nil {
		t.Fatal("Validate gained capture authority")
	}
	if _, err := agent.AggregateReference("aggregate-1", []string{"aggregate-check-1"}); err != nil {
		t.Fatal("Validate could not construct pure references")
	}
	if _, err := db.GetResultAggregate(t.Context(), v.binding, "aggregate-1"); err != nil {
		t.Fatal("historical reference reading lost", err)
	}
	if d := validator(ref); !d.Valid {
		t.Fatalf("read-only retained proof rejected: %+v", d)
	}
}
