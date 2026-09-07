package turn

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/store"
)

// testOwnerLogin is [fixture.request]'s Issue.Author: the "issue owner" a
// confirmation must match (US-12).
const testOwnerLogin = "octo-reporter"

// blockedProposal drives one real blocked-merge run with suggestions
// enabled, using [conflictSuggestion]'s fixed answer, and returns the
// fixture, the worktree directory, and the persisted, still-pending
// proposal it produced — the starting point every T-12.9 test in this file
// confirms or refuses.
func blockedProposal(t *testing.T) (*fixture, string, store.ConflictProposal) {
	t.Helper()

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

	result := f.execute(f.request())
	if result.ConflictProposal == nil {
		t.Fatalf("ConflictProposal is nil, want a persisted proposal\nlogs:\n%s", f.logs)
	}
	stored, err := f.db.GetConflictProposal(t.Context(), result.ConflictProposal.ID)
	if err != nil {
		t.Fatalf("GetConflictProposal: %v", err)
	}
	return f, dir, stored
}

// TestConfirmedProposalHasNoExportedFields is AC-12.1's compile-time half,
// proven the same way [git.Worktree]'s own construction guarantee is
// (TestWorktreeHasNoExportedConstructionPath): every field of
// confirmedProposal is unexported, so no package outside internal/turn can
// build one directly. [ExecuteApplyResolution] requires one as a field of
// [ApplyRequest], and the only function in this package that produces a
// non-zero one is [ValidateConfirmation] — together that is what makes "did
// this confirmation pass every gate" a compile-time property of the call
// rather than a runtime check a future change could forget.
func TestConfirmedProposalHasNoExportedFields(t *testing.T) {
	t.Parallel()

	typ := reflect.TypeOf(confirmedProposal{})
	for i := range typ.NumField() {
		if field := typ.Field(i); field.IsExported() {
			t.Errorf("confirmedProposal.%s is exported; a caller outside this package "+
				"could then build a usable confirmedProposal without calling ValidateConfirmation", field.Name)
		}
	}
}

// TestApplyConfirmedResolutionHappyPath is AC-12.5 at the turn level: a
// valid confirmation on an unchanged branch writes only the confirmed file,
// produces exactly one two-parent merge commit, pushes it, and marks the
// proposal applied.
func TestApplyConfirmedResolutionHappyPath(t *testing.T) {
	t.Parallel()

	f, dir, proposal := blockedProposal(t)

	confirmed, refusal, err := ValidateConfirmation(t.Context(), f.db, f.session.ID, proposal.ID, testOwnerLogin, testOwnerLogin)
	if err != nil || refusal != ConfirmationOK {
		t.Fatalf("ValidateConfirmation = (refusal=%s, err=%v), want ConfirmationOK", refusal, err)
	}

	result, err := ExecuteApplyResolution(t.Context(), f.opts, ApplyRequest{
		SessionID: f.session.ID, ConfirmedCommentID: 99001, Proposal: confirmed,
	})
	if err != nil {
		t.Fatalf("ExecuteApplyResolution: %v\nlogs:\n%s", err, f.logs)
	}
	if result.Outcome != ApplyOutcomeApplied {
		t.Fatalf("Outcome = %s (reason %q), want applied\nlogs:\n%s", result.Outcome, result.Reason, f.logs)
	}
	if result.CommitSHA == "" {
		t.Error("CommitSHA is empty")
	}
	if result.Proposal.Status != store.ConflictProposalApplied {
		t.Errorf("proposal status = %s, want applied", result.Proposal.Status)
	}
	if result.Proposal.ConfirmedBy != testOwnerLogin || result.Proposal.AppliedCommitSHA != result.CommitSHA {
		t.Errorf("proposal = %+v, want ConfirmedBy=%q and AppliedCommitSHA=%q", result.Proposal, testOwnerLogin, result.CommitSHA)
	}

	content, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	if string(content) != "# fixture\nresolved by the advisor\n" {
		t.Errorf("README.md = %q, want the confirmed resolution", content)
	}
	if status := f.repo.GitIn(dir, "status", "--porcelain"); status != "" {
		t.Errorf("worktree not clean after applying: %q", status)
	}

	head := f.repo.GitIn(dir, "rev-parse", "HEAD")
	if head != result.CommitSHA {
		t.Errorf("branch HEAD = %s, want %s", head, result.CommitSHA)
	}
	if parents := strings.Fields(f.repo.GitIn(dir, "log", "-1", "--pretty=%P", head)); len(parents) != 2 {
		t.Errorf("merge commit parents = %v, want exactly two", parents)
	}
	message := f.repo.GitIn(dir, "log", "-1", "--pretty=%B", head)
	for _, want := range []string{
		"Litefactory-Conflict-Proposal: " + strconv.FormatInt(proposal.ID, 10),
		"Litefactory-Confirmed-By: " + testOwnerLogin,
		"Litefactory-Model: claude",
	} {
		if !strings.Contains(message, want) {
			t.Errorf("commit message missing %q; got:\n%s", want, message)
		}
	}

	originHead := f.repo.GitIn(f.repo.Origin, "rev-parse", testBranch)
	if originHead != head {
		t.Errorf("origin's %s = %s, want %s (pushed)", testBranch, originHead, head)
	}
}

// TestApplyConfirmedResolutionRefusesNonOwner is AC-12.2: a confirmation
// from a non-owning login is refused, writes nothing, and leaves the
// proposal pending.
func TestApplyConfirmedResolutionRefusesNonOwner(t *testing.T) {
	t.Parallel()

	f, dir, proposal := blockedProposal(t)
	before := f.repo.GitIn(dir, "rev-parse", "HEAD")

	_, refusal, err := ValidateConfirmation(t.Context(), f.db, f.session.ID, proposal.ID, testOwnerLogin, "someone-else")
	if err != nil {
		t.Fatalf("ValidateConfirmation error = %v", err)
	}
	if refusal != ConfirmationNotOwner {
		t.Fatalf("refusal = %s, want ConfirmationNotOwner", refusal)
	}

	stillPending, err := f.db.GetConflictProposal(t.Context(), proposal.ID)
	if err != nil || stillPending.Status != store.ConflictProposalPending {
		t.Fatalf("proposal after non-owner confirmation = %+v, %v; want still pending", stillPending, err)
	}
	if after := f.repo.GitIn(dir, "rev-parse", "HEAD"); after != before {
		t.Errorf("HEAD moved from %s to %s though the confirmation was refused", before, after)
	}
}

// TestApplyConfirmedResolutionRefusesUnknownProposal is AC-12.3's first
// case: a proposal id that does not exist is refused as unknown.
func TestApplyConfirmedResolutionRefusesUnknownProposal(t *testing.T) {
	t.Parallel()

	f, _, _ := blockedProposal(t)

	_, refusal, err := ValidateConfirmation(t.Context(), f.db, f.session.ID, 9999999, testOwnerLogin, testOwnerLogin)
	if err != nil {
		t.Fatalf("ValidateConfirmation error = %v", err)
	}
	if refusal != ConfirmationUnknownProposal {
		t.Fatalf("refusal = %s, want ConfirmationUnknownProposal", refusal)
	}
}

// TestApplyConfirmedResolutionRefusesCrossSessionProposal proves a
// confirmation cannot be used to probe another session's proposal: a real
// proposal id belonging to a different session is refused identically to an
// unknown one.
func TestApplyConfirmedResolutionRefusesCrossSessionProposal(t *testing.T) {
	t.Parallel()

	f, _, proposal := blockedProposal(t)

	_, refusal, err := ValidateConfirmation(t.Context(), f.db, f.session.ID+1, proposal.ID, testOwnerLogin, testOwnerLogin)
	if err != nil {
		t.Fatalf("ValidateConfirmation error = %v", err)
	}
	if refusal != ConfirmationUnknownProposal {
		t.Fatalf("refusal = %s, want ConfirmationUnknownProposal", refusal)
	}
}

// TestApplyConfirmedResolutionRefusesExpiredProposal is AC-12.3's second
// case.
func TestApplyConfirmedResolutionRefusesExpiredProposal(t *testing.T) {
	t.Parallel()

	f, _, proposal := blockedProposal(t)

	cutoff := proposal.CreatedAt.Add(time.Hour)
	if _, err := f.db.ExpireConflictProposals(t.Context(), cutoff, cutoff.Add(time.Minute)); err != nil {
		t.Fatalf("ExpireConflictProposals: %v", err)
	}

	_, refusal, err := ValidateConfirmation(t.Context(), f.db, f.session.ID, proposal.ID, testOwnerLogin, testOwnerLogin)
	if err != nil {
		t.Fatalf("ValidateConfirmation error = %v", err)
	}
	if refusal != ConfirmationExpired {
		t.Fatalf("refusal = %s, want ConfirmationExpired", refusal)
	}
}

// TestApplyConfirmedResolutionRefusesAlreadyDecidedProposal is AC-12.3's
// third case, and AC-12.9: confirming an already-applied proposal a second
// time is refused as already applied, produces no second commit.
func TestApplyConfirmedResolutionRefusesAlreadyDecidedProposal(t *testing.T) {
	t.Parallel()

	f, dir, proposal := blockedProposal(t)

	confirmed, refusal, err := ValidateConfirmation(t.Context(), f.db, f.session.ID, proposal.ID, testOwnerLogin, testOwnerLogin)
	if err != nil || refusal != ConfirmationOK {
		t.Fatalf("first ValidateConfirmation = (%s, %v), want ConfirmationOK", refusal, err)
	}
	first, err := ExecuteApplyResolution(t.Context(), f.opts, ApplyRequest{
		SessionID: f.session.ID, ConfirmedCommentID: 99001, Proposal: confirmed,
	})
	if err != nil || first.Outcome != ApplyOutcomeApplied {
		t.Fatalf("first ExecuteApplyResolution = (%+v, %v), want applied", first, err)
	}
	headAfterFirst := f.repo.GitIn(dir, "rev-parse", "HEAD")

	_, refusal, err = ValidateConfirmation(t.Context(), f.db, f.session.ID, proposal.ID, testOwnerLogin, testOwnerLogin)
	if err != nil {
		t.Fatalf("second ValidateConfirmation error = %v", err)
	}
	if refusal != ConfirmationAlreadyApplied {
		t.Fatalf("second refusal = %s, want ConfirmationAlreadyApplied", refusal)
	}
	if head := f.repo.GitIn(dir, "rev-parse", "HEAD"); head != headAfterFirst {
		t.Errorf("HEAD moved from %s to %s: a second commit was made", headAfterFirst, head)
	}
}

// TestApplyConfirmedResolutionSupersedesOnStaleBase is AC-12.4: when the
// base branch moves after the proposal was computed, the confirmation is
// still ConfirmationOK (status is still pending), but ExecuteApplyResolution
// finds the re-verified base sha no longer matches, writes nothing, and
// marks the proposal superseded.
func TestApplyConfirmedResolutionSupersedesOnStaleBase(t *testing.T) {
	t.Parallel()

	f, dir, proposal := blockedProposal(t)
	before := f.repo.GitIn(dir, "rev-parse", "HEAD")

	// Advance the base branch further, from the registered clone itself, so
	// the next fetch moves origin/<base> past what the proposal recorded.
	if err := os.WriteFile(filepath.Join(f.repo.Dir, "further.txt"), []byte("more base work\n"), 0o600); err != nil {
		t.Fatalf("write further.txt: %v", err)
	}
	f.repo.Git("add", "--all")
	f.repo.Git("commit", "--quiet", "--message", "advance the base further")
	f.repo.Git("push", "--quiet", "origin", gittest.DefaultBranch)

	confirmed, refusal, err := ValidateConfirmation(t.Context(), f.db, f.session.ID, proposal.ID, testOwnerLogin, testOwnerLogin)
	if err != nil || refusal != ConfirmationOK {
		t.Fatalf("ValidateConfirmation = (%s, %v), want ConfirmationOK: the base moving does not change the proposal's own status", refusal, err)
	}

	result, err := ExecuteApplyResolution(t.Context(), f.opts, ApplyRequest{
		SessionID: f.session.ID, ConfirmedCommentID: 99001, Proposal: confirmed,
	})
	if err != nil {
		t.Fatalf("ExecuteApplyResolution: %v\nlogs:\n%s", err, f.logs)
	}
	if result.Outcome != ApplyOutcomeSuperseded {
		t.Fatalf("Outcome = %s (reason %q), want superseded\nlogs:\n%s", result.Outcome, result.Reason, f.logs)
	}
	if result.Proposal.Status != store.ConflictProposalSuperseded {
		t.Errorf("proposal status = %s, want superseded", result.Proposal.Status)
	}
	if after := f.repo.GitIn(dir, "rev-parse", "HEAD"); after != before {
		t.Errorf("HEAD moved from %s to %s though the proposal was stale and nothing should have been written", before, after)
	}

	// A second confirmation attempt now finds the proposal decided.
	_, refusal, err = ValidateConfirmation(t.Context(), f.db, f.session.ID, proposal.ID, testOwnerLogin, testOwnerLogin)
	if err != nil {
		t.Fatalf("ValidateConfirmation after supersede error = %v", err)
	}
	if refusal != ConfirmationSuperseded {
		t.Fatalf("refusal after supersede = %s, want ConfirmationSuperseded", refusal)
	}
}

// TestApplyConfirmedResolutionRollsBackOnMutatorFailure is T-12.10/AC-12.6's
// turn-level half — the git-level abort-mid-merge cases are covered
// directly in internal/git/worktree_apply_test.go
// (TestWorktreeApplyRollsBackOnPartialFileList,
// TestWorktreeCompleteMergeRefusesRemainingConflicts). Here, an untracked
// file left in the worktree before confirmation forces
// [git.Worktree.WriteResolvedFiles]'s own precondition refusal
// (ErrWorktreeUncommittedChanges, S-5): the application never even starts a
// merge, ExecuteApplyResolution reports ApplyOutcomeFailed rather than an
// error, and — this is the property specific to this layer — the proposal
// is left pending rather than moved to any terminal status, so a fresh
// confirmation could retry in a later, independently terminal run
// (AC-12.10); this run never retries itself.
func TestApplyConfirmedResolutionRollsBackOnMutatorFailure(t *testing.T) {
	t.Parallel()

	f, dir, proposal := blockedProposal(t)
	before := f.repo.GitIn(dir, "rev-parse", "HEAD")

	if err := os.WriteFile(filepath.Join(dir, "left-behind.txt"), []byte("oops\n"), 0o600); err != nil {
		t.Fatalf("write left-behind.txt: %v", err)
	}

	confirmed, refusal, err := ValidateConfirmation(t.Context(), f.db, f.session.ID, proposal.ID, testOwnerLogin, testOwnerLogin)
	if err != nil || refusal != ConfirmationOK {
		t.Fatalf("ValidateConfirmation = (%s, %v), want ConfirmationOK", refusal, err)
	}

	result, err := ExecuteApplyResolution(t.Context(), f.opts, ApplyRequest{
		SessionID: f.session.ID, ConfirmedCommentID: 99001, Proposal: confirmed,
	})
	if err != nil {
		t.Fatalf("ExecuteApplyResolution: %v\nlogs:\n%s", err, f.logs)
	}
	if result.Outcome != ApplyOutcomeFailed {
		t.Fatalf("Outcome = %s, want failed\nlogs:\n%s", result.Outcome, f.logs)
	}
	if result.Reason == "" {
		t.Error("Reason is empty for a failed application")
	}
	if result.Proposal.Status != store.ConflictProposalPending {
		t.Errorf("proposal status = %s, want still pending after a failed application", result.Proposal.Status)
	}

	if after := f.repo.GitIn(dir, "rev-parse", "HEAD"); after != before {
		t.Errorf("HEAD moved from %s to %s though the application failed and must roll back", before, after)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "left-behind.txt")); err != nil || string(got) != "oops\n" {
		t.Errorf("left-behind.txt = %q, %v; a failed application must not touch unrelated files", got, err)
	}

	// A fresh confirmation of the same proposal is not refused as
	// already-decided: pending means retryable in a later run (AC-12.10).
	_, refusal, err = ValidateConfirmation(t.Context(), f.db, f.session.ID, proposal.ID, testOwnerLogin, testOwnerLogin)
	if err != nil || refusal != ConfirmationOK {
		t.Fatalf("ValidateConfirmation after a failed application = (%s, %v), want ConfirmationOK", refusal, err)
	}
}
