package report_test

import (
	"strings"
	"testing"
)

// TestPublicationBulletGating is AC-008: the Publication bullet renders only
// when a commit exists but never reached the remote — the one publication
// state this comment still discloses — and is dropped whenever a commit was
// pushed, or no commit exists at all, regardless of what the field says.
func TestPublicationBulletGating(t *testing.T) {
	t.Parallel()

	t.Run("commit not pushed/renders", func(t *testing.T) {
		t.Parallel()

		run := successRun()
		run.Pushed = false
		run.Publication = "Local commit " + run.CommitSHA + " retained; publication not confirmed."

		body := render(t, newReporter(t), run).String()
		if !strings.Contains(body, "- **Publication:** "+run.Publication) {
			t.Errorf("missing Publication bullet:\n%s", body)
		}
	})

	t.Run("commit pushed/dropped even though the field is set", func(t *testing.T) {
		t.Parallel()

		run := successRun()
		run.Publication = "Commit " + run.CommitSHA + " published successfully."

		body := render(t, newReporter(t), run).String()
		if strings.Contains(body, "**Publication:**") {
			t.Errorf("a pushed commit's report still carries a Publication bullet:\n%s", body)
		}
	})

	t.Run("no commit/dropped even though the field is set", func(t *testing.T) {
		t.Parallel()

		run := failureRun()
		run.Pushed = false
		run.Publication = "Not published by this run; local edits and commits are retained."

		body := render(t, newReporter(t), run).String()
		if strings.Contains(body, "**Publication:**") {
			t.Errorf("a run with no commit still carries a Publication bullet:\n%s", body)
		}
	})
}
