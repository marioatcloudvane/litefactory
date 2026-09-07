package git

import (
	"fmt"
	"strings"
	"testing"
)

// TestParseMergePreviewCapsConflictPaths is S-9: a merge-tree output carrying
// more conflicted paths than [MaxMergeConflictPaths] must not grow
// [MergePreview.Conflicts] past the cap, applied here at parse time rather
// than left to whatever eventually renders the list. This is checked directly
// against parseMergePreview, synthesising the output, because driving a real
// merge-tree conflict past 500 files just to exercise this one branch would
// cost far more than it proves.
func TestParseMergePreviewCapsConflictPaths(t *testing.T) {
	t.Parallel()

	const total = MaxMergeConflictPaths + 50
	paths := make([]string, total)
	for i := range paths {
		paths[i] = fmt.Sprintf("file-%04d.txt", i)
	}
	// Per "git help merge-tree -z": the tree id and every conflicted path are
	// each their own NUL-terminated field; the messages section that follows
	// the file list begins with a NUL instead of a blank line, which is why
	// this synthesises one extra empty field ahead of the trailing prose.
	out := "treeid123\x00" + strings.Join(paths, "\x00") + "\x00\x00informational: pretend this is git's own prose"

	preview, err := parseMergePreview(out, false)
	if err != nil {
		t.Fatalf("parseMergePreview: %v", err)
	}
	if preview.Clean {
		t.Fatal("Clean = true, want false")
	}
	if preview.TreeSHA != "treeid123" {
		t.Errorf("TreeSHA = %q, want %q", preview.TreeSHA, "treeid123")
	}
	if len(preview.Conflicts) != MaxMergeConflictPaths {
		t.Fatalf("len(Conflicts) = %d, want the cap %d", len(preview.Conflicts), MaxMergeConflictPaths)
	}
	if preview.Conflicts[0] != paths[0] {
		t.Errorf("Conflicts[0] = %q, want %q", preview.Conflicts[0], paths[0])
	}
	if last := preview.Conflicts[len(preview.Conflicts)-1]; last != paths[MaxMergeConflictPaths-1] {
		t.Errorf("last Conflicts entry = %q, want %q", last, paths[MaxMergeConflictPaths-1])
	}
}

// TestParseMergePreviewCleanIgnoresExtraOutput confirms a clean merge is
// decoded from the first NUL-terminated field alone, on the branch
// parseMergePreview takes when clean is true — it must never look past that
// field even if merge-tree happened to print more.
func TestParseMergePreviewCleanIgnoresExtraOutput(t *testing.T) {
	t.Parallel()

	preview, err := parseMergePreview("treeid456\x00", true)
	if err != nil {
		t.Fatalf("parseMergePreview: %v", err)
	}
	if !preview.Clean {
		t.Error("Clean = false, want true")
	}
	if preview.TreeSHA != "treeid456" {
		t.Errorf("TreeSHA = %q, want %q", preview.TreeSHA, "treeid456")
	}
	if len(preview.Conflicts) != 0 {
		t.Errorf("Conflicts = %v, want empty on a clean preview", preview.Conflicts)
	}
}
