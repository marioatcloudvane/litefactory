package report_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
)

func TestRunFailureDiagnosticVisibleAndSanitized(t *testing.T) {
	t.Parallel()
	const secret = "private-startup-secret-value"
	const explanation = "A sandbox restriction is possible; known nested Codex limitation. Use fixtures and provenance: docs/agent-evidence-capture.md."
	run := failureRun()
	run.WorktreePath = testWorktreePath
	run.Result.Diagnostic = explanation + "\n```\n# forged heading\n</details> " + secret
	before := run.Result.Diagnostic
	rep := newReporter(t, func(o *redact.Options) { o.Environ = []string{"STARTUP_TOKEN=" + secret} })
	body := render(t, rep, run)
	text := body.String()
	if !body.Valid() {
		t.Fatal("invalid rendered body")
	}
	for _, want := range []string{explanation, "[redacted]", "exit", "1"} {
		if !strings.Contains(text, want) {
			t.Errorf("body missing %q: %s", want, text)
		}
	}
	for _, unwanted := range []string{secret, "\n# forged heading\n", "\n</details> " + secret} {
		if strings.Contains(text, unwanted) {
			t.Errorf("unsafe diagnostic output %q", unwanted)
		}
	}
	if strings.Count(text, testWorktreePath) != 1 {
		t.Errorf("worktree count=%d", strings.Count(text, testWorktreePath))
	}
	if run.Result.Diagnostic != before || run.Result.Outcome != agent.OutcomeFailed || run.Result.ExitCode != 1 {
		t.Fatal("render mutated result")
	}
}

func TestRunDiagnosticOnlyForFailedOutcome(t *testing.T) {
	t.Parallel()
	for _, outcome := range []agent.Outcome{agent.OutcomeSucceeded, agent.OutcomeNoResult, agent.OutcomeInterrupted, agent.OutcomeTimedOut, agent.OutcomeAbandoned} {
		t.Run(outcome.String(), func(t *testing.T) {
			t.Parallel()
			run := failureRun()
			run.Result.Outcome = outcome
			run.Result.Diagnostic = "UNEXPECTED_STARTUP_HINT"
			body := render(t, newReporter(t), run).String()
			if strings.Contains(body, run.Result.Diagnostic) {
				t.Fatalf("hint leaked into %s report", outcome)
			}
		})
	}
}

func TestRunFailureDiagnosticBudget(t *testing.T) {
	t.Parallel()
	for _, limit := range []int{1500, 4000, 16000} {
		t.Run(fmt.Sprintf("limit_%d", limit), func(t *testing.T) {
			t.Parallel()
			rep, err := report.New(report.Options{Redactor: redact.New(redact.Options{Environ: []string{}}), Limit: limit})
			if err != nil {
				t.Fatal(err)
			}
			run := failureRun()
			run.WorktreePath = testWorktreePath
			run.Result.Diagnostic = "Recognizable startup guidance " + strings.Repeat("é", 10000) + "DIAGNOSTIC_TAIL_MUST_BE_BOUNDED"
			run.Output = strings.Repeat("verbose output\n", 1000)
			body := render(t, rep, run)
			if !body.Valid() || body.Len() > limit {
				t.Fatalf("invalid or oversized body %d > %d", body.Len(), limit)
			}
			if strings.Contains(body.String(), "DIAGNOSTIC_TAIL_MUST_BE_BOUNDED") {
				t.Fatal("unbounded diagnostic")
			}
			if strings.Count(body.String(), testWorktreePath) != 1 {
				t.Fatalf("missing or duplicated worktree: %s", body.String())
			}
			if limit >= 4000 && !strings.Contains(body.String(), "Recognizable startup guidance") {
				t.Fatal("diagnostic lost at usable body budget")
			}
		})
	}
}
