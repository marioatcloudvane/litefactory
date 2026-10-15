package turn

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

func workerSyncBindingFixture(t *testing.T) (*fixture, *implementRuntime, ActivityRequest) {
	t.Helper()
	f, _, req := assembledImplementFixture(t, nil)
	seedImplementStory(t, f)
	raw, err := os.ReadFile("../teamsource/implement/implement.dot")
	mustProgress(t, err)
	profile, err := graphexec.ParseProfile(raw)
	mustProgress(t, err)
	definition := &teamsource.WorkflowDefinition{Profile: profile, Digest: strings.Repeat("a", 64)}
	pin, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: profile.Team, Mode: "implement", ProfileVersion: profile.ProfileVersion, Path: "fixture/implement.dot", Digest: profile.Digest, TeamSnapshotDigest: definition.Digest, CreatedAt: time.Now()})
	mustProgress(t, err)
	gate, ok := graphexec.ImplementWorkerSyncNode(profile)
	if !ok {
		t.Fatal("worker sync gate missing")
	}
	instance, err := f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: f.session.ID, DefinitionID: pin.ID, CurrentNode: gate.Name, StartedAt: time.Now()})
	mustProgress(t, err)
	attempt, err := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: instance.ID, Node: gate.Name, StartedAt: time.Now()})
	mustProgress(t, err)
	row := implementWorkerRow(t, f)
	rt := &implementRuntime{opts: f.opts, req: req, worker: &row, definition: definition}
	return f, rt, ActivityRequest{Workflow: agent.WorkflowImplement, Mode: agent.ModeImplement, Node: gate.Name, InstanceID: instance.ID, NodeAttemptID: attempt.ID}
}

func TestImplementWorkerSyncRejectsForeignIdentityBeforeCheckoutEffects(t *testing.T) {
	f, rt, request := workerSyncBindingFixture(t)
	mustProgress(t, rt.verifyWorkerSyncInvocation(t.Context(), request))
	instance, err := f.db.GetGraphInstance(t.Context(), request.InstanceID)
	mustProgress(t, err)
	other := f.otherSession(901)
	foreign, err := f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: other.ID, DefinitionID: instance.DefinitionID, CurrentNode: request.Node, StartedAt: time.Now()})
	mustProgress(t, err)
	foreignAttempt, err := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: foreign.ID, Node: request.Node, StartedAt: time.Now()})
	mustProgress(t, err)
	for _, invalid := range []ActivityRequest{
		{Workflow: request.Workflow, Mode: request.Mode, Node: request.Node, InstanceID: foreign.ID, NodeAttemptID: foreignAttempt.ID},
		{Workflow: request.Workflow, Mode: request.Mode, Node: request.Node, InstanceID: request.InstanceID, NodeAttemptID: foreignAttempt.ID},
	} {
		if _, err := rt.workerSyncBaseBinding().Bind(t.Context(), f.opts, invalid); err == nil {
			t.Fatalf("foreign invocation accepted: %+v", invalid)
		}
	}
	session, err := f.db.GetSession(t.Context(), f.session.ID)
	mustProgress(t, err)
	if session.Branch != "" || session.WorktreePath != "" {
		t.Fatalf("rejected identity allocated resources: %+v", session)
	}
}

func TestImplementWorkerSyncRecoversRecordedFirstPublication(t *testing.T) {
	f, rt, request := workerSyncBindingFixture(t)
	ctx := t.Context()
	var err error
	f.session, err = f.db.FreezeSessionSlug(ctx, f.session.ID, testSlug, "lf/story-001")
	mustProgress(t, err)
	locked, err := f.db.AcquireSessionLock(ctx, f.session.ID, f.opts.InstanceID)
	mustProgress(t, err)
	if !locked {
		t.Fatal("writer exclusion unavailable")
	}
	t.Cleanup(func() {
		mustProgress(t, f.db.ReleaseSessionLock(context.WithoutCancel(ctx), f.session.ID, f.opts.InstanceID))
	})
	clone, err := f.opts.Git.Open(ctx, f.repo.Dir)
	mustProgress(t, err)
	mustProgress(t, clone.Fetch(ctx, f.opts.remote()))
	retained, err := git.EnsureWorktree(ctx, clone, git.EnsureWorktreeOptions{Dir: filepath.Join(f.storeRepo.WorktreeRoot, "interrupted-initialization"), Branch: "lf/story-001", BaseBranch: f.storeRepo.BaseBranch, Remote: f.opts.remote()})
	mustProgress(t, err)
	mustProgress(t, f.db.SetSessionWorktree(ctx, f.session.ID, retained.Worktree.Path()))
	head, err := retained.Worktree.RevParse(ctx, "HEAD", git.RevParseOptions{})
	mustProgress(t, err)
	payload, err := json.Marshal(workerInitialization{Working: "lf/story-001", Base: f.storeRepo.BaseBranch, Commit: head, Session: f.session.ID})
	mustProgress(t, err)
	_, err = f.db.RecordGraphOutput(ctx, store.GraphOutput{NodeAttemptID: request.NodeAttemptID, Kind: store.GraphOutputJSON, Payload: payload, CreatedAt: time.Now()})
	mustProgress(t, err)
	// The process stopped after recording initialization and before publication.
	result, err := (workerSyncActivity{SyncBaseActivity{rt.workerSyncBaseBinding()}}).Execute(ctx, f.opts, request)
	mustProgress(t, err)
	var evidence syncBaseOutput
	mustProgress(t, json.Unmarshal(result.Output, &evidence))
	if result.Outcome != "current" || evidence.Commit != head || !evidence.CheckoutVerified {
		t.Fatalf("interrupted publication not reconciled: %+v %+v", result, evidence)
	}
	remote, err := clone.ShowRef(ctx, "refs/remotes/"+f.opts.remote()+"/lf/story-001")
	mustProgress(t, err)
	if remote != head {
		t.Fatalf("initialization publication=%s expected=%s", remote, head)
	}
}
