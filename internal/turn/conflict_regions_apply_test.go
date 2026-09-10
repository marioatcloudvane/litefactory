package turn

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestRegionConfirmationLegacyUnknownAndFreshAuthorization(t *testing.T) {
	t.Parallel()
	f, dir, supported := blockedProposal(t)
	for _, format := range []int{0, 99} {
		params := store.CreateConflictProposalParams{SessionID: f.session.ID, TriggerCommentID: int64(500 + format), Model: "codex", Format: format,
			BaseSHA: supported.BaseSHA, BranchSHA: supported.BranchSHA, Files: []store.ConflictProposalFile{{Path: "README.md", Resolution: "legacy fragment"}}}
		legacy, _, err := f.db.CreateOrGetConflictProposal(t.Context(), params)
		if err != nil {
			t.Fatal(err)
		}
		gittest.AssertUnchanged(t, dir, func() {
			_, refusal, err := ValidateConfirmation(t.Context(), f.db, f.session.ID, legacy.ID, ownerUserID, ownerUserID, testOwnerLogin)
			if err != nil || refusal != ConfirmationUnsupportedFormat {
				t.Fatalf("format %d refusal %v error %v", format, refusal, err)
			}
		})
	}
	fresh, _, err := f.db.CreateOrGetConflictProposal(t.Context(), store.CreateConflictProposalParams{SessionID: f.session.ID, TriggerCommentID: 999, Model: "codex", Format: 1,
		BaseSHA: supported.BaseSHA, BranchSHA: supported.BranchSHA, Inputs: supported.Inputs, Files: supported.Files})
	if err != nil {
		t.Fatal(err)
	}
	if fresh.ID == supported.ID {
		t.Fatal("regeneration reused proposal identity")
	}
	_, refusal, err := ValidateConfirmation(t.Context(), f.db, f.session.ID, supported.ID, ownerUserID, ownerUserID, testOwnerLogin)
	if err != nil || refusal != ConfirmationSuperseded {
		t.Fatalf("old confirmation retained authority: %v %v", refusal, err)
	}
	_, refusal, err = ValidateConfirmation(t.Context(), f.db, f.session.ID, fresh.ID, ownerUserID, strangerUserID, testOwnerLogin)
	if err != nil || refusal != ConfirmationNotOwner {
		t.Fatalf("fresh proposal bypassed numeric ownership: %v %v", refusal, err)
	}
}

func TestRegionConfirmationRechecksStoredPayload(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(*store.ConflictProposalFile)
	}{
		{"resolution", func(f *store.ConflictProposalFile) { f.Details.Regions[0].Resolution = "tampered replacement\n" }},
		{"digest", func(f *store.ConflictProposalFile) { f.Details.OriginalDigest = "bad" }},
		{"metrics", func(f *store.ConflictProposalFile) { f.Details.Metrics.AfterLines++ }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f, dir, proposal := blockedProposal(t)
			tc.change(&proposal.Files[0])
			changed, _, err := f.db.CreateOrGetConflictProposal(t.Context(), store.CreateConflictProposalParams{SessionID: f.session.ID, TriggerCommentID: 900, Model: "codex", Format: 1, BaseSHA: proposal.BaseSHA, BranchSHA: proposal.BranchSHA, Inputs: proposal.Inputs, Files: proposal.Files})
			if err != nil {
				t.Fatal(err)
			}
			confirmed, refusal, err := ValidateConfirmation(t.Context(), f.db, f.session.ID, changed.ID, ownerUserID, ownerUserID, testOwnerLogin)
			if err != nil || refusal != ConfirmationOK {
				t.Fatalf("identity gate %v %v", refusal, err)
			}
			gittest.AssertUnchanged(t, dir, func() {
				result, err := ExecuteApplyResolution(t.Context(), f.opts, ApplyRequest{SessionID: f.session.ID, ConfirmedCommentID: 901, Proposal: confirmed})
				if err != nil || result.Outcome != ApplyOutcomeFailed {
					t.Fatalf("tampered proposal applied: %+v %v", result, err)
				}
			})
		})
	}
}

func TestRegionConfirmationPushFailureRetainsLocalCommit(t *testing.T) {
	t.Parallel()
	f, dir, proposal := blockedProposal(t)
	before := f.repo.GitIn(dir, "rev-parse", "HEAD")
	remoteBefore := f.repo.Git("--git-dir", f.repo.Origin, "show-ref")
	f.repo.Git("config", "remote.origin.pushurl", filepath.Join(t.TempDir(), "missing.git"))
	confirmed, refusal, err := ValidateConfirmation(t.Context(), f.db, f.session.ID, proposal.ID, ownerUserID, ownerUserID, testOwnerLogin)
	if err != nil || refusal != ConfirmationOK {
		t.Fatalf("confirmation: %v %v", refusal, err)
	}
	result, err := ExecuteApplyResolution(t.Context(), f.opts, ApplyRequest{SessionID: f.session.ID, ConfirmedCommentID: 99001, Proposal: confirmed})
	if err != nil || result.Outcome != ApplyOutcomeFailed || !strings.Contains(result.Reason, "could not be pushed") {
		t.Fatalf("push failure outcome: %+v %v", result, err)
	}
	after := f.repo.GitIn(dir, "rev-parse", "HEAD")
	if after == before || len(strings.Fields(f.repo.GitIn(dir, "log", "-1", "--pretty=%P"))) != 2 {
		t.Fatal("successful local merge commit was undone")
	}
	if f.repo.GitIn(dir, "status", "--porcelain") != "" {
		t.Fatal("completed local commit left dirty state")
	}
	if f.repo.Git("--git-dir", f.repo.Origin, "show-ref") != remoteBefore {
		t.Fatal("failed push changed remote")
	}
	stored, err := f.db.GetConflictProposal(t.Context(), proposal.ID)
	if err != nil || stored.Status != store.ConflictProposalPending || stored.AppliedCommitSHA != "" {
		t.Fatalf("post-push failure semantics changed: %+v %v", stored, err)
	}
}
