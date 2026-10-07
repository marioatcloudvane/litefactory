package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	ar "github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
)

type graphPlanHTTP struct {
	mu                                sync.Mutex
	children                          map[int]map[string]any
	attached                          map[int]bool
	creates, patches                  int
	failGet                           int
	loseCreate, failAttach, losePatch bool
}

func (s *graphPlanHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	parent := map[string]any{"id": int64(1230), "number": 123, "state": "open"}
	path := strings.TrimPrefix(r.URL.Path, "/repos/acme/acme-api/issues")
	var out any
	switch {
	case path == "/123" && r.Method == http.MethodGet:
		out = parent
	case path == "" && r.Method == http.MethodPost:
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.creates++
		number := 399 + s.creates
		body["id"] = int64(number * 10)
		body["number"] = number
		body["state"] = "open"
		body["html_url"] = fmt.Sprintf("https://github.com/acme/acme-api/issues/%d", number)
		s.children[number] = body
		out = body
		if s.loseCreate {
			s.loseCreate = false
			http.Error(w, `{"message":"lost create reply"}`, http.StatusInternalServerError)
			return
		}
	case path == "" && r.Method == http.MethodGet:
		list := []any{}
		for _, child := range s.children {
			list = append(list, child)
		}
		out = list
	case path == "/123/sub_issues" && r.Method == http.MethodPost:
		var payload struct {
			SubIssueID int64 `json:"sub_issue_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if s.failAttach {
			s.attached[int(payload.SubIssueID/10)] = true
			s.failAttach = false
			http.Error(w, `{"message":"attachment interrupted"}`, http.StatusInternalServerError)
			return
		}
		number := int(payload.SubIssueID / 10)
		s.attached[number] = true
		out = s.children[number]
	default:
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		number, err := strconv.Atoi(parts[0])
		if err != nil {
			http.Error(w, `{"message":"unknown fixture path"}`, http.StatusBadRequest)
			return
		}
		child := s.children[number]
		if child == nil {
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
			return
		}
		switch {
		case len(parts) == 2 && parts[1] == "parent":
			if !s.attached[number] {
				http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
				return
			}
			out = parent
		case r.Method == http.MethodGet:
			if s.failGet == number {
				s.failGet = 0
				http.Error(w, `{"message":"interrupted observation"}`, http.StatusInternalServerError)
				return
			}
			out = child
		case r.Method == http.MethodPatch:
			var patch map[string]any
			if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			for k, v := range patch {
				child[k] = v
			}
			s.patches++
			out = child
			if s.losePatch {
				s.losePatch = false
				http.Error(w, `{"message":"lost update reply"}`, http.StatusInternalServerError)
				return
			}
		default:
			http.Error(w, `{"message":"unexpected fixture method"}`, http.StatusBadRequest)
			return
		}
	}
	if err := json.NewEncoder(w).Encode(out); err != nil {
		return
	}
}

// graphPlanAnswer is what a fake Plan agent submits for one node: the stage the
// factory selected and the story request it authors. The factory-owned members are
// never part of it, which is what the shared route requires.
//
// TerminalOnly skips the live submission, Terminal replaces the envelope's
// result, and Printed replaces the printed text entirely, so a test can present
// terminal-only, mismatched and arbitrary printed output.
type graphPlanAnswer struct {
	Stage        string
	Request      *ar.PlanStoryRequest
	TerminalOnly bool
	Terminal     *ar.Result
	Printed      *string
	Ask          bool
	// Stop ends the process with this outcome instead of a result, and Reason
	// is what the provider reported.
	Stop   agent.Outcome
	Reason string
}

func graphPlanFixture(t *testing.T, kind string, mutate ...func(*graphPlanAnswer)) (*fixture, *graphPlanHTTP, Request, *int) {
	t.Helper()
	f, remote, base, calls, _ := graphPlanFixtureWithForge(t, kind, mutate...)
	return f, remote, base, calls
}

// graphPlanFixtureWithForge also returns the fake comment forge, so a test can
// count and read what Plan published, or make publication fail.
func graphPlanFixtureWithForge(t *testing.T, kind string, mutate ...func(*graphPlanAnswer)) (*fixture, *graphPlanHTTP, Request, *int, *fakeForge) {
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
		key, stage := matched[1], matched[2]
		// Every story stage is told which roles may own a task, so the
		// acceptance agent never has to ask the owner for them.
		if !strings.Contains(req.Prompt, strings.TrimSpace(planOwnerRolesBinding(planbrief.TaskOwnerScope()))) {
			t.Fatalf("the %s stage prompt does not name the task owner roles", stage)
		}
		d := planbrief.PlanDraft{Key: key, Name: "Bounded Plan", UserStory: planbrief.UserStoryContent{Actor: "owner", Want: "shape intent", Benefit: "clear scope", Notes: []string{}}, Problem: "Unclear plan", InScope: []string{"Story discussion"}, NotInScope: []string{"Implementation"}, Dependencies: planbrief.DependencyContent{StoryKeys: []string{}, IssueRefs: []string{}}}
		d.Frontend = kind == "frontend"
		operation, expected := "create", ""
		changed := []string{}
		if binding, err := f.db.GetPlanStoryDraft(t.Context(), f.ref().String(), key); err == nil {
			operation, expected = "update", binding.Revision
			if err = json.Unmarshal([]byte(binding.DraftJSON), &d); err != nil {
				t.Fatal(err)
			}
		}
		if stage == "technical" {
			d.Architecture = &planbrief.PlanArchitecture{General: []planbrief.GeneralDecision{{Area: "Roles", Decision: "Reuse focused roles", Rationale: "Existing boundaries"}}}
			d.Guidance = []string{"agent-setup"}
			changed = []string{"architecture", "guidance"}
			if kind == "go" {
				d.Guidance = []string{"go"}
				d.Architecture.ServiceDesign = []planbrief.ServiceDefinition{{Component: "Store", Responsibility: "Own SQL", Access: "Single connection"}}
			}
			if kind == "frontend" {
				d.Frontend = true
				d.Design = []string{"https://example.com/design"}
				changed = append(changed, "design")
			}
		}
		if stage == "acceptance" {
			d.Criteria = []planbrief.PlanCriterion{{ID: "AC-001", Text: "Story remains stable", ProofMethod: "Scoped check"}}
			d.Tasks = []planbrief.TaskContent{{ID: "T-001", Description: "Implement", OwnerRole: "go-implementation-engineer", CriterionIDs: []string{"AC-001"}, DependsOn: []string{}}}
			changed = []string{"criteria", "tasks"}
		}
		answer := graphPlanAnswer{Stage: stage, Request: &ar.PlanStoryRequest{Operation: operation, ExpectedRevision: expected, Draft: d, ChangedSections: changed}}
		for _, change := range mutate {
			change(&answer)
		}
		result, response := ar.Result{Outcome: "completed", Comment: "Please review this candidate.", Payload: ar.Payload{Plan: &ar.Plan{StoryRequest: answer.Request}}}, "done"
		if answer.Ask {
			result, response = ar.Result{Outcome: "incomplete", Reason: "The actor is unclear.", Comment: "Please clarify the actor.", Questions: []ar.Question{{ID: "q-1", Audience: "business", Text: "Who needs this?"}}, Payload: ar.Payload{Plan: &ar.Plan{}}}, "ask"
		}
		if answer.Stop != 0 {
			return &agent.RunResult{Outcome: answer.Stop, Reason: answer.Reason, FinishedAt: time.Now()}, nil
		}
		envelope, err := json.Marshal(ar.ResearchNode{Version: 1, Response: response, Result: result})
		if err != nil {
			t.Fatal(err)
		}
		if !answer.TerminalOnly {
			submitLiveResult(t, req, string(envelope))
		}
		if answer.Terminal != nil {
			if envelope, err = json.Marshal(ar.ResearchNode{Version: 1, Response: response, Result: *answer.Terminal}); err != nil {
				t.Fatal(err)
			}
		}
		printed := string(envelope)
		if answer.Printed != nil {
			printed = *answer.Printed
		}
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: printed, FinishedAt: time.Now()}, nil
	})
	var remote *graphPlanHTTP
	var base Request
	var commentForge *fakeForge
	f, remote, base, commentForge = setupGraphPlanFixture(t, runner, true)
	return f, remote, base, &calls, commentForge
}

// setupGraphPlanFixture assembles a graph Plan session around a scripted runner.
// seedStory starts the scope with its first story recorded, as a session that
// already has stories does; without it the scope is empty and starts at the set
// stage.
func setupGraphPlanFixture(t *testing.T, runner agent.Runner, seedStory bool) (*fixture, *graphPlanHTTP, Request, *fakeForge) {
	t.Helper()
	f, commentForge, base := assembledResearchFixture(t, runner)
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
	// New sessions default to legacy Plan; these tests exercise the graph route.
	_, err = f.db.AdmitPlanWorkflowPath(t.Context(), f.session.ID, "graph")
	mustProgress(t, err)
	base.Mode = agent.ModePlan
	base.Command = ParseCommand("/plan shape stories")
	base.Trigger.Body = "/plan shape stories"
	base.Trigger.AuthorID = 7
	if seedStory {
		state := planbrief.PlanState{SharedRevision: "initial", Stories: []planbrief.PlanStoryState{{Key: "story-001", Stage: "story", Approvals: map[string]string{}, Draft: planbrief.PlanDraft{Key: "story-001"}}}}
		_, err = f.db.SavePlanWorkflowState(t.Context(), f.ref().String(), 0, state)
		mustProgress(t, err)
	}
	return f, remote, base, commentForge
}

func graphPlanState(t *testing.T, f *fixture) planbrief.PlanState {
	t.Helper()
	s, _, err := f.db.GetPlanWorkflowState(t.Context(), f.ref().String())
	mustProgress(t, err)
	return s
}
func graphPlanApprove(t *testing.T, f *fixture, base Request, id int64) Request {
	t.Helper()
	s := graphPlanState(t, f)
	var item planbrief.PlanStoryState
	for _, candidate := range s.Stories {
		if candidate.Wait != nil {
			item = candidate
			break
		}
	}
	if item.Wait == nil {
		t.Fatal("Plan owner wait absent")
	}
	w := item.Wait
	reply := &planbrief.PlanOwnerReply{ID: id, OwnerID: 7, ActorID: 7, IssueID: w.IssueID, Key: item.Key, Stage: w.Stage, Candidate: w.Candidate, WaitID: w.ID, Body: "/approve-plan " + item.Key + " " + w.Stage + " " + w.Candidate}
	base.Trigger = Trigger{CommentID: id, Body: reply.Body, Author: "owner", AuthorID: 7, PlanReply: reply}
	base.Command = Command{Kind: KindFreeText, Text: reply.Body}
	return asStoryTask(t, f, base, item.Key)
}
func executeGraphPlan(t *testing.T, f *fixture, req Request) Result {
	t.Helper()
	out, err := Execute(t.Context(), f.opts, req)
	if err != nil || out.Err != nil {
		t.Fatalf("Plan execute err=%v result=%+v logs=%s", err, out, f.logs.String())
	}
	return out
}

func TestPlanGraphBoundary_ProgressiveHandoffAndSave(t *testing.T) {
	for _, kind := range []string{"agent", "go", "frontend"} {
		t.Run(kind, func(t *testing.T) {
			f, remote, base, calls := graphPlanFixture(t, kind)
			executeGraphPlan(t, f, base)
			binding, err := f.db.GetPlanStoryDraft(t.Context(), f.ref().String(), "story-001")
			mustProgress(t, err)
			id := binding.IssueID
			number := binding.IssueNumber
			remote.mu.Lock()
			initial, _ := remote.children[number]["body"].(string)
			remote.mu.Unlock()
			if !strings.Contains(initial, "# Task") || strings.Contains(initial, "## Architecture - General") {
				t.Fatal("initial scaffold populated later content")
			}
			for i := range 3 {
				req := graphPlanApprove(t, f, base, base.Trigger.CommentID+int64(i+1))
				executeGraphPlan(t, f, req)
			}
			runParent(t, f, base)
			state := graphPlanState(t, f)
			if state.AssembledWait == "" || state.PlanReady(planbrief.ContentScope{Roles: []string{"go-implementation-engineer"}}) {
				t.Fatal("third-stage set wait missing or prematurely ready")
			}
			// A separately opened migrated connection observes exact durable wait state.
			reopened, err := store.Open(t.Context(), f.paths.DBPath())
			mustProgress(t, err)
			restored, _, err := reopened.GetPlanWorkflowState(t.Context(), f.ref().String())
			mustProgress(t, err)
			mustProgress(t, reopened.Close())
			if restored.AssembledWait != state.AssembledWait || len(restored.ConsumedReplies) != 3 {
				t.Fatal("restart lost approvals/reply correlation")
			}
			final := base
			final.Trigger = Trigger{CommentID: base.Trigger.CommentID + 10, Body: "/approve-plan-set " + state.AssembledWait, AuthorID: 7}
			final.Command = Command{Kind: KindFreeText, Text: final.Trigger.Body}
			out := executeGraphPlan(t, f, final)
			state = graphPlanState(t, f)
			feature, err := f.db.GetManagedFeature(t.Context(), f.ref().String())
			mustProgress(t, err)
			if !feature.Ready || !state.PlanReady(planbrief.ContentScope{Roles: []string{"go-implementation-engineer"}}) || *calls != 3 {
				t.Fatalf("handoff unready or extra provider: %+v calls=%d", feature, *calls)
			}
			finalBinding, err := f.db.GetPlanStoryDraft(t.Context(), f.ref().String(), "story-001")
			mustProgress(t, err)
			if finalBinding.IssueID != id || finalBinding.IssueNumber != number || remote.creates != 1 {
				t.Fatal("handoff recreated issue")
			}
			remote.mu.Lock()
			body, _ := remote.children[number]["body"].(string)
			title, _ := remote.children[number]["title"].(string)
			remote.mu.Unlock()
			if title != "123-001: Bounded Plan" || !strings.Contains(body, "## Architecture - General") || !strings.Contains(body, planScaffoldHeadings(t)[4]) {
				t.Fatal("final projection reverted to legacy format")
			}
			if kind == "frontend" && !strings.Contains(body, "# Design") {
				t.Fatal("frontend references lost")
			}
			if kind == "agent" && strings.Contains(body, "## Service Design") {
				t.Fatal("agent scope acquired service design")
			}
			var index string
			mustProgress(t, filepath.WalkDir(out.WorktreePath, func(path string, entry os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if entry.Name() == "plan_brief.md" {
					raw, e := os.ReadFile(path)
					if e != nil {
						return e
					}
					index = string(raw)
				}
				return nil
			}))
			if !strings.Contains(index, "[#400]") || strings.Contains(index, "Story remains stable") {
				t.Fatal("saved index is not link-only", index)
			}
		})
	}
}

func TestPlanGraphBoundary_DirectEditsPreserveAndReopen(t *testing.T) {
	f, remote, base, _ := graphPlanFixture(t, "agent")
	executeGraphPlan(t, f, base)
	binding, err := f.db.GetPlanStoryDraft(t.Context(), f.ref().String(), "story-001")
	mustProgress(t, err)
	client, ok := f.opts.SubIssueCreator.(PlanStoryForge)
	if !ok {
		t.Fatal("fixture requires PlanStoryForge")
	}
	scope := planbrief.ContentScope{Roles: []string{"go-implementation-engineer"}}
	for i := range 2 {
		remote.mu.Lock()
		body, ok := remote.children[binding.IssueNumber]["body"].(string)
		if !ok {
			remote.mu.Unlock()
			t.Fatal("fixture child body must be a string")
		}
		remote.children[binding.IssueNumber]["body"] = strings.Replace(body, "# Userstory\n", "# Userstory\n\n", 1)
		remote.mu.Unlock()
		issue, err := client.GetIssue(t.Context(), github.Repo{Owner: "acme", Name: "acme-api"}, binding.IssueNumber)
		mustProgress(t, err)
		if err = ReconcilePlanChildEdit(t.Context(), f.db, f.ref().String(), 123, 1230, issue, scope); err != nil {
			t.Fatalf("presentation-only edit %d rejected: %v", i+1, err)
		}
	}
	remote.mu.Lock()
	body, ok := remote.children[binding.IssueNumber]["body"].(string)
	if !ok {
		remote.mu.Unlock()
		t.Fatal("fixture child body must be a string")
	}
	remote.children[binding.IssueNumber]["body"] = strings.Replace(body, "I want shape intent", "I want revised intent", 1)
	remote.mu.Unlock()
	issue, err := client.GetIssue(t.Context(), github.Repo{Owner: "acme", Name: "acme-api"}, binding.IssueNumber)
	mustProgress(t, err)
	mustProgress(t, ReconcilePlanChildEdit(t.Context(), f.db, f.ref().String(), 123, 1230, issue, scope))
	state := graphPlanState(t, f)
	if state.Stories[0].Stage != "story" || state.Stories[0].Wait != nil || state.Stories[0].Draft.UserStory.Want != "revised intent" {
		t.Fatal("semantic owner edit not reconciled", state)
	}
	got, err := f.db.GetPlanStoryDraft(t.Context(), f.ref().String(), "story-001")
	mustProgress(t, err)
	if got.Revision != PlanIssueRevision(issue.Title, issue.Body) || got.IssueID != binding.IssueID {
		t.Fatal("owner edit discarded revision or identity")
	}
}

func TestPlanGraphBoundary_AgentSuppliedFactoryValuesRefused(t *testing.T) {
	f, _, _, _ := graphPlanFixture(t, "agent")
	runtime := planGraphRuntime{opts: f.opts, req: f.request(), result: Result{Ref: f.ref()}, parentRef: f.ref(), parentNumber: f.ref().Issue, selected: "story-001", stage: "story"}
	trusted, err := runtime.resultContext(t.Context(), ActivityRequest{InstanceID: 1, NodeAttemptID: 2})
	mustProgress(t, err)
	for _, member := range []string{"upstream", "stage", "candidate", "request_id", "shared_revision"} {
		value := `"foreign"`
		if member == "upstream" {
			value = `["foreign-upstream"]`
		}
		raw := `{"version":1,"response":"done","result":{"outcome":"completed","comment":"Review","payload":{"plan":{"storyrequest":{"operation":"create","` + member + `":` + value + `,"draft":` + graphBindingDraft + `}}}}}`
		if _, d := ar.DecodePlanNode([]byte(raw), trusted); d.Valid {
			t.Fatalf("agent-supplied %s accepted", member)
		}
	}
}

// C1 (#410): a Plan step whose valid result was submitted succeeds on it
// whatever it printed afterwards, and the story effect applies the retained
// request.
func TestPlanGraphBoundary_SubmittedResultStandsWhateverIsPrinted(t *testing.T) {
	printed := func(text string) func(*graphPlanAnswer) {
		return func(a *graphPlanAnswer) { a.Printed = &text }
	}
	for name, change := range map[string]func(*graphPlanAnswer){
		"differing envelope": func(a *graphPlanAnswer) {
			other := ar.Result{Outcome: "completed", Comment: "A different result.", Payload: ar.Payload{Plan: &ar.Plan{StoryRequest: a.Request}}}
			a.Terminal = &other
		},
		"missing closing brace": printed(`{"version":1,"response":"done","result":{"outcome":"completed","comment":"Please review this candidate."}`),
		"json, not an envelope": printed(`{"outcome":"completed"}`),
		"prose":                 printed("Submitted; the receipt is accepted."),
		"empty":                 printed(""),
	} {
		t.Run(name, func(t *testing.T) {
			f, remote, base, _ := graphPlanFixture(t, "agent", change)
			executeGraphPlan(t, f, base)
			if remote.creates != 1 {
				t.Fatalf("the retained story request was not applied: creates=%d", remote.creates)
			}
			binding, err := f.db.GetPlanStoryDraft(t.Context(), f.ref().String(), "story-001")
			mustProgress(t, err)
			var draft planbrief.PlanDraft
			mustProgress(t, json.Unmarshal([]byte(binding.DraftJSON), &draft))
			if draft.Name != "Bounded Plan" {
				t.Fatalf("story does not carry the retained draft: %+v", draft)
			}
		})
	}
}

func TestPlanGraphBoundary_TerminalOnlyOutputCannotCreateStories(t *testing.T) {
	for _, tt := range []struct {
		name   string
		change func(*graphPlanAnswer)
		want   string
	}{
		{"terminal only", func(a *graphPlanAnswer) { a.TerminalOnly = true }, "no accepted shared result"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f, remote, base, _ := graphPlanFixture(t, "agent", tt.change)
			out, err := Execute(t.Context(), f.opts, base)
			if err == nil && out.Err == nil {
				t.Fatal("output that is not the retained result completed nominally")
			}
			if joined := fmt.Sprint(err, out.Err, out.Reason); !strings.Contains(joined, tt.want) {
				t.Fatalf("cause not reported: %q", joined)
			}
			if remote.creates != 0 {
				t.Fatal("a story was created from output that is not the retained result")
			}
			if _, err := f.db.GetPlanStoryDraft(t.Context(), f.ref().String(), "story-001"); err == nil {
				t.Fatal("story draft retained from output that is not the retained result")
			}
		})
	}
}

func TestPlanGraphBoundary_LostCreateAndAttachmentRecovery(t *testing.T) {
	for _, phase := range []string{"create", "attachment"} {
		t.Run(phase, func(t *testing.T) {
			f, remote, base, _ := graphPlanFixture(t, "agent")
			remote.loseCreate = phase == "create"
			remote.failAttach = phase == "attachment"
			_, _ = Execute(t.Context(), f.opts, base)
			if remote.creates != 1 {
				t.Fatalf("initial create count=%d", remote.creates)
			}
			next := base
			next.Trigger.CommentID++
			next.Trigger.Body = "/plan Continue retained draft"
			next.Command = Command{Kind: KindFreeText, Text: next.Trigger.Body}
			executeGraphPlan(t, f, next)
			binding, err := f.db.GetPlanStoryDraft(t.Context(), f.ref().String(), "story-001")
			mustProgress(t, err)
			if binding.IssueID != 4000 || binding.IssueNumber != 400 || remote.creates != 1 || !remote.attached[400] {
				t.Fatal("recovery lost or duplicated identity", binding, remote.creates)
			}
		})
	}
}

func graphPlanApproveKey(t *testing.T, f *fixture, base Request, key string, id int64) Request {
	t.Helper()
	state := graphPlanState(t, f)
	for _, item := range state.Stories {
		if item.Key == key && item.Wait != nil {
			w := item.Wait
			reply := &planbrief.PlanOwnerReply{ID: id, OwnerID: 7, ActorID: 7, IssueID: w.IssueID, Key: key, Stage: w.Stage, Candidate: w.Candidate, WaitID: w.ID, Body: "/approve-plan " + key + " " + w.Stage + " " + w.Candidate}
			base.Trigger = Trigger{CommentID: id, Body: reply.Body, AuthorID: 7, PlanReply: reply}
			base.Command = Command{Kind: KindFreeText, Text: reply.Body}
			return asStoryTask(t, f, base, key)
		}
	}
	t.Fatal("selected story wait missing", key)
	return base
}

func TestPlanGraphBoundary_ThreeIndependentStories(t *testing.T) {
	f, remote, base, _ := graphPlanFixture(t, "agent")
	executeGraphPlan(t, f, base)
	id := base.Trigger.CommentID
	for range 2 {
		id++
		next := base
		next.Trigger = Trigger{CommentID: id, AuthorID: 7, Body: "/plan-new-story"}
		next.Command = Command{Kind: KindFreeText, Text: next.Trigger.Body}
		executeGraphPlan(t, f, next)
	}
	for _, key := range []string{"story-002", "story-003"} {
		for range 3 {
			id++
			executeGraphPlan(t, f, graphPlanApproveKey(t, f, base, key, id))
		}
	}
	state := graphPlanState(t, f)
	if len(state.Stories) != 3 || remote.creates != 3 {
		t.Fatal("missing independent stories", len(state.Stories), remote.creates)
	}
	for _, item := range state.Stories {
		if item.Key == "story-001" {
			if item.Stage != "story" || item.Wait == nil || len(item.Approvals) != 0 {
				t.Fatal("held story promoted")
			}
		} else if item.Stage != "acceptance" || len(item.Approvals) != 3 {
			t.Fatal("independent story did not advance", item)
		}
	}
	feature, err := f.db.GetManagedFeature(t.Context(), f.ref().String())
	if err == nil && feature.Ready {
		t.Fatal("partial scope marked ready")
	}
}

func TestPlanGraphBoundary_ChildAuthorityAndMixedFeedback(t *testing.T) {
	f, _, base, calls := graphPlanFixture(t, "agent")
	executeGraphPlan(t, f, base)
	for i, kind := range []string{"foreign owner", "wrong issue", "stale candidate", "wrong story"} {
		t.Run(kind, func(t *testing.T) {
			req := graphPlanApprove(t, f, base, base.Trigger.CommentID+int64(i+1))
			switch kind {
			case "foreign owner":
				req.Trigger.AuthorID = 8
				req.Trigger.PlanReply.ActorID = 8
			case "wrong issue":
				req.Trigger.PlanReply.IssueID++
			case "stale candidate":
				req.Trigger.PlanReply.Candidate = "old"
			case "wrong story":
				req.Trigger.PlanReply.Key = "foreign"
			}
			out, err := Execute(t.Context(), f.opts, req)
			if err == nil && out.Err == nil {
				t.Fatal("unbound child reply accepted")
			}
			state := graphPlanState(t, f)
			if state.Stories[0].Stage != "story" || len(state.Stories[0].Approvals) != 0 || *calls != 1 {
				t.Fatal("unbound reply progressed workflow")
			}
		})
	}
	mixed := graphPlanApprove(t, f, base, base.Trigger.CommentID+10)
	mixed.Trigger.Body += " but change the actor"
	mixed.Trigger.PlanReply.Body = mixed.Trigger.Body
	mixed.Command.Text = mixed.Trigger.Body
	executeGraphPlan(t, f, mixed)
	if state := graphPlanState(t, f); state.Stories[0].Stage != "story" || len(state.Stories[0].Approvals) != 0 {
		t.Fatal("mixed feedback promoted stage")
	}
}

func TestPlanGraphBoundary_SaveFailureKeepsPublicationUnready(t *testing.T) {
	f, remote, base, calls := graphPlanFixture(t, "agent")
	executeGraphPlan(t, f, base)
	for i := range 3 {
		executeGraphPlan(t, f, graphPlanApprove(t, f, base, base.Trigger.CommentID+int64(i+1)))
	}
	runParent(t, f, base)
	state := graphPlanState(t, f)
	if state.AssembledWait == "" {
		t.Fatal("set review missing")
	}
	hook := filepath.Join(f.repo.Origin, "hooks", "pre-receive")
	mustProgress(t, os.WriteFile(hook, []byte("#!/bin/sh\necho rejected >&2\nexit 1\n"), 0700))
	req := base
	req.Trigger = Trigger{CommentID: base.Trigger.CommentID + 10, AuthorID: 7, Body: "/approve-plan-set " + state.AssembledWait}
	req.Command = Command{Kind: KindFreeText, Text: req.Trigger.Body}
	out, err := Execute(t.Context(), f.opts, req)
	if err == nil && out.Err == nil {
		t.Fatal("failed push claimed success")
	}
	state = graphPlanState(t, f)
	if state.PublishedRevision == "" || state.SavedRevision != "" {
		t.Fatal("save failure lost publication or claimed save", state)
	}
	feature, err := f.db.GetManagedFeature(t.Context(), f.ref().String())
	mustProgress(t, err)
	if feature.Ready {
		t.Fatal("failed save marked feature ready")
	}
	mustProgress(t, os.Remove(hook))
	req.Trigger.CommentID++
	req.Trigger.Body = "/plan Resume retained save"
	req.Command.Text = req.Trigger.Body
	executeGraphPlan(t, f, req)
	state = graphPlanState(t, f)
	if !state.PlanReady(planbrief.ContentScope{Roles: []string{"go-implementation-engineer"}}) || remote.creates != 1 || *calls != 3 {
		t.Fatal("save retry duplicated issue/provider or remained unready")
	}
}

func TestPlanGraphBoundary_LostUpdatePreservesIdentity(t *testing.T) {
	f, remote, base, _ := graphPlanFixture(t, "agent")
	executeGraphPlan(t, f, base)
	remote.losePatch = true
	req := graphPlanApprove(t, f, base, base.Trigger.CommentID+1)
	_, _ = Execute(t.Context(), f.opts, req)
	before, err := f.db.GetPlanStoryDraft(t.Context(), f.ref().String(), "story-001")
	mustProgress(t, err)
	next := base
	next.Trigger.CommentID += 2
	next.Trigger.Body = "/plan Continue retained update"
	next.Command = Command{Kind: KindFreeText, Text: next.Trigger.Body}
	executeGraphPlan(t, f, next)
	after, err := f.db.GetPlanStoryDraft(t.Context(), f.ref().String(), "story-001")
	mustProgress(t, err)
	var draft planbrief.PlanDraft
	mustProgress(t, json.Unmarshal([]byte(after.DraftJSON), &draft))
	if after.IssueID != before.IssueID || remote.creates != 1 || draft.Architecture == nil || len(draft.Architecture.General) == 0 {
		t.Fatal("lost update recovery lost architecture or identity")
	}
}

func TestPlanGraphBoundary_OwnerRetirementExcludesOnlyConfirmedScope(t *testing.T) {
	technical := 0
	f, remote, base, _ := graphPlanFixture(t, "agent", func(answer *graphPlanAnswer) {
		if answer.Stage == "technical" {
			technical++
			if technical == 2 {
				answer.Request.Operation = "retire"
				answer.Request.ChangedSections = nil
			}
		}
	})
	executeGraphPlan(t, f, base)
	add := base
	add.Trigger = Trigger{CommentID: base.Trigger.CommentID + 1, AuthorID: 7, Body: "/plan-new-story"}
	add.Command = Command{Kind: KindFreeText, Text: add.Trigger.Body}
	executeGraphPlan(t, f, add)
	executeGraphPlan(t, f, graphPlanApproveKey(t, f, base, "story-001", base.Trigger.CommentID+2))
	feedback := graphPlanApproveKey(t, f, base, "story-001", base.Trigger.CommentID+3)
	feedback.Trigger.Body = "This intent is no longer relevant"
	feedback.Trigger.PlanReply.Body = feedback.Trigger.Body
	feedback.Command.Text = feedback.Trigger.Body
	executeGraphPlan(t, f, feedback)
	state := graphPlanState(t, f)
	item := state.Stories[0]
	if item.Wait == nil || item.Wait.Kind != "retire" || item.Retired {
		t.Fatal("retirement proposal closed scope or lost exact wait")
	}
	// Another independent story advances after the retirement proposal; the
	// original wait must still locate its own retained proposal after restart.
	executeGraphPlan(t, f, graphPlanApproveKey(t, f, base, "story-002", base.Trigger.CommentID+4))
	req := graphPlanApproveKey(t, f, base, item.Key, base.Trigger.CommentID+5)
	req.Trigger.Body = "/retire-plan " + item.Key + " " + item.Candidate
	req.Trigger.PlanReply.Body = req.Trigger.Body
	req.Command.Text = req.Trigger.Body
	executeGraphPlan(t, f, req)
	state = graphPlanState(t, f)
	if !state.Stories[0].Retired || state.Stories[1].Retired {
		t.Fatal("retirement changed unrelated scope")
	}
	remote.mu.Lock()
	status := remote.children[400]["state"]
	reason := remote.children[400]["state_reason"]
	remote.mu.Unlock()
	if status != "closed" || reason != "not_planned" || remote.creates != 2 {
		t.Fatal("retirement did not retain and close original issue", status, reason)
	}
}

func TestPlanGraphBoundary_PartialHandoffResumesSameIssues(t *testing.T) {
	f, remote, base, calls := graphPlanFixture(t, "agent")
	executeGraphPlan(t, f, base)
	id := base.Trigger.CommentID + 1
	next := base
	next.Trigger = Trigger{CommentID: id, AuthorID: 7, Body: "/plan-new-story"}
	next.Command = Command{Kind: KindFreeText, Text: next.Trigger.Body}
	executeGraphPlan(t, f, next)
	for _, key := range []string{"story-001", "story-002"} {
		for range 3 {
			id++
			executeGraphPlan(t, f, graphPlanApproveKey(t, f, base, key, id))
		}
	}
	runParent(t, f, base)
	state := graphPlanState(t, f)
	id++
	next.Trigger = Trigger{CommentID: id, AuthorID: 7, Body: "/approve-plan-set " + state.PlanSetRevision()}
	next.Command = Command{Kind: KindFreeText, Text: next.Trigger.Body}
	remote.failGet = 401
	out, err := Execute(t.Context(), f.opts, next)
	if err == nil && out.Err == nil {
		t.Fatal("interrupted second story observation reported success")
	}
	state = graphPlanState(t, f)
	if state.PublishedRevision != "" || state.SavedRevision != "" {
		t.Fatal("partial publication established readiness", state)
	}
	feature, err := f.db.GetManagedFeature(t.Context(), f.ref().String())
	mustProgress(t, err)
	if feature.Ready {
		t.Fatal("partial publication feature ready")
	}
	first, err := f.db.ListManagedStories(t.Context(), f.ref().String(), feature.Generation)
	mustProgress(t, err)
	if len(first) != 1 || first[0].IssueID != 4000 {
		t.Fatal("first retained identity changed", first)
	}
	id++
	next.Trigger = Trigger{CommentID: id, AuthorID: 7, Body: "/plan Resume retained publication"}
	next.Command.Text = next.Trigger.Body
	executeGraphPlan(t, f, next)
	state = graphPlanState(t, f)
	if !state.PlanReady(planbrief.ContentScope{Roles: []string{"go-implementation-engineer"}}) || remote.creates != 2 || *calls != 6 {
		t.Fatal("partial handoff recovery duplicated or remained unready", remote.creates, *calls, state)
	}
	second, err := f.db.ListManagedStories(t.Context(), f.ref().String(), feature.Generation)
	mustProgress(t, err)
	if len(second) != 2 || second[1].IssueID != 4010 {
		t.Fatal("second retained identity changed", second)
	}
}

// Later nodes read the retained accepted shared result, resolved from the reference
// the agent node kept. Any other JSON output, which includes what a private format
// retained, is refused as a result and is never parsed.
func TestPlanGraphBoundary_LaterNodesReadTheRetainedSharedResult(t *testing.T) {
	f, _, base, _ := graphPlanFixture(t, "agent")
	executeGraphPlan(t, f, base)
	instance, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "plan")
	mustProgress(t, err)
	attempts, err := f.db.ListGraphNodeAttempts(t.Context(), instance.ID)
	mustProgress(t, err)
	runtime := planGraphRuntime{opts: f.opts, req: f.request(), result: Result{Ref: f.ref()}, parentRef: f.ref(), parentNumber: f.ref().Issue, selected: "story-001", stage: "story"}
	var agentSeen, refused int
	for _, attempt := range attempts {
		got, err := runtime.loadRetainedResult(t.Context(), attempt.ID)
		switch attempt.Node {
		case "story_agent":
			agentSeen++
			if err != nil || got.Response != "done" || got.Request == nil || got.Request.Stage != "story" || got.Request.Candidate != planCandidate(instance.ID, attempt.ID) || got.Request.RequestID != planRequestID(instance.ID, attempt.ID) || got.Request.Draft.Key != "story-001" {
				t.Fatalf("retained result not read back with the factory's binding: %+v %v", got, err)
			}
		case "story_draft":
			if err == nil || !strings.Contains(err.Error(), "never read as a result") {
				t.Fatalf("a non-reference output was read as a result: %+v %v", got, err)
			}
			refused++
		}
	}
	if agentSeen != 1 || refused != 1 {
		t.Fatalf("expected one agent and one effect attempt, saw %d and %d", agentSeen, refused)
	}
}

// A question is an ask under the shared contract: it names no story branch, so it
// can publish its questions and wait without any story effect.
func TestPlanGraphBoundary_AskPublishesQuestionsWithoutAStoryEffect(t *testing.T) {
	f, remote, base, _ := graphPlanFixture(t, "agent", func(a *graphPlanAnswer) { a.Ask = true })
	executeGraphPlan(t, f, base)
	if remote.creates != 0 {
		t.Fatal("a question created a story")
	}
	if _, err := f.db.GetPlanStoryDraft(t.Context(), f.ref().String(), "story-001"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a question retained a story draft: %v", err)
	}
	instance, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "plan")
	mustProgress(t, err)
	intents, err := f.db.ListGraphComments(t.Context(), instance.ID, "", 10)
	mustProgress(t, err)
	if len(intents) != 1 || intents[0].Node != "story_ask" {
		t.Fatalf("the question was not published as retained: %+v", intents)
	}
}
