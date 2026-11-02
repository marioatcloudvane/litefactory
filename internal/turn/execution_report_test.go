package turn

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestExecutionRunEvidenceKeepsObservedModelsAndFailedLaunch(t *testing.T) {
	f := newFixture(t, nil)
	run := f.seedRun(1, "evidence", 21, string(agent.ModeResearch), store.RunSucceeded)
	reporter := newReporter(t, f, newFakeForge(t), nil)
	entries, err := reporter.executionRunEvidence(t.Context(), run, 0)
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
	entries, err = reporter.executionRunEvidence(t.Context(), run, 0)
	mustProgress(t, err)
	body := report.RenderExecutions(entries, report.ExecutionReportLimit)
	for _, want := range []string{"actual-one", "actual-two", "invocation-0", "invocation-1", failed.ID, "not reported"} {
		if !strings.Contains(body, want) {
			t.Fatalf("lost %q: %s", want, body)
		}
	}
	mustProgress(t, f.db.Close())
	if _, err = reporter.executionRunEvidence(t.Context(), run, 0); err == nil {
		t.Fatal("storage failure hidden")
	}
}

func TestBugAnalysisExecutionEvidenceOutsideDiagnosisAndFrozenReplay(t *testing.T) {
	f, forge, p, instance := conversationFixture(t)
	activities := ActivityRegistry{}
	mustProgress(t, activities.Register(graphexec.ResearchLoopAction, GraphBugConversation{Profile: p, Reporter: f.opts.ResearchReporter}))
	_, err := ExecuteNodeAction(t.Context(), f.opts, GraphNodeRequest{Profile: p, Activities: activities, InstanceID: instance.ID, At: f.opts.MCP.Now()})
	mustProgress(t, err)
	source := completeConversationAnalystWith(t, f, instance, "done", 0, "Empty input safe", nil)
	_, err = ExecuteNodeAction(t.Context(), f.opts, GraphNodeRequest{Profile: p, Activities: activities, InstanceID: instance.ID, At: f.opts.MCP.Now()})
	mustProgress(t, err)
	diagnosis, _, err := f.db.CurrentGraphDiagnosis(t.Context(), instance.ID)
	mustProgress(t, err)
	wait, err := f.db.GetGraphOwnerWait(t.Context(), instance.ID)
	mustProgress(t, err)
	retained, err := f.db.GetReportDelivery(t.Context(), wait.DeliveryKey)
	mustProgress(t, err)
	if !strings.Contains(retained.Body, "Agents and reported models") || !strings.Contains(retained.Body, "not reported") || !strings.Contains(retained.Body, "claude") {
		t.Fatalf("missing analysis evidence: %s", retained.Body)
	}
	// Later evidence must not rewrite the published diagnosis or retained report.
	_, err = f.db.StartMeterAttempt(t.Context(), source.RunID, "claude")
	mustProgress(t, err)
	_, err = f.opts.ResearchReporter.RecoverGraphStopQuestion(t.Context(), wait.DeliveryKey)
	mustProgress(t, err)
	again, _, err := f.db.CurrentGraphDiagnosis(t.Context(), instance.ID)
	mustProgress(t, err)
	replay, err := f.db.GetReportDelivery(t.Context(), wait.DeliveryKey)
	mustProgress(t, err)
	if again.Digest != diagnosis.Digest || replay.Body != retained.Body || forge.attemptCount() != 1 {
		t.Fatal("evidence changed diagnosis or replayed publication")
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

func TestGraphExecutionEvidenceIncludesJudgeInvocationModels(t *testing.T) {
	f, _, p, instance := conversationFixture(t)
	activities := ActivityRegistry{}
	mustProgress(t, activities.Register(graphexec.ResearchLoopAction, GraphBugConversation{Profile: p, Reporter: f.opts.ResearchReporter}))
	_, err := ExecuteNodeAction(t.Context(), f.opts, GraphNodeRequest{Profile: p, Activities: activities, InstanceID: instance.ID, At: f.opts.MCP.Now()})
	mustProgress(t, err)
	completeConversationAnalystWith(t, f, instance, "done", 0, "Empty input safe", nil)
	source, err := f.db.ResolveJudgeSource(t.Context(), f.session.TaskRef, "")
	mustProgress(t, err)
	request, err := f.db.AdmitJudgeRequest(t.Context(), store.JudgeRequest{SessionID: f.session.ID, Task: f.session.TaskRef, TicketKey: "graph-execution", CommentID: "judge-models", Source: source, Configuration: []byte(`{}`), Evidence: []byte(`{}`)})
	mustProgress(t, err)
	judge, err := f.db.StartJudgeAttempt(t.Context(), request.ID, "codex", "configured-not-observed", "high")
	mustProgress(t, err)
	mustProgress(t, f.db.RecordJudgeInvocation(t.Context(), judge.ID, "native-model", "test-client"))
	meter, err := f.db.StartJudgeMeterAttempt(t.Context(), judge.ID, "codex")
	mustProgress(t, err)
	for i, model := range []string{"native-model", "child-model"} {
		id := fmt.Sprintf("judge-%d", i)
		parent := ""
		if i > 0 {
			parent = "judge-0"
		}
		output := int64(1)
		mustProgress(t, f.db.RecordMeterEvent(t.Context(), meter.ID, "codex", agent.UsageEvent{Invocation: &agent.UsageInvocation{ID: id, ParentID: parent, SourceID: "judge", SourceInvocationID: id, Coverage: agent.UsageCoverage{Status: agent.CollectionPartial}}}))
		mustProgress(t, f.db.RecordMeterEvent(t.Context(), meter.ID, "codex", agent.UsageEvent{Observation: &agent.UsageObservation{AttemptID: meter.ID, SourceID: "judge", EventKey: id, InvocationID: id, ActualModel: &model, ModelProvenance: "provider", Mode: agent.CounterIncremental, Scope: agent.UsageExclusive, Tokens: agent.TokenUsage{Output: &output}, Coverage: agent.UsageCoverage{Status: agent.CollectionPartial}}}))
	}
	entries, err := f.opts.ResearchReporter.executionGraphEvidence(t.Context(), instance.ID)
	mustProgress(t, err)
	body := report.RenderExecutions(entries, report.ExecutionReportLimit)
	for _, want := range []string{"advisory judge", "codex", "native-model", "child-model", "judge-0", "judge-1"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in %s", want, body)
		}
	}
	if strings.Contains(body, "configured-not-observed") {
		t.Fatal("configured model substituted for observed model")
	}
}
