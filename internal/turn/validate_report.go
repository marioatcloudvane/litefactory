package turn

import (
	"fmt"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
)

// The lights of the Validate report: one in the header for the worst overall
// state and one at the start of each story row for the worst state of that
// story. Cells stay plain text.
const (
	lightGreen  = "🟢"
	lightYellow = "🟡"
	lightRed    = "🔴"
)

// renderValidateReport is the one report the owner reads on the parent: one
// header line with the overall light, the tested commit, the suite and the
// revision; one line of delivery state; one table row per story; and one Next
// line. Per-item detail and command templates live on the story sub-issues. A
// failed suite replaces the table with the suite and repair state. It reads
// records only.
func renderValidateReport(st ValidateState, target *ValidateDeliveryTarget, notice string) string {
	var b strings.Builder
	rows := validateStoryRows(st, target)
	fmt.Fprintf(&b, "%s **Validate report** · commit `%s` · suite %s · revision `%s`\n\n", validateOverallLight(st, rows), short(st.Commit), st.Suite.Outcome, st.Revision)
	b.WriteString("Delivery: " + deliveryText(st, target) + "\n\n")
	if st.DeliveryCommit != "" {
		fmt.Fprintf(&b, "Displayed delivery commit: `%s`. Test evidence remains bound to `%s`.\n\n", st.DeliveryCommit, st.Commit)
	}
	for _, action := range []string{ValidateShipCommand, ValidateReturnCommand} {
		if !st.Assessed || st.Reimplementing || effectDone(st.Effects.Close) {
			continue
		}
		if action == ValidateReturnCommand && (!st.Reimplement || !st.Assessed || st.Reimplementing) {
			continue
		}
		ref := store.OwnerDecisionReference("", action, validateDecisionTarget(st), st.Revision)
		// The publication hook supplies the task-bound reference below when known.
		if st.DecisionTask != "" {
			ref = store.OwnerDecisionReference(st.DecisionTask, action, validateDecisionTarget(st), st.Revision)
		}
		if action == ValidateShipCommand {
			fmt.Fprintf(&b, "Ship this displayed commit despite its displayed blockers: `%s %s -- <reason>`. %s\n\n", action, ref, validateShipConsequence)
		} else {
			fmt.Fprintf(&b, "Return reviewed work to Implement: `%s %s -- <requested change>`.\n\n", action, ref)
		}
	}
	if roles, _ := st.notReviewed(); len(roles) > 0 && st.Suite.Outcome == store.SuitePassed {
		b.WriteString("To rerun the missing reviews: " + validateRetryStep(st.Commit) + "\n\n")
	}
	if legacy := st.implementLine(); legacy != "" {
		b.WriteString("Legacy repair option: " + legacy + "\n\n")
	}
	switch {
	case strings.TrimSpace(notice) != "" && st.Reimplementing:
		b.WriteString("> **Sent back to Implement:** " + planTableCell(notice) + "\n\n")
	case strings.TrimSpace(notice) != "":
		b.WriteString("> **Your last command changed nothing:** " + planTableCell(notice) + "\n\n")
	}
	if validateOverallLight(st, rows) == lightRed {
		b.WriteString("> [!CAUTION]\n> Something blocks delivery: see the red rows below.\n\n")
	}
	switch {
	case st.Suite.Outcome != store.SuitePassed:
		suite := "The full suite " + st.Suite.Outcome
		if st.Suite.RunID != 0 {
			suite += fmt.Sprintf(" (suite run %d)", st.Suite.RunID)
		}
		if st.Suite.Reason != "" {
			suite += ": " + clipText(st.Suite.Reason, 300)
		}
		b.WriteString(suite + "\n\nRepair rounds: " + repairRoundsText(st.Rounds) + "\n")
	case !st.Assessed:
		b.WriteString("The assessment of this commit is not recorded yet.\n")
	default:
		b.WriteString("| Story | Criteria | Code | Architecture | Your part |\n|---|---|---|---|---|\n")
		for _, r := range rows {
			fmt.Fprintf(&b, "| %s %s | %s | %s | %s | %s |\n", r.light, planTableCell(r.story), planTableCell(r.criteria), planTableCell(r.code), planTableCell(r.architecture), planTableCell(r.part))
		}
		if len(st.Rounds) > 0 {
			// Owner-chosen repairs: a rejected or spent round shows why.
			b.WriteString("\nRepair rounds: " + repairRoundsText(st.Rounds) + "\n")
		}
	}
	if st.Handback != nil {
		b.WriteString("\n**What still needs fixing:** " + planTableCell(clipText(strings.TrimSpace(st.Handback.OwnerInstruction), 600)) + "\n")
	}
	if len(st.Open) > 0 {
		b.WriteString("\n**Displayed blockers:**\n")
		for _, blocker := range st.Open {
			b.WriteString("- " + planTableCell(blocker) + "\n")
		}
	}
	if w := st.Waiver; w != nil {
		fmt.Fprintf(&b, "\n**Owner shipping waiver:** task %s · Validate %d · tested `%s` · displayed delivery `%s` · report `%s` · evidence revision `%s` · authenticated owner %d · source comment %d · time %s.\n\nReason: %s\n", planTableCell(w.Task), w.InstanceID, w.TestedCommit, w.DeliveryCommit, w.ReportRevision, w.EvidenceRevision, w.ActorID, w.CommentID, w.CreatedAt.UTC().Format(time.RFC3339Nano), planTableCell(w.Reason))
		for _, blocker := range w.Blockers {
			b.WriteString("- Waived blocker: " + planTableCell(blocker) + "\n")
		}
		if st.DeliveryCommit != "" && st.DeliveryCommit != w.DeliveryCommit {
			b.WriteString("\nThe current displayed head differs from the retained waiver; a fresh shipping decision is required.\n")
		}
	}
	var closeSet []int
	if target != nil {
		closeSet = target.CloseSet()
	}
	b.WriteString("\nNext: " + st.NextStep(closeSet) + "\n")
	return b.String()
}

// validateStoryRow is one story of the overview table.
type validateStoryRow struct {
	key, light, story, criteria, code, architecture, part string
}

// validateStoryRows builds one row per assessed story, in assessment order.
func validateStoryRows(st ValidateState, target *ValidateDeliveryTarget) []validateStoryRow {
	var rows []validateStoryRow
	for _, story := range st.Stories {
		var findings []ValidateFinding
		for _, f := range st.Findings {
			if f.Story == story.Story {
				findings = append(findings, f)
			}
		}
		r := validateStoryRow{key: story.Story, light: validateStoryLight(st, findings), story: story.Story}
		ref, ok := target.story(story.Story)
		if ok && ref.Title != "" {
			r.story += " " + ref.Title
		}
		if ok && ref.Number > 0 {
			r.story += fmt.Sprintf(" (%s)", ref.display().Reference())
		}
		r.criteria = criteriaCell(st, findings)
		r.code = reviewCell(st, findings, agent.ReviewRoleCode)
		r.architecture = reviewCell(st, findings, agent.ReviewRoleArchitecture)
		r.part = "—"
		checks, choices := ownerItemCounts(st, findings)
		var parts []string
		if checks > 0 {
			parts = append(parts, plural(checks, "check", "checks"))
		}
		if choices > 0 {
			parts = append(parts, plural(choices, "choice", "choices"))
		}
		if len(parts) > 0 {
			r.part = strings.Join(parts, ", ")
			if ok && ref.Number > 0 {
				r.part += fmt.Sprintf(" on %s", ref.display().Reference())
			}
		}
		rows = append(rows, r)
	}
	return rows
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// story finds the issue of a story key in the delivery target.
func (t *ValidateDeliveryTarget) story(key string) (ValidateIssueRef, bool) {
	if t == nil {
		return ValidateIssueRef{}, false
	}
	for _, s := range t.Stories {
		if s.Key != "" && s.Key == key {
			return s, true
		}
	}
	return ValidateIssueRef{}, false
}

// validateItemBlocks says an item blocks delivery until a new tested snapshot
// or a new review: a failed criterion, a blocking finding or an item whose
// reviewer has no usable result.
func validateItemBlocks(f ValidateFinding) bool {
	switch {
	case f.State == findingNotReviewed:
		return true
	case f.Criterion && f.needsChoice():
		return true
	case f.Blocking && f.needsChoice():
		return true
	}
	return false
}

// validateOwnerItem says an item waits for the owner: a manual check, a
// finding without a choice, or an issue the owner chose that is not created.
func validateOwnerItem(st ValidateState, f ValidateFinding) bool {
	switch {
	case f.State == findingAwaitingCheck || f.State == findingOwnerFailed && f.checkable():
		return true
	case f.needsChoice() && f.Choice == nil:
		return true
	case f.Choice != nil && f.Choice.Choice == store.ChoiceIssue:
		e, ok := st.Effects.Issues[f.ID]
		return ok && (e.State == store.EffectFailed || e.State == store.EffectUncertain)
	}
	return false
}

func validateStoryLight(st ValidateState, findings []ValidateFinding) string {
	light := lightGreen
	for _, f := range findings {
		if validateItemBlocks(f) {
			return lightRed
		}
		if validateOwnerItem(st, f) {
			light = lightYellow
		}
		if f.Choice != nil && f.Choice.Choice == store.ChoiceIssue {
			if e, ok := st.Effects.Issues[f.ID]; !ok || e.State != store.EffectSucceeded {
				light = lightYellow
			}
		}
	}
	return light
}

// validateOverallLight is the worst state of the run: red when the suite did
// not pass or a story blocks, green when delivery may go ahead, else yellow.
func validateOverallLight(st ValidateState, rows []validateStoryRow) string {
	if st.Suite.Outcome != store.SuitePassed {
		return lightRed
	}
	light := lightGreen
	for _, r := range rows {
		if r.light == lightRed {
			return lightRed
		}
		if r.light == lightYellow {
			light = lightYellow
		}
	}
	if !st.Ready {
		light = lightYellow
	}
	return light
}

// criteriaCell counts a story's criteria: satisfied (by the reviewer or by the
// owner's check), awaiting the owner's check and failed. Other reviews of the
// acceptance reviewer, such as the plan review, are added when not accepted.
func criteriaCell(st ValidateState, findings []ValidateFinding) string {
	var total, satisfied, awaiting, failed, notReviewed int
	var other []string
	for _, f := range findings {
		if !f.Criterion {
			if f.Role == agent.ReviewRoleAcceptance && f.State != findingSatisfied && f.State != findingNotReviewed {
				other = append(other, f.Name+" review: "+reviewState(f))
			}
			continue
		}
		total++
		switch f.State {
		case findingSatisfied, findingOwnerPassed:
			satisfied++
		case findingAwaitingCheck:
			awaiting++
		case findingNotReviewed:
			notReviewed++
		default:
			failed++
		}
	}
	if notReviewed > 0 {
		return "not reviewed: " + reviewerReason(st.Reviews, agent.ReviewRoleAcceptance)
	}
	parts := []string{fmt.Sprintf("%d of %d satisfied", satisfied, total)}
	if awaiting > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", awaiting, findingAwaitingCheck))
	}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", failed))
	}
	return strings.Join(append(parts, other...), " · ")
}

// reviewCell is the state of one reviewer's reviews of a story: accepted, the
// number of findings, or not reviewed with the reviewer's reason.
func reviewCell(st ValidateState, findings []ValidateFinding, role string) string {
	var open []ValidateFinding
	seen := false
	for _, f := range findings {
		if f.Criterion || f.Role != role {
			continue
		}
		seen = true
		if f.State == findingNotReviewed {
			return "not reviewed: " + reviewerReason(st.Reviews, role)
		}
		if f.State != findingSatisfied {
			open = append(open, f)
		}
	}
	if !seen {
		return "—"
	}
	if len(open) == 0 {
		return "accepted"
	}
	text := plural(len(open), "finding", "findings")
	var notes []string
	for _, f := range open {
		notes = append(notes, reviewState(f))
	}
	return text + " (" + strings.Join(notes, "; ") + ")"
}

// reviewState is the short state of one open review finding.
func reviewState(f ValidateFinding) string {
	if f.acceptedDeviation() {
		return "owner accepted: " + strings.Join(strings.Fields(clipText(f.Verdict.Evidence, 160)), " ")
	}
	switch f.State {
	case findingAwaitingCheck, findingOwnerPassed, findingOwnerFailed:
		return f.State
	}
	kind := "optional"
	if f.Blocking {
		kind = "blocking"
	}
	if f.Choice != nil {
		return kind + ", " + choiceWords(f)
	}
	return kind
}

// ownerItemCounts counts what a story needs from the owner: manual checks and
// choices (a finding without a choice, or a failed issue to retry).
func ownerItemCounts(st ValidateState, findings []ValidateFinding) (checks, choices int) {
	for _, f := range findings {
		if !validateOwnerItem(st, f) {
			continue
		}
		if f.State == findingAwaitingCheck || f.State == findingOwnerFailed && f.checkable() {
			checks++
		} else {
			choices++
		}
	}
	return checks, choices
}

// validateRetryStep is the one next step for reviews that did not happen: a new
// Validate run on the same commit reuses the passed suite and repeats the reviews.
func validateRetryStep(commit string) string {
	return fmt.Sprintf("Run Validate again on commit `%s` by commenting `%s`: the passed suite is reused and only the reviews run again.", short(commit), ValidateStartCommand)
}

func repairRoundsText(rounds []store.RepairRound) string {
	if len(rounds) == 0 {
		return "none needed"
	}
	var parts []string
	for _, r := range rounds {
		part := fmt.Sprintf("round %d: %s", r.Round, r.State)
		if reason := strings.TrimSpace(r.Reason); reason != "" {
			part += " (" + planTableCell(clipText(reason, 200)) + ")"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "; ")
}

func effectText(name string, e *store.ValidateEffect) string {
	if e == nil {
		return name + ": not started"
	}
	text := name + ": " + e.State
	if e.State == store.EffectFailed || e.State == store.EffectUncertain {
		retry := "repeat your command to retry"
		if name == "merge" && (ValidateState{Effects: ValidateDeliveryRows{Merge: e}}).mergeConflicted() {
			retry = "resolve the conflict and validate again"
		}
		text += " (" + clipText(e.Error, 160) + "; " + retry + ")"
	}
	return text
}

func deliveryText(st ValidateState, target *ValidateDeliveryTarget) string {
	var parts []string
	if effectDone(st.Effects.Close) && st.Waiver != nil && len(st.Waiver.Blockers) > 0 {
		parts = append(parts, "shipped with owner waiver")
	}
	for _, e := range []struct {
		name   string
		effect *store.ValidateEffect
	}{{"push", st.Effects.Push}, {"pull request", st.Effects.PR}, {"merge", st.Effects.Merge}, {"close", st.Effects.Close}} {
		if e.effect != nil {
			parts = append(parts, effectText(e.name, e.effect))
		}
	}
	for _, f := range st.Findings {
		if e, ok := st.Effects.Issues[f.ID]; ok {
			r := decodeEffectResult(&e)
			text := "issue for " + f.ID + ": " + e.State
			if r.Issue > 0 {
				reference := r.IssueReference
				if reference == "" {
					reference = "created issue"
					if target != nil && strings.HasPrefix(target.Parent.display().Label, "#") {
						reference = fmt.Sprintf("#%d", r.Issue)
					}
				}
				text += " (" + reference + ")"
			} else if e.Error != "" {
				text += " (" + clipText(e.Error, 120) + "; repeat your command to retry)"
			}
			parts = append(parts, text)
		}
	}
	if len(parts) == 0 {
		return "nothing is pushed, merged or closed until you give the command for it."
	}
	return strings.Join(parts, "; ")
}
