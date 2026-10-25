package turn

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// Submissions may already be durable when expiry wins the execution race. A
// timeout must retain the actual input receipt without manufacturing acceptance
// or removing the story's stop. Each case reopens the durable store and replays
// the held attempt, rather than relying on the first activity's memory.
func TestWorkerSubmissionTimeoutRetainsActualReceiptAcrossRestart(t *testing.T) {
	oldClock := agentBudgetClock
	agentBudgetClock = func(time.Duration) time.Duration { return 100 * time.Millisecond }
	defer func() { agentBudgetClock = oldClock }()
	for _, phase := range []string{"before", "at cancelled boundary", "after", "invalid", "missing", "during release"} {
		t.Run(phase, func(t *testing.T) {
			f := newRetainedFixture(t, "implement")
			f.policy.AfterAgentFailure = implementAfterAgentFailure
			at := time.Now().UTC()
			mustProgress(t, f.db.EnsureImplementStories(t.Context(), implementTaskParent, 1, []store.ImplementStorySeed{{Key: "story-001", Position: 0, Branch: "lf/story-001"}}, at))
			row, err := f.db.GetImplementStory(t.Context(), implementTaskParent, 1, "story-001")
			mustProgress(t, err)
			_, err = f.db.TransitionImplementStory(t.Context(), store.ImplementTransition{Parent: implementTaskParent, Generation: 1, Key: row.Key, From: store.ImplementWaiting, To: store.ImplementRunning, ExpectedRevision: row.Revision, At: at})
			mustProgress(t, err)
			mustProgress(t, f.db.SetImplementWorkerSession(t.Context(), implementTaskParent, 1, row.Key, f.session.ID, at))
			def, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "builtin", Mode: "implement", ProfileVersion: 1, Path: "implement.dot", Digest: strings.Repeat("d", 64), CreatedAt: at})
			mustProgress(t, err)
			instance, err := f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: f.session.ID, DefinitionID: def.ID, CurrentNode: "worker_agent", StartedAt: at})
			mustProgress(t, err)
			run, err := f.db.GetRun(t.Context(), f.opts.Attempt.RunID)
			mustProgress(t, err)
			segment, err := f.db.GetSegment(t.Context(), run.SegmentID)
			mustProgress(t, err)
			attempt, err := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: instance.ID, Node: "worker_agent", RunID: run.ID, Limit: 3, StartedAt: at})
			mustProgress(t, err)
			ref, err := naming.ParseRef(f.session.TaskRef)
			mustProgress(t, err)
			worktree := t.TempDir()
			mustProgress(t, os.MkdirAll(filepath.Join(worktree, ".aiteam/session"), 0700))
			input := []byte(`{"result":` + f.result + `}`)
			if phase == "invalid" {
				input = []byte(`{"result":{"outcome":"completed","comment":"Incomplete input"}}`)
			}
			var submitted AgentResultSubmission
			var submitErr error
			submit := func(ctx context.Context) { submitted, submitErr = f.service.Submit(ctx, "expiry-race", input) }
			runner := newStubRunner(func(ctx context.Context, _ agent.Request) (*agent.RunResult, error) {
				if phase == "before" || phase == "invalid" || phase == "during release" {
					submit(ctx)
				}
				if phase == "during release" {
					return &agent.RunResult{Outcome: agent.OutcomeSucceeded}, nil
				}
				<-ctx.Done()
				if phase == "after" {
					submit(t.Context())
				}
				if phase == "at cancelled boundary" {
					submit(ctx)
				}
				return &agent.RunResult{Outcome: agent.OutcomeFailed, Reason: "stopped at deadline"}, ctx.Err()
			})
			handler := AgentActivity{Policy: f.policy, Prepare: func(prepareCtx context.Context, _ Options, _ ActivityRequest) (ResearchInvocation, error) {
				return ResearchInvocation{Request: agent.Request{Delivery: fixtureNativeDelivery(t, f.policy), Mode: "implement", Ref: ref, RunOrdinal: run.Ordinal, SegmentOrdinal: segment.Ordinal, Session: agent.SessionRef{Ref: segment.SessionRef, Resume: segment.ResumeToken}, WorktreePath: worktree, Prompt: "Work", Permissions: agent.Permissions{Profile: "implement"}}, Kind: agent.Kind(store.AgentClaude), SessionPath: ".aiteam/session", Results: f.service, Release: func() error {
					if phase == "during release" {
						if prepareCtx.Err() != nil {
							t.Fatal("release fixture expired before teardown")
						}
						<-prepareCtx.Done()
					}
					return nil
				}}, nil
			}}
			opts := Options{DB: f.db, Runners: runnerSet(runner)}
			request := ActivityRequest{Mode: f.policy.Mode, Node: "worker_agent", InstanceID: instance.ID, NodeAttemptID: attempt.ID, RunID: run.ID}
			out, err := handler.Execute(t.Context(), opts, request)
			if err != nil || out.Outcome != "failed" || out.ProcessResult == nil || out.ProcessResult.Outcome != agent.OutcomeTimedOut {
				t.Fatalf("timeout settlement: %+v %v", out, err)
			}
			timeout, err := f.db.GetAttemptTimeout(t.Context(), attempt.ID)
			mustProgress(t, err)
			held, err := f.db.GetImplementStory(t.Context(), implementTaskParent, 1, row.Key)
			mustProgress(t, err)
			if held.TimeoutAttemptID != attempt.ID || held.Attempts != 0 || !strings.Contains(held.LastFailure, "effective limit") {
				t.Fatalf("timeout lost hold/cause: %+v", held)
			}
			durable, err := store.Open(t.Context(), f.db.Path())
			mustProgress(t, err)
			defer func() { mustProgress(t, durable.Close()) }()
			reopenedOptions := f.opts
			reopenedOptions.DB = durable
			resumedService, err := BindAgentResultService(t.Context(), reopenedOptions)
			mustProgress(t, err)
			receipt, receiptErr := resumedService.Receipt(t.Context(), run.ID, "expiry-race")
			switch phase {
			case "missing", "at cancelled boundary":
				if phase == "at cancelled boundary" && !errors.Is(submitErr, context.DeadlineExceeded) {
					t.Fatalf("expired input unexpectedly submitted: %v", submitErr)
				}
				if !errors.Is(receiptErr, store.ErrNotFound) || receipt.Receipt != nil {
					t.Fatalf("fabricated receipt: %+v %v", receipt, receiptErr)
				}
			default:
				if submitErr != nil || receiptErr != nil || submitted.Receipt == nil || receipt.Receipt == nil {
					t.Fatalf("lost real receipt: submit=%v lookup=%v", submitErr, receiptErr)
				}
				if !reflect.DeepEqual(submitted.Receipt.Latest, receipt.Receipt.Latest) || submitted.Receipt.InputState != receipt.Receipt.InputState || submitted.Receipt.AppliedRevision != receipt.Receipt.AppliedRevision {
					t.Fatalf("receipt dimensions changed: before=%+v after=%+v", submitted.Receipt, receipt.Receipt)
				}
				if receipt.Receipt.Latest.Engineering != "pending" || receipt.Receipt.Latest.Acceptance != "pending" || receipt.Receipt.Latest.Publication != "pending" {
					t.Fatalf("timeout manufactured engineering/acceptance/publication: %+v", receipt.Receipt.Latest)
				}
				if phase == "invalid" {
					if submitted.Validation.Valid || receipt.Receipt.InputState != "rejected" || receipt.Snapshot != nil {
						t.Fatalf("invalid input accepted: %+v", receipt)
					}
				} else if !submitted.Validation.Valid || receipt.Receipt.Latest.Input != "accepted" {
					t.Fatalf("valid input not retained: %+v", receipt)
				}
			}
			opts.DB = durable
			prepareCalls := 0
			handler.Prepare = func(context.Context, Options, ActivityRequest) (ResearchInvocation, error) {
				prepareCalls++
				return ResearchInvocation{}, errors.New("held work must not prepare")
			}
			replay, replayErr := handler.Execute(t.Context(), opts, request)
			if replayErr != nil || replay.Outcome != "failed" || prepareCalls != 0 {
				t.Fatalf("held restart launched: %+v %v calls=%d", replay, replayErr, prepareCalls)
			}
			timeoutAfter, err := durable.GetAttemptTimeout(t.Context(), attempt.ID)
			mustProgress(t, err)
			heldAfter, err := durable.GetImplementStory(t.Context(), implementTaskParent, 1, row.Key)
			mustProgress(t, err)
			if !reflect.DeepEqual(timeout, timeoutAfter) || heldAfter.TimeoutAttemptID != held.TimeoutAttemptID || heldAfter.LastFailure != held.LastFailure {
				t.Fatal("restart/replay changed durable timeout cause or hold")
			}
			finalReceipt, finalErr := resumedService.Receipt(t.Context(), run.ID, "expiry-race")
			if receipt.Receipt != nil {
				if finalErr != nil || finalReceipt.Receipt == nil || !reflect.DeepEqual(receipt.Receipt.Latest, finalReceipt.Receipt.Latest) || receipt.Receipt.InputState != finalReceipt.Receipt.InputState {
					t.Fatalf("held replay changed actual receipt: %+v %v", finalReceipt, finalErr)
				}
			} else if !errors.Is(finalErr, store.ErrNotFound) || finalReceipt.Receipt != nil {
				t.Fatalf("held replay fabricated a receipt: %+v %v", finalReceipt, finalErr)
			}
		})
	}
}

func TestWorkerDeadlineAcceptedResearchSurvivesExpiry(t *testing.T) {
	oldClock := agentBudgetClock
	agentBudgetClock = func(time.Duration) time.Duration { return 500 * time.Millisecond }
	defer func() { agentBudgetClock = oldClock }()
	for _, parentLimited := range []bool{false, true} {
		name := "worker deadline"
		if parentLimited {
			name = "parent deadline"
		}
		t.Run(name, func(t *testing.T) {
			f := newRetainedFixture(t, "research")
			f.submit(t, f.result)
			before, err := f.service.Receipt(t.Context(), f.opts.Attempt.RunID, "req-1")
			mustProgress(t, err)
			at := time.Now().UTC()
			definition, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "builtin", Mode: "research", ProfileVersion: 1, Path: "research.dot", Digest: strings.Repeat("c", 64), CreatedAt: at})
			mustProgress(t, err)
			instance, err := f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: f.session.ID, DefinitionID: definition.ID, CurrentNode: "work", StartedAt: at})
			mustProgress(t, err)
			run, err := f.db.GetRun(t.Context(), f.opts.Attempt.RunID)
			mustProgress(t, err)
			segment, err := f.db.GetSegment(t.Context(), run.SegmentID)
			mustProgress(t, err)
			attempt, err := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: instance.ID, Node: "work", RunID: run.ID, Limit: 1, StartedAt: at})
			mustProgress(t, err)
			ref, err := naming.ParseRef(f.session.TaskRef)
			mustProgress(t, err)
			worktree := t.TempDir()
			runner := newStubRunner(func(ctx context.Context, _ agent.Request) (*agent.RunResult, error) {
				<-ctx.Done()
				return &agent.RunResult{Outcome: agent.OutcomeFailed, Reason: "deadline after durable submission"}, ctx.Err()
			})
			handler := AgentActivity{Policy: f.policy, Prepare: func(context.Context, Options, ActivityRequest) (ResearchInvocation, error) {
				return ResearchInvocation{Request: agent.Request{Delivery: fixtureNativeDelivery(t, f.policy), Mode: "research", Ref: ref, RunOrdinal: run.Ordinal, SegmentOrdinal: segment.Ordinal, Session: agent.SessionRef{Ref: segment.SessionRef, Resume: segment.ResumeToken}, WorktreePath: worktree, Prompt: "Work", Permissions: agent.Permissions{Profile: "research"}}, Kind: agent.Kind(store.AgentClaude), SessionPath: ".aiteam/session", Results: f.service}, nil
			}}
			executionCtx := t.Context()
			if parentLimited {
				shortened, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
				defer cancel()
				executionCtx = shortened
			}
			out, err := handler.Execute(executionCtx, Options{DB: f.db, Runners: runnerSet(runner)}, ActivityRequest{Mode: f.policy.Mode, Node: "work", InstanceID: instance.ID, NodeAttemptID: attempt.ID, RunID: run.ID})
			if err != nil || out.Outcome != "success" || out.Kind != store.GraphOutputJSON || len(out.Output) == 0 {
				t.Fatalf("accepted Research answer discarded on expiry: %+v %v", out, err)
			}
			timeout, err := f.db.GetAttemptTimeout(t.Context(), attempt.ID)
			if err != nil || timeout.NodeAttemptID != attempt.ID {
				t.Fatalf("successful accepted answer lost timeout evidence: %+v %v", timeout, err)
			}
			after, err := f.service.Receipt(t.Context(), run.ID, "req-1")
			mustProgress(t, err)
			if before.Receipt == nil || after.Receipt == nil || !reflect.DeepEqual(before.Receipt.Latest, after.Receipt.Latest) || before.Receipt.AppliedRevision != after.Receipt.AppliedRevision {
				t.Fatalf("expiry changed actual Research receipt: before=%+v after=%+v", before.Receipt, after.Receipt)
			}
			var node agentresult.ResearchNode
			mustProgress(t, json.Unmarshal(out.Output, &node))
			retained, _, err := f.service.retainedCurrent(t.Context())
			mustProgress(t, err)
			if !agentresult.SameResult(node.Result, retained.Result) {
				t.Fatal("successful activity did not return the durable accepted answer")
			}
		})
	}
}
