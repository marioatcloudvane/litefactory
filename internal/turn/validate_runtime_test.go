package turn

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/bugchecks"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/store"
)

const runtimeOwnerID = int64(4242)

// runtimeRecipe is a check recipe whose every command passes.
const runtimeRecipe = `version: 1
phases:
  regression:
    commands:
      - argv: ["true"]
        timeout_seconds: 30
  focused:
    commands:
      - argv: ["true"]
        timeout_seconds: 30
  full:
    commands:
      - argv: ["true"]
        timeout_seconds: 30
      - argv: ["true"]
        timeout_seconds: 30
`

// runtimeReviewers is the fake agent provider: it answers every reviewer from
// the factory data in its prompt, through the live result route.
type runtimeReviewers struct {
	t     *testing.T
	mu    sync.Mutex
	calls []agent.Request
}

func (r *runtimeReviewers) run(_ context.Context, req agent.Request) (*agent.RunResult, error) {
	r.mu.Lock()
	r.calls = append(r.calls, req)
	r.mu.Unlock()
	start := strings.Index(req.Prompt, "<validate-review>")
	end := strings.Index(req.Prompt, "</validate-review>")
	if req.Mode != "validate" || start < 0 || end < start {
		r.t.Errorf("only reviewers run in this flow: mode=%q", req.Mode)
		return &agent.RunResult{Outcome: agent.OutcomeFailed, Reason: "unexpected", FinishedAt: time.Now()}, nil
	}
	var in validateReviewInput
	mustProgress(r.t, json.Unmarshal([]byte(req.Prompt[start+len("<validate-review>"):end]), &in))
	result := goodReview(ValidateReviewRequest{Role: in.Role, Stories: in.Stories, Evidence: in.Evidence, SnapshotID: in.SnapshotID})
	raw, err := json.Marshal(struct {
		Result agentresult.Result `json:"result"`
	}{*result})
	mustProgress(r.t, err)
	submitLiveResult(r.t, req, string(raw))
	return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "Reviewed.", FinishedAt: time.Now()}, nil
}

// TestValidateProductionRuntimeRunsTheGraphToTheOwnerWait drives /validate on a
// finished Implement parent through the production host bindings. Only the
// agent provider process, the check sandbox launcher and GitHub are fakes; git,
// the store, the result route and the suite are real.
func TestValidateProductionRuntimeRunsTheGraphToTheOwnerWait(t *testing.T) {
	reviewers := &runtimeReviewers{t: t}
	f, forge, _ := assembledImplementFixture(t, lifecycleMCPHost(reviewers.run))
	parent := seedValidateParent(t, f,
		store.ManagedStory{Key: "story-001", IssueNumber: 201, IssueID: 7001},
		store.ManagedStory{Key: "story-002", IssueNumber: 202, IssueID: 7002})
	v := &validateAdmissionFixture{f: f, parent: parent}
	v.allMerged(t)

	// The integration branch on the remote holds the recipe.
	session, err := f.db.GetSession(t.Context(), f.session.ID)
	mustProgress(t, err)
	f.repo.Git("checkout", "--quiet", "-b", session.Branch)
	f.repo.WriteFile(".litefactory/bug-checks.yaml", runtimeRecipe)
	head := f.repo.Commit("integrate the stories")
	f.repo.Git("push", "--quiet", "origin", session.Branch)
	f.repo.Git("checkout", "--quiet", gittest.DefaultBranch)

	// The check sandbox launcher is a fake that runs the command it is given.
	bin := t.TempDir()
	launcher := filepath.Join(bin, "sandbox")
	mustProgress(t, os.WriteFile(launcher, []byte("#!/bin/sh\nwhile [ \"$#\" -gt 0 ] && [ \"$1\" != \"--\" ]; do shift; done\nshift\nexec \"$@\"\n"), 0o700))
	f.opts.MCP.BugChecksIsolation = bugchecks.Isolation{Binary: launcher, Home: t.TempDir(), PATH: "/usr/bin:/bin", Revision: "test"}

	slots := &flowSlots{free: 3}
	runtime, err := NewValidateRuntime(t.Context(), f.opts, ValidateRuntimeConfig{Forge: forge.client, OwnerID: runtimeOwnerID, Slots: slots})
	mustProgress(t, err)
	f.opts.Validate = runtime

	req := v.request(7001, runtimeOwnerID, "/validate check the finished work")
	out, err := Execute(t.Context(), f.opts, req)
	if err != nil {
		t.Fatalf("%+v %v\nlogs:\n%s", out, err, f.logs)
	}
	if !out.ResearchGraph {
		t.Fatalf("the start took the legacy path: %+v", out)
	}

	entries := v.entries(t)
	if len(entries) != 1 || entries[0].State != store.ValidateEntryAdmitted || entries[0].Commit != head {
		t.Fatalf("one entry admitted on the integration head %s: %+v", head, entries)
	}
	inst, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "validate")
	mustProgress(t, err)
	run, err := f.db.LatestSuiteRun(t.Context(), inst.ID, head)
	if err != nil || run.State != store.SuitePassed {
		t.Fatalf("the suite ran on the integration head and passed: %+v %v", run, err)
	}
	reviews, err := f.db.ListValidateReviews(t.Context(), inst.ID, head)
	mustProgress(t, err)
	roles := map[string]string{}
	for _, r := range reviews {
		roles[r.Role] = r.State
	}
	for _, role := range ValidateReviewRoles {
		if roles[role] != store.ReviewAccepted {
			t.Errorf("the %s reviewer's answer was recorded: %+v", role, reviews)
		}
	}
	if len(reviewers.calls) != 3 || slots.took != 3 || slots.free != 3 {
		t.Fatalf("three reviewers ran, each in its own run slot: calls=%d took=%d free=%d", len(reviewers.calls), slots.took, slots.free)
	}
	for _, call := range reviewers.calls {
		if call.Delivery == nil || !strings.HasPrefix(call.Delivery.MainRole, "validate-") {
			t.Errorf("the reviewer was given its role: %+v", call.Delivery)
		}
		if call.MCPLaunch == nil || call.WorktreePath == "" || call.WorktreePath == session.WorktreePath {
			t.Errorf("the reviewer runs on a scratch checkout with the result route: %q", call.WorktreePath)
		}
		if strings.Contains(strings.Join(toolSurface(call), ","), "Edit") || strings.Contains(strings.Join(toolSurface(call), ","), "Bash") {
			t.Errorf("a reviewer only reads: %v", toolSurface(call))
		}
	}
	wait, err := f.db.GetGraphOwnerWait(t.Context(), inst.ID)
	if err != nil || wait.State != "waiting" || inst.Status != store.GraphRunning {
		t.Fatalf("the run rests at the owner wait: %+v %+v %v", inst, wait, err)
	}
	outcome, err := f.db.GetValidateOutcome(t.Context(), inst.ID, head)
	if err != nil || outcome.Outcome == "" {
		t.Fatalf("an outcome was recorded: %+v %v", outcome, err)
	}

	// A lost reviewer reply is found again by its attempt alone.
	reviewer := &validateReviewers{host: &validateHost{opts: f.opts}}
	for _, r := range reviews {
		got, found, err := reviewer.Recover(t.Context(), r.Attempt)
		if err != nil || !found || got.Outcome != "completed" {
			t.Fatalf("the %s reviewer's retained result is recovered: %v %v %+v", r.Role, found, err, got)
		}
	}
	if _, found, err := reviewer.Recover(t.Context(), "validate-review-0-none-000000000000"); err != nil || found {
		t.Fatalf("an unknown attempt has no result: %v %v", found, err)
	}
}

func toolSurface(req agent.Request) []string {
	raw, _ := json.Marshal(req.Permissions.Payload)
	return []string{string(raw)}
}

// seedValidateParent is seedImplementParent with story bodies that declare
// their acceptance criteria.
func seedValidateParent(t *testing.T, f *fixture, stories ...store.ManagedStory) string {
	t.Helper()
	ctx := t.Context()
	parent := f.session.TaskRef
	mustProgress(t, f.db.PutManagedFeature(ctx, store.ManagedFeature{ParentTask: parent, Generation: 1, State: "planning", WorkflowVersion: store.WorkflowSeparated}))
	var revisions []store.ApprovedStoryRevision
	for _, story := range stories {
		story.ParentTask, story.Generation, story.State = parent, 1, "planning"
		mustProgress(t, f.db.PutManagedStory(ctx, story))
		body := "Body of " + story.Key + "\n\n## Acceptance criteria\n\n- AC-001: the endpoint answers\n- AC-002: the answer names the build\n"
		revisions = append(revisions, store.ApprovedStoryRevision{ManagedStory: story, ParentID: 9, Title: "Title of " + story.Key, Body: body, Hash: strings.Repeat("cd", 32), FetchedAt: implementClock})
	}
	mustProgress(t, f.db.SetManagedFeatureReady(ctx, parent, 1, true))
	mustProgress(t, f.db.ApproveManagedStories(ctx, parent, 1, revisions))
	for _, story := range stories {
		mustProgress(t, f.db.DeliverStoryScopeNotice(ctx, parent, 1, story.Key, "freeze"))
	}
	mustProgress(t, f.db.ActivateManagedScope(ctx, parent, 1))
	_, err := f.db.FreezeSessionSlug(ctx, f.session.ID, "feature", "lf/123-feature")
	mustProgress(t, err)
	return parent
}
