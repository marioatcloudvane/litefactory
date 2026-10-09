package turn

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
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
	v.parent = seedImplementParent(t, f,
		store.ManagedStory{Key: "story-001", IssueNumber: 201, IssueID: 7001},
		store.ManagedStory{Key: "story-002", IssueNumber: 202, IssueID: 7002})
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
