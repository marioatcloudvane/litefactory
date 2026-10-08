package turn

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

func durableActivityFixture(t *testing.T) (*ActivityService, submission.Client, *store.DB, store.Session, *ToolDispatcher) {
	t.Helper()
	db, session, opts := agentResultFixture(t, "implement")
	app, err := BindAgentResultService(t.Context(), opts)
	mustProgress(t, err)
	parent := "github:example/results#507"
	mustProgress(t, db.EnsureImplementStories(t.Context(), parent, 1, []store.ImplementStorySeed{{Key: "story-001", Branch: "lf/story-001"}}, time.Now()))
	_, err = db.TransitionImplementStory(t.Context(), store.ImplementTransition{Parent: parent, Generation: 1, Key: "story-001", From: store.ImplementWaiting, To: store.ImplementRunning, ExpectedRevision: 1, At: time.Now()})
	mustProgress(t, err)
	mustProgress(t, db.SetImplementWorkerSession(t.Context(), parent, 1, "story-001", session.ID, time.Now()))
	receipts, err := NewReceiptService(db, app.Binding(), nil, nil)
	mustProgress(t, err)
	feedback, err := newFeedbackServiceWithoutEvidence(app)
	mustProgress(t, err)
	o := FamilyDispatcherOptions{Service: app, Receipts: receipts, Feedback: feedback, WithoutEvidence: true}
	activity, err := NewActivityService(ActivityOptions{Receipts: o.Receipts, Now: time.Now()})
	mustProgress(t, err)
	o.Registrations = []FamilyRegistration{{Name: "activity_update", Owner: activity, Handle: activity.HandleUpdate}, {Name: "activity_get", Owner: activity, Handle: activity.HandleGet}}
	client, guard, _ := familySocketFixture(t, db, o)
	dispatcher, err := NewFamilyToolDispatcher(guard, o)
	mustProgress(t, err)
	return activity, client, db, session, dispatcher
}

func durableActivityArgs(seq int, kind, message string) []byte {
	raw, _ := json.Marshal(map[string]any{"sequence": seq, "kind": kind, "message": message})
	return raw
}
func TestDurableActivitySocketKindsRestartAndRetry(t *testing.T) {
	activity, client, db, _, _ := durableActivityFixture(t)
	missing := activityResponse(t, mcpToolCall(t, client, "activity_update", activityArgs(1, "working")))
	if missing.Code != "kind_required" {
		t.Fatalf("missing kind %+v", missing)
	}
	var last submission.Response
	for i, kind := range []string{"implementation", "testing", "repair"} {
		last = mcpToolCall(t, client, "activity_update", durableActivityArgs(i+1, kind, "done"))
		got := activityResponse(t, last)
		if got.Disposition != "accepted" || !got.Activity.Durable || got.Activity.Kind != kind {
			t.Fatalf("report %+v", got)
		}
		receipt, err := activity.receipts.Get(t.Context(), last.RequestID)
		mustProgress(t, err)
		if receipt.Disposition != "confirmed" || receipt.Reference.Kind != "activity" || receipt.Reference.ID < 1 || receipt.States != (ReceiptDimensions{}) || len(receipt.Publications) != 0 {
			t.Fatalf("receipt %+v", receipt)
		}
	}
	replay := mcpToolCall(t, client, "activity_update", durableActivityArgs(3, "repair", "done"))
	if replay.RequestID != last.RequestID || activityResponse(t, replay).Disposition != "replayed" {
		t.Fatalf("retry %+v", replay)
	}
	if got := activityResponse(t, mcpToolCall(t, client, "activity_update", durableActivityArgs(3, "testing", "done"))); got.Code != "sequence_conflict" {
		t.Fatalf("changed kind %+v", got)
	}
	// Reopen the actual database and bind a new service with the same immutable attempt.
	reopened, err := store.Open(t.Context(), db.Path())
	mustProgress(t, err)
	defer reopened.Close()
	receipts, err := NewReceiptService(reopened, activity.Binding(), nil, nil)
	mustProgress(t, err)
	fresh, err := NewActivityService(ActivityOptions{Receipts: receipts, Now: time.Now()})
	mustProgress(t, err)
	raw, err := fresh.HandleGet(t.Context(), "", []byte(`{}`))
	mustProgress(t, err)
	var view ActivityView
	mustProgress(t, json.Unmarshal(raw, &view))
	if !view.Durable || view.Kind != "repair" || view.Latest == nil || view.Latest.Message != "done" || view.AcceptedUpdates != 3 {
		t.Fatalf("restart %+v", view)
	}
	reports, err := reopened.LatestImplementProgress(t.Context(), "github:example/results#507", 1)
	mustProgress(t, err)
	if len(reports) != 3 {
		t.Fatalf("stored reports %+v", reports)
	}
	story, err := reopened.GetImplementStory(t.Context(), "github:example/results#507", 1, "story-001")
	mustProgress(t, err)
	if story.State != store.ImplementRunning || story.Revision != 2 {
		t.Fatalf("advisory changed story %+v", story)
	}
}

func TestDurableActivityPendingAndUncertainDispatcherRecovery(t *testing.T) {
	activity, _, _, _, dispatcher := durableActivityFixture(t)
	for i, state := range []string{"pending", "uncertain"} {
		args := durableActivityArgs(i+1, "testing", fmt.Sprintf("checking %s", state))
		requestID := fmt.Sprintf("lost-%s", state)
		_, _, err := activity.receipts.Begin(t.Context(), requestID, "activity_update", args)
		mustProgress(t, err)
		if state == "uncertain" {
			mustProgress(t, activity.receipts.Finish(t.Context(), requestID, "uncertain", store.FamilyReference{}))
		}
		payload, err := json.Marshal(submission.ToolCall{Name: "activity_update", Arguments: args})
		mustProgress(t, err)
		raw, err := dispatcher.Handler(t.Context(), submission.Request{Method: "tools/call", RequestID: requestID, Payload: payload})
		mustProgress(t, err)
		var response ActivityUpdateResponse
		mustProgress(t, json.Unmarshal(raw, &response))
		if response.Disposition != "accepted" {
			t.Fatalf("recovery %+v", response)
		}
		receipt, err := activity.receipts.Get(t.Context(), requestID)
		mustProgress(t, err)
		if receipt.Disposition != "confirmed" || receipt.Reference.Kind != "activity" {
			t.Fatalf("receipt %+v", receipt)
		}
	}
}

func TestActivityNonImplementKindRefusal(t *testing.T) {
	for _, mode := range []string{"research", "plan", "validate", "spike"} {
		t.Run(mode, func(t *testing.T) {
			db, _, opts := agentResultFixture(t, mode)
			app, err := BindAgentResultService(t.Context(), opts)
			mustProgress(t, err)
			receipts, err := NewReceiptService(db, app.Binding(), nil, nil)
			mustProgress(t, err)
			activity, err := NewActivityService(ActivityOptions{Receipts: receipts, Now: time.Now()})
			mustProgress(t, err)
			args := durableActivityArgs(1, "implementation", "work")
			_, _, err = receipts.Begin(t.Context(), "with-kind", "activity_update", args)
			mustProgress(t, err)
			raw, err := activity.HandleUpdate(t.Context(), "with-kind", args)
			mustProgress(t, err)
			var got ActivityUpdateResponse
			mustProgress(t, json.Unmarshal(raw, &got))
			if got.Code != "kind_not_supported" || got.Activity.Durable {
				t.Fatalf("kind refusal %+v", got)
			}
			args = activityArgs(1, "work")
			_, _, err = receipts.Begin(t.Context(), "no-kind", "activity_update", args)
			mustProgress(t, err)
			raw, err = activity.HandleUpdate(t.Context(), "no-kind", args)
			mustProgress(t, err)
			mustProgress(t, json.Unmarshal(raw, &got))
			if got.Disposition != "accepted" || got.Activity.Durable {
				t.Fatalf("memory behavior %+v", got)
			}
		})
	}
}
