package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/gittest"
)

// AC-003/008: scratch origins exercise Git's actual atomic ref guards. The
// second update happens after the production observation, at the mutation.
func TestRetirementExactHeadRaces(t *testing.T) {
	for _, kind := range []string{"local_branch", "remote_branch"} {
		t.Run(kind, func(t *testing.T) {
			r := gittest.New(t)
			branch := "lf/213-retire"
			old := r.Head()
			r.Git("branch", branch)
			r.Git("push", "origin", branch)
			r.WriteFile("later.txt", "later base\n")
			advanced := r.Commit("later base")
			r.Git("push", "origin", "main")
			b, err := Resolve(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("scratch probe: %s; %s expected %s rejects concurrent %s", b.Version(), kind, old, advanced)
			c, err := b.Open(t.Context(), r.Dir)
			if err != nil {
				t.Fatal(err)
			}
			observation, err := ObserveRetirement(t.Context(), c, "", branch, "main")
			if err != nil {
				t.Fatal(err)
			}
			quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
			if kind == "local_branch" {
				wrapper := filepath.Join(t.TempDir(), "git-wrapper")
				script := "#!/bin/sh\nif [ \"$1\" = update-ref ]; then\n" + quote(b.Path()) + " -C " + quote(r.Dir) + " update-ref refs/heads/" + branch + " " + advanced + "\nfi\nexec " + quote(b.Path()) + " \"$@\"\n"
				if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
					t.Fatal(err)
				}
				c.runner.binary = wrapper
			} else {
				// pre-push runs after advertisement: receive-pack itself must reject the
				// stale old value, even though the advertised value matched the lease.
				hook := filepath.Join(r.Dir, ".git", "hooks", "pre-push")
				script := "#!/bin/sh\n" + quote(b.Path()) + " --git-dir=" + quote(r.Origin) + " update-ref refs/heads/" + branch + " " + advanced + "\n"
				if err := os.WriteFile(hook, []byte(script), 0700); err != nil {
					t.Fatal(err)
				}
			}
			r.Dirty()
			gittest.AssertUnchanged(t, r.Dir, func() {
				err = GuardedRetirement(t.Context(), c, observation, "main", kind)
			})
			if err == nil {
				t.Fatal("deleted concurrently advanced ref")
			}
			t.Logf("actual rejection: %v", err)
			dir := r.Dir
			if kind == "remote_branch" {
				dir = r.Origin
			}
			if got := r.GitIn(dir, "rev-parse", "refs/heads/"+branch); got != advanced {
				t.Fatalf("tip %s, want preserved %s", got, advanced)
			}
		})
	}
}

func TestRetirementGuardRevalidatesSafety(t *testing.T) {
	for _, scenario := range []string{"dirty", "locked", "detached", "advanced", "protected_base"} {
		t.Run(scenario, func(t *testing.T) {
			r := gittest.New(t)
			branch := "lf/213-retire"
			path := filepath.Join(filepath.Dir(r.Dir), "tasks", "task-213")
			r.Git("worktree", "add", "-b", branch, path, "main")
			r.Git("push", "origin", branch)
			b, err := Resolve(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			c, err := b.Open(t.Context(), r.Dir)
			if err != nil {
				t.Fatal(err)
			}
			previous, err := ObserveRetirement(t.Context(), c, path, branch, "main")
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "dirty":
				if err := os.WriteFile(filepath.Join(path, "only-copy.txt"), []byte("valuable"), 0600); err != nil {
					t.Fatal(err)
				}
			case "locked":
				r.Git("worktree", "lock", path)
			case "detached":
				r.GitIn(path, "checkout", "--detach")
			case "advanced":
				if err := os.WriteFile(filepath.Join(path, "only-copy.txt"), []byte("valuable"), 0600); err != nil {
					t.Fatal(err)
				}
				r.GitIn(path, "add", ".")
				r.GitIn(path, "commit", "-m", "local only")
			case "protected_base":
				previous.Branch = "main"
			}
			r.Dirty()
			for _, kind := range []string{"worktree", "local_branch", "remote_branch"} {
				gittest.AssertUnchanged(t, r.Dir, func() { err = GuardedRetirement(t.Context(), c, previous, "main", kind) })
				if err == nil {
					t.Fatalf("%s allowed %s deletion", scenario, kind)
				}
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("protected worktree lost %v", err)
			}
			if r.Git("ls-remote", "origin", "refs/heads/"+branch) == "" {
				t.Fatal("protected remote lost")
			}
		})
	}
}
