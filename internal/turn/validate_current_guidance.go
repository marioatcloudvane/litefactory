package turn

import (
	"context"
	"fmt"

	"github.com/cloudvane/litefactory/internal/naming"
)

// validateCurrentGuidance is read-only: a refused reply is never rebound to a
// new finding, report or shipping target.
func validateCurrentGuidance(ctx context.Context, opts Options, req ActivityRequest, st ValidateState) string {
	instance, err := opts.DB.GetGraphInstance(ctx, req.InstanceID)
	if err != nil {
		return "The current report destination could not be read. Nothing was approved or delivered."
	}
	session, err := opts.DB.GetSession(ctx, instance.SessionID)
	if err != nil {
		return "The affected issue could not be read. Nothing was approved or delivered."
	}
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		return "The affected issue destination is unavailable. Nothing was approved or delivered."
	}
	display, e := opts.DB.LatestOwnerDecisionDisplay(ctx, session.TaskRef, ValidateShipCommand)
	if e != nil || display.InstanceID != req.InstanceID || display.Revision != st.Revision || display.TestedCommit != st.Commit {
		return fmt.Sprintf("The current report binding for %s cannot be confirmed yet. Its existing refresh must finish before choosing again. Nothing was approved or delivered.", session.TaskRef)
	}
	if opts.ResearchReporter == nil || opts.ResearchReporter.validateHead == nil {
		return fmt.Sprintf("The current delivery head for %s cannot be confirmed yet. Its existing report refresh must finish before choosing again. Nothing was approved or delivered.", session.TaskRef)
	}
	if opts.ResearchReporter != nil && opts.ResearchReporter.validateHead != nil {
		head, e := opts.ResearchReporter.validateHead(ctx, session)
		if e != nil || display.DeliveryCommit != head {
			return fmt.Sprintf("The displayed delivery head for %s is no longer confirmed. Comment `%s` to validate the current head before choosing delivery. Nothing was approved or delivered.", session.TaskRef, ValidateStartCommand)
		}
	}
	report, err := opts.DB.GetValidateReport(ctx, req.InstanceID)
	if err != nil || report.CommentID == 0 || report.Revision != st.Revision || report.CommentID != display.PublicationID {
		return fmt.Sprintf("Current report revision `%s` for %s is not confirmed at a recoverable destination yet. Its existing report refresh must finish before choosing again. Nothing was approved or delivered.", st.Revision, session.TaskRef)
	}
	if opts.ResearchReporter == nil {
		return "The current report publisher is unavailable. Nothing was approved or delivered."
	}
	link, err := opts.ResearchReporter.observedDecisionURL(ctx, ref, report.CommentID)
	if err != nil || link == "" {
		return fmt.Sprintf("The current report for %s is revision `%s`, but its exact destination is unavailable. Nothing was approved or delivered.", session.TaskRef, st.Revision)
	}
	return fmt.Sprintf("For %s, use the current choices and prerequisites in [Validate revision %s](%s). Give a fresh choice there with your own evidence or reason where required; this refused reply changed no finding or delivery permission.", session.TaskRef, st.Revision, link)
}
