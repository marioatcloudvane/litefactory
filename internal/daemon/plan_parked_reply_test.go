package daemon_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/cloudvane/litefactory/internal/teamsource/teamsourcetest"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
	"github.com/cloudvane/litefactory/internal/turn"
)

// #1007 C7: a parent-task owner free-text comment whose retained launch is
// bound to a finished Plan instance is answered with the "not applied" reason
// and consumed by the real daemon completion path, instead of being deferred as
// a discussion without a confirmed answer on every retry.
func TestPlanParkedReplyDaemon_ParentCommentOnFinishedInstanceIsAnsweredAndConsumed(t *testing.T) {
	const notApplied = "This answer was not applied; please post it again."
	ctx := t.Context()
	f := newFixture(t)
	f.plantCursor()
	f.issueWithLabels("Shape stories", "Mode: Plan")
	f.settle()
	session := f.sessionOf(testIssue)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err := f.db.AdmitPlanWorkflowPath(ctx, session.ID, "graph")
	check(err)

	// The launch of comment 40 is bound to an instance that has since ended, as
	// it was before #1056 prevented the binding.
	now := f.clock.Now()
	definition, err := f.db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "core", Mode: "plan", ProfileVersion: 1, Path: "core/plan.dot", Digest: strings.Repeat("b", 64), CreatedAt: now})
	check(err)
	admission, err := f.db.AdmitWorkflowLaunch(ctx, store.WorkflowLaunch{Mode: "plan", SessionID: session.ID, TriggerID: planFailureOwnerComment, DefinitionID: definition.ID, Start: "select_set", ContentDigest: strings.Repeat("c", 64), TopologyDigest: strings.Repeat("d", 64), Files: []byte(`{"plan.dot":"ZGlncmFwaCB7fQ=="}`), Reply: "1A, 2A"})
	check(err)
	check(f.db.SetGraphInstanceStatus(ctx, admission.Instance.ID, store.GraphSucceeded, time.Time{}, now))

	binary, err := git.Resolve(ctx)
	check(err)
	authority, err := turn.NewAttemptAuthority(ctx, f.db)
	check(err)
	t.Cleanup(func() { _ = authority.Close(context.WithoutCancel(ctx)) })
	agentCalls := 0
	runner := retryPlanRunner(func(context.Context, agent.Request) (*agent.RunResult, error) {
		agentCalls++
		return nil, errors.New("a retired comment must not reach an agent")
	})
	opts := turn.Options{DB: f.db, Git: binary, Paths: f.paths, InstanceID: "parked-reply", Logger: f.logger, CloneGate: make(lifecycleCloneGate, 1), Runners: turn.RunnerSet{"claude": runner}, Author: git.Author{Name: "Test", Email: "test@example.invalid"}, ResearchReporter: f.reporter, SubIssueCreator: f.client, MCP: &turn.MCPRuntime{Authority: authority, Paths: f.paths, Executable: "/factory/litefactory", Redactor: redact.New(redact.Options{}), Comments: f.client, BotID: botID, Now: f.clock.Now}}
	opts.WorkflowSource = func(ctx context.Context, _ store.Repo, _ agent.Workflow, specs graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
		def, err := teamsourcetest.CapturePlan(ctx, specs)
		if err != nil {
			return nil, err
		}
		configBytes := []byte("plan: plan/plan.dot\n")
		digest := sha256.Sum256(configBytes)
		def.Origin = teamsource.WorkflowOrigin{Kind: "local", Source: "local", Slug: "fixture", Version: "1.0.0", GraphPath: "plan/plan.dot", GraphFile: "plan.dot", Team: def.Profile.Team, TeamConfig: configBytes, ConfigDigest: hex.EncodeToString(digest[:])}
		return def, nil
	}
	f.daemon = f.newDaemon(daemon.Options{Turn: daemon.NewExecutor(opts), Agents: opts.Runners})

	f.forge.addComment(testIssue, planFailureOwnerComment, owner(), "1A, 2A")
	if !settleUntilHandled(f, planFailureOwnerComment, 4) {
		t.Fatalf("parent comment on a finished instance was never consumed: watermark=%d logs=%s", f.watermark(), f.logs)
	}
	posted, _, _ := f.forge.snapshot()
	replies := 0
	for _, body := range posted {
		if strings.Contains(body, notApplied) {
			replies++
		}
	}
	logs := f.logs.String()
	if replies != 1 || agentCalls != 0 || strings.Contains(logs, "plan discussion has no confirmed answer") || strings.Contains(logs, "graph instance already terminal") {
		t.Fatalf("finished binding not answered once: replies=%d agent=%d logs=%s", replies, agentCalls, logs)
	}
	// Handled once: later ticks neither post again nor re-run the comment.
	for range 2 {
		f.clock.advance(time.Minute)
		f.settle()
	}
	posted, _, _ = f.forge.snapshot()
	again := 0
	for _, body := range posted {
		if strings.Contains(body, notApplied) {
			again++
		}
	}
	if again != 1 {
		t.Fatalf("not-applied reply posted %d times", again)
	}
}
