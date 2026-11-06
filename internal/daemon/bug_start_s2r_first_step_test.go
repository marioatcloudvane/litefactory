package daemon_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/store"
)

const s2rUntouched = "labelled; Bug admitted, analysis not yet dispatched"

// TestBugStartS2rDaemonKilledBeforeFirstAttemptKeepsUntouchedAndResumes kills
// the start step's Bug run on the real start path after the private start
// trigger admitted the instance and before the start node's attempt is saved.
// On this path no run row exists yet: the Bug runtime prepares and starts a
// run only for an agent node, and the start node is not one. The killed
// process therefore leaves the untouched launch: the instance at the start
// node with no run, attempt, transition, wait, dispatch or graph comment. The
// next process's orphan cleanup, Bug Reconcile and start step rounds keep that
// shape, never hand off again and keep the filing admitted, not recorded. A
// fresh owner comment then resumes the same Bug to exactly one analyst attempt
// and the filing settles as dispatched.
func TestBugStartS2rDaemonKilledBeforeFirstAttemptKeepsUntouchedAndResumes(t *testing.T) {
	s := s2rKillFirstStep(t)
	ctx := t.Context()

	bug := s.restart(t)
	reconciled, err := s.f.db.ReconcileOrphans(ctx, s2rRestarted)
	btCheck(t, err)
	if len(reconciled.AbandonedRuns) != 0 {
		t.Fatalf("orphan cleanup abandoned runs: %+v", reconciled.AbandonedRuns)
	}
	btCheck(t, bug.Reconcile(ctx))
	for range 4 {
		s.harness.Step(ctx)
		btCheck(t, s.f.daemon.Quiesce(ctx))
		s.f.clock.advance(30 * time.Second)
	}

	instance, err := s.f.db.LatestStandaloneBugInstance(ctx, s.filing.SessionID)
	btCheck(t, err)
	s.assertUntouched(t, instance)
	if s.handOffs.Load() != 1 {
		t.Fatalf("the start step handed off again after the restart: %d", s.handOffs.Load())
	}
	filing := s.readFiling(t)
	if filing.State != store.BugFilingStartAdmitted || filing.Reason != s2rUntouched || filing.AdmissionTrigger != s.trigger {
		t.Fatalf("filing after the restart=%+v", filing)
	}
	if result := s.inspect(t); result.State != "executing" || result.Summary != s2rUntouched {
		t.Fatalf("partial result=%+v", result)
	}

	s.f.forge.addComment(filing.IssueNumber, s2rReplyID, owner(), "run it again")
	for i := 0; i < 10 && s.runner.analysts == 0; i++ {
		s.f.settle()
		s.f.clock.advance(30 * time.Second)
	}
	s.assertReplyLaunched(t, instance)
	runs, err := s.f.db.ListRuns(ctx, s.filing.SessionID)
	btCheck(t, err)
	if len(runs) != 1 || runs[0].TriggerCommentID != s2rReplyID {
		t.Fatalf("runs after the reply=%+v", runs)
	}
	s.assertResumed(t, instance)
}

// assertUntouched checks the killed-before-the-first-attempt state after the
// restart: the untouched launch of the private start trigger.
func (s *s2rRun) assertUntouched(t *testing.T, instance store.GraphInstance) {
	t.Helper()
	ctx := t.Context()
	if instance.Status != store.GraphRunning || instance.CurrentNode != "conversation" || !instance.NextDueAt.IsZero() {
		t.Fatalf("instance=%+v", instance)
	}
	if launched, found, err := s.f.db.WorkflowLaunchInstance(ctx, s.filing.SessionID, s.trigger); err != nil || !found || launched != instance.ID {
		t.Fatalf("the private start trigger is not the instance's launch: %d %t %v", launched, found, err)
	}
	instances, err := s.f.db.ListStandaloneBugInstances(ctx)
	btCheck(t, err)
	if len(instances) != 1 {
		t.Fatalf("instances=%+v", instances)
	}
	runs, err := s.f.db.ListRuns(ctx, s.filing.SessionID)
	btCheck(t, err)
	if len(runs) != 0 {
		t.Fatalf("runs=%+v", runs)
	}
	attempts, err := s.f.db.ListGraphNodeAttempts(ctx, instance.ID)
	btCheck(t, err)
	if len(attempts) != 0 {
		t.Fatalf("attempts=%+v", attempts)
	}
	transitions, err := s.f.db.ListGraphTransitions(ctx, instance.ID)
	btCheck(t, err)
	if len(transitions) != 0 {
		t.Fatalf("transitions=%+v", transitions)
	}
	if _, err = s.f.db.GetGraphOwnerWait(ctx, instance.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("owner wait parked: %v", err)
	}
	if _, err = s.f.db.LatestGraphDispatch(ctx, instance.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("graph dispatch recorded: %v", err)
	}
	if comments, err := s.f.db.ListGraphComments(ctx, instance.ID, "", 1); err != nil || len(comments) != 0 {
		t.Fatalf("graph comments=%+v err=%v", comments, err)
	}
	starts, err := s.f.db.BugFilingStartAttempts(ctx, s.filing.ID)
	btCheck(t, err)
	if len(starts) != 1 || starts[0].ID != s.attempt.ID {
		t.Fatalf("start attempts=%+v", starts)
	}
	if s.runner.analysts != 0 {
		t.Fatalf("analyst invoked %d times", s.runner.analysts)
	}
}

// s2rKillFirstStep runs the real flow up to a kill before the start node's
// first attempt is saved: the turn's MCP clock is read for the conversation
// node's request before its attempt row exists.
func s2rKillFirstStep(t *testing.T) *s2rRun {
	t.Helper()
	tempRoot, err := filepath.EvalSymlinks(os.TempDir())
	btCheck(t, err)
	t.Setenv("TMPDIR", tempRoot)
	ctx := t.Context()
	f := newFixture(t)
	bugFileForgeLabels(f, map[string]string{"bug": "LA_bug"})
	s := &s2rRun{f: f, handOffs: daemon.CountBugStartHandOffsForTest(t)}

	s.repo = gittest.New(t)
	btCheck(t, os.Rename(s.repo.Dir, f.repo.Path))
	s.repo.Dir = f.repo.Path
	s.repo.WriteFile("behavior.txt", "original behavior\n")
	s.repo.Commit("seed Bug fixture")
	s.repo.Git("push", "origin", "main")
	f.repo.RemoteURL = s.repo.Origin
	f.repo, err = f.db.UpsertRepo(ctx, f.repo)
	btCheck(t, err)
	s.runner = &bugPathRunner{repo: s.repo}

	armed := &atomic.Bool{}
	killed := &atomic.Int64{}
	db := f.db
	killingNow := func() time.Time {
		if armed.Load() {
			if trigger, ok := s2rBeforeFirstAttempt(db); ok {
				armed.Store(false)
				killed.Store(trigger)
				runtime.Goexit()
			}
		}
		return time.Now()
	}
	s2rBugRuntime(t, f, s.repo, s.runner, s2rKilledName, killingNow)
	f.plantCursor()

	s.cfg = config.DefaultRemoteConnector()
	s.cfg.Enabled = true
	s.cfg.OwnerID = ownerID
	s.cfg.GitHubClientID = "fixture"
	s.cfg.GitHubSecretRef.Env = "TEST_GITHUB_SECRET"
	s.cfg.CookieSecretRef.Env = "TEST_COOKIE_SECRET"
	s.token = brainstormTasksToken(t, f, s.cfg.ResourceURL, f.clock.Now())
	s.harness = f.daemon.EnableBugFilingForTest(s.cfg, f.clock.Now)
	s.call = s2rConnector(t, f, s.cfg, s.harness.Actions, s.token)

	s.call("bug_file_propose", map[string]any{"repository": testOwnerName + "/" + testRepoName, "title": "Saved filters vanish after reload", "description": "Expected: saved filters stay after a reload.\nActual: the list is empty.", "client_request_key": "owner-bug-s2r-first"}, &s.proposal)
	f.clock.advance(5 * time.Second)
	s.call("action_confirm", map[string]any{"proposal_id": s.proposal.ID}, &s.proposal)

	armed.Store(true)
	for i := 0; i < 20 && killed.Load() == 0; i++ {
		if _, e := s.harness.Actions.Step(ctx, s.proposal.ID); e != nil {
			t.Fatalf("action step: %v", e)
		}
		s.harness.Step(ctx)
		btCheck(t, f.daemon.Quiesce(ctx))
		if killed.Load() != 0 {
			break
		}
		f.settle()
		f.clock.advance(30 * time.Second)
	}
	if killed.Load() == 0 {
		t.Fatalf("the process was not killed before the first attempt; logs=%s", f.logs)
	}
	filings, err := f.db.ActiveBugFilings(ctx, 10)
	btCheck(t, err)
	if len(filings) != 1 {
		t.Fatalf("filings at the kill=%+v", filings)
	}
	s.filing = filings[0]
	starts, err := f.db.BugFilingStartAttempts(ctx, s.filing.ID)
	btCheck(t, err)
	if len(starts) != 1 || starts[0].State != store.BugStartRunning {
		t.Fatalf("start attempts at the kill=%+v", starts)
	}
	s.attempt, s.trigger = starts[0], killed.Load()
	if !store.IsBugStartTrigger(s.trigger) || s.handOffs.Load() != 1 {
		t.Fatalf("killed trigger %d is not a start trigger or hand-offs=%d", s.trigger, s.handOffs.Load())
	}
	// At the kill: no run row exists; the start node is not an agent node.
	runs, err := f.db.ListRuns(ctx, s.filing.SessionID)
	btCheck(t, err)
	if len(runs) != 0 || s.runner.analysts != 0 {
		t.Fatalf("runs at the kill=%+v analysts=%d", runs, s.runner.analysts)
	}
	return s
}

// s2rBeforeFirstAttempt reports the private trigger of a running start
// attempt whose Bug instance sits at the start node with no attempt yet.
func s2rBeforeFirstAttempt(db *store.DB) (int64, bool) {
	ctx := context.Background()
	filings, err := db.ActiveBugFilings(ctx, 10)
	if err != nil {
		return 0, false
	}
	for _, filing := range filings {
		if filing.SessionID == 0 {
			continue
		}
		starts, err := db.BugFilingStartAttempts(ctx, filing.ID)
		if err != nil || len(starts) != 1 || starts[0].State != store.BugStartRunning {
			continue
		}
		instance, err := db.LatestStandaloneBugInstance(ctx, filing.SessionID)
		if err != nil || instance.Status != store.GraphRunning || instance.CurrentNode != "conversation" {
			continue
		}
		attempts, err := db.ListGraphNodeAttempts(ctx, instance.ID)
		if err != nil || len(attempts) != 0 {
			continue
		}
		return starts[0].TriggerID(), true
	}
	return 0, false
}
