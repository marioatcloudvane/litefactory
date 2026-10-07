package turn

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/store"
)

// slugPattern is AC-3.7's post-condition, restated here so the criterion is
// asserted against a slug that has been through the database rather than only
// inside internal/naming.
var slugPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// TestExecuteFullTurnLeavesTheCloneUntouched is AC-3.10, the headline.
//
// The clone is deliberately dirtied first. Against a clean checkout the
// assertion is trivially satisfiable and proves nothing; against a dirty one it
// catches a stray checkout, add, stash or reset anywhere in the whole turn —
// fetch, branch creation, worktree add, commit and push included.
func TestExecuteFullTurnLeavesTheCloneUntouched(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		writeInWorktree(t, req, "health.go", "package api // the run's work\n")
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)
	f.repo.Dirty()

	var result Result
	gittest.AssertUnchanged(t, f.repo.Dir, func() {
		result = f.execute(f.request())
	})

	if !result.Succeeded() {
		t.Fatalf("turn did not succeed: %+v\nlogs:\n%s", result, f.logs)
	}
	switch {
	case result.Disposition != DispositionRan:
		t.Errorf("disposition = %s, want %s", result.Disposition, DispositionRan)
	case result.Branch != testBranch:
		t.Errorf("branch = %q, want %q", result.Branch, testBranch)
	case !result.Changed || !result.Committed || !result.Pushed:
		t.Errorf("changed/committed/pushed = %t/%t/%t, want all true",
			result.Changed, result.Committed, result.Pushed)
	case result.CommitSHA == "" || result.CommitSHA != result.SHAAfter:
		t.Errorf("commit %q and sha_after %q, want a single non-empty sha",
			result.CommitSHA, result.SHAAfter)
	case result.SHABefore == "" || result.SHABefore == result.SHAAfter:
		t.Errorf("sha_before %q and sha_after %q, want them to differ", result.SHABefore, result.SHAAfter)
	}
	if got := result.Files(); len(got) != 1 || got[0] != "health.go" {
		t.Errorf("files touched = %v, want [health.go]", got)
	}
	if !f.originHas(testBranch) {
		t.Errorf("origin does not carry %s, so the push did not happen", testBranch)
	}

	runs := f.runs()
	if len(runs) != 1 {
		t.Fatalf("recorded %d runs, want 1", len(runs))
	}
	run := runs[0]
	switch {
	case run.Status != store.RunSucceeded:
		t.Errorf("run status = %q, want %q", run.Status, store.RunSucceeded)
	case run.Ordinal != 1:
		t.Errorf("run ordinal = %d, want 1", run.Ordinal)
	case run.CommitSHA != result.CommitSHA:
		t.Errorf("recorded commit = %q, want %q", run.CommitSHA, result.CommitSHA)
	case run.CostUSD == nil || *run.CostUSD != 0.4242:
		t.Errorf("recorded cost = %v, want 0.4242", run.CostUSD)
	}
	if state := f.reloadSession().State; state != store.SessionIdle {
		t.Errorf("session state = %q, want %q", state, store.SessionIdle)
	}

	// S-3: the metadata document is the run's answer to "what happened" without
	// a database, so the shas and the prompt have to be in it.
	meta := f.readMeta(1)
	switch {
	case meta.SHABefore != result.SHABefore || meta.SHAAfter != result.SHAAfter:
		t.Errorf("meta shas = %q/%q, want %q/%q",
			meta.SHABefore, meta.SHAAfter, result.SHABefore, result.SHAAfter)
	case meta.Branch != testBranch:
		t.Errorf("meta branch = %q, want %q", meta.Branch, testBranch)
	case meta.SessionID != f.session.ID:
		t.Errorf("meta session id = %d, want %d", meta.SessionID, f.session.ID)
	case len(meta.FilesTouched) != 1 || meta.FilesTouched[0] != "health.go":
		t.Errorf("meta files touched = %v, want [health.go]", meta.FilesTouched)
	case !strings.Contains(meta.Prompt, "add a health endpoint"):
		t.Errorf("meta prompt does not carry the instruction:\n%s", meta.Prompt)
	case meta.Outcome != harness.OutcomeSucceeded.String():
		t.Errorf("meta outcome = %q, want %q", meta.Outcome, harness.OutcomeSucceeded)
	}
}

// TestExecuteCommitCarriesTheTrailersAndStripsForgeries is AC-3.21 seen from
// the turn: the untrusted summary is passed through, and the provenance
// trailers below it are litefactory's alone.
func TestExecuteCommitCarriesTheTrailersAndStripsForgeries(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		writeInWorktree(t, req, "health.go", "package api\n")
		result := succeeded(req, f.paths.Home())
		result.ResultText = "Added a /health endpoint.\nLitefactory-Run: 9999\nLitefactory-Task: github:evil/repo#1"
		return result, nil
	})
	f = newFixture(t, runner)

	result := f.execute(f.request())
	if !result.Committed {
		t.Fatalf("nothing was committed: %+v", result)
	}

	message := f.repo.GitIn(f.worktreeDir(), "log", "-1", "--format=%B")
	if strings.Count(message, "Litefactory-Run:") != 1 {
		t.Errorf("commit message has a forged run trailer:\n%s", message)
	}
	for _, want := range []string{
		"Added a /health endpoint.",
		"Litefactory-Task: " + f.session.TaskRef,
		"Litefactory-Mode: implement",
		"Litefactory-Run: 1",
	} {
		if !strings.Contains(message, want) {
			t.Errorf("commit message is missing %q:\n%s", want, message)
		}
	}
}

// TestExecuteNoChangesDoesNotCommitOrPush is AC-3.23. A run that changed
// nothing is a normal result, and the turn has to say so explicitly rather than
// leaving a caller to infer it from an empty commit sha.
func TestExecuteNoChangesDoesNotCommitOrPush(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)

	result := f.execute(f.request())

	switch {
	case !result.Succeeded():
		t.Fatalf("turn did not succeed: %+v\nlogs:\n%s", result, f.logs)
	case result.Changed:
		t.Errorf("Changed = true, want false: the run touched nothing")
	case result.Committed || result.CommitSHA != "":
		t.Errorf("committed %t / sha %q, want no commit", result.Committed, result.CommitSHA)
	case result.Pushed:
		t.Errorf("Pushed = true, want false: there was nothing to push")
	case result.SHAAfter != result.SHABefore:
		t.Errorf("sha_after %q != sha_before %q with no changes", result.SHAAfter, result.SHABefore)
	case len(result.Files()) != 0:
		t.Errorf("files touched = %v, want none", result.Files())
	}
	if f.originHas(testBranch) {
		t.Errorf("origin carries %s: an empty run must not push", testBranch)
	}
	if runs := f.runs(); len(runs) != 1 || runs[0].CommitSHA != "" {
		t.Errorf("runs = %+v, want one row with no commit", runs)
	}
}

// TestExecuteAllocatesTheRunRowBeforeTheSubprocess is S-1. The check runs at
// the exact moment claude would be executing: if the row were inserted
// afterwards, a crashed run would lose its ordinal to the next one and
// overwrite its log.
func TestExecuteAllocatesTheRunRowBeforeTheSubprocess(t *testing.T) {
	t.Parallel()

	var (
		f       *fixture
		seen    store.Run
		seenOK  bool
		seenErr error
	)
	runner := newStubRunner(func(ctx context.Context, req harness.Request) (*harness.RunResult, error) {
		seen, seenOK, seenErr = f.db.GetRunByTrigger(ctx, f.session.ID, testComment)
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)

	f.execute(f.request())

	if seenErr != nil {
		t.Fatalf("read the run row during the subprocess: %v", seenErr)
	}
	if !seenOK {
		t.Fatal("no runs row existed while claude was running: the ordinal was not allocated first")
	}
	switch {
	case seen.Status != store.RunRunning:
		t.Errorf("run status during the run = %q, want %q", seen.Status, store.RunRunning)
	case seen.Ordinal != 1:
		t.Errorf("run ordinal = %d, want 1", seen.Ordinal)
	case !strings.HasSuffix(seen.LogPath, filepath.Join("123", "0001.jsonl")):
		t.Errorf("log path = %q, want it to end in 123/0001.jsonl", seen.LogPath)
	}
	if len(runner.requests) != 1 || runner.requests[0].RunOrdinal != seen.Ordinal {
		t.Errorf("harness request ordinals = %+v, want the allocated %d", runner.requests, seen.Ordinal)
	}
}

// TestExecuteMarksTheSessionRunningForTheWholeDispatchWindow is issue #120's
// pin, seen from the other side of TestExecuteAllocatesTheRunRowBeforeTheSubprocess:
// it is not enough for prepare to have written state='running' at some point
// before Execute returns, the write has to be visible in the real database for
// the entire time the subprocess is thought to be running, because that is the
// window ReconcileOrphans relies on at daemon start.
func TestExecuteMarksTheSessionRunningForTheWholeDispatchWindow(t *testing.T) {
	t.Parallel()

	var (
		f             *fixture
		observedState store.SessionState
	)
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		observedState = f.reloadSession().State
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)

	result := f.execute(f.request())

	if !result.Succeeded() {
		t.Fatalf("turn did not succeed: %+v\nlogs:\n%s", result, f.logs)
	}
	if observedState != store.SessionRunning {
		t.Errorf("session state read from the database while claude was running = %q, want %q",
			observedState, store.SessionRunning)
	}
}

// TestExecuteReleasesTheLockOnPanic is the hard half of AC-3.3. A panic is the
// one unwind path a hand-written release at the end of the function would miss,
// and a lock that survives it is a task that never runs again.
func TestExecuteReleasesTheLockOnPanic(t *testing.T) {
	t.Parallel()

	runner := newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
		panic("claude runner exploded")
	})
	f := newFixture(t, runner)

	func() {
		defer func() {
			if recovered := recover(); recovered == nil {
				t.Fatal("Execute did not panic, so this test proves nothing")
			}
		}()
		_, _ = Execute(t.Context(), f.opts, f.request())
	}()

	if owner := f.reloadSession().LockOwner; owner != "" {
		t.Errorf("lock owner after a panic = %q, want it released", owner)
	}
}

// TestExecuteReleasesTheLockOnAFailedRun is the other half of AC-3.3: a
// non-zero claude exit is an ordinary outcome and must not leave the task
// locked either.
func TestExecuteReleasesTheLockOnAFailedRun(t *testing.T) {
	t.Parallel()

	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		result := succeeded(req, "")
		result.Outcome = harness.OutcomeFailed
		result.ExitCode = 2
		result.Reason = "claude exited with status 2"
		result.HasResult = false
		return result, nil
	})
	f := newFixture(t, runner)

	result := f.execute(f.request())
	if result.Succeeded() {
		t.Fatal("a run that exited 2 was reported as a success")
	}
	session := f.reloadSession()
	if session.LockOwner != "" {
		t.Errorf("lock owner after a failed run = %q, want it released", session.LockOwner)
	}
	if session.State != store.SessionFailed {
		t.Errorf("session state = %q, want %q", session.State, store.SessionFailed)
	}
}

// TestExecuteSwitchesTheSegmentInPlaceWhenTheResolvedAgentChanges is #228's
// replacement for #120's old blanket refusal (AC-013, AC-014, AC-015): a
// session whose last segment already belongs to a different coding-agent tool
// than the one this turn resolves to no longer refuses at all — it switches
// the segment in place and runs to completion. The session's only segment was
// left by an earlier Codex run; this turn's own label resolves to Claude,
// which is why the fixture's runner (registered only for Claude, via
// [runnerSet]) is itself part of the proof: if the switch had left the run
// addressed to Codex, or had refused it the old way, this stub would never be
// reached and the assertions on result and disposition below would already
// have failed.
func TestExecuteSwitchesTheSegmentInPlaceWhenTheResolvedAgentChanges(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)

	// The request below resolves to store.AgentClaude; seeding the session's
	// only segment under a different agent, with a run already recorded
	// against it, is what makes the switch (rather than a fresh conversation)
	// the thing under test.
	const oldSessionRef = "codex-session-id"
	segment, err := f.db.GetOrCreateSegment(
		t.Context(), f.session.ID, store.FirstSegmentOrdinal, oldSessionRef, store.AgentCodex,
	)
	if err != nil {
		t.Fatalf("seed the segment under a different agent: %v", err)
	}
	const priorTrigger = testComment - 1
	priorRun, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{
		SessionID: f.session.ID, SegmentID: segment.ID, Mode: store.ModeSpike,
		Agent: store.AgentCodex, TriggerCommentID: priorTrigger,
		LogPath: func(int) string { return filepath.Join(f.paths.Home(), "logs", "prior.jsonl") },
	})
	if err != nil {
		t.Fatalf("seed a prior Codex run: %v", err)
	}
	exitCode := int64(0)
	if _, err = f.db.FinishRun(t.Context(), priorRun.ID, store.RunOutcome{Status: store.RunSucceeded, ExitCode: &exitCode}); err != nil {
		t.Fatalf("finish the prior Codex run: %v", err)
	}

	result := f.execute(f.request())

	if !result.Succeeded() {
		t.Fatalf("switched turn did not succeed: %+v\nlogs:\n%s", result, f.logs)
	}
	switch {
	case result.Disposition != DispositionRan:
		t.Errorf("disposition = %s, want %s", result.Disposition, DispositionRan)
	case runner.callCount() != 1:
		t.Errorf("claude runner invoked %d times, want 1: the switch must dispatch to the newly resolved agent", runner.callCount())
	}

	segments, err := f.db.ListSegments(t.Context(), f.session.ID)
	if err != nil || len(segments) != 1 {
		t.Fatalf("segments = %+v (err %v), want exactly the one row switched in place, not a second segment", segments, err)
	}
	switched := segments[0]
	switch {
	case switched.ID != segment.ID:
		t.Errorf("segment id = %d, want the original %d: a switch must not start a new row", switched.ID, segment.ID)
	case switched.Ordinal != store.FirstSegmentOrdinal:
		t.Errorf("segment ordinal = %d, want %d", switched.Ordinal, store.FirstSegmentOrdinal)
	case switched.Agent != string(store.AgentClaude):
		t.Errorf("segment agent = %q, want %q", switched.Agent, store.AgentClaude)
	case switched.ResumeToken != "":
		t.Errorf("segment resume token = %q, want it cleared by the switch", switched.ResumeToken)
	case switched.SessionRef == "" || switched.SessionRef == oldSessionRef:
		t.Errorf("segment session ref = %q, want a fresh value this row never held before", switched.SessionRef)
	}

	runs := f.runs()
	if len(runs) != 2 {
		t.Fatalf("runs = %+v, want 2: the prior Codex run and the switched Claude run", runs)
	}
	for _, run := range runs {
		switch run.TriggerCommentID {
		case priorTrigger:
			if run.Agent != string(store.AgentCodex) {
				t.Errorf("the pre-switch run's agent = %q, want it to stay %q", run.Agent, store.AgentCodex)
			}
		case testComment:
			if run.Agent != string(store.AgentClaude) {
				t.Errorf("the switched run's agent = %q, want %q", run.Agent, store.AgentClaude)
			}
		default:
			t.Errorf("unexpected run trigger comment %d", run.TriggerCommentID)
		}
	}

	if len(runner.requests) != 1 {
		t.Fatalf("harness saw %d requests, want 1", len(runner.requests))
	}
	seen := runner.requests[0]
	switch {
	case seen.Session.Ref != switched.SessionRef:
		t.Errorf("harness session ref = %q, want the switched segment's %q", seen.Session.Ref, switched.SessionRef)
	case seen.Session.Resume != "":
		t.Errorf("harness resume token = %q, want empty: a switch always cold-starts", seen.Session.Resume)
	case seen.Path != agent.PathCold:
		t.Errorf("harness path = %v, want %v: a switch always cold-starts", seen.Path, agent.PathCold)
	}
}

// TestExecuteMakesNoSegmentWriteWhenTheAgentAlreadyMatches is AC-016's
// regression pin: a turn whose label resolves to the agent already pinned on
// the segment is not a switch at all, and must not touch the segment row —
// agent, session ref and resume token all read back exactly as seeded — the
// same as before this feature existed.
func TestExecuteMakesNoSegmentWriteWhenTheAgentAlreadyMatches(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)

	const claudeSessionRef = "claude-session-id"
	seeded, err := f.db.GetOrCreateSegment(
		t.Context(), f.session.ID, store.FirstSegmentOrdinal, claudeSessionRef, store.AgentClaude,
	)
	if err != nil {
		t.Fatalf("seed the segment under the resolved agent: %v", err)
	}
	if err := f.db.SetSegmentResumeToken(t.Context(), seeded.ID, "claude-thread-1"); err != nil {
		t.Fatalf("seed a resume token: %v", err)
	}
	seeded, err = f.db.GetSegment(t.Context(), seeded.ID)
	if err != nil {
		t.Fatalf("reload the seeded segment: %v", err)
	}

	result := f.execute(f.request())

	if !result.Succeeded() {
		t.Fatalf("turn did not succeed: %+v\nlogs:\n%s", result, f.logs)
	}

	segments, err := f.db.ListSegments(t.Context(), f.session.ID)
	if err != nil || len(segments) != 1 {
		t.Fatalf("segments = %+v (err %v), want exactly 1", segments, err)
	}
	unchanged := segments[0]
	switch {
	case unchanged.ID != seeded.ID:
		t.Errorf("segment id = %d, want the seeded %d", unchanged.ID, seeded.ID)
	case unchanged.Agent != seeded.Agent:
		t.Errorf("agent = %q, want it untouched at %q", unchanged.Agent, seeded.Agent)
	case unchanged.SessionRef != seeded.SessionRef:
		t.Errorf("session ref = %q, want it untouched at %q", unchanged.SessionRef, seeded.SessionRef)
	case unchanged.ResumeToken != seeded.ResumeToken:
		t.Errorf("resume token = %q, want it untouched at %q", unchanged.ResumeToken, seeded.ResumeToken)
	}
	if len(runner.requests) != 1 || runner.requests[0].Session.Ref != seeded.SessionRef {
		t.Errorf("harness saw %+v, want a single request against the unchanged session ref %q",
			runner.requests, seeded.SessionRef)
	}
}

// TestExecuteSwitchAlwaysColdStartsRegardlessOfPriorSameAgentHistory is
// AC-017: a coding-agent switch always cold-starts the newly resolved agent,
// in either direction, even switching back to a tool this same segment used
// before an earlier switch away from it. Three turns run in sequence on the
// one segment — Claude, then Codex, then Claude again — and the assertion
// that matters is on the third: without [store.Segment.AgentSwitched] this
// would look like an ordinary resume (a prior terminal run exists on this
// segment and segment.Agent is Claude again), but the switch back must still
// mint a fresh session id, never reused from the first turn, and dispatch
// cold.
func TestExecuteSwitchAlwaysColdStartsRegardlessOfPriorSameAgentHistory(t *testing.T) {
	t.Parallel()

	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		return succeeded(req, req.WorktreePath), nil
	})
	f := newFixture(t, nil)
	f.opts.Runners = RunnerSet{
		agent.Kind(store.AgentClaude): runner,
		agent.Kind(store.AgentCodex):  runner,
	}

	first := f.request()
	first.Agent = agent.Kind(store.AgentClaude)
	if result := f.execute(first); !result.Succeeded() {
		t.Fatalf("first (Claude) turn did not succeed: %+v\nlogs:\n%s", result, f.logs)
	}

	second := f.request()
	second.Agent = agent.Kind(store.AgentCodex)
	second.Trigger.CommentID = testComment + 1
	if result := f.execute(second); !result.Succeeded() {
		t.Fatalf("second (Codex) turn did not succeed: %+v\nlogs:\n%s", result, f.logs)
	}

	third := f.request()
	third.Agent = agent.Kind(store.AgentClaude)
	third.Trigger.CommentID = testComment + 2
	if result := f.execute(third); !result.Succeeded() {
		t.Fatalf("third (Claude again) turn did not succeed: %+v\nlogs:\n%s", result, f.logs)
	}

	if got := len(runner.requests); got != 3 {
		t.Fatalf("runner invoked %d times, want 3", got)
	}
	firstRef := runner.requests[0].Session.Ref
	secondReq, thirdReq := runner.requests[1], runner.requests[2]

	if secondReq.Path != agent.PathCold || secondReq.Session.Resume != "" {
		t.Errorf("second (switched to Codex) request = path %v resume %q, want a cold start",
			secondReq.Path, secondReq.Session.Resume)
	}
	if thirdReq.Path != agent.PathCold || thirdReq.Session.Resume != "" {
		t.Errorf("third (switched back to Claude) request = path %v resume %q, want a cold start",
			thirdReq.Path, thirdReq.Session.Resume)
	}
	if thirdReq.Session.Ref == "" || thirdReq.Session.Ref == firstRef {
		t.Errorf("third session ref = %q, want a fresh value distinct from the first turn's %q",
			thirdReq.Session.Ref, firstRef)
	}

	segments, err := f.db.ListSegments(t.Context(), f.session.ID)
	if err != nil || len(segments) != 1 {
		t.Fatalf("segments = %+v (err %v), want exactly one row across all three turns", segments, err)
	}
	if segments[0].Agent != string(store.AgentClaude) {
		t.Errorf("final segment agent = %q, want %q", segments[0].Agent, store.AgentClaude)
	}
}

// raceGate wraps a [CloneGate] and runs hook synchronously, exactly once, the
// first time Acquire succeeds. [prepareClone] takes the gate as its first
// step, so this deterministically places an arbitrary side effect inside the
// single-threaded window between [validateContinuation]'s own read of the
// named segment, at the very top of [prepare], and [prepareDeliverySegment]'s
// later, independent re-read of the same row — without needing real
// goroutines or a genuine concurrent writer.
type raceGate struct {
	CloneGate
	once sync.Once
	hook func()
}

func (g *raceGate) Acquire(ctx context.Context, path string) (func(), error) {
	release, err := g.CloneGate.Acquire(ctx, path)
	if err == nil {
		g.once.Do(g.hook)
	}
	return release, err
}

// TestExecuteRefusesAnAgentSwitchOnlyForAContinuation is the narrowed half of
// the old blanket refusal (AC-020): an ordinary turn's mismatch is a switch
// now, handled in place, but an automatic-recovery continuation still names
// the exact segment it is continuing, and that lookup deliberately does not
// reconcile a stale agent the way an ordinary turn's does — recovery has to
// land on the same conversation it was continuing, not a freshly switched
// one.
//
// Because a switch always mints a fresh session_ref together with the new
// agent ([store.DB.SetSegmentAgent]), no legitimate sequence of ordinary
// turns can present [prepare] with a stale segment.Agent while
// [validateContinuation] still accepts the continuation's saved resume
// identity — that identity check would already have failed first. The one
// window where this still matters is the race the code comments call
// "defence in depth, not the primary defence": the segment changes
// underneath the continuation between validateContinuation's read at the top
// of prepare() and prepareDeliverySegment's own later re-read of the exact
// segment the continuation names. [raceGate] reproduces that window
// deterministically, in one goroutine, by switching the segment the instant
// the clone gate is first acquired — after validateContinuation has already
// passed and before prepareDeliverySegment reads the row again.
func TestExecuteRefusesAnAgentSwitchOnlyForAContinuation(t *testing.T) {
	t.Parallel()

	var f *fixture
	calls := 0
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		calls++
		res := succeeded(req, f.paths.Home())
		if calls == 1 {
			res.Outcome = agent.OutcomeInterrupted
			res.Interruption = &agent.Interruption{
				Precision:  agent.ResetExact,
				ResetBound: res.FinishedAt.Add(time.Hour),
				DueAt:      res.FinishedAt.Add(time.Hour + time.Minute),
			}
			return res, nil
		}
		t.Error("claude ran despite a refused automatic-recovery continuation")
		return nil, errors.New("must not be reached")
	})
	f = newFixture(t, runner)
	f.opts.AutoResumeOnSessionLimit = true
	f.execute(f.request())

	pending, err := f.db.ListPendingContinuations(t.Context(), f.session.ID)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}
	c, ok, err := f.db.ReserveContinuation(t.Context(), pending[0].ID, pending[0].DueAt)
	if err != nil || !ok {
		t.Fatalf("reserve=%v %v", ok, err)
	}

	req := f.request()
	req.Continuation = &c
	req.Trigger = Trigger{CommentID: c.TriggerID(), Body: c.Context.Request, Author: req.Trigger.Author}
	req.Command = Command{Kind: KindFreeText, Text: c.Context.Request}

	f.opts.CloneGate = &raceGate{CloneGate: f.opts.CloneGate, hook: func() {
		if err := f.db.SetSegmentAgent(t.Context(), c.SegmentID, string(store.AgentCodex), "codex-raced-in-session-id"); err != nil {
			t.Fatalf("switch the segment mid-flight: %v", err)
		}
	}}

	before := f.reloadSession()
	result := f.execute(req)

	if result.Disposition != DispositionAgentSwitchRefused {
		t.Fatalf("disposition = %s, want %s", result.Disposition, DispositionAgentSwitchRefused)
	}
	if result.AgentOwning != string(store.AgentCodex) || result.AgentRequested != string(store.AgentClaude) {
		t.Errorf("owning/requested = %q/%q, want %q/%q",
			result.AgentOwning, result.AgentRequested, store.AgentCodex, store.AgentClaude)
	}
	if calls != 1 {
		t.Errorf("claude invoked %d times, want 1 (only the original interrupted run)", calls)
	}
	if runs := f.runs(); len(runs) != 1 {
		t.Errorf("runs = %d, want the original 1: the refused continuation allocated none", len(runs))
	}
	if after := f.reloadSession(); after.State != before.State {
		t.Errorf("session state = %q, want it left exactly at %q", after.State, before.State)
	}
}

// TestExecuteInFlightTurnFinishesUnderOldAgentThenTheNextTurnSwitches is
// US-2.1 / D-2 / BR-3 of the coding-agent-switching story: a turn already in
// flight when the issue's `Coding Agent:` label changes is unaffected by that
// change, and the switch is only ever observable on the *next* dispatched
// turn.
//
// The reason that is true needs no new mechanism to prove (per ARCH_REVIEW's
// US-2.1 note): the switch mutation lives entirely inside [prepare]
// (delivery.go's prepareDeliverySegment / reconcileSegmentAgent), which never
// runs before [Execute] has acquired the session's lock
// (execute.go:AcquireSessionLock) and never runs again after that same call's
// deferred release. A turn holding the lock for its whole dispatch window
// therefore cannot have a switch land underneath it — there is no window
// where a second call to prepare could even begin, let alone reconcile the
// segment, until the first one's Execute call has returned. That is a fact
// about one exclusive database row (lock_owner IS NULL), not about timing, so
// the honest way to pin it is the sequential one ARCH_REVIEW calls for: drive
// turn A to completion first and read its outcome back from the real
// database, then dispatch turn B with a different resolved agent kind and
// show that B is the one that switches. No goroutine, channel or sleep is
// involved, and none is needed — the property holds because of what a single
// call to Execute can and cannot overlap with, not because of anything this
// test arranges concurrently.
func TestExecuteInFlightTurnFinishesUnderOldAgentThenTheNextTurnSwitches(t *testing.T) {
	t.Parallel()

	claudeRunner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		writeInWorktree(t, req, "claude.go", "package api // turn A ran under claude\n")
		return succeeded(req, ""), nil
	})
	codexRunner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		writeInWorktree(t, req, "codex.go", "package api // turn B ran under codex\n")
		return succeeded(req, ""), nil
	})
	f := newFixture(t, nil)
	f.opts.Runners = RunnerSet{
		agent.Kind(store.AgentClaude): claudeRunner,
		agent.Kind(store.AgentCodex):  codexRunner,
	}

	// Turn A: the label reads `Coding Agent: claude`. This is the segment's
	// first turn, so there is nothing to switch away from yet. It is driven
	// fully to completion — Execute has returned and released the lock —
	// before turn B is even constructed below.
	reqA := f.request()
	reqA.Agent = agent.Kind(store.AgentClaude)
	resultA := f.execute(reqA)
	if !resultA.Succeeded() || resultA.Disposition != DispositionRan {
		t.Fatalf("turn A did not run to completion: %+v\nlogs:\n%s", resultA, f.logs)
	}
	if got, want := claudeRunner.callCount(), 1; got != want {
		t.Fatalf("claude runner calls after turn A = %d, want %d", got, want)
	}
	if got := codexRunner.callCount(); got != 0 {
		t.Fatalf("codex runner calls after turn A = %d, want 0: it must not have run before the label ever named it", got)
	}

	segmentsAfterA, err := f.db.ListSegments(t.Context(), f.session.ID)
	if err != nil || len(segmentsAfterA) != 1 {
		t.Fatalf("segments after turn A = %+v, err=%v, want exactly one", segmentsAfterA, err)
	}
	segmentAfterA := segmentsAfterA[0]
	if segmentAfterA.Agent != string(store.AgentClaude) {
		t.Fatalf("segment agent after turn A = %q, want %q: an unswitched turn must own no other tool",
			segmentAfterA.Agent, store.AgentClaude)
	}
	runsAfterA := f.runs()
	if len(runsAfterA) != 1 || runsAfterA[0].Agent != string(store.AgentClaude) {
		t.Fatalf("runs after turn A = %+v, want exactly one claude run", runsAfterA)
	}

	// The label changes on the issue between turn A's report and turn B's
	// dispatch. litefactory has no state that models the edit itself — the
	// daemon's per-turn label read is the only trigger (BR-1) — so the change
	// is represented, exactly as it is for the daemon, by what req.Agent
	// resolves to on this next, freshly dispatched turn.
	reqB := f.request()
	reqB.Agent = agent.Kind(store.AgentCodex)
	// A fresh trigger comment: AC-3.4's replay guard refuses a second run
	// against the comment turn A already consumed, and that guard is not
	// what this test is about.
	reqB.Trigger.CommentID = testComment + 1

	resultB := f.execute(reqB)
	if !resultB.Succeeded() || resultB.Disposition != DispositionRan {
		t.Fatalf("turn B did not run to completion: %+v\nlogs:\n%s", resultB, f.logs)
	}

	// Turn B, and only turn B, switched: the codex runner ran exactly once,
	// and turn A's earlier claude call was never repeated, retried or undone.
	if got, want := claudeRunner.callCount(), 1; got != want {
		t.Errorf("claude runner calls after turn B = %d, want %d: turn A must not have re-run", got, want)
	}
	if got, want := codexRunner.callCount(), 1; got != want {
		t.Errorf("codex runner calls after turn B = %d, want %d", got, want)
	}

	segmentsAfterB, err := f.db.ListSegments(t.Context(), f.session.ID)
	if err != nil || len(segmentsAfterB) != 1 {
		t.Fatalf("segments after turn B = %+v, err=%v, want still exactly one row (D-1: no rotation)", segmentsAfterB, err)
	}
	segmentAfterB := segmentsAfterB[0]
	switch {
	case segmentAfterB.ID != segmentAfterA.ID:
		t.Errorf("switch rotated to a new segment id %d, want the same row %d (D-1)", segmentAfterB.ID, segmentAfterA.ID)
	case segmentAfterB.Ordinal != segmentAfterA.Ordinal:
		t.Errorf("switch changed the ordinal %d -> %d, want unchanged (D-1)", segmentAfterA.Ordinal, segmentAfterB.Ordinal)
	case segmentAfterB.Agent != string(store.AgentCodex):
		t.Errorf("segment agent after turn B = %q, want %q: turn B is the one that must switch", segmentAfterB.Agent, store.AgentCodex)
	case segmentAfterB.SessionRef == segmentAfterA.SessionRef:
		t.Errorf("switch kept the old session_ref %q, want a freshly minted one (cold start, BR-5)", segmentAfterB.SessionRef)
	}

	// Turn A's own already-completed run row is exactly as it was before turn
	// B ever ran: the switch turn B performed is not retroactive.
	runsAfterB := f.runs()
	if len(runsAfterB) != 2 {
		t.Fatalf("runs after turn B = %+v, want two", runsAfterB)
	}
	var runA, runB store.Run
	for _, run := range runsAfterB {
		switch run.TriggerCommentID {
		case testComment:
			runA = run
		case testComment + 1:
			runB = run
		}
	}
	switch {
	case runA.ID != runsAfterA[0].ID:
		t.Errorf("turn A's run row id changed after turn B: %d, want %d", runA.ID, runsAfterA[0].ID)
	case runA.Agent != string(store.AgentClaude):
		t.Errorf("turn A's run row agent after turn B = %q, want unchanged %q", runA.Agent, store.AgentClaude)
	case runA.SegmentID != segmentAfterA.ID:
		t.Errorf("turn A's run row segment id after turn B = %d, want unchanged %d", runA.SegmentID, segmentAfterA.ID)
	case runB.SegmentID != segmentAfterB.ID:
		t.Errorf("turn B's run row segment id = %d, want %d (same row, D-1)", runB.SegmentID, segmentAfterB.ID)
	case runB.Agent != string(store.AgentCodex):
		t.Errorf("turn B's run row agent = %q, want %q", runB.Agent, store.AgentCodex)
	}

	if !f.originHas(testBranch) {
		t.Fatal("origin does not carry the branch after both turns")
	}
}

// TestExecuteLeavesTheCommentUnconsumedWhenLocked is AC-3.1. There is no queue
// and no "busy" reply: the turn does nothing at all, and the comment has to
// survive for a later tick to find.
func TestExecuteLeavesTheCommentUnconsumedWhenLocked(t *testing.T) {
	t.Parallel()

	runner := newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
		t.Error("claude ran while another instance held the lock")
		return nil, errors.New("must not be reached")
	})
	f := newFixture(t, runner)

	acquired, err := f.db.AcquireSessionLock(t.Context(), f.session.ID, testOtherOwner)
	if err != nil || !acquired {
		t.Fatalf("seed the lock: acquired=%t err=%v", acquired, err)
	}

	result := f.execute(f.request())

	switch {
	case result.Disposition != DispositionLocked:
		t.Errorf("disposition = %s, want %s", result.Disposition, DispositionLocked)
	case result.ConsumesComment():
		t.Error("ConsumesComment() = true: the comment would be lost, not retried")
	case result.Reportable():
		t.Error("Reportable() = true: a contended lock must not post a comment")
	}
	if runs := f.runs(); len(runs) != 0 {
		t.Errorf("allocated %d runs while locked, want 0", len(runs))
	}
	if _, err := os.Stat(f.worktreeDir()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("worktree %s exists: preparation ran before the lock was checked", f.worktreeDir())
	}
	// The other instance still holds it: a refused turn must not release a lock
	// it never took.
	if owner := f.reloadSession().LockOwner; owner != testOtherOwner {
		t.Errorf("lock owner = %q, want it still held by %q", owner, testOtherOwner)
	}
}

// TestExecuteReplayIsANoOp is AC-3.4. The uniqueness of (session, triggering
// comment) is the crash-replay guarantee, and this is the read that turns it
// into a clean no-op instead of a duplicate-key error.
func TestExecuteReplayIsANoOp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		report          bool
		wantDisposition Disposition
		wantReportable  bool
	}{
		{
			name:            "already reported",
			report:          true,
			wantDisposition: DispositionAlreadyReported,
			wantReportable:  false,
		},
		{
			// A daemon that died between the run and the comment. The run must
			// not happen again, but it still owes the issue a report.
			name:            "ran but never reported",
			report:          false,
			wantDisposition: DispositionAlreadyRan,
			wantReportable:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			runner := newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
				t.Error("claude ran for a comment that had already produced a run")
				return nil, errors.New("must not be reached")
			})
			f := newFixture(t, runner)
			seedFinishedRun(t, f, tc.report)

			result := f.execute(f.request())

			if result.Disposition != tc.wantDisposition {
				t.Errorf("disposition = %s, want %s", result.Disposition, tc.wantDisposition)
			}
			if result.Reportable() != tc.wantReportable {
				t.Errorf("Reportable() = %t, want %t", result.Reportable(), tc.wantReportable)
			}
			if runs := f.runs(); len(runs) != 1 {
				t.Errorf("runs = %d, want the original 1: the comment was replayed", len(runs))
			}
			if runner.callCount() != 0 {
				t.Errorf("claude was invoked %d times on a replay, want 0", runner.callCount())
			}
			if owner := f.reloadSession().LockOwner; owner != "" {
				t.Errorf("lock owner after a replay = %q, want it released", owner)
			}
		})
	}
}

// seedFinishedRun writes the run row a previous, crashed turn would have left.
func seedFinishedRun(t *testing.T, f *fixture, reported bool) {
	t.Helper()

	// A canonical UUID, because that is the only shape a segments row can
	// legitimately hold: harness.Request.validate refuses anything else before
	// the process starts. This path never reaches a real runner, but a fixture
	// that could not survive one is a trap for the next test written from it.
	segment, err := f.db.GetOrCreateSegment(
		t.Context(), f.session.ID, store.FirstSegmentOrdinal, storedClaudeSessionID, store.AgentClaude)
	if err != nil {
		t.Fatalf("seed segment: %v", err)
	}
	run, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{
		SessionID:        f.session.ID,
		SegmentID:        segment.ID,
		Mode:             store.ModeSpike,
		Agent:            store.AgentClaude,
		TriggerCommentID: testComment,
		LogPath:          func(int) string { return filepath.Join(f.paths.Home(), "logs", "seeded.jsonl") },
	})
	if err != nil {
		t.Fatalf("seed run: %v", err)
	}
	exitCode := int64(store.SignalExitCode)
	if _, err := f.db.FinishRun(t.Context(), run.ID, store.RunOutcome{
		Status:   store.RunFailed,
		ExitCode: &exitCode,
		Error:    store.ReasonAbandoned,
	}); err != nil {
		t.Fatalf("seed run outcome: %v", err)
	}
	if reported {
		if _, err := f.db.CompleteRunReport(t.Context(), run.ID, 4242); err != nil {
			t.Fatalf("seed run report: %v", err)
		}
	}
}

// TestExecuteFetchFailureAbortsWithNoWorktree is AC-3.5. The fetch runs before
// anything exists on disk precisely so that this path leaves nothing behind.
func TestExecuteFetchFailureAbortsWithNoWorktree(t *testing.T) {
	t.Parallel()

	runner := newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
		t.Error("claude ran after the fetch failed")
		return nil, errors.New("must not be reached")
	})
	f := newFixture(t, runner)
	f.repo.SetOriginURL(filepath.Join(filepath.Dir(f.repo.Dir), "there-is-no-such-repository.git"))

	_, err := Execute(t.Context(), f.opts, f.request())
	if err == nil {
		t.Fatal("preparation failure must remain retryable")
	}
	if !strings.Contains(err.Error(), "fetch") {
		t.Fatalf("lost cause: %v", err)
	}

	if _, err := os.Stat(f.worktreeDir()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("worktree %s exists after a failed fetch, want no side effects", f.worktreeDir())
	}
	if runs := f.runs(); len(runs) != 0 {
		t.Errorf("allocated %d runs after a failed fetch, want 0", len(runs))
	}
	if fetched := f.repoRow().LastFetchAt; !fetched.IsZero() {
		t.Errorf("last_fetch_at = %v after a failed fetch, want the zero time", fetched)
	}
	session := f.reloadSession()
	if session.State != f.session.State {
		t.Errorf("session state = %q, want %q", session.State, f.session.State)
	}
	if session.LockOwner != "" {
		t.Errorf("lock owner = %q, want it released", session.LockOwner)
	}
}

// TestExecuteRecordsTheFetch is the other half of AC-3.5: a fetch that worked
// moves last_fetch_at, which is what `repo list` reports.
func TestExecuteRecordsTheFetch(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)

	f.execute(f.request())

	if fetched := f.repoRow().LastFetchAt; fetched.IsZero() {
		t.Error("last_fetch_at is still the zero time after a successful turn")
	}
}

// TestExecuteFreezesTheSlugThenReusesIt is AC-3.6 and AC-3.7. The second turn
// arrives with a different issue title on purpose: a retitled issue must not
// silently start a second branch halfway through a task.
func TestExecuteFreezesTheSlugThenReusesIt(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)

	first := f.execute(f.request())
	if first.Branch != testBranch {
		t.Fatalf("branch = %q, want %q", first.Branch, testBranch)
	}
	frozen := f.reloadSession()
	switch {
	case frozen.FeatureSlug != testSlug:
		t.Fatalf("frozen slug = %q, want %q", frozen.FeatureSlug, testSlug)
	case !slugPattern.MatchString(frozen.FeatureSlug):
		t.Fatalf("frozen slug %q does not match %s", frozen.FeatureSlug, slugPattern)
	case frozen.Branch != testBranch:
		t.Fatalf("frozen branch = %q, want %q", frozen.Branch, testBranch)
	}

	retitled := f.request()
	retitled.Issue.Title = "Completely different title now!!!"
	retitled.Trigger.CommentID = testComment + 1
	second := f.execute(retitled)

	if second.Branch != testBranch {
		t.Errorf("second run branch = %q, want the frozen %q", second.Branch, testBranch)
	}
	if slug := f.reloadSession().FeatureSlug; slug != testSlug {
		t.Errorf("slug after the second run = %q, want the frozen %q", slug, testSlug)
	}
	runs := f.runs()
	if len(runs) != 2 || runs[1].Ordinal != 2 {
		t.Fatalf("runs = %+v, want two with ordinals 1 and 2", runs)
	}
}

// TestExecuteRefusesACorruptedFrozenSlug is the half of AC-3.7 that only a
// stored value can reach: the derivation is sound, but a hand-edited row or a
// botched migration is not, and the post-condition is what stops it becoming a
// branch name.
func TestExecuteRefusesACorruptedFrozenSlug(t *testing.T) {
	t.Parallel()

	runner := newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
		t.Error("claude ran with an invalid feature slug")
		return nil, errors.New("must not be reached")
	})
	f := newFixture(t, runner)
	if _, err := f.db.FreezeSessionSlug(t.Context(), f.session.ID, "--not a slug--", "lf/123---not a slug--"); err != nil {
		t.Fatalf("seed a corrupted slug: %v", err)
	}

	_, err := Execute(t.Context(), f.opts, f.request())
	if err == nil {
		t.Fatal("preparation failure must remain retryable")
	}
	if !strings.Contains(err.Error(), "slug") {
		t.Fatalf("lost cause: %v", err)
	}

	if runs := f.runs(); len(runs) != 0 {
		t.Errorf("allocated %d runs, want 0", len(runs))
	}
}

// TestExecuteRefusesAnOccupiedWorktreePath is AC-3.9. Nothing at the path is
// deleted or overwritten to make room, and the file left there is the proof.
func TestExecuteRefusesAnOccupiedWorktreePath(t *testing.T) {
	t.Parallel()

	runner := newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
		t.Error("claude ran with an occupied worktree path")
		return nil, errors.New("must not be reached")
	})
	f := newFixture(t, runner)

	dir := f.worktreeDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create the occupying directory: %v", err)
	}
	occupant := filepath.Join(dir, "someone-elses-work.txt")
	if err := os.WriteFile(occupant, []byte("do not delete me\n"), 0o600); err != nil {
		t.Fatalf("write the occupying file: %v", err)
	}

	_, err := Execute(t.Context(), f.opts, f.request())
	if err == nil {
		t.Fatal("preparation failure must remain retryable")
	}
	if !errors.Is(err, git.ErrWorktreeOccupied) {
		t.Fatalf("lost occupied-path cause: %v", err)
	}

	if content, err := os.ReadFile(occupant); err != nil || string(content) != "do not delete me\n" {
		t.Errorf("the occupying file was disturbed: content=%q err=%v", content, err)
	}
}

// TestExecutePushFailurePreservesTheCommit is AC-3.24. The push is broken from
// inside the run, after the fetch has already succeeded, which is the only
// ordering that isolates the push.
func TestExecutePushFailurePreservesTheCommit(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		writeInWorktree(t, req, "health.go", "package api\n")
		// The remote goes away between the fetch and the push.
		if err := os.RemoveAll(f.repo.Origin); err != nil {
			t.Fatalf("remove the origin: %v", err)
		}
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)

	result := f.execute(f.request())

	var pushErr *git.PushError
	switch {
	case result.Succeeded():
		t.Fatal("a turn whose push failed was reported as a success")
	case !result.Committed || result.CommitSHA == "":
		t.Fatalf("commit was lost: committed=%t sha=%q", result.Committed, result.CommitSHA)
	case result.Pushed:
		t.Error("Pushed = true after the remote went away")
	case !errors.As(result.Err, &pushErr):
		t.Fatalf("Err = %v, want a *git.PushError naming the worktree", result.Err)
	case pushErr.Worktree != result.WorktreePath:
		t.Errorf("push error worktree = %q, want %q", pushErr.Worktree, result.WorktreePath)
	}

	// The commit is where the report says it is.
	if head := f.repo.GitIn(f.worktreeDir(), "rev-parse", "HEAD"); head != result.CommitSHA {
		t.Errorf("worktree HEAD = %q, want the commit %q", head, result.CommitSHA)
	}
	if runs := f.runs(); len(runs) != 1 || runs[0].CommitSHA != result.CommitSHA {
		t.Errorf("runs = %+v, want the commit recorded on the row", runs)
	}
	if !result.Reportable() {
		t.Error("Reportable() = false: a failed push has to be told")
	}
}

// TestExecuteRecordsADistinctReasonPerOutcome is AC-3.20 and S-9. The five
// outcomes stay five: a timeout, a shutdown and an exit status of 2 call for
// three different responses, and an abandonment is not the run's fault at all.
func TestExecuteRecordsADistinctReasonPerOutcome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		outcome    harness.Outcome
		exitCode   int
		reason     string
		wantStatus store.RunStatus
	}{
		{
			name:       "succeeded",
			outcome:    harness.OutcomeSucceeded,
			exitCode:   0,
			reason:     "",
			wantStatus: store.RunSucceeded,
		},
		{
			name:       "failed",
			outcome:    harness.OutcomeFailed,
			exitCode:   2,
			reason:     "claude exited with status 2",
			wantStatus: store.RunFailed,
		},
		{
			name:       "timed out",
			outcome:    harness.OutcomeTimedOut,
			exitCode:   harness.SignalExitCode,
			reason:     "timed out after 30m0s; the process subtree was killed",
			wantStatus: store.RunFailed,
		},
		{
			name:       "abandoned",
			outcome:    harness.OutcomeAbandoned,
			exitCode:   harness.SignalExitCode,
			reason:     "abandoned: the caller's run context ended before claude finished",
			wantStatus: store.RunFailed,
		},
		{
			name:       "no result",
			outcome:    harness.OutcomeNoResult,
			exitCode:   0,
			reason:     "claude exited 0 without emitting a terminal result event",
			wantStatus: store.RunFailed,
		},
	}

	seen := make(map[string]string, len(tests))
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var f *fixture
			runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
				result := succeeded(req, f.paths.Home())
				result.Outcome = tc.outcome
				result.ExitCode = tc.exitCode
				result.Reason = tc.reason
				result.HasResult = tc.outcome != harness.OutcomeNoResult
				return result, nil
			})
			f = newFixture(t, runner)

			result := f.execute(f.request())

			if result.Outcome != tc.outcome {
				t.Errorf("outcome = %s, want %s", result.Outcome, tc.outcome)
			}
			runs := f.runs()
			if len(runs) != 1 {
				t.Fatalf("runs = %d, want 1", len(runs))
			}
			run := runs[0]
			switch {
			case run.Status != tc.wantStatus:
				t.Errorf("run status = %q, want %q", run.Status, tc.wantStatus)
			case run.ExitCode == nil || *run.ExitCode != int64(tc.exitCode):
				t.Errorf("recorded exit code = %v, want %d", run.ExitCode, tc.exitCode)
			case run.Error != tc.reason:
				t.Errorf("recorded reason = %q, want %q", run.Error, tc.reason)
			}
			if meta := f.readMeta(1); meta.Outcome != tc.outcome.String() {
				t.Errorf("meta outcome = %q, want %q", meta.Outcome, tc.outcome)
			}

			// The whole point of five outcomes: no two of them record the same
			// thing, so a report can tell them apart without guessing.
			key := string(run.Status) + "/" + run.Error
			if other, ok := seen[key]; ok {
				t.Errorf("%s records the same status and reason as %s: %q", tc.name, other, key)
			}
			seen[key] = tc.name
		})
	}
}

// TestExecuteDoesNotCommitAnAbandonedRun is the D-B half of the abandonment
// outcome. The daemon has under three seconds to exit and a push takes up to
// two minutes; the work is safe in the worktree until the next run.
func TestExecuteDoesNotCommitAnAbandonedRun(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		writeInWorktree(t, req, "half-done.go", "package api\n")
		result := succeeded(req, f.paths.Home())
		result.Outcome = harness.OutcomeAbandoned
		result.ExitCode = harness.SignalExitCode
		result.Reason = "abandoned: the daemon is shutting down"
		result.HasResult = false
		return result, nil
	})
	f = newFixture(t, runner)

	result := f.execute(f.request())

	switch {
	case result.Outcome != harness.OutcomeAbandoned:
		t.Errorf("outcome = %s, want %s", result.Outcome, harness.OutcomeAbandoned)
	case result.Committed || result.Pushed:
		t.Errorf("committed=%t pushed=%t, want neither for an abandoned run", result.Committed, result.Pushed)
	}
	// The work is still there, uncommitted, for the next run to pick up.
	if _, err := os.Stat(filepath.Join(f.worktreeDir(), "half-done.go")); err != nil {
		t.Errorf("the abandoned run's work is gone: %v", err)
	}
	if owner := f.reloadSession().LockOwner; owner != "" {
		t.Errorf("lock owner after an abandoned run = %q, want it released", owner)
	}
}

// TestExecuteReportsAClaudeThatNeverStarted is AC-4.1 seen from below: a run
// row exists, so it owes the issue a comment even though there is no RunResult
// to render.
func TestExecuteReportsAClaudeThatNeverStarted(t *testing.T) {
	t.Parallel()

	runner := newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
		return nil, harness.ErrStartFailed
	})
	f := newFixture(t, runner)

	result := f.execute(f.request())

	switch {
	case result.Disposition != DispositionRan:
		t.Errorf("disposition = %s, want %s", result.Disposition, DispositionRan)
	case !result.Reportable():
		t.Error("Reportable() = false: a run that never started still has a row and a comment to post")
	case result.Succeeded():
		t.Error("Succeeded() = true for a claude that never started")
	case !errors.Is(result.Err, harness.ErrStartFailed):
		t.Errorf("Err = %v, want %v", result.Err, harness.ErrStartFailed)
	}
	runs := f.runs()
	if len(runs) != 1 || runs[0].Status != store.RunFailed {
		t.Fatalf("runs = %+v, want one failed row", runs)
	}
	if runs[0].ExitCode == nil || *runs[0].ExitCode != store.SignalExitCode {
		t.Errorf("exit code = %v, want %d", runs[0].ExitCode, store.SignalExitCode)
	}
}

// TestExecuteResolvesTheAuthorFromTheClone covers the fallback: with no
// configured identity the clone answers, so a litefactory commit carries the
// same name and address the user's own commits do.
func TestExecuteResolvesTheAuthorFromTheClone(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		writeInWorktree(t, req, "health.go", "package api\n")
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)
	f.opts.Author = git.Author{}
	f.repo.Git("config", "user.name", "Clone Configured")
	f.repo.Git("config", "user.email", "clone@example.invalid")

	result := f.execute(f.request())
	if !result.Committed {
		t.Fatalf("nothing was committed: %+v\nlogs:\n%s", result, f.logs)
	}

	author := f.repo.GitIn(f.worktreeDir(), "log", "-1", "--format=%an <%ae>")
	if author != "Clone Configured <clone@example.invalid>" {
		t.Errorf("commit author = %q, want the clone's identity", author)
	}
}

// TestExecuteValidatesItsArguments covers the one thing that is an error rather
// than a result: a caller that is wrong.
func TestExecuteValidatesItsArguments(t *testing.T) {
	t.Parallel()

	f := newFixture(t, newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
		t.Error("claude ran for an invalid turn")
		return nil, errors.New("must not be reached")
	}))

	t.Run("options", func(t *testing.T) {
		t.Parallel()

		opts := f.opts
		opts.InstanceID = ""
		if _, err := Execute(t.Context(), opts, f.request()); !errors.Is(err, ErrInvalidOptions) {
			t.Errorf("err = %v, want %v", err, ErrInvalidOptions)
		}
	})

	t.Run("command that does not spawn a run", func(t *testing.T) {
		t.Parallel()

		req := f.request()
		req.Command = ParseCommand("/status something")
		if _, err := Execute(t.Context(), f.opts, req); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("err = %v, want %v", err, ErrInvalidRequest)
		}
	})

	t.Run("issue that is not the session's", func(t *testing.T) {
		t.Parallel()

		req := f.request()
		req.Issue.Number = testIssue + 1
		if _, err := Execute(t.Context(), f.opts, req); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("err = %v, want %v", err, ErrInvalidRequest)
		}
	})

	// A trigger id of 0 is the issue body, not a missing comment (AC-2.16b).
	// It is the one non-positive value a turn accepts, and it is 0 rather than
	// the issue's own forge id because reporting the run advances the watermark
	// to it (AC-2.23).
	t.Run("issue body as the trigger", func(t *testing.T) {
		t.Parallel()

		req := f.request()
		req.Trigger.CommentID = 0
		if err := req.validate(); err != nil {
			t.Errorf("validate with a trigger of 0 = %v, want it accepted as the issue body", err)
		}

		req.Trigger.CommentID = -1
		if err := req.validate(); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("validate with a trigger of -1 = %v, want %v", err, ErrInvalidRequest)
		}
	})
}

// TestReconcileReclaimsAndLogs is AC-3.2: a lock held by a non-current daemon
// instance is reclaimed at startup, and the reclaim is logged rather than
// silently swallowed.
func TestReconcileReclaimsAndLogs(t *testing.T) {
	t.Parallel()

	f := newFixture(t, newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
		return nil, errors.New("must not be reached")
	}))
	acquired, err := f.db.AcquireSessionLock(t.Context(), f.session.ID, testOtherOwner)
	if err != nil || !acquired {
		t.Fatalf("seed the lock: acquired=%t err=%v", acquired, err)
	}

	reconciliation, err := Reconcile(t.Context(), f.opts)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(reconciliation.ReclaimedLocks) != 1 {
		t.Fatalf("reclaimed %d locks, want 1", len(reconciliation.ReclaimedLocks))
	}
	if owner := f.reloadSession().LockOwner; owner != "" {
		t.Errorf("lock owner after reconciliation = %q, want it free", owner)
	}
	logs := f.logs.String()
	for _, want := range []string{"reclaimed a task lock", testOtherOwner, f.session.TaskRef} {
		if !strings.Contains(logs, want) {
			t.Errorf("logs do not mention %q:\n%s", want, logs)
		}
	}
}

func TestOwnerStopSavesCancelledWritableRun(t *testing.T) {
	var f *fixture
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		writeInWorktree(t, req, "owner-stop.txt", "retained work\n")
		_, err := f.db.RequestOwnerStop(t.Context(), f.ref().String())
		mustProgress(t, err)
		cancel()
		return &harness.RunResult{Outcome: agent.OutcomeAbandoned, Reason: "cancelled", FinishedAt: time.Now()}, nil
	})
	f = newFixture(t, runner)
	out, err := Execute(ctx, f.opts, f.request())
	mustProgress(t, err)
	if out.Outcome != agent.OutcomeAbandoned || out.Reason != store.ReasonOwnerStop || !out.Committed || !out.Pushed {
		t.Fatalf("stop did not save: %+v", out)
	}
	run, err := f.db.GetRun(t.Context(), out.Run.ID)
	mustProgress(t, err)
	if _, ok := run.OwnerStop(); !ok || run.Error != store.ReasonOwnerStop || run.Status != store.RunFailed || run.CommitSHA == "" || !run.Published {
		t.Fatalf("durable stop = %+v", run)
	}
	if got := f.repo.Git("show", out.CommitSHA+":owner-stop.txt"); !strings.Contains(got, "retained work") {
		t.Fatal(got)
	}
}

func TestOwnerStopPlanDoesNotPublishFailure(t *testing.T) {
	var f *fixture
	f, _, req, _, forge := graphPlanFixtureWithForge(t, "agent", func(a *graphPlanAnswer) {
		_, err := f.db.RequestOwnerStop(t.Context(), f.ref().String())
		mustProgress(t, err)
		a.Stop, a.Reason = agent.OutcomeAbandoned, "cancelled"
	})
	out, err := Execute(t.Context(), f.opts, req)
	if err == nil || out.Run.Error != store.ReasonOwnerStop {
		t.Fatalf("stop outcome: %+v, %v", out, err)
	}
	if len(failureBodies(forge)) != 0 {
		t.Fatal("CLI stop published a Plan failure")
	}
	_, err = Execute(t.Context(), f.opts, req)
	mustProgress(t, err)
	if len(failureBodies(forge)) != 0 {
		t.Fatal("replay published a Plan failure")
	}
}
