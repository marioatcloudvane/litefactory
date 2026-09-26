package turn

import (
	"fmt"
	"strings"
)

// bugSteps names each Bug parent node the way the owner thinks of it.
var bugSteps = map[string]string{
	"record":          "recording the bug",
	"analysis_needed": "deciding whether analysis is needed",
	"analyst":         "analysing the bug",
	"record_analysis": "recording the analysis",
	"analysis_done":   "checking the analysis",
	"attempt":         "reserving a repair attempt",
	"fixer":           "repairing the bug",
	"record_repair":   "recording the repair",
	"test":            "running the project checks",
	"pr":              "preparing the pull request",
	"merge":           "merging the pull request",
	"close":           "closing the issue",
}

// bugRetryable are the steps an owner comment restarts after a failure; the
// store enforces the same set.
var bugRetryable = map[string]bool{"analyst": true, "record_analysis": true, "fixer": true}

// bugFailureComment explains a stopped Bug parent to the owner: where it
// stopped, what that means when the cause is known, and what they can do.
// The raw error stays available, collapsed, for diagnosis.
func bugFailureComment(node string, cause error) string {
	step := bugSteps[node]
	if step == "" {
		step = "at step `" + node + "`"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "**The Bug workflow stopped while %s.**\n\n", step)
	if explanation := bugExplanation(cause.Error()); explanation != "" {
		b.WriteString(explanation + "\n\n")
	}
	if bugRetryable[node] {
		b.WriteString("Reply on this issue, for example \"try again\", to run this step again. Your reply can also say what to do differently. It counts against the same analysis or repair budget.\n")
	} else {
		b.WriteString("A comment cannot restart this step; the factory keeps its evidence for recovery.\n")
	}
	bugDetails(&b, cause.Error())
	return b.String()
}

// bugAdmissionComment explains why an owner comment could not be applied.
func bugAdmissionComment(cause error) string {
	var b strings.Builder
	b.WriteString("**Your comment could not be applied to the Bug workflow.**\n\n")
	if explanation := bugExplanation(cause.Error()); explanation != "" {
		b.WriteString(explanation + "\n")
	}
	bugDetails(&b, cause.Error())
	return b.String()
}

// bugExplanation turns known causes into plain words.
func bugExplanation(text string) string {
	switch {
	case strings.Contains(text, "changes an existing meaning"):
		return "The revised analysis reworded a criterion or question that an earlier revision already recorded. Recorded criteria keep their exact wording, because the repair and its checks are measured against them; a changed meaning needs a new ID."
	case strings.Contains(text, "not waiting for an owner reply"):
		return "This Bug task is not waiting for a reply right now, so there is nothing this comment could continue."
	case strings.Contains(text, "only a Bug parent that failed in its analyst or fixer can be retried"):
		return "Only a failed analysis or repair can be restarted by a comment."
	default:
		return ""
	}
}

// bugDetails appends the raw error, collapsed, so diagnosis stays possible
// without making it the message.
func bugDetails(b *strings.Builder, text string) {
	fmt.Fprintf(b, "\n<details><summary>Technical details</summary>\n\n```text\n%s\n```\n</details>\n", strings.ReplaceAll(text, "```", "'''"))
}
