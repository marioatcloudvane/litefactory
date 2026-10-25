package turn

import (
	"context"
	"errors"
	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/teamsource/teamsourcetest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

func bugSyncRestartFixture(t *testing.T) (*fixture, *graphexec.Profile, store.GraphInstance) {
	t.Helper()
	f := newFixture(t, nil)
	specs, err := SimplifiedBugSpecs()
	mustProgress(t, err)
	definition, err := teamsourcetest.CaptureBug(t.Context(), specs)
	mustProgress(t, err)
	profile := definition.Profile
	gate, ok := graphexec.BugFixerSyncGate(profile)
	if !ok {
		t.Fatal("canonical source lacks sync gate")
	}
	at := time.Now().UTC()
	saved, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: profile.Team, Workflow: "bug", ProfileVersion: 2, Path: "bug.dot", Digest: profile.Digest, CreatedAt: at})
	mustProgress(t, err)
	instance, err := f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: f.session.ID, DefinitionID: saved.ID, CurrentNode: gate.Name, StartedAt: at})
	mustProgress(t, err)
	return f, profile, instance
}

func completeBugSyncRestartAttempt(t *testing.T, f *fixture, instance store.GraphInstance, node, status, outcome, next string, outputs []store.GraphOutput) store.GraphNodeAttempt {
	t.Helper()
	at := time.Now().UTC()
	attempt, err := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: instance.ID, Node: node, StartedAt: at})
	mustProgress(t, err)
	completion := store.GraphCompletion{Status: status, Outcome: outcome, FinishedAt: at, InstanceStatus: store.GraphRunning, Outputs: outputs}
	if next != "" {
		completion.Transition = &store.GraphTransition{InstanceID: instance.ID, FromNode: node, ToNode: next, OnOutcome: outcome, At: at}
	}
	mustProgress(t, f.db.CompleteGraphNodeAttempt(t.Context(), attempt.ID, completion))
	return attempt
}

func TestBugFreshSyncRejectsBlockedInterruptedAndPreviouslyUsedEvidence(t *testing.T) {
	for _, tc := range []struct{ name, status, outcome, next string }{
		{"retry", store.GraphSucceeded, "retry", "sync_before_fixer"},
		{"conflict", store.GraphSucceeded, "conflict", ""},
		{"failed", store.GraphFailed, "", ""},
		{"successful without transition", store.GraphSucceeded, "synced", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, p, instance := bugSyncRestartFixture(t)
			gate, _ := graphexec.BugFixerSyncGate(p)
			completeBugSyncRestartAttempt(t, f, instance, gate.Name, tc.status, tc.outcome, tc.next, nil)
			if err := graphBugFreshSync(t.Context(), f.db, p, instance.ID, 0); err == nil {
				t.Fatal("unsuccessful synchronization authorized fixer")
			}
		})
	}
	for _, outcome := range []string{"synced", "current"} {
		t.Run(outcome, func(t *testing.T) {
			f, p, instance := bugSyncRestartFixture(t)
			gate, _ := graphexec.BugFixerSyncGate(p)
			fixer, _ := graphexec.Decide(p, gate.Name, outcome)
			if err := graphBugFreshSync(t.Context(), f.db, p, instance.ID, 0); err == nil {
				t.Fatal("missing sync admitted")
			}
			completeBugSyncRestartAttempt(t, f, instance, gate.Name, store.GraphSucceeded, outcome, fixer, nil)
			mustProgress(t, graphBugFreshSync(t.Context(), f.db, p, instance.ID, 0))
			attempt, err := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: instance.ID, Node: fixer, StartedAt: time.Now().UTC()})
			mustProgress(t, err)
			mustProgress(t, graphBugFreshSync(t.Context(), f.db, p, instance.ID, attempt.ID))
			if err = graphBugFreshSync(t.Context(), f.db, p, instance.ID, attempt.ID+1); err == nil {
				t.Fatal("foreign active allocation admitted")
			}
			mustProgress(t, f.db.CompleteGraphNodeAttempt(t.Context(), attempt.ID, store.GraphCompletion{Status: store.GraphFailed, FinishedAt: time.Now().UTC(), InstanceStatus: store.GraphRunning}))
			if err = graphBugFreshSync(t.Context(), f.db, p, instance.ID, 0); err == nil {
				t.Fatal("earlier successful sync reused after fixer")
			}
		})
	}
}

type bugSyncRestartCheckout struct{ refreshes int }

func (*bugSyncRestartCheckout) Guard(context.Context, string, string) (string, error) {
	return "saved", nil
}
func (c *bugSyncRestartCheckout) Refresh(context.Context, string, string) error {
	c.refreshes++
	return errors.New("retained checkout moved")
}

func TestBugSyncRecoveryRejectsMissingEvidenceAndFailedCheckoutHandoff(t *testing.T) {
	for _, raw := range []string{"", `{"result":"current","working":"work","base":"main","commit":"saved","checkout_verified":true}`} {
		t.Run(raw, func(t *testing.T) {
			f, p, instance := bugSyncRestartFixture(t)
			gate, _ := graphexec.BugFixerSyncGate(p)
			fixer, _ := graphexec.Decide(p, gate.Name, "current")
			var outputs []store.GraphOutput
			if raw != "" {
				outputs = []store.GraphOutput{{Kind: store.GraphOutputJSON, Payload: []byte(raw)}}
			}
			completeBugSyncRestartAttempt(t, f, instance, gate.Name, store.GraphSucceeded, "current", fixer, outputs)
			checkout := &bugSyncRestartCheckout{}
			binding := SyncBaseBinding{Bind: func(context.Context, Options, ActivityRequest) (SyncBaseTarget, error) {
				return SyncBaseTarget{Working: "work", Base: "main", Checkout: checkout, Lock: func(context.Context) (func() error, bool, error) { return func() error { return nil }, true, nil }}, nil
			}}
			if err := verifySyncHandoffBeforeAgent(t.Context(), f.opts, p, instance, binding); err == nil {
				t.Fatal("unverified recovered checkout admitted")
			}
			if raw == "" && checkout.refreshes != 0 {
				t.Fatal("missing evidence refreshed checkout")
			}
			if raw != "" && checkout.refreshes != 1 {
				t.Fatal("saved evidence was not reverified")
			}
		})
	}
}

func TestBugSyncGateCannotReplaceDiagnosisApproval(t *testing.T) {
	f, p, instance := bugSyncRestartFixture(t)
	gate, _ := graphexec.BugFixerSyncGate(p)
	called := false
	activity := graphBugSyncActivity{profile: p, binding: SyncBaseBinding{Bind: func(context.Context, Options, ActivityRequest) (SyncBaseTarget, error) {
		called = true
		return SyncBaseTarget{}, nil
	}}}
	if _, err := activity.Execute(t.Context(), f.opts, ActivityRequest{InstanceID: instance.ID, Node: gate.Name}); err == nil {
		t.Fatal("sync admitted without approved diagnosis")
	}
	if called {
		t.Fatal("source binding reached before repair approval")
	}
	attempts, err := f.db.ListGraphNodeAttempts(t.Context(), instance.ID)
	mustProgress(t, err)
	if len(attempts) != 0 {
		t.Fatal("approval rejection allocated repair work")
	}
}

// Run the concrete Bug loop from its retained capture after a process restart.
// Both a claimed fixer restart and an unverified successful handoff must return
// to the explicit gate before any provider permissions or preparation are used.
func TestBugRuntimeRestartReturnsThroughSyncBeforeFixerPermissions(t *testing.T) {
	for _, claimed := range []bool{false, true} {
		name := "missing handoff"
		if claimed {
			name = "claimed fixer restart"
		}
		t.Run(name, func(t *testing.T) {
			f, p, instance := approvedGraphSyncDiagnosticsFixture(t)
			ctx := t.Context()
			gate, _ := graphexec.BugFixerSyncGate(p)
			fixer, _ := graphexec.Decide(p, gate.Name, "synced")
			completeBugSyncRestartAttempt(t, f, instance, gate.Name, store.GraphSucceeded, "synced", fixer, nil)
			originalReply := int64(0)
			if claimed {
				_, err := parkGraphAction(ctx, f.opts, GraphNodeRequest{Profile: p, InstanceID: instance.ID, At: f.opts.MCP.Now()}, GraphNodeResult{InstanceID: instance.ID, Node: fixer, NextNode: fixer, Activity: ActivityResult{Diagnostic: "Fixer interrupted; retained work needs another attempt"}}, "exhausted", false)
				mustProgress(t, err)
				wait, err := f.db.GetGraphOwnerWait(ctx, instance.ID)
				mustProgress(t, err)
				delivery, err := f.db.GetReportDelivery(ctx, wait.DeliveryKey)
				mustProgress(t, err)
				originalReply = delivery.CommentID + 1000
				_, err = AdmitGraphBugOwnerReply(ctx, f.db, store.GraphOwnerReply{InstanceID: instance.ID, OwnerID: 7, AuthorID: 7, CommentID: originalReply, QuestionCommentID: delivery.CommentID, TaskRef: f.session.TaskRef, SourceRef: f.session.TaskRef, Workflow: "bug", DeliveryKey: wait.DeliveryKey, Body: "Continue the retained fixer work", At: f.opts.MCP.Now().Add(time.Minute)})
				mustProgress(t, err)
			}
			branch := "lf/123-sync-restart"
			_, err := f.db.FreezeSessionSlug(ctx, f.session.ID, "sync-restart", branch)
			mustProgress(t, err)
			workpath := filepath.Join(f.storeRepo.WorktreeRoot, "sync-restart")
			mustProgress(t, f.db.SetSessionWorktree(ctx, f.session.ID, workpath))
			clone, err := f.opts.Git.Open(ctx, f.repo.Dir)
			mustProgress(t, err)
			_, err = git.EnsureWorktree(ctx, clone, git.EnsureWorktreeOptions{Dir: workpath, Branch: branch, BaseBranch: gittest.DefaultBranch})
			mustProgress(t, err)
			f.repo.GitIn(workpath, "push", "origin", branch)
			dirty := filepath.Join(workpath, "retained.txt")
			mustProgress(t, os.WriteFile(dirty, []byte("unpublished retained repair\n"), 0600))
			locked, err := f.db.AcquireSessionLock(ctx, f.session.ID, f.opts.InstanceID)
			mustProgress(t, err)
			if !locked {
				t.Fatal("test task lock unavailable")
			}
			permissions := 0
			runtime := &BugRuntime{opts: f.opts, cfg: BugRuntimeConfig{OwnerID: 7, Permissions: func(agent.Kind, string) (BugRolePermissions, error) {
				permissions++
				return BugRolePermissions{}, errors.New("fixer permissions reached before safe sync")
			}}}
			_, err = runtime.executeGraph(ctx, Request{SessionID: f.session.ID, Workflow: agent.WorkflowBug}, Result{Session: f.session})
			mustProgress(t, err)
			if permissions != 0 {
				t.Fatal("unsafe retained checkout reached fixer permissions")
			}
			attempts, err := f.db.ListGraphNodeAttempts(ctx, instance.ID)
			mustProgress(t, err)
			syncs := 0
			for _, attempt := range attempts {
				if attempt.Node == fixer {
					t.Fatal("fixer dispatched while synchronization blocked")
				}
				if attempt.Node == gate.Name {
					syncs++
					if syncs > 1 && (attempt.Status != store.GraphSucceeded || attempt.Outcome != "conflict") {
						t.Fatalf("unsafe checkout sync=%+v", attempt)
					}
				}
			}
			if syncs != 2 {
				t.Fatalf("sync dispatches=%d want prior success plus one safety refusal", syncs)
			}
			transitions, err := f.db.ListGraphTransitions(ctx, instance.ID)
			mustProgress(t, err)
			rerouted := false
			for _, tr := range transitions {
				if tr.FromNode == fixer && tr.ToNode == gate.Name && tr.OnOutcome == "sync_required" {
					rerouted = true
				}
			}
			if !rerouted {
				t.Fatal("restart did not traverse declared sync prerequisite")
			}
			wait, err := f.db.GetGraphOwnerWait(ctx, instance.ID)
			mustProgress(t, err)
			if wait.State != "waiting" || wait.OriginNode != gate.Name {
				t.Fatalf("sync blockage did not park: %+v", wait)
			}
			body, err := os.ReadFile(dirty)
			mustProgress(t, err)
			if string(body) != "unpublished retained repair\n" {
				t.Fatal("restart changed retained repair")
			}
			if claimed {
				guidance, err := f.db.GraphDiagnosisGuidance(ctx, instance.ID)
				mustProgress(t, err)
				found := false
				for _, g := range guidance {
					if g.ReplyBody == "Continue the retained fixer work" {
						found = true
					}
				}
				if !found {
					t.Fatalf("original authenticated continuation %d lost", originalReply)
				}
			}
			delivery, err := f.db.GetReportDelivery(ctx, wait.DeliveryKey)
			mustProgress(t, err)
			if !strings.Contains(delivery.Body, "retained") {
				t.Fatal("parking omitted actual retained-work blockage")
			}
		})
	}
}
