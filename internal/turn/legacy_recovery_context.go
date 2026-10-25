package turn

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// legacyRecoveryContext prepares historical source material for existing intake.
// It neither migrates work nor carries approvals into another issue.
func legacyRecoveryContext(ctx context.Context, db *store.DB, req Request) (string, error) {
	session, err := db.GetSession(ctx, req.SessionID)
	if err != nil {
		return "", err
	}
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		return "", err
	}
	source := session.TaskRef
	if ref.Forge == naming.ForgeGitHub {
		source = ref.IssueURL()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n\nContext for the successor task (historical evidence, not new authority):\n\nSource: %s. Keep this source reference in the new task, then reply here with the new task link so both histories remain connected.\n\nOriginal request: %s\n%s\n", source, legacyContextText(req.Issue.Title, 1024), legacyContextText(req.Issue.Body, 6000))
	if session.Branch != "" {
		fmt.Fprintf(&b, "\nRetained branch: %s. Preserve existing edits and inspect them before repeating changes.\n", session.Branch)
	}
	guidance, err := db.CurrentOwnerGuidance(ctx, session.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return "", err
	}
	if err == nil && guidance.Revision > 0 {
		fmt.Fprintf(&b, "\nStanding guidance at source, revision %d (owner must restate any intended authority on the new task):\n%s\n", guidance.Revision, legacyContextText(guidance.Body, 4000))
	}
	comments, err := db.ListComments(ctx, session.ID)
	if err != nil {
		return "", err
	}
	if len(comments) > 6 {
		comments = comments[len(comments)-6:]
	}
	for _, c := range comments {
		fmt.Fprintf(&b, "\nRetained source comment %d by actor %d (%s):\n%s\n", c.ForgeCommentID, c.AuthorUserID, c.Classification, legacyContextText(c.Body, 1500))
	}
	runs, err := db.ListRuns(ctx, session.ID)
	if err != nil {
		return "", err
	}
	if len(runs) > 6 {
		runs = runs[len(runs)-6:]
	}
	for _, run := range runs {
		fmt.Fprintf(&b, "\nRun %d: status %s, confirmed successful execution %t, retained commit %s. Reason: %s\n", run.ID, run.Status, run.Succeeded(), run.CommitSHA, legacyContextText(run.Error, 1000))
	}
	instance, err := db.LatestSessionGraphInstance(ctx, session.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return "", err
	}
	if err == nil {
		attempts, err := db.ListGraphNodeAttempts(ctx, instance.ID)
		if err != nil {
			return "", err
		}
		if len(attempts) > 4 {
			attempts = attempts[len(attempts)-4:]
		}
		for _, attempt := range attempts {
			outputs, err := db.ListGraphResultOutputsBounded(ctx, attempt.ID, 8192, 20)
			if errors.Is(err, store.ErrGraphInvariant) {
				fmt.Fprintf(&b, "\nRetained findings for instance %d, step %s, attempt %d exceed this passage's bound; inspect them on the linked source before proceeding.\n", instance.ID, attempt.Node, attempt.ID)
				continue
			}
			if err != nil {
				return "", err
			}
			for _, output := range outputs {
				fmt.Fprintf(&b, "\nRetained %s from step %s, attempt %d, source owner comment %d:\n%s\n", output.Kind, attempt.Node, attempt.ID, output.OwnerCommentID, legacyContextText(string(output.Payload), 2000))
			}
		}
	}
	b.WriteString("\nRetain prior findings and answers from the linked source. Completed effects stay settled; unconfirmed external effects require inspection, not repetition. The old history and approvals remain historical evidence.\n")
	return b.String(), nil
}

func legacyContextText(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return strings.ToValidUTF8(text[:limit], "") + " [shortened; consult the linked source]"
}
