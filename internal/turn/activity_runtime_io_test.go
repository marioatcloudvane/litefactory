package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

func activityRuntimePreparation(t *testing.T, mode string) (*preparation, *SubmissionSession) {
	t.Helper()
	if mode != "implement" && mode != "validate" {
		p, _ := lifecycleMCPPreparation(t, mode)
		return p, nil
	}
	f, _, ss, o := submissionSessionFixture(t, store.WorkflowSeparated)
	if mode == "validate" {
		mustProgress(t, ss.Close(t.Context()))
		zero := int64(0)
		_, err := f.db.FinishRun(t.Context(), o.RunID, store.RunOutcome{Status: store.RunSucceeded, ExitCode: &zero})
		mustProgress(t, err)
		prior, err := f.db.GetRun(t.Context(), o.RunID)
		mustProgress(t, err)
		next, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: f.session.ID, SegmentID: prior.SegmentID, Mode: mode, Agent: store.AgentClaude, TriggerCommentID: 2, LogPath: func(int) string { return "validate.log" }})
		mustProgress(t, err)
		o.RunID = next.ID
		ss, err = StartSubmissionSession(t.Context(), SubmissionSessionOptions{DB: f.db, Progress: f.opts.StoryProgress, Paths: f.opts.Paths, RunID: next.ID, Worktree: o.Worktree, Validation: &agent.ValidationContext{Version: 1, Parent: f.session.TaskRef, Generation: 1, Stories: []agent.ValidationStory{{Story: o.Bundle.Stories[0], Criteria: []string{"AC-001"}}}}})
		mustProgress(t, err)
		t.Cleanup(func() { mustProgress(t, ss.Close(context.WithoutCancel(t.Context()))) })
	}
	authority, err := NewAttemptAuthority(t.Context(), f.db)
	mustProgress(t, err)
	t.Cleanup(func() { mustProgress(t, authority.Close(context.WithoutCancel(t.Context()))) })
	ref, err := naming.ParseRef(f.session.TaskRef)
	mustProgress(t, err)
	return &preparation{ref: ref, session: f.session, run: store.Run{ID: o.RunID}, mcpRuntime: &MCPRuntime{Authority: authority, Paths: f.opts.Paths, Executable: "/factory/litefactory", Now: time.Now}}, ss
}

func activityRuntimeView(t *testing.T, client submission.Client) ActivityView {
	t.Helper()
	r := mcpToolCall(t, client, "activity_get", []byte(`{}`))
	if r.Error != "" {
		t.Fatal(r.Error)
	}
	var v ActivityView
	mustProgress(t, json.Unmarshal(r.Result, &v))
	return v
}
func activityRuntimeCleared(t *testing.T, p *preparedMCP) {
	t.Helper()
	v, err := p.activity.Lifecycle(ActivityLifecycle{Now: p.runtime.Now()})
	mustProgress(t, err)
	if v.Status != "cleared" || v.Latest != nil {
		t.Fatalf("retained activity %+v", v)
	}
	if !errors.Is(p.guard.Check(t.Context()), store.ErrResultAuthority) {
		t.Fatal("obsolete guard remains authorized")
	}
	if _, ok := p.runtime.activities[p]; ok {
		t.Fatal("terminal owner remains registered")
	}
}

func TestActivityRuntimeIO_ProductionModes(t *testing.T) {
	for _, mode := range []string{"research", "plan", "implement", "validate", "spike"} {
		t.Run(mode, func(t *testing.T) {
			prep, session := activityRuntimePreparation(t, mode)
			p, spec, err := prepareMCP(t.Context(), prep, agent.Request{Mode: mode}, session)
			mustProgress(t, err)
			t.Cleanup(func() { mustProgress(t, p.close(context.WithoutCancel(t.Context()))) })
			if p.activity == nil || len(prep.mcpRuntime.activities) != 1 {
				t.Fatal("production did not retain activity")
			}
			client := submission.Client{DescriptorPath: spec.DescriptorPath()}
			catalog, err := client.List(t.Context())
			mustProgress(t, err)
			if catalog.Error != "" {
				t.Fatal(catalog.Error)
			}
			var discovered struct {
				Tools []submission.ToolSpec `json:"tools"`
			}
			mustProgress(t, json.Unmarshal(catalog.Result, &discovered))
			names := make([]string, len(discovered.Tools))
			for i, tool := range discovered.Tools {
				names[i] = tool.Name
			}
			activityExposed := mode != "plan" && mode != "spike"
			hasActivity := slices.Contains(names, "activity_get") && slices.Contains(names, "activity_update")
			if !slices.Equal(names, spec.PermittedTools()) || hasActivity != activityExposed {
				t.Fatalf("catalog %v", names)
			}
			if len(p.guard.Lease().PermissionDigest) != 64 || submission.FamilyCatalogVersion != "families-v2" {
				t.Fatal("activity catalog missing version/digest")
			}
			before, err := prep.mcpRuntime.Authority.db.GetRun(t.Context(), prep.run.ID)
			mustProgress(t, err)
			if !activityExposed {
				// Plan and spike no longer carry the advisory activity family; the
				// service is still tracked internally but unreachable over MCP.
				for _, call := range []struct {
					name string
					args []byte
				}{{"activity_get", []byte(`{}`)}, {"activity_update", activityArgs(1, "blocked")}} {
					r, err := client.Call(t.Context(), call.name, call.args)
					mustProgress(t, err)
					if r.Error == "" {
						t.Fatalf("unpermitted %s admitted for %s", call.name, mode)
					}
				}
			} else {
				if v := activityRuntimeView(t, client); v.Status != "unavailable" {
					t.Fatalf("initial %+v", v)
				}
				for seq := 1; seq <= 2; seq++ {
					got := activityResponse(t, mcpToolCall(t, client, "activity_update", activityArgs(seq, fmt.Sprintf("work %d", seq))))
					if got.Disposition != "accepted" {
						t.Fatalf("update %+v", got)
					}
					v := activityRuntimeView(t, client)
					if v.HighestSequence != int64(seq) || v.Latest == nil || v.Latest.Message != fmt.Sprintf("work %d", seq) {
						t.Fatalf("cached or wrong-scope read %+v", v)
					}
				}
			}
			if mode == "plan" || mode == "spike" {
				for _, tool := range []string{"issues_comment", "issues_create_batch", "evidence_submit"} {
					r, err := client.Call(t.Context(), tool, []byte(`{}`))
					mustProgress(t, err)
					if r.Error == "" || slices.Contains(names, tool) {
						t.Fatalf("unexpected %s grant", tool)
					}
				}
			}
			after, err := prep.mcpRuntime.Authority.db.GetRun(t.Context(), prep.run.ID)
			mustProgress(t, err)
			revisions, err := prep.mcpRuntime.Authority.db.ResultRevisionNumbers(t.Context(), p.service.Binding())
			mustProgress(t, err)
			if !reflect.DeepEqual(before, after) || len(revisions) != 0 {
				t.Fatal("activity changed workflow completion")
			}
			mustProgress(t, p.endActivity(t.Context(), prep.mcpRuntime.Now()))
			activityRuntimeCleared(t, p)
			for _, call := range []struct {
				name string
				args []byte
			}{{"activity_get", []byte(`{}`)}, {"activity_update", activityArgs(2, "work 2")}, {"activity_update", activityArgs(3, "late")}} {
				r, err := client.Call(t.Context(), call.name, call.args)
				mustProgress(t, err)
				if r.Error == "" {
					t.Fatalf("obsolete %s admitted", call.name)
				}
			}
		})
	}
}

func TestActivityRuntimeIO_ReplacementExpiryAndShutdown(t *testing.T) {
	prep, _ := lifecycleMCPPreparation(t)
	clock := time.Now()
	prep.mcpRuntime.Now = func() time.Time { return clock }
	old, _, err := prepareMCP(t.Context(), prep, agent.Request{Mode: "research"}, nil)
	mustProgress(t, err)
	t.Cleanup(func() { mustProgress(t, old.close(context.WithoutCancel(t.Context()))) })
	replacement, spec, err := prepareMCP(t.Context(), prep, agent.Request{Mode: "research"}, nil)
	mustProgress(t, err)
	t.Cleanup(func() { mustProgress(t, replacement.close(context.WithoutCancel(t.Context()))) })
	activityRuntimeCleared(t, old)
	client := submission.Client{DescriptorPath: spec.DescriptorPath()}
	update := mcpToolCall(t, client, "activity_update", activityArgs(1, "retained receipt"))
	activityResponse(t, update)
	receipt, err := replacement.activity.receipts.Get(t.Context(), update.RequestID)
	mustProgress(t, err)
	mustProgress(t, old.close(t.Context()))
	if v := activityRuntimeView(t, client); v.HighestSequence != 1 {
		t.Fatal("late predecessor exit cleared successor")
	}
	clock = replacement.guard.Lease().ExpiresAt
	mustProgress(t, prep.mcpRuntime.CleanActivities(t.Context(), clock))
	activityRuntimeCleared(t, replacement)
	reopened, err := store.Open(t.Context(), prep.mcpRuntime.Authority.db.Path())
	mustProgress(t, err)
	defer reopened.Close()
	receipts, err := NewReceiptService(reopened, replacement.service.Binding(), nil, nil)
	mustProgress(t, err)
	retained, err := receipts.Get(t.Context(), update.RequestID)
	mustProgress(t, err)
	if !reflect.DeepEqual(receipt, retained) {
		t.Fatal("cleanup destroyed durable receipt")
	}
	// Return the factory clock to current wall time for a fresh lease. The prior
	// immutable lease stays revoked; this creates a different result attempt.
	clock = time.Now()
	current, _, err := prepareMCP(t.Context(), prep, agent.Request{Mode: "research"}, nil)
	mustProgress(t, err)
	t.Cleanup(func() { mustProgress(t, current.close(context.WithoutCancel(t.Context()))) })
	mustProgress(t, prep.mcpRuntime.Close(t.Context()))
	activityRuntimeCleared(t, current)
	if err := prep.mcpRuntime.trackActivity(t.Context(), current, clock); !errors.Is(err, store.ErrResultAuthority) {
		t.Fatalf("shutdown reopened: %v", err)
	}
}

func TestActivityRuntimeIO_RunnerTermination(t *testing.T) {
	for _, tc := range []struct {
		name    string
		outcome agent.Outcome
		cancel  bool
		text    string
		want    agent.Outcome
	}{
		{"success", agent.OutcomeSucceeded, false, string(resultCreate("research")), agent.OutcomeSucceeded},
		{"failure", agent.OutcomeFailed, false, "", agent.OutcomeFailed},
		{"cancellation", agent.OutcomeSucceeded, true, "", agent.OutcomeAbandoned},
		{"interruption", agent.OutcomeInterrupted, false, "", agent.OutcomeInterrupted},
		{"missing output", agent.OutcomeSucceeded, false, "", agent.OutcomeFailed},
		{"malformed output", agent.OutcomeSucceeded, false, "{", agent.OutcomeFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prep, db := lifecycleMCPPreparation(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var captured *preparedMCP
			var receiptID string
			prep.runner = lifecycleMCPHost(func(ctx context.Context, req agent.Request) (*agent.RunResult, error) {
				for p := range prep.mcpRuntime.activities {
					captured = p
				}
				if captured == nil {
					t.Fatal("runner has no retained activity")
				}
				client := submission.Client{DescriptorPath: req.MCPLaunch.DescriptorPath()}
				response := mcpToolCall(t, client, "activity_update", activityArgs(1, "working"))
				activityResponse(t, response)
				receiptID = response.RequestID
				if tc.cancel {
					cancel()
				}
				return &agent.RunResult{Outcome: tc.outcome, Reason: "original process cause", ResultText: tc.text}, nil
			})
			result, err := runSubmissionBound(ctx, prep, agent.Request{Mode: "research"})
			mustProgress(t, err)
			if result.Outcome != tc.want {
				t.Fatalf("outcome %+v", result)
			}
			if (tc.outcome == agent.OutcomeFailed || tc.outcome == agent.OutcomeInterrupted) && result.Reason != "original process cause" {
				t.Fatal("original cause lost")
			}
			activityRuntimeCleared(t, captured)
			next, _, err := prepareMCP(t.Context(), prep, agent.Request{Mode: "research"}, nil)
			mustProgress(t, err)
			defer func() { mustProgress(t, next.close(context.WithoutCancel(t.Context()))) }()
			receipt, err := next.activity.receipts.Get(t.Context(), receiptID)
			mustProgress(t, err)
			if receipt.Disposition != "confirmed" {
				t.Fatalf("checkpoint receipt %+v", receipt)
			}
			run, err := db.GetRun(t.Context(), prep.run.ID)
			mustProgress(t, err)
			if run.Status == store.RunSucceeded {
				t.Fatal("activity set run completion")
			}
		})
	}
}

func TestActivityRuntimeIO_BoundedOwnersAndCancelledCleanup(t *testing.T) {
	prep, db := lifecycleMCPPreparation(t)
	repo, err := db.GetRepo(t.Context(), prep.session.RepoID)
	mustProgress(t, err)
	owners := make([]*preparedMCP, 0, maxRuntimeActivities)
	t.Cleanup(func() {
		for _, p := range owners {
			mustProgress(t, p.close(context.WithoutCancel(t.Context())))
		}
	})
	for i := 0; i <= maxRuntimeActivities; i++ {
		next := *prep
		next.session, err = db.GetOrCreateSession(t.Context(), repo, 1000+i)
		mustProgress(t, err)
		next.ref, err = naming.ParseRef(next.session.TaskRef)
		mustProgress(t, err)
		next.run.ID = agentResultRun(t, db, next.session, "research", fmt.Sprintf("bound-%d", i), 1).RunID
		p, _, err := prepareMCP(t.Context(), &next, agent.Request{Mode: "research"}, nil)
		if i == maxRuntimeActivities {
			if err == nil || p != nil {
				t.Fatal("unbounded runtime owner registration")
			}
			break
		}
		mustProgress(t, err)
		owners = append(owners, p)
	}
	if len(prep.mcpRuntime.activities) != maxRuntimeActivities {
		t.Fatal("wrong capacity")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := prep.mcpRuntime.CleanActivities(ctx, time.Now()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cleanup ignored cancellation: %v", err)
	}
	if len(prep.mcpRuntime.activities) != maxRuntimeActivities {
		t.Fatal("cancelled cleanup mutated owners")
	}
	// Cancel just one attempt; cleanup is factory-scheduled, with no sleeps.
	owners[0].guard.cancel()
	mustProgress(t, prep.mcpRuntime.CleanActivities(t.Context(), time.Now()))
	activityRuntimeCleared(t, owners[0])
	if len(prep.mcpRuntime.activities) != maxRuntimeActivities-1 {
		t.Fatal("cleanup removed unrelated owners")
	}
	for _, p := range owners[1:] {
		mustProgress(t, p.guard.Check(t.Context()))
	}
	mustProgress(t, prep.mcpRuntime.Close(t.Context()))
	if len(prep.mcpRuntime.activities) != 0 {
		t.Fatal("shutdown leaked owners")
	}
}

func TestActivityRuntimeIO_CleanupPreservesUncertainPublication(t *testing.T) {
	f := commentHTTPSetup(t, false)
	f.lost = true
	reply, err := f.client.Call(t.Context(), "issues_comment", []byte(`{"issue_number":284,"message":"status"}`))
	mustProgress(t, err)
	if reply.Error == "" {
		t.Fatal("lost publication reply became success")
	}
	before, err := f.db.PendingCommentOperations(t.Context(), 0, "", "", 1)
	mustProgress(t, err)
	if len(before) != 1 {
		t.Fatal("missing durable uncertain intent")
	}
	authority, err := NewAttemptAuthority(t.Context(), f.db)
	mustProgress(t, err)
	session, err := f.db.GetSessionByTaskRef(t.Context(), f.receipts.Binding().Task)
	mustProgress(t, err)
	ref, err := naming.ParseRef(session.TaskRef)
	mustProgress(t, err)
	// Reuse the actual publication database, with a fresh startup authority and
	// result attempt, to test runtime cleanup rather than display-only cleanup.
	template, _ := lifecycleMCPPreparation(t)
	runtime := &MCPRuntime{Authority: authority, Paths: template.mcpRuntime.Paths, Executable: "/factory/litefactory", Now: time.Now}
	prep := &preparation{ref: ref, session: session, run: store.Run{ID: f.receipts.Binding().RunID}, mcpRuntime: runtime}
	p, _, err := prepareMCP(t.Context(), prep, agent.Request{Mode: "research"}, nil)
	mustProgress(t, err)
	defer func() { mustProgress(t, p.close(context.WithoutCancel(t.Context()))) }()
	mustProgress(t, runtime.Close(t.Context()))
	activityRuntimeCleared(t, p)
	reopened, err := store.Open(t.Context(), f.db.Path())
	mustProgress(t, err)
	defer reopened.Close()
	after, err := reopened.PendingCommentOperations(t.Context(), 0, "", "", 1)
	mustProgress(t, err)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("shutdown altered pending publication intent")
	}
	f.mu.Lock()
	body := f.body
	data, err := json.Marshal([]any{map[string]any{"id": 555, "body": body, "user": map[string]any{"id": 9}}})
	mustProgress(t, err)
	f.observation = string(data)
	f.mu.Unlock()
	observed, err := f.service.ReconcileComment(t.Context(), f.receipts.scope, reply.RequestID, 1, func(context.Context, store.CommentOperation) error { return nil })
	mustProgress(t, err)
	if observed.State != "confirmed" {
		t.Fatalf("recovery after cleanup %+v", observed)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.posts != 1 || f.observations != 1 {
		t.Fatalf("redispatched after cleanup: posts=%d observations=%d", f.posts, f.observations)
	}
}
