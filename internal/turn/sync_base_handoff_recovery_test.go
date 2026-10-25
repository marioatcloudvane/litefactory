package turn

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestRecoveredSyncHandoffRefreshesCleanCheckoutAndPreservesLaterWork(t *testing.T) {
	f := newFixture(t, nil)
	g := newSyncGit(t)
	retained, err := git.EnsureWorktree(t.Context(), g.clone, git.EnsureWorktreeOptions{Dir: filepath.Join(t.TempDir(), "retained"), Branch: implementFeature, BaseBranch: gittest.DefaultBranch})
	mustProgress(t, err)
	g.advance(gittest.DefaultBranch, "base.txt", "new base\n")
	result, _, err := g.syncBase()
	mustProgress(t, err)
	var evidence syncBaseOutput
	mustProgress(t, json.Unmarshal(result.Output, &evidence))
	evidence.CheckoutVerified = true
	profile := &graphexec.Profile{Nodes: []graphexec.Node{{Name: "sync", Action: graphexec.SyncBaseAction}, {Name: "agent", Action: ResearchAgentAction}}}
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	def, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "test", Mode: "implement", ProfileVersion: 1, Path: "test/recovery.dot", Digest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", CreatedAt: at})
	mustProgress(t, err)
	instance, err := f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: f.session.ID, DefinitionID: def.ID, Status: store.GraphRunning, CurrentNode: "sync", StartedAt: at})
	mustProgress(t, err)
	attempt, err := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: instance.ID, Node: "sync", StartedAt: at})
	mustProgress(t, err)
	raw, err := json.Marshal(evidence)
	mustProgress(t, err)
	_, err = f.db.RecordGraphOutput(t.Context(), store.GraphOutput{NodeAttemptID: attempt.ID, Kind: store.GraphOutputJSON, Payload: raw, CreatedAt: at})
	mustProgress(t, err)
	mustProgress(t, f.db.CompleteGraphNodeAttempt(t.Context(), attempt.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: result.Outcome, FinishedAt: at, InstanceStatus: store.GraphRunning, Transition: &store.GraphTransition{InstanceID: instance.ID, FromNode: "sync", ToNode: "agent", OnOutcome: result.Outcome, At: at}}))
	locks, releases := 0, 0
	binding := SyncBaseBinding{Bind: func(context.Context, Options, ActivityRequest) (SyncBaseTarget, error) {
		return SyncBaseTarget{Working: evidence.Working, Base: evidence.Base, Checkout: GitSyncBaseCheckout{Worktree: retained.Worktree, Author: git.Author{Name: "Test", Email: "test@example.invalid"}}, Lock: func(context.Context) (func() error, bool, error) {
			locks++
			return func() error { releases++; return nil }, true, nil
		}}, nil
	}}
	mustProgress(t, verifySyncHandoffBeforeAgent(t.Context(), f.opts, profile, instance, binding))
	head, err := retained.Worktree.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
	mustProgress(t, err)
	if head != evidence.Commit || locks != 1 || releases != 1 {
		t.Fatalf("recovery head=%s commit=%s locks=%d releases=%d", head, evidence.Commit, locks, releases)
	}
	dirty := filepath.Join(retained.Worktree.Path(), "retained.txt")
	mustProgress(t, os.WriteFile(dirty, []byte("retained work\n"), 0600))
	if err = verifySyncHandoffBeforeAgent(t.Context(), f.opts, profile, instance, binding); err == nil {
		t.Fatal("dirty checkout allowed dependent agent")
	}
	kept, err := os.ReadFile(dirty)
	mustProgress(t, err)
	if string(kept) != "retained work\n" {
		t.Fatal("recovery changed retained bytes")
	}
	_, err = f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: instance.ID, Node: "agent", StartedAt: at.Add(time.Second)})
	mustProgress(t, err)
	before := locks
	mustProgress(t, verifySyncHandoffBeforeAgent(t.Context(), f.opts, profile, instance, binding))
	if locks != before {
		t.Fatal("old sync was replayed after intervening agent")
	}
}

type unchangedSyncCheckout struct {
	head      string
	refreshes int
}

func (c *unchangedSyncCheckout) Guard(context.Context, string, string) (string, error) {
	return "", nil
}
func (c *unchangedSyncCheckout) Refresh(context.Context, string, string) error {
	c.refreshes++
	return nil
}
func (c *unchangedSyncCheckout) DeferredHead(context.Context, string) (string, error) {
	return c.head, nil
}

func TestCurrentUnchangedSyncHandoffVerifiesIdentityWithoutRefresh(t *testing.T) {
	f, profile, instance := bugSyncRestartFixture(t)
	gate, _ := graphexec.BugFixerSyncGate(profile)
	fixer, _ := graphexec.Decide(profile, gate.Name, "current")
	evidence := syncBaseOutput{Working: "retained", Base: "main", Result: "current", Commit: "original", CheckoutVerified: true, CheckoutUnchanged: true}
	raw, err := json.Marshal(evidence)
	mustProgress(t, err)
	completeBugSyncRestartAttempt(t, f, instance, gate.Name, store.GraphSucceeded, "current", fixer, []store.GraphOutput{{Kind: store.GraphOutputJSON, Payload: raw}})
	checkout := &unchangedSyncCheckout{head: "original"}
	binding := SyncBaseBinding{Bind: func(context.Context, Options, ActivityRequest) (SyncBaseTarget, error) {
		return SyncBaseTarget{Working: "retained", Base: "main", Checkout: checkout, Lock: func(context.Context) (func() error, bool, error) { return func() error { return nil }, true, nil }}, nil
	}}
	mustProgress(t, verifySyncHandoffBeforeAgent(t.Context(), f.opts, profile, instance, binding))
	if checkout.refreshes != 0 {
		t.Fatal("healthy retained handoff refreshed checkout")
	}
	checkout.head = "changed"
	if err = verifySyncHandoffBeforeAgent(t.Context(), f.opts, profile, instance, binding); err == nil {
		t.Fatal("changed retained identity reused current observation")
	}
	if checkout.refreshes != 0 {
		t.Fatal("failed handoff refreshed checkout")
	}
}
