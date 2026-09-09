package turn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
)

func TestRegionPreviewStatisticsMatchAppliedRawResult(t *testing.T) {
	t.Parallel()
	f, dir, proposal := blockedProposal(t)
	original, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	m := proposal.Files[0].Details.Metrics
	if m.BeforeBytes != len(original) || m.BeforeLines != git.LogicalLines(string(original)) {
		t.Fatal("preview baseline metrics do not describe task branch")
	}
	if proposal.BranchSHA != f.repo.GitIn(dir, "rev-parse", "HEAD") {
		t.Fatal("wrong preview baseline SHA")
	}
	rendered := conflictProposalReply(proposal)
	if rendered.BranchSHA != proposal.BranchSHA || rendered.Files[0].Metrics != report.ConflictPreviewMetrics(m) || rendered.Files[0].Diff == "" {
		t.Fatal("preview mapping lost identity/statistics/diff")
	}
	confirmed, refusal, err := ValidateConfirmation(t.Context(), f.db, f.session.ID, proposal.ID, ownerUserID, ownerUserID, testOwnerLogin)
	if err != nil || refusal != ConfirmationOK {
		t.Fatalf("confirmation: %v %v", refusal, err)
	}
	result, err := ExecuteApplyResolution(t.Context(), f.opts, ApplyRequest{SessionID: f.session.ID, ConfirmedCommentID: 99001, Proposal: confirmed})
	if err != nil || result.Outcome != ApplyOutcomeApplied {
		t.Fatalf("apply: %+v %v", result, err)
	}
	applied, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if m.AfterBytes != len(applied) || m.AfterLines != git.LogicalLines(string(applied)) || proposal.Files[0].Details.ResultDigest != git.ConflictDigest(string(applied)) {
		t.Fatal("preview raw result did not match application")
	}
}

func TestRegionPreviewOverflowPreventsProposalPersistence(t *testing.T) {
	t.Parallel()
	f, _, old := blockedProposal(t)
	renderer, err := report.New(report.Options{Redactor: redact.New(redact.Options{Environ: []string{}}), Limit: 300})
	if err != nil {
		t.Fatal(err)
	}
	f.opts.ConflictPreviewer = renderer
	req := f.request()
	req.Trigger.CommentID++
	result := f.execute(req)
	if result.ConflictProposal != nil || !result.ConflictSuggestionFailed || !strings.Contains(result.ConflictSuggestionReason, "metadata exceeds") {
		t.Fatalf("unreviewable proposal created: %+v", result)
	}
	still, err := f.db.GetConflictProposal(t.Context(), old.ID)
	if err != nil || still.Status != old.Status {
		t.Fatal("failed preview superseded old proposal")
	}
}

func TestRegionPreparedDiffSanitizedBeforeStoreMapping(t *testing.T) {
	t.Parallel()
	token := "ghp_abcdefghijklmnopqrstuvwxyz1234567890"
	renderer, err := report.New(report.Options{Redactor: redact.New(redact.Options{Environ: []string{"API_TOKEN=" + token}}), Limit: 65536})
	if err != nil {
		t.Fatal(err)
	}
	raw := git.ValidatedConflict{Path: "a.txt", Diff: "+ " + token + "\x1b[31m\r\n", Result: token}
	ref, err := naming.ParseRef("github:acme/acme-api#123")
	if err != nil {
		t.Fatal(err)
	}
	preview, err := renderer.PrepareConflictPreview(t.Context(), ref, rawConflictPreview([]git.ValidatedConflict{raw}, "codex", strings.Repeat("a", 40)))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(preview.Files[0].Diff, token) || strings.ContainsAny(preview.Files[0].Diff, "\x1b\r") {
		t.Fatal("raw sensitive diff survived")
	}
	if raw.Result != token {
		t.Fatal("preview altered raw result")
	}
}
