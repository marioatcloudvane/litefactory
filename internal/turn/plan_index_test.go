package turn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPlanIndexRefusesSymlinksAndConcurrentReplacement(t *testing.T) {
	for _, kind := range []string{"file symlink", "ancestor symlink", "file replacement", "ancestor replacement", "content edit", "normal"} {
		t.Run(kind, func(t *testing.T) {
			worktree := t.TempDir()
			outside := t.TempDir()
			const original = "# Plan\n\n## Story Index\nold\n\n## Notes\nKeep.\n"
			const sentinel = "outside must survive"
			write := func(path, body string) {
				t.Helper()
				if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			mkdir := func(path string) {
				t.Helper()
				if err := os.MkdirAll(path, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			symlink := func(target, path string) {
				t.Helper()
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			}
			rename := func(from, to string) {
				t.Helper()
				if err := os.Rename(from, to); err != nil {
					t.Fatal(err)
				}
			}
			mkdir(filepath.Join(worktree, "session"))
			plan := filepath.Join(worktree, "session", "plan_brief.md")
			write(plan, original)
			external := filepath.Join(outside, "plan_brief.md")
			write(external, sentinel)
			if kind == "file symlink" {
				if err := os.Remove(plan); err != nil {
					t.Fatal(err)
				}
				symlink(external, plan)
			}
			if kind == "ancestor symlink" {
				rename(filepath.Join(worktree, "session"), filepath.Join(worktree, "saved"))
				symlink(outside, filepath.Join(worktree, "session"))
			}
			root, err := os.OpenRoot(worktree)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			index, err := openPlanIndex(root, "session")
			if strings.HasSuffix(kind, "symlink") {
				if err == nil {
					index.dir.Close()
					t.Fatal("symlink accepted before publication")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				defer index.dir.Close()
				switch kind {
				case "file replacement":
					rename(plan, plan+".old")
					write(plan, original)
				case "ancestor replacement":
					rename(filepath.Join(worktree, "session"), filepath.Join(worktree, "saved"))
					symlink(outside, filepath.Join(worktree, "session"))
				case "content edit":
					write(plan, original+"newer edit")
				}
				err = index.replace(root, []byte("## Story Index\nnew"))
				if kind == "normal" {
					if err != nil {
						t.Fatal(err)
					}
					data, err := os.ReadFile(plan)
					if err != nil {
						t.Fatal(err)
					}
					if !strings.Contains(string(data), "\nnew\n") || !strings.Contains(string(data), "## Notes\nKeep.") {
						t.Fatalf("incorrect rewrite: %s", data)
					}
				} else if err == nil {
					t.Fatal("changed destination accepted")
				}
			}
			data, err := os.ReadFile(external)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != sentinel {
				t.Fatal("external file changed")
			}
		})
	}
}

func TestPlanSessionLookupRefusesSymlinkComponents(t *testing.T) {
	for _, component := range []string{"marker", "session", "branch"} {
		t.Run(component, func(t *testing.T) {
			root := t.TempDir()
			relative := writeSession(t, root, "2026-09-01", "plan_brief.md")
			target := filepath.Join(root, filepath.FromSlash(relative))
			switch component {
			case "marker":
				target = filepath.Join(target, "plan_brief.md")
			case "branch":
				target = filepath.Dir(filepath.Dir(target))
			}
			backup := filepath.Join(t.TempDir(), "original")
			if err := os.Rename(target, backup); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(backup, target); err != nil {
				t.Fatal(err)
			}
			for _, day := range []int{1, 2} {
				if dir, ok := findSessionDir(root, testSessionBranch, testSessionIssue, time.Date(2026, 9, day, 0, 0, 0, 0, time.UTC), "plan_brief.md", nil); ok {
					t.Fatalf("followed %s symlink to %s", component, dir)
				}
			}
		})
	}
}
