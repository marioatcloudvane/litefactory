package team

import (
	"os"
	"path/filepath"
	"testing"
)

// writeInstalled writes content at root/destPath, creating any parent
// directories status_test.go's fixtures need (a root-file target like
// RootFileClaude has none).
func writeInstalled(t *testing.T, root, destPath string, content []byte) {
	t.Helper()
	full := filepath.Join(root, destPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o777); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(full, content, 0o666); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestClassifyInSync(t *testing.T) {
	root := t.TempDir()
	rendered := []byte("rendered content\n")
	rec := LockRecord{
		AgentKey: "go-architect", Harness: "claude", DestPath: ".claude/agents/go-architect.md",
		SourcePath: "src/go-architect.md", SourceHash: "src-hash", RenderedHash: HashBytes(rendered),
	}
	writeInstalled(t, root, rec.DestPath, rendered)

	status, err := Classify(root, rec, rec.SourceHash, nil, rendered)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if status != StatusInSync {
		t.Errorf("status = %v, want StatusInSync", status)
	}
}

func TestClassifyLocallyModified(t *testing.T) {
	root := t.TempDir()
	rendered := []byte("rendered content\n")
	handEdited := []byte("hand edited content\n")
	rec := LockRecord{
		AgentKey: "go-architect", Harness: "claude", DestPath: ".claude/agents/go-architect.md",
		SourcePath: "src/go-architect.md", SourceHash: "src-hash", RenderedHash: HashBytes(rendered),
	}
	writeInstalled(t, root, rec.DestPath, handEdited)

	// The current source is unchanged since install (freshSourceHash ==
	// rec.SourceHash); the only difference is a hand-edit to the installed
	// file.
	status, err := Classify(root, rec, rec.SourceHash, nil, rendered)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if status != StatusLocallyModified {
		t.Errorf("status = %v, want StatusLocallyModified", status)
	}
}

func TestClassifyUpstreamUpdated(t *testing.T) {
	root := t.TempDir()
	originalRendered := []byte("original rendered content\n")
	newRendered := []byte("new rendered content\n")
	rec := LockRecord{
		AgentKey: "go-architect", Harness: "claude", DestPath: ".claude/agents/go-architect.md",
		SourcePath: "src/go-architect.md", SourceHash: "old-src-hash", RenderedHash: HashBytes(originalRendered),
	}
	// Installed bytes are untouched since install.
	writeInstalled(t, root, rec.DestPath, originalRendered)

	// The current source has moved on (freshSourceHash != rec.SourceHash),
	// producing a different render than what's installed.
	status, err := Classify(root, rec, "new-src-hash", nil, newRendered)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if status != StatusUpstreamUpdated {
		t.Errorf("status = %v, want StatusUpstreamUpdated", status)
	}
}

func TestClassifyConflict(t *testing.T) {
	root := t.TempDir()
	originalRendered := []byte("original rendered content\n")
	newRendered := []byte("new rendered content\n")
	handEdited := []byte("hand edited on top of the original\n")
	rec := LockRecord{
		AgentKey: "go-architect", Harness: "claude", DestPath: ".claude/agents/go-architect.md",
		SourcePath: "src/go-architect.md", SourceHash: "old-src-hash", RenderedHash: HashBytes(originalRendered),
	}
	// Both sides changed: the installed file was hand-edited away from what
	// was originally installed, and the current source has also moved on.
	writeInstalled(t, root, rec.DestPath, handEdited)

	status, err := Classify(root, rec, "new-src-hash", nil, newRendered)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if status != StatusConflict {
		t.Errorf("status = %v, want StatusConflict", status)
	}
}

func TestClassifyMissing(t *testing.T) {
	root := t.TempDir()
	rec := LockRecord{
		AgentKey: "go-architect", Harness: "claude", DestPath: ".claude/agents/go-architect.md",
		SourcePath: "src/go-architect.md", SourceHash: "src-hash", RenderedHash: "rendered-hash",
	}
	// Nothing written at rec.DestPath at all.

	status, err := Classify(root, rec, rec.SourceHash, nil, []byte("anything"))
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if status != StatusMissing {
		t.Errorf("status = %v, want StatusMissing", status)
	}
}

func TestClassifyRootFileInSync(t *testing.T) {
	root := t.TempDir()
	body := []byte("agent body\n")
	teams := []string{"backend"}
	section := buildMarkerSection(teams, "go-architect", body)
	rec := LockRecord{
		AgentKey: "go-architect", Harness: "claude", DestPath: RootFileClaude,
		SourcePath: "src/go-architect.md", SourceHash: "src-hash",
		RenderedHash: markerSpanHash(teams, "go-architect", body), Teams: encodeTeams(teams),
	}
	writeInstalled(t, root, rec.DestPath, section)

	status, err := Classify(root, rec, rec.SourceHash, teams, body)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if status != StatusInSync {
		t.Errorf("status = %v, want StatusInSync", status)
	}
}

func TestClassifyRootFileMissingSection(t *testing.T) {
	root := t.TempDir()
	// CLAUDE.md exists, but has no marker section for this agent key.
	writeInstalled(t, root, RootFileClaude, []byte("# unrelated content\n"))
	rec := LockRecord{
		AgentKey: "go-architect", Harness: "claude", DestPath: RootFileClaude,
		SourcePath: "src/go-architect.md", SourceHash: "src-hash", RenderedHash: "rendered-hash",
	}

	status, err := Classify(root, rec, rec.SourceHash, nil, []byte("body"))
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if status != StatusMissing {
		t.Errorf("status = %v, want StatusMissing", status)
	}
}

func TestStatusStringNamesEveryState(t *testing.T) {
	cases := []struct {
		status Status
		want   string
	}{
		{StatusInSync, "in sync"},
		{StatusLocallyModified, "locally modified"},
		{StatusUpstreamUpdated, "upstream updated"},
		{StatusConflict, "conflict"},
		{StatusMissing, "missing"},
	}
	for _, c := range cases {
		if got := c.status.String(); got != c.want {
			t.Errorf("Status(%d).String() = %q, want %q", c.status, got, c.want)
		}
	}
}
