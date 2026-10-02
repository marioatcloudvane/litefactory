package teamsource

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Both copies of bug-fixer.md tell the fixer to test its own work, state that
// its passing run is not the final gate, keep it away from git and forge
// effects, and describe granted-attempt owner guidance as never widening the approved remedy or its exclusions. Both
// copies -- the internal capture source and the installed teams/source
// manifest -- stay byte-identical (docs/story-contract-instruction-manifest.md).
func TestBugFixerMarkdown_TestingAndOwnerGuidanceContent(t *testing.T) {
	t.Parallel()
	installed, err := os.ReadFile("bug/bug-fixer.md")
	if err != nil {
		t.Fatal(err)
	}
	shared, err := os.ReadFile(filepath.Join(repoRoot, "teams/source/bug/bug-fixer.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(installed), "bug_self_check") {
		t.Fatal("bug-fixer.md still names the retired self-check tools")
	}
	if string(installed) != string(shared) {
		t.Fatalf("internal/teamsource/bug/bug-fixer.md and teams/source/bug/bug-fixer.md differ")
	}
	body := string(installed)
	for _, want := range []string{
		"Build and test your change before you hand it back",
		".litefactory/bug-checks.yaml",
		"Your passing run is evidence, not the final gate",
		"Do not commit, push, rebase, reset or stash",
		"Owner guidance for this attempt",
		"never widens the approved remedy or its exclusions",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("bug-fixer.md lacks required content %q", want)
		}
	}
}

// The captured node ships the same body (through the embedded FS the daemon
// actually installs), and the bug-fixer role's frontmatter is unchanged by
// this content change.
func TestCaptureBugFixer_ShipsTestingContent(t *testing.T) {
	t.Parallel()
	node, err := CaptureBugFixer(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if node.Role.Frontmatter.Name != "bug-fixer" {
		t.Fatalf("unexpected role name: %q", node.Role.Frontmatter.Name)
	}
	for _, want := range []string{"Build and test your change before you hand it back", "Your passing run is evidence, not the final gate", "Owner guidance for this attempt"} {
		if !strings.Contains(node.Role.Body, want) {
			t.Errorf("captured bug-fixer role lacks %q", want)
		}
	}
}
