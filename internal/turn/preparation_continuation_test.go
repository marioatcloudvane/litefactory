package turn

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"

	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/store"
)

// AC-001 follows one owner command all the way through real Git preparation,
// the admitted writer and ordinary durable report delivery. Publication is not
// assumed: unresolved remote work can still prevent the normal final push.
func TestPreparationCaveatsContinueOriginalRequest(t *testing.T) {
	for _, tc := range []struct {
		name       string
		task, base bool
		capped     bool
		dirty      bool
	}{
		{name: "task conflict", task: true},
		{name: "base conflict", base: true},
		{name: "both conflicts", task: true, base: true},
		{name: "task distance cap", task: true, capped: true},
		{name: "base distance cap", base: true, capped: true},
		{name: "both distance caps", task: true, base: true, capped: true},
		{name: "both phases preserve dirty work", task: true, base: true, dirty: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var f *fixture
			var before retainedSnapshot
			runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
				observed := captureRetained(t, f, req.WorktreePath)
				assertRetained(t, before, observed)
				t.Logf("invocation ref=%s run=%d profile=%s permissions=%+v; before HEAD=%s raw_index=%x files=%q modes=%v; observed HEAD=%s raw_index=%x files=%q modes=%v", req.Ref, req.RunOrdinal, req.Permissions.Profile, req.Permissions.Payload, before.head, before.index, before.files, before.modes, observed.head, observed.index, observed.files, observed.modes)
				if req.Ref != f.ref() || req.RunOrdinal != 1 || req.WorktreePath != f.worktreeDir() {
					t.Fatalf("preparation changed invocation identity: %+v", req)
				}
				if !strings.Contains(req.Prompt, "add a health endpoint that reports the build sha") {
					t.Fatal("original owner request was lost")
				}
				writeInWorktree(t, req, "health.go", "package api // authorized unaffected work\n")
				return succeeded(req, f.paths.Home()), nil
			})
			f = newFixture(t, runner)
			f.opts.AutoSyncBase = true
			f.opts.BlockRunOnConflict = true // Compatibility setting cannot revoke this request.
			if tc.capped {
				f.opts.AutoSyncMaxBehind = 1
			}
			dir := seedTaskWorktree(t, f)
			commitInSyncDir(t, f.repo, dir, "retained.txt", "retained owner work\n")
			initial := f.repo.GitIn(dir, "rev-parse", "HEAD")
			if !tc.capped {
				commitInSyncDir(t, f.repo, dir, "README.md", "local conflicting work\n")
			}
			if tc.task {
				name := "README.md"
				if tc.capped {
					name = "remote-task.txt"
				}
				remote := taskOperator(t, f.repo, testBranch, initial, name, "remote task work\n")
				if tc.capped {
					taskOperator(t, f.repo, testBranch, remote, "remote-task-two.txt", "more remote task work\n")
				}
			}
			if tc.base {
				if tc.capped {
					advanceSyncBase(t, f.repo, 2)
				} else {
					commitInSyncDir(t, f.repo, f.repo.Dir, "README.md", "remote base conflicting work\n")
					f.repo.Git("push", "--quiet", "origin", f.storeRepo.BaseBranch)
				}
			}
			// The dirty variant includes staged and unstaged bytes. Every
			// variant must preserve the raw index until useful work starts.
			if tc.dirty {
				dirtyRetained(t, f, dir)
			}
			before = captureRetained(t, f, dir)
			req := f.request()
			result := f.execute(req)
			if runner.callCount() != 1 || result.Harness == nil || result.Run.ID == 0 {
				t.Fatalf("original request did not reach useful work: %s %s\n%s", result.Disposition, result.Reason, f.logs)
			}
			if result.Session.ID != req.SessionID || result.Branch != testBranch || result.Run.TriggerCommentID != req.Trigger.CommentID {
				t.Fatalf("task, branch or trigger identity changed: %+v", result)
			}
			if body, err := os.ReadFile(filepath.Join(dir, "health.go")); err != nil || !strings.Contains(string(body), "authorized unaffected work") {
				t.Fatalf("authorized work missing: %q %v", body, err)
			}
			if len(result.Run.SyncObservations) != 2 {
				t.Fatalf("lost independent task/base evidence: %+v", result.Run.SyncObservations)
			}
			forge := newFakeForge(t)
			reporter := newReporter(t, f, forge, nil)
			if _, err := reporter.ReportTurn(t.Context(), result); err != nil {
				t.Fatalf("normal result delivery: %v", err)
			}
			body := forge.only(t).body
			t.Logf("normal result after original request (session=%d branch=%s trigger=%d):\n%s", result.Session.ID, result.Branch, result.Run.TriggerCommentID, body)
			if strings.Count(body, "Preparation caveat") != 1 {
				t.Fatalf("want one combined preparation passage:\n%s", body)
			}
			if runs := f.runs(); len(runs) != 1 || runs[0].Status != store.RunReported || watermark(t, f) != req.Trigger.CommentID {
				t.Fatalf("request was retried or normal delivery was not retained: %+v", runs)
			}
		})
	}

	t.Run("unavailable context with both sync phases and pending refinement", func(t *testing.T) {
		var f *fixture
		var unavailable string
		const oldBytes = "{ malformed owner context; do not admit these bytes"
		runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
			if req.PlanWorkingContext != "" || strings.Contains(req.Prompt, oldBytes) || !strings.Contains(req.Prompt, "Optional planning context is unavailable") {
				t.Fatal("optional context was admitted or not explained")
			}
			if req.PlanContextPath == unavailable {
				t.Fatal("fresh result would overwrite unavailable context")
			}
			t.Logf("Plan invocation ref=%s run=%d attempt=%s fresh_context=%s profile=%s; unavailable source omitted from prompt", req.Ref, req.PlanRunID, req.PlanAttempt, req.PlanContextPath, req.Permissions.Profile)
			writePlanOutcome(t, req, "discussion")
			writePlanContext(t, f, req)
			return succeeded(req, f.paths.Home()), nil
		})
		f = newFixture(t, runner)
		setupOutcomeForge(t, f)
		f.opts.AutoSyncBase, f.opts.AutoSyncMaxBehind = true, 1
		dir := seedTaskWorktree(t, f)
		contextDir, err := naming.AITeamSessionDir(testBranch, time.Now().Add(-24*time.Hour), testSlug)
		mustProgress(t, err)
		unavailable = filepath.ToSlash(filepath.Join(contextDir, planbrief.ContextFileName))
		mustProgress(t, os.MkdirAll(filepath.Join(dir, contextDir), 0700))
		mustProgress(t, os.WriteFile(filepath.Join(dir, unavailable), []byte(oldBytes), 0600))
		f.repo.GitIn(dir, "add", "--all")
		f.repo.GitIn(dir, "commit", "--quiet", "-m", "retain unavailable optional context")
		initial := f.repo.GitIn(dir, "rev-parse", "HEAD")
		remote := taskOperator(t, f.repo, testBranch, initial, "remote-task.txt", "remote task work")
		taskOperator(t, f.repo, testBranch, remote, "remote-task-two.txt", "more remote task work")
		advanceSyncBase(t, f.repo, 2)
		pending := store.StoryRefinement{SessionID: f.session.ID, CommentID: 8080, ChildNumber: 200, ChildID: 2000, Generation: 1, ActorID: 42, Body: "Unapproved proposed scope must remain pending"}
		mustProgress(t, f.db.AdmitStoryRefinement(t.Context(), pending))
		req := planRequest(f)
		result := f.execute(req)
		if runner.callCount() != 1 || result.Harness == nil || result.Run.Status != store.RunSucceeded {
			t.Fatalf("same request did not finish current-attempt discussion: %+v logs=%s", result, f.logs)
		}
		if result.Err != nil && !strings.Contains(result.Err.Error(), "push") {
			t.Fatalf("current-attempt finalization rejected valid discussion: %v", result.Err)
		}
		if data, err := os.ReadFile(filepath.Join(dir, unavailable)); err != nil || string(data) != oldBytes {
			t.Fatalf("unavailable source changed: %q %v", data, err)
		}
		remaining, err := f.db.ListPendingStoryRefinements(t.Context(), f.session.ID)
		mustProgress(t, err)
		if !reflect.DeepEqual(remaining, []store.StoryRefinement{pending}) {
			t.Fatalf("preparation consumed or changed pending scope: %+v", remaining)
		}
		forge := newFakeForge(t)
		_, err = newReporter(t, f, forge, nil).ReportTurn(t.Context(), result)
		mustProgress(t, err)
		body := forge.only(t).body
		t.Logf("unavailable source retained path=%s bytes=%q; pending refinement=%+v; normal result:\n%s", unavailable, oldBytes, remaining, body)
		if strings.Count(body, "Preparation caveat") != 1 || !strings.Contains(strings.ToLower(body), "context") || !strings.Contains(strings.ToLower(body), "refinement") {
			t.Fatalf("normal result lost combined caveats: %s", body)
		}
	})

	t.Run("canonical managed worker reaches approved work through deferred sync", func(t *testing.T) {
		var f *fixture
		calls := 0
		runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
			calls++
			t.Logf("approved managed invocation ref=%s graph=%+v profile=%s permissions=%+v", req.Ref, req.Graph, req.Permissions.Profile, req.Permissions.Payload)
			if req.Graph == nil || req.Graph.Node != "worker_agent" || req.Permissions.Profile != "implement" || !strings.Contains(req.Prompt, "As an operator I want a health endpoint") {
				t.Fatalf("approved worker authority or scope changed: %+v", req)
			}
			mustProgress(t, os.WriteFile(filepath.Join(req.WorktreePath, "health.go"), []byte("package api // approved unaffected work\n"), 0600))
			submitLiveResult(t, req, implementBlockedJSON)
			return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "approved local work retained", FinishedAt: time.Now()}, nil
		})
		var req Request
		f, _, req = assembledImplementFixture(t, runner)
		enableWorkerSyncFixture(f)
		seedImplementStory(t, f)
		row := implementWorkerRow(t, f)
		_, err := f.db.FreezeSessionSlug(t.Context(), f.session.ID, testSlug, row.Branch)
		mustProgress(t, err)
		dir := f.worktreeDir()
		f.repo.Git("worktree", "add", "-b", row.Branch, dir, "HEAD")
		conflictBaseAndBranch(t, f.repo, dir)
		f.repo.GitIn(dir, "push", "--quiet", "origin", "HEAD:refs/heads/"+row.Branch)
		pending := store.StoryRefinement{SessionID: f.session.ID, CommentID: 8080, ChildNumber: 200, ChildID: 2000, Generation: 1, ActorID: 42, Body: "Proposed scope remains unapproved"}
		mustProgress(t, f.db.AdmitStoryRefinement(t.Context(), pending))
		out, err := Execute(t.Context(), f.opts, req)
		if err != nil || calls != 1 {
			t.Fatalf("canonical deferred route did not reach approved writer: calls=%d result=%+v err=%v logs=%s", calls, out, err, f.logs)
		}
		instance, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "implement")
		mustProgress(t, err)
		attempt, err := f.db.LatestGraphNodeAttempt(t.Context(), instance.ID, "sync_before_worker")
		mustProgress(t, err)
		if attempt.Outcome != "deferred" {
			t.Fatalf("preparation conflict was relabeled or stopped: %+v", attempt)
		}
		if data, err := os.ReadFile(filepath.Join(dir, "health.go")); err != nil || !strings.Contains(string(data), "approved unaffected work") {
			t.Fatalf("approved work missing: %q %v", data, err)
		}
		remaining, err := f.db.ListPendingStoryRefinements(t.Context(), f.session.ID)
		mustProgress(t, err)
		if !reflect.DeepEqual(remaining, []store.StoryRefinement{pending}) {
			t.Fatalf("pending scope changed: %+v", remaining)
		}
	})
}
