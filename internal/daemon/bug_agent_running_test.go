package daemon_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/turn"
)

func TestBugExecutingRequestKeepsSelectedAgent(t *testing.T) {
	f := newFixture(t)
	runtime, err := turn.NewGraphBugRuntime(t.Context(), turn.Options{DB: f.db, Git: new(git.Binary), CloneGate: unusedRecoveryCloneGate{}, ResearchReporter: f.reporter, MCP: &turn.MCPRuntime{Redactor: redact.New(redact.Options{Logger: f.logger}), BotID: botID}}, turn.BugRuntimeConfig{Forge: f.client, OwnerID: ownerID, Permissions: func(agent.Kind, string) (turn.BugRolePermissions, error) { return turn.BugRolePermissions{}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan turn.Request, 1)
	released := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(released) }) }
	defer func() {
		release()
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 5*time.Second)
		defer cancel()
		if err := f.daemon.Quiesce(ctx); err != nil {
			t.Errorf("joining Bug invocation: %v", err)
		}
	}()
	observed := make(chan agent.Kind, 1)
	f.turns.fn = func(ctx context.Context, req turn.Request) (turn.Result, error) {
		started <- req
		select {
		case <-released:
		case <-ctx.Done():
			return turn.Result{}, ctx.Err()
		}
		observed <- req.Agent
		return turn.Result{}, nil
	}
	f.daemon = f.newDaemon(daemon.Options{Bug: runtime, Agents: turn.RunnerSet{agent.Kind("claude"): nil, agent.Kind("codex"): nil}})
	f.plantCursor()
	f.forge.addIssue(fakeIssue{Number: testIssue, Title: "Pinned Bug", Body: "Investigate", User: owner(), Labels: toFakeLabels("bug", "Coding Agent: Codex")})
	f.tick()
	select {
	case req := <-started:
		if req.Agent != agent.Kind("codex") {
			t.Fatalf("selected=%s", req.Agent)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Bug dispatch did not start")
	}
	f.issueWithLabels("Pinned Bug", "bug", "Coding Agent: Claude")
	f.tick()
	if calls := f.turns.calls(); len(calls) != 1 {
		t.Fatalf("running invocation was replaced: %+v", calls)
	}
	release()
	if err := f.daemon.Quiesce(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-observed:
		if got != agent.Kind("codex") {
			t.Fatalf("running agent changed: %s", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Bug dispatch did not finish")
	}
}
