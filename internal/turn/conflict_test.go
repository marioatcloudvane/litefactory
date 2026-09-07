package turn

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

// fakeConflictBlobReader is a narrow test double for [conflictBlobReader]: no
// git, no worktree, just canned content per path.
type fakeConflictBlobReader struct {
	content map[string]string
	err     map[string]error
}

func (f fakeConflictBlobReader) ConflictBlob(_ context.Context, _ string, path string) (string, error) {
	if err, ok := f.err[path]; ok {
		return "", err
	}
	return f.content[path], nil
}

func TestCollectConflictContentCapsAndExcludes(t *testing.T) {
	t.Parallel()

	reader := fakeConflictBlobReader{content: map[string]string{
		// Ordinary, small conflict: kept.
		"a.go": "package a\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> base\n",
		// Binary content (a NUL byte): excluded whole.
		"binary.dat": "\x00\x01<<<<<<<\n\x00>>>>>>>",
		// Not valid UTF-8: excluded whole.
		"invalid.txt": "<<<<<<<\n\xff\xfe\n>>>>>>>",
		// No conflict markers at all (e.g. a rename/mode conflict
		// merge-tree cannot represent with markers): excluded whole.
		"nomarkers.go": "package nomarkers\nordinary content, no conflict here\n",
		// Present in the conflict list but unreadable: excluded whole.
		"missing.go": "",
	}, err: map[string]error{
		"missing.go": errors.New("no such blob"),
	}}

	conflicts := []string{"a.go", "binary.dat", "invalid.txt", "nomarkers.go", "missing.go"}
	files, excluded := collectConflictContent(t.Context(), reader, "tree-sha", conflicts, 1<<20)

	if len(files) != 1 || files[0].Path != "a.go" {
		t.Fatalf("files = %+v, want exactly a.go", files)
	}
	if got := files[0].Content; got == "" {
		t.Error("a.go content is empty, want the extracted conflict hunk")
	}
	wantExcluded := []string{"binary.dat", "invalid.txt", "nomarkers.go", "missing.go"}
	if len(excluded) != len(wantExcluded) {
		t.Fatalf("excluded = %v, want %v", excluded, wantExcluded)
	}
	for i, path := range wantExcluded {
		if excluded[i] != path {
			t.Errorf("excluded[%d] = %q, want %q", i, excluded[i], path)
		}
	}
}

// TestCollectConflictContentExcludesWholeOverPerFileBudget is AC-11.5's
// per-file cap: a conflicting file whose extracted hunk alone exceeds
// [conflictFilePerFileMaxBytes] is excluded whole, never truncated.
func TestCollectConflictContentExcludesWholeOverPerFileBudget(t *testing.T) {
	t.Parallel()

	huge := make([]byte, conflictFilePerFileMaxBytes+1)
	for i := range huge {
		huge[i] = 'x'
	}
	blob := "<<<<<<< HEAD\n" + string(huge) + "\n=======\ntheirs\n>>>>>>> base\n"
	reader := fakeConflictBlobReader{content: map[string]string{"big.go": blob}}

	files, excluded := collectConflictContent(t.Context(), reader, "tree-sha", []string{"big.go"}, 1<<20)
	if len(files) != 0 || len(excluded) != 1 || excluded[0] != "big.go" {
		t.Fatalf("files=%v excluded=%v, want big.go excluded whole and no file kept", files, excluded)
	}
}

// TestCollectConflictContentExcludesWholeOverTotalBudget is AC-11.5's total
// cap: once the running total would exceed totalMaxBytes, further files are
// excluded whole rather than truncated, and earlier ones already counted
// are kept exactly as collected.
func TestCollectConflictContentExcludesWholeOverTotalBudget(t *testing.T) {
	t.Parallel()

	one := "<<<<<<< HEAD\none\n=======\nONE\n>>>>>>> base\n"
	two := "<<<<<<< HEAD\ntwo\n=======\nTWO\n>>>>>>> base\n"
	reader := fakeConflictBlobReader{content: map[string]string{"a.go": one, "b.go": two}}

	// A total budget wide enough for the first file's capped content but not
	// for both.
	total := len(extractedContent(one))
	files, excluded := collectConflictContent(t.Context(), reader, "tree-sha", []string{"a.go", "b.go"}, total)

	if len(files) != 1 || files[0].Path != "a.go" {
		t.Fatalf("files = %+v, want exactly a.go kept", files)
	}
	if len(excluded) != 1 || excluded[0] != "b.go" {
		t.Fatalf("excluded = %v, want exactly b.go excluded over the total budget", excluded)
	}
}

// extractedContent mirrors what capturedConflictContent computes for a blob
// with exactly one hunk, so the total-budget test above can size its budget
// against the real capped length rather than a guessed constant.
func extractedContent(blob string) string {
	hunks := extractConflictHunks(blob)
	joined := ""
	for i, h := range hunks {
		if i > 0 {
			joined += "\n...\n"
		}
		joined += h
	}
	return joined
}

func TestExtractConflictHunksFindsMultipleRegions(t *testing.T) {
	t.Parallel()

	blob := "before\n<<<<<<< HEAD\nours1\n=======\ntheirs1\n>>>>>>> base\nmiddle\n" +
		"<<<<<<< HEAD\nours2\n=======\ntheirs2\n>>>>>>> base\nafter\n"
	hunks := extractConflictHunks(blob)
	if len(hunks) != 2 {
		t.Fatalf("extractConflictHunks found %d hunks, want 2:\n%v", len(hunks), hunks)
	}
	for _, h := range hunks {
		if !containsAllMarkers(h) {
			t.Errorf("hunk missing a marker: %q", h)
		}
	}
}

func containsAllMarkers(s string) bool {
	for _, m := range conflictMarkers {
		if !contains(s, m) {
			return false
		}
	}
	return true
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestValidateConflictResolutionAcceptsAMatchingResponse(t *testing.T) {
	t.Parallel()

	resolution := agent.ConflictResolution{Files: []agent.ConflictResolutionFile{
		{Path: "a.go", Resolution: "package a\n"},
		{Path: "b.go", Resolution: "package b\n"},
	}}
	if err := validateConflictResolution(resolution, []string{"a.go", "b.go"}); err != nil {
		t.Fatalf("validateConflictResolution: %v", err)
	}
}

func TestValidateConflictResolutionRejectsAnOutOfScopePath(t *testing.T) {
	t.Parallel()

	resolution := agent.ConflictResolution{Files: []agent.ConflictResolutionFile{
		{Path: "not-conflicting.go", Resolution: "package x\n"},
	}}
	if err := validateConflictResolution(resolution, []string{"a.go"}); err == nil {
		t.Fatal("validateConflictResolution accepted a path outside the conflicting set")
	}
}

func TestValidateConflictResolutionRejectsADuplicatePath(t *testing.T) {
	t.Parallel()

	resolution := agent.ConflictResolution{Files: []agent.ConflictResolutionFile{
		{Path: "a.go", Resolution: "one"},
		{Path: "a.go", Resolution: "two"},
	}}
	if err := validateConflictResolution(resolution, []string{"a.go"}); err == nil {
		t.Fatal("validateConflictResolution accepted the same path twice")
	}
}

func TestValidateConflictResolutionRejectsAConflictMarker(t *testing.T) {
	t.Parallel()

	for _, marker := range conflictMarkers {
		resolution := agent.ConflictResolution{Files: []agent.ConflictResolutionFile{
			{Path: "a.go", Resolution: "package a\n" + marker + "\n"},
		}}
		if err := validateConflictResolution(resolution, []string{"a.go"}); err == nil {
			t.Errorf("validateConflictResolution accepted a resolution carrying %q", marker)
		}
	}
}

func TestConflictAdvisorForNarrowsARegisteredRunner(t *testing.T) {
	t.Parallel()

	kind := agent.Kind("claude")
	advisorRunner := &stubConflictRunner{stubRunner: newStubRunner(nil)}
	runners := RunnerSet{kind: advisorRunner}

	advisor, ok := conflictAdvisorFor(kind, runners)
	if !ok || advisor == nil {
		t.Fatal("conflictAdvisorFor did not narrow a runner that implements ConflictAdvisor")
	}

	if _, ok := conflictAdvisorFor("unregistered", runners); ok {
		t.Error("conflictAdvisorFor found an advisor for an unregistered kind")
	}

	plain := RunnerSet{kind: newStubRunner(nil)}
	if _, ok := conflictAdvisorFor(kind, plain); ok {
		t.Error("conflictAdvisorFor narrowed a Runner that does not implement ConflictAdvisor")
	}
}
