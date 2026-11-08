package daemon_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// A real Validate issue activity files the Bug; the daemon then admits it with
// its configured Bug preference, independent of the Claude source reference.
func TestValidateFiledBugUsesConfiguredAgent(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	runtime, err := turn.NewGraphBugRuntime(ctx, turn.Options{DB: f.db, Git: new(git.Binary), CloneGate: unusedRecoveryCloneGate{}, ResearchReporter: f.reporter, MCP: &turn.MCPRuntime{Redactor: redact.New(redact.Options{Logger: f.logger}), BotID: botID}}, turn.BugRuntimeConfig{Code: f.client, Tickets: f.client, OwnerID: ownerID, Permissions: func(agent.Kind, string) (turn.BugRolePermissions, error) { return turn.BugRolePermissions{}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	f.daemon = f.newDaemon(daemon.Options{Bug: runtime, BugAgent: "codex", Agents: turn.RunnerSet{"claude": nil, "codex": nil}})
	f.plantCursor()
	sourceNumber := testIssue - 1
	f.forge.addIssue(fakeIssue{Number: sourceNumber, Title: "Validate source", User: stranger(), Labels: toFakeLabels("Coding Agent: Claude")})
	source, err := f.db.GetOrCreateSession(ctx, f.repo, sourceNumber)
	if err != nil {
		t.Fatal(err)
	}
	at := f.clock.Now()
	profile := &graphexec.Profile{Team: "test", Mode: agent.ModeValidate, ProfileVersion: 1, Digest: strings.Repeat("a", 64), Start: "suite", Nodes: []graphexec.Node{{Name: "suite", Action: graphexec.ValidateSuiteAction}, {Name: "issue", Action: graphexec.ValidateIssueAction}}}
	definition, err := f.db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: profile.Team, Mode: "validate", ProfileVersion: 1, Path: "validate.dot", Digest: profile.Digest, CreatedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	instance, err := f.db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: source.ID, DefinitionID: definition.ID, CurrentNode: "suite", StartedAt: at, NextDueAt: at})
	if err != nil {
		t.Fatal(err)
	}
	suite, err := f.db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: instance.ID, Node: "suite", StartedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	commit := strings.Repeat("b", 40)
	payload, err := json.Marshal(map[string]string{"commit": commit, "outcome": "passed"})
	if err != nil {
		t.Fatal(err)
	}
	err = f.db.CompleteGraphNodeAttempt(ctx, suite.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "passed", FinishedAt: at, NextDueAt: at, Outputs: []store.GraphOutput{{Kind: store.GraphOutputJSON, Payload: payload, CreatedAt: at}}, Transition: &store.GraphTransition{InstanceID: instance.ID, FromNode: "suite", ToNode: "issue", OnOutcome: "passed", At: at}})
	if err != nil {
		t.Fatal(err)
	}
	assessment, err := json.Marshal([]turn.ValidateJoinedStory{{Story: "story-001", Reviews: []agent.ContributionFinding{{Name: "code", Status: "unsatisfied", Classification: "blocking", Evidence: []agent.EvidenceExplanation{{Summary: "A concrete failure needs its own Bug."}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.PutValidateOutcome(ctx, store.ValidateOutcome{InstanceID: instance.ID, Commit: commit, Outcome: store.ValidateAssessed, Assessment: assessment, CreatedAt: at}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.PutValidateChoice(ctx, store.ValidateChoice{InstanceID: instance.ID, Commit: commit, Finding: "story-001:code", Choice: store.ChoiceIssue, IssueKind: "bug", CommentID: 42, AuthorID: ownerID, CreatedAt: at}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = f.db.BeginValidateEffect(ctx, store.ValidateEffect{InstanceID: instance.ID, Kind: store.EffectIssue, Commit: commit, Target: "story-001:code", ConfirmCommentID: 42, CreatedAt: at}); err != nil {
		t.Fatal(err)
	}
	attempt, err := f.db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: instance.ID, Node: "issue", StartedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	var specs graphexec.Registry
	var activities turn.ActivityRegistry
	target := turn.ValidateDeliveryTarget{Task: naming.Ref{Forge: "github", Owner: testOwnerName, Repo: testRepoName, Issue: sourceNumber}, Repo: github.Repo{Owner: testOwnerName, Name: testRepoName}, Parent: turn.ValidateIssueRef{Number: sourceNumber}, BotID: botID}
	bindings := turn.ValidateFlowBindings{Profile: profile, Now: f.clock.Now, Delivery: turn.ValidateDelivery{Forge: f.client, Redactor: redact.New(redact.Options{Environ: []string{}}), Target: func(context.Context, turn.Options, turn.ActivityRequest) (turn.ValidateDeliveryTarget, error) {
		return target, nil
	}, Head: func(context.Context) (string, error) { return commit, nil }, Publish: func(context.Context, string, string) error { return nil }, OwnerID: func(context.Context, turn.Options) (int64, error) { return ownerID, nil }}}
	if err = turn.RegisterValidateFlow(&specs, &activities, bindings); err != nil {
		t.Fatal(err)
	}
	activity, ok := activities.Resolve(graphexec.ValidateIssueAction)
	if !ok {
		t.Fatal("Validate issue activity unavailable")
	}
	if _, err = activity.Execute(ctx, turn.Options{DB: f.db, Logger: f.logger}, turn.ActivityRequest{Mode: agent.ModeValidate, InstanceID: instance.ID, Node: "issue", NodeAttemptID: attempt.ID}); err != nil {
		t.Fatal(err)
	}
	issue, err := f.client.GetIssue(ctx, target.Repo, testIssue)
	if err != nil {
		t.Fatal(err)
	}
	if len(issue.Labels) != 1 || issue.Labels[0] != "bug" || !strings.Contains(issue.Body, "#122") {
		t.Fatalf("unexpected Validate filing: %+v", issue)
	}
	f.settle()
	if len(f.turns.calls()) != 0 {
		t.Fatal("factory-filed body bypassed owner authorization")
	}
	f.forge.addComment(testIssue, 100, owner(), "Investigate this filed Bug")
	f.settle()
	calls := f.turns.calls()
	if len(calls) != 1 || calls[0].Workflow != agent.WorkflowBug || calls[0].Agent != "codex" {
		t.Fatalf("Validate Bug selection=%+v logs=%s", calls, f.logs)
	}
	issue, err = f.client.GetIssue(ctx, target.Repo, testIssue)
	if err != nil {
		t.Fatal(err)
	}
	if len(issue.Labels) != 1 || issue.Labels[0] != "bug" {
		t.Fatalf("agent label written: %v", issue.Labels)
	}
}
