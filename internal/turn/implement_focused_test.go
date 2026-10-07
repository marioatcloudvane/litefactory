package turn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The Implement merge step runs no recipe phase at all, so it can never run the
// full one: the full suite belongs to Validate. This guards the sources of the
// Implement activities against reading the check recipe or running its commands.
func TestImplementSourcesNeverRunTheCheckRecipe(t *testing.T) {
	files, err := filepath.Glob("implement_*.go")
	mustProgress(t, err)
	if len(files) == 0 {
		t.Fatal("no Implement sources found")
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(file)
		mustProgress(t, err)
		for _, banned := range []string{"internal/bugchecks", "ParseBugCheckRecipe", "ReadBugCheckRecipe", "BugCheckPhase", ".Full", "ValidateSuite"} {
			if strings.Contains(string(raw), banned) {
				t.Errorf("%s mentions %q: Implement must run no recipe phase, and the full suite is only Validate's", file, banned)
			}
		}
	}
}
