package daemon_test

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// fakeConfirmApplier stands in for [turn.ExecuteApplyResolution] the same way
// [fakeExecutor] stands in for [turn.Execute] (T-12.5): what this package's
// own tests need is control over what an application *did*, not a real git
// clone and worktree — internal/turn already owns that coverage directly
// against real ones.
type fakeConfirmApplier struct {
	fn func(ctx context.Context, req turn.ApplyRequest) (turn.ApplyResult, error)
}

func (f *fakeConfirmApplier) ApplyConfirmedResolution(ctx context.Context, req turn.ApplyRequest) (turn.ApplyResult, error) {
	return f.fn(ctx, req)
}

// createConflictProposal is the one place every confirm-dispatch test in this
// file gets a real, pending proposal row to confirm against — the daemon's
// own [turn.ValidateConfirmation] call reads the database directly, so a
// fixture-level test has to put a real row there rather than mocking the
// read away.
func createConflictProposal(t *testing.T, f *fixture, sourceComment int64) store.ConflictProposal {
	t.Helper()

	session := f.session()
	proposal, _, err := f.db.CreateOrGetConflictProposal(t.Context(), store.CreateConflictProposalParams{
		SessionID: session.ID, TriggerCommentID: sourceComment, Model: "claude", Format: store.ConflictRegionFormat,
		Files:     []store.ConflictProposalFile{{Path: "README.md", Details: store.ConflictFileDetails{OriginalDigest: "original", ResultDigest: "result", Regions: []store.ConflictRegionResolution{{ID: "region", Resolution: "resolved by the advisor\n"}}}}},
		BaseSHA:   "base0000000000000000000000000000000000",
		BranchSHA: "branch000000000000000000000000000000000",
	})
	if err != nil {
		t.Fatalf("create conflict proposal: %v", err)
	}
	return proposal
}

// TestHandleConfirmInvalidGrammarRepliesAndConsumes is AC-12.3/T-12.5: a
// /confirm control whose grammar [confirm.Parse] rejects never reaches the
// database at all — [daemon.Daemon.handleConfirm] answers
// [report.ReplyConfirmInvalid] straight away — and the comment is still
// consumed, so a later tick does not see it again.
func TestHandleConfirmInvalidGrammarRepliesAndConsumes(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Merge conflict task")
	f.settle() // first sight: creates the session and watermark

	f.forge.addComment(testIssue, 501, owner(), "/confirm not-a-number apply this resolution")
	f.settle()

	if calls := f.turns.calls(); len(calls) != 0 {
		t.Errorf("ran %d turns for a /confirm control, want 0", len(calls))
	}
	posted, _, _ := f.forge.snapshot()
	if len(posted) != 1 {
		t.Fatalf("posted %d comments, want exactly one refusal\nlogs:\n%s", len(posted), f.logs)
	}
	if !strings.Contains(posted[0], "I could not accept that confirmation") {
		t.Errorf("reply does not read as an invalid-confirmation refusal:\n%s", posted[0])
	}
	if got := f.watermark(); got != 501 {
		t.Errorf("watermark is %d, want 501: the malformed control must still be consumed", got)
	}
}

// TestHandleConfirmUnknownProposalReplies is AC-12.3's first case at the
// daemon layer: a /confirm naming a proposal id this task has no record of.
func TestHandleConfirmUnknownProposalReplies(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Merge conflict task")
	f.settle()

	f.forge.addComment(testIssue, 502, owner(), "/confirm 999999 apply this resolution")
	f.settle()

	posted, _, _ := f.forge.snapshot()
	if len(posted) != 1 {
		t.Fatalf("posted %d comments, want exactly one refusal\nlogs:\n%s", len(posted), f.logs)
	}
	if !strings.Contains(posted[0], "I have no pending proposal `999999`") {
		t.Errorf("reply does not name the unknown proposal id:\n%s", posted[0])
	}
	if !strings.Contains(posted[0], "Nothing was applied") {
		t.Errorf("reply does not disclose that nothing was applied:\n%s", posted[0])
	}
}

// TestHandleConfirmExpiredByTTLSweepReplies is T-12.5's TTL-sweep half
// (AC-12.3's third case, reached through the daemon's own expiry rather than
// internal/turn's): [daemon.Daemon.handleConfirm] expires stale proposals
// before it ever parses the confirming comment, so a proposal whose TTL has
// passed is refused as expired even though it was pending when this comment
// was written.
//
// The daemon's own tick clock is a fixture seam wholly independent of the
// wall clock the store stamps created_at with, so this advances the tick
// clock far into the future — safely past any wall-clock reading this test
// could plausibly run at — rather than trying to backdate the row itself.
func TestHandleConfirmExpiredByTTLSweepReplies(t *testing.T) {
	f := newFixture(t)
	f.daemon = f.newDaemon(daemon.Options{ConflictProposalTTL: time.Minute})
	f.plantCursor()
	f.issue("Merge conflict task")
	f.settle()

	proposal := createConflictProposal(t, f, 500)
	f.clock.advance(60 * 24 * time.Hour)

	f.forge.addComment(testIssue, 503, owner(),
		"/confirm "+strconv.FormatInt(proposal.ID, 10)+" apply this resolution")
	f.settle()

	posted, _, _ := f.forge.snapshot()
	if len(posted) != 1 {
		t.Fatalf("posted %d comments, want exactly one refusal\nlogs:\n%s", len(posted), f.logs)
	}
	if !strings.Contains(posted[0], "has expired and can no longer be confirmed") {
		t.Errorf("reply does not read as an expired-proposal refusal:\n%s", posted[0])
	}

	stored, err := f.db.GetConflictProposal(t.Context(), proposal.ID)
	if err != nil {
		t.Fatalf("read the proposal after confirming it: %v", err)
	}
	if stored.Status != store.ConflictProposalExpired {
		t.Errorf("proposal status = %s, want expired", stored.Status)
	}
}

// TestHandleConfirmAppliedPostsDisclosure is AC-12.5's daemon-dispatch half:
// once [turn.ExecuteApplyResolution] (faked here as [fakeConfirmApplier])
// reports [turn.ApplyOutcomeApplied], the posted reply names the model, the
// confirming handle, the applied files and the commit — reusing the exact
// facts the applier returned rather than anything read back off the comment.
func TestHandleConfirmAppliedPostsDisclosure(t *testing.T) {
	f := newFixture(t)
	const commitSHA = "abc1234def5678900000000000000000000abcd"
	applier := &fakeConfirmApplier{fn: func(_ context.Context, req turn.ApplyRequest) (turn.ApplyResult, error) {
		return turn.ApplyResult{
			Outcome:   turn.ApplyOutcomeApplied,
			CommitSHA: commitSHA,
			Proposal: store.ConflictProposal{
				ID: 1, Model: "claude", ConfirmedBy: ownerLogin, Format: store.ConflictRegionFormat,
				Status:           store.ConflictProposalApplied,
				AppliedCommitSHA: commitSHA,
				Files:            []store.ConflictProposalFile{{Path: "README.md", Details: store.ConflictFileDetails{OriginalDigest: "original", ResultDigest: "result", Regions: []store.ConflictRegionResolution{{ID: "region", Resolution: "resolved by the advisor\n"}}}}},
			},
		}, nil
	}}
	f.daemon = f.newDaemon(daemon.Options{Confirmer: applier})
	f.plantCursor()
	f.issue("Merge conflict task")
	f.settle()

	proposal := createConflictProposal(t, f, 500)
	f.forge.addComment(testIssue, 504, owner(),
		"/confirm "+strconv.FormatInt(proposal.ID, 10)+" apply this resolution")
	f.settle()

	posted, _, _ := f.forge.snapshot()
	if len(posted) != 1 {
		t.Fatalf("posted %d comments, want exactly one disclosure\nlogs:\n%s", len(posted), f.logs)
	}
	body := posted[0]
	for _, want := range []string{
		"Applied proposal `1`",
		"claude",
		ownerLogin,
		"README.md",
		"resolved by the advisor",
		commitSHA,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("applied-resolution reply missing %q:\n%s", want, body)
		}
	}
}

// TestHandleConfirmFailedOutcomeReplies is AC-12.6's daemon-dispatch half: an
// application that could not be completed answers the thread with
// [report.ReplyConfirmFailed] rather than silence, naming the actual failure.
// It must not claim restoration that the applier has not established. Previously nothing was
// written.
func TestHandleConfirmFailedOutcomeReplies(t *testing.T) {
	f := newFixture(t)
	applier := &fakeConfirmApplier{fn: func(_ context.Context, req turn.ApplyRequest) (turn.ApplyResult, error) {
		return turn.ApplyResult{
			Outcome: turn.ApplyOutcomeFailed,
			Reason:  "an untracked file was left in the worktree",
			Proposal: store.ConflictProposal{
				ID: 1, Model: "claude", Status: store.ConflictProposalPending,
			},
		}, nil
	}}
	f.daemon = f.newDaemon(daemon.Options{Confirmer: applier})
	f.plantCursor()
	f.issue("Merge conflict task")
	f.settle()

	proposal := createConflictProposal(t, f, 500)
	f.forge.addComment(testIssue, 505, owner(),
		"/confirm "+strconv.FormatInt(proposal.ID, 10)+" apply this resolution")
	f.settle()

	posted, _, _ := f.forge.snapshot()
	if len(posted) != 1 {
		t.Fatalf("posted %d comments, want exactly one failure disclosure\nlogs:\n%s", len(posted), f.logs)
	}
	if !strings.Contains(posted[0], "I could not complete application of proposal") {
		t.Errorf("reply does not read as a failed-application disclosure:\n%s", posted[0])
	}
	if !strings.Contains(posted[0], "an untracked file was left in the worktree") {
		t.Errorf("reply does not disclose the actual failure:\n%s", posted[0])
	}

	// The failing outcome must not itself move the proposal off pending: only
	// ExecuteApplyResolution's own store write does that, and this fake never
	// makes one (AC-12.6, AC-12.10).
	stored, err := f.db.GetConflictProposal(t.Context(), proposal.ID)
	if err != nil {
		t.Fatalf("read the proposal after a failed application: %v", err)
	}
	if stored.Status != store.ConflictProposalPending {
		t.Errorf("proposal status = %s, want still pending", stored.Status)
	}
}

// TestHandleConfirmSupersededOutcomeReplies is AC-12.4's daemon-dispatch
// half: an application that found the branch or base had moved answers with
// [report.ReplyConfirmSupersededProposal] and asks for a fresh suggestion.
func TestHandleConfirmSupersededOutcomeReplies(t *testing.T) {
	f := newFixture(t)
	applier := &fakeConfirmApplier{fn: func(_ context.Context, req turn.ApplyRequest) (turn.ApplyResult, error) {
		return turn.ApplyResult{
			Outcome:  turn.ApplyOutcomeSuperseded,
			Proposal: store.ConflictProposal{ID: 1, Status: store.ConflictProposalSuperseded},
		}, nil
	}}
	f.daemon = f.newDaemon(daemon.Options{Confirmer: applier})
	f.plantCursor()
	f.issue("Merge conflict task")
	f.settle()

	proposal := createConflictProposal(t, f, 500)
	f.forge.addComment(testIssue, 506, owner(),
		"/confirm "+strconv.FormatInt(proposal.ID, 10)+" apply this resolution")
	f.settle()

	posted, _, _ := f.forge.snapshot()
	if len(posted) != 1 {
		t.Fatalf("posted %d comments, want exactly one refusal\nlogs:\n%s", len(posted), f.logs)
	}
	if !strings.Contains(posted[0], "no longer matches this task's current branch or base") {
		t.Errorf("reply does not read as a superseded-proposal refusal:\n%s", posted[0])
	}
	if !strings.Contains(posted[0], "Comment again to ask for a fresh suggestion") {
		t.Errorf("reply does not ask for a fresh suggestion:\n%s", posted[0])
	}
}

func TestHandleConfirmLegacyProposalRequestsFreshRegeneration(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Legacy conflict task")
	f.settle()
	session := f.session()
	p, _, err := f.db.CreateOrGetConflictProposal(t.Context(), store.CreateConflictProposalParams{SessionID: session.ID, TriggerCommentID: 500, Model: "codex",
		BaseSHA: "base", BranchSHA: "branch", Files: []store.ConflictProposalFile{{Path: "README.md", Resolution: "fragment"}}})
	if err != nil {
		t.Fatal(err)
	}
	f.forge.addComment(testIssue, 501, owner(), "/confirm "+strconv.FormatInt(p.ID, 10)+" apply this resolution")
	f.settle()
	posted, _, _ := f.forge.snapshot()
	if len(posted) != 1 || !strings.Contains(posted[0], "Legacy or unknown proposal format") || !strings.Contains(posted[0], "confirm the new proposal number") {
		t.Fatalf("legacy refusal missing actionable reply: %v", posted)
	}
	if calls := f.turns.calls(); len(calls) != 0 {
		t.Fatal("legacy confirmation ran agent")
	}
}
