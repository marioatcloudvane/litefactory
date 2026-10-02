package turn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agentresult"
)

func briefResult() agentresult.Result {
	return agentresult.Result{
		Outcome: "incomplete",
		Comment: "Two things are open.",
		Reason:  "Neither can be settled by reading the repository.",
		Questions: []agentresult.Question{
			{ID: "Q-1", Audience: "business", Text: "Successor issue or closed?",
				Options: []agentresult.Option{{ID: "a", Text: "Successor"}, {ID: "b", Text: "Closed"}}, RecommendedOption: "a"},
		},
		Recommendation: &agentresult.Recommendation{Text: "Start with the catalog."},
		Payload: agentresult.Payload{Research: &agentresult.Research{
			Request:     "Understand mode selection.",
			Decisions:   []agentresult.Statement{{ID: "D-1", Text: "Graph execution stays opt-in."}},
			Assumptions: []agentresult.Statement{{ID: "A-1", Text: "The daemon is not restarted mid-run."}},
			Findings: []agentresult.Finding{{ID: "F-1", Text: "Mode is pinned at admission.",
				Evidence: []agentresult.SourceReference{{Path: "internal/turn/mode.go", Line: 42, Revision: "abc1234"}}}},
		}},
	}
}

// The split is by reader. A decision the owner has to make does not read better
// with a file and line number attached, and an engineer checking a claim should
// not have to scroll past owner questions to reach the evidence.
func TestResearchBriefsSplitByReader(t *testing.T) {
	brief := renderResearchBrief(".aiteam/session", briefResult())
	technical := renderResearchTechnical(briefResult())
	for _, want := range []string{"## Request", "**D-1**", "**A-1**", "## Open questions", "### Q-1", "Product Owner Questions", "*(recommended)*", "## Recommendation"} {
		if !strings.Contains(brief, want) {
			t.Errorf("brief is missing %q:\n%s", want, brief)
		}
	}
	if strings.Contains(brief, "internal/turn/mode.go") || strings.Contains(brief, "F-1") {
		t.Errorf("evidence leaked into the owner's brief:\n%s", brief)
	}
	for _, want := range []string{"## F-1", "`internal/turn/mode.go:42`", "@ `abc1234`"} {
		if !strings.Contains(technical, want) {
			t.Errorf("technical file is missing %q:\n%s", want, technical)
		}
	}
	if strings.Contains(technical, "D-1") || strings.Contains(technical, "Q-1") {
		t.Errorf("owner content leaked into the technical file:\n%s", technical)
	}
}

// Rendering is what makes supersession real: reusing an ID replaces the entry,
// and dropping one removes it. An agent appending Markdown can do neither.
func TestWriteResearchBriefsReplacesSupersededContent(t *testing.T) {
	worktree := t.TempDir()
	const session = ".aiteam/lf-1/2026-09-19/feature"
	first := briefResult()
	if err := WriteResearchBriefs(worktree, session, first); err != nil {
		t.Fatal(err)
	}
	second := briefResult()
	second.Payload.Research.Decisions = []agentresult.Statement{{ID: "D-1", Text: "Graph execution is now the default."}}
	second.Payload.Research.Assumptions = nil
	if err := WriteResearchBriefs(worktree, session, second); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(worktree, session, ResearchBriefName))
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if strings.Contains(body, "stays opt-in") {
		t.Errorf("superseded decision survived the rewrite:\n%s", body)
	}
	if !strings.Contains(body, "now the default") {
		t.Errorf("replacement decision is missing:\n%s", body)
	}
	if strings.Contains(body, "not restarted mid-run") {
		t.Errorf("dropped assumption survived the rewrite:\n%s", body)
	}
	if strings.Count(body, "**D-1**") != 1 {
		t.Errorf("an identity must render exactly once:\n%s", body)
	}
}

func TestWriteResearchBriefsRefusesUnsafeSession(t *testing.T) {
	if err := WriteResearchBriefs(t.TempDir(), "../escape", briefResult()); err == nil {
		t.Error("accepted a session path outside the worktree")
	}
	if err := WriteResearchBriefs(t.TempDir(), ".aiteam/x", agentresult.Result{}); err != nil {
		t.Errorf("a result without a research payload is not an error: %v", err)
	}
}

// The kinds are what a later stage selects on, so they cannot be free text. A
// shaping agent asking for the brief must not receive a raw evidence dump.
func TestResearchArtifactKindsAreClosed(t *testing.T) {
	scope := agentresult.Context{SchemaVersion: agentresult.SchemaVersion, Mode: "research", WorkflowVersion: 2, QuestionsAllowed: true}
	for _, tc := range []struct {
		kind  string
		valid bool
	}{{"brief", true}, {"technical", true}, {"data", true}, {"notes", false}, {"document", false}} {
		t.Run(tc.kind, func(t *testing.T) {
			result := briefResult()
			result.Artifacts = []agentresult.Artifact{{ID: "ART-1", Kind: tc.kind, Path: ".aiteam/lf-1/x/" + ResearchBriefName}}
			d := agentresult.ValidateResult(result, scope)
			if d.Valid != tc.valid {
				t.Fatalf("kind %q: valid=%v want=%v: %+v", tc.kind, d.Valid, tc.valid, d.Diagnostics)
			}
		})
	}
	// Other modes keep an open vocabulary; only Research renders its own files.
	// The Plan payload here is deliberately incomplete, so this asserts the
	// absence of the kind diagnostic rather than overall validity.
	plan := agentresult.Result{Outcome: "completed", Comment: "c",
		Artifacts: []agentresult.Artifact{{ID: "ART-1", Kind: "proposal", Path: "session/x.md"}},
		Payload:   agentresult.Payload{Plan: &agentresult.Plan{}}}
	for _, v := range agentresult.ValidateResult(plan, agentresult.Context{SchemaVersion: agentresult.SchemaVersion, Mode: "plan", WorkflowVersion: 2}).Diagnostics {
		if strings.HasSuffix(v.Path, ".kind") {
			t.Errorf("a non-Research kind must stay open: %+v", v)
		}
	}
}
