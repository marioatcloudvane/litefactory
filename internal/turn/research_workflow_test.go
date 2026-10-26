package turn

import (
	"context"
	"database/sql"
	"errors"
	"github.com/cloudvane/litefactory/internal/teamsource/teamsourcetest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

const researchDoneJSON = `{"version":1,"response":"done","result":{"outcome":"completed","comment":"Investigation finished.","payload":{"research":{"request":"Inspect boundaries"}}}}`
const researchAskJSON = `{"version":1,"response":"ask","result":{"outcome":"incomplete","reason":"Owner scope needed","comment":"Findings retained.","questions":[{"id":"Q-1","audience":"business","text":"Which boundary?"}],"payload":{"research":{"request":"Inspect boundaries"}}}}`

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
	// The shipped graph runs an advisory builder check before each review.
	judge, _, _ := judgeRecoveryFixture(t)
	judge.DB = f.db
	judge.Providers[agent.Kind("codex")], judge.Providers[agent.Kind("claude")] = &recoveryJudgeRunner{}, &recoveryJudgeRunner{}
	f.opts.Judge = judge
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
				if variant == "clear" {
					submitLiveResult(t, req, raw)
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
				// A clear request no longer finishes the graph. Research
				// publishes its findings and the workflow stops at the shaping
				// stage, because one run advances one agent and this run's is
				// spent. The instance keeps its node and owes a dispatch; that
				// owed run is what continues it.
				instance, e := f.db.LatestResearchInstance(t.Context(), f.session.ID)
				mustProgress(t, e)
				if instance.Status != store.GraphRunning || instance.CurrentNode != "shape" {
					t.Fatalf("graph did not park at the shaping stage: %+v", instance)
				}
				if instance.NextDueAt.IsZero() {
					t.Fatalf("a parked instance must be due for its next stage: %+v", instance)
				}
				due, e := f.db.ListDueGraphDispatches(t.Context(), instance.NextDueAt, 10)
				mustProgress(t, e)
				if len(due) != 1 || due[0].InstanceID != instance.ID || due[0].Node != "shape" {
					t.Fatalf("the parked stage owes exactly one dispatch: %+v", due)
				}
			} else if err == nil || out.Run.Status != store.RunFailed || forge.attemptCount() != 1 {
				// A failed investigate step is reported through the declared
				// failure comment, never silently (#410).
				t.Fatalf("failure lost or went unreported %+v posts=%d err=%v", out, forge.attemptCount(), err)
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
			submitLiveResult(t, req, researchAskJSON)
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
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		calls++
		submitLiveResult(t, req, researchDoneJSON)
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

func TestResearchDefaultUsesGraph(t *testing.T) {
	f, _, req := assembledResearchFixture(t, lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
		t.Fatal("admission invoked host")
		return nil, nil
	}))
	mustProgress(t, os.Remove(filepath.Join(f.repo.Dir, ".litefactory", "research.yaml")))
	called := false
	f.opts.ResearchDriver = func(_ context.Context, _ Options, _ Request, result Result, _ *teamsource.ResearchDefinition, _ store.ResearchAdmission) (Result, error) {
		called = true
		return result, nil
	}
	_, handled, err := admitResearchExecution(t.Context(), f.opts, req, Result{Session: f.session, SessionID: f.session.ID})
	if err != nil || !handled || !called {
		t.Fatalf("graph default: handled=%v called=%v err=%v", handled, called, err)
	}
}

func TestResearchCurrentInstructionsRotatePersistedSession(t *testing.T) {
	f, _, req := assembledResearchFixture(t, lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
		t.Fatal("delivery preparation launched provider")
		return nil, nil
	}))
	specs, err := ResearchSpecs()
	mustProgress(t, err)
	definition, err := teamsourcetest.CaptureResearch(t.Context(), specs)
	mustProgress(t, err)
	ref, err := naming.ParseRef(f.session.TaskRef)
	mustProgress(t, err)
	firstRef, err := naming.SessionRef(ref, store.FirstSegmentOrdinal)
	mustProgress(t, err)
	first, delivery, err := prepareWorkflowDeliveryForTest(t.Context(), f.opts, req, definition, "investigate", f.session, ref, firstRef, ResearchWorkflowPolicy())
	mustProgress(t, err)
	same, _, err := prepareWorkflowDeliveryForTest(t.Context(), f.opts, req, definition, "investigate", f.session, ref, firstRef, ResearchWorkflowPolicy())
	mustProgress(t, err)
	if first.ID != same.ID {
		t.Fatal("unchanged instructions rotated provider session")
	}
	node := definition.Nodes["investigate"]
	node.Role.Body += "\nCurrent shipped instruction update."
	definition.Nodes["investigate"] = node
	changed, newDelivery, err := prepareWorkflowDeliveryForTest(t.Context(), f.opts, req, definition, "investigate", f.session, ref, firstRef, ResearchWorkflowPolicy())
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

func TestResearchChangedCaptureWithoutEffectsRecovers(t *testing.T) {
	f := newFixture(t, nil)
	f.opts.ResearchReporter = newReporter(t, f, newFakeForge(t), nil)
	at := time.Date(2026, 9, 20, 1, 0, 0, 0, time.UTC)
	definition, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "builtin", Mode: "research", ProfileVersion: 1, Path: "research/research.dot", Digest: strings.Repeat("a", 64), CreatedAt: at})
	mustProgress(t, err)
	instance, err := f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: f.session.ID, DefinitionID: definition.ID, CurrentNode: "investigate", StartedAt: at})
	mustProgress(t, err)
	// Reopen to ensure rejection depends on the retained capture, not in-memory state.
	mustProgress(t, f.db.Close())
	reopened, err := store.Open(t.Context(), f.paths.DBPath())
	mustProgress(t, err)
	*f.db = *reopened
	err = recoverResearchPublications(t.Context(), f.opts, f.session.ID)
	mustProgress(t, err)
	attempts, err := f.db.ListGraphNodeAttempts(t.Context(), instance.ID)
	mustProgress(t, err)
	deliveries, err := f.db.ListPendingReportDeliveries(t.Context())
	mustProgress(t, err)
	if len(attempts) != 0 || len(deliveries) != 0 {
		t.Fatalf("unsupported capture created effects: attempts=%v deliveries=%v", attempts, deliveries)
	}
	retained, err := f.db.GetGraphDefinition(t.Context(), definition.ID)
	mustProgress(t, err)
	if retained.Digest != definition.Digest {
		t.Fatal("unsupported capture was rewritten")
	}
}

// finishedOldResearchInstance retains a terminal Research instance pinned to a
// capture digest that differs from the current research.dot. It holds one final
// graph comment whose report delivery is completed only when delivered is set.
func finishedOldResearchInstance(t *testing.T, f *fixture, status string, delivered bool) store.GraphInstance {
	t.Helper()
	at := time.Date(2026, 9, 20, 1, 0, 0, 0, time.UTC)
	definition, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "builtin", Mode: "research", ProfileVersion: 1, Path: "research/research.dot", Digest: strings.Repeat("a", 64), CreatedAt: at})
	mustProgress(t, err)
	instance, err := f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: f.session.ID, DefinitionID: definition.ID, CurrentNode: "investigate", StartedAt: at})
	mustProgress(t, err)
	attempt, err := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: instance.ID, Node: "investigate", StartedAt: at})
	mustProgress(t, err)
	mustProgress(t, f.db.CompleteGraphNodeAttempt(t.Context(), attempt.ID, store.GraphCompletion{Status: store.GraphSucceeded, FinishedAt: at.Add(time.Minute), InstanceStatus: status}))
	key := "research-old-capture-final"
	mustProgress(t, f.db.RetainGraphComment(t.Context(), store.GraphCommentIntent{Key: key, InstanceID: instance.ID, SourceAttemptID: attempt.ID, Node: "investigate", Kind: "final", Candidate: []byte(`{"outcome":"completed","comment":"Investigation finished.","payload":{"research":{"request":"Inspect boundaries"}}}`)}))
	if delivered {
		probe, err := sql.Open("sqlite", store.DSN(f.db.Path()))
		mustProgress(t, err)
		defer func() { mustProgress(t, probe.Close()) }()
		_, err = probe.ExecContext(t.Context(), `INSERT INTO report_deliveries(delivery_key,identity_version,session_id,trigger_id,task,destination,trigger_namespace,purpose,bot_user_id,body,marker,possibly_sent,comment_id,completed) VALUES(?,1,?,0,'task','issue','graph','research-final',9999,'body','marker',1,77,1)`, key, f.session.ID)
		mustProgress(t, err)
	}
	instance, err = f.db.GetGraphInstance(t.Context(), instance.ID)
	mustProgress(t, err)
	if instance.Status != status {
		t.Fatalf("fixture instance status %q, want %q", instance.Status, status)
	}
	return instance
}

// A finished Research instance that owes nothing must not block later modes
// after research.dot changes (#404 case 3).
func TestResearchFinishedOldCaptureDoesNotBlockLaterModes(t *testing.T) {
	for _, status := range []string{store.GraphSucceeded, store.GraphFailed} {
		t.Run(status, func(t *testing.T) {
			f := newFixture(t, nil)
			forge := newFakeForge(t)
			f.opts.ResearchReporter = newReporter(t, f, forge, nil)
			instance := finishedOldResearchInstance(t, f, status, true)
			if err := recoverResearchPublications(t.Context(), f.opts, f.session.ID); err != nil {
				t.Fatalf("finished old Research instance blocked later modes: %v", err)
			}
			attempts, err := f.db.ListGraphNodeAttempts(t.Context(), instance.ID)
			mustProgress(t, err)
			deliveries, err := f.db.ListPendingReportDeliveries(t.Context())
			mustProgress(t, err)
			if len(attempts) != 1 || len(deliveries) != 0 || forge.attemptCount() != 0 {
				t.Fatalf("finished instance created effects: attempts=%v deliveries=%v posts=%d", attempts, deliveries, forge.attemptCount())
			}
		})
	}
}

// A graph change removes no source-integrity requirement: a pending comment
// with no retained node output remains unsafe to publish.
func TestResearchChangedCaptureStillRejectsIncompleteSource(t *testing.T) {
	f := newFixture(t, nil)
	forge := newFakeForge(t)
	f.opts.ResearchReporter = newReporter(t, f, forge, nil)
	instance := finishedOldResearchInstance(t, f, store.GraphSucceeded, false)
	err := recoverResearchPublications(t.Context(), f.opts, f.session.ID)
	if err == nil || !strings.Contains(err.Error(), "research attempt has no node output") {
		t.Fatalf("incomplete source recovery: %v", err)
	}
	attempts, err := f.db.ListGraphNodeAttempts(t.Context(), instance.ID)
	mustProgress(t, err)
	deliveries, err := f.db.ListPendingReportDeliveries(t.Context())
	mustProgress(t, err)
	if len(attempts) != 1 || len(deliveries) != 0 || forge.attemptCount() != 0 {
		t.Fatalf("incomplete source created effects: attempts=%v deliveries=%v posts=%d", attempts, deliveries, forge.attemptCount())
	}
}

// The retained accepted result is the only source of application data (#410).
// With no accepted submission the attempt fails with its cause, and the owner
// is told through the one declared failure comment; nothing else is published.
func TestResearchWorkflowTerminalOnlyOutputFailsAndIsReported(t *testing.T) {
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: researchDoneJSON, FinishedAt: time.Now()}, nil
	})
	f, forge, req := assembledResearchFixture(t, runner)
	out, err := Execute(t.Context(), f.opts, req)
	if err == nil || out.Run.Status != store.RunFailed || forge.attemptCount() != 1 || !strings.Contains(err.Error(), "no accepted shared result") {
		t.Fatalf("terminal-only output completed, lost its cause or went unreported: %+v posts=%d err=%v", out.Run, forge.attemptCount(), err)
	}
	instance, e := f.db.LatestResearchInstance(t.Context(), f.session.ID)
	mustProgress(t, e)
	intents, e := f.db.ListGraphComments(t.Context(), instance.ID, "", 100)
	mustProgress(t, e)
	if len(intents) != 1 || intents[0].Kind != "failure" || intents[0].Node != "failed" {
		t.Fatalf("want exactly the declared failure comment, got %+v", intents)
	}
}

// C1 (#410): a Research step whose valid result was submitted succeeds whatever
// the agent printed afterwards: a copy missing one closing brace (the #351 run
// 627 failure), valid JSON that is not an envelope, a differing envelope, prose
// or nothing at all. The retained result is what is published.
func TestResearchWorkflowSubmittedResultStandsWhateverIsPrinted(t *testing.T) {
	for name, terminal := range map[string]string{
		"missing closing brace": researchDoneJSON[:len(researchDoneJSON)-1],
		"json, not an envelope": `{"outcome":"completed","unknown":true}`,
		"differing envelope":    strings.Replace(researchDoneJSON, "Investigation finished.", "A different summary.", 1),
		"prose":                 "I submitted the result and read its receipt.",
		"empty":                 "",
	} {
		t.Run(name, func(t *testing.T) {
			runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
				submitLiveResult(t, req, researchDoneJSON)
				return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: terminal, FinishedAt: time.Now()}, nil
			})
			f, forge, req := assembledResearchFixture(t, runner)
			out, err := Execute(t.Context(), f.opts, req)
			if err != nil || !out.Succeeded() || forge.attemptCount() != 1 {
				t.Fatalf("a submitted result failed on its printed copy: %+v posts=%d err=%v", out, forge.attemptCount(), err)
			}
			instance, e := f.db.LatestResearchInstance(t.Context(), f.session.ID)
			mustProgress(t, e)
			if instance.CurrentNode != "shape" {
				t.Fatalf("investigate did not hand over on its retained result: %+v", instance)
			}
			attempt, e := f.db.LatestGraphNodeAttempt(t.Context(), instance.ID, "investigate")
			mustProgress(t, e)
			scope := agentresult.Context{SchemaVersion: agentresult.SchemaVersion, Mode: "research", WorkflowVersion: store.WorkflowSeparated, QuestionsAllowed: true}
			node, e := LoadResearchNodeOutput(t.Context(), f.db, attempt.ID, scope)
			mustProgress(t, e)
			if node.Response != "done" || node.Result.Comment != "Investigation finished." {
				t.Fatalf("node output is not the retained result: %+v", node)
			}
		})
	}
}

// #1008 C7/C11/C12: an owner comment whose saved launch was admitted, failed
// before any run (here a rate limit, then still running), and whose instance
// then finished, is answered with the not-applied reason on its first replay
// instead of reaching ExecuteNodeAction. A later comment starts a successor.
func TestResearchWorkflowFinishedLaunchRetiresOwnerComment(t *testing.T) {
	calls := 0
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		calls++
		submitLiveResult(t, req, researchDoneJSON)
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "done", FinishedAt: time.Now()}, nil
	})
	f, forge, req := assembledResearchFixture(t, runner)
	transient := 2
	f.opts.ResearchDriver = func(ctx context.Context, opts Options, request Request, result Result, definition *teamsource.ResearchDefinition, admission store.ResearchAdmission) (Result, error) {
		if transient > 0 {
			transient--
			return result, &github.RateLimitError{RetryAfter: time.Minute}
		}
		return ExecuteResearchWorkflow(ctx, opts, request, result, definition, admission)
	}
	for range 2 {
		out, err := Execute(t.Context(), f.opts, req)
		var limited *github.RateLimitError
		if !errors.As(err, &limited) || out.Reason == planAnswerNotApplied || out.Disposition == DispositionPrepareFailed {
			t.Fatalf("transient failure on a running instance was retired: %+v err=%v", out, err)
		}
	}
	instance, err := f.db.LatestResearchInstance(t.Context(), f.session.ID)
	mustProgress(t, err)
	if instance.Status != store.GraphRunning {
		t.Fatalf("instance not running after transient failures: %+v", instance)
	}
	mustProgress(t, f.db.SetGraphInstanceStatus(t.Context(), instance.ID, store.GraphCancelled, time.Time{}, time.Now()))

	out, err := Execute(t.Context(), f.opts, req)
	if err != nil || out.Err != nil || out.Disposition != DispositionPrepareFailed || out.Reason != planAnswerNotApplied || calls != 0 || forge.attemptCount() != 0 {
		t.Fatalf("finished launch not retired: %+v calls=%d posts=%d err=%v", out, calls, forge.attemptCount(), err)
	}
	if errors.Is(err, store.ErrGraphInstanceTerminal) {
		t.Fatalf("finished launch reached the node: %v", err)
	}

	// A different, later owner comment is not affected by A's retirement.
	next := req
	next.Trigger.CommentID = req.Trigger.CommentID + 1
	out, err = Execute(t.Context(), f.opts, next)
	if err != nil || out.Reason == planAnswerNotApplied || !out.Succeeded() || calls != 1 {
		t.Fatalf("later comment did not run on a successor: %+v calls=%d err=%v", out, calls, err)
	}
	successor, err := f.db.LatestResearchInstance(t.Context(), f.session.ID)
	mustProgress(t, err)
	if successor.ID == instance.ID {
		t.Fatalf("later comment reused the finished instance %d", instance.ID)
	}
}

// #1008 C9: a running instance whose saved capture cannot be read keeps the
// comment pending with an error and no refusal; the finished-launch check
// never turns a capture failure into retirement.
func TestResearchWorkflowCaptureFailureOnRunningInstanceStaysPending(t *testing.T) {
	calls := 0
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		calls++
		submitLiveResult(t, req, researchDoneJSON)
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "done", FinishedAt: time.Now()}, nil
	})
	f, forge, req := assembledResearchFixture(t, runner)
	broken := true
	f.opts.ResearchDriver = func(ctx context.Context, opts Options, request Request, result Result, definition *teamsource.ResearchDefinition, admission store.ResearchAdmission) (Result, error) {
		if broken {
			admission.LaunchID += 1000
		}
		return ExecuteResearchWorkflow(ctx, opts, request, result, definition, admission)
	}
	out, err := Execute(t.Context(), f.opts, req)
	if err == nil || !strings.Contains(err.Error(), "intact retained capture") || out.Reason == planAnswerNotApplied || calls != 0 || forge.attemptCount() != 0 {
		t.Fatalf("capture failure was not left pending: %+v calls=%d err=%v", out, calls, err)
	}
	broken = false
	out, err = Execute(t.Context(), f.opts, req)
	if err != nil || !out.Succeeded() || calls != 1 {
		t.Fatalf("readable capture did not proceed: %+v calls=%d err=%v", out, calls, err)
	}
}

// #1008 C5: a factory-private graph dispatch trigger never receives the
// owner-comment refusal; it keeps its existing error path.
func TestWorkflowFinishedInstancePrivateTriggerKeepsErrorPath(t *testing.T) {
	f, forge, req := assembledResearchFixture(t, lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
		return nil, errors.New("must not run")
	}))
	req.Trigger.CommentID = store.GraphDispatch{ID: 1}.TriggerID()
	definition := &teamsource.WorkflowDefinition{Profile: &graphexec.Profile{Mode: agent.ModeResearch}}
	out, err := ExecuteWorkflow(t.Context(), f.opts, req, Result{SessionID: f.session.ID, TriggerCommentID: req.Trigger.CommentID}, definition, store.WorkflowAdmission{Instance: store.GraphInstance{ID: 1, Status: store.GraphSucceeded}}, ResearchWorkflowPolicy())
	if err == nil || out.Reason == planAnswerNotApplied || out.Disposition == DispositionPrepareFailed || forge.attemptCount() != 0 {
		t.Fatalf("private trigger was refused as an owner comment: %+v err=%v", out, err)
	}
}

// #1008 C7 for Spike: the saved launch is admitted, the turn fails before any
// run (result authority unavailable), the instance then finishes, and the
// replay answers the comment instead of failing at the node.
func TestSpikeWorkflowFinishedLaunchRetiresOwnerComment(t *testing.T) {
	calls := 0
	f, forge, req := assembledSpikeFixture(t, lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
		calls++
		return nil, errors.New("a retired comment must not reach an agent")
	}), "execution: graph\n")
	authority := f.opts.MCP
	f.opts.MCP = nil
	if _, err := Execute(t.Context(), f.opts, req); err == nil || !strings.Contains(err.Error(), "result authority") {
		t.Fatalf("pre-agent failure not returned: %v", err)
	}
	f.opts.MCP = authority
	instance := spikeInstance(t, f)
	if instance.Status != store.GraphRunning {
		t.Fatalf("instance not running after pre-agent failure: %+v", instance)
	}
	mustProgress(t, f.db.SetGraphInstanceStatus(t.Context(), instance.ID, store.GraphCancelled, time.Time{}, time.Now()))
	out, err := Execute(t.Context(), f.opts, req)
	if err != nil || out.Disposition != DispositionPrepareFailed || out.Reason != planAnswerNotApplied || calls != 0 || forge.attemptCount() != 0 {
		t.Fatalf("finished Spike launch not retired: %+v calls=%d err=%v", out, calls, err)
	}
}

// #1008 C8: while a finished instance of the session still owes a graph
// comment, a later owner comment is held pending by the existing publication
// guard (ErrWorkflowEffectsPending) and never gets the not-applied refusal.
func TestResearchWorkflowIncompletePublicationStaysPending(t *testing.T) {
	calls := 0
	f, forge, req := assembledResearchFixture(t, lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
		calls++
		return nil, errors.New("must not run")
	}))
	failFirst := true
	f.opts.ResearchDriver = func(ctx context.Context, opts Options, request Request, result Result, definition *teamsource.ResearchDefinition, admission store.ResearchAdmission) (Result, error) {
		if failFirst {
			failFirst = false
			return result, errors.New("failed before any run")
		}
		return ExecuteResearchWorkflow(ctx, opts, request, result, definition, admission)
	}
	if _, err := Execute(t.Context(), f.opts, req); err == nil {
		t.Fatal("pre-run failure was not returned")
	}
	ctx := t.Context()
	instance, err := f.db.LatestResearchInstance(ctx, f.session.ID)
	mustProgress(t, err)
	mustProgress(t, f.db.SetGraphInstanceStatus(ctx, instance.ID, store.GraphCancelled, time.Time{}, time.Now()))
	// Another finished instance of this session owes an undelivered comment.
	pin, err := f.db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "builtin", Mode: "validate", ProfileVersion: 1, Path: "validate.dot", Digest: strings.Repeat("a", 64)})
	mustProgress(t, err)
	other, err := f.db.AdmitWorkflowLaunch(ctx, store.WorkflowLaunch{Mode: "validate", SessionID: f.session.ID, TriggerID: req.Trigger.CommentID + 100, DefinitionID: pin.ID, Start: "check", ContentDigest: strings.Repeat("b", 64), TopologyDigest: strings.Repeat("c", 64), Files: []byte("{}")})
	mustProgress(t, err)
	at := time.Now()
	attempt, err := f.db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: other.Instance.ID, Node: "check", StartedAt: at})
	mustProgress(t, err)
	mustProgress(t, f.db.CompleteGraphNodeAttempt(ctx, attempt.ID, store.GraphCompletion{Status: store.GraphFailed, FinishedAt: at, InstanceStatus: store.GraphFailed}))
	mustProgress(t, f.db.RetainGraphComment(ctx, store.GraphCommentIntent{Key: "unpublished-failure", InstanceID: other.Instance.ID, SourceAttemptID: attempt.ID, Node: "failed", Kind: "failure", Candidate: []byte(researchDoneJSON)}))

	next := req
	next.Trigger.CommentID = req.Trigger.CommentID + 1
	out, err := Execute(ctx, f.opts, next)
	if !errors.Is(err, store.ErrWorkflowEffectsPending) || out.Reason == planAnswerNotApplied || out.Disposition == DispositionPrepareFailed || calls != 0 || forge.attemptCount() != 0 {
		t.Fatalf("incomplete publication did not keep the comment pending: %+v calls=%d posts=%d err=%v", out, calls, forge.attemptCount(), err)
	}
}
