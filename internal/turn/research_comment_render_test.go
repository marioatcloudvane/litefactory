package turn

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agentresult"
)

func questionSet() agentresult.Result {
	return agentresult.Result{
		Outcome: "incomplete",
		Comment: "Two things are still open.",
		Reason:  "Neither can be settled by reading the repository.",
		Questions: []agentresult.Question{
			{ID: "Q-2", Audience: "technical", Text: "Pin the definition per instance, or re-capture on resume?",
				Options: []agentresult.Option{{ID: "a", Text: "Pin per instance"}, {ID: "b", Text: "Re-capture"}}, RecommendedOption: "a"},
			{ID: "Q-1", Audience: "business", Text: "Should a declined scope return as a successor issue?",
				Options: []agentresult.Option{{ID: "a", Text: "Successor issue"}, {ID: "b", Text: "Stays closed"}}, RecommendedOption: "b"},
		},
	}
}

// The layout is the factory's, not the agent's, so the same question set always
// reaches the owner the same way and each role can find its own part.
func TestRenderResearchCommentGroupsByAudience(t *testing.T) {
	body := renderResearchComment(questionSet())
	business := strings.Index(body, "# Product Owner Questions")
	technical := strings.Index(body, "# Technical Questions")
	if business < 0 || technical < 0 {
		t.Fatalf("missing audience headings:\n%s", body)
	}
	if business > technical {
		t.Error("business questions must come first regardless of candidate order")
	}
	if q1, q2 := strings.Index(body, "## Q-1"), strings.Index(body, "## Q-2"); q1 < business || q1 > technical || q2 < technical {
		t.Errorf("questions are not filed under their audience:\n%s", body)
	}
}

// Both of these were carried by the schema and thrown away at render time: an
// owner had to quote text back to answer, and a proposed answer nobody sees is
// not a proposal.
func TestRenderResearchCommentKeepsIdentitiesAndRecommendation(t *testing.T) {
	body := renderResearchComment(questionSet())
	for _, want := range []string{
		"### Options",
		"- **a** — Pin per instance  *(recommended)*",
		"- **b** — Re-capture",
		"- **a** — Successor issue",
		"- **b** — Stays closed  *(recommended)*",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
}

func TestRenderResearchCommentOmitsEmptySections(t *testing.T) {
	body := renderResearchComment(agentresult.Result{Outcome: "completed", Comment: "Done."})
	if strings.Contains(body, "#") {
		t.Errorf("a report without questions must carry no question headings:\n%s", body)
	}
	if strings.TrimSpace(body) != "Done." {
		t.Errorf("unexpected body: %q", body)
	}
	one := agentresult.Result{Comment: "c", Questions: []agentresult.Question{{ID: "Q-1", Audience: "business", Text: "t"}}}
	if b := renderResearchComment(one); strings.Contains(b, "Technical Questions") || strings.Contains(b, "### Options") {
		t.Errorf("empty audience or option section rendered:\n%s", b)
	}
}
