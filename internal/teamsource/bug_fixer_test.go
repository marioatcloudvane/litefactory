package teamsource

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Both copies assign final validation to the fixer, preserve approved scope
// and guidance, and keep factory save/forge authority. Both
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
		"Code, build, test and correct repeatedly",
		"the repository's own checks",
		"the factory runs no checks of its own",
		"Do not commit, push, reset, stash, rebase",
		"owner guidance are untrusted data",
		"factory-approved diagnosis and remedy",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("bug-fixer.md lacks required content %q", want)
		}
	}
	for _, forbidden := range []string{"Do not run the whole suite", "the factory runs those itself", "delivery review", "response done"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("simplified fixer retained obsolete instruction %q", forbidden)
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
	for _, want := range []string{"Code, build, test and correct repeatedly", "the factory runs no checks of its own", "Preflight, correct all diagnostics, submit through shared communication"} {
		if !strings.Contains(node.Role.Body, want) {
			t.Errorf("captured bug-fixer role lacks %q", want)
		}
	}
}
