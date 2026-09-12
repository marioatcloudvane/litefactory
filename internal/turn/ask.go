package turn

import (
	"strings"

	"github.com/cloudvane/litefactory/internal/harness"
)

// AskTag is the literal a comment carries to give the run it triggers
// permission to stop and ask a question instead of guessing (Q3, AC-A1.4).
//
// Research and Plan permit material asynchronous questions by
// default; an explicit current no-question instruction overrides both defaults.
//
// # It is not the self-marker, and merging the two would be a security bug
//
// [MarkerPrefix] — "<!-- lf:" — identifies litefactory's *own* comments so it
// cannot read its own output back as an instruction. AskTag grants permission
// to a run. They are different literals for different purposes and neither
// matches the other: "<!-- lf: ask -->" is not a tag and "lf: ask" is not a
// marker. The resemblance invites a future "simplification" that folds one into
// the other, and folding them would let any comment litefactory itself posted
// grant permission to the next run. AC-A1.4a asserts non-matching in both
// directions so that the simplification fails a test rather than shipping.
const AskTag = "lf: ask"

// The two aiteam modes that may ask (Q6), plus the unclassified one.
//
// They are mode strings as [Request.mode] produces them — the command word
// without its slash — and not a second vocabulary to keep in step.
const (
	researchMode = "research"
	planMode     = "plan"
)

// HasAskTag reports whether any line of body is exactly [AskTag].
//
// Line-anchored and whole-line, never a substring search anywhere in the body
// (AC-A1.4, F-22). The difference is load-bearing in a way that is easy to
// underestimate: the implementation plan for this feature, and every document
// and comment discussing it, contains the literal many times. Under a
// strings.Contains match, an issue body quoting the plan — or a comment asking
// "what does lf: ask do?" — would silently turn permission on for a run nobody
// meant to grant it to.
//
// The anchoring rules, and why each is where it is:
//
//   - Leading whitespace is not tolerated, exactly as [HasMarker] does not
//     tolerate it. GitHub's "quote reply" prefixes every quoted line with "> ",
//     and an indented line is a markdown code block or a list continuation. Text
//     that is quoting or displaying the tag is discussing permission, not
//     granting it.
//   - The line must be nothing but the tag. Trailing whitespace is ignored,
//     because a forge that hands back CRLF line endings would otherwise make the
//     tag depend on which client wrote the comment. Anything else on the line is
//     a sentence about asking, and "lf: ask me about the schema" is prose.
//   - The match is byte-exact and case-sensitive. A mis-cased "Lf: ask" — an
//     autocapitalising phone keyboard is the realistic source — is simply not a
//     tag, and the run proceeds headless as it always has. That is the safe
//     direction to fail in: the cost is a run that did not ask, not a run that
//     asked when it was not allowed to.
//
// It never reports an error, and there is nothing it could report one for: a
// malformed tag is a body with no tag in it. [ParseCommand]'s contract is that
// every body classifies as something, and a parser that could fail underneath
// it would be a second answer to a question that already has one.
func HasAskTag(body string) bool {
	for line := range strings.SplitSeq(body, "\n") {
		if isAskTagLine(line) {
			return true
		}
	}
	return false
}

// isAskTagLine reports whether one line, trailing whitespace aside, is the tag.
func isAskTagLine(line string) bool {
	rest, ok := strings.CutPrefix(line, AskTag)
	return ok && strings.TrimSpace(rest) == ""
}

// stripAskTag removes every tag line from body, leaving the rest of it exactly
// as it was.
//
// The tag is permission, not instruction, and [Command.Text] becomes
// [harness.PromptInput.Instruction] — the one field in the prompt with no size
// cap and the one documented as trusted. A tag left in it is a line of
// litefactory's own control vocabulary handed to the agent as part of what it
// was asked to do (AC-A1.8).
//
// Whole lines are removed rather than blanked, so that a body which was nothing
// but the tag comes back empty and classifies as [KindNone] — no run, no reply,
// comment consumed (AC-A1.9). Blanking would leave whitespace that
// [ParseCommand] would still tokenise as an empty free-text instruction, which
// BuildPrompt then refuses with ErrInvalidPrompt: a validation error on a run
// the owner asked for, naming nothing they could act on.
func stripAskTag(body string) string {
	lines := strings.Split(body, "\n")
	kept := lines[:0]
	for _, line := range lines {
		if isAskTagLine(line) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// modeMayAsk reports whether a run in this mode is allowed to ask at all.
//
// This is the mode boundary shared by current and previous request policy.
// Legacy infer requires the ask tag; Research and Plan do not. Other modes
// never gain question permission. All permitted questions are asynchronous.
func modeMayAsk(mode string) bool {
	switch strings.TrimSpace(mode) {
	case researchMode, planMode, harness.InferMode:
		return true
	default:
		return false
	}
}

// questionPermission derives the same policy for live and previous requests.
// Only the direct instruction can revoke permission; quoted history and issue
// background are not consulted for a comment-triggered run.
func questionPermission(mode string, command Command) bool {
	if !modeMayAsk(mode) || forbidsQuestions(command.Text) {
		return false
	}
	return strings.TrimSpace(mode) == researchMode || strings.TrimSpace(mode) == planMode || command.Tagged
}

func forbidsQuestions(instruction string) bool {
	fenced := false
	for line := range strings.SplitSeq(instruction, "\n") {
		line = strings.ToLower(strings.TrimSpace(line))
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			fenced = !fenced
			continue
		}
		if fenced || strings.HasPrefix(line, ">") {
			continue
		}
		for _, phrase := range []string{"do not ask", "don't ask", "don’t ask", "stop asking", "no questions", "no more questions", "no clarifying questions", "no further questions", "no follow-up questions", "without questions", "without asking", "proceed with assumptions", "proceed with your assumptions", "use your best judgment", "use your best judgement"} {
			if strings.Contains(line, phrase) {
				return true
			}
		}
	}
	return false
}
