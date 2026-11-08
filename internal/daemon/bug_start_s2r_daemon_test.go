package daemon_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/connector"
	"github.com/cloudvane/litefactory/internal/connectorstorage"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

const (
	s2rRecorded   = "labelled; Bug run recorded but not started; reply run it again on the issue to continue"
	s2rReplyID    = int64(6664969403)
	s2rRestarted  = "daemon-instance-restarted"
	s2rKilledName = "daemon-instance-killed"
)

// TestBugStartS2rDaemonInterruptionKeepsPartialAndResumes is the
// run-before-analyst interruption on the real start path. The owner's bug is
// proposed and confirmed through the connector; the filing processor files and
// labels it; the real start step hands off its own private trigger and the Bug
// run starts. The process dies after the conversation moved to the analyst and
// before the analyst attempt is saved. The next process runs orphan cleanup,
// the Bug Reconcile and the start step again: the run failed, nothing is
// parked, the conversation history stays, no analyst attempt exists, the
// start step neither hands off nor restarts and keeps the filing partial. A
// fresh owner comment on the issue and the daemon's ticks then continue the
// same Bug to exactly one analyst attempt, and the start step settles the
// filing as dispatched.
func TestBugStartS2rDaemonInterruptionKeepsPartialAndResumes(t *testing.T) {
	s := s2rKill(t)
	ctx := t.Context()

	// The next process: orphan cleanup, then the Bug Reconcile, then the
	// start step's rounds.
	bug := s.restart(t)
	if _, err := s.f.db.ReconcileOrphans(ctx, s2rRestarted); err != nil {
		t.Fatal(err)
	}
	btCheck(t, bug.Reconcile(ctx))
	for range 3 {
		s.harness.Step(ctx)
		btCheck(t, s.f.daemon.Quiesce(ctx))
		s.f.clock.advance(30 * time.Second)
	}

	instance, err := s.f.db.LatestStandaloneBugInstance(ctx, s.filing.SessionID)
	btCheck(t, err)
	s.assertRetained(t, instance)
	if _, err = s.f.db.LatestGraphDispatch(ctx, instance.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("graph dispatch recorded: %v", err)
	}
	if comments, err := s.f.db.ListGraphComments(ctx, instance.ID, "", 1); err != nil || len(comments) != 0 {
		t.Fatalf("graph comments=%+v err=%v", comments, err)
	}
	s.assertNoNewStart(t)
	filing := s.readFiling(t)
	if filing.State != store.BugFilingStartAdmitted || filing.Reason != s2rRecorded || filing.AdmissionTrigger != s.trigger {
		t.Fatalf("filing after the restart=%+v", filing)
	}
	if result := s.inspect(t); result.State != "executing" || result.Summary != s2rRecorded {
		t.Fatalf("partial result=%+v", result)
	}

	// A fresh real comment by the authenticated owner, then the daemon's
	// ticks.
	s.f.forge.addComment(filing.IssueNumber, s2rReplyID, owner(), "run it again")
	for i := 0; i < 10 && s.runner.analysts == 0; i++ {
		s.f.settle()
		s.f.clock.advance(30 * time.Second)
	}
	s.assertReplyLaunched(t, instance)
	s.assertResumed(t, instance)
}

// TestBugStartS2rDaemonStartupKeepsPartialUntilOwnerReply is the same
// interruption through the real daemon startup: Start runs orphan cleanup, the
// Bug Reconcile and stranded-graph recovery, which leaves a Bug started only
// by the private start trigger alone. With no owner comment the daemon's ticks
// and the start step neither dispatch the analyst nor hand off again, and the
// filing stays partial. The owner's fresh reply then resumes the same Bug.
func TestBugStartS2rDaemonStartupKeepsPartialUntilOwnerReply(t *testing.T) {
	s := s2rKill(t)
	ctx := t.Context()

	s.restart(t)
	reconciled, err := s.f.daemon.Start(ctx)
	btCheck(t, err)
	if reconciled.AbandonedRuns != 1 {
		t.Fatalf("reconciled=%+v", reconciled)
	}
	for range 3 {
		s.f.settle()
		s.harness.Step(ctx)
		btCheck(t, s.f.daemon.Quiesce(ctx))
		s.f.clock.advance(30 * time.Second)
	}
	instance, err := s.f.db.LatestStandaloneBugInstance(ctx, s.filing.SessionID)
	btCheck(t, err)
	s.assertRetained(t, instance)
	if dispatch, err := s.f.db.LatestGraphDispatch(ctx, instance.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("graph dispatch recorded without a reply: %+v %v", dispatch, err)
	}
	s.assertNoNewStart(t)
	filing := s.readFiling(t)
	if filing.State != store.BugFilingStartAdmitted || filing.Reason != s2rRecorded || filing.AdmissionTrigger != s.trigger {
		t.Fatalf("filing after the startup=%+v", filing)
	}
	if result := s.inspect(t); result.State != "executing" || result.Summary != s2rRecorded {
		t.Fatalf("partial result=%+v", result)
	}

	s.f.forge.addComment(filing.IssueNumber, s2rReplyID, owner(), "run it again")
	for i := 0; i < 10 && s.runner.analysts == 0; i++ {
		s.f.settle()
		s.f.clock.advance(30 * time.Second)
	}
	s.assertReplyLaunched(t, instance)
	s.assertResumed(t, instance)
}

// assertReplyLaunched checks the owner's reply recorded its launch on the
// interrupted instance itself.
func (s *s2rRun) assertReplyLaunched(t *testing.T, instance store.GraphInstance) {
	t.Helper()
	launched, found, err := s.f.db.WorkflowLaunchInstance(t.Context(), s.filing.SessionID, s2rReplyID)
	btCheck(t, err)
	if !found || launched != instance.ID {
		t.Fatalf("the owner reply's launch is not on instance %d: %d %t; logs=%s", instance.ID, launched, found, s.f.logs)
	}
}

// s2rRun is the state the killed process left: one filing, its one start
// attempt and the private trigger the start step minted for it.
type s2rRun struct {
	f        *fixture
	repo     *gittest.Repo
	runner   *bugPathRunner
	cfg      config.RemoteConnector
	token    string
	proposal owneraction.Proposal
	harness  daemon.BugFileHarness
	call     func(string, any, any)
	handOffs *atomic.Int32
	filing   store.BugFiling
	attempt  store.BugFilingStartAttempt
	trigger  int64
}

// s2rKill runs the real flow up to the kill.
//
// The kill: the turn's MCP clock is read on the turn's own worker goroutine.
// The first read after the session has a running run whose trigger is a Bug
// start trigger is where the process dies, as in the turn-level
// characterization: after the run started (and the conversation node moved to
// the analyst), before the analyst attempt is saved. runtime.Goexit ends that
// worker goroutine there, so none of the killed process's later work runs: no
// analyst attempt, no run finish, no start-step classification. Only deferred
// in-memory cleanup unwinds (the session's in-flight mark, the capacity slot),
// which a dead process loses anyway. The retained rows are exactly what the
// process wrote before it died. The old daemon is then dropped; the restart
// builds a new one with a new instance id over the same database.
func s2rKill(t *testing.T) *s2rRun {
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
			if trigger, ok := s2rRunningStartRun(db); ok {
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

	s.call("bug_file_propose", map[string]any{"repository": testOwnerName + "/" + testRepoName, "title": "Saved filters vanish after reload", "description": "Expected: saved filters stay after a reload.\nActual: the list is empty.", "client_request_key": "owner-bug-s2r"}, &s.proposal)
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
		t.Fatalf("the process was not killed before the analyst attempt; logs=%s", f.logs)
	}
	if s.handOffs.Load() != 1 {
		t.Fatalf("hand-offs before the kill=%d, want 1", s.handOffs.Load())
	}
	filings, err := f.db.ActiveBugFilings(ctx, 10)
	btCheck(t, err)
	if len(filings) != 1 || filings[0].State != store.BugFilingLabelled {
		t.Fatalf("filings at the kill=%+v", filings)
	}
	s.filing = filings[0]
	starts, err := f.db.BugFilingStartAttempts(ctx, s.filing.ID)
	btCheck(t, err)
	if len(starts) != 1 || starts[0].State != store.BugStartRunning {
		t.Fatalf("start attempts at the kill=%+v", starts)
	}
	s.attempt, s.trigger = starts[0], killed.Load()
	if !store.IsBugStartTrigger(s.trigger) || s.trigger != s.attempt.TriggerID() {
		t.Fatalf("killed run trigger %d is not the start attempt's private trigger %d", s.trigger, s.attempt.TriggerID())
	}
	// At the kill: the run still marked running, the conversation done and
	// moved to the analyst, nothing else.
	runs, err := f.db.ListRuns(ctx, s.filing.SessionID)
	btCheck(t, err)
	if len(runs) != 1 || runs[0].Status != store.RunRunning || s.runner.analysts != 0 {
		t.Fatalf("runs at the kill=%+v analysts=%d", runs, s.runner.analysts)
	}
	return s
}

// restart builds the next process: a new daemon with a new instance id, its
// own Bug runtime and attempt authority, and its own owner-action service and
// connector over the same database.
func (s *s2rRun) restart(t *testing.T) *turn.BugRuntime {
	t.Helper()
	bug := s2rBugRuntime(t, s.f, s.repo, s.runner, s2rRestarted, time.Now)
	s.harness = s.f.daemon.EnableBugFilingForTest(s.cfg, s.f.clock.Now)
	s.call = s2rConnector(t, s.f, s.cfg, s.harness.Actions, s.token)
	return bug
}

// assertRetained checks the run-before-analyst state after orphan cleanup.
func (s *s2rRun) assertRetained(t *testing.T, instance store.GraphInstance) {
	t.Helper()
	ctx := t.Context()
	if instance.Status != store.GraphRunning || instance.CurrentNode != "analyst" {
		t.Fatalf("instance=%+v", instance)
	}
	runs, err := s.f.db.ListRuns(ctx, s.filing.SessionID)
	btCheck(t, err)
	// Orphan cleanup failed the run as abandoned; once its failure report is
	// delivered the status reads reported and the error stays.
	if len(runs) != 1 || (runs[0].Status != store.RunFailed && runs[0].Status != store.RunReported) || runs[0].Error != store.ReasonAbandoned || runs[0].Succeeded() || runs[0].TriggerCommentID != s.trigger {
		t.Fatalf("runs=%+v", runs)
	}
	if launched, found, err := s.f.db.WorkflowLaunchInstance(ctx, s.filing.SessionID, s.trigger); err != nil || !found || launched != instance.ID {
		t.Fatalf("the private start trigger is not the instance's launch: %d %t %v", launched, found, err)
	}
	if _, err = s.f.db.GetGraphOwnerWait(ctx, instance.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("owner wait parked: %v", err)
	}
	attempts, err := s.f.db.ListGraphNodeAttempts(ctx, instance.ID)
	btCheck(t, err)
	if len(attempts) != 1 || attempts[0].Node != "conversation" || attempts[0].Status != store.GraphSucceeded {
		t.Fatalf("attempts=%+v", attempts)
	}
	transitions, err := s.f.db.ListGraphTransitions(ctx, instance.ID)
	btCheck(t, err)
	if len(transitions) != 1 || transitions[0].FromNode != "conversation" || transitions[0].ToNode != "analyst" {
		t.Fatalf("transitions=%+v", transitions)
	}
	if s.runner.analysts != 0 {
		t.Fatalf("analyst invoked %d times", s.runner.analysts)
	}
}

// assertNoNewStart checks the start step neither handed off again nor opened
// another attempt: the killed attempt is settled from its run.
func (s *s2rRun) assertNoNewStart(t *testing.T) {
	t.Helper()
	if s.handOffs.Load() != 1 {
		t.Fatalf("the start step handed off again after the restart: %d", s.handOffs.Load())
	}
	starts, err := s.f.db.BugFilingStartAttempts(t.Context(), s.filing.ID)
	btCheck(t, err)
	if len(starts) != 1 || starts[0].ID != s.attempt.ID || starts[0].Open() {
		t.Fatalf("start attempts after the restart=%+v", starts)
	}
	runs, err := s.f.db.ListRuns(t.Context(), s.filing.SessionID)
	btCheck(t, err)
	if len(runs) != 1 {
		t.Fatalf("the start step restarted the run: %+v", runs)
	}
}

// assertResumed checks the same Bug reached exactly one analyst attempt and
// the start step then settles the filing as dispatched.
func (s *s2rRun) assertResumed(t *testing.T, instance store.GraphInstance) {
	t.Helper()
	ctx := t.Context()
	instances, err := s.f.db.ListStandaloneBugInstances(ctx)
	btCheck(t, err)
	if len(instances) != 1 || instances[0].ID != instance.ID {
		t.Fatalf("instances=%+v, want only %d; logs=%s", instances, instance.ID, s.f.logs)
	}
	if previous, _, err := s.f.db.WorkflowPredecessor(ctx, instance.ID); err != nil || previous != 0 {
		t.Fatalf("successor recorded: %d %v", previous, err)
	}
	attempts, err := s.f.db.ListGraphNodeAttempts(ctx, instance.ID)
	btCheck(t, err)
	analysts := 0
	for _, a := range attempts {
		if a.Node == "analyst" {
			analysts++
		}
	}
	if analysts != 1 || s.runner.analysts != 1 {
		t.Fatalf("analyst attempts=%d analyst runs=%d attempts=%+v logs=%s", analysts, s.runner.analysts, attempts, s.f.logs)
	}
	s.harness.Step(ctx)
	btCheck(t, s.f.daemon.Quiesce(ctx))
	filing := s.readFiling(t)
	if filing.State != store.BugFilingDispatched || s.handOffs.Load() != 1 {
		t.Fatalf("filing=%+v hand-offs=%d", filing, s.handOffs.Load())
	}
	if result := s.inspect(t); result.State != "succeeded" || result.Summary != "Bug analysis started" || result.Reference != filing.IssueLink {
		t.Fatalf("result=%+v", result)
	}
}

func (s *s2rRun) readFiling(t *testing.T) store.BugFiling {
	t.Helper()
	filing, err := s.f.db.ReadBugFiling(t.Context(), s.filing.ID)
	btCheck(t, err)
	return filing
}

// inspect steps the owner action (its Recover) and reads it as the client.
func (s *s2rRun) inspect(t *testing.T) owneraction.Result {
	t.Helper()
	if _, err := s.harness.Actions.Step(t.Context(), s.proposal.ID); err != nil {
		t.Fatalf("action step: %v", err)
	}
	var inspected owneraction.Proposal
	s.call("action_inspect", map[string]any{"proposal_id": s.proposal.ID}, &inspected)
	return inspected.Result
}

// s2rRunningStartRun reports the trigger of a running run started by a Bug
// start trigger on any active filing's session.
func s2rRunningStartRun(db *store.DB) (int64, bool) {
	ctx := context.Background()
	filings, err := db.ActiveBugFilings(ctx, 10)
	if err != nil {
		return 0, false
	}
	for _, filing := range filings {
		if filing.SessionID == 0 {
			continue
		}
		runs, err := db.ListRuns(ctx, filing.SessionID)
		if err != nil {
			continue
		}
		for _, run := range runs {
			if run.Status == store.RunRunning && store.IsBugStartTrigger(run.TriggerCommentID) {
				return run.TriggerCommentID, true
			}
		}
	}
	return 0, false
}

// s2rConnector serves the owner's connector over one daemon's owner actions.
func s2rConnector(t *testing.T, f *fixture, cfg config.RemoteConnector, actions *owneraction.Service, token string) func(string, any, any) {
	t.Helper()
	ctx := t.Context()
	local, err := connector.NewLocalControl(ctx, connectorstorage.New(f.db), brainstormTasksControl{db: f.db, now: f.clock.Now})
	btCheck(t, err)
	server, err := connector.New(ctx, connector.Options{Config: cfg, Storage: connectorstorage.New(f.db), Access: connectorstorage.New(f.db), Logger: slog.New(slog.DiscardHandler), LocalControl: local, GitHubSecret: "fixture-secret", CookieKey: bytes.Repeat([]byte{7}, 32), Now: f.clock.Now, Actions: actions})
	btCheck(t, err)
	return brainstormTasksCaller(t, server.Handler(), cfg.ResourceURL, token)
}

// s2rBugRuntime is one daemon process: the real Bug graph runtime with the
// controlled Codex runner, its own attempt authority and instance id, and the
// given turn clock.
func s2rBugRuntime(t *testing.T, f *fixture, repo *gittest.Repo, runner *bugPathRunner, instanceID string, now func() time.Time) *turn.BugRuntime {
	t.Helper()
	ctx := t.Context()
	binary, err := git.Resolve(ctx)
	btCheck(t, err)
	home, err := os.MkdirTemp("/tmp", "lf-bug-s2r-")
	btCheck(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	home, err = filepath.EvalSymlinks(home)
	btCheck(t, err)
	paths := config.NewPaths(home)
	authority, err := turn.NewAttemptAuthority(ctx, f.db)
	btCheck(t, err)
	t.Cleanup(func() { _ = authority.Close(context.WithoutCancel(ctx)) })
	cfg := config.Defaults()
	claude, codexConfig := config.DefaultClaude(), config.DefaultCodex()
	cfg.Agents = config.Agents{Claude: &claude, Codex: &codexConfig}
	judge := &turn.JudgeService{DB: f.db, Config: &cfg, Providers: map[agent.Kind]agent.JudgeRunner{"codex": &bugPathJudge{controlledJudge: &controlledJudge{}}}, Paths: paths, Executable: "/usr/bin/false", Git: binary, Redactor: redact.New(redact.Options{Logger: f.logger})}
	runners := turn.RunnerSet{"codex": runner, "claude": bugPathUnexpectedRunner{}}
	opts := turn.Options{DB: f.db, Git: binary, Runners: runners, CloneGate: &bugPathGate{}, Paths: paths, InstanceID: instanceID, Author: git.Author{Name: "Fixture", Email: "fixture@example.invalid"}, Logger: f.logger, ResearchReporter: f.reporter, Judge: judge, ConflictContentMaxBytes: 65536, Tiers: cfg.Tiers,
		MCP: &turn.MCPRuntime{Authority: authority, Paths: paths, Executable: "/usr/bin/false", Now: now, Comments: f.client, BotID: botID, Redactor: redact.New(redact.Options{Logger: f.logger})}, WorkflowSource: bugPathWorkflowSource}
	bug, err := turn.NewGraphBugRuntime(ctx, opts, turn.BugRuntimeConfig{Code: &bugPathForge{f: f, repo: repo}, Tickets: &bugPathForge{f: f, repo: repo}, OwnerID: ownerID, Permissions: func(_ agent.Kind, role string) (turn.BugRolePermissions, error) {
		if role == "bug-fixer" {
			return turn.BugRolePermissions{Sandbox: "workspace-write"}, nil
		}
		return turn.BugRolePermissions{Sandbox: "read-only"}, nil
	}})
	btCheck(t, err)
	opts.Bug = bug
	f.turns.set(func(ctx context.Context, req turn.Request) (turn.Result, error) { return turn.Execute(ctx, opts, req) })
	f.daemon = f.newDaemon(daemon.Options{Bug: bug, BugAgent: "codex", Agents: runners, Judge: judge, InstanceID: instanceID})
	return bug
}
