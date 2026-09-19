package turn

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

const researchDoneJSON = `{"version":1,"response":"done","result":{"outcome":"completed","comment":"Investigation finished.","payload":{"research":{"request":"Inspect boundaries"}}}}`
const researchAskJSON = `{"version":1,"response":"ask","result":{"outcome":"incomplete","reason":"Owner scope needed","comment":"Findings retained.","questions":[{"id":"Q-1","text":"Which boundary?"}],"payload":{"research":{"request":"Inspect boundaries"}}}}`

func assembledResearchFixture(t *testing.T, runner Runner) (*fixture, *fakeForge, Request) {
	t.Helper()
	f := newFixture(t, runner)
	authority, err := NewAttemptAuthority(t.Context(), f.db)
	mustProgress(t, err)
	t.Cleanup(func() { mustProgress(t, authority.Close(context.WithoutCancel(t.Context()))) })
	f.opts.MCP = &MCPRuntime{Authority: authority, Paths: config.NewPaths(launchFixtureDirectory(t)), Executable: "/factory/litefactory", Now: time.Now}
	f.opts.Tiers.ModelByTier = map[config.Kind]map[config.Tier]string{"claude": {"balanced": "test-model"}}
	forge := newFakeForge(t)
	f.opts.MCP.Comments = forge.client
	f.opts.MCP.Redactor = redact.New(redact.Options{Environ: []string{}})
	f.opts.MCP.BotID = 9999
	f.opts.ResearchReporter = newReporter(t, f, forge, nil)
	f.opts.ResearchDriver = ExecuteResearchWorkflow
	dir := filepath.Join(f.repo.Dir, ".litefactory")
	mustProgress(t, os.MkdirAll(dir, 0700))
	mustProgress(t, os.WriteFile(filepath.Join(dir, "research.yaml"), []byte("execution: graph\n"), 0600))
	req := f.request()
	req.Mode = agent.ModeResearch
	req.Command = ParseCommand("/research inspect boundaries")
	req.Trigger.Body = "/research inspect boundaries"
	return f, forge, req
}

func TestResearchWorkflowProductionOutcomes(t *testing.T) {
	for _, variant := range []string{"clear", "malformed", "timeout", "cancel"} {
		t.Run(variant, func(t *testing.T) {
			calls := 0
			runner := lifecycleMCPHost(func(ctx context.Context, req agent.Request) (*agent.RunResult, error) {
				calls++
				if req.Delivery == nil || req.Delivery.MainRole != "research" || req.Graph == nil || req.Invocation != agent.InvocationResearchNode {
					t.Fatalf("main role graph delivery missing: %+v", req)
				}
				for _, tool := range req.MCPLaunch.PermittedTools() {
					if tool == "issues_comment" || tool == "issues_create_batch" {
						t.Fatal("private node received direct publication authority")
					}
				}
				if err := lifecycleMCPProbe(ctx, req.MCPLaunch); err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(req.Prompt, "Factory supplied session artifact directory:") || !strings.Contains(req.Prompt, "Shared result helper access") {
					t.Fatal("missing artifact/helper context")
				}
				raw := researchDoneJSON
				outcome := agent.OutcomeSucceeded
				reason := ""
				if variant == "malformed" {
					raw = `{"version":1,"response":"done"}`
				}
				if variant == "timeout" {
					outcome = agent.OutcomeTimedOut
					reason = "timeout"
				}
				if variant == "cancel" {
					return &agent.RunResult{Outcome: agent.OutcomeFailed, Reason: "cancelled", FinishedAt: time.Now()}, context.Canceled
				}
				return &agent.RunResult{Outcome: outcome, Reason: reason, ResultText: raw, FinishedAt: time.Now()}, nil
			})
			f, forge, req := assembledResearchFixture(t, runner)
			out, err := Execute(t.Context(), f.opts, req)
			if variant == "clear" {
				if err != nil || !out.Succeeded() || !out.ResearchGraph || forge.attemptCount() != 1 {
					t.Fatalf("clear outcome %+v posts=%d err=%v", out, forge.attemptCount(), err)
				}
				instance, e := f.db.LatestResearchInstance(t.Context(), f.session.ID)
				mustProgress(t, e)
				if instance.Status != store.GraphSucceeded {
					t.Fatalf("graph not completed: %+v", instance)
				}
			} else if err == nil || out.Run.Status != store.RunFailed || forge.attemptCount() != 0 {
				t.Fatalf("failure lost %+v posts=%d err=%v", out, forge.attemptCount(), err)
			}
			replay, e := Execute(t.Context(), f.opts, req)
			mustProgress(t, e)
			if calls != 1 || !replay.ResearchGraph || replay.Run.ID != out.Run.ID {
				t.Fatalf("replay invoked agent: calls=%d %+v", calls, replay)
			}
		})
	}
}

func TestResearchWorkflowOwnerReplyColdFallback(t *testing.T) {
	calls := 0
	var requests []agent.Request
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		calls++
		requests = append(requests, req)
		if calls == 1 {
			return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: researchAskJSON, FinishedAt: time.Now()}, nil
		}
		if calls == 2 {
			return &agent.RunResult{Outcome: agent.OutcomeFailed, Reason: "resume unavailable", FinishedAt: time.Now()}, agent.ErrSessionNotResumable
		}
		if !strings.Contains(req.Prompt, "Findings retained.") || !strings.Contains(req.Prompt, "Inspect the HTTP boundary") || !strings.Contains(req.Prompt, "Shared result helper access") {
			t.Fatal("cold fallback lost saved findings/reply/helper")
		}
		client := submission.Client{DescriptorPath: req.MCPLaunch.DescriptorPath()}
		update := []byte(`{"expected_revision":1,"operations":[{"op":"set","target":{"field":"outcome"},"value":"completed"},{"op":"set","target":{"field":"comment"},"value":"Investigation finished."},{"op":"remove","target":{"field":"reason"}},{"op":"remove","target":{"field":"questions"}}]}`)
		submitted := mcpDecodeSubmission(t, mcpToolCall(t, client, "communication_submit", update))
		if !submitted.Validation.Valid {
			t.Fatalf("continuation correction: %+v", submitted)
		}
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: researchDoneJSON, FinishedAt: time.Now()}, nil
	})
	f, forge, req := assembledResearchFixture(t, runner)
	out, err := Execute(t.Context(), f.opts, req)
	mustProgress(t, err)
	instance, err := f.db.LatestResearchInstance(t.Context(), f.session.ID)
	mustProgress(t, err)
	wait, err := f.db.GetGraphOwnerWait(t.Context(), instance.ID)
	mustProgress(t, err)
	if wait.State != "waiting" || calls != 1 || forge.attemptCount() != 1 || out.Session.State != store.SessionIdle {
		t.Fatalf("question kept worker or failed wait: %+v %+v", out, wait)
	}
	// Real database restart leaves the durable wait and original question identity.
	mustProgress(t, f.db.Close())
	reopened, err := store.Open(t.Context(), f.paths.DBPath())
	mustProgress(t, err)
	mustProgress(t, reopened.Migrate(t.Context()))
	*f.db = *reopened
	delivery, err := f.db.GetReportDelivery(t.Context(), wait.DeliveryKey)
	mustProgress(t, err)
	reply := store.GraphOwnerReply{InstanceID: instance.ID, OwnerID: 7, AuthorID: 7, CommentID: testComment + 1, QuestionCommentID: delivery.CommentID, TaskRef: f.session.TaskRef, SourceRef: f.session.TaskRef, Mode: "research", DeliveryKey: wait.DeliveryKey, Body: "Inspect the HTTP boundary", At: time.Now()}
	_, err = f.db.ClaimGraphOwnerReply(t.Context(), reply)
	mustProgress(t, err)
	if _, err = f.db.ClaimGraphOwnerReply(t.Context(), reply); err == nil {
		t.Fatal("duplicate reply admitted")
	}
	req.Trigger.CommentID++
	req.Trigger.Body = reply.Body
	req.Command = ParseCommand(reply.Body)
	done, err := Execute(t.Context(), f.opts, req)
	mustProgress(t, err)
	if !done.Succeeded() || calls != 3 || forge.attemptCount() != 2 || requests[1].Path != agent.PathResume || requests[2].Path != agent.PathColdAfterResumeRefused {
		t.Fatalf("continuation/fallback failed calls=%d posts=%d outcome=%+v", calls, forge.attemptCount(), done)
	}
	if requests[0].Session.Ref != requests[1].Session.Ref {
		t.Fatal("compatible continuation rotated session")
	}
	if requests[1].Graph.AttemptID != requests[2].Graph.AttemptID {
		t.Fatal("fallback changed logical attempt")
	}
	if errors.Is(err, agent.ErrSessionNotResumable) {
		t.Fatal(err)
	}
}

func TestResearchWorkflowReportRecoveryDoesNotRetryAgent(t *testing.T) {
	calls := 0
	runner := lifecycleMCPHost(func(_ context.Context, _ agent.Request) (*agent.RunResult, error) {
		calls++
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: researchDoneJSON, FinishedAt: time.Now()}, nil
	})
	f, forge, req := assembledResearchFixture(t, runner)
	f.opts.ResearchReporter = newReporter(t, f, forge, stubRenderer{err: errors.New("render unavailable")})
	failed, err := Execute(t.Context(), f.opts, req)
	if err == nil || failed.Run.Status != store.RunFailed || calls != 1 || forge.attemptCount() != 0 {
		t.Fatalf("render failure not durable: %+v %v", failed, err)
	}
	instance, err := f.db.LatestResearchInstance(t.Context(), f.session.ID)
	mustProgress(t, err)
	intents, err := f.db.ListGraphComments(t.Context(), instance.ID, "", 100)
	mustProgress(t, err)
	if len(intents) != 1 {
		t.Fatalf("lost authored report: %+v", intents)
	}
	f.opts.ResearchReporter = newReporter(t, f, forge, nil)
	replay, err := Execute(t.Context(), f.opts, req)
	mustProgress(t, err)
	if calls != 1 || forge.attemptCount() != 1 || replay.Run.Status != store.RunFailed || !replay.ResearchGraph {
		t.Fatalf("recovery changed execution or duplicated effect: calls=%d posts=%d %+v", calls, forge.attemptCount(), replay)
	}
	delivery, err := f.db.GetReportDelivery(t.Context(), intents[0].Key)
	mustProgress(t, err)
	if !delivery.Completed {
		t.Fatal("recovery did not confirm original effect")
	}
	_, err = Execute(t.Context(), f.opts, req)
	mustProgress(t, err)
	if calls != 1 || forge.attemptCount() != 1 {
		t.Fatal("duplicate dispatch reran or reposted")
	}
}

func TestResearchDefaultRemainsGated(t *testing.T) {
	f, _, req := assembledResearchFixture(t, lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
		t.Fatal("admission invoked host")
		return nil, nil
	}))
	mustProgress(t, os.Remove(filepath.Join(f.repo.Dir, ".litefactory", "research.yaml")))
	f.opts.ResearchDriver = func(context.Context, Options, Request, Result, *teamsource.ResearchDefinition, store.ResearchAdmission) (Result, error) {
		t.Fatal("default activated without authentic evidence")
		return Result{}, nil
	}
	_, handled, err := admitResearchExecution(t.Context(), f.opts, req, Result{Session: f.session, SessionID: f.session.ID})
	if err != nil || handled {
		t.Fatalf("default gate bypassed: handled=%v err=%v", handled, err)
	}
	selected, err := f.db.AdmitResearchPath(t.Context(), f.session.ID, "graph")
	mustProgress(t, err)
	if selected != "legacy" {
		t.Fatal("default legacy path was not preserved")
	}
}

func TestResearchCurrentInstructionsRotatePersistedSession(t *testing.T) {
	f, _, req := assembledResearchFixture(t, lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
		t.Fatal("delivery preparation launched provider")
		return nil, nil
	}))
	specs, err := ResearchSpecs()
	mustProgress(t, err)
	definition, err := teamsource.CaptureResearch(t.Context(), specs)
	mustProgress(t, err)
	ref, err := naming.ParseRef(f.session.TaskRef)
	mustProgress(t, err)
	firstRef, err := naming.SessionRef(ref, store.FirstSegmentOrdinal)
	mustProgress(t, err)
	first, delivery, err := PrepareResearchDelivery(t.Context(), f.opts, req, definition, "investigate", f.session, ref, firstRef)
	mustProgress(t, err)
	same, _, err := PrepareResearchDelivery(t.Context(), f.opts, req, definition, "investigate", f.session, ref, firstRef)
	mustProgress(t, err)
	if first.ID != same.ID {
		t.Fatal("unchanged instructions rotated provider session")
	}
	node := definition.Nodes["investigate"]
	node.Role.Body += "\nCurrent shipped instruction update."
	definition.Nodes["investigate"] = node
	changed, newDelivery, err := PrepareResearchDelivery(t.Context(), f.opts, req, definition, "investigate", f.session, ref, firstRef)
	mustProgress(t, err)
	if changed.ID == first.ID || changed.SessionRef == first.SessionRef || changed.Ordinal != first.Ordinal+1 || newDelivery.Root == delivery.Root {
		t.Fatalf("changed instructions reused old session/bundle: %+v %+v", first, changed)
	}
	oldBytes, err := os.ReadFile(filepath.Join(delivery.Root, "plugin/agents/research.md"))
	mustProgress(t, err)
	newBytes, err := os.ReadFile(filepath.Join(newDelivery.Root, "plugin/agents/research.md"))
	mustProgress(t, err)
	if strings.Contains(string(oldBytes), "Current shipped instruction update") || !strings.Contains(string(newBytes), "Current shipped instruction update") {
		t.Fatal("instruction update mutated old bundle or lost new role")
	}
}

func TestResearchWorkflowPersistedLimitRefusesInvocation(t *testing.T) {
	calls := 0
	f, forge, req := assembledResearchFixture(t, lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
		calls++
		return nil, errors.New("exhausted graph invoked provider")
	}))
	f.opts.ResearchDriver = func(ctx context.Context, opts Options, request Request, result Result, definition *teamsource.ResearchDefinition, admission store.ResearchAdmission) (Result, error) {
		at := time.Date(2026, 9, 19, 1, 0, 0, 0, time.UTC)
		loop, err := opts.DB.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: admission.Instance.ID, Node: "loop", Limit: 8, StartedAt: at})
		mustProgress(t, err)
		mustProgress(t, opts.DB.CompleteGraphNodeAttempt(ctx, loop.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "true", FinishedAt: at, InstanceStatus: store.GraphRunning, Transition: &store.GraphTransition{InstanceID: admission.Instance.ID, FromNode: "loop", ToNode: "investigate", OnOutcome: "true", At: at}}))
		for range 8 {
			attempt, err := opts.DB.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: admission.Instance.ID, Node: "investigate", Limit: 8, StartedAt: at})
			mustProgress(t, err)
			mustProgress(t, opts.DB.CompleteGraphNodeAttempt(ctx, attempt.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "success", FinishedAt: at, InstanceStatus: store.GraphRunning, Outputs: []store.GraphOutput{{Kind: store.GraphOutputJSON, Payload: []byte(researchDoneJSON)}}}))
		}
		return ExecuteResearchWorkflow(ctx, opts, request, result, definition, admission)
	}
	out, err := Execute(t.Context(), f.opts, req)
	if err == nil || !strings.Contains(err.Error(), "limit 8 exhausted") || out.Run.Status != store.RunFailed || calls != 0 || forge.attemptCount() != 0 {
		t.Fatalf("limit failed: calls=%d out=%+v err=%v", calls, out, err)
	}
	instance, e := f.db.LatestResearchInstance(t.Context(), f.session.ID)
	mustProgress(t, e)
	attempts, e := f.db.ListGraphNodeAttempts(t.Context(), instance.ID)
	mustProgress(t, e)
	if len(attempts) != 9 || instance.Status != store.GraphFailed {
		t.Fatalf("exhaustion lost counters: %+v count=%d", instance, len(attempts))
	}
}

func TestResearchAdmissionLeavesOtherModesUnhandled(t *testing.T) {
	for _, mode := range []agent.Mode{agent.ModePlan, agent.ModeImplement, agent.ModeValidate, agent.ModeSpike} {
		t.Run(string(mode), func(t *testing.T) {
			_, handled, err := admitResearchExecution(t.Context(), Options{}, Request{Mode: mode}, Result{})
			if err != nil || handled {
				t.Fatalf("Research admission handled %s: handled=%v err=%v", mode, handled, err)
			}
		})
	}
}
