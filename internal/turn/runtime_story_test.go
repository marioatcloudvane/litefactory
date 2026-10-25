package turn

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

// Exercise the owner-visible report from execution through durable publication
// and replay, rather than checking an isolated recovery fragment.
func TestRuntimeStopGuidanceEndToEnd(t *testing.T) {
	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		writeInWorktree(t, req, "partial.txt", "retained work\n")
		result := succeeded(req, f.paths.Home())
		result.Outcome = agent.OutcomeInterrupted
		result.ResultText = ""
		result.Interruption = &agent.Interruption{}
		return result, nil
	})
	f = newFixture(t, runner)
	f.opts.AutoResumeOnSessionLimit = false
	forge := newFakeForge(t)
	reporter := newReporter(t, f, forge, nil)
	result := f.execute(f.request())
	if _, err := reporter.ReportTurn(t.Context(), result); err != nil {
		t.Fatal(err)
	}
	run := f.runs()[0]
	body := forge.only(t).body
	command := fmt.Sprintf("litefactory logs '%s' --run %d", f.ref().String(), run.Ordinal)
	if !strings.Contains(body, command) {
		t.Fatalf("report lacks affected-run inspection command %q:\n%s", command, body)
	}
	lower := strings.ToLower(body)
	if !strings.Contains(lower, "nothing is scheduled") || !strings.Contains(lower, "disabled") || !strings.Contains(lower, "not complete") {
		t.Fatalf("report must explain retained interrupted work and absent automatic recovery:\n%s", body)
	}
	if strings.Contains(lower, "resume manually") || strings.Count(body, command) != 1 {
		t.Fatalf("report offers competing recovery advice:\n%s", body)
	}
	if run.CommitSHA == "" || run.SemanticOutcome != "interrupted" || run.Status != store.RunReported {
		t.Fatalf("retained run = %+v", run)
	}
	if _, err := reporter.ReportTurn(t.Context(), result); err != nil {
		t.Fatal(err)
	}
	actions, err := f.db.ListPendingContinuations(t.Context(), f.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if runner.callCount() != 1 || len(actions) != 0 || len(forge.all()) != 1 {
		t.Fatalf("replay changed execution/recovery/delivery: calls=%d actions=%d reports=%d", runner.callCount(), len(actions), len(forge.all()))
	}
}

// All absent-continuation reasons must survive complete report assembly; the
// renderer must not turn a missing cause or an unknown action into a schedule.
func TestRuntimeRecoveryGuidanceAssembledStates(t *testing.T) {
	f := newFixture(t, newStubRunner(nil))
	renderer := newReporter(t, f, newFakeForge(t), nil).renderer
	command := "litefactory logs 'github:acme/acme-api#123' --run 7"
	cases := []struct {
		name, cause, status string
		absent              bool
		action              int64
		want                string
	}{
		{name: "disabled", cause: "Automatic session-limit recovery is disabled; resume manually after the limit lifts.", absent: true, want: "disabled"},
		{name: "reset unknown", cause: "No reliable future reset time was supplied; resume manually after the limit lifts.", absent: true, want: "reset time"},
		{name: "identity absent", cause: "No usable conversation identity was recorded; manual recovery is required.", absent: true, want: "conversation identity"},
		{name: "attempts exhausted", cause: "The three automatic invocation limit is exhausted; manual recovery is required.", absent: true, want: "three automatic"},
		{name: "cause absent", absent: true, want: "no reason for the scheduling decision"},
		{name: "eligibility unknown", status: "pending", action: 91, want: "eligibility time is unknown"},
		{name: "settled", status: "succeeded", action: 91, want: "succeeded"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, err := renderer.RenderRun(t.Context(), report.Run{Ref: f.ref(), Ordinal: 7, Result: &agent.RunResult{Outcome: agent.OutcomeInterrupted}, Recovery: &report.Recovery{NoContinuation: tc.absent, ManualReason: tc.cause, ActionID: tc.action, Status: tc.status, InspectionCommand: command}})
			if err != nil {
				t.Fatal(err)
			}
			text := body.String()
			if !strings.Contains(strings.ToLower(text), tc.want) || strings.Count(text, command) != 1 || strings.Contains(text, "resume manually") || strings.Contains(text, "manual recovery is required") {
				t.Fatalf("assembled recovery loses disposition or has competing advice:\n%s", text)
			}
			if strings.Contains(text, "Nothing is scheduled") != tc.absent {
				t.Fatalf("assembled recovery invents action absence:\n%s", text)
			}
		})
	}
}

func TestRuntimeRecoveryReadFailureDoesNotClaimAbsence(t *testing.T) {
	var f *fixture
	f = newFixture(t, newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		return succeeded(req, f.paths.Home()), nil
	}))
	f.execute(f.request())
	reporter := newReporter(t, f, newFakeForge(t), nil)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	rep := RunReport{RunID: f.runs()[0].ID}
	if err := reporter.attachRecoveryReport(ctx, &rep); err == nil {
		t.Fatal("cancelled retained-state read was treated as success")
	}
	if rep.Run.Recovery != nil && rep.Run.Recovery.NoContinuation {
		t.Fatal("failed retained-state read claims no continuation")
	}
}

func TestRuntimeStoppedProjectionAssembledRunIdentity(t *testing.T) {
	var f *fixture
	f = newFixture(t, newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		return succeeded(req, f.paths.Home()), nil
	}))
	f.execute(f.request())
	out := &report.StopRecovery{Step: "agent", Reason: "execution stopped", Disposition: "retained result"}
	if err := projectRecoveryRun(t.Context(), f.db, f.session.ID, f.runs()[0].ID, out); err != nil {
		t.Fatal(err)
	}
	text := report.RenderStopRecovery(out)
	if strings.Count(text, "--run 1") != 1 || !strings.Contains(text, f.session.TaskRef) || !strings.Contains(text, "execution stopped") {
		t.Fatalf("assembled stop loses affected run and one step: %s", text)
	}
}
