package turn

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	ar "github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

func familySocketFixture(t *testing.T, db *store.DB, o FamilyDispatcherOptions) (submission.Client, *AttemptGuard, *AttemptAuthority) {
	t.Helper()
	digest, err := FamilyPermissionDigest(o)
	mustProgress(t, err)
	authority, err := NewAttemptAuthority(t.Context(), db)
	mustProgress(t, err)
	t.Cleanup(func() { mustProgress(t, authority.Close(context.WithoutCancel(t.Context()))) })
	guard, err := authority.Bind(t.Context(), o.Service.Binding(), digest, time.Now().Add(time.Hour))
	mustProgress(t, err)
	dispatcher, err := NewFamilyToolDispatcher(guard, o)
	mustProgress(t, err)
	dir, err := os.MkdirTemp("/tmp", "lf-family-")
	mustProgress(t, err)
	t.Cleanup(func() { mustProgress(t, os.RemoveAll(dir)) })
	dir, err = filepath.EvalSymlinks(dir)
	mustProgress(t, err)
	broker, err := submission.Start(t.Context(), submission.Options{Directory: dir, AllowTools: true, MaxWorkers: 4, RequestTimeout: 5 * time.Second, Handler: dispatcher.Handler})
	mustProgress(t, err)
	t.Cleanup(func() { mustProgress(t, broker.Close()) })
	return submission.Client{DescriptorPath: broker.DescriptorPath()}, guard, authority
}

func familyResearchOptions(t *testing.T, db *store.DB, service *AgentResultService) FamilyDispatcherOptions {
	t.Helper()
	receipts, err := NewReceiptService(db, service.Binding(), nil, nil)
	mustProgress(t, err)
	feedback, err := NewFeedbackService(service, nil)
	mustProgress(t, err)
	return FamilyDispatcherOptions{Service: service, Receipts: receipts, Feedback: feedback}
}

func TestFamilySocketProfilesAndRejectedReceipts(t *testing.T) {
	for _, mode := range []string{"research", "plan", "spike"} {
		t.Run(mode, func(t *testing.T) {
			db, _, opts := agentResultFixture(t, mode)
			service, err := BindAgentResultService(t.Context(), opts)
			mustProgress(t, err)
			o := familyResearchOptions(t, db, service)
			client, guard, _ := familySocketFixture(t, db, o)
			list, err := client.List(t.Context())
			mustProgress(t, err)
			var catalog struct {
				Tools []submission.ToolSpec `json:"tools"`
			}
			mustProgress(t, json.Unmarshal(list.Result, &catalog))
			names := []string{}
			for _, tool := range catalog.Tools {
				names = append(names, tool.Name)
			}
			expected := []string{"communication_preflight", "communication_submit", "communication_receipt"}
			if mode == "research" {
				expected = append(expected, "feedback_preflight", "receipts_get")
			}
			slices.Sort(names)
			slices.Sort(expected)
			if !reflect.DeepEqual(names, expected) {
				t.Fatalf("catalog=%v want=%v", names, expected)
			}
			for _, tool := range []string{"evidence_submit", "issues_create_batch", "unknown_tool"} {
				response, err := client.Call(t.Context(), tool, []byte(`{}`))
				mustProgress(t, err)
				if response.Error == "" {
					t.Fatalf("%s admitted", tool)
				}
				receipt, err := o.Receipts.Get(t.Context(), response.RequestID)
				mustProgress(t, err)
				if receipt.Disposition != "rejected" {
					t.Fatalf("denial not retained: %+v", receipt)
				}
			}
			for _, args := range []string{`{"kind":"application","input":{},"repository":"foreign"}`, `{"kind":"unknown","input":{}}`, `{"kind":"application"}`} {
				response, err := client.Call(t.Context(), "feedback_preflight", []byte(args))
				mustProgress(t, err)
				if response.Error == "" {
					t.Fatalf("malformed/forbidden admitted: %s", args)
				}
				receipt, err := o.Receipts.Get(t.Context(), response.RequestID)
				mustProgress(t, err)
				if receipt.Disposition != "rejected" {
					t.Fatalf("bad receipt: %+v", receipt)
				}
			}
			revisions, err := db.ResultRevisionNumbers(t.Context(), service.Binding())
			mustProgress(t, err)
			if len(revisions) != 0 {
				t.Fatal("denials wrote domain revisions")
			}
			if _, err := NewFamilyToolDispatcher(nil, o); err == nil {
				t.Fatal("missing binding accepted")
			}
			changed := o
			changed.CommentTargets = []int{99}
			if _, err := NewFamilyToolDispatcher(guard, changed); err == nil {
				t.Fatal("changed immutable targets accepted")
			}
		})
	}
}

func TestFamilySocketRevokedAuthority(t *testing.T) {
	for _, state := range []string{"cancelled", "closed", "replaced", "restart"} {
		t.Run(state, func(t *testing.T) {
			db, session, opts := agentResultFixture(t, "research")
			service, err := BindAgentResultService(t.Context(), opts)
			mustProgress(t, err)
			o := familyResearchOptions(t, db, service)
			client, guard, authority := familySocketFixture(t, db, o)
			switch state {
			case "cancelled", "closed":
				mustProgress(t, guard.Revoke(t.Context(), state))
			case "replaced":
				opts.Attempt = agentResultRun(t, db, session, "research", "new-attempt", 2)
				next, err := BindAgentResultService(t.Context(), opts)
				mustProgress(t, err)
				_, err = authority.Bind(t.Context(), next.Binding(), next.MCPPermissionDigest(), time.Now().Add(time.Hour))
				mustProgress(t, err)
			case "restart":
				fresh, err := NewAttemptAuthority(t.Context(), db)
				mustProgress(t, err)
				defer fresh.Close(context.WithoutCancel(t.Context()))
			}
			list, err := client.List(t.Context())
			mustProgress(t, err)
			if list.Error == "" {
				t.Fatal("stale discovery allowed")
			}
			for _, tool := range []string{"feedback_preflight", "evidence_submit", "receipts_get"} {
				args := []byte(`{"kind":"application","input":{}}`)
				if tool == "receipts_get" {
					args = mcpReceiptArgs(t, "missing")
				}
				r, err := client.Call(t.Context(), tool, args)
				mustProgress(t, err)
				if r.Error == "" {
					t.Fatalf("stale %s allowed", tool)
				}
			}
			revisions, err := db.ResultRevisionNumbers(t.Context(), service.Binding())
			mustProgress(t, err)
			if len(revisions) != 0 {
				t.Fatal("stale calls changed domain state")
			}
		})
	}
}

func TestFamilySocketCheckpointPreflightAndSubmit(t *testing.T) {
	f, _, session, options := submissionSessionFixture(t, store.WorkflowSeparated)
	b := session.CheckpointBinding()
	app, err := BindAgentResultService(t.Context(), AgentResultServiceOptions{DB: f.db, Attempt: store.ResultAttempt{Task: b.Parent, RunID: b.RunID, Attempt: b.Attempt, Mode: "implement", WorkContext: "family-checkpoint", SchemaVersion: 1, WorkflowVersion: 2, SubmissionVersion: 2}, Resolve: func(context.Context) (ar.Context, error) {
		return ar.Context{Mode: "implement", SchemaVersion: 1, WorkflowVersion: 2}, nil
	}})
	mustProgress(t, err)
	receipts, err := NewReceiptService(f.db, app.Binding(), nil, map[string]FamilyProjection{"submission": EvidenceReceiptProjection(f.db), "tool:evidence_submit": EvidenceReceiptProjection(f.db)})
	mustProgress(t, err)
	evidence, err := NewEvidenceService(session, receipts)
	mustProgress(t, err)
	feedback, err := NewFeedbackService(app, evidence)
	mustProgress(t, err)
	client, _, _ := familySocketFixture(t, f.db, FamilyDispatcherOptions{Service: app, Receipts: receipts, Evidence: evidence, Feedback: feedback})
	c := capturedContribution(t, options.Worktree, "US-1", "engineering")
	payload, err := json.Marshal(c)
	mustProgress(t, err)
	baseline, err := evidence.Preflight(t.Context(), payload)
	mustProgress(t, err)
	args, err := json.Marshal(map[string]any{"kind": "checkpoint", "input": json.RawMessage(payload)})
	mustProgress(t, err)
	response := mcpToolCall(t, client, "feedback_preflight", args)
	expected, err := json.Marshal(baseline)
	mustProgress(t, err)
	if string(response.Result) != string(expected) || !baseline.Valid {
		t.Fatalf("parity: %s want %s", response.Result, expected)
	}
	if _, err := f.db.EngineeringEvidence(t.Context(), b.Parent, 1, "US-1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("preflight captured evidence: %v", err)
	}
	// Successful preflight must not authorize later stale file content.
	mustProgress(t, os.WriteFile(filepath.Join(options.Worktree, "US-1.go"), []byte("package changed\n"), 0600))
	rejected := mcpToolCall(t, client, "evidence_submit", payload)
	var receipt FamilyReceipt
	mustProgress(t, json.Unmarshal(rejected.Result, &receipt))
	if receipt.States.Engineering == "recorded" || receipt.States.Input != "rejected" {
		t.Fatalf("submit skipped current snapshot validation: %+v", receipt)
	}
	recovered := mcpToolCall(t, client, "receipts_get", mcpReceiptArgs(t, rejected.RequestID))
	var again FamilyReceipt
	mustProgress(t, json.Unmarshal(recovered.Result, &again))
	if !reflect.DeepEqual(receipt, again) {
		t.Fatalf("receipt changed: %+v %+v", receipt, again)
	}
}

func TestFamilySocketValidateUsesRetainedCheckpoint(t *testing.T) {
	f, _, engineering, options := submissionSessionFixture(t, store.WorkflowSeparated)
	service, initialReceipts := evidenceFixtureService(t, f.db, engineering, "implement", store.WorkflowSeparated)
	c := capturedContribution(t, options.Worktree, "US-1", "engineering")
	evidenceSubmitFixture(t, service, initialReceipts, "prior-engineering", c)
	retained, err := f.db.EngineeringEvidence(t.Context(), f.session.TaskRef, 1, "US-1")
	mustProgress(t, err)
	event, err := agent.ParseStoryEvent(retained.Payload)
	mustProgress(t, err)
	mustProgress(t, engineering.Close(t.Context()))
	code := int64(0)
	_, err = f.db.FinishRun(t.Context(), options.RunID, store.RunOutcome{Status: store.RunSucceeded, ExitCode: &code})
	mustProgress(t, err)
	previous, err := f.db.GetRun(t.Context(), options.RunID)
	mustProgress(t, err)
	run, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: f.session.ID, SegmentID: previous.SegmentID, Mode: "validate", Agent: store.AgentClaude, TriggerCommentID: 2, LogPath: func(int) string { return "validate.log" }})
	mustProgress(t, err)
	validation := &agent.ValidationContext{Version: 1, Parent: f.session.TaskRef, Generation: 1, Stories: []agent.ValidationStory{{Story: options.Bundle.Stories[0], Criteria: []string{"AC-001"}, Snapshot: event.Evidence.ImplementationDigest, Evidence: event.Evidence, EvidenceStatus: "satisfied"}}}
	session, err := StartSubmissionSession(t.Context(), SubmissionSessionOptions{DB: f.db, Progress: f.opts.StoryProgress, Paths: f.opts.Paths, RunID: run.ID, Worktree: options.Worktree, Validation: validation})
	mustProgress(t, err)
	defer session.Close(t.Context())
	b := session.CheckpointBinding()
	app, err := BindAgentResultService(t.Context(), AgentResultServiceOptions{DB: f.db, Attempt: store.ResultAttempt{Task: b.Parent, RunID: b.RunID, Attempt: b.Attempt, Mode: "validate", WorkContext: "family-assessment", SchemaVersion: 1, WorkflowVersion: 2, SubmissionVersion: 2}, Resolve: func(context.Context) (ar.Context, error) {
		return ar.Context{Mode: "validate", SchemaVersion: 1, WorkflowVersion: 2}, nil
	}})
	mustProgress(t, err)
	receipts, err := NewReceiptService(f.db, app.Binding(), nil, map[string]FamilyProjection{"submission": EvidenceReceiptProjection(f.db), "tool:evidence_submit": EvidenceReceiptProjection(f.db)})
	mustProgress(t, err)
	evidence, err := NewEvidenceService(session, receipts)
	mustProgress(t, err)
	feedback, err := NewFeedbackService(app, evidence)
	mustProgress(t, err)
	client, _, _ := familySocketFixture(t, f.db, FamilyDispatcherOptions{Service: app, Receipts: receipts, Evidence: evidence, Feedback: feedback})
	list, err := client.List(t.Context())
	mustProgress(t, err)
	var catalog struct {
		Tools []submission.ToolSpec `json:"tools"`
	}
	mustProgress(t, json.Unmarshal(list.Result, &catalog))
	names := []string{}
	for _, tool := range catalog.Tools {
		names = append(names, tool.Name)
	}
	for _, name := range []string{"feedback_preflight", "receipts_get", "evidence_submit"} {
		if !slices.Contains(names, name) {
			t.Fatalf("Validate missing %s", name)
		}
	}
	// Fresh capture remains forbidden even when it references otherwise valid prior proof.
	c.Stage = "validate"
	c.Outcome = "assessed"
	raw, err := json.Marshal(c)
	mustProgress(t, err)
	response := mcpToolCall(t, client, "evidence_submit", raw)
	var denied FamilyReceipt
	mustProgress(t, json.Unmarshal(response.Result, &denied))
	if denied.States.Input != "rejected" {
		t.Fatalf("Validate capture accepted: %+v", denied)
	}
	contribution := agent.Contribution{Stage: "validate", Story: "US-1", Outcome: "assessed", Assessment: &agent.AssessmentContribution{SnapshotID: event.Evidence.ImplementationDigest, Criteria: []agent.ContributionFinding{{Name: "AC-001", Status: "satisfied", Classification: "blocking", Evidence: []agent.EvidenceExplanation{{CheckIDs: []string{event.Evidence.Tests[0].Name}, Summary: "Retained fixture execution."}}}}}}
	for _, name := range []string{"code", "architecture", "plan"} {
		contribution.Assessment.Reviews = append(contribution.Assessment.Reviews, agent.ContributionFinding{Name: name, Status: "satisfied", Classification: "blocking", Evidence: []agent.EvidenceExplanation{{CheckIDs: []string{}, Summary: "Retained fixture source."}}})
	}
	raw, err = json.Marshal(contribution)
	mustProgress(t, err)
	result := mcpToolCall(t, client, "evidence_submit", raw)
	var receipt FamilyReceipt
	mustProgress(t, json.Unmarshal(result.Result, &receipt))
	if receipt.States.Acceptance != "accepted" || receipt.States.Engineering == "recorded" {
		t.Fatalf("stage states: %+v", receipt)
	}
	retry := mcpToolCall(t, client, "evidence_submit", raw)
	if retry.RequestID != result.RequestID || string(retry.Result) != string(result.Result) {
		t.Fatal("assessment retry changed checkpoint")
	}
	after, err := f.db.EngineeringEvidence(t.Context(), f.session.TaskRef, 1, "US-1")
	mustProgress(t, err)
	if !reflect.DeepEqual(retained, after) {
		t.Fatal("Validate altered retained engineering evidence")
	}
	current, err := f.db.GetRun(t.Context(), run.ID)
	mustProgress(t, err)
	if current.Mode != "validate" {
		t.Fatal("evidence chained modes")
	}
}
