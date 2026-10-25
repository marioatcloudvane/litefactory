package turn

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/store"
)

// taskConflictFixture uses an independent remote task tip, leaving origin/main
// unchanged. The ordinary runner leaves the checkout untouched so a later
// explicit confirmation can still use the original proposal.
func taskConflictFixture(t *testing.T, advisor func(context.Context, agent.ConflictResolutionRequest) (agent.ConflictResolution, error)) (*fixture, string) {
	t.Helper()
	runner := &stubConflictRunner{stubRunner: newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
		return nil, errors.New("fixture leaves the original request unchanged for explicit confirmation")
	}), propose: advisor}
	f := newFixture(t, runner)
	f.opts.AutoSyncBase = true
	f.opts.BlockRunOnConflict = false // compatibility setting does not prevent continuation
	f.opts.SuggestConflictResolution = true
	f.opts.ConflictContentMaxBytes = 1 << 16
	dir := seedTaskWorktree(t, f)
	for _, side := range []struct{ dir, text string }{{f.repo.Dir, "operator change"}, {dir, "local change"}} {
		if err := os.WriteFile(filepath.Join(side.dir, "README.md"), []byte("# fixture\n"+side.text+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		f.repo.GitIn(side.dir, "add", "README.md")
		f.repo.GitIn(side.dir, "commit", "-qm", side.text)
	}
	f.repo.Git("push", "--quiet", "origin", "HEAD:refs/heads/"+testBranch)
	return f, dir
}

func taskBlockedProposal(t *testing.T) (*fixture, string, store.ConflictProposal) {
	t.Helper()
	f, dir := taskConflictFixture(t, conflictSuggestion)
	result := f.execute(f.request())
	if result.Disposition != DispositionRan || result.ConflictProposal == nil {
		t.Fatalf("task proposal: %+v\n%s", result, f.logs)
	}
	p, err := f.db.GetConflictProposal(t.Context(), result.ConflictProposal.ID)
	if err != nil {
		t.Fatal(err)
	}
	return f, dir, p
}

func applyTaskTestProposal(t *testing.T, f *fixture, p store.ConflictProposal) ApplyResult {
	t.Helper()
	c, refusal, err := ValidateConfirmation(t.Context(), f.db, f.session.ID, p.ID, ownerUserID, ownerUserID, testOwnerLogin)
	if err != nil || refusal != ConfirmationOK {
		t.Fatalf("confirmation: %s %v", refusal, err)
	}
	r, err := ExecuteApplyResolution(t.Context(), f.opts, ApplyRequest{SessionID: f.session.ID, ConfirmedCommentID: 99001, Proposal: c})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestTaskConflictProposalPreviewAndApply(t *testing.T) {
	t.Parallel()
	var request agent.ConflictResolutionRequest
	f, dir := taskConflictFixture(t, func(ctx context.Context, r agent.ConflictResolutionRequest) (agent.ConflictResolution, error) {
		request = r
		return conflictSuggestion(ctx, r)
	})
	// Base has additional work too; task confirmation must finish only its approved merge.
	f.repo.Git("commit", "--allow-empty", "-qm", "remaining base work")
	f.repo.Git("push", "--quiet", "origin", "HEAD:refs/heads/"+gittest.DefaultBranch)
	before := f.repo.GitIn(dir, "rev-parse", "HEAD")
	remote := f.repo.GitIn(f.repo.Origin, "rev-parse", testBranch)
	base := f.repo.GitIn(f.repo.Origin, "rev-parse", gittest.DefaultBranch)
	result := f.execute(f.request())
	if result.ConflictProposal == nil {
		t.Fatalf("missing proposal: %+v\n%s", result, f.logs)
	}
	p, err := f.db.GetConflictProposal(t.Context(), result.ConflictProposal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Inputs.Observed || p.Inputs.TargetPhase != "task" || p.Inputs.TargetRef != "origin/"+testBranch || p.Inputs.TaskSHA != remote || p.Inputs.BaseSHA != base || p.BranchSHA != before {
		t.Fatalf("incorrect durable inputs: %+v", p)
	}
	if request.TargetPhase != "task" || request.TargetSHA != remote || request.LocalSHA != before || request.BaseInputSHA != base || request.TaskInputSHA != remote {
		t.Fatalf("incorrect advisor inputs: %+v", request)
	}
	if head := f.repo.GitIn(dir, "rev-parse", "HEAD"); head != before {
		t.Fatal("proposal mutated HEAD")
	}
	forge := newFakeForge(t)
	reporter := newReporter(t, f, forge, nil)
	if _, err := reporter.ReportTurn(t.Context(), result); err != nil {
		t.Fatal(err)
	}
	body := forge.only(t).body
	for _, want := range []string{"task", "origin/" + testBranch, "This is a suggestion only", "resolved by the advisor"} {
		if !strings.Contains(body, want) {
			t.Errorf("preview missing %q: %s", want, body)
		}
	}
	applied := applyTaskTestProposal(t, f, p)
	if applied.Outcome != ApplyOutcomeApplied || applied.CommitSHA == "" || applied.Proposal.Status != store.ConflictProposalApplied {
		t.Fatalf("apply: %+v", applied)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "README.md")); err != nil || string(got) != "# fixture\nresolved by the advisor\n" {
		t.Fatalf("approved bytes %q %v", got, err)
	}
	parents := strings.Fields(f.repo.GitIn(dir, "log", "-1", "--pretty=%P"))
	if len(parents) != 2 || parents[0] != before || parents[1] != remote {
		t.Fatalf("parents %v; want %s %s", parents, before, remote)
	}
	if got := f.repo.GitIn(f.repo.Origin, "rev-parse", testBranch); got != applied.CommitSHA {
		t.Fatal("approved merge not published")
	}
	_, refusal, err := ValidateConfirmation(t.Context(), f.db, f.session.ID, p.ID, ownerUserID, ownerUserID, testOwnerLogin)
	if err != nil || refusal != ConfirmationAlreadyApplied {
		t.Fatalf("consumption %s %v", refusal, err)
	}
	runs, err := f.db.ListRuns(t.Context(), f.session.ID)
	if err != nil || len(runs) != 1 || runs[0].ID != result.Run.ID {
		t.Fatalf("confirmation allocated a run beyond the original request: %v %v", runs, err)
	}
}

func TestTaskConflictConfirmationFreshness(t *testing.T) {
	for _, target := range []string{"task", "base"} {
		for _, change := range []string{"head", "base", "task", "task-absent", "historical-unbound"} {
			if target == "base" && change == "task-absent" {
				continue
			}
			t.Run(target+"/"+change, func(t *testing.T) {
				t.Parallel()
				factory := taskBlockedProposal
				if target == "base" {
					factory = blockedProposal
				}
				f, dir, p := factory(t)
				switch change {
				case "head":
					f.repo.GitIn(dir, "commit", "--allow-empty", "-qm", "local movement")
				case "base":
					f.repo.Git("commit", "--allow-empty", "-qm", "base movement")
					f.repo.Git("push", "--quiet", "origin", "HEAD:refs/heads/"+gittest.DefaultBranch)
				case "task":
					f.repo.Git("commit", "--allow-empty", "-qm", "task movement")
					f.repo.Git("push", "--quiet", "origin", "HEAD:refs/heads/"+testBranch)
				case "task-absent":
					f.repo.Git("push", "--quiet", "origin", "--delete", testBranch)
				case "historical-unbound":
					var err error
					p, _, err = f.db.CreateOrGetConflictProposal(t.Context(), store.CreateConflictProposalParams{SessionID: f.session.ID, TriggerCommentID: 999, Model: p.Model, Format: p.Format, BaseSHA: p.BaseSHA, BranchSHA: p.BranchSHA, Files: p.Files})
					if err != nil {
						t.Fatal(err)
					}
				}
				gittest.AssertUnchanged(t, dir, func() {
					r := applyTaskTestProposal(t, f, p)
					if r.Outcome != ApplyOutcomeSuperseded || r.Proposal.Status != store.ConflictProposalSuperseded {
						t.Fatalf("stale inputs applied: %+v", r)
					}
				})
			})
		}
	}
}

func TestTaskConflictConfirmationPreservation(t *testing.T) {
	for _, scenario := range []string{"dirty", "tampered-resolution", "excluded-path", "push-failure"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			f, dir, p := taskBlockedProposal(t)
			before := f.repo.GitIn(dir, "rev-parse", "HEAD")
			remote := f.repo.GitIn(f.repo.Origin, "rev-parse", testBranch)
			switch scenario {
			case "dirty":
				if err := os.WriteFile(filepath.Join(dir, "unfinished.txt"), []byte("preserve me\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "tampered-resolution", "excluded-path":
				if scenario == "tampered-resolution" {
					p.Files[0].Details.Regions[0].Resolution = "unapproved bytes\n"
				} else {
					p.Files[0].Path = ".env"
				}
				var err error
				p, _, err = f.db.CreateOrGetConflictProposal(t.Context(), store.CreateConflictProposalParams{SessionID: f.session.ID, TriggerCommentID: 999, Model: p.Model, Format: p.Format, BaseSHA: p.BaseSHA, BranchSHA: p.BranchSHA, Inputs: p.Inputs, Files: p.Files})
				if err != nil {
					t.Fatal(err)
				}
			case "push-failure":
				f.repo.Git("config", "remote.origin.pushurl", filepath.Join(t.TempDir(), "missing.git"))
			}
			check := func() {
				r := applyTaskTestProposal(t, f, p)
				if r.Outcome != ApplyOutcomeFailed || r.Proposal.Status != store.ConflictProposalPending || r.Reason == "" {
					t.Fatalf("failure semantics: %+v", r)
				}
				if scenario == "push-failure" {
					head := f.repo.GitIn(dir, "rev-parse", "HEAD")
					if head == before || r.CommitSHA != head || !strings.Contains(r.Reason, "could not be pushed") {
						t.Fatalf("lost local commit evidence %+v HEAD %s", r, head)
					}
				}
			}
			if scenario == "push-failure" {
				check()
			} else {
				gittest.AssertUnchanged(t, dir, check)
			}
			if got := f.repo.GitIn(f.repo.Origin, "rev-parse", testBranch); got != remote {
				t.Fatal("failed apply altered origin")
			}
		})
	}
}

func TestTaskConflictAdvisorFallback(t *testing.T) {
	for _, scenario := range []string{"unavailable", "invalid", "excluded"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			f, dir := taskConflictFixture(t, func(ctx context.Context, req agent.ConflictResolutionRequest) (agent.ConflictResolution, error) {
				if scenario == "unavailable" {
					return agent.ConflictResolution{}, errors.New("advisor unavailable")
				}
				r, err := conflictSuggestion(ctx, req)
				if scenario == "invalid" {
					*r.Files[0].Regions[0].Resolution = "<<<<<<< unresolved\n"
				} else {
					r.Files[0].Path = ".env"
				}
				return r, err
			})
			gittest.AssertUnchanged(t, dir, func() {
				r := f.execute(f.request())
				if r.Disposition != DispositionRan || r.ConflictProposal != nil || !r.ConflictSuggestionFailed {
					t.Fatalf("unsupported proposal accepted: %+v", r)
				}
				forge := newFakeForge(t)
				reporter := newReporter(t, f, forge, nil)
				if _, err := reporter.ReportTurn(t.Context(), r); err != nil {
					t.Fatal(err)
				}
				body := forge.only(t).body
				if !strings.Contains(body, "No suggested resolution") || !strings.Contains(body, "task synchronization") || strings.Count(body, "Preparation caveat") != 1 {
					t.Fatalf("missing combined manual-resolution caveat: %s", body)
				}
			})
		})
	}
}

func TestTaskConflictConfirmationOwnerSessionExpiry(t *testing.T) {
	for _, scenario := range []string{"nonowner-same-login", "other-session", "expired"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			f, dir, p := taskBlockedProposal(t)
			session, commenter := f.session.ID, ownerUserID
			want := ConfirmationNotOwner
			switch scenario {
			case "nonowner-same-login":
				commenter = strangerUserID
			case "other-session":
				session++
				want = ConfirmationUnknownProposal
			case "expired":
				cutoff := p.CreatedAt.Add(time.Hour)
				if _, err := f.db.ExpireConflictProposals(t.Context(), cutoff, cutoff.Add(time.Minute)); err != nil {
					t.Fatal(err)
				}
				want = ConfirmationExpired
			}
			gittest.AssertUnchanged(t, dir, func() {
				_, refusal, err := ValidateConfirmation(t.Context(), f.db, session, p.ID, ownerUserID, commenter, testOwnerLogin)
				if err != nil || refusal != want {
					t.Fatalf("identity gate %s %v want %s", refusal, err, want)
				}
			})
			stored, err := f.db.GetConflictProposal(t.Context(), p.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.AppliedCommitSHA != "" || (scenario != "expired" && stored.Status != store.ConflictProposalPending) {
				t.Fatalf("unauthorized application: %+v", stored)
			}
		})
	}
}

func TestTaskConflictProposalRedactsPreview(t *testing.T) {
	t.Parallel()
	token := "ghp_abcdefghijklmnopqrstuvwxyz1234567890"
	f, dir := taskConflictFixture(t, func(ctx context.Context, req agent.ConflictResolutionRequest) (agent.ConflictResolution, error) {
		r, err := conflictSuggestion(ctx, req)
		*r.Files[0].Regions[0].Resolution = "resolved " + token + "\n"
		return r, err
	})
	gittest.AssertUnchanged(t, dir, func() {
		r := f.execute(f.request())
		if r.ConflictProposal == nil {
			t.Fatalf("missing task proposal %+v\n%s", r, f.logs)
		}
		forge := newFakeForge(t)
		reporter := newReporter(t, f, forge, nil)
		if _, err := reporter.ReportTurn(t.Context(), r); err != nil {
			t.Fatal(err)
		}
		body := forge.only(t).body
		if strings.Contains(body, token) || !strings.Contains(body, "task") || !strings.Contains(body, "This is a suggestion only") {
			t.Fatalf("unsafe or missing task preview %s", body)
		}
	})
}
