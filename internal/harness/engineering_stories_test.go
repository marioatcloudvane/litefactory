package harness

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func TestEngineeringInstructionsOnColdAndResume(t *testing.T) {
	for _, resume := range []bool{false, true} {
		in := testPromptInput()
		in.Mode = "implement"
		in.Resume = resume
		in.ApprovedStories = &agent.ApprovedBundle{WorkflowVersion: 2, Parent: in.Ref.String(), RunID: 7, RunOrdinal: in.RunOrdinal, Generation: 1, EventDirectory: agent.StoryEventsDirectory + "/7", Stories: []agent.ApprovedStory{{Key: "US-1", State: "frozen", IssueID: 2000, ParentID: 1230, IssueNumber: 200, Title: "Story", Body: "Frozen requirements", Revision: "revision"}}}
		prompt := buildPrompt(t, in)
		if !strings.Contains(prompt, "- Event kind: engineering_complete") || strings.Contains(prompt, "- Event kind: verified") {
			t.Fatal("new workflow inherited legacy verification contract")
		}
		in.ApprovedStories.ParentRecovery = true
		in.ApprovedStories.EventDirectory = ""
		prompt = buildPrompt(t, in)
		if !strings.Contains(prompt, "Engineering aggregate recovery") || strings.Contains(prompt, "Parent completion recovery") {
			t.Fatal("engineering recovery claims acceptance")
		}
	}
}
