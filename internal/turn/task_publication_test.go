package turn

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/harness"
)

// AC-009: publication follows outstanding commits, not agent edits. Real
// local origins make remote equality and ancestry observable independently.
func TestTaskPublicationNoOpRunner(t *testing.T) {
	for _, scenario := range []string{"preparation merge", "prior retained commit", "already published", "remote fast forward"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			var f *fixture
			var runnerHead string
			f = newFixture(t, newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
				runnerHead = strings.TrimSpace(f.repo.GitIn(f.worktreeDir(), "rev-parse", "HEAD"))
				return succeeded(req, f.paths.Home()), nil
			}))
			if r := executePublicationTurn(f); !r.Succeeded() {
				t.Fatalf("setup: %+v", r)
			}
			f.repo.GitIn(f.worktreeDir(), "push", "origin", "HEAD:refs/heads/"+testBranch)
			before := strings.TrimSpace(f.repo.GitIn(f.worktreeDir(), "rev-parse", "HEAD"))
			switch scenario {
			case "preparation merge":
				advanceSyncBase(t, f.repo, 1)
			case "prior retained commit":
				commitInSyncDir(t, f.repo, f.worktreeDir(), "retained.txt", "retained prior attempt")
			case "remote fast forward":
				f.repo.Git("checkout", "-b", "operator", before)
				commitInSyncDir(t, f.repo, f.repo.Dir, "operator.txt", "operator fix")
				f.repo.Git("push", "origin", "HEAD:refs/heads/"+testBranch)
			}
			r := executePublicationTurn(f)
			if !r.Succeeded() || !r.Pushed {
				t.Fatalf("publication: %+v", r)
			}
			local := strings.TrimSpace(f.repo.GitIn(f.worktreeDir(), "rev-parse", "HEAD"))
			remote := strings.Fields(f.repo.Git("ls-remote", "origin", "refs/heads/"+testBranch))[0]
			if local != remote || local != runnerHead {
				t.Fatalf("local=%s remote=%s verified=%s", local, remote, runnerHead)
			}
			if r.Changed || r.Committed || len(r.Files()) != 0 {
				t.Fatalf("invented agent changes: %+v", r)
			}
			if scenario == "already published" && local != before {
				t.Fatal("equal branch gained a commit")
			}
			if scenario == "preparation merge" && len(strings.Fields(f.repo.GitIn(f.worktreeDir(), "show", "-s", "--format=%P", "HEAD"))) != 2 {
				t.Fatal("expected actual preparation merge")
			}
		})
	}
}

// AC-010: failure retains a preparation merge and dirty work; a subsequent
// successful no-op run publishes the outstanding commit after recovery.
func TestTaskPublicationFailedRunnerRetainsWork(t *testing.T) {
	t.Parallel()
	var f *fixture
	fail := false
	f = newFixture(t, newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		r := succeeded(req, f.paths.Home())
		if fail {
			writeInWorktree(t, req, "unfinished.txt", "unfinished bytes\n")
			r.Outcome = harness.OutcomeFailed
			r.ExitCode = 1
		}
		return r, nil
	}))
	if r := executePublicationTurn(f); !r.Succeeded() {
		t.Fatalf("setup: %+v", r)
	}
	f.repo.GitIn(f.worktreeDir(), "push", "origin", "HEAD:refs/heads/"+testBranch)
	remoteBefore := strings.Fields(f.repo.Git("ls-remote", "origin", "refs/heads/"+testBranch))[0]
	advanceSyncBase(t, f.repo, 1)
	fail = true
	r := executePublicationTurn(f)
	head := strings.TrimSpace(f.repo.GitIn(f.worktreeDir(), "rev-parse", "HEAD"))
	if r.Succeeded() || r.Pushed || head == remoteBefore {
		t.Fatalf("failure did not preserve unpublished merge: %+v", r)
	}
	if remote := strings.Fields(f.repo.Git("ls-remote", "origin", "refs/heads/"+testBranch))[0]; remote != remoteBefore {
		t.Fatal("failed runner published")
	}
	data, err := os.ReadFile(filepath.Join(f.worktreeDir(), "unfinished.txt"))
	if err != nil || string(data) != "unfinished bytes\n" {
		t.Fatalf("lost edits: %q %v", data, err)
	}
	if !strings.Contains(r.Publication, head) || !strings.Contains(r.Publication, "retained") {
		t.Fatalf("missing retained evidence: %s", r.Publication)
	}
	// Explicit local operator recovery preserves the edits as a commit.
	f.repo.GitIn(f.worktreeDir(), "add", "unfinished.txt")
	f.repo.GitIn(f.worktreeDir(), "commit", "-m", "recover unfinished work")
	recovered := strings.TrimSpace(f.repo.GitIn(f.worktreeDir(), "rev-parse", "HEAD"))
	fail = false
	r = executePublicationTurn(f)
	if !r.Succeeded() || !r.Pushed || r.Changed || r.Committed {
		t.Fatalf("recovered publication: %+v", r)
	}
	if remote := strings.Fields(f.repo.Git("ls-remote", "origin", "refs/heads/"+testBranch))[0]; remote != recovered {
		t.Fatal("prior work not published")
	}
}

// AC-011: the operator is a real second writer. Its advance occurs while the
// fake runner is executing, after preparation and before final publication.
func TestTaskPublicationConcurrentWriter(t *testing.T) {
	for _, scenario := range []string{"different remote tip", "movement during push", "identical during push", "identical remote tip", "generic rejection"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			var f *fixture
			active := false
			var verified, operatorHead string
			f = newFixture(t, newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
				if active {
					commitInSyncDir(t, f.repo, f.worktreeDir(), "verified.txt", "verified work")
					verified = strings.TrimSpace(f.repo.GitIn(f.worktreeDir(), "rev-parse", "HEAD"))
					switch scenario {
					case "different remote tip":
						commitInSyncDir(t, f.repo, f.repo.Dir, "operator.txt", "concurrent operator")
						operatorHead = strings.TrimSpace(f.repo.Git("rev-parse", "HEAD"))
						f.repo.Git("push", "origin", "HEAD:refs/heads/"+testBranch)
					case "movement during push":
						commitInSyncDir(t, f.repo, f.repo.Dir, "operator.txt", "operator during publication")
						operatorHead = strings.TrimSpace(f.repo.Git("rev-parse", "HEAD"))
						// The hook pauses the publishing writer until a second real push
						// has advanced origin; disabling hooks only for that writer
						// avoids recursive invocation of this deterministic barrier.
						quotedDir := "'" + strings.ReplaceAll(f.repo.Dir, "'", "'\"'\"'") + "'"
						hook := "#!/bin/sh\nunset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_COMMON_DIR\ngit -C " + quotedDir + " -c core.hooksPath=/dev/null push origin HEAD:refs/heads/" + testBranch + "\n"
						if err := os.WriteFile(filepath.Join(f.repo.Dir, ".git", "hooks", "pre-push"), []byte(hook), 0700); err != nil {
							t.Fatal(err)
						}
					case "identical during push":
						quotedDir := "'" + strings.ReplaceAll(f.worktreeDir(), "'", "'\"'\"'") + "'"
						hook := "#!/bin/sh\nunset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_COMMON_DIR\ngit -C " + quotedDir + " -c core.hooksPath=/dev/null push origin HEAD:refs/heads/" + testBranch + "\n"
						if err := os.WriteFile(filepath.Join(f.repo.Dir, ".git", "hooks", "pre-push"), []byte(hook), 0700); err != nil {
							t.Fatal(err)
						}
					case "identical remote tip":
						f.repo.GitIn(f.worktreeDir(), "push", "origin", "HEAD:refs/heads/"+testBranch)
					case "generic rejection":
						if err := os.WriteFile(filepath.Join(f.repo.Origin, "hooks", "pre-receive"), []byte("#!/bin/sh\necho 'administrative rejection' >&2\nexit 1\n"), 0700); err != nil {
							t.Fatal(err)
						}
					}
				}
				return succeeded(req, f.paths.Home()), nil
			}))
			if r := executePublicationTurn(f); !r.Succeeded() {
				t.Fatalf("setup: %+v", r)
			}
			f.repo.GitIn(f.worktreeDir(), "push", "origin", "HEAD:refs/heads/"+testBranch)
			remoteBefore := strings.Fields(f.repo.Git("ls-remote", "origin", "refs/heads/"+testBranch))[0]
			active = true
			r := executePublicationTurn(f)
			local := strings.TrimSpace(f.repo.GitIn(f.worktreeDir(), "rev-parse", "HEAD"))
			remote := strings.Fields(f.repo.Git("ls-remote", "origin", "refs/heads/"+testBranch))[0]
			if local != verified {
				t.Fatalf("verified tree changed after runner: %s -> %s", verified, local)
			}
			if scenario == "identical remote tip" || scenario == "identical during push" {
				if !r.Succeeded() || !r.Pushed || remote != verified {
					t.Fatalf("identical completion: %+v", r)
				}
			} else {
				if r.Succeeded() || r.Pushed || r.Err == nil {
					t.Fatalf("rejection misreported: %+v", r)
				}
				if !strings.Contains(r.Publication, verified) {
					t.Fatalf("missing retained commit: %s", r.Publication)
				}
				if scenario == "different remote tip" || scenario == "movement during push" {
					if remote != operatorHead || !strings.Contains(r.Publication, "changed") || !strings.Contains(r.Publication, "newly verified run") {
						t.Fatalf("lost operator work or recovery: %s remote=%s", r.Publication, remote)
					}
				} else if remote != remoteBefore || strings.Contains(r.Publication, "tip changed") {
					t.Fatalf("generic rejection mislabeled: %s", r.Publication)
				}
			}
		})
	}
}

// Each execution represents a new owner comment, preserving idempotency of
// repeated delivery of the same comment in the production path.
func executePublicationTurn(f *fixture) Result {
	f.opts.AutoSyncBase = true
	req := f.request()
	req.Trigger.CommentID += int64(len(f.runs()))
	return f.execute(req)
}

func TestTaskPublicationReportInput(t *testing.T) {
	t.Parallel()
	result := Result{Disposition: DispositionRan, Publication: "Local commit abc123 retained; publication not confirmed."}
	result.Run.Ordinal = 2
	in, err := (&Reporter{}).runInput(t.Context(), result)
	if err != nil {
		t.Fatal(err)
	}
	if in.Publication != result.Publication {
		t.Fatalf("publication lost between result and report: %q", in.Publication)
	}
}

// The push is verified once, inside the turn that made it. A trigger that is
// replayed — the run exists, the report did not follow — is rebuilt from the
// runs row, so publication has to survive in the row: a commit confirmed on
// the remote must not come back as "in the local worktree only".
func TestTaskPublicationSurvivesAReplayedTrigger(t *testing.T) {
	t.Parallel()
	var f *fixture
	f = newFixture(t, newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		writeInWorktree(t, req, "published.txt", "work to publish\n")
		return succeeded(req, f.paths.Home()), nil
	}))
	forge := newFakeForge(t)
	reporter := newReporter(t, f, forge, nil)
	f.opts.AutoSyncBase = true

	req := f.request()
	first := f.execute(req)
	if !first.Succeeded() || !first.Pushed || first.CommitSHA == "" {
		t.Fatalf("setup did not publish: %+v", first)
	}
	remote := strings.Fields(f.repo.Git("ls-remote", "origin", "refs/heads/"+testBranch))[0]
	if local := strings.TrimSpace(f.repo.GitIn(f.worktreeDir(), "rev-parse", "HEAD")); local != remote {
		t.Fatalf("setup: local=%s remote=%s", local, remote)
	}

	replay := f.execute(req)
	if replay.Disposition != DispositionAlreadyRan {
		t.Fatalf("replay disposition = %v (%s)", replay.Disposition, replay.Reason)
	}
	if !replay.Pushed || replay.Publication != first.Publication {
		t.Fatalf("replay lost publication: pushed=%v publication=%q, want true and %q",
			replay.Pushed, replay.Publication, first.Publication)
	}

	if _, err := reporter.ReportTurn(t.Context(), replay); err != nil {
		t.Fatal(err)
	}
	body := forge.only(t).body
	if strings.Contains(body, "not pushed") || !strings.Contains(body, "(pushed)") {
		t.Fatalf("verified push reported as unpublished: %s", body)
	}
}
