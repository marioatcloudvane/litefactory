package turn

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestExecutionRunEvidenceKeepsObservedModelsAndFailedLaunch(t *testing.T) {
	f := newFixture(t, nil)
	run := f.seedRun(1, "evidence", 21, string(agent.ModeResearch), store.RunSucceeded)
	reporter := newReporter(t, f, newFakeForge(t), nil)
	entries, err := reporter.executionRunEvidence(t.Context(), run)
	mustProgress(t, err)
	if len(entries) != 1 || entries[0].Agent != "claude" || entries[0].Model != "" {
		t.Fatalf("historical evidence %+v", entries)
	}
	attempt, err := f.db.StartMeterAttempt(t.Context(), run.ID, "claude")
	mustProgress(t, err)
	for i, model := range []string{"actual-one", "actual-two"} {
		id := fmt.Sprintf("invocation-%d", i)
		output := int64(i + 1)
		parent := ""
		if i > 0 {
			parent = "invocation-0"
		}
		mustProgress(t, f.db.RecordMeterEvent(t.Context(), attempt.ID, "claude", agent.UsageEvent{Invocation: &agent.UsageInvocation{ID: id, ParentID: parent, SourceID: "source", SourceInvocationID: id, Coverage: agent.UsageCoverage{Status: agent.CollectionPartial}}}))
		mustProgress(t, f.db.RecordMeterEvent(t.Context(), attempt.ID, "claude", agent.UsageEvent{Observation: &agent.UsageObservation{AttemptID: attempt.ID, SourceID: "source", EventKey: id, InvocationID: id, ActualModel: &model, ModelProvenance: "provider", Mode: agent.CounterIncremental, Scope: agent.UsageExclusive, Tokens: agent.TokenUsage{Output: &output}, Coverage: agent.UsageCoverage{Status: agent.CollectionPartial}}}))
	}
	failed, err := f.db.StartMeterAttempt(t.Context(), run.ID, "claude")
	mustProgress(t, err)
	mustProgress(t, f.db.FinishMeterAttempt(t.Context(), failed.ID, "failure", "unknown", "", "", nil))
	entries, err = reporter.executionRunEvidence(t.Context(), run)
	mustProgress(t, err)
	body := report.RenderExecutions(entries, report.ExecutionReportLimit)
	for _, want := range []string{"actual-one", "actual-two", "invocation-0", "invocation-1", failed.ID, "not reported"} {
		if !strings.Contains(body, want) {
			t.Fatalf("lost %q: %s", want, body)
		}
	}
	mustProgress(t, f.db.Close())
	if _, err = reporter.executionRunEvidence(t.Context(), run); err == nil {
		t.Fatal("storage failure hidden")
	}
}

func TestExecutionEvidenceFrozenRecoverySkipsLiveRead(t *testing.T) {
	f, forge, _, instance := persistentStopReporterFixture(t)
	reporter := newReporter(t, f, forge, nil)
	projection, err := ProjectGraphRecovery(t.Context(), f.db, f.session.ID)
	mustProgress(t, err)
	if projection == nil {
		projection = &report.StopRecovery{InstanceID: instance.ID, Step: "analyst", Disposition: "stopped", Reason: "Diagnosis ready", NextStep: "Review analysis"}
	}
	ref := f.ref()
	intent := reporter.deliveryIntent(ref, ref, f.session.ID, 0, 0, "evidence-replay")
	reads := 0
	build := func() (string, error) {
		reads++
		if reads > 1 {
			return "", fmt.Errorf("live evidence store unavailable")
		}
		return "Agent codex; model not reported", nil
	}
	_, err = reporter.deliverRecoveryProjection(t.Context(), intent, projection, build)
	mustProgress(t, err)
	original := forge.only(t).body
	_, err = reporter.deliverRecoveryProjection(t.Context(), intent, projection, build)
	mustProgress(t, err)
	if reads != 1 || forge.attemptCount() != 1 || forge.only(t).body != original {
		t.Fatal("frozen report re-read evidence or changed publication")
	}
}
