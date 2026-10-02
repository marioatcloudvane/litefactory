package turn

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/store"
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
var bugRetryable = map[string]bool{"analyst": true, "record_analysis": true, "fixer": true, "record_repair": true}

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
		return "The revised analysis reworded a question that an earlier revision already recorded. Recorded questions keep their exact wording, because your answers refer to their IDs; a different question needs a new ID."
	case strings.Contains(text, "bug package differs from retained definition"):
		return "This Bug task started on an earlier version of the Bug workflow's graph, which the factory has since changed, so it cannot continue. File the bug again as a new issue to run it on the current workflow."
	case strings.Contains(text, "not waiting for an owner reply"):
		return "This Bug task is not waiting for a reply right now, so there is nothing this comment could continue."
	case strings.Contains(text, "only a Bug parent that failed in its analyst or fixer"):
		return "Only a failed analysis or repair, or a blocked analysis, can be restarted by a comment."
	case strings.Contains(text, bugExhaustionNotWaitingText):
		return "This Bug task is waiting for the owner to answer its exhaustion question: reply `/grant-bug-attempt <n> [instructions]` to give it one more repair attempt, or `/stop-bug <n>` to stop here."
	default:
		return ""
	}
}

// bugDetails appends the raw error, collapsed, so diagnosis stays possible
// without making it the message.
func bugDetails(b *strings.Builder, text string) {
	fmt.Fprintf(b, "\n<details><summary>Technical details</summary>\n\n```text\n%s\n```\n</details>\n", strings.ReplaceAll(text, "```", "'''"))
}

// bugNoticeComment is the owner-facing report when a Bug parent ends through
// its report node: a finished delivery, or a stop with the step's own reason
// and whether a comment can restart it. The retained outputs stay available,
// collapsed.
func bugNoticeComment(node, outcome string, payloads []string, instance, attempt int64) string {
	var b strings.Builder
	if node == "close" && outcome == "confirmed" {
		b.WriteString("**The Bug workflow is done: the fix is merged and this issue is closed.**\n")
	} else {
		step := bugSteps[node]
		if step == "" {
			step = "at step `" + node + "`"
		}
		how := map[string]string{"blocked": "it is blocked", "exhausted": "its budget is used up", "failed": "it failed", "pending": "a remote effect is still unconfirmed", "cancelled": "it was cancelled"}[outcome]
		if how == "" {
			how = "its outcome was `" + outcome + "`"
		}
		fmt.Fprintf(&b, "**The Bug workflow stopped while %s: %s.**\n\n", step, how)
		for _, p := range payloads {
			var v struct {
				Reason string `json:"reason"`
			}
			if json.Unmarshal([]byte(p), &v) == nil && strings.TrimSpace(v.Reason) != "" {
				b.WriteString(strings.TrimSpace(v.Reason) + "\n\n")
				break
			}
		}
		switch {
		case (node == "record_analysis" && outcome == "blocked") || (node == "analyst" && outcome == "failed"):
			b.WriteString("Reply on this issue with what the analyst should do differently, and it runs again. This counts against the analysis budget.\n")
		case outcome == "exhausted":
			b.WriteString("A comment cannot restart it; the budget for this bug is spent.\n")
		default:
			b.WriteString("A comment cannot restart this step; the factory keeps its evidence for recovery.\n")
		}
	}
	details := fmt.Sprintf("graph %d, attempt %d, %s: %s", instance, attempt, node, outcome)
	for _, p := range payloads {
		details += "\n\n" + p
	}
	bugDetails(&b, details)
	return b.String()
}

// bugExhaustionAttemptFullBytes bounds each fully rendered attempt in the
// exhaustion question so the newest six attempts stay inside the report size
// bound alongside every older one-line entry (AC-011).
const bugExhaustionAttemptFullBytes = 1024

// bugExhaustionQuestionComment renders the exhaustion question: the bounded
// full history of every repair attempt, every earlier exhaustion and its
// decision, and the two commands the owner can reply with.
func bugExhaustionQuestionComment(n int, history store.BugRepairHistory) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**This Bug task's repair budget is spent (exhaustion %d).** A person needs to decide what happens next.\n\n", n)
	b.WriteString("Here is everything tried so far:\n\n")
	total := len(history.Attempts)
	fullFrom := total - 6
	if fullFrom < 0 {
		fullFrom = 0
	}
	for i, a := range history.Attempts {
		if i < fullFrom {
			b.WriteString(bugRepairAttemptOneLine(a) + "\n")
			continue
		}
		b.WriteString(bugRepairAttemptFull(a) + "\n")
	}
	if len(history.Exhaustions) > 0 {
		b.WriteString("\n**Earlier exhaustions:**\n\n")
		for _, x := range history.Exhaustions {
			decision := x.Decision
			if decision == "" {
				decision = "undecided"
			}
			if x.Decision == "grant" && x.Instructions != "" {
				fmt.Fprintf(&b, "- exhaustion %d: %s (instructions: %s)\n", x.N, decision, x.Instructions)
			} else {
				fmt.Fprintf(&b, "- exhaustion %d: %s\n", x.N, decision)
			}
		}
	}
	fmt.Fprintf(&b, "\nReply `/grant-bug-attempt %d` (optionally followed by instructions for what to try differently) to give this bug one more repair attempt, or `/stop-bug %d` to stop here. Either way this issue stays open.\n", n, n)
	return b.String()
}

// bugRepairAttemptFull renders one attempt's full record, bounded to 1 KiB.
func bugRepairAttemptFull(a store.BugRepairHistoryAttempt) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**Attempt %d** (cause: %s", a.Ordinal, a.Cause)
	if a.GrantReference != "" {
		b.WriteString(", granted")
		if a.GrantInstructions != "" {
			fmt.Fprintf(&b, " with instructions: %s", a.GrantInstructions)
		}
	}
	b.WriteString(")\n")
	if a.Summary != "" {
		fmt.Fprintf(&b, "- Summary: %s\n", a.Summary)
	}
	if a.FellShort != "" {
		fmt.Fprintf(&b, "- Fell short: %s\n", a.FellShort)
	} else {
		fmt.Fprintf(&b, "- State: %s\n", a.State)
	}
	text := b.String()
	if len(text) > bugExhaustionAttemptFullBytes {
		text = text[:bugExhaustionAttemptFullBytes-1] + "…\n"
	}
	return text
}

// bugRepairAttemptOneLine renders one older attempt as a single bounded line.
func bugRepairAttemptOneLine(a store.BugRepairHistoryAttempt) string {
	short := a.FellShort
	if short == "" {
		short = a.State
	}
	line := fmt.Sprintf("- Attempt %d (%s): %s", a.Ordinal, a.Cause, short)
	const limit = 200
	if len(line) > limit {
		line = line[:limit-1] + "…"
	}
	return line
}
