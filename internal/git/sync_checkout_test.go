package git_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/gittest"
)

func TestCheckSyncCheckoutPreservesRetainedState(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"clean", "unpublished", "dirty", "transport", "reserved target", "detached", "wrong branch", "merge"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			repo := gittest.New(t)
			wt, dir := newTaskWorktree(t, repo, 42, "Sync guard")
			target := repo.GitIn(dir, "rev-parse", "HEAD")
			want := ""
			switch kind {
			case "unpublished":
				commitInDir(t, repo, dir, "local.txt", "unpublished work")
				want = "local commits"
			case "dirty":
				if err := os.WriteFile(filepath.Join(dir, "owner.txt"), []byte("retained bytes"), 0o600); err != nil {
					t.Fatal(err)
				}
				want = "untracked"
			case "transport":
				if err := os.WriteFile(filepath.Join(dir, ".litefactory-plan-proposal.json"), []byte("retained bytes"), 0o600); err != nil {
					t.Fatal(err)
				}
				repo.GitIn(dir, "add", ".litefactory-plan-proposal.json")
				want = "reserved transport"
			case "reserved target":
				commitInDir(t, repo, repo.Dir, ".litefactory-plan-proposal.json", "incoming transport")
				target = repo.Git("rev-parse", "HEAD")
				want = "reserved factory transport"
			case "detached":
				repo.GitIn(dir, "checkout", "--detach", "HEAD")
				want = "detached"
			case "wrong branch":
				repo.GitIn(dir, "checkout", "-b", "another")
				want = "does not match"
			case "merge":
				mergePath := repo.GitIn(dir, "rev-parse", "--git-path", "MERGE_HEAD")
				if !filepath.IsAbs(mergePath) {
					mergePath = filepath.Join(dir, mergePath)
				}
				if err := os.WriteFile(mergePath, []byte(target+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				want = "merge in progress"
			}
			head := repo.GitIn(dir, "rev-parse", "HEAD")
			index := repo.GitIn(dir, "ls-files", "--stage")
			reason, err := wt.CheckSyncCheckout(t.Context(), wt.Branch(), strings.TrimSpace(target))
			if err != nil {
				t.Fatal(err)
			}
			if want == "" && reason != "" || want != "" && !strings.Contains(reason, want) {
				t.Fatalf("reason %q, want %q", reason, want)
			}
			if got := repo.GitIn(dir, "rev-parse", "HEAD"); got != head {
				t.Fatal("guard changed HEAD")
			}
			if got := repo.GitIn(dir, "ls-files", "--stage"); got != index {
				t.Fatal("guard changed index")
			}
			if kind == "dirty" || kind == "transport" {
				name := "owner.txt"
				if kind == "transport" {
					name = ".litefactory-plan-proposal.json"
				}
				got, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil || string(got) != "retained bytes" {
					t.Fatalf("retained bytes changed: %q %v", got, err)
				}
			}
		})
	}
}

func TestCheckSyncCheckoutPreservesIgnoredCollision(t *testing.T) {
	t.Parallel()
	for _, shape := range []string{"same path", "incoming file", "incoming directory", "unrelated artifact"} {
		t.Run(shape, func(t *testing.T) {
			t.Parallel()
			repo := gittest.New(t)
			wt, dir := newTaskWorktree(t, repo, 42, "Ignored sync guard")
			local, incoming := "cache", "cache"
			switch shape {
			case "incoming file":
				local = "cache/owner.txt"
			case "incoming directory":
				incoming = "cache/tracked.txt"
			case "unrelated artifact":
				incoming = "base.txt"
			}
			exclude := repo.Git("rev-parse", "--git-path", "info/exclude")
			if !filepath.IsAbs(exclude) {
				exclude = filepath.Join(repo.Dir, exclude)
			}
			if err := os.WriteFile(exclude, []byte("cache\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			owner := filepath.Join(dir, local)
			if err := os.MkdirAll(filepath.Dir(owner), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(owner, []byte("ignored owner bytes\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(repo.Dir, incoming)
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("incoming tracked bytes\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			repo.Git("add", "-f", incoming)
			repo.Git("commit", "-m", "incoming path")
			target := repo.Git("rev-parse", "HEAD")
			before := repo.GitIn(dir, "rev-parse", "HEAD")
			index := repo.GitIn(dir, "ls-files", "--stage")
			reason, err := wt.CheckSyncCheckout(t.Context(), wt.Branch(), target)
			if err != nil {
				t.Fatal(err)
			}
			if shape == "unrelated artifact" {
				if reason != "" {
					t.Fatalf("unrelated ignored bytes deferred: %s", reason)
				}
			} else if !strings.Contains(reason, "ignored retained work") {
				t.Fatalf("collision allowed: %q", reason)
			}
			bytes, err := os.ReadFile(owner)
			if err != nil || string(bytes) != "ignored owner bytes\n" {
				t.Fatalf("ignored work changed: %q %v", bytes, err)
			}
			if repo.GitIn(dir, "rev-parse", "HEAD") != before || repo.GitIn(dir, "ls-files", "--stage") != index {
				t.Fatal("read-only guard changed retained state")
			}
		})
	}
}

func TestCheckSyncCheckoutPreservesHiddenTrackedWork(t *testing.T) {
	t.Parallel()
	for _, flag := range []string{"--assume-unchanged", "--skip-worktree"} {
		t.Run(flag, func(t *testing.T) {
			t.Parallel()
			repo := gittest.New(t)
			wt, dir := newTaskWorktree(t, repo, 42, "Hidden sync guard")
			commitInDir(t, repo, dir, "hidden.txt", "tracked original")
			target := repo.GitIn(dir, "rev-parse", "HEAD")
			repo.GitIn(dir, "update-index", flag, "hidden.txt")
			path := filepath.Join(dir, "hidden.txt")
			if err := os.WriteFile(path, []byte("hidden owner bytes\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			status := repo.GitIn(dir, "status", "--porcelain")
			if status != "" {
				t.Fatalf("fixture did not hide modifications: %q", status)
			}
			index := repo.GitIn(dir, "ls-files", "-v")
			reason, err := wt.CheckSyncCheckout(t.Context(), wt.Branch(), target)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(reason, "unsupported index flags") {
				t.Fatalf("hidden work allowed: %q", reason)
			}
			bytes, err := os.ReadFile(path)
			if err != nil || string(bytes) != "hidden owner bytes\n" {
				t.Fatalf("hidden bytes changed: %q %v", bytes, err)
			}
			if repo.GitIn(dir, "rev-parse", "HEAD") != target || repo.GitIn(dir, "ls-files", "-v") != index {
				t.Fatal("guard changed HEAD or index flags")
			}
		})
	}
}

func TestCheckSyncCheckoutUsesConfiguredPathCaseRules(t *testing.T) {
	t.Parallel()
	for _, ignoreCase := range []string{"true", "false"} {
		t.Run(ignoreCase, func(t *testing.T) {
			t.Parallel()
			repo := gittest.New(t)
			repo.Git("config", "core.ignorecase", ignoreCase)
			wt, dir := newTaskWorktree(t, repo, 42, "Case sync guard")
			exclude := repo.Git("rev-parse", "--git-path", "info/exclude")
			if !filepath.IsAbs(exclude) {
				exclude = filepath.Join(repo.Dir, exclude)
			}
			if err := os.WriteFile(exclude, []byte("cache\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			owner := filepath.Join(dir, "cache")
			if err := os.WriteFile(owner, []byte("ignored owner bytes\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(repo.Dir, "Cache"), []byte("incoming tracked bytes\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			repo.Git("add", "-f", "Cache")
			repo.Git("commit", "-m", "incoming case alias")
			target := repo.Git("rev-parse", "HEAD")
			before := repo.GitIn(dir, "rev-parse", "HEAD")
			index := repo.GitIn(dir, "ls-files", "--stage")
			reason, err := wt.CheckSyncCheckout(t.Context(), wt.Branch(), target)
			if err != nil {
				t.Fatal(err)
			}
			localInfo, statErr := os.Lstat(owner)
			if statErr != nil {
				t.Fatal(statErr)
			}
			incomingInfo, _ := os.Lstat(filepath.Join(dir, "Cache"))
			physicalAlias := incomingInfo != nil && os.SameFile(localInfo, incomingInfo)
			if ignoreCase == "true" || physicalAlias {
				if !strings.Contains(reason, "ignored retained work") {
					t.Fatalf("case alias allowed: %q", reason)
				}
			} else if reason != "" {
				t.Fatalf("distinct case paths deferred: %q", reason)
			}
			bytes, err := os.ReadFile(owner)
			if err != nil || string(bytes) != "ignored owner bytes\n" {
				t.Fatalf("ignored work changed: %q %v", bytes, err)
			}
			if repo.GitIn(dir, "rev-parse", "HEAD") != before || repo.GitIn(dir, "ls-files", "--stage") != index {
				t.Fatal("guard changed HEAD or index")
			}
		})
	}
}

func TestCheckSyncCheckoutPhysicalDirectoryAlias(t *testing.T) {
	t.Parallel()
	for _, incomingDirectory := range []bool{false, true} {
		t.Run(fmt.Sprintf("incomingDirectory=%t", incomingDirectory), func(t *testing.T) {
			t.Parallel()
			repo := gittest.New(t)
			repo.Git("config", "core.ignorecase", "false")
			wt, dir := newTaskWorktree(t, repo, 42, "Physical case sync guard")
			exclude := repo.Git("rev-parse", "--git-path", "info/exclude")
			if !filepath.IsAbs(exclude) {
				exclude = filepath.Join(repo.Dir, exclude)
			}
			if err := os.WriteFile(exclude, []byte("cache\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			local, incoming := "cache/owner.txt", "Cache"
			if incomingDirectory {
				local, incoming = "cache", "Cache/tracked.txt"
			}
			owner := filepath.Join(dir, local)
			if err := os.MkdirAll(filepath.Dir(owner), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(owner, []byte("ignored owner bytes\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			lower, err := os.Lstat(filepath.Join(dir, "cache"))
			if err != nil {
				t.Fatal(err)
			}
			upper, err := os.Lstat(filepath.Join(dir, "Cache"))
			if err != nil || !os.SameFile(lower, upper) {
				t.Skip("filesystem has distinct case paths")
			}
			path := filepath.Join(repo.Dir, incoming)
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("incoming bytes\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			repo.Git("add", "-f", incoming)
			repo.Git("commit", "-m", "incoming physical alias")
			target := repo.Git("rev-parse", "HEAD")
			before := repo.GitIn(dir, "rev-parse", "HEAD")
			index := repo.GitIn(dir, "ls-files", "--stage")
			reason, err := wt.CheckSyncCheckout(t.Context(), wt.Branch(), target)
			if err != nil || !strings.Contains(reason, "ignored retained work") {
				t.Fatalf("physical alias allowed: %q %v", reason, err)
			}
			bytes, err := os.ReadFile(owner)
			if err != nil || string(bytes) != "ignored owner bytes\n" {
				t.Fatalf("owner bytes changed %q %v", bytes, err)
			}
			if repo.GitIn(dir, "rev-parse", "HEAD") != before || repo.GitIn(dir, "ls-files", "--stage") != index {
				t.Fatal("guard changed retained HEAD/index")
			}
		})
	}
}
