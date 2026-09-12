package turn

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
)

// TestExistingSessionArtifactsNoDirectory covers a task's first-ever run,
// which has nothing under .aiteam/ yet: neither field names anything.
func TestExistingSessionArtifactsNoDirectory(t *testing.T) {
	t.Parallel()

	day := time.Date(2026, 9, 1, 6, 10, 0, 0, time.UTC)
	dir, brief := existingSessionArtifacts(t.TempDir(), testSessionBranch, testSessionIssue, day, string(agent.ModeImplement), nil)
	if dir != "" || brief != "" {
		t.Errorf("existingSessionArtifacts(no session) = (%q, %q), want (\"\", \"\")", dir, brief)
	}
}

// TestExistingSessionArtifactsManifestOnlyNamesNoBrief covers a session that
// exists but has not reached either brief yet: the directory is found, via
// naming.ManifestName, but no brief is named because none is present.
func TestExistingSessionArtifactsManifestOnlyNamesNoBrief(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	want := writeSession(t, root, "2026-09-01", naming.ManifestName)

	day := time.Date(2026, 9, 1, 6, 10, 0, 0, time.UTC)
	dir, brief := existingSessionArtifacts(root, testSessionBranch, testSessionIssue, day, string(agent.ModeImplement), nil)
	if dir != want {
		t.Errorf("dir = %q, want %q", dir, want)
	}
	if brief != "" {
		t.Errorf("brief = %q, want \"\" (plan_brief.md was never written)", brief)
	}
}

// TestExistingSessionArtifactsImplementFindsPlanBrief covers the ordinary
// implement/validate case: the session directory holds plan_brief.md, and
// that is the brief named.
func TestExistingSessionArtifactsImplementFindsPlanBrief(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	want := writeSession(t, root, "2026-09-01", planbrief.FileName)

	day := time.Date(2026, 9, 1, 6, 10, 0, 0, time.UTC)
	for _, mode := range []string{string(agent.ModeImplement), string(agent.ModeValidate)} {
		dir, brief := existingSessionArtifacts(root, testSessionBranch, testSessionIssue, day, mode, nil)
		if dir != want || brief != planbrief.FileName {
			t.Errorf("mode %q: (dir, brief) = (%q, %q), want (%q, %q)", mode, dir, brief, want, planbrief.FileName)
		}
	}
}

// TestExistingSessionArtifactsPlanPrefersResearchBrief covers Plan's mapping:
// RESEARCH_BRIEF.md if present, else plan_brief.md — both markers exist here,
// so the research brief wins.
func TestExistingSessionArtifactsPlanPrefersResearchBrief(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	want := writeSession(t, root, "2026-09-01", naming.ResearchBriefName)
	abs := filepath.Join(root, filepath.FromSlash(want), planbrief.FileName)
	if err := os.WriteFile(abs, []byte("# plan brief\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", planbrief.FileName, err)
	}

	day := time.Date(2026, 9, 1, 6, 10, 0, 0, time.UTC)
	dir, brief := existingSessionArtifacts(root, testSessionBranch, testSessionIssue, day, string(agent.ModePlan), nil)
	if dir != want || brief != naming.ResearchBriefName {
		t.Errorf("(dir, brief) = (%q, %q), want (%q, %q)", dir, brief, want, naming.ResearchBriefName)
	}
}

// TestExistingSessionArtifactsPlanFallsBackToPlanBrief covers Plan once
// Research is done and only plan_brief.md remains.
func TestExistingSessionArtifactsPlanFallsBackToPlanBrief(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	want := writeSession(t, root, "2026-09-01", planbrief.FileName)

	day := time.Date(2026, 9, 1, 6, 10, 0, 0, time.UTC)
	dir, brief := existingSessionArtifacts(root, testSessionBranch, testSessionIssue, day, string(agent.ModePlan), nil)
	if dir != want || brief != planbrief.FileName {
		t.Errorf("(dir, brief) = (%q, %q), want (%q, %q)", dir, brief, want, planbrief.FileName)
	}
}

// TestExistingSessionArtifactsSpikeNamesDirectoryOnly covers spike (and, by
// the same default case, harness.InferMode): a directory may be named, but
// never a brief, regardless of what is actually present.
func TestExistingSessionArtifactsSpikeNamesDirectoryOnly(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	want := writeSession(t, root, "2026-09-01", planbrief.FileName)

	day := time.Date(2026, 9, 1, 6, 10, 0, 0, time.UTC)
	dir, brief := existingSessionArtifacts(root, testSessionBranch, testSessionIssue, day, string(agent.ModeSpike), nil)
	if dir != want {
		t.Errorf("dir = %q, want %q", dir, want)
	}
	if brief != "" {
		t.Errorf("brief = %q, want \"\" for spike", brief)
	}
}
