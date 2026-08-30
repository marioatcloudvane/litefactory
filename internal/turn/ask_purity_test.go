package turn

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// TestNoAskRoundLimitAnywhereInInternal is AC-A1.14, in the spirit of
// internal/config/purity_test.go's TestForbiddenFlagIsAbsentFromTheTree: a
// design decision stated as a prose rule in a plan does not survive the day
// someone adds "just a small cap" to stop a run asking forever, so this fails
// the build instead.
//
// Q8 put the judgement of when a run has asked enough at agent level — the
// orchestrator decides, and it must honour a direct instruction to move on —
// and a Go-side counter, cap or timeout on asking rounds is a *failing*
// outcome of this story, not a missing feature.
// internal/harness.TestNothingCountsAskingRounds already pins the weaker claim
// that the prompt text itself names no such bound. This test pins the
// stronger one: that no production identifier anywhere under internal/ pairs
// the idea of asking with the idea of a bound at all, so the mechanism cannot
// exist in code that never renders it into a prompt.
//
// It is a name search, not a semantic one, and deliberately coarse: an
// identifier that pairs "ask"/"asking" with "round(s)", "cap", "counter",
// "limit", "timeout", "budget" or "quota", in either order, fails this test.
// Scanning is restricted to non-test files. A counter that gates behaviour has
// to live in code the daemon actually runs, so this is the set that matters;
// it also happens to be what keeps this test from tripping over its own
// sibling in internal/harness, whose name is obliged to describe the exact
// shape of thing it proves does not exist.
func TestNoAskRoundLimitAnywhereInInternal(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not resolve this file's own path")
	}
	// internal/turn/ask_purity_test.go -> internal/
	root := filepath.Dir(filepath.Dir(thisFile))
	if filepath.Base(root) != "internal" {
		t.Fatalf("resolved root %q, want a directory named internal", root)
	}

	pattern := regexp.MustCompile(
		`(?i)ask(ing)?(round|rounds|cap|counter|limit|timeout|budget|quota)` +
			`|(round|rounds|cap|counter|limit|timeout|budget|quota)(ing)?ask`)

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if loc := pattern.FindStringIndex(string(contents)); loc != nil {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				rel = path
			}
			t.Errorf("%s pairs asking with a bound near byte %d; Q8 puts that judgement at "+
				"agent level and a Go implementation of it is a failing outcome (AC-A1.14)",
				rel, loc[0])
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
}
