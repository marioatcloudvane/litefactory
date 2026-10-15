package turn

import (
	"context"
	"errors"
	"github.com/cloudvane/litefactory/internal/providerprocess"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"

	"github.com/cloudvane/litefactory/internal/github"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

// The candidate is authored by the factory, not the agent, so nothing else
// checks it before it reaches the owner. It has to clear the same result
// validation and the same failure-kind guard a published candidate clears.
func TestResearchFailureCandidateIsPublishable(t *testing.T) {
	scope := agentresult.Context{SchemaVersion: agentresult.SchemaVersion, Mode: "research", WorkflowVersion: store.WorkflowSeparated, QuestionsAllowed: true}
	for _, tc := range []struct {
		name  string
		req   Request
		cause error
	}{
		{"diagnostic", Request{}, errors.New("research node result was rejected: $.result.reason blank")},
		{"no cause", Request{}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := researchFailureCandidate(tc.req, tc.cause)
			if d := agentresult.ValidateResult(candidate, scope); !d.Valid || !d.Complete {
				t.Fatalf("candidate is not a valid result: %+v", d.Diagnostics)
			}
			// The guard in ReportWorkflowCandidate for kind "failure".
			if candidate.Outcome == "completed" || candidate.Reason == "" {
				t.Fatalf("candidate fails the failure-kind guard: %+v", candidate)
			}
			if len(candidate.Questions) != 0 {
				t.Error("a failed attempt must not ask the owner anything")
			}
		})
	}
}

// C7 (#410): when the investigate agent's activity returns an error, the owner
// is told. Exactly one declared failure comment is published with the cause and
// how to retry, and the workflow still returns the original error.
func TestResearchAgentActivityErrorPublishesTheFailureComment(t *testing.T) {
	const marker = "provider transport lost: marker-410"
	runner := lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
		return &agent.RunResult{Outcome: agent.OutcomeFailed, Reason: "provider crashed", FinishedAt: time.Now()}, errors.New(marker)
	})
	f, forge, req := assembledResearchFixture(t, runner)
	out, err := Execute(t.Context(), f.opts, req)
	if err == nil || !strings.Contains(err.Error(), marker) || out.Run.Status != store.RunFailed {
		t.Fatalf("the original cause was lost: run=%+v err=%v", out.Run, err)
	}
	posts := forge.all()
	if len(posts) != 1 || forge.attemptCount() != 1 {
		t.Fatalf("want exactly one failure comment, got %d posts (%d attempts)", len(posts), forge.attemptCount())
	}
	for _, want := range []string{marker, "Why this is unfinished", "comment again to start a fresh attempt"} {
		if !strings.Contains(posts[0].body, want) {
			t.Fatalf("failure comment lacks %q:\n%s", want, posts[0].body)
		}
	}
	instance, err := f.db.LatestResearchInstance(t.Context(), f.session.ID)
	mustProgress(t, err)
	intents, err := f.db.ListGraphComments(t.Context(), instance.ID, "", 100)
	mustProgress(t, err)
	if len(intents) != 1 || intents[0].Kind != "failure" || intents[0].Node != "failed" {
		t.Fatalf("failure was not published under the declared failure node: %+v", intents)
	}
	// The listing omits candidates; read the retained intent itself.
	intent, err := f.db.GetGraphComment(t.Context(), intents[0].Key)
	mustProgress(t, err)
	var candidate agentresult.Result
	if d := agentresult.Decode(intent.Candidate, &candidate); !d.Valid || !strings.Contains(candidate.Reason, marker) {
		t.Fatalf("failure candidate reason lacks the cause: %+v", candidate)
	}
	// A replay of the same trigger neither reruns the agent nor posts again.
	if _, err := Execute(t.Context(), f.opts, req); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if forge.attemptCount() != 1 {
		t.Fatalf("replay reposted the failure comment: %d", forge.attemptCount())
	}
}

// Only lf_agent dispatch errors gain the comment (#410): a failure of another
// activity keeps its graph routing, so no failure comment is published for it.
func TestResearchNonAgentDispatchErrorKeepsItsRouting(t *testing.T) {
	w := &researchWorkflow{definition: &teamsource.ResearchDefinition{Profile: &graphexec.Profile{Nodes: []graphexec.Node{{Name: "investigate", Action: ResearchAgentAction}, {Name: "brief", Action: "lf_research_brief"}}}}}
	if !w.agentStep(GraphNodeResult{Node: "investigate", AttemptID: 1}) {
		t.Fatal("an agent step was not recognised")
	}
	for _, step := range []GraphNodeResult{{Node: "brief", AttemptID: 1}, {Node: "investigate"}, {}} {
		if w.agentStep(step) {
			t.Fatalf("step %+v gained the agent failure comment", step)
		}
	}
	cause := errors.New("brief generation failed")
	if err := w.failed(t.Context(), nil, GraphNodeResult{Node: "brief", AttemptID: 1}, cause, true); !errors.Is(err, cause) || err.Error() != cause.Error() {
		t.Fatalf("a non-agent dispatch error changed: %v", err)
	}
}

// The shipped graph is the one that has to admit. If its failure edge stops
// validating, Research silently goes back to reporting nothing on failure.
func TestShippedResearchGraphDeclaresAFailurePublication(t *testing.T) {
	specs, err := ResearchSpecs()
	if err != nil {
		t.Fatal(err)
	}
	definition, err := teamsource.CaptureResearch(t.Context(), specs)
	if err != nil {
		t.Fatal(err)
	}
	if err := graphexec.ValidateResearchControlGraph(definition.Profile); err != nil {
		t.Fatalf("shipped graph does not validate: %v", err)
	}
	target, declared := graphexec.Decide(definition.Profile, "investigate", "failed")
	if !declared {
		t.Fatal("shipped graph declares no failure edge from investigate")
	}
	node, ok := definition.Profile.Node(target)
	if !ok || node.Action != ResearchCommentAction || node.Attrs["lf_kind"] != "failure" {
		t.Fatalf("failure edge targets %q, which is not a failure comment", target)
	}
	next, exists := graphexec.Decide(definition.Profile, target, "published")
	terminal, found := definition.Profile.Node(next)
	if !exists || !found || !terminal.Terminal {
		t.Fatalf("failure publication at %q does not end the graph", target)
	}
}

// Owner authentication uses the same numeric-identity admission as daemon input.
func TestResearchPrelaunchFailureOwnerRetry(t *testing.T) {
	ctx := t.Context()
	calls := 0
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("claude did not start: no such file or directory")
		}
		submitLiveResult(t, req, researchDoneJSON)
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, FinishedAt: time.Now()}, nil
	})
	f, forge, original := assembledResearchFixture(t, runner)
	out, err := Execute(ctx, f.opts, original)
	if err == nil || out.Run.Status != store.RunFailed || calls != 1 || forge.attemptCount() != 1 {
		t.Fatalf("prelaunch failure: %+v err=%v calls=%d posts=%d", out, err, calls, forge.attemptCount())
	}
	failed, err := f.db.LatestResearchInstance(ctx, f.session.ID)
	mustProgress(t, err)
	if failed.Status != store.GraphFailed {
		t.Fatalf("instance not failed: %+v", failed)
	}
	attempts, err := f.db.ListGraphNodeAttempts(ctx, failed.ID)
	mustProgress(t, err)
	intents, err := f.db.ListGraphComments(ctx, failed.ID, "", 100)
	mustProgress(t, err)
	if len(intents) != 1 || intents[0].Kind != "failure" {
		t.Fatalf("failure notice: %+v", intents)
	}
	delivery, err := f.db.GetReportDelivery(ctx, intents[0].Key)
	mustProgress(t, err)
	filter, err := NewFilter(f.db, Identities{OwnerUserID: 7, BotUserID: 9999}, nil)
	mustProgress(t, err)
	retry := original
	retry.Trigger.CommentID++
	retry.Trigger.AuthorID = 7
	retry.Trigger.Body = "retry"
	retry.Command = ParseCommand("retry")
	comment := github.Comment{ID: retry.Trigger.CommentID, Author: github.User{ID: 8, Login: "owner"}, Body: "retry"}
	admitted, err := filter.Admit(ctx, f.session.ID, comment)
	mustProgress(t, err)
	if admitted.Triggers() {
		t.Fatal("non-owner admitted")
	}
	comment.ID++
	retry.Trigger.CommentID = comment.ID
	comment.Author = github.User{ID: 7, Login: "renamed-owner"}
	admitted, err = filter.Admit(ctx, f.session.ID, comment)
	mustProgress(t, err)
	if !admitted.Triggers() {
		t.Fatal("owner retry refused")
	}
	out, err = Execute(ctx, f.opts, retry)
	mustProgress(t, err)
	if !out.Succeeded() || calls != 2 || forge.attemptCount() != 2 {
		t.Fatalf("retry: %+v calls=%d posts=%d", out, calls, forge.attemptCount())
	}
	next, err := f.db.LatestResearchInstance(ctx, f.session.ID)
	mustProgress(t, err)
	prior, _, err := f.db.WorkflowPredecessor(ctx, next.ID)
	mustProgress(t, err)
	if next.ID == failed.ID || prior != failed.ID {
		t.Fatal("retry did not link a fresh instance")
	}
	posts := forge.all()
	if !strings.Contains(posts[1].body, "Investigation finished.") {
		t.Fatalf("retry report: %s", posts[1].body)
	}
	for _, req := range []Request{original, retry} {
		_, err = Execute(ctx, f.opts, req)
		mustProgress(t, err)
	}
	if calls != 2 || forge.attemptCount() != 2 {
		t.Fatal("replayed triggers repeated work or publication")
	}
	after, err := f.db.ListGraphNodeAttempts(ctx, failed.ID)
	mustProgress(t, err)
	afterDelivery, err := f.db.GetReportDelivery(ctx, intents[0].Key)
	mustProgress(t, err)
	if !reflect.DeepEqual(attempts, after) || !reflect.DeepEqual(delivery, afterDelivery) {
		t.Fatal("retry altered predecessor attempts or delivery identity")
	}
}

// The graph keeps one invocation while the pre-process boundary waits for the
// same configured executable; it receives a real process result on recovery.
func TestResearchTransientExecutableRecovery(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "recorded-provider")
	calls, starts := 0, 0
	runner := lifecycleMCPHost(func(ctx context.Context, req agent.Request) (*agent.RunResult, error) {
		calls++
		restored := make(chan error, 1)
		go func() {
			time.Sleep(200 * time.Millisecond)
			restored <- os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0700)
		}()
		var cmd *exec.Cmd
		err := providerprocess.Start(ctx, binary, req.WorktreePath, func() error {
			starts++
			cmd = exec.CommandContext(ctx, binary)
			cmd.Dir = req.WorktreePath
			if err := agent.VerifyRequestDelivery(ctx, req); err != nil {
				return err
			}
			return cmd.Start()
		})
		if restoreErr := <-restored; restoreErr != nil {
			t.Fatal(restoreErr)
		}
		if err != nil {
			return nil, err
		}
		if err = cmd.Wait(); err != nil {
			return nil, err
		}
		submitLiveResult(t, req, researchDoneJSON)
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ExitCode: cmd.ProcessState.ExitCode(), FinishedAt: time.Now()}, nil
	})
	f, forge, req := assembledResearchFixture(t, runner)
	out, err := Execute(t.Context(), f.opts, req)
	if err != nil || out.Run.Status == store.RunFailed || calls != 1 || starts != 2 {
		t.Fatalf("recovery: %+v %v calls=%d starts=%d", out, err, calls, starts)
	}
	for _, post := range forge.all() {
		if strings.Contains(post.body, "could not finish") || strings.Contains(post.body, "no process result") {
			t.Fatalf("intermediate failure: %s", post.body)
		}
	}
}
