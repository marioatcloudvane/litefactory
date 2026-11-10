package harness

import (
	"encoding/json"
	"fmt"
	"github.com/cloudvane/litefactory/internal/agent"
	"strings"
)

func writeApprovedStories(b *strings.Builder, in PromptInput) error {
	bundle := in.ApprovedStories.Clone()
	if bundle == nil {
		return nil
	}
	if in.Mode != "implement" || bundle.Parent != in.Ref.String() || bundle.RunID < 1 || bundle.RunOrdinal != in.RunOrdinal || bundle.Generation < 1 {
		return fmt.Errorf("%w: invalid approved story identity", ErrInvalidPrompt)
	}
	for _, story := range bundle.Stories {
		if story.Key == "" || story.IssueID < 1 || story.ParentID < 1 || story.IssueNumber < 1 || story.Revision == "" || story.Body == "" {
			return fmt.Errorf("%w: incomplete approved story revision", ErrInvalidPrompt)
		}
	}
	data, err := json.Marshal(bundle)
	if err != nil {
		return fmt.Errorf("marshal approved stories: %w", err)
	}
	if len(data) > 16*1024*1024 {
		return fmt.Errorf("%w: approved story bundle exceeds limit", ErrInvalidPrompt)
	}
	// The rules for this scope live in the implement-result skill's
	// approved-scope resource; the prompt carries only the facts.
	fmt.Fprintf(b, "\n## Approved implementation scope\n\n- Workflow version: %d\n\n", bundle.WorkflowVersion)
	b.WriteString(note("Factory-selected immutable GitHub story revisions; JSON source data", string(data)))
	if bundle.ParentRecovery && bundle.WorkflowVersion == 2 {
		b.WriteString("\n## Engineering aggregate recovery\n\n- All story engineering: complete\n")
	} else if bundle.ParentRecovery {
		b.WriteString("\n## Parent completion recovery\n\n- Every selected story: completed\n")
	}
	if bundle.EventDirectory != "" && in.SubmissionVersion != 2 {
		if bundle.EventDirectory != fmt.Sprintf("%s/%d", agent.StoryEventsDirectory, bundle.RunID) {
			return fmt.Errorf("%w: invalid story event directory", ErrInvalidPrompt)
		}
		kind := "verified"
		if bundle.WorkflowVersion == 2 {
			kind = "engineering_complete"
		}
		fmt.Fprintf(b, "\n## Incremental story verification\n\n- Event directory: `%s` (relative to worktree root)\n- Event kind: %s\n", bundle.EventDirectory, kind)
	}
	b.WriteString("\n")
	return nil
}
