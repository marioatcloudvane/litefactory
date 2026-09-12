package harness

import (
	"strings"
	"testing"
)

// TestBuildPromptSessionArtifactsByteIdentity is AC-010: with both fields
// empty, BuildPrompt returns exactly what it returned before this feature
// existed.
func TestBuildPromptSessionArtifactsByteIdentity(t *testing.T) {
	withField := testPromptInput()
	baseline := buildPrompt(t, testPromptInput())
	got := buildPrompt(t, withField)
	if got != baseline {
		t.Fatalf("BuildPrompt with empty session artifact fields changed the prompt:\ngot:  %q\nwant: %q", got, baseline)
	}
}

// TestBuildPromptNamesExistingSessionArtifacts is AC-009: a cold run names
// the located directory and the mode-relevant brief.
func TestBuildPromptNamesExistingSessionArtifacts(t *testing.T) {
	in := testPromptInput()
	in.SessionArtifactDir = ".aiteam/lf-123-add-a-widget/2026-09-01/add-a-widget"
	in.SessionArtifactBrief = "plan_brief.md"

	got := buildPrompt(t, in)
	for _, want := range []string{
		"## Existing session artifacts",
		".aiteam/lf-123-add-a-widget/2026-09-01/add-a-widget",
		".aiteam/lf-123-add-a-widget/2026-09-01/add-a-widget/plan_brief.md",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt missing %q:\n%s", want, got)
		}
	}
}

// TestBuildPromptSessionArtifactsDirectoryOnly covers a mode with a located
// directory but no relevant brief (spike, or a brief simply not present
// yet): the section still names the directory, without inventing a brief.
func TestBuildPromptSessionArtifactsDirectoryOnly(t *testing.T) {
	in := testPromptInput()
	in.SessionArtifactDir = ".aiteam/lf-123-add-a-widget/2026-09-01/add-a-widget"

	got := buildPrompt(t, in)
	if !strings.Contains(got, "## Existing session artifacts") {
		t.Errorf("prompt missing the section heading:\n%s", got)
	}
	if strings.Contains(got, "plan_brief.md") {
		t.Errorf("prompt named a brief that was never supplied:\n%s", got)
	}
}

// TestBuildPromptSessionArtifactsOmittedOnResume is AC-011: a resumed prompt
// never carries the section, even when the fields are populated.
func TestBuildPromptSessionArtifactsOmittedOnResume(t *testing.T) {
	in := testPromptInput()
	in.Resume = true
	in.SessionArtifactDir = ".aiteam/lf-123-add-a-widget/2026-09-01/add-a-widget"
	in.SessionArtifactBrief = "plan_brief.md"

	got := buildPrompt(t, in)
	if strings.Contains(got, "## Existing session artifacts") {
		t.Errorf("resumed prompt carried the existing-session-artifacts section:\n%s", got)
	}
}
