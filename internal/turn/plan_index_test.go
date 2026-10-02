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

// replacePlanIndex opens the session's plan_brief.md and replaces its Story
// Index the way the graph Plan save step does.
func replacePlanIndex(t *testing.T, worktree, index string) error {
	t.Helper()
	root, err := os.OpenRoot(worktree)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	target, err := openPlanIndex(root, "session")
	if err != nil {
		return err
	}
	defer target.dir.Close()
	return target.replace(root, []byte(index))
}

// TestPlanIndexAppendsAMissingStoryIndex is #429: a plan_brief.md rewritten by
// an agent without its "## Story Index" heading used to fail every save with
// "plan brief Story Index missing". The save step now appends one section and
// keeps the agent's text, and a later save replaces that section in place.
func TestPlanIndexAppendsAMissingStoryIndex(t *testing.T) {
	const index = "## Story Index\n\n| ID | Title | Sub-issue |\n| --- | --- | --- |\n| story-001 | One | [#400](https://github.com/acme/acme-api/issues/400) |\n| story-002 | Two | [#401](https://github.com/acme/acme-api/issues/401) |\n"
	for name, tc := range map[string]struct{ original, want string }{
		"prose without newline": {"Plan accepted for story-005.", "Plan accepted for story-005.\n\n" + index + "\n"},
		"prose with newline":    {"Plan accepted for story-005.\n", "Plan accepted for story-005.\n\n" + index + "\n"},
		"prose with blank line": {"# Plan\n\nNotes.\n\n", "# Plan\n\nNotes.\n\n" + index + "\n"},
		"other headings only":   {"# Plan\n\n## Notes\nKeep.\n", "# Plan\n\n## Notes\nKeep.\n\n" + index + "\n"},
		"empty file":            {"", index + "\n"},
	} {
		t.Run(name, func(t *testing.T) {
			worktree := t.TempDir()
			if err := os.MkdirAll(filepath.Join(worktree, "session"), 0o700); err != nil {
				t.Fatal(err)
			}
			plan := filepath.Join(worktree, "session", "plan_brief.md")
			if err := os.WriteFile(plan, []byte(tc.original), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := replacePlanIndex(t, worktree, index); err != nil {
				t.Fatalf("save over a headingless plan_brief.md: %v", err)
			}
			data, err := os.ReadFile(plan)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != tc.want {
				t.Fatalf("plan_brief.md =\n%q\nwant\n%q", data, tc.want)
			}
			// A second save finds the appended section and replaces it in place.
			if err := replacePlanIndex(t, worktree, index); err != nil {
				t.Fatalf("second save: %v", err)
			}
			again, err := os.ReadFile(plan)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Count(string(again), "## Story Index"); got != 1 {
				t.Fatalf("plan_brief.md has %d Story Index sections, want 1:\n%s", got, again)
			}
			if !strings.Contains(string(again), "story-001") || !strings.Contains(string(again), "story-002") {
				t.Fatalf("second save lost stories:\n%s", again)
			}
		})
	}
}

// TestPlanIndexExistingSectionIsReplacedByteForByte pins the pre-#429 output
// for a plan_brief.md that already has its Story Index section.
func TestPlanIndexExistingSectionIsReplacedByteForByte(t *testing.T) {
	for name, tc := range map[string]struct{ original, want string }{
		"section before notes": {"# Plan\n\n## Story Index\nold\n\n## Notes\nKeep.\n", "# Plan\n\n## Story Index\nnew\n## Notes\nKeep.\n"},
		"section last":         {"# Plan\n\n## Story Index\nold\n", "# Plan\n\n## Story Index\nnew\n"},
		"skeleton":             {"# Plan\n\n## Story Index\n\n| ID | Title | Sub-issue |\n| --- | --- | --- |\n", "# Plan\n\n## Story Index\nnew\n"},
	} {
		t.Run(name, func(t *testing.T) {
			worktree := t.TempDir()
			if err := os.MkdirAll(filepath.Join(worktree, "session"), 0o700); err != nil {
				t.Fatal(err)
			}
			plan := filepath.Join(worktree, "session", "plan_brief.md")
			if err := os.WriteFile(plan, []byte(tc.original), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := replacePlanIndex(t, worktree, "## Story Index\nnew"); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(plan)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != tc.want {
				t.Fatalf("plan_brief.md =\n%q\nwant byte-identical\n%q", data, tc.want)
			}
		})
	}
}

// TestPlanIndexWithoutStoryIndexKeepsRefusingSymlinksAndConcurrentChanges is
// the #429 guard that appending a missing section never weakens the symlink,
// identity and concurrent-change checks.
func TestPlanIndexWithoutStoryIndexKeepsRefusingSymlinksAndConcurrentChanges(t *testing.T) {
	for _, kind := range []string{"file symlink", "ancestor symlink", "file replacement", "ancestor replacement", "content edit", "normal"} {
		t.Run(kind, func(t *testing.T) {
			worktree := t.TempDir()
			outside := t.TempDir()
			const original = "Plan accepted; the agent rewrote this file without headings.\n"
			const sentinel = "outside must survive"
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			must(os.MkdirAll(filepath.Join(worktree, "session"), 0o700))
			plan := filepath.Join(worktree, "session", "plan_brief.md")
			must(os.WriteFile(plan, []byte(original), 0o600))
			external := filepath.Join(outside, "plan_brief.md")
			must(os.WriteFile(external, []byte(sentinel), 0o600))
			switch kind {
			case "file symlink":
				must(os.Remove(plan))
				must(os.Symlink(external, plan))
			case "ancestor symlink":
				must(os.Rename(filepath.Join(worktree, "session"), filepath.Join(worktree, "saved")))
				must(os.Symlink(outside, filepath.Join(worktree, "session")))
			}
			root, err := os.OpenRoot(worktree)
			must(err)
			defer root.Close()
			index, err := openPlanIndex(root, "session")
			if strings.HasSuffix(kind, "symlink") {
				if err == nil {
					index.dir.Close()
					t.Fatal("symlink accepted before publication")
				}
				if !strings.Contains(err.Error(), "symlink") {
					t.Fatalf("symlink refused for the wrong reason: %v", err)
				}
			} else {
				if err != nil {
					t.Fatalf("headingless plan_brief.md refused: %v", err)
				}
				defer index.dir.Close()
				want := ""
				switch kind {
				case "file replacement":
					must(os.Rename(plan, plan+".old"))
					must(os.WriteFile(plan, []byte(original), 0o600))
					want = "plan index destination changed during publication"
				case "ancestor replacement":
					must(os.Rename(filepath.Join(worktree, "session"), filepath.Join(worktree, "saved")))
					must(os.Symlink(outside, filepath.Join(worktree, "session")))
					want = "symlink"
				case "content edit":
					must(os.WriteFile(plan, []byte(original+"newer edit"), 0o600))
					want = "plan index contents changed during publication"
				}
				err = index.replace(root, []byte("## Story Index\nnew"))
				if kind == "normal" {
					must(err)
					data, err := os.ReadFile(plan)
					must(err)
					if string(data) != original+"\n## Story Index\nnew\n" {
						t.Fatalf("incorrect append: %q", data)
					}
				} else {
					if err == nil || !strings.Contains(err.Error(), want) {
						t.Fatalf("changed destination: err = %v, want %q", err, want)
					}
					if kind == "content edit" {
						data, readErr := os.ReadFile(plan)
						must(readErr)
						if string(data) != original+"newer edit" {
							t.Fatalf("newer edit overwritten: %q", data)
						}
					}
				}
			}
			data, err := os.ReadFile(external)
			must(err)
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
