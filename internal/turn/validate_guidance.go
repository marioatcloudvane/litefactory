package turn

import (
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/store"
)

// validateDecisionItems keeps each command and its consequence together. Item
// boundaries, rather than arbitrary Markdown lines, are the overflow boundary.
func validateDecisionItems(st ValidateState) []string {
	var items []string
	for _, f := range st.Findings {
		var b strings.Builder
		fmt.Fprintf(&b, "**%s · `%s`**", planTableCell(f.Story), f.ID)
		if f.Summary != "" {
			b.WriteString(": " + escapeImplementText(shortenFindingText(f.Summary, validateStoryTextFloor)))
		}
		b.WriteString("\n")
		switch {
		case f.State == findingNotReviewed:
			b.WriteString("This item has no usable review and takes no owner check or finding choice. Use the parent report's shared missing-review rerun: it reuses the passed suite and repeats the missing reviews.")
		case f.State == findingAwaitingCheck || f.State == findingOwnerFailed && f.checkable():
			fmt.Fprintf(&b, "`%s %s %s pass -- <what you checked>` or `%s %s %s fail -- <what you saw>`\n", ValidateCheckCommand, st.Revision, f.ID, ValidateCheckCommand, st.Revision, f.ID)
			b.WriteString("The owner supplies evidence. Pass settles this check on the tested commit; fail keeps it blocking and exposes its finding choices. Neither verdict permits push, merge or close.")
			if f.needsChoice() {
				b.WriteString("\n" + validateChoiceGuidance(st, f))
			}
		case f.needsChoice() && f.Choice == nil:
			b.WriteString(validateChoiceGuidance(st, f))
		case f.Choice != nil:
			fmt.Fprintf(&b, "Current choice: %s. ", choiceWords(f))
			if f.Choice.Choice == store.ChoiceIssue {
				if e, ok := st.Effects.Issues[f.ID]; ok && e.State == store.EffectFailed {
					fmt.Fprintf(&b, "Issue creation failed; `%s %s %s issue %s` retries this choice. ", ValidateFindingCommand, st.Revision, f.ID, f.Choice.IssueKind)
				} else if ok && e.State == store.EffectUncertain {
					fmt.Fprintf(&b, "Issue creation is uncertain; `%s %s %s issue %s` retries the retained choice. The retry reconciles the existing intent and marker before creating anything; it does not blindly duplicate the issue, and uncertainty may remain. ", ValidateFindingCommand, st.Revision, f.ID, f.Choice.IssueKind)
				}
			}
			if validateItemBlocks(f) {
				b.WriteString("The choice does not clear this blocker; a new tested snapshot or eligible owner verdict remains necessary.")
			}
		default:
			continue
		}
		items = append(items, b.String())
	}
	return items
}

func validateChoiceGuidance(st ValidateState, f ValidateFinding) string {
	var b strings.Builder
	fmt.Fprintf(&b, "`%s %s %s repair` starts a repair only after every blocking finding has a choice. `%s %s %s issue bug` or `%s %s %s issue story` creates a follow-up issue. ", ValidateFindingCommand, st.Revision, f.ID, ValidateFindingCommand, st.Revision, f.ID, ValidateFindingCommand, st.Revision, f.ID)
	if f.Optional {
		fmt.Fprintf(&b, "`%s %s %s defer` leaves this optional suggestion deferred. ", ValidateFindingCommand, st.Revision, f.ID)
	}
	if f.acceptable() {
		fmt.Fprintf(&b, "`%s %s %s accept -- <reason>` records the owner's reason for accepting this deliberate deviation. ", ValidateFindingCommand, st.Revision, f.ID)
	}
	if validateItemBlocks(f) {
		b.WriteString("Repair and issue creation do not clear this blocker: a new tested snapshot or an eligible owner verdict is still required. ")
	}
	b.WriteString("These choices grant no delivery permission.")
	return b.String()
}

func validateGuidanceHeader(st ValidateState) string {
	return fmt.Sprintf("\n**Complete decision guidance** · revision `%s` · tested `%s` · displayed delivery `%s`\n\nGive each command as a separate comment on the parent issue. Only the permitted owner can decide. Settle checks before finding choices; repair waits for all blocking choices. Individual push, merge and close commands authorize only their named step, in that order. The explicit reasoned shipping command authorizes the whole existing sequence and its displayed exception; it stops on a failed or uncertain step and retains factual test and review results.\n\n", st.Revision, st.Commit, st.DeliveryCommit)
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
