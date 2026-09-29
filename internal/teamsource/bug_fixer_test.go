package teamsource

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Story-003 AC-010 / story-004 AC-009: both copies of bug-fixer.md describe
// the two optional self-check tools, state that a self-check pass is not a
// final pass, keep the no-arbitrary-commands rule, and describe granted-attempt
// owner guidance as never widening the approved remedy or its exclusions. Both
// copies -- the internal capture source and the installed teams/source
// manifest -- stay byte-identical (docs/story-contract-instruction-manifest.md).
func TestBugFixerMarkdown_SelfCheckAndOwnerGuidanceContent(t *testing.T) {
	t.Parallel()
	installed, err := os.ReadFile("bug/bug-fixer.md")
	if err != nil {
		t.Fatal(err)
	}
	shared, err := os.ReadFile(filepath.Join(repoRoot, "teams/source/bug/bug-fixer.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(installed) != string(shared) {
		t.Fatalf("internal/teamsource/bug/bug-fixer.md and teams/source/bug/bug-fixer.md differ")
	}
	body := string(installed)
	for _, want := range []string{
		"bug_self_check_start", "bug_self_check_status",
		"optional tools",
		"A self-check pass is not a final pass",
		"Do not execute arbitrary commands, choose check recipes",
		"Owner guidance for this attempt",
		"never widens the approved remedy or its exclusions",
		"If the sandbox is unavailable, finish and report your work the normal way",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("bug-fixer.md lacks required content %q", want)
		}
	}
}

// The captured node ships the same body (through the embedded FS the daemon
// actually installs), and the bug-fixer role's frontmatter is unchanged by
// this content addition.
func TestCaptureBugFixer_ShipsSelfCheckContent(t *testing.T) {
	t.Parallel()
	node, err := CaptureBugFixer(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if node.Role.Frontmatter.Name != "bug-fixer" {
		t.Fatalf("unexpected role name: %q", node.Role.Frontmatter.Name)
	}
	for _, want := range []string{"bug_self_check_start", "bug_self_check_status", "A self-check pass is not a final pass", "Owner guidance for this attempt"} {
		if !strings.Contains(node.Role.Body, want) {
			t.Errorf("captured bug-fixer role lacks %q", want)
		}
	}
}
