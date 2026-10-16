package turn

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
)

type lostSyncReplyRepo struct{ ImplementRepo }

func (r lostSyncReplyRepo) Begin(ctx context.Context, branch string) (ImplementScratch, error) {
	s, err := r.ImplementRepo.Begin(ctx, branch)
	if err != nil {
		return nil, err
	}
	return lostSyncReplyScratch{s}, nil
}

type lostSyncReplyScratch struct{ ImplementScratch }

func (s lostSyncReplyScratch) Publish(ctx context.Context, branch, expected string) error {
	if err := s.ImplementScratch.Publish(ctx, branch, expected); err != nil {
		return err
	}
	return errors.New("push response lost")
}

type failedSyncRefresh struct{ SyncBaseCheckout }

func (c failedSyncRefresh) Refresh(context.Context, string, string) error {
	return errors.New("injected checkout failure")
}

func retainedSyncFixture(t *testing.T) (*implementGit, *git.Worktree, SyncBaseBinding) {
	t.Helper()
	g := newSyncGit(t)
	wt, err := g.clone.WorktreeAdd(t.Context(), filepath.Join(t.TempDir(), "retained"), git.WorktreeAddOptions{Branch: implementFeature})
	if err != nil {
		t.Fatal(err)
	}
	b := SyncBaseBinding{Repo: g.bindings.Repo, Bind: func(ctx context.Context, opts Options, req ActivityRequest) (SyncBaseTarget, error) {
		target, err := g.bindings.implementSyncBase(ctx, opts, req)
		target.Checkout = GitSyncBaseCheckout{Worktree: wt, Author: git.Author{Name: "Test", Email: "test@example.invalid"}}
		return target, err
	}}
	return g, wt, b
}
func executeRecoverySync(t *testing.T, g *implementGit, b SyncBaseBinding) (ActivityResult, syncBaseOutput, error) {
	t.Helper()
	res, err := (SyncBaseActivity{b}).Execute(t.Context(), Options{}, g.req())
	var out syncBaseOutput
	if len(res.Output) > 0 {
		if e := json.Unmarshal(res.Output, &out); e != nil {
			t.Fatal(e)
		}
	}
	return res, out, err
}

func TestSyncBaseRetainedWorkDefersPublication(t *testing.T) {
	for _, kind := range []string{"dirty", "local commit"} {
		t.Run(kind, func(t *testing.T) {
			g, wt, b := retainedSyncFixture(t)
			before := g.remoteFeature()
			ownerPath := filepath.Join(wt.Path(), "owner.txt")
			if err := os.WriteFile(ownerPath, []byte("owner bytes\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if kind == "local commit" {
				g.repo.GitIn(wt.Path(), "add", "owner.txt")
				g.repo.GitIn(wt.Path(), "commit", "-m", "retained local work")
			}
			retainedHead := g.repo.GitIn(wt.Path(), "rev-parse", "HEAD")
			g.advance(gittest.DefaultBranch, "fix.txt", "base fix\n")
			res, out, err := executeRecoverySync(t, g, b)
			if err != nil || res.Outcome != "conflict" || out.Result != "conflict" || len(out.Conflicts) != 0 || out.Reason == "" {
				t.Fatalf("%s %+v %v", res.Outcome, out, err)
			}
			if g.remoteFeature() != before || g.repo.GitIn(wt.Path(), "rev-parse", "HEAD") != retainedHead {
				t.Fatal("deferral changed retained or remote history")
			}
			bytes, err := os.ReadFile(ownerPath)
			if err != nil || string(bytes) != "owner bytes\n" {
				t.Fatalf("retained bytes %q %v", bytes, err)
			}
		})
	}
}

func TestSyncBasePartialEffectsRecoverFromGit(t *testing.T) {
	for _, failure := range []string{"lost reply", "refresh"} {
		t.Run(failure, func(t *testing.T) {
			g, wt, b := retainedSyncFixture(t)
			retainedHead := g.repo.GitIn(wt.Path(), "rev-parse", "HEAD")
			base := g.advance(gittest.DefaultBranch, "fix.txt", "base fix\n")
			recovery := b
			if failure == "lost reply" {
				b.Repo = lostSyncReplyRepo{b.Repo}
			} else {
				bind := b.Bind
				b.Bind = func(ctx context.Context, opts Options, req ActivityRequest) (SyncBaseTarget, error) {
					target, err := bind(ctx, opts, req)
					target.Checkout = failedSyncRefresh{target.Checkout}
					return target, err
				}
			}
			res, out, err := executeRecoverySync(t, g, b)
			if err == nil || res.Outcome != "" || out.Result != "error" || out.Commit == "" || out.CheckoutVerified || !strings.Contains(out.Reason, "reconcile") {
				t.Fatalf("failure missing evidence: %s %+v %v", res.Outcome, out, err)
			}
			published := g.remoteFeature()
			if published != out.Commit || g.repo.GitIn(wt.Path(), "rev-parse", "HEAD") != retainedHead {
				t.Fatal("partial effect evidence does not match Git")
			}
			wantPublication := "published"
			if failure == "lost reply" {
				wantPublication = "uncertain"
			}
			if out.Publication != wantPublication {
				t.Fatalf("publication %q", out.Publication)
			}
			res, out, err = executeRecoverySync(t, g, recovery)
			if err != nil || res.Outcome != "current" || !out.CheckoutVerified || out.Commit != published {
				t.Fatalf("recovery %s %+v %v", res.Outcome, out, err)
			}
			if g.repo.GitIn(wt.Path(), "rev-parse", "HEAD") != published || g.remoteFeature() != published || g.repo.Git("merge-base", base, published) != base {
				t.Fatal("recovery did not verify exact published handoff")
			}
		})
	}
}

func TestSyncBaseDefersIncomingReservedTransport(t *testing.T) {
	g, wt, b := retainedSyncFixture(t)
	before := g.remoteFeature()
	g.advance(gittest.DefaultBranch, ".litefactory-plan-proposal.json", "incoming transport\n")
	res, out, err := executeRecoverySync(t, g, b)
	if err != nil || res.Outcome != "conflict" || out.Result != "conflict" || len(out.Conflicts) != 0 || !strings.Contains(out.Reason, "reserved") {
		t.Fatalf("%s %+v %v", res.Outcome, out, err)
	}
	if g.remoteFeature() != before || g.repo.GitIn(wt.Path(), "rev-parse", "HEAD") != before {
		t.Fatal("reserved transport deferral changed history")
	}
}

func TestSyncBaseProposedHandoffPreservesIgnoredWork(t *testing.T) {
	g, wt, b := retainedSyncFixture(t)
	before := g.remoteFeature()
	g.advance(gittest.DefaultBranch, "incoming.txt", "incoming tracked bytes\n")
	g.repo.GitIn(wt.Path(), "config", "core.excludesFile", os.DevNull)
	exclude := g.repo.GitIn(wt.Path(), "rev-parse", "--git-path", "info/exclude")
	mustProgress(t, os.WriteFile(exclude, []byte("incoming.txt\n"), 0600))
	path := filepath.Join(wt.Path(), "incoming.txt")
	mustProgress(t, os.WriteFile(path, []byte("retained ignored bytes\n"), 0600))
	res, out, err := executeRecoverySync(t, g, b)
	if err != nil || res.Outcome != "conflict" || out.Result != "conflict" || len(out.Conflicts) != 0 || out.Reason != "incoming tracked paths overlap ignored retained work" {
		t.Fatalf("handoff refusal: %+v %+v %v", res, out, err)
	}
	data, err := os.ReadFile(path)
	mustProgress(t, err)
	if string(data) != "retained ignored bytes\n" || g.remoteFeature() != before || g.repo.GitIn(wt.Path(), "rev-parse", "HEAD") != before {
		t.Fatal("handoff refusal changed retained work or published")
	}
}
