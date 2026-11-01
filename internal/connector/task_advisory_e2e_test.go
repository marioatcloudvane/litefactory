package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/cloudvane/litefactory/internal/taskdiscussion"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
)

// seedAdvisoryReview retains actual workflow policy, including an owner approval
// and a displayed review wait, so the private journey can detect workflow writes.
func seedAdvisoryReview(t *testing.T, db *store.DB, task string) {
	t.Helper()
	state := planbrief.PlanState{
		SharedRevision: "shared-advisory-basis",
		Stories: []planbrief.PlanStoryState{{
			Key: "story-001", Stage: "technical", Candidate: "candidate-saved",
			Draft:     planbrief.PlanDraft{Key: "story-001", Name: "Explain a saved architecture"},
			Approvals: map[string]string{}, Effect: "applied",
		}},
	}
	approval, err := state.ReviewRevision("story-001", "story")
	if err != nil {
		t.Fatal(err)
	}
	state.Stories[0].Approvals["story"] = approval
	review, err := state.ReviewRevision("story-001", "technical")
	if err != nil {
		t.Fatal(err)
	}
	state.Stories[0].Wait = &planbrief.PlanWait{
		ID: "technical-owner-wait", Stage: "technical", Candidate: "candidate-saved",
		Revision: review, Kind: "review", IssueID: 1007,
	}
	if _, err := db.SavePlanWorkflowState(t.Context(), task, 0, state); err != nil {
		t.Fatal(err)
	}
}

func advisoryWorkflowSnapshot(t *testing.T, db *store.DB, task string) string {
	t.Helper()
	session, err := db.GetSessionByTaskRef(t.Context(), task)
	if err != nil {
		t.Fatal(err)
	}
	state, revision, err := db.GetPlanWorkflowState(t.Context(), task)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := json.Marshal(struct {
		Session  store.Session
		State    planbrief.PlanState
		Revision int64
	}{session, state, revision})
	if err != nil {
		t.Fatal(err)
	}
	return string(snapshot)
}

// advisoryJourneyCapture stands in for repository/evidence I/O. It reads the
// actual saved review and returns that immutable basis to the production service.
type advisoryJourneyCapture struct{ db *store.DB }

func (c advisoryJourneyCapture) Capture(ctx context.Context, task, operation string) (taskdiscussion.Basis, error) {
	state, _, err := c.db.GetPlanWorkflowState(ctx, task)
	if err != nil {
		return taskdiscussion.Basis{}, err
	}
	target, err := c.db.ResolveConnectorDiscussionTarget(ctx, task)
	if err != nil {
		return taskdiscussion.Basis{}, err
	}
	revision, err := state.ReviewRevision("story-001", "technical")
	if err != nil {
		return taskdiscussion.Basis{}, err
	}
	evidence, err := json.Marshal(state.Stories[0].Draft)
	if err != nil {
		return taskdiscussion.Basis{}, err
	}
	return taskdiscussion.Basis{TaskRef: task, RepositoryRef: target.RepositoryRef, Stage: state.Stories[0].Stage, Role: "plan-technical", Revision: revision, Commit: "0123456789012345678901234567890123456789", Evidence: evidence, Guidance: json.RawMessage(`{"fixture":"isolated-read-only"}`), Sources: []string{"saved-plan:" + revision}, Gaps: []string{"original_session_unavailable"}}, nil
}

type advisoryJourneyRunner struct{ requests []taskdiscussion.RunRequest }

func (r *advisoryJourneyRunner) Run(_ context.Context, in taskdiscussion.RunRequest) (taskdiscussion.Answer, error) {
	r.requests = append(r.requests, in)
	return taskdiscussion.Answer{Answer: "The saved architecture keeps analysis separate from workflow changes.", Sources: in.Basis.Sources, Gaps: in.Basis.Gaps}, nil
}
func (*advisoryJourneyRunner) Recover(context.Context, taskdiscussion.RunRequest) (taskdiscussion.Answer, bool, error) {
	return taskdiscussion.Answer{}, false, nil
}

// TestTaskAdvisoryPrivateFollowupJourney crosses authenticated MCP, repository
// consent, durable admission, the worker and private result recovery. Repository
// I/O and paid provider execution are controlled; no live external service runs.
func TestTaskAdvisoryPrivateFollowupJourney(t *testing.T) {
	f := newConnectorFixture(t, nil)
	target := seedDiscussionTarget(t, f.db)
	seedAdvisoryReview(t, f.db, target.TaskRef)
	before := advisoryWorkflowSnapshot(t, f.db, target.TaskRef)
	runner := &advisoryJourneyRunner{}
	service, err := taskdiscussion.New(taskdiscussion.Options{DB: f.db, Now: func() time.Time { return f.now }, Capture: advisoryJourneyCapture{f.db}, Runner: runner, Protect: func(_ context.Context, value string) (string, error) { return value, nil }, Deadline: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	f.options.TaskDiscussion = service
	handler := f.server(t)
	client := registerConnectorClient(t, handler, "private discussion client", "none")
	token, _ := issueDiscussionTokens(t, f, client, target, AdvisoryScope)
	call := func(name string, args map[string]any) map[string]any {
		t.Helper()
		response := connectorJSON(t, handler, f.options.Config.ResourceURL, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}}, connectorBearer(token))
		var envelope struct {
			Error  any `json:"error"`
			Result struct {
				IsError bool           `json:"isError"`
				Value   map[string]any `json:"structuredContent"`
			} `json:"result"`
		}
		connectorDecode(t, response, &envelope)
		if response.Code != http.StatusOK || envelope.Error != nil || envelope.Result.IsError || envelope.Result.Value == nil {
			t.Fatalf("%s: HTTP %d %s", name, response.Code, response.Body.String())
		}
		return envelope.Result.Value
	}
	firstArgs := map[string]any{"task_ref": target.TaskRef, "question": "Ask whoever worked on this why analysis is separate.", "client_request_key": "private-question-one"}
	admitted := call("ask_task_agent", firstArgs)
	job := journeyValue[string](t, admitted["job_ref"])
	conversation := journeyValue[string](t, admitted["conversation_ref"])
	if admitted["state"] != "queued" || runner.requests != nil {
		t.Fatalf("admission did not return before paid execution: %+v", admitted)
	}
	replay := call("ask_task_agent", firstArgs)
	if replay["job_ref"] != job {
		t.Fatal("lost admission reply created different work")
	}
	if ran, err := service.RunOnce(t.Context()); err != nil || !ran {
		t.Fatalf("first worker: %v %v", ran, err)
	}
	// Recompose the HTTP boundary to represent reconnecting with retained references.
	handler = f.server(t)
	saved := call("advisory_job_get", map[string]any{"job_ref": job})
	answer := journeyValue[map[string]any](t, saved["result"])
	if saved["state"] != "succeeded" || saved["freshness"] != "current" || answer["answer"] != "The saved architecture keeps analysis separate from workflow changes." {
		t.Fatalf("private answer unavailable: %+v", saved)
	}
	if len(journeyValue[[]any](t, answer["sources"])) != 1 || len(journeyValue[[]any](t, answer["gaps"])) != 1 {
		t.Fatalf("answer omitted sources or missing context: %+v", answer)
	}
	firstBasis := journeyValue[map[string]any](t, saved["captured_basis"])
	if firstBasis["stage"] != "technical" || firstBasis["revision"] == "" || firstBasis["commit"] == "" {
		t.Fatalf("answer basis is incomplete: %+v", firstBasis)
	}
	followup := call("ask_task_agent", map[string]any{"task_ref": target.TaskRef, "question": "How does that protect the pending review?", "client_request_key": "private-question-two", "conversation_ref": conversation, "conversation_revision": saved["conversation_revision"]})
	if followup["conversation_ref"] != conversation {
		t.Fatal("follow-up escaped its conversation")
	}
	if ran, err := service.RunOnce(t.Context()); err != nil || !ran {
		t.Fatalf("follow-up worker: %v %v", ran, err)
	}
	second := call("advisory_job_get", map[string]any{"job_ref": followup["job_ref"]})
	if second["state"] != "succeeded" || !reflect.DeepEqual(second["captured_basis"], saved["captured_basis"]) {
		t.Fatalf("follow-up lost recorded basis: %+v", second)
	}
	if len(runner.requests) != 2 || runner.requests[0].ExecutionID == "" || runner.requests[0].ExecutionID == runner.requests[1].ExecutionID {
		t.Fatalf("execution identities are not durable and distinct: %+v", runner.requests)
	}
	history := runner.requests[1].Context
	if !bytes.Contains(history, []byte(journeyValue[string](t, firstArgs["question"]))) || !bytes.Contains(history, []byte(journeyValue[string](t, answer["answer"]))) || len(history) > 32768 {
		t.Fatalf("follow-up did not receive its bounded exchange: %s", history)
	}
	if after := advisoryWorkflowSnapshot(t, f.db, target.TaskRef); after != before {
		t.Fatalf("private discussion changed task workflow:\nbefore %s\nafter  %s", before, after)
	}
	// No publisher was installed: the answer and follow-up have no publication path.
	if ran, err := service.RunOnce(t.Context()); err != nil || ran {
		t.Fatalf("reconnect or retrieval repeated provider execution: %v %v", ran, err)
	}
}
