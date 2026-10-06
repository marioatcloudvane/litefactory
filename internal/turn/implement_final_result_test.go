package turn

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/store"
)

func implementResultWith(outcome, reason string) []byte {
	return []byte(`{"result":{"outcome":"` + outcome + `","reason":"` + reason + `","comment":"Stopped early","payload":{"implement":{}}}}`)
}

// TestImplementFinalResultAcceptsIncompleteAndBlocked pins AC-001/AC-002: an
// accepted incomplete or blocked answer is a valid run that keeps its outcome
// and reason, the report text shows both, and the success-only gate stays shut.
func TestImplementFinalResultAcceptsIncompleteAndBlocked(t *testing.T) {
	for _, outcome := range []string{"incomplete", "blocked"} {
		t.Run(outcome, func(t *testing.T) {
			_, _, o := agentResultFixture(t, "implement")
			service, err := BindAgentResultService(t.Context(), o)
			mustProgress(t, err)
			live, err := service.Submit(t.Context(), "live", implementResultWith(outcome, "needs a product decision"))
			mustProgress(t, err)
			if live.Snapshot == nil {
				t.Fatalf("submit rejected: %+v", live)
			}
			prep := &preparation{}
			got, err := (&preparedMCP{service: service}).finalize(t.Context(), prep, nil, &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "printed text is ignored"}, nil)
			mustProgress(t, err)
			if !got.Succeeded() || got.Reason != "" {
				t.Fatalf("an honest %s answer was reported as a failure: %+v", outcome, got)
			}
			if prep.acceptedOutcome != outcome || prep.acceptedReason != "needs a product decision" {
				t.Fatalf("outcome and reason not kept: %q %q", prep.acceptedOutcome, prep.acceptedReason)
			}
			for _, want := range []string{"Implement outcome: " + outcome, "Reason: needs a product decision"} {
				if !strings.Contains(got.ResultText, want) {
					t.Errorf("report text lacks %q: %q", want, got.ResultText)
				}
			}
			if prep.workComplete() {
				t.Error("success-only steps would run for an unfinished answer")
			}
		})
	}
}

// TestImplementFinalResultMissingSubmissionStillFails pins that only an
// accepted answer is honest: no submission is a failed run, as before.
func TestImplementFinalResultMissingSubmissionStillFails(t *testing.T) {
	_, _, o := agentResultFixture(t, "implement")
	service, err := BindAgentResultService(t.Context(), o)
	mustProgress(t, err)
	got, err := (&preparedMCP{service: service}).finalize(t.Context(), &preparation{}, nil, &agent.RunResult{Outcome: agent.OutcomeSucceeded}, nil)
	mustProgress(t, err)
	if got.Succeeded() || got.Outcome != agent.OutcomeFailed {
		t.Fatalf("missing submission was accepted: %+v", got)
	}
}

// TestImplementFinalResultCheckedOnlyAtSubmit pins AC-003: after exit the
// factory reads back the accepted result; it neither parses terminal text nor
// resubmits nor runs the completion check again.
func TestImplementFinalResultCheckedOnlyAtSubmit(t *testing.T) {
	db, _, o := agentResultFixture(t, "implement")
	calls := 0
	o.ValidateCompletion = func(context.Context, store.ResultBinding, agentresult.Result, agentresult.Context) agentresult.Diagnostics {
		calls++
		if calls > 1 {
			return agentresult.Diagnostics{Complete: true, Diagnostics: []agentresult.Diagnostic{{Path: "$.result", Code: "stale", Message: "worktree bytes changed"}}}
		}
		return agentresult.Diagnostics{Valid: true, Complete: true}
	}
	service, err := BindAgentResultService(t.Context(), o)
	mustProgress(t, err)
	live, err := service.Submit(t.Context(), "live", implementResultWith("incomplete", "stopped"))
	mustProgress(t, err)
	if live.Snapshot == nil {
		t.Fatalf("submit rejected: %+v", live)
	}
	submitCalls := calls
	got, err := (&preparedMCP{service: service}).finalize(t.Context(), &preparation{}, nil, &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: `{"result":{"outcome":"completed"}}`}, nil)
	mustProgress(t, err)
	if !got.Succeeded() {
		t.Fatalf("a change after submit failed the run: %+v", got)
	}
	if calls != submitCalls {
		t.Fatalf("completion rechecked after exit: %d calls, %d at submit", calls, submitCalls)
	}
	revisions, err := db.ResultRevisionNumbers(t.Context(), service.Binding())
	mustProgress(t, err)
	if len(revisions) != 1 {
		t.Fatalf("terminal text allocated a revision: %v", revisions)
	}
}
