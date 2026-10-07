package daemon

import (
	"io"
	"log/slog"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

type researchRecoveryFixture struct {
	db       *store.DB
	session  store.Session
	run      store.Run
	instance store.GraphInstance
	attempt  store.GraphNodeAttempt
	at       time.Time
}

func researchRecoveryCheck(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func newResearchRecoveryFixture(t *testing.T) *researchRecoveryFixture {
	t.Helper()
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "recovery.db"))
	researchRecoveryCheck(t, err)
	f := &researchRecoveryFixture{db: db, at: time.Date(2026, 9, 19, 1, 0, 0, 0, time.UTC)}
	t.Cleanup(func() { researchRecoveryCheck(t, f.db.Close()) })
	researchRecoveryCheck(t, db.Migrate(ctx))
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "research", RemoteURL: "git@github.com:acme/research.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	researchRecoveryCheck(t, err)
	f.session, err = db.GetOrCreateSession(ctx, repo, 342)
	researchRecoveryCheck(t, err)
	segment, err := db.GetOrCreateSegment(ctx, f.session.ID, 1, "research", store.AgentClaude)
	researchRecoveryCheck(t, err)
	f.run, err = db.AllocateRun(ctx, store.AllocateRunParams{SessionID: f.session.ID, SegmentID: segment.ID, Mode: "research", Agent: store.AgentClaude, TriggerCommentID: 20, LogPath: func(int) string { return "research.log" }})
	researchRecoveryCheck(t, err)
	definition, err := db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "builtin", Mode: "research", ProfileVersion: 1, Path: "research.dot", Digest: strings.Repeat("a", 64), CreatedAt: f.at})
	researchRecoveryCheck(t, err)
	f.instance, err = db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: f.session.ID, DefinitionID: definition.ID, CurrentNode: "research", StartedAt: f.at, NextDueAt: f.at})
	researchRecoveryCheck(t, err)
	f.attempt, err = db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: f.instance.ID, Node: "research", RunID: f.run.ID, Limit: 3, StartedAt: f.at})
	researchRecoveryCheck(t, err)
	return f
}

func (f *researchRecoveryFixture) restart(t *testing.T) Reconciled {
	t.Helper()
	path := f.db.Path()
	researchRecoveryCheck(t, f.db.Close())
	var err error
	f.db, err = store.Open(t.Context(), path)
	researchRecoveryCheck(t, err)
	researchRecoveryCheck(t, f.db.Migrate(t.Context()))
	// No runner or forge is configured: startup must neither invoke nor publish.
	d := &Daemon{db: f.db, instanceID: "replacement", now: func() time.Time { return f.at.Add(time.Hour) }, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	result, err := d.Start(t.Context())
	researchRecoveryCheck(t, err)
	return result
}

func (f *researchRecoveryFixture) authored(t *testing.T, question bool) {
	t.Helper()
	ctx := t.Context()
	researchRecoveryCheck(t, f.db.CompleteGraphNodeAttempt(ctx, f.attempt.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "success", FinishedAt: f.at, Outputs: []store.GraphOutput{{Kind: store.GraphOutputJSON, Payload: []byte(`{"response":"ask","findings":"retained"}`)}}}))
	kind := "final"
	if question {
		kind = "question"
	}
	researchRecoveryCheck(t, f.db.RetainGraphComment(ctx, store.GraphCommentIntent{Key: "original-operation", InstanceID: f.instance.ID, SourceAttemptID: f.attempt.ID, Node: "comment", Kind: kind, Candidate: []byte(`{"outcome":"incomplete"}`)}))
	_, err := f.db.EnsureReportDelivery(ctx, store.ReportDelivery{Key: "original-operation", SessionID: f.session.ID, RunID: f.run.ID, TriggerID: 20, Task: f.session.TaskRef, Destination: "acme/research#342", TriggerNamespace: "issue-comment", Purpose: "research-" + kind, BotUserID: 99, Body: "Exact authored text", Marker: "original-operation"})
	researchRecoveryCheck(t, err)
}

func TestResearchRecoveryStartupBoundaries(t *testing.T) {
	for _, boundary := range []string{"running", "timeout", "retained", "uncertain"} {
		t.Run(boundary, func(t *testing.T) {
			f := newResearchRecoveryFixture(t)
			ctx := t.Context()
			if boundary == "timeout" {
				_, err := f.db.FinishRun(ctx, f.run.ID, store.RunOutcome{Status: store.RunFailed, Error: "timeout", FinishedAt: f.run.StartedAt.Add(time.Second)})
				researchRecoveryCheck(t, err)
			}
			var before []store.GraphOutput
			var delivery store.ReportDelivery
			if boundary == "retained" || boundary == "uncertain" {
				f.authored(t, false)
				var err error
				before, err = f.db.ListGraphOutputs(ctx, f.attempt.ID)
				researchRecoveryCheck(t, err)
				if boundary == "uncertain" {
					_, err = f.db.MarkReportDeliverySent(ctx, "original-operation")
					researchRecoveryCheck(t, err)
				}
				delivery, err = f.db.GetReportDelivery(ctx, "original-operation")
				researchRecoveryCheck(t, err)
			}
			result := f.restart(t)
			if result.Reported != 0 {
				t.Fatal("startup published private node failure")
			}
			attempts, err := f.db.ListGraphNodeAttempts(ctx, f.instance.ID)
			researchRecoveryCheck(t, err)
			if len(attempts) != 1 {
				t.Fatalf("startup allocated attempts: %+v", attempts)
			}
			run, err := f.db.GetRun(ctx, f.run.ID)
			researchRecoveryCheck(t, err)
			if run.Succeeded() || run.Status != store.RunFailed {
				t.Fatalf("startup hid failed process: %+v", run)
			}
			if boundary == "timeout" && run.Error != "timeout" {
				t.Fatalf("timeout overwritten: %+v", run)
			}
			after, err := f.db.ListGraphOutputs(ctx, f.attempt.ID)
			researchRecoveryCheck(t, err)
			if before != nil {
				if attempts[0].Status != store.GraphSucceeded || !reflect.DeepEqual(before, after) {
					t.Fatalf("authored output changed: %+v %+v", attempts, after)
				}
				got, e := f.db.GetReportDelivery(ctx, "original-operation")
				researchRecoveryCheck(t, e)
				if !reflect.DeepEqual(got, delivery) {
					t.Fatalf("recovery changed effect identity/state: before=%+v after=%+v", delivery, got)
				}
			} else if attempts[0].Status != store.GraphFailed || len(after) != 1 || string(after[0].Payload) != run.Error {
				t.Fatalf("failed attempt lost cause: %+v outputs=%+v run=%+v", attempts, after, run)
			}
			f.restart(t)
			again, err := f.db.ListGraphOutputs(ctx, f.attempt.ID)
			researchRecoveryCheck(t, err)
			if !reflect.DeepEqual(after, again) {
				t.Fatal("repeated startup duplicated diagnostics or results")
			}
			pending, err := f.db.ListPendingContinuations(ctx, 0)
			researchRecoveryCheck(t, err)
			if len(pending) != 0 {
				t.Fatal("startup created automatic retry")
			}
		})
	}
}

func TestResearchRecoveryOwnerClaimSurvivesStartupUntilAllocation(t *testing.T) {
	f := newResearchRecoveryFixture(t)
	ctx := t.Context()
	f.authored(t, true)
	_, err := f.db.MarkReportDeliverySent(ctx, "original-operation")
	researchRecoveryCheck(t, err)
	_, err = f.db.ConfirmReportDelivery(ctx, "original-operation", 101)
	researchRecoveryCheck(t, err)
	_, err = f.db.CompleteReportDelivery(ctx, "original-operation")
	researchRecoveryCheck(t, err)
	claim, err := f.db.ClaimGraphOwnerReply(ctx, store.GraphOwnerReply{InstanceID: f.instance.ID, OwnerID: 7, AuthorID: 7, CommentID: 102, QuestionCommentID: 101, TaskRef: f.session.TaskRef, SourceRef: f.session.TaskRef, Mode: "research", DeliveryKey: "original-operation", Body: "Continue saved findings", At: f.at.Add(time.Minute)})
	researchRecoveryCheck(t, err)
	f.restart(t)
	got, err := f.db.GetGraphOwnerWait(ctx, f.instance.ID)
	researchRecoveryCheck(t, err)
	if !reflect.DeepEqual(claim, got) {
		t.Fatalf("unallocated claim lost: %+v %+v", claim, got)
	}
	run, err := f.db.AllocateRun(ctx, store.AllocateRunParams{SessionID: f.session.ID, SegmentID: f.run.SegmentID, Mode: "research", Agent: store.AgentClaude, TriggerCommentID: 102, LogPath: func(int) string { return "reply.log" }})
	researchRecoveryCheck(t, err)
	attempt, err := f.db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: f.instance.ID, Node: "research", RunID: run.ID, Limit: 3, StartedAt: f.at.Add(time.Hour)})
	researchRecoveryCheck(t, err)
	f.restart(t)
	got, err = f.db.GetGraphOwnerWait(ctx, f.instance.ID)
	researchRecoveryCheck(t, err)
	if got.State != "allocated" || got.ContinuationAttemptID != attempt.ID || got.ReplyBody != claim.ReplyBody {
		t.Fatalf("allocated reply lost: %+v", got)
	}
	attempts, err := f.db.ListGraphNodeAttempts(ctx, f.instance.ID)
	researchRecoveryCheck(t, err)
	if len(attempts) != 2 || attempts[1].Status != store.GraphFailed {
		t.Fatalf("allocated reply replayed: %+v", attempts)
	}
}

func TestResearchRecoveryCancellationPreservesFindingsAndUncertainEffects(t *testing.T) {
	for _, reason := range []string{"issue closed", "explicit mode switch", "worktree retired"} {
		for _, paused := range []bool{false, true} {
			name := reason + "/active"
			if paused {
				name = reason + "/paused"
			}
			t.Run(name, func(t *testing.T) {
				f := newResearchRecoveryFixture(t)
				ctx := t.Context()
				if paused {
					f.authored(t, true)
				} else {
					_, err := f.db.RecordGraphOutput(ctx, store.GraphOutput{NodeAttemptID: f.attempt.ID, Kind: store.GraphOutputText, Payload: []byte("unfinished findings"), CreatedAt: f.at})
					researchRecoveryCheck(t, err)
				}
				var before store.ReportDelivery
				if paused {
					_, err := f.db.MarkReportDeliverySent(ctx, "original-operation")
					researchRecoveryCheck(t, err)
					before, err = f.db.GetReportDelivery(ctx, "original-operation")
					researchRecoveryCheck(t, err)
				}
				if reason == "worktree retired" {
					id, e := f.db.RecordRetirementIntent(ctx, store.RetirementIntent{SessionID: f.session.ID, SweepID: "sweep", Kind: "worktree", Resource: "/retired/worktree", Action: "remove", ObservedAt: f.at})
					researchRecoveryCheck(t, e)
					researchRecoveryCheck(t, f.db.RecordRetirementOutcome(ctx, id, "removed", "operator retirement", true))
				} else {
					researchRecoveryCheck(t, f.db.CancelResearchGraphs(ctx, f.session.ID, reason, f.at.Add(time.Hour)))
				}
				// Cancellation bookkeeping does not manufacture a successful subprocess.
				run, err := f.db.GetRun(ctx, f.run.ID)
				researchRecoveryCheck(t, err)
				if run.Status != store.RunRunning || run.Succeeded() {
					t.Fatalf("cancellation rewrote process outcome: %+v", run)
				}
				f.restart(t)
				instance, err := f.db.GetGraphInstance(ctx, f.instance.ID)
				researchRecoveryCheck(t, err)
				if instance.Status != store.GraphCancelled || !instance.NextDueAt.IsZero() {
					t.Fatalf("cancelled graph still schedulable: %+v", instance)
				}
				if paused {
					after, err := f.db.GetReportDelivery(ctx, "original-operation")
					researchRecoveryCheck(t, err)
					if !reflect.DeepEqual(before, after) {
						t.Fatalf("cancellation changed uncertainty: %+v %+v", before, after)
					}
				}
				outputs, err := f.db.ListGraphOutputs(ctx, f.attempt.ID)
				researchRecoveryCheck(t, err)
				if paused {
					if len(outputs) != 1 || !strings.Contains(string(outputs[0].Payload), "retained") {
						t.Fatalf("authored output lost: %+v", outputs)
					}
					wait, e := f.db.GetGraphOwnerWait(ctx, f.instance.ID)
					researchRecoveryCheck(t, e)
					if wait.State != "waiting" {
						t.Fatalf("wait evidence lost: %+v", wait)
					}
				} else if len(outputs) != 2 || string(outputs[0].Payload) != "unfinished findings" || string(outputs[1].Payload) != reason {
					t.Fatalf("cancellation findings/cause: %+v", outputs)
				}
			})
		}
	}
}

func TestResearchRecoveryInterruptedRunCannotCreateAutomaticContinuation(t *testing.T) {
	f := newResearchRecoveryFixture(t)
	ctx := t.Context()
	at := f.run.StartedAt.Add(time.Second)
	intent := &store.Continuation{ResetBound: at.Add(time.Hour), DueAt: at.Add(2 * time.Hour), Precision: "exact", Context: store.ContinuationContext{Task: f.session.TaskRef, Worktree: "/worktree", Agent: f.run.Agent, Mode: "research", Request: "research original request", ResumeIdentity: "saved-session", SessionRef: "session-ref", SegmentOrdinal: 1, OwnerCommentID: f.run.TriggerCommentID, ApprovedGeneration: 1}}
	run, continuation, err := f.db.FinishInterruptedRun(ctx, f.run.ID, store.RunOutcome{Status: store.RunFailed, SemanticOutcome: "interrupted", Error: "quota exhaustion", FinishedAt: at}, intent, "saved-session")
	researchRecoveryCheck(t, err)
	if continuation != nil || run.Succeeded() || run.Error != "quota exhaustion" {
		t.Fatalf("graph interruption created retry or lost cause: run=%+v continuation=%+v", run, continuation)
	}
	f.restart(t)
	pending, err := f.db.ListPendingContinuations(ctx, 0)
	researchRecoveryCheck(t, err)
	if len(pending) != 0 {
		t.Fatalf("graph quota retry: %+v", pending)
	}
	_, err = f.db.CompleteRunReport(ctx, f.run.ID, 123)
	researchRecoveryCheck(t, err)
	got, err := f.db.GetRun(ctx, f.run.ID)
	researchRecoveryCheck(t, err)
	if got.Succeeded() || got.Error != "quota exhaustion" || got.SemanticOutcome != "interrupted" {
		t.Fatalf("report bookkeeping rewrote failure: %+v", got)
	}
}
