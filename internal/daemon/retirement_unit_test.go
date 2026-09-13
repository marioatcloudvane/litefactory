package daemon_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
)

type retirementIssueStub struct {
	issue github.Issue
	err   error
}

func (s retirementIssueStub) GetIssue(context.Context, github.Repo, int) (github.Issue, error) {
	return s.issue, s.err
}
func retirementUnitFixture() (store.RetirementOwner, daemon.RetirementOptions, git.RetirementObservation) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	owner := store.RetirementOwner{Canonical: true, RunCount: 1, LastFinishedAt: now.Add(-15 * 24 * time.Hour)}
	owner.Repo = store.Repo{Forge: store.ForgeGitHub, Owner: "owner", Name: "repo", FeaturePrefix: "lf", WorktreeRoot: "/worktrees", BaseBranch: "main"}
	owner.Session = store.Session{TaskRef: "github:owner/repo#42", IssueNumber: 42, FeatureSlug: "feature", Branch: "lf/42-feature", WorktreePath: "/worktrees/task-42", CreatedAt: now.Add(-100 * 24 * time.Hour), UpdatedAt: now}
	obs := git.RetirementObservation{BaseSHA: "base", LocalSHA: "local", RemoteSHA: "remote", WorktreeSHA: "local", WorktreeExists: true, WorktreeSafe: true, LocalPreserved: true, LocalMerged: true, RemoteMerged: true, BranchInUse: true}
	opts := daemon.RetirementOptions{Now: now, RetainDays: 14, Issues: retirementIssueStub{issue: github.Issue{ID: 100, Number: 42, State: "closed", APIURL: "https://api.github.com/repos/owner/repo/issues/42", StateReason: "completed"}}}
	return owner, opts, obs
}

func TestRetirementIssueClosed_Identity(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		change func(*github.Issue)
		want   bool
	}{
		{"closed matching issue", func(*github.Issue) {}, true},
		{"open", func(i *github.Issue) { i.State = "open" }, false},
		{"unknown state", func(i *github.Issue) { i.State = "merged" }, false},
		{"null issue", func(i *github.Issue) { *i = github.Issue{} }, false},
		{"different number", func(i *github.Issue) { i.Number = 43 }, false},
		{"different repo", func(i *github.Issue) { i.APIURL = "https://api.github.com/repos/other/repo/issues/42" }, false},
		{"malformed URL", func(i *github.Issue) { i.APIURL = ":%" }, false},
		{"missing ID", func(i *github.Issue) { i.ID = 0 }, false},
		{"query suffix", func(i *github.Issue) { i.APIURL += "?state=closed" }, false},
		{"abandoned closed", func(i *github.Issue) { i.StateReason = "not_planned" }, true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			owner, opts, _ := retirementUnitFixture()
			stub, ok := opts.Issues.(retirementIssueStub)
			if !ok {
				t.Fatal("fixture issue reader is not a retirementIssueStub")
			}
			issue := stub.issue
			tt.change(&issue)
			if got := daemon.RetirementIssueClosed(owner.Repo, 42, issue); got != tt.want {
				t.Fatalf("closed = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvaluateRetirement_ConservativeDecisions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		change  func(*store.RetirementOwner, *daemon.RetirementOptions, *git.RetirementObservation)
		actions [3]string
	}{
		{"expired merged", func(*store.RetirementOwner, *daemon.RetirementOptions, *git.RetirementObservation) {}, [3]string{"would_remove", "would_remove", "would_remove"}},
		{"recent run despite old creation", func(o *store.RetirementOwner, p *daemon.RetirementOptions, g *git.RetirementObservation) {
			o.LastFinishedAt = p.Now.Add(-time.Hour)
		}, [3]string{"kept", "kept", "would_remove"}},
		{"exact retention boundary", func(o *store.RetirementOwner, p *daemon.RetirementOptions, g *git.RetirementObservation) {
			o.LastFinishedAt = p.Now.Add(-14 * 24 * time.Hour)
		}, [3]string{"would_remove", "would_remove", "would_remove"}},
		{"no run creation fallback", func(o *store.RetirementOwner, p *daemon.RetirementOptions, g *git.RetirementObservation) {
			o.RunCount = 0
			o.LastFinishedAt = time.Time{}
		}, [3]string{"would_remove", "would_remove", "would_remove"}},
		{"run missing finish", func(o *store.RetirementOwner, p *daemon.RetirementOptions, g *git.RetirementObservation) {
			o.LastFinishedAt = time.Time{}
		}, [3]string{"kept", "kept", "would_remove"}},
		{"active", func(o *store.RetirementOwner, p *daemon.RetirementOptions, g *git.RetirementObservation) {
			o.Active = true
		}, [3]string{"kept", "kept", "kept"}},
		{"pending effect", func(o *store.RetirementOwner, p *daemon.RetirementOptions, g *git.RetirementObservation) {
			o.Pending = true
		}, [3]string{"kept", "kept", "kept"}},
		{"shared child not canonical", func(o *store.RetirementOwner, p *daemon.RetirementOptions, g *git.RetirementObservation) {
			o.Canonical = false
		}, [3]string{"kept", "kept", "kept"}},
		{"manual path", func(o *store.RetirementOwner, p *daemon.RetirementOptions, g *git.RetirementObservation) {
			o.Session.WorktreePath = "/manual"
		}, [3]string{"kept", "kept", "kept"}},
		{"local files defer all", func(o *store.RetirementOwner, p *daemon.RetirementOptions, g *git.RetirementObservation) {
			g.Dirty = true
		}, [3]string{"kept", "kept", "kept"}},
		{"local only commits defer all", func(o *store.RetirementOwner, p *daemon.RetirementOptions, g *git.RetirementObservation) {
			g.LocalPreserved = false
		}, [3]string{"kept", "kept", "kept"}},
		{"unmerged preserved abandonment", func(o *store.RetirementOwner, p *daemon.RetirementOptions, g *git.RetirementObservation) {
			g.LocalMerged = false
			g.RemoteMerged = false
		}, [3]string{"would_remove", "kept", "kept"}},
		{"independent remote tip", func(o *store.RetirementOwner, p *daemon.RetirementOptions, g *git.RetirementObservation) {
			g.RemoteMerged = false
		}, [3]string{"would_remove", "would_remove", "kept"}},
		{"independent local tip", func(o *store.RetirementOwner, p *daemon.RetirementOptions, g *git.RetirementObservation) {
			g.LocalMerged = false
		}, [3]string{"would_remove", "kept", "would_remove"}},
		{"other checkout uses branch", func(o *store.RetirementOwner, p *daemon.RetirementOptions, g *git.RetirementObservation) {
			g.OtherWorktreeUsesBranch = true
		}, [3]string{"would_remove", "kept", "would_remove"}},
		{"protected base", func(o *store.RetirementOwner, p *daemon.RetirementOptions, g *git.RetirementObservation) {
			g.Protected = true
		}, [3]string{"kept", "kept", "kept"}},
		{"missing upstream preserved in base", func(o *store.RetirementOwner, p *daemon.RetirementOptions, g *git.RetirementObservation) {
			g.RemoteSHA = ""
		}, [3]string{"would_remove", "would_remove", "already_absent"}},
		{"unsafe checkout", func(o *store.RetirementOwner, p *daemon.RetirementOptions, g *git.RetirementObservation) {
			g.WorktreeSafe = false
			g.WorktreeReason = "locked"
		}, [3]string{"kept", "kept", "kept"}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			o, p, g := retirementUnitFixture()
			tt.change(&o, &p, &g)
			p.ObserveGit = func(context.Context, store.RetirementOwner) (git.RetirementObservation, error) { return g, nil }
			got := daemon.EvaluateRetirement(context.Background(), p, o)
			if len(got.Decisions) != 3 {
				t.Fatalf("decisions = %+v", got)
			}
			for i, d := range got.Decisions {
				if d.Action != tt.actions[i] || d.Reason == "" {
					t.Errorf("%s = %s (%s), want %s and reason", d.Kind, d.Action, d.Reason, tt.actions[i])
				}
			}
			if !got.ObservedAt.Equal(p.Now) {
				t.Error("observation time not retained")
			}
		})
	}
}

func TestEvaluateRetirement_UnavailableEvidence(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"issue", "git"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			o, p, g := retirementUnitFixture()
			failure := errors.New("unavailable")
			p.ObserveGit = func(context.Context, store.RetirementOwner) (git.RetirementObservation, error) {
				if stage == "git" {
					return g, failure
				}
				t.Error("Git must not be inspected without closure")
				return g, nil
			}
			if stage == "issue" {
				p.Issues = retirementIssueStub{err: failure}
			}
			got := daemon.EvaluateRetirement(context.Background(), p, o)
			if !errors.Is(got.Error, failure) {
				t.Fatalf("cause = %v", got.Error)
			}
			for _, d := range got.Decisions {
				if d.Action != "unknown" {
					t.Errorf("%s action = %s", d.Kind, d.Action)
				}
			}
		})
	}
}

func TestEvaluateRetirement_ClosureAndEvidence(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"open", "closed", "unknown", ""} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			o, p, g := retirementUnitFixture()
			stub, ok := p.Issues.(retirementIssueStub)
			if !ok {
				t.Fatal("fixture issue reader is not a retirementIssueStub")
			}
			issue := stub.issue
			issue.State = state
			p.Issues = retirementIssueStub{issue: issue}
			p.ObserveGit = func(context.Context, store.RetirementOwner) (git.RetirementObservation, error) {
				if state != "closed" {
					t.Error("Git inspected without closed issue")
				}
				return g, nil
			}
			got := daemon.EvaluateRetirement(context.Background(), p, o)
			want := "unknown"
			if state == "open" {
				want = "kept"
			}
			if state == "closed" {
				want = "would_remove"
			}
			for _, d := range got.Decisions {
				if d.Action != want {
					t.Errorf("%s=%s want %s", d.Kind, d.Action, want)
				}
			}
			if got.IssueState != state || got.IssueReason != issue.StateReason {
				t.Error("issue observation lost")
			}
			if state == "closed" {
				if got.RetentionSource != "last_completed_run" || !got.RetentionAt.Equal(o.LastFinishedAt) || !got.ExpiresAt.Equal(o.LastFinishedAt.Add(14*24*time.Hour)) {
					t.Fatalf("retention evidence = %+v", got)
				}
				if got.Git.BaseSHA != "base" || got.Decisions[0].ExpectedSHA != "local" || got.Decisions[2].ExpectedSHA != "remote" {
					t.Error("exact SHA evidence lost")
				}
			}
		})
	}
}
