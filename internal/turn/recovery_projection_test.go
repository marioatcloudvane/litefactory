package turn

import (
	"context"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
	"strings"
	"testing"
	"time"
)

func TestRecoveryProjectionUsesAffectedRunOrdinal(t *testing.T) {
	var f *fixture
	f = newFixture(t, newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		return succeeded(req, f.paths.Home()), nil
	}))
	f.execute(f.request())
	runs := f.runs()
	out := &report.StopRecovery{}
	if err := projectRecoveryRun(t.Context(), f.db, f.session.ID, runs[0].ID, out); err != nil {
		t.Fatal(err)
	}
	if out.Route != "inspect-run" || !strings.Contains(out.NextStep, "--run 1") || !strings.Contains(out.NextStep, f.session.TaskRef) {
		t.Fatalf("projection = %+v", out)
	}
	out = &report.StopRecovery{NextStep: "inspect missing evidence"}
	if err := projectRecoveryRun(t.Context(), f.db, f.session.ID, runs[0].ID+999, out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Unresolved, "unknown") || out.NextStep != "inspect missing evidence" {
		t.Fatalf("missing projection = %+v", out)
	}
}

func TestRecoveryProjectionDoesNotAssignPriorDispatchToCurrentStep(t *testing.T) {
	f, _, _, instance := persistentStopReporterFixture(t)
	at := time.Now().UTC()
	_, err := f.db.ParkGraphInstance(t.Context(), instance.ID, "analyst", at, at)
	mustProgress(t, err)
	attempt, err := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: instance.ID, Node: "analyst", FactoryStep: true, StartedAt: at})
	mustProgress(t, err)
	mustProgress(t, f.db.CompleteGraphNodeAttempt(t.Context(), attempt.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "done", FinishedAt: at.Add(time.Second), Transition: &store.GraphTransition{InstanceID: instance.ID, FromNode: "analyst", ToNode: "next", OnOutcome: "done", At: at.Add(time.Second)}}))
	projection, err := ProjectGraphRecovery(t.Context(), f.db, f.session.ID)
	mustProgress(t, err)
	text := report.RenderStopRecovery(projection)
	if projection.Step != "next" || projection.Disposition != store.GraphRunning || strings.Contains(text, "next`: pending") || !strings.Contains(text, "latest dispatch belongs to step analyst") || !strings.Contains(text, "litefactory logs") {
		t.Fatalf("prior dispatch misattributed: %s", text)
	}
}

func TestGraphRecoveryFinalReportsUseRetainedWorkDisposition(t *testing.T) {
	for _, state := range []string{"pending", "running", "missing-liveness", "cancelled-known-result"} {
		t.Run(state, func(t *testing.T) {
			f, forge, _, instance := persistentStopReporterFixture(t)
			at := time.Now().UTC()
			dispatch, err := f.db.ParkGraphInstance(t.Context(), instance.ID, "analyst", at, at)
			mustProgress(t, err)
			switch state {
			case "running", "cancelled-known-result":
				_, reserved, err := f.db.ReserveGraphDispatch(t.Context(), dispatch.ID, at)
				mustProgress(t, err)
				if !reserved {
					t.Fatal("dispatch not reserved")
				}
				segment, err := f.db.GetOrCreateSegment(t.Context(), f.session.ID, 1, "graph-guidance", store.AgentClaude)
				mustProgress(t, err)
				run, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: f.session.ID, SegmentID: segment.ID, Workflow: "bug", Agent: store.AgentClaude, TriggerCommentID: dispatch.TriggerID(), StartedAt: at, LogPath: func(int) string { return "graph-guidance.log" }})
				mustProgress(t, err)
				if state == "cancelled-known-result" {
					_, err = f.db.FinishRun(t.Context(), run.ID, store.RunOutcome{Status: store.RunSucceeded, FinishedAt: at.Add(time.Second)})
					mustProgress(t, err)
					mustProgress(t, f.db.SetGraphInstanceStatus(t.Context(), instance.ID, store.GraphCancelled, time.Time{}, at.Add(time.Second)))
				}
			case "missing-liveness":
				mustProgress(t, f.db.CancelGraphDispatch(t.Context(), dispatch.ID, "admission unavailable", at))
				_, err := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: instance.ID, Node: "analyst", FactoryStep: true, StartedAt: at})
				mustProgress(t, err)
			}
			projection, err := ProjectGraphRecovery(t.Context(), f.db, f.session.ID)
			mustProgress(t, err)
			reporter := newReporter(t, f, forge, nil)
			ref := f.ref()
			intent := reporter.deliveryIntent(ref, ref, f.session.ID, dispatch.TriggerID(), 0, "graph-guidance-evidence")
			_, err = reporter.deliverRecoveryProjection(t.Context(), intent, projection, "")
			mustProgress(t, err)
			body := forge.only(t).body
			t.Logf("Final graph report (%s):\n%s", state, body)
			if !strings.Contains(body, "Work at `analyst`") {
				t.Fatal(body)
			}
			if state == "pending" || state == "running" {
				if !strings.Contains(body, "Please wait") || !strings.Contains(body, "is "+state) || strings.Contains(body, "litefactory logs") {
					t.Fatal(body)
				}
			} else {
				if strings.Count(body, "litefactory logs") != 1 || !strings.Contains(body, f.session.TaskRef) || strings.Contains(body, "Please wait") {
					t.Fatal(body)
				}
				if state == "missing-liveness" && (!strings.Contains(body, "no run record") || !strings.Contains(body, "admission unavailable")) {
					t.Fatal(body)
				}
				if state == "cancelled-known-result" && (!strings.Contains(body, "cancelled") || !strings.Contains(body, "Run 1 has retained status succeeded") || !strings.Contains(body, "--run 1")) {
					t.Fatal(body)
				}
			}
		})
	}
}
