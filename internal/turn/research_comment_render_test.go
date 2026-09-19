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
	body := renderResearchComment(questionSet(), "")
	business := strings.Index(body, "# Product Owner Questions")
	technical := strings.Index(body, "# Technical Questions")
	if business < 0 || technical < 0 {
		t.Fatalf("missing audience headings:\n%s", body)
	}
	if business > technical {
		t.Error("business questions must come first regardless of candidate order")
	}
	if q1, q2 := strings.Index(body, "Answer with `Q-1`"), strings.Index(body, "Answer with `Q-2`"); q1 < business || q1 > technical || q2 < technical {
		t.Errorf("questions are not filed under their audience:\n%s", body)
	}
}

// Both of these were carried by the schema and thrown away at render time: an
// owner had to quote text back to answer, and a proposed answer nobody sees is
// not a proposal.
func TestRenderResearchCommentKeepsIdentitiesAndRecommendation(t *testing.T) {
	body := renderResearchComment(questionSet(), "")
	for _, want := range []string{
		"### Options",
		"- **`a`** — Pin per instance  \u2705 **recommended**",
		"- **`b`** — Re-capture",
		"- **`a`** — Successor issue",
		"- **`b`** — Stays closed  \u2705 **recommended**",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
}

func TestRenderResearchCommentOmitsEmptySections(t *testing.T) {
	body := renderResearchComment(agentresult.Result{Outcome: "completed", Comment: "Done."}, "")
	if strings.Contains(body, "#") {
		t.Errorf("a report without questions must carry no question headings:\n%s", body)
	}
	if strings.TrimSpace(body) != "Done." {
		t.Errorf("unexpected body: %q", body)
	}
	one := agentresult.Result{Comment: "c", Questions: []agentresult.Question{{ID: "Q-1", Audience: "business", Text: "t"}}}
	if b := renderResearchComment(one, ""); strings.Contains(b, "Technical Questions") || strings.Contains(b, "### Options") {
		t.Errorf("empty audience or option section rendered:\n%s", b)
	}
}

// The footer is the only thing telling an owner whether the factory is still
// working. Without it a parked report looks finished, and replying to a running
// instance is refused outright rather than queued — so the natural response to
// a report that reads as complete is the one that breaks it.
func TestRenderResearchCommentCarriesTheNextStep(t *testing.T) {
	body := renderResearchComment(questionSet(), "reply on this issue to continue.")
	if !strings.Contains(body, "**Next:** reply on this issue to continue.") {
		t.Errorf("the next step is missing:\n%s", body)
	}
	if strings.Index(body, "**Next:**") < strings.Index(body, "# Technical Questions") {
		t.Error("the next step must come after the questions it follows")
	}
	if plain := renderResearchComment(questionSet(), ""); strings.Contains(plain, "**Next:**") {
		t.Error("an unknown next step must render nothing rather than a guess")
	}
}

// Each publication kind means something different for the owner, and "final"
// means two different things depending on whether the graph goes on.
func TestResearchNextStepFollowsTheGraph(t *testing.T) {
	p := shapingProfile(t)
	for _, tc := range []struct{ node, kind, want string }{
		{"ask", "question", "reply on this issue"},
		{"clarify", "question", "reply on this issue"},
		{"report", "final", "starts on its own"},
		{"agreed", "final", "finished"},
		{"failed", "failure", "failed"},
	} {
		got := researchNextStep(p, tc.node, tc.kind)
		if !strings.Contains(got, tc.want) {
			t.Errorf("%s/%s: want %q in %q", tc.node, tc.kind, tc.want, got)
		}
	}
	if researchNextStep(nil, "report", "final") != "" {
		t.Error("without a graph there is nothing certain to say")
	}
}

// The heading over the reason has to match what follows it: the same field
// explains why an owner is being asked, or why work stopped short.
func TestRenderResearchCommentNamesTheReasonForWhatItIs(t *testing.T) {
	asking := renderResearchComment(questionSet(), "")
	if !strings.Contains(asking, "## Why I need your input") {
		t.Errorf("a report with questions must say why the owner is needed:\n%s", asking)
	}
	stopped := agentresult.Result{Outcome: "blocked", Comment: "Stopped.", Reason: "The export job could not be located."}
	body := renderResearchComment(stopped, "")
	if !strings.Contains(body, "## Why this is unfinished") || strings.Contains(body, "your input") {
		t.Errorf("a report without questions must say why it stopped:\n%s", body)
	}
}

// Questions are numbered for a reader and identified for an answer. Numbering
// runs across the whole report so "the second question" is unambiguous even
// though the questions are filed under different readers.
func TestRenderResearchCommentNumbersQuestionsForAReader(t *testing.T) {
	body := renderResearchComment(questionSet(), "")
	for _, want := range []string{
		"## Question 1  \u00b7  Should a declined scope return as a successor issue?",
		"_Answer with `Q-1`_",
		"## Question 2  \u00b7  Pin the definition per instance, or re-capture on resume?",
		"_Answer with `Q-2`_",
		"- **`a`** — Successor issue",
		"\u2705 **recommended**",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
	if strings.Contains(body, "## Q-1 —") {
		t.Error("the bare identifier is no longer the heading")
	}
}
