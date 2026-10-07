package turn

import (
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
)

// renderValidateReport is the one report the owner reads. It has one row per
// work item (suite, repair rounds, each reviewer, manual checks, findings,
// delivery), the findings with their stable ids and evidence, the handback text,
// what is open and the exact next commands. It reads records only.
func renderValidateReport(st ValidateState, target *ValidateDeliveryTarget, notice string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**Validate report** · tested commit `%s` · revision `%s`\n\n", short(st.Commit), st.Revision)
	b.WriteString("Nothing is pushed, merged or closed until you give the command for it.\n\n")
	if strings.TrimSpace(notice) != "" {
		b.WriteString("> **Your last command changed nothing:** " + planTableCell(notice) + "\n\n")
	}
	b.WriteString("| Work item | State |\n|---|---|\n")
	row := func(item, state string) { fmt.Fprintf(&b, "| %s | %s |\n", planTableCell(item), planTableCell(state)) }
	suite := st.Suite.Outcome
	if st.Suite.RunID != 0 {
		suite += fmt.Sprintf(" (suite run %d)", st.Suite.RunID)
	}
	if st.Suite.Reason != "" {
		suite += ": " + clipText(st.Suite.Reason, 200)
	}
	row("Full suite", suite)
	row("Repair rounds", repairRoundsText(st.Rounds))
	for _, role := range agent.ReviewRoles {
		row("Reviewer: "+role, reviewerText(st.Reviews, role))
	}
	row("Manual checks", manualChecksText(st))
	row("Findings", findingsText(st))
	row("Delivery", deliveryText(st))

	b.WriteString("\n**Findings**\n\n")
	any := false
	for _, f := range st.Findings {
		if f.State == findingSatisfied {
			continue
		}
		any = true
		fmt.Fprintf(&b, "- `%s` — **%s** (%s%s)", f.ID, f.State, kindWord(f), choiceSuffix(f))
		if f.Summary != "" {
			b.WriteString(": " + planTableCell(f.Summary))
		}
		if len(f.Checks) > 0 {
			b.WriteString(" Evidence: " + strings.Join(f.Checks, ", ") + ".")
		}
		if f.Verdict != nil {
			fmt.Fprintf(&b, " Your check (%s): %s", f.Verdict.Verdict, planTableCell(clipText(f.Verdict.Evidence, 300)))
		}
		if f.State == findingAwaitingCheck {
			b.WriteString(" Reviewer's instructions: see the finding above, then record pass or fail with evidence.")
		}
		b.WriteString("\n")
	}
	if !any {
		b.WriteString("No finding is open.\n")
	}

	if st.Handback != nil {
		b.WriteString("\n**Handback to Implement**\n\n")
		b.WriteString(strings.TrimSpace(st.Handback.OwnerInstruction) + "\n")
	}

	b.WriteString("\n**Open**\n\n")
	if len(st.Open) == 0 {
		b.WriteString("Nothing is open. Delivery can go ahead on your command.\n")
	}
	for _, o := range st.Open {
		b.WriteString("- " + o + "\n")
	}

	b.WriteString("\n**Next commands**\n\n")
	var closeSet []int
	if target != nil {
		closeSet = target.CloseSet()
	}
	commands := st.NextCommands(closeSet)
	if len(commands) == 0 {
		b.WriteString("None now. ")
		if !st.Ready {
			b.WriteString("A blocking finding clears only on a new tested snapshot: hand it back to Implement and run Validate again.")
		}
		b.WriteString("\n")
	}
	for _, c := range commands {
		b.WriteString("- " + c + "\n")
	}
	return b.String()
}

func kindWord(f ValidateFinding) string {
	switch {
	case f.Criterion:
		return "criterion, blocking"
	case f.Blocking:
		return "review, blocking"
	}
	return "review, optional"
}

func choiceSuffix(f ValidateFinding) string {
	if f.Choice == nil {
		return ""
	}
	if f.Choice.Choice == store.ChoiceIssue {
		return ", your choice: issue " + f.Choice.IssueKind
	}
	return ", your choice: " + f.Choice.Choice
}

func repairRoundsText(rounds []store.RepairRound) string {
	if len(rounds) == 0 {
		return "none needed"
	}
	var parts []string
	for _, r := range rounds {
		parts = append(parts, fmt.Sprintf("round %d: %s", r.Round, r.State))
	}
	return strings.Join(parts, "; ")
}

func reviewerText(reviews []store.ValidateReview, role string) string {
	for _, r := range reviews {
		if r.Role == role {
			if r.Reason != "" {
				return r.State + ": " + clipText(r.Reason, 160)
			}
			return r.State
		}
	}
	return "not run"
}

func manualChecksText(st ValidateState) string {
	var awaiting, passed, failed int
	for _, f := range st.Findings {
		switch f.State {
		case findingAwaitingCheck:
			awaiting++
		case findingOwnerPassed:
			passed++
		case findingOwnerFailed:
			failed++
		}
	}
	if awaiting+passed+failed == 0 {
		return "none needed"
	}
	return fmt.Sprintf("Awaiting owner check: %d · passed: %d · failed: %d", awaiting, passed, failed)
}

func findingsText(st ValidateState) string {
	var blocking, optional, choiceMade int
	for _, f := range st.Findings {
		if !f.needsChoice() {
			continue
		}
		if f.Blocking {
			blocking++
		} else {
			optional++
		}
		if f.Choice != nil {
			choiceMade++
		}
	}
	return fmt.Sprintf("blocking open: %d · optional open: %d · choices recorded: %d", blocking, optional, choiceMade)
}

func effectText(name string, e *store.ValidateEffect) string {
	if e == nil {
		return name + ": not started"
	}
	text := name + ": " + e.State
	if e.State == store.EffectFailed || e.State == store.EffectUncertain {
		text += " (" + clipText(e.Error, 160) + "; repeat your command to retry)"
	}
	return text
}

func deliveryText(st ValidateState) string {
	parts := []string{effectText("push", st.Effects.Push), effectText("pull request", st.Effects.PR), effectText("merge", st.Effects.Merge), effectText("close", st.Effects.Close)}
	for _, f := range st.Findings {
		if e, ok := st.Effects.Issues[f.ID]; ok {
			r := decodeEffectResult(&e)
			text := "issue for " + f.ID + ": " + e.State
			if r.Issue > 0 {
				text += fmt.Sprintf(" (#%d)", r.Issue)
			} else if e.Error != "" {
				text += " (" + clipText(e.Error, 120) + "; repeat your command to retry)"
			}
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "; ")
}
