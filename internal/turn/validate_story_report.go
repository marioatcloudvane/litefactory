package turn

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// refreshValidateStories projects the Validate state of each assessed story
// onto its sub-issue: the satisfied criteria are ticked in the story body, and
// one factory comment lists only what the story needs from the owner. A forge
// failure is returned so the next refresh retries; a conflict with the frozen
// requirements is logged and leaves the body alone.
func (b ValidateFlowBindings) refreshValidateStories(ctx context.Context, opts Options, req ActivityRequest, st ValidateState, target ValidateDeliveryTarget) error {
	if !st.Assessed || st.Suite.Outcome != store.SuitePassed {
		return nil
	}
	instance, err := opts.DB.GetGraphInstance(ctx, req.InstanceID)
	if err != nil {
		return err
	}
	session, err := opts.DB.GetSession(ctx, instance.SessionID)
	if err != nil {
		return err
	}
	task, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		return err
	}
	var faults []error
	for _, s := range target.Stories {
		if s.Key == "" || s.Number < 1 {
			continue
		}
		i := slices.IndexFunc(st.Stories, func(j ValidateJoinedStory) bool { return j.Story == s.Key })
		if i < 0 {
			continue
		}
		if b.Delivery.tickets() != nil {
			if err := tickValidateStory(ctx, opts, b.Delivery.tickets(), task, target, s, st, st.Stories[i]); err != nil {
				faults = append(faults, fmt.Errorf("story %s (%s): %w", s.Key, s.display().Reference(), err))
				continue
			}
		}
		text := renderValidateStoryComment(st, s, target.Parent.Number, target.Parent.display().Reference())
		if err := opts.ResearchReporter.reportValidateStory(ctx, session.ID, task, s.Number, fmt.Sprintf("validate-story:%d:%s", req.InstanceID, s.Key), text); err != nil {
			faults = append(faults, fmt.Errorf("story %s (%s) comment: %w", s.Key, s.display().Reference(), err))
		}
	}
	return errors.Join(faults...)
}

// tickValidateStory ticks every satisfied or owner-passed criterion in the
// story body and leaves the others unticked, against the frozen approved body,
// then reads the body back to confirm.
func tickValidateStory(ctx context.Context, opts Options, forge ValidateTickets, task naming.Ref, target ValidateDeliveryTarget, s ValidateIssueRef, st ValidateState, joined ValidateJoinedStory) error {
	if s.Body == "" || slices.Contains(joined.Unresolved, agent.ReviewRoleAcceptance) {
		return nil
	}
	checked := map[string]bool{}
	for _, f := range st.Findings {
		if f.Story == s.Key && f.Criterion {
			checked[f.Name] = f.State == findingSatisfied || f.State == findingOwnerPassed
		}
	}
	if len(checked) == 0 {
		return nil
	}
	skip := func(reason string) error {
		if opts.Logger != nil {
			opts.Logger.Warn("validate leaves the story body unticked", slog.String("story", s.Key), slog.Int("issue", s.Number), slog.String("reason", reason))
		}
		return nil
	}
	renderer, ok := opts.ResearchReporter.renderer.(acceptanceRenderer)
	if !ok {
		return skip("the acceptance renderer is unavailable")
	}
	decision := store.AcceptanceDecision{Parent: task.String(), Generation: target.Generation, Story: s.Key, Revision: s.Revision, FrozenBody: s.Body}
	repo := target.ticketRepo()
	live, err := forge.GetIssue(ctx, repo, s.Number)
	if err != nil {
		return err
	}
	if s.ID != 0 && live.ID != s.ID {
		return skip("the story issue identity changed")
	}
	projected, err := projectStoryAcceptance(ctx, opts.DB, decision, live.Body, checked)
	if err != nil {
		return skip(err.Error())
	}
	if projected == live.Body {
		return nil
	}
	title, body, err := renderer.RenderAcceptanceProjection(ctx, live.Title, projected)
	if err != nil {
		return skip(err.Error())
	}
	if _, err := forge.UpdateIssue(ctx, repo, s.Number, title, body); err != nil {
		return err
	}
	confirmed, err := forge.GetIssue(ctx, repo, s.Number)
	if err != nil {
		return err
	}
	want, err := projectStoryAcceptance(ctx, opts.DB, decision, confirmed.Body, checked)
	if err != nil || want != confirmed.Body {
		return errors.New("the ticked criteria are not confirmed in the story body")
	}
	return nil
}

// renderValidateStoryComment is the one Validate comment on a story sub-issue:
// the story's light and counts, then only the owner's open items with one
// command template each, the failed criteria and blocking findings with their
// reason, or "Nothing to do".
//
// Each finding's text is shown up to validateFindingTextLimit bytes. When the
// comment would not fit the forge's comment limit less a reserve for the marker
// and redaction, the per-finding bound is halved down to a floor; below that,
// later findings are listed without their text, and if even that does not fit,
// the rest are counted in a closing line (#1123).
func renderValidateStoryComment(st ValidateState, s ValidateIssueRef, parent int, parentReference ...string) string {
	reference := fmt.Sprintf("#%d", parent)
	if len(parentReference) > 0 {
		reference = parentReference[0]
	}
	budget := github.CommentSizeLimit - validateStoryCommentReserve
	bound := validateFindingTextLimit
	for {
		body := renderValidateStoryCommentBound(st, s, reference, bound, 0)
		if len(body) <= budget {
			return body
		}
		if bound <= validateStoryTextFloor {
			return renderValidateStoryCommentBound(st, s, reference, bound, budget)
		}
		bound = max(bound/2, validateStoryTextFloor)
	}
}

// validateStoryCommentReserve leaves room for the delivery marker and
// redaction under the forge's comment limit.
const validateStoryCommentReserve = 4096

// validateStoryTextFloor is the smallest per-finding text bound before later
// findings are listed without text.
const validateStoryTextFloor = 512

// validateStoryLine is one listed finding with and without its text.
type validateStoryLine struct {
	full, bare string
}

// renderValidateStoryCommentBound renders the story comment with each
// finding's text shortened to bound bytes. A positive fit is the byte budget
// the body must stay within: findings keep their text while the running body
// fits, every later finding is listed without text, and once even that does
// not fit the remainder is counted in a closing line.
func renderValidateStoryCommentBound(st ValidateState, s ValidateIssueRef, reference string, bound, fit int) string {
	var findings []ValidateFinding
	for _, f := range st.Findings {
		if f.Story == s.Key {
			findings = append(findings, f)
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s **Validate · story %s** · commit `%s` · revision `%s`\n\n", validateStoryLight(st, findings), s.Key, short(st.Commit), st.Revision)
	fmt.Fprintf(&b, "Criteria: %s. Satisfied criteria are ticked in the story body.\n\n", criteriaCell(st, findings))
	fmt.Fprintf(&b, "Code: %s · Architecture: %s\n", reviewCell(st, findings, agent.ReviewRoleCode), reviewCell(st, findings, agent.ReviewRoleArchitecture))

	var notReviewed []string
	for _, role := range ValidateReviewRoles {
		for _, f := range findings {
			if f.Role == role && f.State == findingNotReviewed {
				notReviewed = append(notReviewed, fmt.Sprintf("- The %s reviewer has no usable result: %s. The next step on the parent %s runs the reviews again.", role, planTableCell(reviewerReason(st.Reviews, role)), reference))
				break
			}
		}
	}
	if len(notReviewed) > 0 {
		b.WriteString("\n**Not reviewed**\n\n" + strings.Join(notReviewed, "\n") + "\n")
	}

	var blocks []validateStoryLine
	for _, f := range findings {
		if f.State == findingNotReviewed || !validateItemBlocks(f) {
			continue
		}
		reason := f.Summary
		if f.Verdict != nil && f.Verdict.Verdict == store.VerdictFail {
			reason = "your check failed: " + f.Verdict.Evidence
		}
		kind := "Criterion"
		if !f.Criterion {
			kind = "Blocking finding"
		}
		line := fmt.Sprintf("- %s `%s` failed", kind, f.Name)
		full := line
		if strings.TrimSpace(reason) != "" {
			full += ": " + planTableCell(shortenFindingText(reason, bound))
		}
		recovery := " It clears only on a new tested snapshot."
		if f.acceptable() {
			recovery = " It clears on a new tested snapshot, or when you accept it as a deliberate deviation."
		}
		if f.checkable() {
			recovery = " A later evidenced /validate-check verdict can clear it."
		}
		blocks = append(blocks, validateStoryLine{full: full + recovery, bare: line + "." + recovery})
	}

	var items []validateStoryLine
	rev := st.Revision
	for _, f := range findings {
		if !validateOwnerItem(st, f) {
			continue
		}
		look := ""
		if sum := strings.TrimSpace(f.Summary); sum != "" {
			look = ": " + planTableCell(shortenFindingText(sum, bound))
		}
		switch {
		case f.State == findingAwaitingCheck || f.State == findingOwnerFailed && f.checkable():
			format := "- `%s` awaits your check%s\n  `%s %s %s pass|fail -- <evidence>`"
			items = append(items, validateStoryLine{
				full: fmt.Sprintf(format, f.Name, look, ValidateCheckCommand, rev, f.ID),
				bare: fmt.Sprintf(format, f.Name, "", ValidateCheckCommand, rev, f.ID),
			})
		case f.needsChoice() && f.Choice == nil:
			choices := "repair|issue bug|issue story"
			if f.Optional {
				choices += "|defer"
			}
			if f.acceptable() {
				choices += "|accept -- <reason>"
			}
			kind := "optional finding"
			if f.Blocking {
				kind = "blocking finding"
			}
			if f.Criterion {
				kind = "failed criterion"
			}
			format := "- `%s` (%s) needs your choice%s\n  `%s %s %s %s`"
			items = append(items, validateStoryLine{
				full: fmt.Sprintf(format, f.Name, kind, look, ValidateFindingCommand, rev, f.ID, choices),
				bare: fmt.Sprintf(format, f.Name, kind, "", ValidateFindingCommand, rev, f.ID, choices),
			})
		default:
			e := st.Effects.Issues[f.ID]
			line := fmt.Sprintf("- The issue for `%s` is %s (%s); repeat your choice\n  `%s %s %s issue %s`", f.Name, e.State, planTableCell(clipText(e.Error, 160)), ValidateFindingCommand, rev, f.ID, f.Choice.IssueKind)
			items = append(items, validateStoryLine{full: line, bare: line})
		}
	}

	blockHead := "\n**Blocks**\n\n"
	itemHead := fmt.Sprintf("\n**Your part** (give each command as its own comment on the parent %s):\n\n", reference)
	closing := ""
	if fit > 0 {
		blocks, items, closing = fitValidateStoryLines(b.Len(), fit, blockHead, itemHead, blocks, items)
	}
	if len(blocks) > 0 {
		b.WriteString(blockHead + joinValidateStoryLines(blocks) + "\n")
	}
	if len(items) == 0 && closing == "" {
		b.WriteString("\n**Your part:** nothing to do.\n")
		return b.String()
	}
	if len(items) > 0 {
		b.WriteString(itemHead + joinValidateStoryLines(items) + "\n")
	}
	if closing != "" {
		b.WriteString("\n" + closing + "\n")
	}
	return b.String()
}

// fitValidateStoryLines decides, in order, which listed findings keep their
// text within budget given the bytes already used. A finding keeps its text
// while the body, with every later finding listed without text, still fits.
// From the first that does not, it and every later finding are listed without
// text; once even that does not fit, the rest are dropped and counted in the
// returned closing line.
func fitValidateStoryLines(used, budget int, blockHead, itemHead string, blocks, items []validateStoryLine) (fitBlocks, fitItems []validateStoryLine, closing string) {
	const closingReserve = 200
	all := append(append([]validateStoryLine(nil), blocks...), items...)
	used += len(blockHead) + len(itemHead) + len("\n\n\n") + closingReserve
	later := make([]int, len(all)+1)
	for i := len(all) - 1; i >= 0; i-- {
		later[i] = later[i+1] + len(all[i].bare) + 1
	}
	keep, bare := 0, false
	for i, line := range all {
		if !bare && used+len(line.full)+1+later[i+1] <= budget {
			used += len(line.full) + 1
			keep++
			continue
		}
		bare = true
		if used+len(line.bare)+1 > budget {
			break
		}
		all[i].full = line.bare
		used += len(line.bare) + 1
		keep++
	}
	if omitted := len(all) - keep; omitted > 0 {
		noun := "findings are"
		if omitted == 1 {
			noun = "finding is"
		}
		closing = fmt.Sprintf("%d more %s not listed here; see the parent issue's Validate report for their commands.", omitted, noun)
	}
	all = all[:keep]
	split := min(len(blocks), len(all))
	return all[:split], all[split:], closing
}

func joinValidateStoryLines(lines []validateStoryLine) string {
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = line.full
	}
	return strings.Join(out, "\n")
}

// reportValidateStory posts the story's Validate comment once on its sub-issue
// and then edits it in place.
func (r *Reporter) reportValidateStory(ctx context.Context, sessionID int64, task naming.Ref, issue int, purpose, text string) error {
	destination := task
	destination.Issue = issue
	intent := r.deliveryIntent(task, destination, sessionID, 0, 0, purpose)
	_, err := r.upsertValidateComment(ctx, intent, destination, text)
	return err
}
