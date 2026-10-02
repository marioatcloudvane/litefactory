package harness

import (
	"strings"
	"testing"
)

// TestBuildPromptSessionArtifactsByteIdentity is AC-010: with both fields
// empty, BuildPrompt returns exactly what it returned before this feature
// existed. The comparison is not vacuous: the same input is first proven to
// actually change the prompt once the two fields are populated, and only then
// cleared back to their zero value to prove the empty case is byte-identical
// to the baseline — never a leftover heading, blank line or other residue
// from the fields having existed at all on a zero-valued input.
func TestBuildPromptSessionArtifactsByteIdentity(t *testing.T) {
	baseline := buildPrompt(t, testPromptInput())

	withField := testPromptInput()
	withField.SessionArtifactDir = ".aiteam/lf-123-add-a-widget/2026-09-01/add-a-widget"
	withField.SessionArtifactBrief = "plan_brief.md"
	populated := buildPrompt(t, withField)
	if populated == baseline {
		t.Fatal("populating the session artifact fields did not change the prompt; this test proves nothing")
	}

	withField.SessionArtifactDir = ""
	withField.SessionArtifactBrief = ""
	got := buildPrompt(t, withField)
	if got != baseline {
		t.Fatalf("BuildPrompt with the session artifact fields cleared back to empty did not match the baseline:\ngot:  %q\nwant: %q", got, baseline)
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

func TestPlanResearchDocumentPointers(t *testing.T) {
	for _, brief := range []string{"RESEARCH_BRIEF.md", "plan_brief.md"} {
		in := testPromptInput()
		in.SessionArtifactDir = ".aiteam/session"
		in.SessionArtifactBrief = brief
		got := buildPrompt(t, in)
		want := "Read `.aiteam/session/" + brief + "` first for the context already saved there before doing anything else.\n"
		if !strings.Contains(got, want) || strings.Contains(got, "RESEARCH_TECHNICAL.md") || strings.Contains(got, "Research documents") {
			t.Fatalf("legacy pointer changed: %s", got)
		}
	}
	in := testPromptInput()
	in.SessionArtifactDir = ".aiteam/session"
	in.SessionArtifactBrief = "RESEARCH_BRIEF.md"
	in.SessionArtifactTechnical = "RESEARCH_TECHNICAL.md"
	got := buildPrompt(t, in)
	for _, want := range []string{".aiteam/session/RESEARCH_BRIEF.md", ".aiteam/session/RESEARCH_TECHNICAL.md", "Research documents"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s", want)
		}
	}
	in.Resume = true
	if strings.Contains(buildPrompt(t, in), "## Existing session artifacts") {
		t.Fatal("pointer repeated on resume")
	}
}
