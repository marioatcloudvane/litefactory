package turn

import (
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/store"
)

// validateShownCommands tracks the commands already written, so the report
// shows each exact command once: the summary first, then the reference.
type validateShownCommands map[string]bool

// show writes a command as code the first time and refers back to it later.
func (s validateShownCommands) show(command string) string {
	if s[command] {
		return "the command above"
	}
	s[command] = true
	return "`" + command + "`"
}

// validateDecisionItems keeps each command and its consequence together. Item
// boundaries, rather than arbitrary Markdown lines, are the overflow boundary.
func validateDecisionItems(st ValidateState) []string {
	return validateDecisionItemsFor(false, st)
}

// validateDecisionItemsFor escapes the untrusted story key and summary with
// the destination's inline contract; Linear writes no numeric entities.
func validateDecisionItemsFor(onLinear bool, st ValidateState) []string {
	return validateFindingReference(onLinear, st, validateShownCommands{})
}

// validateFindingReference is one reference entry per open finding with every
// command it takes now. The shared consequences of checks and choices are in
// the reference header, once.
func validateFindingReference(onLinear bool, st ValidateState, shown validateShownCommands) []string {
	var items []string
	for _, f := range st.Findings {
		var b strings.Builder
		fmt.Fprintf(&b, "**%s · `%s`**", planTableCellFor(onLinear, f.Story), f.ID)
		if f.Summary != "" {
			b.WriteString(": " + implementInline(onLinear, shortenFindingText(f.Summary, validateStoryTextFloor)))
		}
		b.WriteString("\n")
		switch {
		case f.State == findingNotReviewed:
			// It takes no check or choice; the missing-review entry and the
			// blockers in the facts cover it once.
			continue
		case f.State == findingAwaitingCheck || f.State == findingOwnerFailed && f.checkable():
			fmt.Fprintf(&b, "Check: %s or %s", shown.show(fmt.Sprintf("%s %s %s pass -- <what you checked>", ValidateCheckCommand, st.Revision, f.ID)), shown.show(fmt.Sprintf("%s %s %s fail -- <what you saw>", ValidateCheckCommand, st.Revision, f.ID)))
			if f.needsChoice() {
				b.WriteString("\n" + validateChoiceGuidanceShown(st, f, shown))
			}
		case f.needsChoice() && f.Choice == nil:
			b.WriteString(validateChoiceGuidanceShown(st, f, shown))
		case f.Choice != nil:
			fmt.Fprintf(&b, "Current choice: %s.", choiceWords(f))
			if f.Choice.Choice == store.ChoiceIssue {
				retry := shown.show(fmt.Sprintf("%s %s %s issue %s", ValidateFindingCommand, st.Revision, f.ID, f.Choice.IssueKind))
				if e, ok := st.Effects.Issues[f.ID]; ok && e.State == store.EffectFailed {
					fmt.Fprintf(&b, " Issue creation failed; %s retries this choice.", retry)
				} else if ok && e.State == store.EffectUncertain {
					fmt.Fprintf(&b, " Issue creation is uncertain; %s retries the retained choice after reconciling the existing intent and marker, without blindly duplicating the issue.", retry)
				}
			}
			if validateItemBlocks(f) {
				b.WriteString(" The choice does not clear this blocker.")
			}
		default:
			continue
		}
		if row, ok := st.FactoryBugs[f.ID]; ok {
			b.WriteString("\n" + factoryBugState(row))
		}
		items = append(items, b.String())
	}
	return items
}

// validateChoiceGuidanceShown lists a finding's choice commands.
func validateChoiceGuidanceShown(st ValidateState, f ValidateFinding, shown validateShownCommands) string {
	cmd := func(choice string) string {
		return shown.show(fmt.Sprintf("%s %s %s %s", ValidateFindingCommand, st.Revision, f.ID, choice))
	}
	choices := []string{"repair " + cmd("repair"), "issue " + cmd("issue bug") + " or " + cmd("issue story")}
	if f.Optional {
		choices = append(choices, "defer "+cmd("defer"))
	}
	if f.acceptable() {
		choices = append(choices, "accept a deliberate deviation "+cmd("accept -- <reason>"))
	}
	text := "Choose: " + strings.Join(choices, ", ") + "."
	if validateItemBlocks(f) {
		text += " Repair and an issue do not clear this blocker."
	}
	return text
}

// validatePartConsequences states, once per comment, what checks and finding
// choices do. Each numbered guidance part stands alone, so each carries it.
const validatePartConsequences = "Checks: the owner supplies evidence; pass settles a check on the tested commit, fail keeps it blocking and opens its finding choices. Finding choices: repair starts once every blocking finding has a choice, an issue files a follow-up, defer leaves an optional suggestion for later and accept records your reason for a deliberate deviation; repair and issues never clear a blocker, only a new tested snapshot or an eligible owner verdict does. No check or choice permits push, merge or close."

// validateGuidanceHeader opens the command reference with the one statement of
// how commands work and what they authorize.
func validateGuidanceHeader(st ValidateState) string {
	return fmt.Sprintf("\n**Command reference** · revision `%s` · tested `%s` · displayed delivery `%s`\n\n"+
		"Give each command as its own comment on the parent issue; only the permitted owner can decide. %s Push, merge and close each authorize only their own step, in that order. %s\n\n",
		st.Revision, st.Commit, st.DeliveryCommit, validatePartConsequences, validateShipConsequence)
}

// validateCommandReference is every command the owner can give now, each
// written once: commands the summary shows are referred back to.
func validateCommandReference(onLinear bool, st ValidateState, sum validateSummary, closeSet []int, hidden int) []string {
	shown := validateShownCommands{}
	for _, c := range sum.commands(hidden) {
		shown[c] = true
	}
	var items []string
	closed := st.closed()
	if !closed && st.Assessed {
		label := "**Ship despite the shown blockers:** "
		if st.Ready {
			label = "**Ship (push, pull request, merge, close):** "
		}
		items = append(items, label+shown.show(fmt.Sprintf("%s %s -- <reason>", ValidateShipCommand, st.shipReference()))+".")
		if st.Reimplement {
			ref := store.OwnerDecisionReference(st.DecisionTask, ValidateReturnCommand, validateDecisionTarget(st), st.Revision)
			items = append(items, "**Return reviewed work to Implement with your explanation:** "+shown.show(fmt.Sprintf("%s %s -- <requested change>", ValidateReturnCommand, ref))+".")
		}
	}
	if st.implementLine() != "" {
		items = append(items, fmt.Sprintf("**Send back to Implement:** %s sends the blocking findings or the failed suite back on the same feature branch; Validate runs again on the new head. Or push a fix yourself and comment %s.", shown.show(ValidateImplementCommand+" "+st.Revision), shown.show(ValidateStartCommand)))
	}
	if roles, _ := st.notReviewed(); len(roles) > 0 && suiteCoverageSatisfied(st.Suite) {
		items = append(items, fmt.Sprintf("**Missing reviews:** comment %s on commit `%s`; the passed suite is reused and only the reviews run again.", shown.show(ValidateStartCommand), short(st.Commit)))
	}
	if st.mergeConflicted() {
		items = append(items, fmt.Sprintf("**Merge conflict:** comment %s; Implement merges the target branch and its sync-resolver resolves the conflict. Then comment %s.", shown.show(ValidateSyncCommand), shown.show(ValidateStartCommand)))
	}
	if !closed {
		if c := st.deliveryCommandText(closeSet, shown); c != "" {
			items = append(items, "**Next delivery step:** "+c)
		}
	}
	for _, c := range st.Candidates {
		var b strings.Builder
		fmt.Fprintf(&b, "**Retained repair `%s`:** inspect %s", c.ID, shown.show(fmt.Sprintf("%s %s %s %s inspect", ValidateRepairCommand, st.Revision, c.ID, c.ReviewDigest)))
		if c.Resolution == "pending" {
			fmt.Fprintf(&b, ", discard %s", shown.show(fmt.Sprintf("%s %s %s %s discard", ValidateRepairCommand, st.Revision, c.ID, c.ReviewDigest)))
			if c.Eligible && c.ReviewComplete && st.RepairAdoption {
				fmt.Fprintf(&b, ", or accept %s, which adopts this exact tree for fresh suite and reviewer checks.", shown.show(fmt.Sprintf("%s %s %s %s accept", ValidateRepairCommand, st.Revision, c.ID, c.ReviewDigest)))
			} else {
				b.WriteString(". Acceptance is unavailable: complete eligible analysis, unchanged review delivery and a graph with adoption are required; frozen criteria remain protected.")
			}
		}
		items = append(items, b.String())
	}
	for i := range st.Failures {
		ref := validateFailureRef(i)
		row, filed := st.FactoryBugs[ref]
		proposed := false
		for _, l := range append([]validateSummaryLine{sum.Next}, sum.Items...) {
			proposed = proposed || l.Ref == ref && l.Action == agentresult.ReportActionFactoryBug
		}
		switch {
		case filed:
			items = append(items, fmt.Sprintf("**`%s` factory Bug:** %s", ref, factoryBugState(row)))
		case proposed:
			if command, ok := st.reportCommand(&validateReportItem{Ref: ref, Actions: []string{agentresult.ReportActionFactoryBug}}, agentresult.ReportActionFactoryBug); ok {
				items = append(items, fmt.Sprintf("**`%s` factory Bug:** %s files one upstream factory Bug through the log-analysis filing path. Nothing is filed without it.", ref, shown.show(command)))
			}
		}
	}
	items = append(items, validateFindingReference(onLinear, st, shown)...)
	// A recommendation that did not fit the summary keeps its exact command
	// here when no entry above already holds it.
	for _, l := range sum.Items[len(sum.Items)-min(hidden, len(sum.Items)):] {
		if l.Command != "" && !shown[l.Command] {
			items = append(items, fmt.Sprintf("**`%s`:** %s", l.Ref, shown.show(l.Command)))
		}
	}
	return items
}

// factoryBugState is the recorded state of one factory Bug filing.
func factoryBugState(row store.ValidateFactoryBug) string {
	switch row.State {
	case store.FactoryBugFiled:
		return "Factory Bug filed: " + row.Reference + "."
	case store.FactoryBugFailed:
		return "Factory Bug filing failed (" + clipText(row.Error, 160) + "); repeat the command to retry."
	}
	return "Factory Bug filing is under way."
}

// deliveryCommandText is the next delivery command with what it authorizes.
func (s ValidateState) deliveryCommandText(closeSet []int, shown validateShownCommands) string {
	rev := s.Revision
	switch {
	case !s.Ready:
	case !effectDone(s.Effects.PR) || !effectDone(s.Effects.Push):
		return shown.show(fmt.Sprintf("%s %s", ValidatePushCommand, rev)) + " pushes the tested commit and opens or recovers its pull request."
	case !effectDone(s.Effects.Merge):
		r := decodeEffectResult(s.Effects.PR)
		return shown.show(fmt.Sprintf("%s %s %d %s", ValidateMergeCommand, rev, r.Number, r.Head)) + " merges only this pull request at this exact head."
	case !effectDone(s.Effects.Close):
		return shown.show(fmt.Sprintf("%s %s %s", ValidateCloseCommand, rev, validateNumbers(closeSet))) + " closes exactly the accepted issues; already closed issues are not repeated."
	}
	return ""
}

func packValidateDecisionItems(items []string, budget int) ([]string, error) {
	var parts []string
	var b strings.Builder
	for _, item := range items {
		if len(item) > budget {
			return nil, fmt.Errorf("Validate decision item exceeds the intact-item publication budget")
		}
		if b.Len() > 0 && b.Len()+len(item)+2 > budget {
			parts = append(parts, b.String())
			b.Reset()
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(item)
	}
	if b.Len() > 0 {
		parts = append(parts, b.String())
	}
	return parts, nil
}
