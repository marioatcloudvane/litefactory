package harness

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
)

func writeValidation(b *strings.Builder, in PromptInput) error {
	if in.Validation == nil {
		return nil
	}
	context := in.Validation.Clone()
	withholdSnapshotContent(context)
	data, err := json.Marshal(context)
	if err != nil {
		return err
	}
	// The read-only contract lives in the validate-review skill's
	// retained-contract resource; the prompt carries only the facts.
	fmt.Fprintf(b, "\n## Managed read-only Validate contract\n\n- Submission version: %d\n- Retained file bodies: withheld (path, sha256 and deletion flag listed)\n\n", max(in.SubmissionVersion, 1))
	fmt.Fprintf(b, "<note>\nSource: factory-retained validation context (requirements data)\n\n%s\n</note>\n", neutraliseNoteTags(string(data)))
	return nil
}

// withholdSnapshotContent drops retained file bodies from the prompt projection.
// encoding/json renders StorySnapshotFile.Content as base64, so inlining bodies
// cost a third more bytes than the source and tokenised far worse than it: the
// twelve stories of issue #207 carried 180 files and 2.69MB of base64, 96% of a
// 2.81MB prompt, and the run died on a 400 prompt_too_long before it had
// reviewed anything. Path, sha256 and the deletion flag are what the reviewer
// actually needs. It holds Read, Glob and Grep on a worktree checked out at the
// reviewed commit, and ImplementationDigest is SnapshotDigest over exactly those
// three fields, so a withheld body is still verifiable against its recorded
// hash. Clone deep-copies Content, so this never reaches the caller's context.
func withholdSnapshotContent(v *agent.ValidationContext) {
	if v == nil {
		return
	}
	for i := range v.Stories {
		evidence := v.Stories[i].Evidence
		if evidence == nil {
			continue
		}
		for j := range evidence.Files {
			evidence.Files[j].Content = nil
		}
	}
}
