package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
)

// refusingStepCapacity is a full shared pool: it admits nothing.
type refusingStepCapacity struct{ refusals int }

func (c *refusingStepCapacity) Replace(context.Context, int) (bool, error) {
	c.refusals++
	return false, nil
}
func (c *refusingStepCapacity) Release()                    {}
func (c *refusingStepCapacity) Capacity() (used, total int) { return 3, 3 }

// presyncDeferredEvidence reads the latest sync_before_worker attempt and its
// retained evidence.
func presyncDeferredEvidence(t *testing.T, f *fixture) (store.GraphNodeAttempt, syncBaseOutput) {
	t.Helper()
	instance, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "implement")
	mustProgress(t, err)
	attempt, err := f.db.LatestGraphNodeAttempt(t.Context(), instance.ID, "sync_before_worker")
	mustProgress(t, err)
	outputs, err := f.db.ListGraphOutputs(t.Context(), attempt.ID)
	mustProgress(t, err)
	var evidence syncBaseOutput
	for _, output := range outputs {
		if output.Kind != store.GraphOutputJSON {
			continue
		}
		var candidate syncBaseOutput
		mustProgress(t, json.Unmarshal(output.Payload, &candidate))
		if candidate.Result == attempt.Outcome {
			evidence = candidate
		}
	}
	return attempt, evidence
}

// reserveOwedDispatch reserves the next owed graph dispatch as a request.
func reserveOwedDispatch(t *testing.T, f *fixture, req Request) Request {
	t.Helper()
	dispatch, ok, err := f.db.NextFreshGraphDispatch(t.Context(), f.session.ID)
	mustProgress(t, err)
	if !ok {
		t.Fatal("owed dispatch absent")
	}
	reserved, ok, err := f.db.ReserveGraphDispatch(t.Context(), dispatch.ID, dispatch.DueAt)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	next := req
	next.Trigger.CommentID, next.Trigger.Body = reserved.TriggerID(), "Continue the workflow."
	return next
}

func presyncGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func implementRunCommits(t *testing.T, f *fixture, branch string) []string {
	t.Helper()
	f.repo.Git("fetch", "--quiet", "origin")
	var out []string
	for _, line := range strings.Split(f.repo.Git("log", "--format=%s", "origin/"+branch), "\n") {
		if strings.HasPrefix(line, "Implement run") {
			out = append(out, line)
		}
	}
	return out
}

// presyncFixture runs one worker that leaves uncommitted edits and answers
// incomplete, so the graph passes sync_before_worker before the run ends.
func presyncFixture(t *testing.T) (*fixture, Request, *int, *string) {
	t.Helper()
	calls := 0
	var retained string
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		calls++
		retained = req.WorktreePath
		mustProgress(t, os.WriteFile(filepath.Join(req.WorktreePath, fmt.Sprintf("work-%d.txt", calls)), []byte("worker edit\n"), 0o600))
		submitLiveResult(t, req, implementIncompleteJSON)
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "done", FinishedAt: time.Now()}, nil
	})
	f, _, req := assembledImplementFixture(t, runner)
	enableWorkerSyncFixture(f)
	seedImplementStory(t, f)
	_, err := f.db.FreezeSessionSlug(t.Context(), f.session.ID, testSlug, "lf/story-001")
	mustProgress(t, err)
	_, err = Execute(t.Context(), f.opts, req)
	mustProgress(t, err)
	if calls != 1 || retained == "" {
		t.Fatalf("initial launch: calls=%d retained=%q", calls, retained)
	}
	instance, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "implement")
	mustProgress(t, err)
	if instance.CurrentNode != "worker_flagship" {
		t.Fatalf("worker did not park after its next gate: %+v", instance)
	}
	return f, req, &calls, &retained
}

// c-own-commit and c-capacity-only: the factory's own end-of-run commit is in
// the deferred snapshot, so capacity deferrals and admission start the worker.
func TestImplementWorkerPresyncSaveSurvivesCapacityDeferral(t *testing.T) {
	f, req, calls, _ := presyncFixture(t)
	const branch = "lf/story-001"
	commits := implementRunCommits(t, f, branch)
	if len(commits) != 1 {
		t.Fatalf("worker run commits=%v", commits)
	}
	head := strings.TrimSpace(f.repo.Git("rev-parse", "origin/"+branch))
	_, evidence := presyncDeferredEvidence(t, f)
	if evidence.Commit != head {
		t.Fatalf("sync snapshot %s precedes the factory's own commit %s", evidence.Commit, head)
	}

	pool := &refusingStepCapacity{}
	for range 3 {
		next := reserveOwedDispatch(t, f, req)
		out, err := Execute(WithStepCapacity(t.Context(), pool), f.opts, next)
		if err != nil || !out.GraphDeferred {
			t.Fatalf("capacity deferral: out=%+v err=%v", out, err)
		}
	}
	if pool.refusals != 3 || *calls != 1 {
		t.Fatalf("capacity refusals=%d calls=%d", pool.refusals, *calls)
	}
	next := reserveOwedDispatch(t, f, req)
	if _, err := Execute(t.Context(), f.opts, next); err != nil {
		t.Fatalf("admitted worker refused: %v", err)
	}
	if *calls != 2 {
		t.Fatalf("admitted worker did not start: calls=%d", *calls)
	}
	session, err := f.db.GetSession(t.Context(), f.session.ID)
	mustProgress(t, err)
	if session.State == store.SessionFailed {
		t.Fatalf("session failed: %+v", session)
	}
	instance, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "implement")
	mustProgress(t, err)
	if instance.Status == store.GraphFailed {
		t.Fatalf("graph failed: %+v", instance)
	}
	if got := implementRunCommits(t, f, branch); len(got) != 2 {
		t.Fatalf("each worker run saves once: %v", got)
	}
}

// c-foreign-refused: a foreign commit after the saved handoff still refuses,
// without starting a run, committing or pushing. The deferred route's listing
// of changed paths is covered by TestDeferredSyncHandoffRefusesForeignCommitWithPaths.
func TestImplementWorkerForeignCommitAfterHandoffRefuses(t *testing.T) {
	f, req, calls, retained := presyncFixture(t)
	const branch = "lf/story-001"
	before := implementRunCommits(t, f, branch)
	mustProgress(t, os.WriteFile(filepath.Join(*retained, "foreign.txt"), []byte("foreign\n"), 0o600))
	presyncGit(t, *retained, "add", "foreign.txt")
	presyncGit(t, *retained, "-c", "user.name=Foreign", "-c", "user.email=foreign@example.invalid", "commit", "--quiet", "-m", "foreign change")
	remoteBefore := strings.TrimSpace(f.repo.Git("rev-parse", "origin/"+branch))
	next := reserveOwedDispatch(t, f, req)
	_, err := Execute(t.Context(), f.opts, next)
	if err == nil {
		t.Fatal("foreign commit after the handoff was accepted")
	}
	if *calls != 1 {
		t.Fatalf("refused handoff started a run: calls=%d", *calls)
	}
	if after := implementRunCommits(t, f, branch); len(after) != len(before) || strings.TrimSpace(f.repo.Git("rev-parse", "origin/"+branch)) != remoteBefore {
		t.Fatalf("refused handoff committed or pushed: before=%v after=%v", before, after)
	}
}

// c-no-unneeded-save: a story saved by its done step is not saved again by
// finish, and the first sync_before_worker of the run saves nothing.
func TestImplementWorkerCompletedStorySavesOnce(t *testing.T) {
	calls := 0
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		calls++
		mustProgress(t, os.WriteFile(filepath.Join(req.WorktreePath, "finished.txt"), []byte("done\n"), 0o600))
		submitLiveResult(t, req, implementCompletedJSON)
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "done", FinishedAt: time.Now()}, nil
	})
	f, _, req := assembledImplementFixture(t, runner)
	enableWorkerSyncFixture(f)
	seedImplementStory(t, f)
	_, err := f.db.FreezeSessionSlug(t.Context(), f.session.ID, testSlug, "lf/story-001")
	mustProgress(t, err)
	_, err = Execute(t.Context(), f.opts, req)
	mustProgress(t, err)
	runOwedImplementStages(t, f, req)
	if row := implementWorkerRow(t, f); calls != 1 || row.State != store.ImplementDone {
		t.Fatalf("completed story: calls=%d row=%+v", calls, row)
	}
	if got := implementRunCommits(t, f, "lf/story-001"); len(got) != 1 {
		t.Fatalf("completed story saved %d times: %v", len(got), got)
	}
}

// c-presync-save-error: a failed pre-sync save (commit made, push refused)
// fails the run as an end-of-run save failure does, without a second save or
// a sync in that pass.
func TestImplementWorkerPresyncSaveFailureFailsRunWithoutSync(t *testing.T) {
	var f *fixture
	calls := 0
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		calls++
		mustProgress(t, os.WriteFile(filepath.Join(req.WorktreePath, "work.txt"), []byte("worker edit\n"), 0o600))
		hook := filepath.Join(f.repo.Origin, "hooks", "pre-receive")
		mustProgress(t, os.MkdirAll(filepath.Dir(hook), 0o700))
		mustProgress(t, os.WriteFile(hook, []byte("#!/bin/sh\nexit 1\n"), 0o700))
		submitLiveResult(t, req, implementIncompleteJSON)
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "done", FinishedAt: time.Now()}, nil
	})
	var req Request
	f, _, req = assembledImplementFixture(t, runner)
	enableWorkerSyncFixture(f)
	seedImplementStory(t, f)
	_, err := f.db.FreezeSessionSlug(t.Context(), f.session.ID, testSlug, "lf/story-001")
	mustProgress(t, err)
	out, err := Execute(t.Context(), f.opts, req)
	if err == nil || calls != 1 {
		t.Fatalf("refused push did not fail the run: calls=%d err=%v", calls, err)
	}
	session, err := f.db.GetSession(t.Context(), f.session.ID)
	mustProgress(t, err)
	if session.State != store.SessionFailed {
		t.Fatalf("session state %s", session.State)
	}
	run, err := f.db.GetRun(t.Context(), out.Run.ID)
	mustProgress(t, err)
	if run.Status != store.RunFailed {
		t.Fatalf("run status %s", run.Status)
	}
	instance, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "implement")
	mustProgress(t, err)
	attempts, err := f.db.ListGraphNodeAttempts(t.Context(), instance.ID)
	mustProgress(t, err)
	syncs := 0
	for _, a := range attempts {
		if a.Node == "sync_before_worker" {
			syncs++
		}
	}
	if syncs != 1 {
		t.Fatalf("a sync ran after the failed save: sync attempts=%d", syncs)
	}
	log := strings.TrimSpace(runGitOutput(t, out.WorktreePath, "log", "--format=%s"))
	commits := 0
	for _, line := range strings.Split(log, "\n") {
		if strings.HasPrefix(line, "Implement run") {
			commits++
		}
	}
	if commits != 1 {
		t.Fatalf("failed save made %d commits", commits)
	}
}

func runGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(out)
}
