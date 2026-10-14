package turn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

func persistentStopReporterFixture(t *testing.T) (*fixture, *fakeForge, *graphexec.Profile, store.GraphInstance) {
	t.Helper()
	f := newFixture(t, nil)
	_, err := f.db.AdmitWorkflow(t.Context(), store.TaskWorkflowAdmission{SessionID: f.session.ID, Version: 1, Family: "standalone", Workflow: "bug"})
	mustProgress(t, err)
	profile := &graphexec.Profile{Team: "builtin", Workflow: agent.WorkflowBug, ProfileVersion: 1, Digest: strings.Repeat("d", 64), Nodes: []graphexec.Node{{Name: "analyst", Action: ResearchAgentAction, Limit: 1, Attrs: map[string]string{"lf_park": "owner", "lf_stop": "blocked,failed,invalid,exhausted", "lf_response": "result"}}}}
	def, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: profile.Team, Workflow: "bug", ProfileVersion: 1, Path: "bug.dot", Digest: profile.Digest, CreatedAt: time.Now().UTC()})
	mustProgress(t, err)
	instance, err := f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: f.session.ID, DefinitionID: def.ID, CurrentNode: "analyst", StartedAt: time.Now().UTC(), NextDueAt: time.Now().UTC()})
	mustProgress(t, err)
	return f, newFakeForge(t), profile, instance
}

func retainPersistentStopQuestion(t *testing.T, f *fixture, instance store.GraphInstance) string {
	t.Helper()
	question := GraphStopQuestion{Version: 1, InstanceID: instance.ID, Origin: "analyst", Cause: "exhausted", Reason: "Finite budget spent; retain the prior findings."}
	raw, err := json.Marshal(question)
	mustProgress(t, err)
	digest := sha256.Sum256(raw)
	key := hex.EncodeToString(digest[:])
	mustProgress(t, f.db.ParkGraphNode(t.Context(), store.GraphStop{Key: key, InstanceID: instance.ID, Origin: question.Origin, Cause: question.Cause, Reason: question.Reason, Candidate: raw, At: time.Now().UTC()}))
	return key
}

func reopenPersistentStopFixture(t *testing.T, f *fixture) {
	t.Helper()
	// Preserve the fixture-owned DB pointer so its cleanup and new reporter use
	// the reopened real database rather than an abandoned connection.
	reopened, err := store.Open(t.Context(), f.paths.DBPath())
	mustProgress(t, err)
	*f.db = *reopened
}

func TestGraphStopPersistentReporterLostAcknowledgment(t *testing.T) {
	f, forge, _, instance := persistentStopReporterFixture(t)
	key := retainPersistentStopQuestion(t, f, instance)
	reporter := newReporter(t, f, forge, nil)
	forge.afterPost = func() { mustProgress(t, f.db.Close()) }
	posted, err := reporter.RecoverGraphStopQuestion(t.Context(), key)
	if err == nil || posted.CommentID == 0 {
		t.Fatalf("lost persistence acknowledgment hidden: posted=%+v err=%v", posted, err)
	}
	original := forge.only(t).body
	reopenPersistentStopFixture(t, f)
	forge.afterPost = nil
	before, err := f.db.GetReportDelivery(t.Context(), key)
	mustProgress(t, err)
	if !before.PossiblySent || before.Completed || before.Body != original {
		t.Fatalf("publication debt lost: %+v", before)
	}
	reporter = newReporter(t, f, forge, nil)
	recovered, err := reporter.RecoverGraphStopQuestion(t.Context(), key)
	mustProgress(t, err)
	if recovered.CommentID != posted.CommentID || forge.attemptCount() != 1 {
		t.Fatalf("recovery repeated uncertain post: %+v posts=%d", recovered, forge.attemptCount())
	}
	after, err := f.db.GetReportDelivery(t.Context(), key)
	mustProgress(t, err)
	if !after.Completed || after.Body != original || after.Key != key {
		t.Fatalf("immutable delivery changed: %+v", after)
	}
	wait, err := f.db.GetGraphOwnerWait(t.Context(), instance.ID)
	mustProgress(t, err)
	if wait.State != "waiting" || wait.SourceAttemptID != 0 || wait.OriginNode != "analyst" {
		t.Fatalf("publication released source-less work: %+v", wait)
	}
	attempts, err := f.db.ListGraphNodeAttempts(t.Context(), instance.ID)
	mustProgress(t, err)
	if len(attempts) != 0 {
		t.Fatalf("publication invented agent dispatch: %+v", attempts)
	}
	if _, err = reporter.RecoverWorkflowComment(t.Context(), key, agentresult.Context{}, BugWorkflowPolicy()); err != nil {
		t.Fatal(err)
	}
	if forge.attemptCount() != 1 {
		t.Fatal("completed recovery reposted")
	}
}

func TestGraphStopPersistentReporterRefusalRetainsDebt(t *testing.T) {
	f, forge, _, instance := persistentStopReporterFixture(t)
	key := retainPersistentStopQuestion(t, f, instance)
	forge.failWith(503)
	_, err := newReporter(t, f, forge, nil).RecoverGraphStopQuestion(t.Context(), key)
	if err == nil {
		t.Fatal("publication failure hidden")
	}
	before, err := f.db.GetReportDelivery(t.Context(), key)
	mustProgress(t, err)
	if before.Completed || !before.PossiblySent || before.CommentID != 0 {
		t.Fatalf("failure manufactured publication: %+v", before)
	}
	mustProgress(t, f.db.Close())
	reopenPersistentStopFixture(t, f)
	forge.recover()
	_, err = newReporter(t, f, forge, nil).RecoverGraphStopQuestion(t.Context(), key)
	if err == nil {
		t.Fatal("unknown publication nonexecution blindly retried")
	}
	if forge.attemptCount() != 1 {
		t.Fatalf("uncertain refusal repeated mutation: %d", forge.attemptCount())
	}
	after, err := f.db.GetReportDelivery(t.Context(), key)
	mustProgress(t, err)
	if after.Completed || after.Body != before.Body {
		t.Fatalf("uncertainty erased: %+v", after)
	}
}

func TestGraphStopPersistentOrphanRecoveryParksWithoutDispatch(t *testing.T) {
	f, forge, profile, instance := persistentStopReporterFixture(t)
	segment, err := f.db.GetOrCreateSegment(t.Context(), f.session.ID, 1, "bug-results", store.AgentClaude)
	mustProgress(t, err)
	run, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: f.session.ID, SegmentID: segment.ID, Workflow: "bug", Agent: store.AgentClaude, TriggerCommentID: 5, LogPath: func(int) string { return "orphan.log" }})
	mustProgress(t, err)
	at := time.Now().UTC()
	attempt, err := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: instance.ID, Node: "analyst", RunID: run.ID, Limit: 1, StartedAt: at})
	mustProgress(t, err)
	req := GraphNodeRequest{Profile: profile, InstanceID: instance.ID, At: at}
	if _, err = RecoverGraphStoppedExecution(t.Context(), f.opts, req, attempt.ID); err == nil {
		t.Fatal("unreconciled active run parked")
	}
	_, err = f.db.FinishRun(t.Context(), run.ID, store.RunOutcome{Status: store.RunFailed, Error: "provider interrupted after allocation"})
	mustProgress(t, err)
	mustProgress(t, f.db.Close())
	reopenPersistentStopFixture(t, f)
	f.opts.ResearchReporter = newReporter(t, f, forge, nil)
	recovered, err := RecoverGraphStoppedExecution(t.Context(), f.opts, req, attempt.ID)
	mustProgress(t, err)
	if recovered.Status != "waiting_owner" || recovered.NextNode != "analyst" {
		t.Fatalf("orphan progressed: %+v", recovered)
	}
	history, err := f.db.ListGraphNodeAttempts(t.Context(), instance.ID)
	mustProgress(t, err)
	if len(history) != 1 || history[0].ID != attempt.ID || history[0].Status != store.GraphFailed {
		t.Fatalf("recovery allocated/reclassified execution: %+v", history)
	}
	if !strings.Contains(forge.only(t).body, "provider interrupted after allocation") {
		t.Fatal("interruption reason lost")
	}
}

func TestGraphStopPersistentAcceptedBugInputRoutesUnlessInterrupted(t *testing.T) {
	for _, interrupted := range []bool{false, true} {
		name := "failed"
		if interrupted {
			name = "interrupted"
		}
		t.Run(name, func(t *testing.T) {
			db, session, binding, _ := bugResultBoundaryFixture(t)
			service, err := BindAgentResultService(t.Context(), binding)
			mustProgress(t, err)
			run, err := db.GetRun(t.Context(), binding.Attempt.RunID)
			mustProgress(t, err)
			segment, err := db.GetSegment(t.Context(), run.SegmentID)
			mustProgress(t, err)
			at := time.Now().UTC()
			definition, err := db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "builtin", Workflow: "bug", ProfileVersion: 1, Path: "bug.dot", Digest: strings.Repeat("e", 64), CreatedAt: at})
			mustProgress(t, err)
			instance, err := db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: session.ID, DefinitionID: definition.ID, CurrentNode: "analyst", StartedAt: at})
			mustProgress(t, err)
			profile := &graphexec.Profile{Team: "builtin", Workflow: agent.WorkflowBug, ProfileVersion: 1, Digest: definition.Digest, Nodes: []graphexec.Node{{Name: "analyst", Action: ResearchAgentAction, Limit: 1, Attrs: map[string]string{"lf_park": "owner", "lf_stop": "blocked,failed,invalid,exhausted", "lf_response": "result"}}}}
			authority, err := NewAttemptAuthority(t.Context(), db)
			mustProgress(t, err)
			t.Cleanup(func() { _ = authority.Close(context.WithoutCancel(t.Context())) })
			worktree := t.TempDir()
			mustProgress(t, os.MkdirAll(filepath.Join(worktree, ".aiteam/session"), 0700))
			ref, err := naming.ParseRef(session.TaskRef)
			mustProgress(t, err)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			runner := newStubRunner(func(runctx context.Context, _ agent.Request) (*agent.RunResult, error) {
				calls++
				data := bugResultBoundaryBlocked()
				d, err := service.Preflight(runctx, data)
				if err != nil || !d.Valid {
					t.Fatalf("preflight=%+v %v", d, err)
				}
				accepted, err := service.Submit(runctx, "accepted-before-failure", data)
				if err != nil || !accepted.Validation.Valid {
					t.Fatalf("submit=%+v %v", accepted, err)
				}
				_, err = service.Receipt(runctx, run.ID, "accepted-before-failure")
				mustProgress(t, err)
				if interrupted {
					cancel()
					return &agent.RunResult{Outcome: agent.OutcomeFailed, Reason: "provider interrupted"}, context.Canceled
				}
				return &agent.RunResult{Outcome: agent.OutcomeFailed, Reason: "provider failed", ResultText: `{"response":"done"}`}, errors.New("controlled process failure")
			})
			delivered := fixtureNativeDelivery(t, BugWorkflowPolicy())
			handler := AgentActivity{Policy: BugWorkflowPolicy(), Prepare: func(prepctx context.Context, _ Options, _ ActivityRequest) (ResearchInvocation, error) {
				prepared := &preparedMCP{runtime: &MCPRuntime{Authority: authority, Paths: config.NewPaths(launchFixtureDirectory(t)), Executable: "/factory/litefactory", Now: time.Now}, service: service}
				spec, err := prepareBugResultBroker(prepctx, prepared, binding.Attempt.Attempt)
				if err != nil {
					return ResearchInvocation{}, err
				}
				t.Cleanup(func() { _ = prepared.close(context.WithoutCancel(t.Context())) })
				request := agent.Request{Delivery: delivered, Workflow: agent.WorkflowBug, MCPLaunch: spec, Ref: ref, RunOrdinal: run.Ordinal, SegmentOrdinal: segment.Ordinal, Session: agent.SessionRef{Ref: segment.SessionRef, Resume: segment.ResumeToken}, WorktreePath: worktree, Prompt: "Investigate", Permissions: agent.Permissions{Profile: "bug-analyst"}}
				return ResearchInvocation{Request: request, Kind: agent.Kind(store.AgentClaude), SessionPath: ".aiteam/session", Results: service}, nil
			}}
			var registry ActivityRegistry
			mustProgress(t, registry.Register(ResearchAgentAction, handler))
			// No reporter: execution must still save its immutable stop and leave
			// publication owed, including when the provider cancels the caller context.
			out, err := ExecuteNodeAction(ctx, Options{DB: db, Runners: runnerSet(runner)}, GraphNodeRequest{Profile: profile, Activities: registry, InstanceID: instance.ID, RunID: run.ID, At: at})
			if out.Status != "waiting_owner" || out.NextNode != "analyst" || calls != 1 {
				t.Fatalf("stop did not park: %+v calls=%d err=%v", out, calls, err)
			}
			attempt, err2 := db.GetGraphNodeAttempt(t.Context(), out.AttemptID)
			mustProgress(t, err2)
			if !interrupted {
				// The accepted submission is the agent's answer; the failed exit
				// afterwards does not discard it.
				if attempt.Status != store.GraphSucceeded || attempt.Outcome != "blocked" {
					t.Fatalf("accepted Bug submission was discarded: %+v err=%v", attempt, err)
				}
			} else {
				if err == nil || attempt.Status != store.GraphFailed || attempt.Outcome != "" {
					t.Fatalf("interrupted execution gained routing outcome: %+v err=%v", attempt, err)
				}
				if !strings.Contains(string(out.Activity.Output), "Reproduction requires owner evidence") || !strings.Contains(out.Activity.Diagnostic, "Factory execution failure") {
					t.Fatalf("accepted history/execution cause lost: %+v", out.Activity)
				}
			}
			wait, err := db.GetGraphOwnerWait(t.Context(), instance.ID)
			mustProgress(t, err)
			if wait.State != "waiting" || wait.SourceAttemptID != attempt.ID {
				t.Fatalf("failed execution wait=%+v", wait)
			}
		})
	}
}

func TestGraphRunlessStopProgressSurvivesShorteningAndLostAcknowledgment(t *testing.T) {
	f, forge, profile, instance := persistentStopReporterFixture(t)
	ctx, at := t.Context(), time.Now().UTC()
	attempt, err := f.db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: instance.ID, Node: "analyst", Limit: 1, StartedAt: at})
	mustProgress(t, err)
	mustProgress(t, f.db.FinishGraphNodeAttempt(ctx, attempt.ID, store.GraphSucceeded, "done", at))
	req := GraphNodeRequest{Profile: profile, InstanceID: instance.ID, At: at}
	out := GraphNodeResult{InstanceID: instance.ID, Node: "analyst", Activity: ActivityResult{Diagnostic: strings.Repeat("long explanation ", 2000)}}
	// Parking remains durable even when no reporter is available.
	f.opts.ResearchReporter = nil
	_, err = parkGraphAction(ctx, f.opts, req, out, "exhausted", false)
	if err == nil {
		t.Fatal("missing reporter hidden")
	}
	wait, err := f.db.GetGraphOwnerWait(ctx, instance.ID)
	mustProgress(t, err)
	intent, err := f.db.GetGraphComment(ctx, wait.DeliveryKey)
	mustProgress(t, err)
	var question GraphStopQuestion
	mustProgress(t, json.Unmarshal(intent.Candidate, &question))
	if !strings.Contains(question.Reason, "[explanation shortened]") || !strings.HasSuffix(question.Reason, fmt.Sprintf("Latest completed attempt: %d.", attempt.ID)) || question.SourceAttemptID != 0 {
		t.Fatalf("progress lost: %+v", question)
	}
	f.opts.ResearchReporter = newReporter(t, f, forge, nil)
	forge.afterPost = func() { mustProgress(t, f.db.Close()) }
	_, err = parkGraphAction(ctx, f.opts, req, out, "exhausted", false)
	if err == nil {
		t.Fatal("lost acknowledgment hidden")
	}
	reopenPersistentStopFixture(t, f)
	forge.afterPost = nil
	f.opts.ResearchReporter = newReporter(t, f, forge, nil)
	req.At = at.Add(time.Hour)
	for range 2 {
		_, err = parkGraphAction(ctx, f.opts, req, out, "exhausted", false)
		mustProgress(t, err)
	}
	recovered, err := f.db.GetGraphOwnerWait(ctx, instance.ID)
	mustProgress(t, err)
	if recovered.DeliveryKey != wait.DeliveryKey || recovered.State != "waiting" || recovered.ContinuationAttemptID != 0 || forge.attemptCount() != 1 {
		t.Fatalf("retry changed stop or allowance: %+v", recovered)
	}
}
