package turn

import (
	"context"
	"errors"
	"fmt"
	"github.com/cloudvane/litefactory/internal/config"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

// implementWorkerFixture is a ledger of stories with one running story whose
// worker session exists, and a runtime for that worker.
type implementWorkerFixture struct {
	*implementFixture
	rt      *implementRuntime
	session store.Session
	saves   int
	saveErr error
}

func newImplementWorkerFixture(t *testing.T) *implementWorkerFixture {
	t.Helper()
	f := &implementWorkerFixture{implementFixture: newImplementFixture(t, implementStories(3, nil)...)}
	ctx := t.Context()
	if err := f.db.EnsureImplementStories(ctx, implementParent.String(), 1, f.auth.seeds(), implementClock); err != nil {
		t.Fatal(err)
	}
	f.drive("story-001", store.ImplementRunning, implementClock)
	repo, err := f.db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: "git@github.com:acme/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: filepath.Join(t.TempDir(), "w")})
	if err != nil {
		t.Fatal(err)
	}
	if f.session, err = f.db.GetOrCreateSession(ctx, repo, 11); err != nil {
		t.Fatal(err)
	}
	if err = f.db.SetImplementWorkerSession(ctx, implementParent.String(), 1, "story-001", f.session.ID, implementClock); err != nil {
		t.Fatal(err)
	}
	row, err := f.db.GetImplementStory(ctx, implementParent.String(), 1, "story-001")
	if err != nil {
		t.Fatal(err)
	}
	f.rt = &implementRuntime{opts: Options{DB: f.db}, parentRef: implementParent, generation: 1, worker: &row, save: func(context.Context) (Result, error) {
		f.saves++
		return Result{}, f.saveErr
	}}
	return f
}

func (f *implementWorkerFixture) budget() string {
	f.t.Helper()
	res, err := f.rt.budget(f.t.Context(), f.rt.opts, ActivityRequest{})
	if err != nil {
		f.t.Fatal(err)
	}
	return res.Outcome
}

func (f *implementWorkerFixture) fail(reason string) {
	f.t.Helper()
	if _, err := f.db.RecordImplementFailure(f.t.Context(), implementParent.String(), 1, "story-001", reason, implementClock); err != nil {
		f.t.Fatal(err)
	}
}

func TestImplementPolicyIsAdmittedAndRegistered(t *testing.T) {
	policy := ImplementWorkflowPolicy()
	if err := policy.Admit(); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range RegisteredWorkflowPolicies() {
		found = found || p.Mode == agent.ModeImplement
	}
	if !found {
		t.Fatal("the Implement policy is not in the registry every workflow guard walks")
	}
}

func TestImplementGraphCapturesAndEveryNodeIsBounded(t *testing.T) {
	definition, err := captureImplement(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	profile := definition.Profile
	if profile.Start != "select_path" || profile.Mode != agent.ModeImplement {
		t.Fatalf("start=%q mode=%q", profile.Start, profile.Mode)
	}
	roles := map[string]int{}
	for _, n := range profile.Nodes {
		if !n.Terminal && n.Limit <= 0 {
			t.Errorf("node %s has no positive lf_limit", n.Name)
		}
		if n.Action == "lf_agent" {
			roles[n.Attrs["lf_agent_markdown"]]++
			if n.Attrs["lf_skills"] != "implement-result,litefactory-rules" || n.Limit != 3 {
				t.Errorf("agent node = %+v", n)
			}
		}
	}
	if roles["story-worker"] != 1 || roles["sync-resolver"] != 2 || len(roles) != 2 {
		t.Fatalf("one story worker and a resolver per base sync: %v", roles)
	}
	for _, role := range []string{"story-worker.md", "sync-resolver.md"} {
		if _, ok := definition.Files[role]; !ok {
			t.Fatalf("the role %s is not captured: %v", role, definition.Files)
		}
	}
	if to, _ := graphexec.Decide(profile, "select_path", "coordinator"); to != "sync_start" {
		t.Errorf("coordinator path starts at %q", to)
	}
	if to, _ := graphexec.Decide(profile, "select_path", "worker"); to != "budget" {
		t.Errorf("worker path starts at %q", to)
	}
}

func TestImplementBudgetIsTheFirstAttemptAndTwoRepairs(t *testing.T) {
	f := newImplementWorkerFixture(t)
	for attempt, want := range []string{"run", "run", "run", "park"} {
		if got := f.budget(); got != want {
			t.Fatalf("before attempt %d the budget says %q, want %q", attempt+1, got, want)
		}
		f.fail("the process crashed")
	}
	if got := f.budget(); got != "park" {
		t.Fatalf("attempts never exceed %d: %q", store.MaxImplementAttempts, got)
	}
	row := f.row("story-001")
	if row.Attempts != store.MaxImplementAttempts {
		t.Fatalf("attempts = %d", row.Attempts)
	}
}

func TestImplementParkAfterTheBudgetKeepsTheLastFailureInPlainWords(t *testing.T) {
	f, _, rt := parkNoteFixture(t)
	res, err := rt.parkStory(t.Context(), f.opts, ActivityRequest{})
	if err != nil || res.Outcome != "parked" {
		t.Fatalf("%+v %v", res, err)
	}
	row := implementWorkerRow(t, f)
	if row.State != store.ImplementParked || row.ParkOrigin != store.ImplementOriginWorker || !strings.Contains(row.Reason, "3 attempt") || !strings.Contains(row.Reason, "the build did not compile") {
		t.Fatalf("parked row = %+v", row)
	}
	again, err := rt.parkStory(t.Context(), f.opts, ActivityRequest{})
	if err != nil || again.Outcome != "parked" {
		t.Fatalf("parking twice is a replay: %+v %v", again, err)
	}

}

func TestImplementStoryDoneSavesBeforeTheLedgerMoves(t *testing.T) {
	f := newImplementWorkerFixture(t)
	f.saveErr = errors.New("push rejected")
	res, err := f.rt.storyDone(t.Context(), f.rt.opts, ActivityRequest{})
	if err != nil || res.Outcome != "retry" {
		t.Fatalf("a failed save is a failed attempt to repair: %+v %v", res, err)
	}
	row := f.row("story-001")
	if row.State != store.ImplementRunning || row.Attempts != 1 || !strings.Contains(row.LastFailure, "push rejected") {
		t.Fatalf("a failed push must never mark the story done: %+v", row)
	}
	f.saveErr = nil
	res, err = f.rt.storyDone(t.Context(), f.rt.opts, ActivityRequest{})
	if err != nil || res.Outcome != "done" || f.saves != 2 {
		t.Fatalf("%+v %v saves=%d", res, err, f.saves)
	}
	if row = f.row("story-001"); row.State != store.ImplementDone {
		t.Fatalf("%+v", row)
	}
	if again, err := f.rt.storyDone(t.Context(), f.rt.opts, ActivityRequest{}); err != nil || again.Outcome != "done" || f.saves != 2 {
		t.Fatalf("a replay must not save twice: %+v %v saves=%d", again, err, f.saves)
	}
}

func TestImplementStoryDoneTreatsACancelledRunAsInfrastructure(t *testing.T) {
	f := newImplementWorkerFixture(t)
	f.saveErr = context.Canceled
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := f.rt.storyDone(ctx, f.rt.opts, ActivityRequest{}); err == nil {
		t.Fatal("a cancelled save is an error, not a retry")
	}
	if row := f.row("story-001"); row.State != store.ImplementRunning || row.Attempts != 0 {
		t.Fatalf("infrastructure faults never use the repair budget: %+v", row)
	}
}

func TestImplementStoryDoneRefusesAStoryThatIsNotRunning(t *testing.T) {
	f := newImplementWorkerFixture(t)
	f.drive("story-001", store.ImplementMerged, implementClock)
	if _, err := f.rt.storyDone(t.Context(), f.rt.opts, ActivityRequest{}); err == nil {
		t.Fatal("a merged story cannot be marked done again")
	}
}

func TestImplementAfterAgentFailureSettlesProcessFailuresOnly(t *testing.T) {
	f := newImplementWorkerFixture(t)
	definition := mustCaptureImplement(t)
	pin, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: definition.Profile.Team, Mode: "implement", ProfileVersion: 1, Path: "implement/implement.dot", Digest: definition.Profile.Digest, TeamSnapshotDigest: definition.Digest})
	if err != nil {
		t.Fatal(err)
	}
	instance, err := f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: f.session.ID, DefinitionID: pin.ID, CurrentNode: "worker_agent", StartedAt: implementClock, NextDueAt: implementClock})
	if err != nil {
		t.Fatal(err)
	}
	a := ActivityRequest{InstanceID: instance.ID, Node: "worker_agent"}
	cause := errors.New("research process outcome: failed")

	// Preparation failed before any process ran: an infrastructure fault.
	if res, err := implementAfterAgentFailure(t.Context(), f.rt.opts, a, ActivityResult{}, cause); !errors.Is(err, cause) || res.Outcome != "" {
		t.Fatalf("a failure before the process ran passes through: %+v %v", res, err)
	}
	// A cancelled run is shutdown, not a failed attempt.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	process := &agent.RunResult{Outcome: agent.OutcomeFailed, Reason: "exit 1"}
	if _, err := implementAfterAgentFailure(ctx, f.rt.opts, a, ActivityResult{ProcessResult: process}, cause); !errors.Is(err, cause) {
		t.Fatalf("a cancelled run passes through: %v", err)
	}
	if row := f.row("story-001"); row.Attempts != 0 {
		t.Fatalf("infrastructure used the budget: %+v", row)
	}
	// A process that ran and failed is a failed attempt, routed by the graph.
	res, err := implementAfterAgentFailure(t.Context(), f.rt.opts, a, ActivityResult{ProcessResult: process, Diagnostic: "exit 1"}, cause)
	if err != nil || res.Outcome != "failed" || res.ProcessResult != process {
		t.Fatalf("%+v %v", res, err)
	}
	if row := f.row("story-001"); row.Attempts != 1 || !strings.Contains(row.LastFailure, "exit 1") {
		t.Fatalf("%+v", row)
	}
	// Any other outcome, such as an abandoned process, is shutdown too.
	abandoned := &agent.RunResult{Outcome: agent.OutcomeAbandoned}
	if _, err := implementAfterAgentFailure(t.Context(), f.rt.opts, a, ActivityResult{ProcessResult: abandoned}, cause); !errors.Is(err, cause) {
		t.Fatalf("an abandoned run passes through: %v", err)
	}
}

func mustCaptureImplement(t *testing.T) *teamsource.WorkflowDefinition {
	t.Helper()
	definition, err := captureImplement(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return definition
}

func TestImplementResumeNamesStoriesByKeyOrResumesAll(t *testing.T) {
	f := newImplementFixture(t, implementStories(4, nil)...)
	if err := f.db.EnsureImplementStories(t.Context(), implementParent.String(), 1, f.auth.seeds(), implementClock); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"story-001", "story-002", "story-003"} {
		f.drive(key, store.ImplementDone, implementClock)
		f.drive(key, store.ImplementParked, implementClock)
	}
	rt := &implementRuntime{opts: Options{DB: f.db}, parentRef: implementParent, generation: 1}

	n, err := rt.resumeStories(t.Context(), "Please redo story-002 with the v2 API. (story-0021 is something else)")
	if err != nil || n != 1 {
		t.Fatalf("resumed %d: %v", n, err)
	}
	row := f.row("story-002")
	if row.State != store.ImplementWaiting || row.Attempts != 0 || !strings.Contains(row.Guidance, "v2 API") || row.Branch != "lf/story-002" {
		t.Fatalf("named story: %+v", row)
	}
	for _, key := range []string{"story-001", "story-003"} {
		if got := f.row(key); got.State != store.ImplementParked {
			t.Fatalf("%s must stay parked: %+v", key, got)
		}
	}
	if n, err = rt.resumeStories(t.Context(), "Go ahead, try again."); err != nil || n != 2 {
		t.Fatalf("a reply that names none resumes all: %d %v", n, err)
	}
	for _, key := range []string{"story-001", "story-003"} {
		if got := f.row(key); got.State != store.ImplementWaiting || got.Guidance != "Go ahead, try again." {
			t.Fatalf("%s: %+v", key, got)
		}
	}
	if n, err = rt.resumeStories(t.Context(), "again"); err != nil || n != 0 {
		t.Fatalf("a reply resumes only rows that are still parked: %d %v", n, err)
	}
}

func TestImplementResumeRestartsThroughReadyWithinTheCap(t *testing.T) {
	f := newImplementFixture(t, implementStories(6, nil)...)
	if err := f.db.EnsureImplementStories(t.Context(), implementParent.String(), 1, f.auth.seeds(), implementClock); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 6; i++ {
		key := fmt.Sprintf("story-%03d", i)
		f.drive(key, store.ImplementRunning, implementClock)
		f.drive(key, store.ImplementDone, implementClock)
		f.drive(key, store.ImplementParked, implementClock)
	}
	rt := &implementRuntime{opts: Options{DB: f.db}, parentRef: implementParent, generation: 1}
	if n, err := rt.resumeStories(t.Context(), "retry all"); err != nil || n != 6 {
		t.Fatalf("%d %v", n, err)
	}
	_, out := f.ready()
	if len(out.Started) != store.MaxImplementRunning || out.Running != store.MaxImplementRunning || len(out.Waiting) != 2 {
		t.Fatalf("resumed stories restart through ready within the cap of four: %+v", out)
	}
	for _, key := range []string{"story-005", "story-006"} {
		if got := f.row(key); got.State != store.ImplementWaiting || got.Guidance != "retry all" {
			t.Fatalf("capacity-blocked guidance lost: %+v", got)
		}
	}
}

func TestNamesStoryMatchesWholeKeysOnly(t *testing.T) {
	for text, want := range map[string]bool{
		"story-002":                 true,
		"Redo STORY-002, please":    true,
		"(story-002)":               true,
		"story-0021":                false,
		"my-story-002":              false,
		"story-002-b":               false,
		"nothing to see":            false,
		"story-002\nand story-003":  true,
		"story_002":                 false,
		"the story-002/other split": true,
	} {
		if got := namesStory(text, "story-002"); got != want {
			t.Errorf("namesStory(%q) = %v, want %v", text, got, want)
		}
	}
	if namesStory("anything", " ") {
		t.Error("a blank key names nothing")
	}
}

func TestImplementAnyParkedAnswersFromTheLedger(t *testing.T) {
	f := newImplementWorkerFixture(t)
	f.rt.worker = nil
	res, err := f.rt.anyParked(t.Context(), f.rt.opts, ActivityRequest{})
	if err != nil || res.Outcome != "false" {
		t.Fatalf("%+v %v", res, err)
	}
	f.drive("story-001", store.ImplementParked, implementClock)
	if res, err = f.rt.anyParked(t.Context(), f.rt.opts, ActivityRequest{}); err != nil || res.Outcome != "true" {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestImplementSelectSendsAStoryDownTheWorkerPath(t *testing.T) {
	worker := &implementRuntime{worker: &store.ImplementStory{Key: "a"}}
	if res, _ := worker.selectPath(t.Context(), Options{}, ActivityRequest{}); res.Outcome != "worker" {
		t.Fatalf("%+v", res)
	}
	if res, _ := (&implementRuntime{}).selectPath(t.Context(), Options{}, ActivityRequest{}); res.Outcome != "coordinator" {
		t.Fatalf("%+v", res)
	}
}

func TestImplementCoordinatorRestsOnlyWhileStoriesBuild(t *testing.T) {
	f := newImplementFixture(t, implementStories(3, nil)...)
	if err := f.db.EnsureImplementStories(t.Context(), implementParent.String(), 1, f.auth.seeds(), implementClock); err != nil {
		t.Fatal(err)
	}
	opts := Options{DB: f.db, MCP: &MCPRuntime{Now: func() time.Time { return implementClock }}}
	rt := &implementRuntime{opts: opts, parentRef: implementParent, generation: 1}
	await := graphexec.Node{Name: graphexec.ImplementAwaitNode, Action: graphexec.ImplementAwaitAction}
	decide := func() ParkDecision {
		t.Helper()
		d, err := rt.parkHook(t.Context(), opts, store.GraphInstance{}, await)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	if d := decide(); d.Park {
		t.Fatalf("nothing is building, so there is nothing to wait for: %+v", d)
	}
	f.drive("story-001", store.ImplementRunning, implementClock)
	d := decide()
	if !d.Park || !d.Due.Equal(implementClock.Add(implementPollInterval)) {
		t.Fatalf("a running story rests the coordinator with a backstop poll: %+v", d)
	}
	f.drive("story-002", store.ImplementDone, implementClock)
	if d := decide(); d.Park {
		t.Fatalf("a story waiting to be merged keeps the coordinator working: %+v", d)
	}
	other := graphexec.Node{Name: "merge", Action: graphexec.ImplementMergeAction}
	if d, err := rt.parkHook(t.Context(), opts, store.GraphInstance{}, other); err != nil || d.Park {
		t.Fatalf("only the await node rests: %+v %v", d, err)
	}
	worker := &implementRuntime{opts: opts, parentRef: implementParent, generation: 1, worker: &store.ImplementStory{}}
	if d, err := worker.parkHook(t.Context(), opts, store.GraphInstance{}, await); err != nil || d.Park {
		t.Fatalf("a worker never rests at await: %+v %v", d, err)
	}
}

func TestImplementQuestionListsEveryParkedStoryOnce(t *testing.T) {
	parked := []store.ImplementStory{
		{Key: "story-002", Reason: "The worker stopped with a blocked answer: the payment API key is missing.", ParkOrigin: store.ImplementOriginWorker},
		{Key: "story-005", Reason: "the integration branch had a conflict in the order service.", ParkOrigin: store.ImplementOriginMerge},
	}
	candidate := implementQuestionCandidate(parked)
	if err := validateImplementComment(store.GraphNodeAttempt{}, "question", candidate, ImplementWorkflowPolicy().baseContext(true)); err != nil {
		t.Fatal(err)
	}
	if len(candidate.Questions) != 2 {
		t.Fatalf("one question per parked story, in one comment: %+v", candidate.Questions)
	}
	text := renderImplementComment(candidate, "", naming.Ref{}, "")
	for _, want := range []string{"2 stories are stopped and need your decision", "story-002", "payment API key is missing", "story-005", "The integration branch had a conflict in the order service.", "could not be merged", "names no story restarts all of them"} {
		if !strings.Contains(text, want) {
			t.Errorf("the question does not say %q:\n%s", want, text)
		}
	}
	t.Logf("sample batched question:\n%s", text)
	one := implementQuestionCandidate(parked[:1])
	if !strings.Contains(one.Comment, "1 story is stopped and needs your decision") {
		t.Fatalf("%q", one.Comment)
	}
}

func TestImplementCommentContractRefusesAnythingButTheBatchedQuestion(t *testing.T) {
	scope := ImplementWorkflowPolicy().baseContext(true)
	good := implementQuestionCandidate([]store.ImplementStory{{Key: "a", Reason: "r"}})
	for name, mutate := range map[string]func(*agentresult.Result){
		"completed":      func(r *agentresult.Result) { r.Outcome = "completed" },
		"no questions":   func(r *agentresult.Result) { r.Questions = nil },
		"blank reason":   func(r *agentresult.Result) { r.Reason = " " },
		"unknown reader": func(r *agentresult.Result) { r.Questions[0].Audience = "nobody" },
		"duplicate ids":  func(r *agentresult.Result) { r.Questions = append(r.Questions, r.Questions[0]) },
		"blank question": func(r *agentresult.Result) { r.Questions[0].Text = "" },
		"no intro":       func(r *agentresult.Result) { r.Comment = "" },
	} {
		c := good
		c.Questions = append([]agentresult.Question(nil), good.Questions...)
		mutate(&c)
		if err := validateImplementComment(store.GraphNodeAttempt{}, "question", c, scope); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	for _, kind := range []string{"final", "failure", "review"} {
		if err := validateImplementComment(store.GraphNodeAttempt{}, kind, good, scope); err == nil {
			t.Errorf("kind %s accepted", kind)
		}
	}
	other := scope
	other.Mode = "spike"
	if err := validateImplementComment(store.GraphNodeAttempt{}, "question", good, other); err == nil {
		t.Error("another mode accepted")
	}
}

func TestImplementCompletionReplyIsAFinalCommentWithTheNextStep(t *testing.T) {
	scope := ImplementWorkflowPolicy().baseContext(true)
	rows := []store.ImplementStory{
		{Key: "story-001", MergeCommit: "aaa111", StateAt: implementClock.Add(2 * time.Minute)},
		{Key: "story-002", MergeCommit: "bbb222", StateAt: implementClock.Add(time.Minute)},
	}
	candidate := implementCompletionCandidate(rows, "lf/9-feature", "verified333", false)
	if err := validateImplementComment(store.GraphNodeAttempt{}, "final", candidate, scope); err != nil {
		t.Fatal(err)
	}
	text := renderImplementComment(candidate, "", naming.Ref{}, "")
	for _, want := range []string{"all 2 stories are merged into `lf/9-feature`, now at commit `verified333`", "**story-001**: merged as `aaa111`", "**story-002**: merged as `bbb222`", ValidateStartCommand} {
		if !strings.Contains(text, want) {
			t.Errorf("the completion reply does not say %q:\n%s", want, text)
		}
	}
	if strings.Index(text, "story-001") > strings.Index(text, "story-002") {
		t.Errorf("stories are listed in ledger order:\n%s", text)
	}
	auto := implementCompletionCandidate(rows[:1], "lf/9-feature", "verified333", true)
	if !strings.Contains(auto.Comment, "all 1 story is merged") || !strings.Contains(auto.Reason, "start automatically") || strings.Contains(auto.Reason, ValidateStartCommand) {
		t.Fatalf("auto-start wording: %+v", auto)
	}
	for name, mutate := range map[string]func(*agentresult.Result){
		"incomplete": func(r *agentresult.Result) { r.Outcome = "incomplete" },
		"no comment": func(r *agentresult.Result) { r.Comment = " " },
		"a question": func(r *agentresult.Result) {
			r.Questions = []agentresult.Question{{ID: "q1", Audience: "business", Text: "?"}}
		},
	} {
		c := candidate
		mutate(&c)
		if err := validateImplementComment(store.GraphNodeAttempt{}, "final", c, scope); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if err := validateImplementComment(store.GraphNodeAttempt{}, "question", candidate, scope); err == nil {
		t.Error("a completion reply is not a question")
	}
}

func TestImplementRespondMapsTheAcceptedOutcome(t *testing.T) {
	for _, tc := range []struct {
		r       agentresult.Result
		want    string
		wantErr bool
	}{
		{agentresult.Result{Outcome: "completed"}, "completed", false},
		{agentresult.Result{Outcome: "incomplete", Reason: "half done"}, "incomplete", false},
		{agentresult.Result{Outcome: "blocked", Reason: "need a key"}, "blocked", false},
		{agentresult.Result{Outcome: "blocked"}, "", true},
		{agentresult.Result{Outcome: "completed", Questions: []agentresult.Question{{ID: "q"}}}, "", true},
		{agentresult.Result{Outcome: "approved"}, "", true},
	} {
		got, err := implementRespond(tc.r)
		if got != tc.want || (err != nil) != tc.wantErr {
			t.Errorf("%+v = %q, %v", tc.r, got, err)
		}
	}
}

func TestImplementNodeDecoderIsSubmitTimeOnly(t *testing.T) {
	scope := agentresult.Context{Mode: "implement"}
	good := `{"version":1,"response":"completed","result":{"outcome":"completed","comment":"built","payload":{"implement":{"stories":[]}}}}`
	if _, d := agentresult.DecodeImplementNode([]byte(good), scope); !d.Valid {
		t.Fatalf("%+v", d)
	}
	for name, raw := range map[string]string{
		"response differs from outcome": `{"version":1,"response":"blocked","result":{"outcome":"completed","comment":"x","payload":{"implement":{}}}}`,
		"unknown response":              `{"version":1,"response":"done","result":{"outcome":"completed","comment":"x","payload":{"implement":{}}}}`,
		"bad version":                   `{"version":2,"response":"completed","result":{"outcome":"completed","comment":"x","payload":{"implement":{}}}}`,
	} {
		if _, d := agentresult.DecodeImplementNode([]byte(raw), scope); d.Valid {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, d := agentresult.DecodeImplementNode([]byte(good), agentresult.Context{Mode: "spike"}); d.Valid {
		t.Error("another mode accepted")
	}
}

func TestImplementEarlyReplyEligibilityAndUnusableTargets(t *testing.T) {
	for _, tc := range []struct {
		name, body  string
		trigger     int64
		park        bool
		wantRefusal bool
	}{
		{"running target", "story-001: change API", 91, true, true},
		{"merged target", "story-001: change API", 91, true, true},
		{"no parked targets", "please retry", 91, false, true},
		{"command", "/implement story-002", 91, true, false},
		{"issue body", "story-002", 0, true, false},
		{"private dispatch", "story-002", store.GraphDispatch{ID: 1}.TriggerID(), true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newImplementWorkerFixture(t)
			f.rt.worker = nil
			if tc.name == "merged target" {
				f.drive("story-001", store.ImplementMerged, implementClock)
			}
			if tc.park {
				f.drive("story-002", store.ImplementRunning, implementClock)
				f.drive("story-002", store.ImplementParked, implementClock)
			}
			before := f.row("story-002")
			f.rt.req = Request{SessionID: f.session.ID, Trigger: Trigger{CommentID: tc.trigger, Body: tc.body}}
			pin, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "builtin", Mode: "implement", ProfileVersion: 1, Path: "implement.dot", Digest: strings.Repeat("a", 64)})
			mustProgress(t, err)
			instance, err := f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: f.session.ID, DefinitionID: pin.ID, CurrentNode: "await", StartedAt: implementClock})
			mustProgress(t, err)
			err = f.rt.checkEarlyReply(t.Context())
			if err == nil {
				err = f.rt.applyReply(t.Context(), instance)
			}
			if errors.Is(err, ErrImplementReplyNotApplied) != tc.wantRefusal || (err != nil && !tc.wantRefusal) {
				t.Fatalf("reply error: %v", err)
			}
			if got := f.row("story-002"); got.Revision != before.Revision || got.Guidance != before.Guidance {
				t.Fatalf("unusable input changed story: %+v", got)
			}
		})
	}
}

func TestImplementScratchDirUsesSibling(t *testing.T) {
	opts := Options{MCP: &MCPRuntime{}}
	opts.MCP.Paths = config.NewPaths(filepath.Join(t.TempDir(), "home"))
	if got, want := implementScratchDir(opts), opts.MCP.Paths.ImplementScratchDir(); got != want {
		t.Fatalf("scratch = %q, want %q", got, want)
	}
}
