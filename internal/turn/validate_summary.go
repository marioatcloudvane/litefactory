package turn

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/store"
)

// validateSummaryLines bounds the summary to about one screen.
const validateSummaryLines = 12

// validateSummaryLine is one recommendation as the owner reads it: the item,
// what to do and the one exact command, rendered by the factory.
type validateSummaryLine struct {
	Ref       string
	Text      string
	Command   string
	Action    string
	Diagnosis string
	Evidence  string
	// Agent says the reporter wrote the recommendation; otherwise it is the
	// factory's own from the facts.
	Agent bool
}

// validateSummary is the short owner summary of the current state. It is
// derived from records only: the reporter's stored summary where its items
// still apply, the factory's own recommendation for everything else.
type validateSummary struct {
	Headline   string
	WhatFailed []string
	Why        string
	Certainty  string
	Next       validateSummaryLine
	Items      []validateSummaryLine
	Reruns     []agentresult.ReportRerun
	// Agent says the reporter's summary is in force.
	Agent bool
}

// buildValidateSummary combines the stored summary with the current state.
// An agent item whose action no longer applies is replaced by the factory's
// recommendation for that item, so every command shown is valid now.
func buildValidateSummary(st ValidateState) validateSummary {
	sum := validateSummary{}
	items := st.reportItems()
	fallback := map[string]validateSummaryLine{}
	var order []string
	for i := range items {
		line := fallbackItemLine(st, &items[i])
		fallback[line.Ref] = line
		order = append(order, line.Ref)
	}
	agent := map[string]validateSummaryLine{}
	if s := st.Summary; s != nil && !st.closed() {
		sum.Agent = true
		sum.Headline, sum.WhatFailed, sum.Why, sum.Certainty, sum.Reruns = s.Headline, s.WhatFailed, s.Why, s.Certainty, s.Reruns
		for _, it := range s.Items {
			if line, ok := agentItemLine(st, it); ok {
				agent[it.Ref] = line
			}
		}
		if s.Next != nil {
			if line, ok := agentItemLine(st, *s.Next); ok {
				sum.Next = line
			}
		}
	}
	if sum.Next.Text == "" {
		sum.Next = fallbackNext(st, fallback, order)
	}
	if !sum.Agent {
		sum.Headline, sum.WhatFailed, sum.Why, sum.Certainty = fallbackHeadline(st), fallbackFailed(st), fallbackWhy(st), ""
		if sum.Why != "" {
			sum.Certainty = "unsure"
			if st.Handback != nil && len(st.Failures) > 0 && strings.TrimSpace(st.Failures[0].Cause) != "" && !strings.EqualFold(strings.TrimSpace(st.Failures[0].Cause), agentresult.RepairUnknownCause) {
				sum.Certainty = "likely"
			}
		}
	}
	for _, ref := range order {
		if ref == sum.Next.Ref {
			continue
		}
		if line, ok := agent[ref]; ok {
			sum.Items = append(sum.Items, line)
			continue
		}
		sum.Items = append(sum.Items, fallback[ref])
	}
	return sum
}

// agentItemLine renders one reporter item when its action still applies.
func agentItemLine(st ValidateState, it agentresult.ReportItem) (validateSummaryLine, bool) {
	var item *validateReportItem
	if it.Ref != "" {
		found, ok := st.reportItem(it.Ref)
		if !ok {
			return validateSummaryLine{}, false
		}
		item = &found
	}
	command, ok := st.reportCommand(item, it.Action)
	if !ok {
		return validateSummaryLine{}, false
	}
	return validateSummaryLine{Ref: it.Ref, Text: it.Recommendation, Command: command, Action: it.Action, Diagnosis: it.Diagnosis, Evidence: it.Evidence, Agent: true}, true
}

// fallbackItemLine is the factory's own recommendation for one item.
func fallbackItemLine(st ValidateState, item *validateReportItem) validateSummaryLine {
	line := validateSummaryLine{Ref: item.Ref}
	pick := func(action, text string) {
		if command, ok := st.reportCommand(item, action); ok && line.Command == "" {
			line.Action, line.Command, line.Text = action, command, text
		}
	}
	switch {
	case item.Failure != nil:
		f := item.Failure
		what := firstNonBlank(strings.TrimSpace(f.Objective), strings.TrimSpace(f.Cause), strings.TrimSpace(f.Reason), "find out why it fails")
		pick(agentresult.ReportActionSendBack, "Send it back so Implement fixes it: "+clipRunes(oneLine(what), 140))
		pick(agentresult.ReportActionRerun, "Push a fix to the feature branch, then run Validate again: "+clipRunes(oneLine(what), 140))
	case item.Finding != nil:
		f := item.Finding
		summary := clipRunes(oneLine(firstNonBlank(f.Defect, f.Summary, "the "+f.Name+" finding")), 140)
		switch {
		case f.State == findingAwaitingCheck || f.State == findingOwnerFailed && f.checkable():
			pick(agentresult.ReportActionCheckPass, "Check it yourself, then record what you saw: "+summary)
		case f.Choice != nil && f.Choice.Choice == store.ChoiceIssue:
			pick("issue-"+f.Choice.IssueKind, "Creating the issue you chose did not finish; retry it.")
		case f.Optional:
			pick(agentresult.ReportActionDefer, "Optional suggestion; defer it unless you want it now: "+summary)
		default:
			pick(agentresult.ReportActionRepair, "Let the repair agent fix it: "+summary)
		}
		pick(agentresult.ReportActionSendBack, "Send it back to Implement: "+summary)
	case item.Candidate != nil:
		pick(agentresult.ReportActionRepair, "A repair was kept aside for you; review it before you decide.")
	}
	return line
}

// fallbackNext is the factory's own one next step.
func fallbackNext(st ValidateState, items map[string]validateSummaryLine, order []string) validateSummaryLine {
	global := func(action, text string) validateSummaryLine {
		command, _ := st.reportCommand(nil, action)
		return validateSummaryLine{Text: text, Command: command, Action: action}
	}
	roles, _ := st.notReviewed()
	switch {
	case st.Reimplementing:
		return validateSummaryLine{Text: "Nothing: the work went back to Implement on the same feature branch. Validate runs again on the new head when Implement finishes."}
	case effectDone(st.Effects.Close):
		return validateSummaryLine{Text: "Nothing. Delivery is complete."}
	case st.mergeConflicted():
		return validateSummaryLine{Text: fmt.Sprintf("Pull request #%d conflicts with its target branch. Let Implement merge the target branch and resolve the conflict, then validate again.", decodeEffectResult(st.Effects.PR).Number), Command: ValidateSyncCommand}
	case !st.Assessed:
		return validateSummaryLine{Text: "Wait for the assessment of this commit; the report updates by itself."}
	case len(roles) > 0 && suiteCoverageSatisfied(st.Suite):
		return global(agentresult.ReportActionRerun, "Run the missing reviews again; the passed suite is reused.")
	}
	for _, ref := range order {
		if line := items[ref]; line.Command != "" {
			return line
		}
	}
	if st.Ready {
		return global(agentresult.ReportActionShip, "Everything passed. Ship it with a short reason.")
	}
	return validateSummaryLine{Text: "Wait: an issue you chose is still being created; the report updates by itself."}
}

func fallbackHeadline(st ValidateState) string {
	owner := 0
	for _, f := range st.Findings {
		if validateOwnerItem(st, f) {
			owner++
		}
	}
	roles, _ := st.notReviewed()
	switch {
	case st.Reimplementing:
		return "Sent back to Implement."
	case effectDone(st.Effects.Close):
		return "Delivered: the pull request is merged and the issues are closed."
	case st.Suite.Outcome == store.SuiteBlocked:
		return "The suite could not give a trustworthy answer on this commit."
	case !suiteCoverageSatisfied(st.Suite):
		return fmt.Sprintf("The suite failed on this commit: %s.", plural(len(st.Failures), "failing command", "failing commands"))
	case !st.Assessed:
		return "The suite passed; the review is not recorded yet."
	case len(roles) > 0:
		return "The suite passed, but some reviews are missing."
	case owner > 0:
		return fmt.Sprintf("The suite passed; %s.", plural(owner, "item needs you", "items need you"))
	case len(st.blockingIDs()) > 0:
		return "The suite passed, but a blocking finding remains."
	case st.Ready:
		return "Everything passed. Ready to ship."
	}
	return "The suite passed; delivery waits for an issue you chose."
}

func fallbackFailed(st ValidateState) []string {
	var out []string
	for i, f := range st.Failures {
		if i == agentresult.MaxReportFailedLines {
			break
		}
		line := "`" + clipRunes(oneLine(f.Command), 80) + "`"
		if tests := failingTestNames(f.OutputTail); len(tests) > 0 {
			line += ": " + clipRunes(strings.Join(tests, ", "), 120) + " failed"
		} else if why := oneLine(firstNonBlank(f.Reason, f.Cause)); why != "" {
			line += ": " + clipRunes(why, 120)
		}
		out = append(out, line)
	}
	return out
}

// failingTestPattern finds failing test names in Go, pytest and xUnit-style
// output: "--- FAIL: TestX", "FAILED tests/a.py::test_b", "Failed TestY".
var failingTestPattern = regexp.MustCompile(`(?m)(?:--- FAIL: |^FAILED |^\s*Failed )([A-Za-z_][\w./:\[\]-]*)`)

// failingTestNames lists up to three distinct failing tests an output tail names.
func failingTestNames(tail string) []string {
	var out []string
	for _, m := range failingTestPattern.FindAllStringSubmatch(tail, -1) {
		if !slices.Contains(out, m[1]) {
			out = append(out, m[1])
		}
		if len(out) == 3 {
			break
		}
	}
	return out
}

func fallbackWhy(st ValidateState) string {
	if len(st.Failures) == 0 {
		return ""
	}
	for _, f := range st.Failures {
		if c := strings.TrimSpace(f.Cause); c != "" && !strings.EqualFold(c, agentresult.RepairUnknownCause) {
			return clipRunes(oneLine(c), 240)
		}
	}
	return "Not known yet: no diagnosis is recorded for the failures."
}

// commands lists every command the summary shows, in order; the last hidden
// items did not fit the summary, so their commands stay in the reference.
func (s validateSummary) commands(hidden int) []string {
	var out []string
	shown := s.Items[:len(s.Items)-min(hidden, len(s.Items))]
	for _, l := range append([]validateSummaryLine{s.Next}, shown...) {
		if l.Command != "" {
			out = append(out, l.Command)
		}
	}
	return out
}

// render writes the summary. Untrusted text (the reporter's and the
// reviewers') is escaped for the destination; commands are factory text.
func (s validateSummary) render(st ValidateState, onLinear bool, light, notice string) (string, []validateSummaryLine) {
	var b strings.Builder
	header := fmt.Sprintf("%s **Validate report** · commit `%s` · suite %s · revision `%s`", light, short(st.Commit), st.Suite.Outcome, st.Revision)
	if st.Advice > 1 {
		header += fmt.Sprintf(" · advice %d", st.Advice)
	}
	b.WriteString(header + "\n\n")
	used := 1
	text := func(v string) string { return implementInline(onLinear, v) }
	headline := s.Headline
	if !s.Agent {
		// The factory's own headline and failure lines are factory text with
		// code spans; only their untrusted parts were clipped from records.
		b.WriteString(planTableCellFor(onLinear, headline) + "\n\n")
	} else {
		b.WriteString(text(headline) + "\n\n")
	}
	used++
	switch {
	case strings.TrimSpace(notice) != "" && st.Reimplementing:
		b.WriteString("> **Sent back to Implement:** " + planTableCellFor(onLinear, notice) + "\n\n")
		used++
	case strings.TrimSpace(notice) != "":
		b.WriteString("> **Your last command changed nothing:** " + planTableCellFor(onLinear, notice) + "\n\n")
		used++
	}
	if len(s.WhatFailed) > 0 {
		b.WriteString("**What failed:**\n")
		for _, line := range s.WhatFailed {
			if s.Agent {
				line = text(line)
			} else {
				line = validateFactoryLine(onLinear, line)
			}
			b.WriteString("- " + line + "\n")
		}
		b.WriteString("\n")
		used += 1 + len(s.WhatFailed)
	}
	if s.Why != "" {
		why := s.Why
		if s.Agent {
			why = text(why)
		} else {
			why = planTableCellFor(onLinear, why)
		}
		if s.Certainty != "" {
			why += " (" + s.Certainty + ")"
		}
		b.WriteString("**Why:** " + why + "\n\n")
		used++
	}
	b.WriteString("**Next:** " + summaryLineText(onLinear, s.Next, false) + "\n")
	used++
	budget := max(validateSummaryLines-used, 2)
	var rest []validateSummaryLine
	if len(s.Items) > 0 {
		b.WriteString("\n")
		shown := s.Items
		if len(shown) > budget {
			shown, rest = s.Items[:budget-1], s.Items[budget-1:]
		}
		for _, l := range shown {
			b.WriteString("- " + summaryLineText(onLinear, l, true) + "\n")
		}
		if len(rest) > 0 {
			fmt.Fprintf(&b, "- %s in the details.\n", plural(len(rest), "more item is", "more items are"))
		}
	}
	return b.String(), rest
}

// summaryLineText writes one recommendation with its command.
func summaryLineText(onLinear bool, l validateSummaryLine, withRef bool) string {
	var b strings.Builder
	if withRef && l.Ref != "" {
		b.WriteString("`" + l.Ref + "`: ")
	}
	if l.Agent {
		b.WriteString(implementInline(onLinear, l.Text))
	} else {
		b.WriteString(planTableCellFor(onLinear, l.Text))
	}
	if l.Command != "" {
		b.WriteString(" `" + l.Command + "`")
	}
	return b.String()
}

// validateFactoryLine escapes a factory line whose leading code span holds a
// recorded command: the span stays code, the rest is escaped as text.
func validateFactoryLine(onLinear bool, line string) string {
	if strings.HasPrefix(line, "`") {
		if end := strings.Index(line[1:], "`"); end >= 0 {
			code := line[1 : end+1]
			rest := line[end+2:]
			return codeSpanFor(code) + planTableCellForKeep(onLinear, rest)
		}
	}
	return planTableCellFor(onLinear, line)
}

// codeSpanFor writes text as one inert code span.
func codeSpanFor(text string) string {
	fence := "`"
	for strings.Contains(text, fence) {
		fence += "`"
	}
	if strings.HasPrefix(text, "`") || strings.HasSuffix(text, "`") {
		return fence + " " + text + " " + fence
	}
	return fence + text + fence
}

// planTableCellForKeep escapes text but keeps an empty value empty.
func planTableCellForKeep(onLinear bool, text string) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	lead := ""
	if strings.HasPrefix(text, ": ") {
		lead, text = ": ", text[2:]
	}
	return lead + planTableCellFor(onLinear, text)
}
