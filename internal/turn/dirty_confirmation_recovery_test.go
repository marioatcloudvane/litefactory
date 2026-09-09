package turn

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/store"
)

// AC-171-5 follows the owner's entire recovery sequence, keeping the
// committed conflict present until a fresh proposal can be reviewed.
func TestDirtyConfirmationRecoveryLifecycle(t *testing.T) {
	t.Parallel()
	f, dir, proposal := blockedProposal(t)
	for _, staged := range []bool{true, false} {
		text := "# fixture\nretained working edit\n"
		if staged {
			text = "# fixture\nretained staged edit\n"
		}
		if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		if staged {
			f.repo.GitIn(dir, "add", "README.md")
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "retained.txt"), []byte("unfinished owner work\n"), 0600); err != nil {
		t.Fatal(err)
	}
	before := captureRetained(t, f, dir)
	runner := &stubConflictRunner{
		stubRunner: newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
			assertRetained(t, before, captureRetained(t, f, req.WorktreePath))
			if !strings.Contains(req.Prompt, "The base was not merged") {
				t.Errorf("recovery prompt lacks deferral disclosure: %s", req.Prompt)
			}
			return succeeded(req, f.paths.Home()), nil
		}),
		propose: conflictSuggestion,
	}
	f.opts.Runners = RunnerSet{store.AgentClaude: runner}
	confirmed, refusal, err := ValidateConfirmation(t.Context(), f.db, f.session.ID, proposal.ID, ownerUserID, ownerUserID, testOwnerLogin)
	if err != nil || refusal != ConfirmationOK {
		t.Fatalf("confirmation = %s, %v", refusal, err)
	}
	applyReq := ApplyRequest{SessionID: f.session.ID, ConfirmedCommentID: 99001, Proposal: confirmed}
	refused, err := ExecuteApplyResolution(t.Context(), f.opts, applyReq)
	if err != nil || refused.Outcome != ApplyOutcomeFailed || refused.CommitSHA != "" {
		t.Fatalf("dirty apply = %+v, %v", refused, err)
	}
	for _, text := range []string{"not applied", "work is preserved", "proposal remains pending", "ordinary owner comment", "synchronization deferred", "clean branch", "fresh confirmation"} {
		if !strings.Contains(refused.Reason, text) {
			t.Errorf("recovery guidance lacks %q: %s", text, refused.Reason)
		}
	}
	assertRetained(t, before, captureRetained(t, f, dir))
	pending, err := f.db.GetConflictProposal(t.Context(), proposal.ID)
	if err != nil || pending.Status != store.ConflictProposalPending || pending.BranchSHA != proposal.BranchSHA || pending.BaseSHA != proposal.BaseSHA {
		t.Fatalf("pending proposal changed: %+v, %v", pending, err)
	}
	if runner.callCount() != 0 || runner.proposeCallCount() != 0 {
		t.Fatal("dirty confirmation started an agent")
	}

	req := f.request()
	req.Trigger.CommentID++
	req.Trigger.Body = "finish the retained work"
	recovered := f.execute(req)
	if !recovered.Succeeded() || !recovered.Committed || recovered.CommitSHA == before.head {
		t.Fatalf("recovery did not save new HEAD: %+v logs %s", recovered, f.logs)
	}
	if runner.callCount() != 1 || runner.proposeCallCount() != 0 {
		t.Fatal("normal dirty recovery did not bypass conflicting-tip preview")
	}
	runs := f.runs()
	if runs[len(runs)-1].SyncOutcome != store.SyncDeferredDirty {
		t.Fatalf("recovery sync = %s", runs[len(runs)-1].SyncOutcome)
	}
	if status := f.repo.GitIn(dir, "status", "--porcelain"); status != "" {
		t.Fatalf("recovery did not leave a clean tree: %s", status)
	}
	savedHead := f.repo.GitIn(dir, "rev-parse", "HEAD")
	applyReq.ConfirmedCommentID++
	stale, err := ExecuteApplyResolution(t.Context(), f.opts, applyReq)
	if err != nil || stale.Outcome != ApplyOutcomeSuperseded || stale.Proposal.Status != store.ConflictProposalSuperseded {
		t.Fatalf("old confirmation = %+v, %v", stale, err)
	}
	if head := f.repo.GitIn(dir, "rev-parse", "HEAD"); head != savedHead {
		t.Fatal("stale confirmation changed HEAD")
	}
	_, refusal, err = ValidateConfirmation(t.Context(), f.db, f.session.ID, proposal.ID, ownerUserID, ownerUserID, testOwnerLogin)
	if err != nil || refusal != ConfirmationSuperseded {
		t.Fatalf("old proposal still confirmable: %s, %v", refusal, err)
	}
	req.Trigger.CommentID++
	fresh := f.execute(req)
	if fresh.ConflictProposal == nil || fresh.ConflictProposal.ID == proposal.ID {
		t.Fatalf("clean retry lacks fresh proposal: %+v logs %s", fresh, f.logs)
	}
	next, err := f.db.GetConflictProposal(t.Context(), fresh.ConflictProposal.ID)
	if err != nil || next.Status != store.ConflictProposalPending || next.BranchSHA != savedHead || next.BaseSHA != proposal.BaseSHA {
		t.Fatalf("fresh proposal = %+v, %v", next, err)
	}
	if runner.callCount() != 1 || runner.proposeCallCount() != 1 {
		t.Fatal("fresh clean conflict did not reach only the advisor")
	}
}
