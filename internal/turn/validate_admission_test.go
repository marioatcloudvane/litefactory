package turn

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/bugchecks"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

var errValidateStop = errors.New("validate test: stop at the first bound step")

const (
	validateOwnerID = int64(77)
	validateHeadA   = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	validateHeadB   = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// validateAdmissionFixture is a managed parent whose Implement ran on the graph.
type validateAdmissionFixture struct {
	f      *fixture
	parent string
	head   string
	binds  int
	result Result
}

func newValidateAdmissionFixture(t *testing.T) *validateAdmissionFixture {
	t.Helper()
	f, _, _ := assembledImplementFixture(t, lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
		t.Error("no agent runs at admission")
		return nil, nil
	}))
	v := &validateAdmissionFixture{f: f, head: validateHeadA}
	v.parent = seedValidateParent(t, f,
		store.ManagedStory{Key: "story-001", IssueNumber: 201, IssueID: 7001},
		store.ManagedStory{Key: "story-002", IssueNumber: 202, IssueID: 7002})
	f.repo.Git("push", "--quiet", "origin", "HEAD:refs/heads/lf/123-feature")
	ref, err := naming.ParseRef(f.session.TaskRef)
	mustProgress(t, err)
	v.result = Result{Ref: ref, SessionID: f.session.ID}
	f.opts.Validate = &ValidateRuntime{
		OwnerID: func(context.Context, Options) (int64, error) { return validateOwnerID, nil },
		Head:    func(context.Context, Options, store.Session) (string, error) { return v.head, nil },
		Bind: func(context.Context, Options, Request, store.ValidateEntry) (ValidateSuiteBindings, ValidateFlowBindings, error) {
			v.binds++
			return ValidateSuiteBindings{}, ValidateFlowBindings{}, errValidateStop
		},
	}
	return v
}

// implementRan records a finished Implement coordinator instance and puts each
// story in the given state.
func (v *validateAdmissionFixture) implementRan(t *testing.T, status string, states map[string]string) {
	t.Helper()
	ctx := t.Context()
	mustProgress(t, v.f.db.EnsureImplementStories(ctx, v.parent, 1, []store.ImplementStorySeed{{Key: "story-001", Position: 0, Branch: "lf/201-a"}, {Key: "story-002", Position: 1, Branch: "lf/202-b"}}, implementClock))
	for key, to := range states {
		row, err := v.f.db.GetImplementStory(ctx, v.parent, 1, key)
		mustProgress(t, err)
		path := map[string][]string{
			store.ImplementWaiting: nil,
			store.ImplementRunning: {store.ImplementRunning},
			store.ImplementParked:  {store.ImplementRunning, store.ImplementParked},
			store.ImplementMerged:  {store.ImplementRunning, store.ImplementDone, store.ImplementMerged},
		}[to]
		from := store.ImplementWaiting
		for _, step := range path {
			tr := store.ImplementTransition{Parent: v.parent, Generation: 1, Key: key, From: from, To: step, ExpectedRevision: row.Revision, At: implementClock}
			switch step {
			case store.ImplementParked:
				tr.Reason = "needs a decision"
			case store.ImplementMerged:
				tr.MergeCommit = strings.Repeat("c", 40)
			}
			row, err = v.f.db.TransitionImplementStory(ctx, tr)
			mustProgress(t, err)
			from = step
		}
	}
	def := mustCaptureImplement(t)
	pin, err := v.f.db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: def.Profile.Team, Mode: "implement", ProfileVersion: def.Profile.ProfileVersion, Path: "implement/implement.dot", Digest: def.Profile.Digest, TeamSnapshotDigest: def.Digest})
	mustProgress(t, err)
	inst, err := v.f.db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: v.f.session.ID, DefinitionID: pin.ID, CurrentNode: def.Profile.Start, StartedAt: implementClock})
	mustProgress(t, err)
	if status != store.GraphRunning {
		mustProgress(t, v.f.db.SetGraphInstanceStatus(ctx, inst.ID, status, time.Time{}, implementClock))
	}
}

func (v *validateAdmissionFixture) allMerged(t *testing.T) {
	t.Helper()
	v.implementRan(t, store.GraphSucceeded, map[string]string{"story-001": store.ImplementMerged, "story-002": store.ImplementMerged})
}

func (v *validateAdmissionFixture) request(comment, author int64, body string) Request {
	req := v.f.request()
	req.Mode = agent.ModeValidate
	req.Trigger = Trigger{CommentID: comment, AuthorID: author, Body: body, Author: "owner"}
	req.Command = ParseCommand(body)
	return req
}

func (v *validateAdmissionFixture) validateInstances(t *testing.T) int {
	t.Helper()
	n := 0
	for _, e := range v.entries(t) {
		if e.InstanceID > 0 {
			n++
		}
	}
	return n
}

func (v *validateAdmissionFixture) entries(t *testing.T) []store.ValidateEntry {
	t.Helper()
	list, err := v.f.db.ListValidateEntries(t.Context(), v.f.session.ID)
	mustProgress(t, err)
	return list
}

func (v *validateAdmissionFixture) admit(t *testing.T, req Request) (Result, bool, error) {
	t.Helper()
	return admitValidateExecution(t.Context(), v.f.opts, req, v.result)
}

func TestValidateOwnerStartAdmitsOneRunPinnedToTheFullHead(t *testing.T) {
	v := newValidateAdmissionFixture(t)
	v.allMerged(t)
	out, handled, err := v.admit(t, v.request(5001, validateOwnerID, "/validate check the finished work"))
	if !handled || !errors.Is(err, errValidateStop) {
		t.Fatalf("the start reaches the host bindings: handled=%v err=%v out=%+v", handled, err, out)
	}
	entries := v.entries(t)
	if len(entries) != 1 {
		t.Fatalf("one entry: %+v", entries)
	}
	e := entries[0]
	if e.State != store.ValidateEntryAdmitted || e.Commit != validateHeadA || e.Source != store.ValidateSourceOwner || e.CommentID != 5001 || e.AuthorID != validateOwnerID || e.Note != "check the finished work" || e.InstanceID < 1 {
		t.Fatalf("admitted entry pinned to the full head: %+v", e)
	}
	inst, err := v.f.db.LatestWorkflowInstance(t.Context(), v.f.session.ID, "validate")
	if err != nil || inst.ID != e.InstanceID {
		t.Fatalf("the entry names the Validate instance: %+v %v", inst, err)
	}
}

func TestValidateStartFromAnyoneButTheOwnerStartsNothing(t *testing.T) {
	v := newValidateAdmissionFixture(t)
	v.allMerged(t)
	for name, req := range map[string]Request{
		"non-owner": v.request(5002, 1234, "/validate please"),
		"bot":       v.request(5003, 9999, "/validate please"),
	} {
		out, handled, err := v.admit(t, req)
		if !handled || err != nil || !strings.Contains(out.Reason, "Only the owner") || out.Disposition != DispositionPrepareFailed {
			t.Fatalf("%s: handled=%v err=%v out=%+v", name, handled, err, out)
		}
	}
	// Quoted text and free text are not the /validate command at all.
	for name, body := range map[string]string{"quoted": "> /validate please\nthanks", "free text": "please validate this"} {
		if _, handled, err := v.admit(t, v.request(5004, validateOwnerID, body)); handled || err != nil {
			t.Fatalf("%s must start nothing: handled=%v err=%v", name, handled, err)
		}
	}
	if len(v.entries(t)) != 0 || v.binds != 0 {
		t.Fatalf("nothing was saved or bound: %+v binds=%d", v.entries(t), v.binds)
	}
}

func TestValidateGateRefusesUnfinishedImplementWithAPlainReply(t *testing.T) {
	for name, tc := range map[string]struct {
		status string
		states map[string]string
		want   []string
	}{
		"still running":       {store.GraphRunning, map[string]string{"story-001": store.ImplementMerged, "story-002": store.ImplementRunning}, []string{"still running", "story-002 (running)"}},
		"waiting parked":      {store.GraphRunning, map[string]string{"story-001": store.ImplementMerged, "story-002": store.ImplementParked}, []string{"waiting for you", "story-002 (parked)"}},
		"finished but parked": {store.GraphSucceeded, map[string]string{"story-001": store.ImplementParked, "story-002": store.ImplementMerged}, []string{"Not every approved story is merged", "story-001 (parked)"}},
		"failed":              {store.GraphFailed, map[string]string{"story-001": store.ImplementMerged, "story-002": store.ImplementMerged}, []string{"did not finish"}},
	} {
		t.Run(name, func(t *testing.T) {
			v := newValidateAdmissionFixture(t)
			v.implementRan(t, tc.status, tc.states)
			out, handled, err := v.admit(t, v.request(5010, validateOwnerID, "/validate go"))
			if !handled || err != nil || out.Disposition != DispositionPrepareFailed {
				t.Fatalf("handled=%v err=%v out=%+v", handled, err, out)
			}
			for _, want := range tc.want {
				if !strings.Contains(out.Reason, want) {
					t.Errorf("the reply must contain %q:\n%s", want, out.Reason)
				}
			}
			entries := v.entries(t)
			if len(entries) != 1 || entries[0].State != store.ValidateEntryRefused || entries[0].Reason != out.Reason || v.validateInstances(t) != 0 || v.binds != 0 {
				t.Fatalf("the refusal is recorded with its reason and nothing starts: %+v binds=%d", entries, v.binds)
			}
			if _, err := v.f.db.LatestWorkflowInstance(t.Context(), v.f.session.ID, "validate"); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("no Validate instance exists: %v", err)
			}
		})
	}
}

func TestValidateRefusedStartIsRetriedByALaterStart(t *testing.T) {
	v := newValidateAdmissionFixture(t)
	v.implementRan(t, store.GraphRunning, map[string]string{"story-001": store.ImplementMerged, "story-002": store.ImplementRunning})
	if _, _, err := v.admit(t, v.request(5010, validateOwnerID, "/validate go")); err != nil {
		t.Fatal(err)
	}
	// Implement finishes; the same commit now starts.
	mustProgress(t, v.f.db.SetGraphInstanceStatus(t.Context(), mustImplementInstance(t, v).ID, store.GraphSucceeded, time.Time{}, implementClock))
	for _, key := range []string{"story-002"} {
		row, err := v.f.db.GetImplementStory(t.Context(), v.parent, 1, key)
		mustProgress(t, err)
		row, err = v.f.db.TransitionImplementStory(t.Context(), store.ImplementTransition{Parent: v.parent, Generation: 1, Key: key, From: store.ImplementRunning, To: store.ImplementDone, ExpectedRevision: row.Revision, At: implementClock})
		mustProgress(t, err)
		_, err = v.f.db.TransitionImplementStory(t.Context(), store.ImplementTransition{Parent: v.parent, Generation: 1, Key: key, From: store.ImplementDone, To: store.ImplementMerged, ExpectedRevision: row.Revision, MergeCommit: strings.Repeat("d", 40), At: implementClock})
		mustProgress(t, err)
	}
	if _, handled, err := v.admit(t, v.request(5011, validateOwnerID, "/validate again")); !handled || !errors.Is(err, errValidateStop) {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	entries := v.entries(t)
	if len(entries) != 1 || entries[0].State != store.ValidateEntryAdmitted || entries[0].CommentID != 5011 {
		t.Fatalf("one entry, reopened and admitted: %+v", entries)
	}
}

func mustImplementInstance(t *testing.T, v *validateAdmissionFixture) store.GraphInstance {
	t.Helper()
	inst, err := v.f.db.LatestWorkflowInstance(t.Context(), v.f.session.ID, "implement")
	mustProgress(t, err)
	return inst
}

func TestValidateStartTwiceGivesOneInstanceAndTheRepeatLinksTheReport(t *testing.T) {
	v := newValidateAdmissionFixture(t)
	v.allMerged(t)
	if _, handled, err := v.admit(t, v.request(5001, validateOwnerID, "/validate first")); !handled || !errors.Is(err, errValidateStop) {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	first := v.entries(t)[0]

	// The same comment replayed (a lost reply, a restart) goes on with the same
	// entry and instance; it neither refuses nor starts a second one.
	if _, handled, err := v.admit(t, v.request(5001, validateOwnerID, "/validate first")); !handled || !errors.Is(err, errValidateStop) {
		t.Fatalf("replay: handled=%v err=%v", handled, err)
	}
	if entries := v.entries(t); len(entries) != 1 || entries[0].InstanceID != first.InstanceID {
		t.Fatalf("the replay keeps one entry and instance: %+v", entries)
	}
	// A second /validate comment for the same result starts nothing new.
	mustProgress(t, v.f.db.PutValidateReport(t.Context(), store.ValidateReport{InstanceID: first.InstanceID, CommentID: 9090, Marker: "m", Revision: "r", Digest: "d", UpdatedAt: implementClock}))
	out, handled, err := v.admit(t, v.request(5002, validateOwnerID, "/validate second"))
	if !handled || err != nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	if !strings.Contains(out.Reason, "already been started") || !strings.Contains(out.Reason, "#issuecomment-9090") {
		t.Fatalf("the repeat reply links the report:\n%s", out.Reason)
	}
	if entries := v.entries(t); len(entries) != 1 || entries[0].InstanceID != first.InstanceID || v.binds != 2 {
		t.Fatalf("exactly one instance and one run: %+v binds=%d", entries, v.binds)
	}
}

func TestValidateAutomaticStartThenManualStartIsOneRun(t *testing.T) {
	v := newValidateAdmissionFixture(t)
	v.allMerged(t)
	stored, _, err := v.f.db.CreateOrGetSchedule(t.Context(), store.CreateScheduleParams{SessionID: v.f.session.ID, SourceCommentID: 1 << 56, OwnerHandle: "owner", Payload: ValidateAutoPayload, DueAt: implementClock})
	mustProgress(t, err)
	auto := v.request(stored.TriggerID(), 0, ValidateAutoPayload)
	if _, handled, err := v.admit(t, auto); !handled || !errors.Is(err, errValidateStop) {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	entries := v.entries(t)
	if len(entries) != 1 || entries[0].Source != store.ValidateSourceAuto || entries[0].State != store.ValidateEntryAdmitted || entries[0].Commit != validateHeadA {
		t.Fatalf("automatic entry: %+v", entries)
	}
	out, handled, err := v.admit(t, v.request(5005, validateOwnerID, "/validate by hand"))
	if !handled || err != nil || !strings.Contains(out.Reason, "already been started") {
		t.Fatalf("manual after automatic: handled=%v err=%v out=%+v", handled, err, out)
	}
	if len(v.entries(t)) != 1 || v.binds != 1 {
		t.Fatalf("still one run: %+v binds=%d", v.entries(t), v.binds)
	}
}

func TestValidateNewHeadGetsANewEntryAndASuccessorInstance(t *testing.T) {
	v := newValidateAdmissionFixture(t)
	v.allMerged(t)
	if _, _, err := v.admit(t, v.request(5001, validateOwnerID, "/validate first")); !errors.Is(err, errValidateStop) {
		t.Fatal(err)
	}
	first := v.entries(t)[0]

	// A push while the first run is still going is refused: two runs would move one branch.
	v.head = validateHeadB
	out, handled, err := v.admit(t, v.request(5006, validateOwnerID, "/validate newer"))
	if !handled || err != nil || !strings.Contains(out.Reason, "still going") {
		t.Fatalf("handled=%v err=%v out=%+v", handled, err, out)
	}
	// Once the first run ended, the new commit starts a successor.
	mustProgress(t, v.f.db.SetGraphInstanceStatus(t.Context(), first.InstanceID, store.GraphSucceeded, time.Time{}, implementClock))
	if _, handled, err := v.admit(t, v.request(5007, validateOwnerID, "/validate newer")); !handled || !errors.Is(err, errValidateStop) {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	var admitted []store.ValidateEntry
	for _, e := range v.entries(t) {
		if e.State == store.ValidateEntryAdmitted {
			admitted = append(admitted, e)
		}
	}
	if len(admitted) != 2 || admitted[0].Commit != validateHeadA || admitted[1].Commit != validateHeadB || admitted[0].InstanceID == admitted[1].InstanceID {
		t.Fatalf("a new head is a new entry with a successor instance: %+v", admitted)
	}
	pred, _, err := v.f.db.WorkflowPredecessor(t.Context(), admitted[1].InstanceID)
	if err != nil || pred != admitted[0].InstanceID {
		t.Fatalf("the successor links its predecessor: %d %v", pred, err)
	}
}

func TestValidateAdmissionLeavesLegacyWorkOnTheLegacyPath(t *testing.T) {
	v := newValidateAdmissionFixture(t)
	v.allMerged(t)
	req := v.request(5001, validateOwnerID, "/validate go")
	for name, tc := range map[string]struct {
		req  Request
		opts func(*Options)
	}{
		"another mode":     {req: func() Request { r := req; r.Mode = agent.ModePlan; return r }()},
		"bug":              {req: func() Request { r := req; r.Workflow = agent.WorkflowBug; return r }()},
		"no host bindings": {req: req, opts: func(o *Options) { o.Validate = nil }},
		"no reporter":      {req: req, opts: func(o *Options) { o.ResearchReporter = nil }},
	} {
		opts := v.f.opts
		if tc.opts != nil {
			tc.opts(&opts)
		}
		if _, handled, err := admitValidateExecution(t.Context(), opts, tc.req, v.result); handled || err != nil {
			t.Errorf("%s: handled=%v err=%v", name, handled, err)
		}
	}
	// A separated feature whose Implement never ran on the graph keeps the legacy path.
	f2, _, _ := assembledImplementFixture(t, lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) { return nil, nil }))
	f2.opts.Validate = v.f.opts.Validate
	seedImplementParent(t, f2, store.ManagedStory{Key: "story-001", IssueNumber: 201, IssueID: 7001})
	ref, err := naming.ParseRef(f2.session.TaskRef)
	mustProgress(t, err)
	r2 := req
	r2.SessionID = f2.session.ID
	if _, handled, err := admitValidateExecution(t.Context(), f2.opts, r2, Result{Ref: ref, SessionID: f2.session.ID}); handled || err != nil {
		t.Fatalf("no graph Implement: handled=%v err=%v", handled, err)
	}
	// An unmanaged task stays legacy too.
	f3, _, _ := assembledImplementFixture(t, lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) { return nil, nil }))
	f3.opts.Validate = v.f.opts.Validate
	ref3, err := naming.ParseRef(f3.session.TaskRef)
	mustProgress(t, err)
	r3 := req
	r3.SessionID = f3.session.ID
	if _, handled, err := admitValidateExecution(t.Context(), f3.opts, r3, Result{Ref: ref3, SessionID: f3.session.ID}); handled || err != nil {
		t.Fatalf("unmanaged: handled=%v err=%v", handled, err)
	}
}

func TestImplementHandoffNextStepLineFollowsTheSetting(t *testing.T) {
	off, on := implementValidateNextStep(false), implementValidateNextStep(true)
	if !strings.Contains(off, "`"+ValidateStartCommand+"`") || !strings.HasPrefix(ValidateStartCommand, "/validate ") || strings.Count(off, "\n") != 1 {
		t.Fatalf("off: one line with the exact command: %q", off)
	}
	if !strings.Contains(on, "start automatically") || strings.Contains(on, "/validate") || strings.Count(on, "\n") != 1 {
		t.Fatalf("on: one line saying it starts by itself: %q", on)
	}
	// Both lines are valid Validate start commands.
	if c := ParseCommand(ValidateStartCommand); c.Kind != KindSpike || c.Name != "/validate" {
		t.Fatalf("the shown command parses as /validate: %+v", c)
	}
}

// claimOwnerReply claims the waiting instance's owner wait with one comment,
// as the daemon's tick does before it hands the comment to Execute.
func (a *adoptFlow) claimOwnerReply(t *testing.T, instanceID int64, body string) int64 {
	t.Helper()
	wait, err := a.f.db.GetGraphOwnerWait(t.Context(), instanceID)
	if err != nil || wait.State != "waiting" {
		t.Fatalf("no open owner wait: %+v %v", wait, err)
	}
	delivery, err := a.f.db.GetReportDelivery(t.Context(), wait.DeliveryKey)
	mustProgress(t, err)
	comment := delivery.CommentID + 1
	_, err = a.f.db.ClaimGraphOwnerReply(t.Context(), store.GraphOwnerReply{InstanceID: instanceID, OwnerID: validateOwnerID, AuthorID: validateOwnerID, CommentID: comment, QuestionCommentID: delivery.CommentID, TaskRef: a.f.session.TaskRef, SourceRef: a.f.session.TaskRef, Mode: "validate", Workflow: "validate", DeliveryKey: wait.DeliveryKey, Body: body, At: time.Now()})
	mustProgress(t, err)
	return comment
}

// waitingValidate runs a manual start to the owner wait and returns the
// instance and its current report revision.
func (a *adoptFlow) waitingValidate(t *testing.T) (store.GraphInstance, string) {
	t.Helper()
	if out, err := a.manual(t, 6001); err != nil {
		t.Fatalf("%+v %v", out, err)
	}
	inst, err := a.f.db.LatestWorkflowInstance(t.Context(), a.f.session.ID, "validate")
	mustProgress(t, err)
	if got := a.reached(t); got.Wait != "waiting" || got.Instances != 1 {
		t.Fatalf("the run rests at the owner wait: %+v", got)
	}
	st, err := LoadValidateState(t.Context(), a.f.opts, captureValidate(t).Profile, inst.ID)
	mustProgress(t, err)
	if st.Revision == "" {
		t.Fatal("the report has a revision")
	}
	return inst, st.Revision
}

// visits counts the instance's attempts at one node.
func (a *adoptFlow) visits(t *testing.T, instanceID int64, node string) int {
	t.Helper()
	attempts, err := a.f.db.ListGraphNodeAttempts(t.Context(), instanceID)
	mustProgress(t, err)
	n := 0
	for _, at := range attempts {
		if at.Node == node {
			n++
		}
	}
	return n
}

// #591 c-ship-tick: an owner /validate-ship claimed as the reply to a waiting
// Validate instance continues that same instance through the real Execute to
// the push step; it is never a plain agent run.
func TestValidateShipReplyContinuesTheWaitingInstanceToThePush(t *testing.T) {
	a := newAdoptFlow(t)
	inst, rev := a.waitingValidate(t)
	body := ValidateShipCommand + " " + rev + " -- opened the page and clicked through it"
	resolved := a.visits(t, inst.ID, "owner_resolve")
	comment := a.claimOwnerReply(t, inst.ID, body)
	binds := a.binds

	out, err := Execute(t.Context(), a.f.opts, a.request(comment, validateOwnerID, body))
	if err != nil {
		t.Fatalf("%+v %v", out, err)
	}
	if !out.ResearchGraph || a.binds != binds+1 {
		t.Fatalf("the ship took the legacy path: %+v binds=%d", out, a.binds)
	}
	latest, err := a.f.db.LatestWorkflowInstance(t.Context(), a.f.session.ID, "validate")
	mustProgress(t, err)
	if latest.ID != inst.ID || len(a.entries(t)) != 1 {
		t.Fatalf("the same instance continues, no successor: %+v (was %d) %+v", latest, inst.ID, a.entries(t))
	}
	if pred, _, err := a.f.db.WorkflowPredecessor(t.Context(), latest.ID); err != nil || pred != 0 {
		t.Fatalf("no successor link: %d %v", pred, err)
	}
	if a.visits(t, inst.ID, "owner_resolve") <= resolved || a.visits(t, inst.ID, "owner_push") == 0 {
		t.Fatalf("the ship went through the resolve step to the push: node %s", latest.CurrentNode)
	}
	if out.Run.ID > 0 {
		graph, err := a.f.db.HasWorkflowGraphRun(t.Context(), out.Run.ID, "validate")
		if err != nil || !graph {
			t.Fatalf("the run for the comment is a graph run: %+v %v %v", out.Run, graph, err)
		}
	}
}

// #591 c-no-plain-run: every other owner control claimed as the reply goes to
// the waiting instance's resolve step, never to a plain agent run.
func TestValidateControlRepliesContinueTheWaitingInstance(t *testing.T) {
	for _, name := range []string{ValidateCheckCommand, ValidateFindingCommand, ValidatePushCommand, ValidateMergeCommand, ValidateCloseCommand} {
		t.Run(name, func(t *testing.T) {
			a := newAdoptFlow(t)
			inst, rev := a.waitingValidate(t)
			body := map[string]string{
				ValidateCheckCommand:   ValidateCheckCommand + " " + rev + " story-001:AC-001 pass -- looked fine",
				ValidateFindingCommand: ValidateFindingCommand + " " + rev + " story-001:code defer",
				ValidatePushCommand:    ValidatePushCommand + " " + rev,
				ValidateMergeCommand:   ValidateMergeCommand + " " + rev + " 601 " + validateHeadA,
				ValidateCloseCommand:   ValidateCloseCommand + " " + rev + " 201",
			}[name]
			resolved := a.visits(t, inst.ID, "owner_resolve")
			comment := a.claimOwnerReply(t, inst.ID, body)
			binds := a.binds
			out, err := Execute(t.Context(), a.f.opts, a.request(comment, validateOwnerID, body))
			if err != nil {
				t.Fatalf("%+v %v", out, err)
			}
			if !out.ResearchGraph || a.binds != binds+1 || a.visits(t, inst.ID, "owner_resolve") <= resolved {
				t.Fatalf("the control took the legacy path: %+v binds=%d", out, a.binds)
			}
			latest, err := a.f.db.LatestWorkflowInstance(t.Context(), a.f.session.ID, "validate")
			mustProgress(t, err)
			if latest.ID != inst.ID {
				t.Fatalf("the same instance continues: %d, was %d", latest.ID, inst.ID)
			}
			if wait, err := a.f.db.GetGraphOwnerWait(t.Context(), inst.ID); err != nil || wait.State == "claimed" && wait.ReplyID == comment {
				t.Fatalf("the claimed reply was consumed: %+v %v", wait, err)
			}
		})
	}
}

// #591 c-unclaimed-control: a control that holds no owner-wait claim is
// refused with a notice and starts nothing.
func TestValidateUnclaimedControlIsRefusedAndStartsNothing(t *testing.T) {
	a := newAdoptFlow(t)
	inst, rev := a.waitingValidate(t)
	binds, resolved := a.binds, a.visits(t, inst.ID, "owner_resolve")
	body := ValidateShipCommand + " " + rev + " -- checked"
	out, err := Execute(t.Context(), a.f.opts, a.request(99001, validateOwnerID, body))
	if err != nil {
		t.Fatalf("%+v %v", out, err)
	}
	if out.ResearchGraph || out.Disposition != DispositionPrepareFailed || !strings.Contains(out.Reason, "No Validate run is waiting") || a.binds != binds {
		t.Fatalf("an unclaimed control is refused: %+v binds=%d", out, a.binds)
	}
	after, err := a.f.db.GetGraphInstance(t.Context(), inst.ID)
	mustProgress(t, err)
	if after.Status != inst.Status || after.CurrentNode != inst.CurrentNode || a.visits(t, inst.ID, "owner_resolve") != resolved {
		t.Fatalf("the instance is unchanged: %+v was %+v", after, inst)
	}
	if wait, err := a.f.db.GetGraphOwnerWait(t.Context(), inst.ID); err != nil || wait.State != "waiting" || wait.ReplyID != 0 {
		t.Fatalf("the wait stays open: %+v %v", wait, err)
	}

	// Without any Validate instance the control is refused the same way.
	v := newValidateAdmissionFixture(t)
	v.allMerged(t)
	out, handled, err := v.admit(t, v.request(6202, validateOwnerID, body))
	if !handled || err != nil || !strings.Contains(out.Reason, "No Validate run is waiting") || v.binds != 0 || v.validateInstances(t) != 0 {
		t.Fatalf("no Validate run: handled=%v err=%v out=%+v", handled, err, out)
	}
}

// #602: an owner /validate on a newer commit ends a run on an older commit that
// only waits for the owner and starts a successor on the new commit.
func TestValidateNewHeadSupersedesARunWaitingForTheOwner(t *testing.T) {
	a := newAdoptFlow(t)
	old, _ := a.waitingValidate(t)
	a.f.opts.Validate.Bind = func(context.Context, Options, Request, store.ValidateEntry) (ValidateSuiteBindings, ValidateFlowBindings, error) {
		a.binds++
		return ValidateSuiteBindings{}, ValidateFlowBindings{}, errValidateStop
	}
	a.head = validateHeadB
	if out, handled, err := a.admit(t, a.request(6101, validateOwnerID, "/validate newer")); !handled || !errors.Is(err, errValidateStop) {
		t.Fatalf("handled=%v err=%v out=%+v", handled, err, out)
	}
	after, err := a.f.db.GetGraphInstance(t.Context(), old.ID)
	mustProgress(t, err)
	if after.Status != store.GraphCancelled {
		t.Fatalf("the waiting run is cancelled: %+v", after)
	}
	if wait, err := a.f.db.GetGraphOwnerWait(t.Context(), old.ID); err != nil || wait.State != "cancelled" {
		t.Fatalf("the owner wait is cancelled: %+v %v", wait, err)
	}
	trace, err := a.f.db.ListGraphTransitions(t.Context(), old.ID)
	mustProgress(t, err)
	reason := false
	for _, tr := range trace {
		reason = reason || tr.OnOutcome == "superseded by a newer commit"
	}
	if !reason {
		t.Fatalf("the trace names the reason: %+v", trace)
	}
	var fresh store.ValidateEntry
	for _, e := range a.entries(t) {
		if e.State == store.ValidateEntryAdmitted && e.Commit == validateHeadB {
			fresh = e
		}
	}
	if fresh.InstanceID == 0 || fresh.InstanceID == old.ID {
		t.Fatalf("a new run is admitted on the new commit: %+v", a.entries(t))
	}
	if pred, _, err := a.f.db.WorkflowPredecessor(t.Context(), fresh.InstanceID); err != nil || pred != old.ID {
		t.Fatalf("the successor links its predecessor: %d %v", pred, err)
	}
}

// #602: a run on an older commit that is executing still refuses a start on a
// newer commit and is left untouched.
func TestValidateNewHeadIsRefusedWhileTheOlderRunExecutes(t *testing.T) {
	a := newAdoptFlow(t)
	old, _ := a.waitingValidate(t)
	// The owner's reply was taken and its step is running.
	a.claimOwnerReply(t, old.ID, ValidateCheckCommand+" r story-001:AC-001 pass -- looked fine")
	wait, err := a.f.db.GetGraphOwnerWait(t.Context(), old.ID)
	mustProgress(t, err)
	cur, err := a.f.db.GetGraphInstance(t.Context(), old.ID)
	mustProgress(t, err)
	node := wait.OriginNode
	if node == "" {
		node = cur.CurrentNode
	}
	_, err = a.f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: old.ID, Node: node, Limit: 64, StartedAt: time.Now()})
	mustProgress(t, err)
	binds := a.binds
	a.head = validateHeadB
	out, handled, err := a.admit(t, a.request(6102, validateOwnerID, "/validate newer"))
	if !handled || err != nil || !strings.Contains(out.Reason, "still going") || a.binds != binds {
		t.Fatalf("handled=%v err=%v out=%+v binds=%d", handled, err, out, a.binds)
	}
	after, err := a.f.db.GetGraphInstance(t.Context(), old.ID)
	mustProgress(t, err)
	if after.Status != store.GraphRunning {
		t.Fatalf("the executing run is untouched: %+v", after)
	}
	if after, err := a.f.db.GetGraphOwnerWait(t.Context(), old.ID); err != nil || after.State != "allocated" {
		t.Fatalf("the owner wait is untouched: %+v %v", after, err)
	}
	for _, e := range a.entries(t) {
		if e.Commit == validateHeadB && e.InstanceID > 0 {
			t.Fatalf("no run is admitted on the new commit: %+v", e)
		}
	}
}

// #815: a real shared-driver repair run leaves its ID on the factory resolver.
// The next owner control must resume that resolver without starting an agent,
// even with other workflow instances on the same session.
func TestValidateImplementReplyAfterAgentWithMixedHistory(t *testing.T) {
	a := newAdoptFlow(t)
	ctx := t.Context()
	db := a.f.db
	session := a.f.reloadSession()
	binary, err := git.Resolve(ctx)
	mustProgress(t, err)
	clone, err := binary.Open(ctx, a.f.repo.Dir)
	mustProgress(t, err)
	tree, err := git.EnsureWorktree(ctx, clone, git.EnsureWorktreeOptions{Dir: a.f.worktreeDir(), Branch: session.Branch, BaseBranch: gittest.DefaultBranch})
	mustProgress(t, err)
	a.head, err = tree.Worktree.RevParse(ctx, "HEAD", git.RevParseOptions{})
	mustProgress(t, err)

	// Retain a Plan question alongside the already finished Implement instance.
	specs, err := PlanSpecs()
	mustProgress(t, err)
	planDefinition, err := capturedWorkflow(ctx, a.f.opts, a.request(5900, validateOwnerID, "/plan"), agent.WorkflowPlan, specs)
	mustProgress(t, err)
	files, err := json.Marshal(planDefinition.Files)
	mustProgress(t, err)
	topology, err := graphexec.SemanticDigest(planDefinition.Profile)
	mustProgress(t, err)
	def, err := db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: planDefinition.Profile.Team, Mode: "plan", ProfileVersion: planDefinition.Profile.ProfileVersion, Path: workflowGraphPath(planDefinition), Digest: planDefinition.Profile.Digest})
	mustProgress(t, err)
	_, err = db.AdmitPlanWorkflowPath(ctx, session.ID, "graph")
	mustProgress(t, err)
	admission, err := db.AdmitWorkflowLaunch(ctx, store.WorkflowLaunch{Mode: "plan", SessionID: session.ID, TriggerID: 5900, DefinitionID: def.ID, Start: planDefinition.Profile.Start, ContentDigest: planDefinition.Digest, TopologyDigest: topology, Files: files, Provenance: store.WorkflowProvenance(planDefinition.Origin)})
	mustProgress(t, err)
	plan := admission.Instance
	segment, err := db.GetOrCreateSegment(ctx, session.ID, 1, "plan", store.AgentClaude)
	mustProgress(t, err)
	planRun, err := db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: "plan", Agent: store.AgentClaude, TriggerCommentID: 5900, LogPath: func(int) string { return "plan.log" }})
	mustProgress(t, err)
	source, err := db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: plan.ID, LaunchID: admission.LaunchID, Node: planDefinition.Profile.Start, RunID: planRun.ID, Limit: 3, StartedAt: implementClock})
	mustProgress(t, err)
	mustProgress(t, db.CompleteGraphNodeAttempt(ctx, source.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "success", FinishedAt: implementClock}))
	mustProgress(t, db.RetainGraphComment(ctx, store.GraphCommentIntent{Key: "plan-wait", InstanceID: plan.ID, SourceAttemptID: source.ID, Node: "ask", Kind: "question", Candidate: []byte(`{"outcome":"incomplete"}`)}))
	_, err = db.EnsureReportDelivery(ctx, store.ReportDelivery{Key: "plan-wait", SessionID: session.ID, RunID: planRun.ID, TriggerID: 5900, Task: session.TaskRef, Destination: "acme/acme-api#123", TriggerNamespace: "issue-comment", Purpose: "plan-question", BotUserID: ownerBotID, Body: "Plan waiting", Marker: "plan-wait"})
	mustProgress(t, err)
	_, err = db.MarkReportDeliverySent(ctx, "plan-wait")
	mustProgress(t, err)
	_, err = db.ConfirmReportDelivery(ctx, "plan-wait", 5901)
	mustProgress(t, err)
	_, err = db.CompleteReportDelivery(ctx, "plan-wait")
	mustProgress(t, err)
	plan, err = db.GetGraphInstance(ctx, plan.ID)
	mustProgress(t, err)
	planWait, err := db.GetGraphOwnerWait(ctx, plan.ID)
	mustProgress(t, err)
	implement := mustImplementInstance(t, a.validateAdmissionFixture)

	// Add the third merged story to the existing two-story ledger.
	mustProgress(t, db.EnsureImplementStories(ctx, a.parent, 1, []store.ImplementStorySeed{{Key: "story-003", Position: 2, Branch: "lf/203-c"}}, implementClock))
	for _, step := range [][2]string{{store.ImplementWaiting, store.ImplementRunning}, {store.ImplementRunning, store.ImplementDone}, {store.ImplementDone, store.ImplementMerged}} {
		row, e := db.GetImplementStory(ctx, a.parent, 1, "story-003")
		mustProgress(t, e)
		tr := store.ImplementTransition{Parent: a.parent, Generation: 1, Key: row.Key, From: step[0], To: step[1], ExpectedRevision: row.Revision, At: implementClock}
		if step[1] == store.ImplementMerged {
			tr.MergeCommit = a.head
		}
		_, e = db.TransitionImplementStory(ctx, tr)
		mustProgress(t, e)
	}
	before, err := db.ListImplementStories(ctx, a.parent, 1)
	mustProgress(t, err)
	agentCalls := 0
	a.f.opts.Validate.Permissions = validateRepairResolver(t)
	a.f.opts.Runners = RunnerSet{agent.Kind(store.AgentClaude): lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		agentCalls++
		repair := handbackReport("The repair needs story implementation.")
		raw, e := json.Marshal(map[string]any{"result": agentresult.Result{Outcome: "completed", Comment: "Return the failed suite to Implement.", Payload: agentresult.Payload{Validate: &agentresult.Validate{Repair: &repair}}}})
		mustProgress(t, e)
		submitLiveResult(t, req, string(raw))
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "Handed back.", FinishedAt: time.Now()}, nil
	})}
	bind := a.f.opts.Validate.Bind
	moves := 0
	a.f.opts.Validate.Bind = func(ctx context.Context, opts Options, req Request, entry store.ValidateEntry) (ValidateSuiteBindings, ValidateFlowBindings, error) {
		suite, flow, e := bind(ctx, opts, req, entry)
		run := suite.Run
		suite.Run = func(ctx context.Context, dir string, command config.BugCheckCommand, started func(bugchecks.CommandResult) error) bugchecks.CommandResult {
			result := run(ctx, dir, command, started)
			if command.Argv[0] == "full-one" {
				code := 1
				result.Exit, result.Outcome, result.Output = &code, "failed", "TestTotal failed"
			}
			return result
		}
		flow.Worktree = func() (*git.Worktree, error) { return tree.Worktree, nil }
		flow.Stories = func(context.Context, Options, ActivityRequest) ([]ValidateStory, error) {
			var stories []ValidateStory
			for _, row := range before {
				stories = append(stories, ValidateStory{Key: row.Key, Title: row.Key, Revision: strings.Repeat("ab", 32), Criteria: []string{"AC-001"}})
			}
			return stories, nil
		}
		target := flow.Delivery.Target
		flow.Delivery.Target = func(ctx context.Context, opts Options, req ActivityRequest) (ValidateDeliveryTarget, error) {
			out, e := target(ctx, opts, req)
			out.Generation = 1
			out.Stories = []ValidateIssueRef{{Key: "story-001", Number: 201, ID: 7001}, {Key: "story-002", Number: 202, ID: 7002}, {Key: "story-003", Number: 203, ID: 7003}}
			return out, e
		}
		flow.AdvanceStory = func(context.Context, string, string) (bool, error) { moves++; return true, nil }
		return suite, flow, e
	}
	a.reviewers.run = func(req ValidateReviewRequest) (*agentresult.Result, error) {
		result := goodReview(req)
		for i := range result.Payload.Validate.Stories {
			story := &result.Payload.Validate.Stories[i]
			for j := range story.Criteria {
				story.Criteria[j].Status = "unmet"
				story.Criteria[j].Evidence = []agent.EvidenceExplanation{{Summary: story.Story + " needs its own repair.", CheckIDs: []string{}}}
			}
		}
		return result, nil
	}
	inst, revision := a.waitingValidate(t)
	wait, err := db.GetGraphOwnerWait(ctx, inst.ID)
	mustProgress(t, err)
	resolver, err := db.LatestGraphNodeAttempt(ctx, inst.ID, "owner_resolve")
	mustProgress(t, err)
	outcome, err := db.GetValidateOutcome(ctx, inst.ID, a.head)
	mustProgress(t, err)
	if agentCalls != 1 || resolver.RunID == 0 || resolver.ID != wait.SourceAttemptID || outcome.Outcome != "handed_back" {
		t.Fatalf("must reproduce inherited run after handback: calls=%d source=%+v wait=%+v outcome=%+v", agentCalls, resolver, wait, outcome)
	}
	body := ValidateImplementCommand + " " + revision
	comment := a.claimOwnerReply(t, inst.ID, body)
	out, err := Execute(ctx, a.f.opts, a.request(comment, validateOwnerID, body))
	mustProgress(t, err)
	if !out.ResearchGraph || out.Run.ID != 0 || agentCalls != 1 {
		t.Fatalf("control must run without an agent: %+v calls=%d", out, agentCalls)
	}
	latest, err := db.LatestWorkflowInstance(ctx, session.ID, "validate")
	mustProgress(t, err)
	wait, err = db.GetGraphOwnerWait(ctx, inst.ID)
	mustProgress(t, err)
	resolver, err = db.LatestGraphNodeAttempt(ctx, inst.ID, "owner_resolve")
	mustProgress(t, err)
	if latest.ID != inst.ID || latest.CurrentNode != "reimplementing" || latest.Status != store.GraphSucceeded || resolver.RunID != 0 || resolver.Outcome != "implement" || wait.ContinuationAttemptID != resolver.ID || wait.State != "allocated" {
		t.Fatalf("wrong continuation: %+v %+v %+v", latest, resolver, wait)
	}
	after, err := db.ListImplementStories(ctx, a.parent, 1)
	mustProgress(t, err)
	for i, row := range after {
		if row.State != store.ImplementWaiting || row.Generation != before[i].Generation || row.Branch != before[i].Branch || !strings.Contains(row.Guidance, row.Key+" needs its own repair.") {
			t.Errorf("story not reopened with its findings: %+v", row)
		}
		for _, other := range after {
			if other.Key != row.Key && strings.Contains(row.Guidance, other.Key) {
				t.Errorf("foreign findings in %s", row.Key)
			}
		}
	}
	if len(after) != 3 || moves != 3 {
		t.Fatalf("stories=%d moves=%d", len(after), moves)
	}
	_, err = Execute(ctx, a.f.opts, a.request(comment, validateOwnerID, body))
	mustProgress(t, err)
	replayed, err := db.ListImplementStories(ctx, a.parent, 1)
	mustProgress(t, err)
	count, err := db.CountPendingSchedules(ctx)
	mustProgress(t, err)
	status, found, err := db.ScheduleStatusBySource(ctx, session.ID, validateImplementWorkID(inst.ID, a.head))
	mustProgress(t, err)
	if !reflect.DeepEqual(after, replayed) || count != 1 || !found || status != store.SchedulePending || moves != 3 {
		t.Fatalf("replay changed handback: count=%d status=%s moves=%d", count, status, moves)
	}
	for _, prior := range []store.GraphInstance{plan, implement} {
		current, e := db.GetGraphInstance(ctx, prior.ID)
		mustProgress(t, e)
		if !reflect.DeepEqual(current, prior) {
			t.Errorf("other workflow changed: %+v -> %+v", prior, current)
		}
	}
	currentWait, err := db.GetGraphOwnerWait(ctx, plan.ID)
	mustProgress(t, err)
	if !reflect.DeepEqual(currentWait, planWait) {
		t.Fatalf("Plan wait changed: %+v", currentWait)
	}
}

// #1183: an owner /validate on the same commit ends a Validate instance that
// nothing can advance any more, settles its leftover prelaunch run and starts
// a fresh instance. While anything is live, or the entry gate is closed, the
// earlier instance stays and the owner hears that Validate already started.
func TestValidateSameCommitRestartEndsOnlyAnIdleInstance(t *testing.T) {
	for _, tc := range []string{"orphaned prelaunch", "pending dispatch", "live prelaunch", "running run", "closed gate"} {
		t.Run(tc, func(t *testing.T) {
			v := newValidateAdmissionFixture(t)
			v.allMerged(t)
			ctx := t.Context()
			if _, handled, err := v.admit(t, v.request(5001, validateOwnerID, "/validate first")); !handled || !errors.Is(err, errValidateStop) {
				t.Fatalf("handled=%v err=%v", handled, err)
			}
			first := v.entries(t)[0]
			segment, err := v.f.db.GetOrCreateSegment(ctx, v.f.session.ID, 1, "validate", store.AgentClaude)
			mustProgress(t, err)
			allocate := func(trigger int64, prelaunch bool) store.Run {
				t.Helper()
				run, err := v.f.db.AllocateRun(ctx, store.AllocateRunParams{SessionID: v.f.session.ID, SegmentID: segment.ID, Mode: "validate", Agent: store.AgentClaude, TriggerCommentID: trigger, StartedAt: implementClock, Prelaunch: prelaunch, LogPath: func(int) string { return "validate.log" }})
				mustProgress(t, err)
				return run
			}
			// The suite step was dispatched after a capacity wait.
			d, err := v.f.db.ParkGraphInstance(ctx, first.InstanceID, "suite", implementClock, implementClock)
			mustProgress(t, err)
			var orphan store.Run
			if tc != "pending dispatch" {
				if _, ok, err := v.f.db.ReserveGraphDispatch(ctx, d.ID, implementClock); err != nil || !ok {
					t.Fatalf("reserve: %v %v", ok, err)
				}
				orphan = allocate(d.TriggerID(), true)
				if tc != "live prelaunch" {
					mustProgress(t, v.f.db.FinishGraphDispatch(ctx, d.ID, implementClock))
				}
			}
			switch tc {
			case "running run":
				allocate(4999, false)
			case "closed gate":
				// Implement started again, so the entry gate is closed.
				prior := mustImplementInstance(t, v)
				_, err := v.f.db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: v.f.session.ID, DefinitionID: prior.DefinitionID, CurrentNode: prior.CurrentNode, StartedAt: implementClock})
				mustProgress(t, err)
			}
			out, handled, err := v.admit(t, v.request(5002, validateOwnerID, "/validate again"))
			old, ierr := v.f.db.GetGraphInstance(ctx, first.InstanceID)
			mustProgress(t, ierr)
			if tc == "orphaned prelaunch" {
				if !handled || !errors.Is(err, errValidateStop) || strings.Contains(out.Reason, "already been started") {
					t.Fatalf("handled=%v err=%v out=%+v", handled, err, out)
				}
				entries := v.entries(t)
				if len(entries) != 1 || entries[0].State != store.ValidateEntryAdmitted || entries[0].InstanceID == first.InstanceID || entries[0].CommentID != 5002 {
					t.Fatalf("a fresh instance on the reopened entry: %+v", entries)
				}
				fresh, err := v.f.db.GetGraphInstance(ctx, entries[0].InstanceID)
				mustProgress(t, err)
				if old.Status != store.GraphCancelled || fresh.Status != store.GraphRunning {
					t.Fatalf("old %s fresh %s", old.Status, fresh.Status)
				}
				run, err := v.f.db.GetRun(ctx, orphan.ID)
				mustProgress(t, err)
				if run.Status != store.RunFailed {
					t.Fatalf("the leftover prelaunch run is %s", run.Status)
				}
				return
			}
			if old.Status != store.GraphRunning {
				t.Fatalf("a live instance was ended: %s", old.Status)
			}
			if tc == "closed gate" {
				if !handled || err != nil || out.Disposition != DispositionPrepareFailed {
					t.Fatalf("handled=%v err=%v out=%+v", handled, err, out)
				}
				return
			}
			if !handled || err != nil || !strings.Contains(out.Reason, "Validate has already been started") {
				t.Fatalf("handled=%v err=%v reason=%q", handled, err, out.Reason)
			}
			if entries := v.entries(t); len(entries) != 1 || entries[0].InstanceID != first.InstanceID {
				t.Fatalf("the earlier instance stays: %+v", entries)
			}
		})
	}
}
