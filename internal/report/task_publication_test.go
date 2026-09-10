package report_test

import (
	"strings"
	"testing"
)

func TestTaskPublicationReport(t *testing.T) {
	for _, tc := range []struct {
		name, publication string
		pushed            bool
	}{
		{"preparation published", "Commit abc123 published successfully.", true},
		{"failed runner retained", "Not published by this run; local edits and commits are retained. Retained local commit: abc123.", false},
		{"concurrent push retained", "Local commit abc123 retained; publication not confirmed. Remote task tip changed to def456; request a newly verified run.", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			run := successRun()
			if !tc.pushed {
				run = failureRun()
			}
			run.Pushed = tc.pushed
			run.Publication = tc.publication
			body := render(t, newReporter(t), run).String()
			if !strings.Contains(body, tc.publication) {
				t.Fatalf("missing publication status %q:\n%s", tc.publication, body)
			}
		})
	}
}
