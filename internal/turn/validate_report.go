package turn

import (
	"fmt"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/coverage"
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

// renderValidateReport is the one report the owner reads on the parent: a
// short summary of at most about a dozen lines (what failed, why, the next
// step and one line per open item, each with its one exact command), then
// collapsed details holding every recorded fact and the complete command
// reference. It reads records only, so the same records render the same bytes.
func renderValidateReport(st ValidateState, target *ValidateDeliveryTarget, notice string) string {
	return renderValidateReportLayout(st, target, notice).full()
}

// validateReportLayout is the parent report in its parts: the summary, the
// recorded facts and the command reference entries. Publication keeps the
// summary whole and may move the reference into numbered parts.
type validateReportLayout struct {
	Summary  string
	Facts    string
	Header   string
	Commands []string
	Linear   bool
}

// open starts the details. Linear already folds factory markers into its one
// collapsible footer, so its details are a plain section, not a second one.
func (l validateReportLayout) open() string {
	if l.Linear {
		return "---\n\n**Details**\n\n"
	}
	return "<details>\n<summary>Details</summary>\n\n"
}

func (l validateReportLayout) close() string {
	if l.Linear {
		return ""
	}
	return "\n</details>\n"
}

// full is the complete report in one comment.
func (l validateReportLayout) full() string {
	var b strings.Builder
	b.WriteString(l.Summary + "\n" + l.open() + l.Facts)
	if len(l.Commands) > 0 {
		b.WriteString(l.Header + strings.Join(l.Commands, "\n\n") + "\n")
	}
	b.WriteString(l.close())
	return b.String()
}

// overview is the report without its command reference, which publication
// moves into numbered parts; a compact overview drops the facts as well.
func (l validateReportLayout) overview(compact bool) string {
	facts := l.Facts
	if compact {
		facts = "The detailed overview exceeds this comment's capacity. Every item's state, commands and consequences are in the numbered parts below.\n"
	}
	return l.Summary + "\n" + l.open() + facts + l.close()
}

func renderValidateReportLayout(st ValidateState, target *ValidateDeliveryTarget, notice string) validateReportLayout {
	onLinear := target != nil && target.Task.Forge == "linear"
	rows := validateStoryRows(st, target)
	sum := buildValidateSummary(st)
	summary, rest := sum.render(st, onLinear, validateOverallLight(st, rows), notice)
	var closeSet []int
	if target != nil {
		closeSet = target.CloseSet()
	}
	return validateReportLayout{
		Summary:  summary,
		Facts:    renderValidateFacts(st, target, rows, sum, rest),
		Header:   validateGuidanceHeader(st),
		Commands: validateCommandReference(onLinear, st, sum, closeSet, len(rest)),
		Linear:   onLinear,
	}
}

// renderValidateFacts is every recorded fact the report shows: delivery,
// coverage, the tested and displayed commits, the suite and repair rounds, the
// story table, each failure, the retained repairs, the blockers, the waiver
// and the reporter's diagnosis. Commands live in the command reference.
func renderValidateFacts(st ValidateState, target *ValidateDeliveryTarget, rows []validateStoryRow, sum validateSummary, rest []validateSummaryLine) string {
	var b strings.Builder
	onLinear := target != nil && target.Task.Forge == "linear"
	b.WriteString("Delivery: " + deliveryText(st, target) + "\n\n")
	fmt.Fprintf(&b, "Tested commit: `%s`.", st.Commit)
	if st.DeliveryCommit != "" {
		fmt.Fprintf(&b, " Displayed delivery commit: `%s`; the test evidence is bound to the tested commit.", st.DeliveryCommit)
	}
	b.WriteString("\n\n")
	if st.Suite.AssessmentID != "" {
		fmt.Fprintf(&b, "Coverage assessment `%s` (digest `%s`). Failed executions remain failed.\n\n", st.Suite.AssessmentID, st.Suite.AssessmentDigest)
		if m := st.Suite.Assessment; m != nil && m.Commit == st.Commit && coverage.Digest(m) == st.Suite.AssessmentDigest {
			fmt.Fprintf(&b, "Earlier suite commit: `%s`; pinned comparison commit: `%s`. Measured validation duration: %s.\n\n", m.PreviousCommit, m.BaseCommit, m.Duration.Round(time.Millisecond))
			if m.FullReason != "" {
				fmt.Fprintf(&b, "Full execution reason: %s\n\n", planTableCellFor(onLinear, m.FullReason))
			}
			if m.FullDuration > 0 && m.Duration > 0 && m.Duration < m.FullDuration {
				fmt.Fprintf(&b, "Estimated savings against comparable full execution (%s): %s.\n\n", m.FullDuration.Round(time.Millisecond), (m.FullDuration - m.Duration).Round(time.Millisecond))
			}
			if len(m.Results) > 0 {
				b.WriteString("| Unit | Evidence | Original commit | Source | Reason |\n|---|---|---|---|---|\n")
				for _, r := range m.Results {
					fmt.Fprintf(&b, "| %s | %s | `%s` | `%s` | %s |\n", planTableCellFor(onLinear, r.Unit), planTableCellFor(onLinear, r.Kind), r.Origin.Commit, r.Origin.Evidence, planTableCellFor(onLinear, r.Reason))
				}
				b.WriteString("\n")
			}
		}
	}
	switch {
	case !suiteCoverageSatisfied(st.Suite):
		suite := "The full suite " + st.Suite.Outcome
		if st.Suite.RunID != 0 {
			suite += fmt.Sprintf(" (suite run %d)", st.Suite.RunID)
		}
		if st.Suite.Reason != "" {
			suite += ": " + planTableCellFor(onLinear, clipText(st.Suite.Reason, 300))
		}
		b.WriteString(suite + "\n\nRepair rounds: " + repairRoundsText(onLinear, st.Rounds) + "\n\n")
	case !st.Assessed:
		b.WriteString("The assessment of this commit is not recorded yet.\n\n")
	default:
		b.WriteString("| Story | Criteria | Code | Architecture | Your part |\n|---|---|---|---|---|\n")
		for _, r := range rows {
			story, part := planTableCell(r.story), planTableCell(r.part)
			if onLinear {
				// Linear rows escape only their untrusted parts, so the
				// appended ticket reference links stay intact.
				story, part = r.story, r.part
			}
			fmt.Fprintf(&b, "| %s %s | %s | %s | %s | %s |\n", r.light, story, planTableCellFor(onLinear, r.criteria), planTableCellFor(onLinear, r.code), planTableCellFor(onLinear, r.architecture), part)
		}
		b.WriteString("\n")
		if len(st.Rounds) > 0 {
			// Owner-chosen repairs: a rejected or spent round shows why.
			b.WriteString("Repair rounds: " + repairRoundsText(onLinear, st.Rounds) + "\n\n")
		}
	}
	if len(st.Failures) > 0 {
		b.WriteString("**Failures:**\n\n")
		for i, f := range st.Failures {
			fmt.Fprintf(&b, "%d. `%s` %s", i+1, validateFailureRef(i), codeSpanFor(oneLine(f.Command)))
			if f.Exit != nil {
				fmt.Fprintf(&b, " · exit %d", *f.Exit)
			}
			if f.SuiteRecord != "" {
				b.WriteString(" · " + f.SuiteRecord)
			}
			if r := strings.TrimSpace(f.Reason); r != "" {
				b.WriteString(" · " + planTableCellFor(onLinear, r))
			}
			b.WriteString("\n")
			if c := strings.TrimSpace(f.Cause); c != "" {
				b.WriteString("   Cause: " + planTableCellFor(onLinear, c) + "\n")
			}
			if o := strings.TrimSpace(f.Objective); o != "" {
				b.WriteString("   What Implement must do: " + planTableCellFor(onLinear, o) + "\n")
			}
			if len(f.Locations) > 0 {
				var where []string
				for _, l := range f.Locations {
					if l.Line > 0 {
						where = append(where, fmt.Sprintf("%s:%d", l.Path, l.Line))
					} else {
						where = append(where, l.Path)
					}
				}
				b.WriteString("   Look at: " + planTableCellFor(onLinear, strings.Join(where, ", ")) + "\n")
			}
			if tail := strings.Join(strings.Fields(tailRunes(f.OutputTail, handbackInstructionTail)), " "); tail != "" {
				b.WriteString("   Output tail: " + codeSpanFor(tail) + "\n")
			}
		}
		b.WriteString("\n")
	}
	for _, c := range st.Candidates {
		fmt.Fprintf(&b, "**Retained repair `%s`** · %s · review `%s`", c.ID, c.Resolution, c.ReviewDigest)
		if link := st.CandidateLinks[c.ID]; link != "" {
			fmt.Fprintf(&b, " · [complete ordered review](%s)", link)
		} else {
			b.WriteString(" · complete review delivery is pending")
		}
		b.WriteString("\n\n")
	}
	if len(st.Open) > 0 {
		b.WriteString("**Displayed blockers:**\n")
		for _, blocker := range st.Open {
			b.WriteString("- " + planTableCellFor(onLinear, blocker) + "\n")
		}
		b.WriteString("\n")
	}
	if w := st.Waiver; w != nil {
		fmt.Fprintf(&b, "**Owner shipping waiver:** task %s · Validate %d · tested `%s` · displayed delivery `%s` · report `%s` · evidence revision `%s` · authenticated owner %d · source comment %d · time %s.\n\nReason: %s\n", planTableCellFor(onLinear, w.Task), w.InstanceID, w.TestedCommit, w.DeliveryCommit, w.ReportRevision, w.EvidenceRevision, w.ActorID, w.CommentID, w.CreatedAt.UTC().Format(time.RFC3339Nano), planTableCellFor(onLinear, w.Reason))
		for _, blocker := range w.Blockers {
			b.WriteString("- Waived blocker: " + planTableCellFor(onLinear, blocker) + "\n")
		}
		if st.DeliveryCommit != "" && st.DeliveryCommit != w.DeliveryCommit {
			b.WriteString("\nThe current displayed head differs from the retained waiver; a fresh shipping decision is required.\n")
		}
		b.WriteString("\n")
	}
	b.WriteString(renderReporterFacts(onLinear, st, sum, rest))
	return b.String()
}

// renderReporterFacts says who wrote the summary, the diagnosis of each
// recommended item, the reporter's re-runs, and the recommendations that did
// not fit the summary.
func renderReporterFacts(onLinear bool, st ValidateState, sum validateSummary, rest []validateSummaryLine) string {
	var b strings.Builder
	switch {
	case sum.Agent:
		fmt.Fprintf(&b, "Summary by the Validate reporter (advice %d); the factory wrote every command.\n", max(st.Advice, 1))
	case st.ReporterFallback != "":
		b.WriteString("Summary by the factory from the records: " + planTableCellFor(onLinear, st.ReporterFallback) + ".\n")
	default:
		b.WriteString("Summary by the factory from the records.\n")
	}
	var diagnosed []string
	for _, l := range append([]validateSummaryLine{sum.Next}, sum.Items...) {
		if !l.Agent || l.Diagnosis == "" || l.Diagnosis == agentresult.DiagnosisNone {
			continue
		}
		line := "- "
		if l.Ref != "" {
			line += "`" + l.Ref + "`: "
		}
		line += l.Diagnosis
		if e := strings.TrimSpace(l.Evidence); e != "" {
			line += " · evidence " + implementInline(onLinear, e)
		}
		diagnosed = append(diagnosed, line)
	}
	if len(diagnosed) > 0 {
		b.WriteString("\nDiagnosis:\n" + strings.Join(diagnosed, "\n") + "\n")
	}
	if len(sum.Reruns) > 0 {
		b.WriteString("\nRe-runs by the reporter (read-only):\n")
		for i, r := range sum.Reruns {
			fmt.Fprintf(&b, "%d. `rerun-%d` on the %s commit: %s → %s", i+1, i+1, r.Commit, codeSpanFor(oneLine(r.Command)), r.Result)
			if n := strings.TrimSpace(r.Note); n != "" {
				b.WriteString(" · " + implementInline(onLinear, n))
			}
			b.WriteString("\n")
		}
	}
	if len(rest) > 0 {
		b.WriteString("\nMore items:\n")
		for _, l := range rest {
			b.WriteString("- " + summaryLineText(onLinear, validateSummaryLine{Ref: l.Ref, Text: l.Text, Agent: l.Agent}, true) + " (its commands are in the command reference)\n")
		}
	}
	return b.String() + "\n"
}

// validateStoryRow is one story of the overview table.
type validateStoryRow struct {
	key, light, story, criteria, code, architecture, part string
}

// validateStoryRows builds one row per assessed story, in assessment order.
func validateStoryRows(st ValidateState, target *ValidateDeliveryTarget) []validateStoryRow {
	onLinear := target != nil && target.Task.Forge == "linear"
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
		if onLinear {
			r.story = planTableCellFor(true, r.story)
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
			if onLinear {
				r.part = planTableCellFor(true, r.part)
			}
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
	if !suiteCoverageSatisfied(st.Suite) {
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

func repairRoundsText(onLinear bool, rounds []store.RepairRound) string {
	if len(rounds) == 0 {
		return "none needed"
	}
	var parts []string
	for _, r := range rounds {
		part := fmt.Sprintf("round %d: %s", r.Round, r.State)
		if reason := strings.TrimSpace(r.Reason); reason != "" {
			part += " (" + planTableCellFor(onLinear, clipText(reason, 200)) + ")"
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
			retry = "comment `" + ValidateSyncCommand + "` to resolve it, then validate again"
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
