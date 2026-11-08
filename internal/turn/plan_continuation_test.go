package turn

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestPlanQuestionPermissionCurrentAndPrevious(t *testing.T) {
	cases := []struct {
		name, mode, body string
		want             bool
	}{
		{"plain Plan reply", "plan", "Use canonical labels", true}, {"tagged Plan", "plan", "plan it\nlf: ask", true}, {"Plan do not ask", "plan", "Do not ask any more questions", false}, {"Plan no questions", "plan", "No questions this run\nlf: ask", false}, {"Plan don't ask", "plan", "Don't ask; assume defaults", false},
		{"Plan no clarifying", "plan", "No clarifying questions", false},
		{"Plan no further", "plan", "No further questions", false},
		{"Plan no follow-up", "plan", "No follow-up questions", false}, {"quoted prohibition", "plan", "> do not ask\nProceed", true}, {"fenced prohibition", "plan", "```\nno questions\n```\nProceed", true},
		{"plain Research", "research", "Investigate", true}, {"Research plain answer", "research", "Use canonical labels", true}, {"Research proceed", "research", "Proceed with your assumptions", false}, {"Research quoted prohibition", "research", "> no questions\nInvestigate", true}, {"tagged Research", "research", "Investigate\nlf: ask", true}, {"Research explicit prohibition", "research", "Do not ask\nlf: ask", false}, {"Implement tagged", "implement", "Build\nlf: ask", false}, {"Spike tagged", "spike", "Try\nlf: ask", false}, {"Validate tagged", "validate", "Check\nlf: ask", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := questionPermission(tc.mode, ParseCommand(tc.body)); got != tc.want {
				t.Fatalf("current permission=%v want=%v", got, tc.want)
			}
			previous := store.Run{Mode: tc.mode, TriggerCommentID: 100}
			if got := previousMayAsk(previous, true, tc.body, "unrelated issue\nlf: ask"); got != tc.want {
				t.Fatalf("previous permission=%v want=%v", got, tc.want)
			}
			previous.TriggerCommentID = 0
			if got := previousMayAsk(previous, true, "", tc.body); got != tc.want {
				t.Fatalf("body-trigger previous permission=%v want=%v", got, tc.want)
			}
		})
	}
}
