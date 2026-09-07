package turn

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/store"
)

// conflictSuggestion is the answer a stubbed advisor returns for the
// README.md conflict [conflictBaseAndBranch] produces in every test in this
// file: the one conflicting path, resolved, with no conflict markers left.
func conflictSuggestion(context.Context, agent.ConflictResolutionRequest) (agent.ConflictResolution, error) {
	return agent.ConflictResolution{Files: []agent.ConflictResolutionFile{
		{Path: "README.md", Resolution: "# fixture\nresolved by the advisor\n"},
	}}, nil
}

// TestExecuteSuggestsAResolutionOnABlockedRun is AC-11.1 end to end: a
// blocked run with suggestions enabled produces a persisted proposal, the
// branch SHA is unchanged, and the reply carries the conflicting files, the
// per-file resolution, the model name, the proposal id and the confirmation
// phrase.
func TestExecuteSuggestsAResolutionOnABlockedRun(t *testing.T) {
	t.Parallel()

	runner := &stubConflictRunner{
		stubRunner: newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
			t.Error("claude ran though the base branch could not be merged in cleanly")
			return nil, errors.New("must not be reached")
		}),
		propose: conflictSuggestion,
	}
	f := newFixture(t, runner)
	f.opts.AutoSyncBase = true
	f.opts.BlockRunOnConflict = true
	f.opts.SuggestConflictResolution = true
	f.opts.ConflictContentMaxBytes = 1 << 16

	dir := seedTaskWorktree(t, f)
	conflictBaseAndBranch(t, f.repo, dir)
	before := f.repo.GitIn(dir, "rev-parse", "HEAD")
	forge := newFakeForge(t)
	reporter := newReporter(t, f, forge, nil)

	result := f.execute(f.request())
	if result.Disposition != DispositionSyncRefused {
		t.Fatalf("disposition = %s, want %s\nlogs:\n%s", result.Disposition, DispositionSyncRefused, f.logs)
	}
	if runner.proposeCallCount() != 1 {
		t.Fatalf("advisor called %d times, want 1", runner.proposeCallCount())
	}
	if result.ConflictProposal == nil {
		t.Fatalf("Result.ConflictProposal is nil, want a persisted proposal\nlogs:\n%s", f.logs)
	}
	if result.ConflictSuggestionFailed {
		t.Error("ConflictSuggestionFailed = true though a proposal was produced")
	}

	after := f.repo.GitIn(dir, "rev-parse", "HEAD")
	if after != before {
		t.Errorf("branch HEAD moved from %s to %s though a suggestion must never write to the branch", before, after)
	}

	stored, err := f.db.GetConflictProposal(t.Context(), result.ConflictProposal.ID)
	if err != nil {
		t.Fatalf("GetConflictProposal: %v", err)
	}
	if stored.Status != store.ConflictProposalPending || len(stored.Files) != 1 || stored.Files[0].Path != "README.md" {
		t.Fatalf("stored proposal = %+v, want one pending file for README.md", stored)
	}

	posted, err := reporter.ReportTurn(t.Context(), result)
	if err != nil {
		t.Fatalf("ReportTurn: %v\nlogs:\n%s", err, f.logs)
	}
	if posted.Skipped {
		t.Fatal("Skipped = true, want a comment to be posted")
	}
	post := forge.only(t)
	for _, want := range []string{
		"Conflicting path(s) (1)", "README.md",
		"claude", "resolved by the advisor",
		"This is a suggestion only",
	} {
		if !strings.Contains(post.body, want) {
			t.Errorf("posted body missing %q:\n%s", want, post.body)
		}
	}
}

// TestExecuteSkipsSuggestionWhenAdvisorFails is AC-11.2: a failing advisor
// still produces the full existing conflict refusal, persists no proposal,
// and the reply adds exactly the one "no suggestion" line.
func TestExecuteSkipsSuggestionWhenAdvisorFails(t *testing.T) {
	t.Parallel()

	runner := &stubConflictRunner{
		stubRunner: newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
			t.Error("claude ran though the base branch could not be merged in cleanly")
			return nil, errors.New("must not be reached")
		}),
		propose: func(context.Context, agent.ConflictResolutionRequest) (agent.ConflictResolution, error) {
			return agent.ConflictResolution{}, errors.New("advisor unavailable")
		},
	}
	f := newFixture(t, runner)
	f.opts.AutoSyncBase = true
	f.opts.BlockRunOnConflict = true
	f.opts.SuggestConflictResolution = true
	f.opts.ConflictContentMaxBytes = 1 << 16

	dir := seedTaskWorktree(t, f)
	conflictBaseAndBranch(t, f.repo, dir)
	forge := newFakeForge(t)
	reporter := newReporter(t, f, forge, nil)

	result := f.execute(f.request())
	if result.Disposition != DispositionSyncRefused {
		t.Fatalf("disposition = %s, want %s", result.Disposition, DispositionSyncRefused)
	}
	if result.ConflictProposal != nil {
		t.Fatalf("ConflictProposal = %+v, want nil: the advisor failed", result.ConflictProposal)
	}
	if !result.ConflictSuggestionFailed {
		t.Error("ConflictSuggestionFailed = false, want true: the advisor call failed")
	}

	posted, err := reporter.ReportTurn(t.Context(), result)
	if err != nil {
		t.Fatalf("ReportTurn: %v", err)
	}
	if posted.Skipped {
		t.Fatal("Skipped = true, want a comment to be posted")
	}
	post := forge.only(t)
	if !strings.Contains(post.body, "Conflicting path(s) (1)") || !strings.Contains(post.body, "README.md") {
		t.Errorf("body missing the ordinary conflict refusal:\n%s", post.body)
	}
	if !strings.Contains(post.body, "No suggested resolution could be produced") {
		t.Errorf("body missing the one-line no-suggestion note:\n%s", post.body)
	}
}

// TestExecuteMakesNoAdvisorCallWhenDisabled is AC-11.3: with suggestions
// disabled the run is byte-identical to the no-suggestion path and makes no
// model call at all.
func TestExecuteMakesNoAdvisorCallWhenDisabled(t *testing.T) {
	t.Parallel()

	runner := &stubConflictRunner{
		stubRunner: newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
			t.Error("claude ran though the base branch could not be merged in cleanly")
			return nil, errors.New("must not be reached")
		}),
		propose: func(context.Context, agent.ConflictResolutionRequest) (agent.ConflictResolution, error) {
			t.Error("the conflict-resolution advisor was called though suggestions are disabled")
			return agent.ConflictResolution{}, errors.New("must not be reached")
		},
	}
	f := newFixture(t, runner)
	f.opts.AutoSyncBase = true
	f.opts.BlockRunOnConflict = true
	f.opts.SuggestConflictResolution = false

	dir := seedTaskWorktree(t, f)
	conflictBaseAndBranch(t, f.repo, dir)

	result := f.execute(f.request())
	if result.Disposition != DispositionSyncRefused {
		t.Fatalf("disposition = %s, want %s", result.Disposition, DispositionSyncRefused)
	}
	if runner.proposeCallCount() != 0 {
		t.Fatalf("advisor called %d times, want 0: suggestions are disabled", runner.proposeCallCount())
	}
	if result.ConflictProposal != nil || result.ConflictSuggestionFailed {
		t.Fatalf("result = %+v, want no US-11 fields set at all when disabled", result)
	}
}

// TestExecuteRejectsAnOutOfScopeSuggestion is AC-11.4's first half: an
// advisor response naming a path outside the conflicting set produces no
// proposal.
func TestExecuteRejectsAnOutOfScopeSuggestion(t *testing.T) {
	t.Parallel()

	runner := &stubConflictRunner{
		stubRunner: newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
			return nil, errors.New("must not be reached")
		}),
		propose: func(context.Context, agent.ConflictResolutionRequest) (agent.ConflictResolution, error) {
			return agent.ConflictResolution{Files: []agent.ConflictResolutionFile{
				{Path: "not-a-conflicting-file.go", Resolution: "package x\n"},
			}}, nil
		},
	}
	f := newFixture(t, runner)
	f.opts.AutoSyncBase = true
	f.opts.BlockRunOnConflict = true
	f.opts.SuggestConflictResolution = true
	f.opts.ConflictContentMaxBytes = 1 << 16

	dir := seedTaskWorktree(t, f)
	conflictBaseAndBranch(t, f.repo, dir)

	result := f.execute(f.request())
	if result.ConflictProposal != nil {
		t.Fatalf("ConflictProposal = %+v, want nil: the response named an out-of-scope path", result.ConflictProposal)
	}
	if !result.ConflictSuggestionFailed {
		t.Error("ConflictSuggestionFailed = false, want true")
	}
}

// TestExecuteRejectsASuggestionCarryingConflictMarkers is AC-11.4's second
// half: an advisor response still carrying a conflict marker produces no
// proposal.
func TestExecuteRejectsASuggestionCarryingConflictMarkers(t *testing.T) {
	t.Parallel()

	runner := &stubConflictRunner{
		stubRunner: newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
			return nil, errors.New("must not be reached")
		}),
		propose: func(context.Context, agent.ConflictResolutionRequest) (agent.ConflictResolution, error) {
			return agent.ConflictResolution{Files: []agent.ConflictResolutionFile{
				{Path: "README.md", Resolution: "still has\n<<<<<<< HEAD\nunresolved\n>>>>>>> base\n"},
			}}, nil
		},
	}
	f := newFixture(t, runner)
	f.opts.AutoSyncBase = true
	f.opts.BlockRunOnConflict = true
	f.opts.SuggestConflictResolution = true
	f.opts.ConflictContentMaxBytes = 1 << 16

	dir := seedTaskWorktree(t, f)
	conflictBaseAndBranch(t, f.repo, dir)

	result := f.execute(f.request())
	if result.ConflictProposal != nil {
		t.Fatalf("ConflictProposal = %+v, want nil: the response still carried a conflict marker", result.ConflictProposal)
	}
	if !result.ConflictSuggestionFailed {
		t.Error("ConflictSuggestionFailed = false, want true")
	}
}

// TestConflictAdvisorNeverCalledWhileCloneGateIsHeld is AC-11.7: the
// advisor's call is timed against the gate's own acquire/release, using a
// gate that records both.
func TestConflictAdvisorNeverCalledWhileCloneGateIsHeld(t *testing.T) {
	t.Parallel()

	gate := newRecordingGate()
	var adviseCalledWhileHeld bool
	runner := &stubConflictRunner{
		stubRunner: newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
			return nil, errors.New("must not be reached")
		}),
		propose: func(context.Context, agent.ConflictResolutionRequest) (agent.ConflictResolution, error) {
			if gate.held() {
				adviseCalledWhileHeld = true
			}
			return conflictSuggestion(context.Background(), agent.ConflictResolutionRequest{})
		},
	}
	f := newFixture(t, runner)
	f.opts.AutoSyncBase = true
	f.opts.BlockRunOnConflict = true
	f.opts.SuggestConflictResolution = true
	f.opts.ConflictContentMaxBytes = 1 << 16
	f.opts.CloneGate = gate

	dir := seedTaskWorktree(t, f)
	conflictBaseAndBranch(t, f.repo, dir)

	result := f.execute(f.request())
	if result.ConflictProposal == nil {
		t.Fatalf("ConflictProposal is nil, want a persisted proposal\nlogs:\n%s", f.logs)
	}
	if adviseCalledWhileHeld {
		t.Error("the conflict-resolution advisor was called while the clone gate was held")
	}
}

// TestExecuteAcceptsASuggestionNamingAnExcludedFile is a regression test for
// CR-A-006, a non-blocking code-review advisory: validateConflictResolution
// checks a model-returned path against the full [git.MergePreview.Conflicts]
// list (sr.conflicts) rather than the narrower set of files
// [collectConflictContent] actually collected and sent to the advisor
// (sr.files). huge.txt conflicts — its path is in sr.conflicts — but is
// excluded whole for exceeding the per-file byte budget (AC-11.5), so it is
// never one of the files the advisor is shown. A response naming only
// huge.txt still passes today's validation and is persisted.
//
// This documents the current behaviour so a future change does not alter it
// silently; it is not an endorsement of the gap. If validation is narrowed to
// check against sr.files instead, this test's assertions are expected to
// flip and should be rewritten for the corrected behaviour.
func TestExecuteAcceptsASuggestionNamingAnExcludedFile(t *testing.T) {
	t.Parallel()

	runner := &stubConflictRunner{
		stubRunner: newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
			return nil, errors.New("must not be reached")
		}),
		propose: func(context.Context, agent.ConflictResolutionRequest) (agent.ConflictResolution, error) {
			return agent.ConflictResolution{Files: []agent.ConflictResolutionFile{
				{Path: "huge.txt", Resolution: "resolved without the advisor ever seeing huge.txt's real content\n"},
			}}, nil
		},
	}
	f := newFixture(t, runner)
	f.opts.AutoSyncBase = true
	f.opts.BlockRunOnConflict = true
	f.opts.SuggestConflictResolution = true
	f.opts.ConflictContentMaxBytes = 1 << 16

	dir := seedTaskWorktree(t, f)
	conflictBaseAndBranchWithOversizedFile(t, f.repo, dir)

	result := f.execute(f.request())
	if result.Disposition != DispositionSyncRefused {
		t.Fatalf("disposition = %s, want %s\nlogs:\n%s", result.Disposition, DispositionSyncRefused, f.logs)
	}
	if len(result.SyncConflicts) != 2 {
		t.Fatalf("SyncConflicts = %v, want both README.md and huge.txt listed as conflicting", result.SyncConflicts)
	}

	// CR-A-006: current behaviour accepts this response, though huge.txt's
	// content was never sent to the advisor.
	if result.ConflictProposal == nil {
		t.Fatalf("ConflictProposal is nil; today's validation accepts an excluded file's path (CR-A-006)\nlogs:\n%s", f.logs)
	}
	if len(result.ConflictProposal.Files) != 1 || result.ConflictProposal.Files[0].Path != "huge.txt" {
		t.Errorf("stored proposal files = %+v, want exactly huge.txt", result.ConflictProposal.Files)
	}
}

// TestConflictSuggestionReachesATerminalStateAfterPosting is AC-11.6: once
// [attemptConflictSuggestion] has run, the turn's disposition is one that
// both [Result.Reportable] and [Result.ConsumesComment] answer true for — a
// blocked run with a suggestion attached is exactly as terminal as the
// plain conflict refusal it augments, never a state a later tick queues
// behind or retries.
func TestConflictSuggestionReachesATerminalStateAfterPosting(t *testing.T) {
	t.Parallel()

	runner := &stubConflictRunner{
		stubRunner: newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
			return nil, errors.New("must not be reached")
		}),
		propose: conflictSuggestion,
	}
	f := newFixture(t, runner)
	f.opts.AutoSyncBase = true
	f.opts.BlockRunOnConflict = true
	f.opts.SuggestConflictResolution = true
	f.opts.ConflictContentMaxBytes = 1 << 16

	dir := seedTaskWorktree(t, f)
	conflictBaseAndBranch(t, f.repo, dir)

	result := f.execute(f.request())
	if result.ConflictProposal == nil {
		t.Fatalf("ConflictProposal is nil, want a persisted proposal\nlogs:\n%s", f.logs)
	}
	if !result.Reportable() {
		t.Error("Reportable() = false, want true: a blocked run with a suggestion still owes the issue a comment")
	}
	if !result.ConsumesComment() {
		t.Error("ConsumesComment() = false, want true: a blocked run with a suggestion must not be retried by a later tick")
	}
}

// TestConflictSuggestionCodeNeverSleepsOrPolls is AC-11.6's structural half:
// no function in internal/turn/conflict.go — the whole US-11 orchestration,
// from collecting conflicting content through persisting a proposal — calls
// time.Sleep, starts a ticker, or blocks on time.After. There is no reply to
// wait for in this story (only a later story could add one): a suggestion is
// posted and the turn ends, so nothing here has any legitimate reason to
// pause for a clock.
func TestConflictSuggestionCodeNeverSleepsOrPolls(t *testing.T) {
	t.Parallel()

	const file = "conflict.go"
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}

	forbidden := map[string]bool{"Sleep": true, "After": true, "Tick": true, "NewTicker": true, "NewTimer": true}
	ast.Inspect(parsed, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok || ident.Name != "time" {
			return true
		}
		if forbidden[sel.Sel.Name] {
			t.Errorf("%s calls time.%s at %s; US-11 posts a suggestion and ends the turn, it waits for nothing (AC-11.6)",
				file, sel.Sel.Name, fset.Position(n.Pos()))
		}
		return true
	})
}

// recordingGate wraps [newTestGate]'s real serialising gate and additionally
// tracks whether it is currently held, so a test can assert a call happened
// after release rather than merely happening to succeed.
type recordingGate struct {
	inner    CloneGate
	heldFlag chan bool
}

func newRecordingGate() *recordingGate {
	return &recordingGate{inner: newTestGate(), heldFlag: make(chan bool, 1)}
}

func (g *recordingGate) Acquire(ctx context.Context, path string) (func(), error) {
	release, err := g.inner.Acquire(ctx, path)
	if err != nil {
		return nil, err
	}
	select {
	case g.heldFlag <- true:
	default:
	}
	return func() {
		select {
		case <-g.heldFlag:
		default:
		}
		release()
	}, nil
}

func (g *recordingGate) held() bool {
	select {
	case v := <-g.heldFlag:
		g.heldFlag <- v
		return v
	default:
		return false
	}
}
