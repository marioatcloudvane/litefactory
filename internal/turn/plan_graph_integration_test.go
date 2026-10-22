package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	ar "github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// These tests run a fake Plan agent through the full graph Plan workflow over the
// real shared result service: real SQLite store, the production private broker
// and tool dispatcher the agent reaches through its launch descriptor, the real
// story effect against a fake forge over HTTP, and the real comment reporter. The
// agent is the only fake, so what it submits, in what order and with which
// identity, is the test's to choose.

// planAgentStep is what a fake Plan agent does for one node attempt. It reaches the
// shared result route only through the client its launch descriptor names.
type planAgentStep struct {
	f      *fixture
	t      *testing.T
	req    agent.Request
	client submission.Client
	story  string
	stage  string
}

type planAgentBehavior func(s planAgentStep) (*agent.RunResult, error)

// graphPlanAgentFixture is graphPlanFixtureWithForge with the agent's behavior
// supplied by the test rather than fixed.
func graphPlanAgentFixture(t *testing.T, behave planAgentBehavior) (*fixture, *graphPlanHTTP, Request, *fakeForge, *int) {
	t.Helper()
	calls := 0
	var f *fixture
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		calls++
		if req.Graph == nil || req.Delivery == nil || req.Mode != "plan" {
			t.Fatal("Plan native graph invocation absent")
		}
		matched := regexp.MustCompile(`Factory-selected story: ([a-z0-9-]+); stage: ([a-z]+);`).FindStringSubmatch(req.Prompt)
		if len(matched) != 3 {
			t.Fatal("factory story binding missing")
		}
		return behave(planAgentStep{f: f, t: t, req: req, client: submission.Client{DescriptorPath: req.MCPLaunch.DescriptorPath()}, story: matched[1], stage: matched[2]})
	})
	var forge *fakeForge
	var base Request
	f, forge, base = assembledResearchFixture(t, runner)
	mustProgress(t, os.MkdirAll(f.opts.MCP.Paths.StoryProgressLocksDir(), 0700))
	f.opts.Tiers.ModelByTier = map[config.Kind]map[config.Tier]string{"claude": {"balanced": "test-model", "flagship": "test-model"}}
	_, err := f.db.UpsertIdentity(t.Context(), store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, UserID: 7, Handle: "owner", Source: store.SourceGH})
	mustProgress(t, err)
	remote := &graphPlanHTTP{children: map[int]map[string]any{}, attached: map[int]bool{}}
	server := httptest.NewServer(remote)
	t.Cleanup(server.Close)
	client, err := github.New(github.Options{APIBase: server.URL + "/", Token: secrets.NewToken("fixture"), HTTPClient: server.Client()})
	mustProgress(t, err)
	f.opts.SubIssueCreator = client
	_, err = f.db.AdmitPlanWorkflowPath(t.Context(), f.session.ID, "graph")
	mustProgress(t, err)
	// The scope already has its first story, so it starts at the story stage.
	_, err = f.db.SavePlanWorkflowState(t.Context(), f.ref().String(), 0, planbrief.PlanState{SharedRevision: "initial", Stories: []planbrief.PlanStoryState{{Key: "story-001", Stage: "story", Approvals: map[string]string{}, Draft: planbrief.PlanDraft{Key: "story-001"}}}})
	mustProgress(t, err)
	base.Mode = agent.ModePlan
	base.Command = ParseCommand("/plan shape stories")
	base.Trigger.Body = "/plan shape stories"
	base.Trigger.AuthorID = 7
	return f, remote, base, forge, &calls
}

// integrationCreateRequest is the story request a fake agent authors at the story
// stage, the one graphPlanFixture also authors.
func integrationCreateRequest(key string) *ar.PlanStoryRequest {
	d := planbrief.PlanDraft{Key: key, Name: "Bounded Plan", UserStory: planbrief.UserStoryContent{Actor: "owner", Want: "shape intent", Benefit: "clear scope", Notes: []string{}}, Problem: "Unclear plan", InScope: []string{"Story discussion"}, NotInScope: []string{"Implementation"}, Dependencies: planbrief.DependencyContent{StoryKeys: []string{}, IssueRefs: []string{}}}
	return &ar.PlanStoryRequest{Operation: "create", Draft: d, ChangedSections: []string{}}
}

func integrationDone(request *ar.PlanStoryRequest, comment string) ar.Result {
	return ar.Result{Outcome: "completed", Comment: comment, Payload: ar.Payload{Plan: &ar.Plan{StoryRequest: request}}}
}

// submitBytes is the payload of a shared submission for one result.
func submitBytes(t *testing.T, result ar.Result) []byte {
	t.Helper()
	raw, err := json.Marshal(result)
	mustProgress(t, err)
	return append(append([]byte(`{"result":`), raw...), '}')
}

// withFactoryMember returns the same submission with an agent-supplied member the
// factory owns, which the shared route must refuse.
func withFactoryMember(t *testing.T, data []byte, member, value string) []byte {
	t.Helper()
	marker := `"storyrequest":{`
	text := string(data)
	if !strings.Contains(text, marker) {
		t.Fatalf("no story request in %s", text)
	}
	return []byte(strings.Replace(text, marker, marker+`"`+member+`":"`+value+`",`, 1))
}

// agentDone returns the terminal envelope for the result the agent submitted.
func agentDone(t *testing.T, result ar.Result) *agent.RunResult {
	t.Helper()
	envelope, err := json.Marshal(ar.ResearchNode{Version: 1, Response: "done", Result: result})
	mustProgress(t, err)
	return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: string(envelope), FinishedAt: time.Now()}
}

func (s planAgentStep) call(tool string, data []byte) (submission.Response, AgentResultSubmission) {
	s.t.Helper()
	r, err := s.client.Call(s.t.Context(), tool, data)
	if err != nil {
		s.t.Fatalf("%s: %v", tool, err)
	}
	var got AgentResultSubmission
	if tool != "communication_receipt" || r.Error == "" {
		if r.Error != "" {
			s.t.Fatalf("%s: broker error %q", tool, r.Error)
		}
		mustProgress(s.t, json.Unmarshal(r.Result, &got))
	}
	return r, got
}

// preflight is the shared preflight tool: diagnostics only, nothing retained.
func (s planAgentStep) preflight(data []byte) ar.UpdateDiagnostics {
	s.t.Helper()
	r, err := s.client.Call(s.t.Context(), "communication_preflight", data)
	if err != nil || r.Error != "" {
		s.t.Fatalf("communication_preflight: %v %q", err, r.Error)
	}
	var d ar.UpdateDiagnostics
	mustProgress(s.t, json.Unmarshal(r.Result, &d))
	return d
}

func diagnosticPaths(d ar.UpdateDiagnostics) string {
	var out []string
	for _, item := range d.Diagnostics.Diagnostics {
		out = append(out, item.Path+": "+item.Message)
	}
	return strings.Join(out, " | ")
}

// retainedRevisions lists the accepted result revisions the story stage's agent
// attempt retained, resolved through the reference its node output keeps.
func retainedRevisions(t *testing.T, f *fixture, node string) ([]int64, planResultRef) {
	t.Helper()
	instance, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "plan")
	mustProgress(t, err)
	attempts, err := f.db.ListGraphNodeAttempts(t.Context(), instance.ID)
	mustProgress(t, err)
	for _, attempt := range attempts {
		if attempt.Node != node {
			continue
		}
		outputs, err := f.db.ListGraphOutputs(t.Context(), attempt.ID)
		mustProgress(t, err)
		for _, output := range outputs {
			if ref, ok := decodePlanResultRef(output.Payload); ok {
				revisions, err := f.db.ResultRevisionNumbers(t.Context(), ref.Binding)
				mustProgress(t, err)
				return revisions, ref
			}
		}
	}
	t.Fatalf("no retained result reference for %s", node)
	return nil, planResultRef{}
}

// IT-008-1 (AC-001, AC-003): the agent is refused with named diagnostics, corrects
// the payload under a new request identity in the same attempt, and the workflow
// completes on the corrected retained result alone: one retained revision, one
// story, and the refusal never reached an effect.
func TestPlanGraphIntegration_NamedRefusalThenSameAttemptCorrection(t *testing.T) {
	for _, tt := range []struct {
		name     string
		break_   func(t *testing.T, data []byte) []byte
		wantPath string
	}{
		{"agent-supplied factory stage", func(t *testing.T, d []byte) []byte { return withFactoryMember(t, d, "stage", "technical") }, "storyrequest.stage"},
		{"agent-supplied candidate", func(t *testing.T, d []byte) []byte { return withFactoryMember(t, d, "candidate", "forged") }, "storyrequest.candidate"},
		{"agent-supplied request identity", func(t *testing.T, d []byte) []byte { return withFactoryMember(t, d, "request_id", "forged") }, "storyrequest.request_id"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var refusedID, acceptedID, preflightSeen string
			var refusedReceipt, acceptedReceipt *store.ResultReceipt
			f, remote, base, _, calls := graphPlanAgentFixture(t, func(s planAgentStep) (*agent.RunResult, error) {
				good := integrationDone(integrationCreateRequest(s.story), "Please review this candidate.")
				data := submitBytes(t, good)
				bad := tt.break_(t, data)

				// Preflight names what is wrong and retains nothing.
				pre := s.preflight(bad)
				if pre.Valid || len(pre.Diagnostics.Diagnostics) == 0 {
					t.Fatalf("preflight accepted the refused payload: %+v", pre)
				}
				preflightSeen = diagnosticPaths(pre)

				// Submit refuses it, with a recorded rejected receipt and no result.
				first, refused := s.call("communication_submit", bad)
				if refused.Validation.Valid || refused.Receipt == nil || refused.Receipt.InputState != "rejected" || refused.Snapshot != nil {
					t.Fatalf("submit did not refuse: %+v", refused)
				}
				if diagnosticPaths(refused.Validation) != preflightSeen {
					t.Fatalf("preflight and submit named different faults:\n%s\n%s", preflightSeen, diagnosticPaths(refused.Validation))
				}
				refusedID, refusedReceipt = first.RequestID, refused.Receipt

				// The corrected content is a different request: a new identity.
				second, accepted := s.call("communication_submit", data)
				if !accepted.Validation.Valid || accepted.Receipt == nil || accepted.Receipt.InputState != "accepted" || accepted.Receipt.AppliedRevision != 1 {
					t.Fatalf("corrected payload not accepted: %+v", accepted)
				}
				acceptedID, acceptedReceipt = second.RequestID, accepted.Receipt
				return agentDone(t, good), nil
			})
			out, err := Execute(t.Context(), f.opts, base)
			if err != nil || out.Err != nil || !out.Succeeded() {
				t.Fatalf("the corrected attempt did not complete: err=%v out=%+v logs=%s", err, out.Err, f.logs.String())
			}
			if *calls != 1 {
				t.Fatalf("correction needed %d agent invocations, want the same attempt", *calls)
			}
			if refusedID == "" || acceptedID == "" || refusedID == acceptedID {
				t.Fatalf("correction reused the refused identity: %q %q", refusedID, acceptedID)
			}
			t.Logf("refusal: %s", preflightSeen)
			if !strings.Contains(preflightSeen, tt.wantPath) {
				t.Fatalf("the refusal does not name the offending member: %s", preflightSeen)
			}
			if refusedReceipt.AppliedRevision != 0 || acceptedReceipt.AppliedRevision != 1 {
				t.Fatalf("receipts: %+v %+v", refusedReceipt, acceptedReceipt)
			}
			revisions, ref := retainedRevisions(t, f, "story_agent")
			if len(revisions) != 1 || revisions[0] != 1 || ref.Revision != 1 {
				t.Fatalf("the refusal or the correction left %v retained (reference revision %d)", revisions, ref.Revision)
			}
			if remote.creates != 1 {
				t.Fatalf("expected exactly one story effect, got %d", remote.creates)
			}
			binding, err := f.db.GetPlanStoryDraft(t.Context(), f.ref().String(), "story-001")
			mustProgress(t, err)
			var draft planbrief.PlanDraft
			mustProgress(t, json.Unmarshal([]byte(binding.DraftJSON), &draft))
			if draft.Name != "Bounded Plan" || binding.Candidate == "forged" || binding.Candidate == "" {
				t.Fatalf("the story does not carry the factory-bound corrected candidate: %+v", binding)
			}
		})
	}
}

// IT-008-2 (AC-001): a refusal is not completion. An agent that is refused and
// then answers with the refused content as its terminal envelope, without ever
// submitting a correction, fails its attempt: the terminal object cannot stand in
// for an accepted result, and no story is created.
func TestPlanGraphIntegration_RefusedSubmissionCannotBeSmuggledThroughTheTerminalEnvelope(t *testing.T) {
	f, remote, base, forge, _ := graphPlanAgentFixture(t, func(s planAgentStep) (*agent.RunResult, error) {
		good := integrationDone(integrationCreateRequest(s.story), "Please review this candidate.")
		_, refused := s.call("communication_submit", withFactoryMember(t, submitBytes(t, good), "stage", "technical"))
		if refused.Validation.Valid || refused.Snapshot != nil {
			t.Fatalf("submit did not refuse: %+v", refused)
		}
		return agentDone(t, good), nil
	})
	out, err := Execute(t.Context(), f.opts, base)
	if err == nil && out.Err == nil {
		t.Fatal("a refused submission completed nominally through the terminal envelope")
	}
	if joined := fmt.Sprint(err, out.Err, out.Reason); !strings.Contains(joined, "no accepted shared result") {
		t.Fatalf("cause not named: %q", joined)
	}
	if remote.creates != 0 {
		t.Fatal("a story was created from a refused submission")
	}
	if _, err := f.db.GetPlanStoryDraft(t.Context(), f.ref().String(), "story-001"); err == nil {
		t.Fatal("story draft retained from a refused submission")
	}
	requireFailureComment(t, forge, "no accepted shared result")
}

// IT-008-3 (AC-036, AC-037): submit applies the same validation against current
// state, so bytes that were valid at preflight are refused once the bound state
// changed, with nothing retained and no effect. The story exists by the time the
// agent submits a create for it.
func TestPlanGraphIntegration_StaleAfterPreflightRefusedWithoutRetentionOrEffects(t *testing.T) {
	var preflightValid bool
	var submitted AgentResultSubmission
	f, remote, base, _, _ := graphPlanAgentFixture(t, func(s planAgentStep) (*agent.RunResult, error) {
		good := integrationDone(integrationCreateRequest(s.story), "Please review this candidate.")
		data := submitBytes(t, good)
		preflightValid = s.preflight(data).Valid
		// Between preflight and submit the story acquires a saved revision, as if
		// another attempt or an owner edit had bound one.
		binding, err := s.f.db.ReservePlanStory(s.t.Context(), s.f.ref().String(), s.story)
		mustProgress(s.t, err)
		mustProgress(s.t, s.f.db.BindPlanStoryIssue(s.t.Context(), s.f.ref().String(), s.story, 9000, 900))
		binding.IssueID, binding.IssueNumber, binding.Revision = 9000, 900, "revision-after-preflight"
		binding.Candidate, binding.DraftJSON = "other-candidate", `{"key":"`+s.story+`"}`
		mustProgress(s.t, s.f.db.PutPlanStoryCandidate(s.t.Context(), store.PlanStoryCandidate{Parent: s.f.ref().String(), Key: s.story, RequestID: "other", Candidate: "other-candidate", RequestJSON: `{"operation":"create"}`}))
		mustProgress(s.t, s.f.db.ApplyPlanStoryDraft(s.t.Context(), binding, "", store.PlanStoryEffect{Parent: s.f.ref().String(), RequestID: "other", Phase: "attach", Outcome: "applied", IssueID: 9000, IssueNumber: 900, Revision: binding.Revision}))
		_, submitted = s.call("communication_submit", data)
		return agentDone(t, good), nil
	})
	out, err := Execute(t.Context(), f.opts, base)
	if !preflightValid {
		t.Fatal("the payload was not valid at preflight")
	}
	if submitted.Validation.Valid || submitted.Snapshot != nil || submitted.Receipt == nil || submitted.Receipt.InputState != "rejected" {
		t.Fatalf("stale bytes were accepted at submit: %+v", submitted)
	}
	if submitted.Validation.Baseline == nil {
		t.Fatalf("the refusal does not name the current baseline to update: %s", diagnosticPaths(submitted.Validation))
	}
	if err == nil && out.Err == nil {
		t.Fatal("stale content completed the attempt")
	}
	if remote.creates != 0 || remote.patches != 0 {
		t.Fatalf("stale content reached the forge: creates=%d patches=%d", remote.creates, remote.patches)
	}
	after, err := f.db.GetPlanStoryDraft(t.Context(), f.ref().String(), "story-001")
	mustProgress(t, err)
	if after.Revision != "revision-after-preflight" || after.Candidate != "other-candidate" {
		t.Fatalf("stale content changed the story: %+v", after)
	}
}

// IT-008-4 (AC-036): a submission whose attempt authority is gone cannot be
// retained or applied. The attempt's authority is revoked between preflight and
// submit, and the agent's terminal envelope changes nothing.
func TestPlanGraphIntegration_TerminalOnlyAndUnretainedSubmissionsCreateNothing(t *testing.T) {
	for _, tt := range []struct {
		name string
		act  func(s planAgentStep, good ar.Result) *agent.RunResult
		want string
	}{
		{"terminal only", func(s planAgentStep, good ar.Result) *agent.RunResult { return agentDone(s.t, good) }, "no accepted shared result"},
		{"preflight only", func(s planAgentStep, good ar.Result) *agent.RunResult {
			if pre := s.preflight(submitBytes(s.t, good)); !pre.Valid {
				s.t.Fatalf("preflight refused a valid payload: %+v", pre)
			}
			return agentDone(s.t, good)
		}, "no accepted shared result"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f, remote, base, forge, _ := graphPlanAgentFixture(t, func(s planAgentStep) (*agent.RunResult, error) {
				return tt.act(s, integrationDone(integrationCreateRequest(s.story), "Please review this candidate.")), nil
			})
			out, err := Execute(t.Context(), f.opts, base)
			if err == nil && out.Err == nil {
				t.Fatal("output that was never retained completed nominally")
			}
			if joined := fmt.Sprint(err, out.Err, out.Reason); !strings.Contains(joined, tt.want) {
				t.Fatalf("cause not named: %q", joined)
			}
			if remote.creates != 0 {
				t.Fatal("a story was created without a retained result")
			}
			if _, err := f.db.GetPlanStoryDraft(t.Context(), f.ref().String(), "story-001"); err == nil {
				t.Fatal("story draft retained without a retained result")
			}
			for _, post := range forge.all() {
				if strings.Contains(post.body, "To promote this exact candidate") {
					t.Fatal("a review request was published without a retained result")
				}
			}
			requireFailureComment(t, forge, tt.want)
		})
	}
}

// IT-005-1 (AC-003): every later node reads the retained accepted shared result
// through the reference the agent node kept. The review comment carries the
// retained result's comment, the story effect applies the retained draft, control
// routes on the retained response, and no node output keeps a private copy of the
// application payload.
func TestPlanGraphIntegration_LaterNodesUseTheRetainedAcceptedResult(t *testing.T) {
	const marker = "RETAINED-COMMENT-MARKER-91"
	f, remote, base, forge, _ := graphPlanAgentFixture(t, func(s planAgentStep) (*agent.RunResult, error) {
		good := integrationDone(integrationCreateRequest(s.story), marker)
		_, accepted := s.call("communication_submit", submitBytes(t, good))
		if !accepted.Validation.Valid || accepted.Receipt == nil || accepted.Receipt.AppliedRevision != 1 {
			t.Fatalf("submission not accepted: %+v", accepted)
		}
		return agentDone(t, good), nil
	})
	out, err := Execute(t.Context(), f.opts, base)
	if err != nil || out.Err != nil || !out.Succeeded() {
		t.Fatalf("attempt did not complete: %v %v %s", err, out.Err, f.logs.String())
	}
	// effect: the retained draft, and only that, was applied.
	if remote.creates != 1 {
		t.Fatalf("story effects: %d", remote.creates)
	}
	remote.mu.Lock()
	title, _ := remote.children[400]["title"].(string)
	remote.mu.Unlock()
	if !strings.Contains(title, "Bounded Plan") {
		t.Fatalf("the story was not created from the retained draft: %q", title)
	}
	// comment: the review request quotes the retained comment and the exact
	// candidate the factory bound.
	binding, err := f.db.GetPlanStoryDraft(t.Context(), f.ref().String(), "story-001")
	mustProgress(t, err)
	var review string
	for _, post := range forge.all() {
		if strings.Contains(post.body, marker) {
			review = post.body
		}
	}
	if review == "" || !strings.Contains(review, "/approve-plan story-001 story "+binding.Candidate) {
		t.Fatalf("review comment does not carry the retained comment and bound candidate: %q", review)
	}
	// control and comment outputs: only the agent node holds a reference; nothing
	// downstream keeps the application payload.
	instance, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "plan")
	mustProgress(t, err)
	attempts, err := f.db.ListGraphNodeAttempts(t.Context(), instance.ID)
	mustProgress(t, err)
	references := 0
	for _, attempt := range attempts {
		outputs, err := f.db.ListGraphOutputs(t.Context(), attempt.ID)
		mustProgress(t, err)
		for _, output := range outputs {
			if _, ok := decodePlanResultRef(output.Payload); ok {
				references++
				if attempt.Node != "story_agent" {
					t.Fatalf("node %s kept a result reference of its own", attempt.Node)
				}
				continue
			}
			if strings.Contains(string(output.Payload), marker) || strings.Contains(string(output.Payload), "Bounded Plan") {
				t.Fatalf("node %s kept a private copy of the application payload: %s", attempt.Node, output.Payload)
			}
		}
	}
	if references != 1 {
		t.Fatalf("expected exactly one result reference, got %d", references)
	}
	transitions, err := f.db.ListGraphTransitions(t.Context(), instance.ID)
	mustProgress(t, err)
	var route []string
	for _, tr := range transitions {
		route = append(route, tr.FromNode+"/"+tr.OnOutcome+"->"+tr.ToNode)
	}
	t.Logf("route: %v", route)
	if joined := strings.Join(route, " "); !strings.Contains(joined, "story_agent/success->story_question") || !strings.Contains(joined, "story_question/false->story_answer") || !strings.Contains(joined, "story_draft/applied") {
		t.Fatalf("control did not route the retained done response to the story effect: %v", route)
	}
}

// #997 AC-4: a parent story-progress lock still busy past the wait parks the
// story at story_draft with no failure comment and no attempt; the next
// dispatch after release writes the draft.
func TestPlanGraphIntegration_BusyParentParksStoryDraftUntilReleased(t *testing.T) {
	previous := planStoryLockWait
	planStoryLockWait = 50 * time.Millisecond
	defer func() { planStoryLockWait = previous }()
	f, remote, base, forge, calls := graphPlanAgentFixture(t, func(s planAgentStep) (*agent.RunResult, error) {
		good := integrationDone(integrationCreateRequest(s.story), "busy parent draft")
		_, accepted := s.call("communication_submit", submitBytes(t, good))
		if !accepted.Validation.Valid || accepted.Receipt == nil {
			t.Fatalf("submission not accepted: %+v", accepted)
		}
		return agentDone(t, good), nil
	})
	f.opts.GraphRetryDelay = time.Second
	release, err := f.db.LockStoryProgress(t.Context(), f.ref().String(), f.opts.MCP.Paths.StoryProgressLocksDir())
	mustProgress(t, err)
	released := false
	defer func() {
		if !released {
			mustProgress(t, release())
		}
	}()
	out, err := Execute(t.Context(), f.opts, base)
	if err != nil || out.Err != nil {
		t.Fatalf("busy parent failed the turn: %v %v %s", err, out.Err, f.logs.String())
	}
	for _, post := range forge.all() {
		if strings.Contains(post.body, "could not finish this step") || strings.Contains(post.body, "reply on this issue") {
			t.Fatalf("busy parent posted a failure comment: %q", post.body)
		}
	}
	instance, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "plan")
	mustProgress(t, err)
	if instance.Status != store.GraphRunning || instance.CurrentNode != "story_draft" || instance.NextDueAt.IsZero() {
		t.Fatalf("instance not parked at story_draft: %+v", instance)
	}
	attempts, err := f.db.ListGraphNodeAttempts(t.Context(), instance.ID)
	mustProgress(t, err)
	for _, attempt := range attempts {
		if attempt.Node == "story_draft" {
			t.Fatalf("busy parent reserved a story_draft attempt: %+v", attempt)
		}
	}
	if remote.creates != 0 {
		t.Fatalf("story written under a busy parent: %d", remote.creates)
	}
	mustProgress(t, release())
	released = true
	out, ran, err := runDueGraphStep(t, f, base)
	if !ran || err != nil || out.Err != nil {
		t.Fatalf("next dispatch: ran=%v %v %v %s", ran, err, out.Err, f.logs.String())
	}
	if *calls != 1 || remote.creates != 1 {
		t.Fatalf("agent calls=%d story effects=%d", *calls, remote.creates)
	}
	transitions, err := f.db.ListGraphTransitions(t.Context(), instance.ID)
	mustProgress(t, err)
	applied := false
	for _, tr := range transitions {
		applied = applied || (tr.FromNode == "story_draft" && tr.OnOutcome == "applied")
	}
	if !applied {
		t.Fatalf("story_draft did not apply after release: %+v", transitions)
	}
	draft, err := f.db.GetPlanStoryDraft(t.Context(), f.ref().String(), "story-001")
	mustProgress(t, err)
	if draft.IssueID == 0 || draft.Candidate == "" {
		t.Fatalf("plan story draft not recorded: %+v", draft)
	}
}

// IT-005-2 (AC-003): an ask reaches the owner through the retained result: the
// published comment carries the retained comment and the retained question text,
// and no story is created.
func TestPlanGraphIntegration_AskQuestionsPublishedFromTheRetainedResult(t *testing.T) {
	const marker = "RETAINED-ASK-COMMENT-17"
	f, remote, base, forge, _ := graphPlanAgentFixture(t, func(s planAgentStep) (*agent.RunResult, error) {
		ask := ar.Result{Outcome: "incomplete", Reason: "The actor is unclear.", Comment: marker, Questions: []ar.Question{{ID: "q-1", Audience: "business", Text: "Who exactly needs this story?"}}, Payload: ar.Payload{Plan: &ar.Plan{}}}
		_, accepted := s.call("communication_submit", submitBytes(t, ask))
		if !accepted.Validation.Valid || accepted.Receipt == nil {
			t.Fatalf("ask not accepted: %+v", accepted)
		}
		envelope, err := json.Marshal(ar.ResearchNode{Version: 1, Response: "ask", Result: ask})
		mustProgress(t, err)
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: string(envelope), FinishedAt: time.Now()}, nil
	})
	out, err := Execute(t.Context(), f.opts, base)
	if err != nil || out.Err != nil || !out.Succeeded() {
		t.Fatalf("attempt did not complete: %v %v %s", err, out.Err, f.logs.String())
	}
	if remote.creates != 0 {
		t.Fatal("a question created a story")
	}
	published := ""
	for _, post := range forge.all() {
		if strings.Contains(post.body, marker) {
			published = post.body
		}
	}
	if !strings.Contains(published, "Who exactly needs this story?") {
		t.Fatalf("the retained question was not published with the retained comment: %q", published)
	}
	instance, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "plan")
	mustProgress(t, err)
	transitions, err := f.db.ListGraphTransitions(t.Context(), instance.ID)
	mustProgress(t, err)
	var route []string
	for _, tr := range transitions {
		route = append(route, tr.FromNode+"/"+tr.OnOutcome+"->"+tr.ToNode)
	}
	if joined := strings.Join(route, " "); !strings.Contains(joined, "story_question/true->story_ask") || strings.Contains(joined, "story_draft") {
		t.Fatalf("control did not route the retained ask response to the question comment: %v", route)
	}
}

// IT-026-1 (AC-038): a submit whose reply is lost after the service recorded it.
// The agent recovers the receipt by request identity, does not resend, and the
// workflow completes on exactly one retained result and one story.
func TestPlanGraphIntegration_LostReplyRecoveredByReceiptCompletesWithOneResultAndOneEffect(t *testing.T) {
	var lostID string
	var recovered AgentResultSubmission
	f, remote, base, _, calls := graphPlanAgentFixture(t, func(s planAgentStep) (*agent.RunResult, error) {
		good := integrationDone(integrationCreateRequest(s.story), "Please review this candidate.")
		data := submitBytes(t, good)
		// The service recorded the request; the agent never sees the reply.
		first, _ := s.call("communication_submit", data)
		lostID = first.RequestID
		// Look the request up by identity before anything is retried.
		_, recovered = s.call("communication_receipt", []byte(`{"request_id":"`+lostID+`"}`))
		return agentDone(t, good), nil
	})
	out, err := Execute(t.Context(), f.opts, base)
	if err != nil || out.Err != nil || !out.Succeeded() {
		t.Fatalf("attempt did not complete: %v %v %s", err, out.Err, f.logs.String())
	}
	if recovered.Receipt == nil || recovered.Receipt.RequestID != lostID || recovered.Receipt.InputState != "accepted" || recovered.Receipt.AppliedRevision != 1 {
		t.Fatalf("receipt lookup did not recover the accepted candidate: %+v", recovered.Receipt)
	}
	if latest := recovered.Receipt.Latest; latest.Input != "accepted" || latest.Engineering != "pending" || latest.Acceptance != "pending" || latest.Publication != "pending" {
		t.Fatalf("recovery claimed more than input acceptance: %+v", latest)
	}
	if *calls != 1 {
		t.Fatalf("recovery needed a second agent invocation: %d", *calls)
	}
	revisions, _ := retainedRevisions(t, f, "story_agent")
	if len(revisions) != 1 || revisions[0] != 1 {
		t.Fatalf("retained revisions %v, want exactly one", revisions)
	}
	if remote.creates != 1 {
		t.Fatalf("story effects %d, want exactly one", remote.creates)
	}
}

// IT-026-2 (AC-038): the first request never reached the service (the call was
// cancelled before it was sent). The receipt lookup finds nothing and says so;
// resending the identical bytes keeps the identity and yields exactly one result;
// a further identical resend is a replay, not a second candidate.
func TestPlanGraphIntegration_UncertainSubmitRetriedWithIdenticalBytesYieldsOneResult(t *testing.T) {
	var lostID, retryID, replayID string
	var lookupResult submission.Response
	var retried, replayed AgentResultSubmission
	f, remote, base, _, _ := graphPlanAgentFixture(t, func(s planAgentStep) (*agent.RunResult, error) {
		good := integrationDone(integrationCreateRequest(s.story), "Please review this candidate.")
		data := submitBytes(t, good)
		cancelled, cancel := context.WithCancel(s.t.Context())
		cancel()
		r, err := s.client.Call(cancelled, "communication_submit", data)
		if err == nil {
			t.Fatalf("a cancelled call reported success: %+v", r)
		}
		lostID = r.RequestID
		if lostID == "" {
			t.Fatal("no request identity was kept for the unanswered submit")
		}
		// Nothing was recorded: the lookup finds no receipt.
		lookup, err := s.client.Call(s.t.Context(), "communication_receipt", []byte(`{"request_id":"`+lostID+`"}`))
		if err != nil {
			t.Fatalf("receipt lookup: %v", err)
		}
		lookupResult = lookup
		// No receipt: resend identical bytes; the identity is kept.
		var first submission.Response
		first, retried = s.call("communication_submit", data)
		retryID = first.RequestID
		again, replay := s.call("communication_submit", data)
		replayID, replayed = again.RequestID, replay
		return agentDone(t, good), nil
	})
	out, err := Execute(t.Context(), f.opts, base)
	if err != nil || out.Err != nil || !out.Succeeded() {
		t.Fatalf("attempt did not complete: %v %v %s", err, out.Err, f.logs.String())
	}
	if got := decodeReceipt(t, lookupResult); got.Validation.Valid || got.Receipt != nil || got.Snapshot != nil || len(got.Validation.Diagnostics.Diagnostics) != 1 || got.Validation.Diagnostics.Diagnostics[0].Code != "not_recorded" {
		t.Fatalf("recovery claimed an outcome that was never recorded: %+v", lookupResult)
	}
	if retryID != lostID || replayID != lostID {
		t.Fatalf("identical bytes changed identity: %q %q %q", lostID, retryID, replayID)
	}
	if retried.Receipt == nil || retried.Receipt.AppliedRevision != 1 || replayed.Receipt == nil || replayed.Receipt.ID != retried.Receipt.ID {
		t.Fatalf("retry and replay did not settle on one receipt: %+v %+v", retried.Receipt, replayed.Receipt)
	}
	revisions, _ := retainedRevisions(t, f, "story_agent")
	if len(revisions) != 1 {
		t.Fatalf("retained revisions %v, want exactly one", revisions)
	}
	if remote.creates != 1 {
		t.Fatalf("story effects %d, want exactly one", remote.creates)
	}
}

// IT-036-1 (AC-036, AC-037): the attempt's authority is revoked after a valid
// preflight. The submit is refused by the service, nothing is retained, the
// terminal envelope cannot stand in for it, and no story or review request is
// produced.
func TestPlanGraphIntegration_AuthorityRevokedAfterPreflightRetainsNothingAndAppliesNothing(t *testing.T) {
	var preflightValid bool
	var submitError string
	f, remote, base, forge, _ := graphPlanAgentFixture(t, func(s planAgentStep) (*agent.RunResult, error) {
		good := integrationDone(integrationCreateRequest(s.story), "Please review this candidate.")
		data := submitBytes(t, good)
		preflightValid = s.preflight(data).Valid
		authority := s.f.opts.MCP.Authority
		authority.mu.Lock()
		guards := make([]*AttemptGuard, 0, len(authority.guards))
		for _, g := range authority.guards {
			guards = append(guards, g)
		}
		authority.mu.Unlock()
		if len(guards) == 0 {
			t.Fatal("no live attempt authority to revoke")
		}
		for _, g := range guards {
			mustProgress(t, g.Revoke(s.t.Context(), "cancelled"))
		}
		r, err := s.client.Call(s.t.Context(), "communication_submit", data)
		if err != nil {
			t.Fatalf("submit transport: %v", err)
		}
		submitError = r.Error
		return agentDone(t, good), nil
	})
	out, err := Execute(t.Context(), f.opts, base)
	if !preflightValid {
		t.Fatal("the payload was not valid at preflight")
	}
	if submitError == "" {
		t.Fatal("a submit under revoked authority was accepted")
	}
	t.Logf("submit refusal: %s", submitError)
	if err == nil && out.Err == nil {
		t.Fatal("output under revoked authority completed nominally")
	}
	if remote.creates != 0 || remote.patches != 0 {
		t.Fatalf("revoked authority reached the forge: creates=%d patches=%d", remote.creates, remote.patches)
	}
	if _, err := f.db.GetPlanStoryDraft(t.Context(), f.ref().String(), "story-001"); err == nil {
		t.Fatal("story draft retained under revoked authority")
	}
	for _, post := range forge.all() {
		if strings.Contains(post.body, "To promote this exact candidate") {
			t.Fatal("a review request was published under revoked authority")
		}
	}
}
