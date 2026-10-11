package turn

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
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
	for _, s := range in.Stories {
		if !strings.Contains(s.Text, "AC-") {
			r.t.Errorf("reviewer %s got story %s without its approved text: %q", in.Role, s.Key, s.Text)
		}
	}
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
	if err == nil && strings.Contains(run.Reason, "operation not permitted") {
		// A sandboxed test run (an agent's Bug or Implement check of this
		// repository) may not observe the processes it starts; the daemon can.
		t.Skip("process observation is not permitted here")
	}
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

// TestValidateStoriesCarryTheOwnerGuidanceOfTheImplementLedger checks that a
// story the owner resumed with a decision reaches every reviewer with that
// decision next to its frozen criteria, while a story without guidance, or
// without a ledger row, is given exactly as before.
func TestValidateStoriesCarryTheOwnerGuidanceOfTheImplementLedger(t *testing.T) {
	f, _, _ := assembledImplementFixture(t, lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
		t.Error("no agent runs here")
		return nil, nil
	}))
	parent := seedValidateParent(t, f,
		store.ManagedStory{Key: "story-001", IssueNumber: 201, IssueID: 7001},
		store.ManagedStory{Key: "story-002", IssueNumber: 202, IssueID: 7002},
		store.ManagedStory{Key: "story-003", IssueNumber: 203, IssueID: 7003})
	ctx := t.Context()
	mustProgress(t, f.db.EnsureImplementStories(ctx, parent, 1, []store.ImplementStorySeed{{Key: "story-001", Position: 0, Branch: "lf/201-a"}, {Key: "story-002", Position: 1, Branch: "lf/202-b"}}, implementClock))
	const reply = "Approval for option 2! Continue with the implementation!"
	const question = "exactly one visible reply cannot be guaranteed"
	moves := map[string][]store.ImplementTransition{
		"story-001": {
			{From: store.ImplementWaiting, To: store.ImplementRunning},
			{From: store.ImplementRunning, To: store.ImplementParked, Reason: question},
			{From: store.ImplementParked, To: store.ImplementWaiting, Guidance: reply},
			{From: store.ImplementWaiting, To: store.ImplementRunning},
			{From: store.ImplementRunning, To: store.ImplementDone},
			{From: store.ImplementDone, To: store.ImplementMerged, MergeCommit: strings.Repeat("c", 40)},
		},
		"story-002": {
			{From: store.ImplementWaiting, To: store.ImplementRunning},
			{From: store.ImplementRunning, To: store.ImplementDone},
			{From: store.ImplementDone, To: store.ImplementMerged, MergeCommit: strings.Repeat("d", 40)},
		},
	}
	for key, steps := range moves {
		row, err := f.db.GetImplementStory(ctx, parent, 1, key)
		mustProgress(t, err)
		for _, tr := range steps {
			tr.Parent, tr.Generation, tr.Key, tr.ExpectedRevision, tr.At = parent, 1, key, row.Revision, implementClock
			row, err = f.db.TransitionImplementStory(ctx, tr)
			mustProgress(t, err)
		}
	}
	session, err := f.db.GetSession(ctx, f.session.ID)
	mustProgress(t, err)
	feature, err := f.db.GetManagedFeature(ctx, parent)
	mustProgress(t, err)
	revisions, err := f.db.ListApprovedStoryRevisions(ctx, parent, feature.Generation)
	mustProgress(t, err)
	h := &validateHost{opts: f.opts, session: session, feature: feature, revisions: revisions}
	stories, err := h.stories(ctx)
	mustProgress(t, err)
	if len(stories) != 3 {
		t.Fatalf("three stories are under review: %+v", stories)
	}
	byKey := map[string]ValidateStory{}
	for _, s := range stories {
		byKey[s.Key] = s
	}
	if got := byKey["story-001"]; got.OwnerGuidance != reply || got.OwnerQuestion != question {
		t.Fatalf("the resumed story carries the worker's question and the owner's reply: %+v", got)
	}
	for _, key := range []string{"story-002", "story-003"} {
		if got := byKey[key]; got.OwnerGuidance != "" || got.OwnerQuestion != "" {
			t.Fatalf("%s has no owner guidance: %+v", key, got)
		}
	}
	for _, r := range revisions {
		s := byKey[r.Key]
		if s.Text != r.Body || s.Revision != r.Hash || len(s.Criteria) != 2 || s.Criteria[0] != "AC-001" {
			t.Fatalf("the frozen story is given unchanged: %+v", s)
		}
	}

	prompt, err := validateReviewPrompt(ValidateReviewRequest{Role: "acceptance", Commit: strings.Repeat("a", 40), SnapshotID: "snap", Stories: stories})
	mustProgress(t, err)
	if !strings.Contains(prompt, "A story's owner_guidance is the owner's decision recorded during Implement") ||
		!strings.Contains(prompt, "its owner_question, when present, is the worker's question that owner_guidance answers") ||
		!strings.Contains(prompt, "as amended by that decision") || !strings.Contains(prompt, "cite the decision") ||
		!strings.Contains(prompt, "never fail a criterion or classify a finding as blocking only because it differs from the frozen text in the way the owner approved") {
		t.Fatalf("the prompt tells reviewers how to use owner guidance:\n%s", prompt)
	}
	start, end := strings.Index(prompt, "<validate-review>"), strings.Index(prompt, "</validate-review>")
	var raw struct {
		Stories []map[string]any `json:"stories"`
	}
	mustProgress(t, json.Unmarshal([]byte(prompt[start+len("<validate-review>"):end]), &raw))
	for _, s := range raw.Stories {
		guidance, has := s["owner_guidance"]
		asked, hasQuestion := s["owner_question"]
		switch s["Key"] {
		case "story-001":
			if guidance != reply || asked != question {
				t.Fatalf("story-001's reviewer input holds the question and the reply: %+v", s)
			}
		default:
			if has || hasQuestion {
				t.Fatalf("a story without guidance serializes as before: %+v", s)
			}
		}
	}

	// The owner guidance changes nothing a reviewer's result is judged against.
	plain := append([]ValidateStory(nil), stories...)
	for i := range plain {
		plain[i].OwnerGuidance, plain[i].OwnerQuestion = "", ""
	}
	evidence := []ValidateEvidence{{ID: "suite-1-0", State: store.SuitePassed}}
	with := validateReviewScope("acceptance", stories, "snap", strings.Repeat("cd", 32), evidence)
	without := validateReviewScope("acceptance", plain, "snap", strings.Repeat("cd", 32), evidence)
	if !reflect.DeepEqual(with.Stories, without.Stories) || with.ReviewRole != without.ReviewRole || with.Mode != without.Mode {
		t.Fatalf("the review scope ignores owner guidance:\n%+v\n%+v", with, without)
	}
}
