package turn

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/naming"
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
	body := renderResearchComment(questionSet(), "", nil)
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

func TestRenderResearchCommentOmitsEmptySections(t *testing.T) {
	body := renderResearchComment(agentresult.Result{Outcome: "completed", Comment: "Done."}, "", nil)
	if strings.Contains(body, "#") {
		t.Errorf("a report without questions must carry no question headings:\n%s", body)
	}
	if strings.TrimSpace(body) != "Done." {
		t.Errorf("unexpected body: %q", body)
	}
	one := agentresult.Result{Comment: "c", Questions: []agentresult.Question{{ID: "Q-1", Audience: "business", Text: "t"}}}
	if b := renderResearchComment(one, "", nil); strings.Contains(b, "Technical Questions") || strings.Contains(b, "### Options") {
		t.Errorf("empty audience or option section rendered:\n%s", b)
	}
}

// The footer is the only thing telling an owner whether the factory is still
// working. Without it a parked report looks finished, and replying to a running
// instance is refused outright rather than queued — so the natural response to
// a report that reads as complete is the one that breaks it.
func TestRenderResearchCommentCarriesTheNextStep(t *testing.T) {
	body := renderResearchComment(questionSet(), "reply on this issue to continue.", nil)
	if !strings.Contains(body, "**Next:** reply on this issue to continue.") {
		t.Errorf("the next step is missing:\n%s", body)
	}
	if strings.Index(body, "**Next:**") < strings.Index(body, "# Technical Questions") {
		t.Error("the next step must come after the questions it follows")
	}
	if plain := renderResearchComment(questionSet(), "", nil); strings.Contains(plain, "**Next:**") {
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
	asking := renderResearchComment(questionSet(), "", nil)
	if !strings.Contains(asking, "## Why I need your input") {
		t.Errorf("a report with questions must say why the owner is needed:\n%s", asking)
	}
	stopped := agentresult.Result{Outcome: "blocked", Comment: "Stopped.", Reason: "The export job could not be located."}
	body := renderResearchComment(stopped, "", nil)
	if !strings.Contains(body, "## Why this is unfinished") || strings.Contains(body, "your input") {
		t.Errorf("a report without questions must say why it stopped:\n%s", body)
	}
}

// An owner answers by identifier, never by quoting text back: every question
// carries its id, every option its id, and the proposed answer is marked on the
// option it names. Layout and wording are not pinned.
func TestRenderResearchCommentNumbersQuestionsForAReader(t *testing.T) {
	set := questionSet()
	body := renderResearchComment(set, "", nil)
	for _, q := range set.Questions {
		start := strings.Index(body, q.Text)
		if start < 0 {
			t.Fatalf("question %s is missing:\n%s", q.ID, body)
		}
		end := len(body)
		for _, other := range set.Questions {
			if at := strings.Index(body, other.Text); at > start && at < end {
				end = at
			}
		}
		section := body[start:end]
		if !strings.Contains(section, "`"+q.ID+"`") {
			t.Errorf("question %s does not carry its id:\n%s", q.ID, section)
		}
		for _, o := range q.Options {
			line := ""
			for l := range strings.SplitSeq(section, "\n") {
				if strings.Contains(l, "`"+o.ID+"`") && strings.Contains(l, o.Text) {
					line = l
				}
			}
			if line == "" {
				t.Errorf("question %s option %s is not identified:\n%s", q.ID, o.ID, section)
			}
			if recommended := strings.Contains(line, "recommended"); recommended != (o.ID == q.RecommendedOption) {
				t.Errorf("question %s option %s recommended=%t:\n%s", q.ID, o.ID, recommended, section)
			}
		}
	}
}

// The factory writes two documents and, until now, never said so. A report that
// describes findings without pointing at the evidence behind it makes the
// reader take it on faith.
func TestRenderResearchCommentLinksSavedDocuments(t *testing.T) {
	ref, err := naming.ParseRef("github:acme/api#7")
	if err != nil {
		t.Fatal(err)
	}
	links := researchArtifactLinks(ref, "lf/7-export", []agentresult.Artifact{
		{ID: "brief", Kind: "brief", Path: ".aiteam/lf-7/RESEARCH_BRIEF.md"},
		{ID: "tech", Kind: "technical", Path: ".aiteam/lf-7/RESEARCH_TECHNICAL.md"},
		{ID: "junk", Kind: "notes", Path: ".aiteam/lf-7/NOTES.md"},
	})
	if len(links) != 2 {
		t.Fatalf("want the two known kinds, got %+v", links)
	}
	body := renderResearchComment(questionSet(), "reply on this issue to continue.", links)
	for _, want := range []string{
		"## Saved documents",
		"[Research brief](https://github.com/acme/api/blob/lf/7-export/.aiteam/lf-7/RESEARCH_BRIEF.md)",
		"[Technical findings](https://github.com/acme/api/blob/lf/7-export/.aiteam/lf-7/RESEARCH_TECHNICAL.md)",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
	if strings.Index(body, "## Saved documents") > strings.Index(body, "**Next:**") {
		t.Error("the documents belong above the next step, not after it")
	}
	// Refusing is safe; a wrong link is worse than none.
	if l := researchArtifactLinks(ref, "", nil); l != nil {
		t.Error("no branch means no honest link")
	}
	if l := researchArtifactLinks(ref, "b", []agentresult.Artifact{{Kind: "brief", Path: "../escape.md"}}); len(l) != 0 {
		t.Error("an unsafe path must not become a link")
	}
	if body := renderResearchComment(questionSet(), "", nil); strings.Contains(body, "Saved documents") {
		t.Error("no documents means no empty section")
	}
}

// The same question activity is a clarification in one place and a review gate
// in another. They are separate kinds now, because the difference is not only
// wording: the publication guard demands an incomplete result with questions
// for one and a completed result without them for the other, and overloading
// the kind meant a review could never be published at all.
func TestResearchNextStepDistinguishesAReviewGate(t *testing.T) {
	p := shapingProfile(t)
	review := researchNextStep(p, "review", "review")
	clarify := researchNextStep(p, "clarify", "question")
	if review == clarify {
		t.Fatal("a review gate and a clarification must not read the same")
	}
	for _, want := range []string{"dig deeper", "`done`", "`/plan plan the approved scope above into stories`", "Nothing moves on until you say so"} {
		if !strings.Contains(review, want) {
			t.Errorf("the review gate is missing %q: %q", want, review)
		}
	}
	if !researchNodeIsReview(p, "review") {
		t.Error("the review node is not recognised as one")
	}
	for _, notReview := range []string{"clarify", "report", "agreed"} {
		if researchNodeIsReview(p, notReview) {
			t.Errorf("%q is not a review gate", notReview)
		}
	}
}
