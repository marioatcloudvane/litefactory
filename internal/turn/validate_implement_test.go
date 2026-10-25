package turn

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

// #705: three merged stories, of which story-002 and story-003 have blocking
// findings. advanced records the story branches the step moved forward.
type implementBackFixture struct {
	*ownerFixture
	parent   string
	advanced []string
	advance  func(branch, commit string) (bool, error)
}

func newImplementBackFixture(t *testing.T) *implementBackFixture {
	t.Helper()
	return newImplementBackFixtureMerged(t, nil)
}

// newImplementBackFixtureMerged is newImplementBackFixture with the merge
// commit of each story chosen by merged; nil keeps the tested base commit.
func newImplementBackFixtureMerged(t *testing.T, merged func(o *ownerFixture, key string) string) *implementBackFixture {
	t.Helper()
	o := newOwnerFixture(t)
	f := &implementBackFixture{ownerFixture: o}
	o.stories = []ValidateStory{
		{Key: "story-001", Title: "One", Revision: strings.Repeat("ab", 32), Criteria: []string{"AC-001", "AC-002"}},
		{Key: "story-002", Title: "Two", Revision: strings.Repeat("cd", 32), Criteria: []string{"AC-001", "AC-002"}},
		{Key: "story-003", Title: "Three", Revision: strings.Repeat("ef", 32), Criteria: []string{"AC-001", "AC-002"}},
	}
	o.target.Generation = 1
	o.target.Stories = []ValidateIssueRef{{Number: 511, ID: 5110, Key: "story-001"}, {Number: 512, ID: 5120, Key: "story-002"}, {Number: 513, ID: 5130, Key: "story-003"}}
	o.reviewers.run = func(req ValidateReviewRequest) (*agentresult.Result, error) {
		res := goodReview(req)
		for si := range res.Payload.Validate.Stories {
			s := &res.Payload.Validate.Stories[si]
			if s.Story == "story-001" {
				continue
			}
			for i := range s.Criteria {
				if s.Criteria[i].Name == "AC-002" {
					s.Criteria[i].Status = "unmet"
					s.Criteria[i].Evidence = []agent.EvidenceExplanation{{Summary: "app/app.go:3 Total returns 9; " + s.Story + " AC-002 needs 10.", CheckIDs: []string{}}}
				}
			}
			for i := range s.Reviews {
				if s.Story == "story-003" && s.Reviews[i].Name == "code" {
					s.Reviews[i].Status, s.Reviews[i].Classification = "unmet", "blocking"
					s.Reviews[i].Evidence = []agent.EvidenceExplanation{{Summary: "app/app_test.go:5 the golden-output test is missing.", CheckIDs: []string{}}}
				}
			}
		}
		return res, nil
	}
	session, err := o.db.GetSession(t.Context(), o.instance.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	f.parent = session.TaskRef
	var seeds []store.ImplementStorySeed
	for i, s := range o.stories {
		seeds = append(seeds, store.ImplementStorySeed{Key: s.Key, Position: i, Branch: "lf/" + s.Key})
	}
	if err := o.db.EnsureImplementStories(t.Context(), f.parent, 1, seeds, o.clock); err != nil {
		t.Fatal(err)
	}
	for _, s := range o.stories {
		for _, step := range [][2]string{{store.ImplementWaiting, store.ImplementRunning}, {store.ImplementRunning, store.ImplementDone}, {store.ImplementDone, store.ImplementMerged}} {
			row := f.row(s.Key)
			in := store.ImplementTransition{Parent: f.parent, Generation: 1, Key: s.Key, From: step[0], To: step[1], ExpectedRevision: row.Revision, At: o.clock}
			if step[1] == store.ImplementMerged {
				in.MergeCommit = o.base
				if merged != nil {
					in.MergeCommit = merged(o, s.Key)
				}
			}
			if _, err := o.db.TransitionImplementStory(t.Context(), in); err != nil {
				t.Fatal(err)
			}
		}
	}
	f.advance = func(string, string) (bool, error) { return true, nil }
	o.b.AdvanceStory = func(_ context.Context, branch, commit string) (bool, error) {
		f.advanced = append(f.advanced, branch+"@"+commit)
		return f.advance(branch, commit)
	}
	o.registry.handlers[graphexec.ValidateImplementAction] = ValidateImplementActivity{o.b}
	return f
}

func (f *implementBackFixture) row(key string) store.ImplementStory {
	f.t.Helper()
	row, err := f.db.GetImplementStory(f.t.Context(), f.parent, 1, key)
	if err != nil {
		f.t.Fatal(err)
	}
	return row
}

func (f *implementBackFixture) rows() []store.ImplementStory {
	f.t.Helper()
	rows, err := f.db.ListImplementStories(f.t.Context(), f.parent, 1)
	if err != nil {
		f.t.Fatal(err)
	}
	return rows
}

func (f *implementBackFixture) schedules() int {
	f.t.Helper()
	n, err := f.db.CountPendingSchedules(f.t.Context())
	if err != nil {
		f.t.Fatal(err)
	}
	return n
}

// C2: /validate-implement reopens exactly the stories with blocking findings on
// the same generation, with their findings as guidance, and schedules the
// parent's Implement coordinator once; the run ends reimplementing.
func TestValidateImplementReopensTheStoriesWithBlockingFindings(t *testing.T) {
	f := newImplementBackFixture(t)
	f.toOwner()
	st := f.state()
	if !st.Reimplement || st.Reimplementing {
		t.Fatalf("the packaged graph declares the step and nothing is handed back yet: %+v", st)
	}
	report := f.forge.bodies("**Validate report**")
	if len(report) != 1 || !strings.Contains(report[0].Body, ValidateImplementCommand+" "+st.Revision) {
		t.Fatalf("the report offers /validate-implement with the revision:\n%s", report[0].Body)
	}
	nodes := f.say(f.command(ValidateImplementCommand))
	if !strings.HasSuffix(joinNodes(nodes), "owner_resolve>owner_implement>reimplementing") {
		t.Fatalf("path %s", joinNodes(nodes))
	}
	inst, err := f.db.GetGraphInstance(t.Context(), f.instance.ID)
	if err != nil || inst.Status != store.GraphSucceeded || inst.CurrentNode != "reimplementing" {
		t.Fatalf("the run ends reimplementing: %+v %v", inst, err)
	}
	if got := f.resolveOutcomes(); got[len(got)-1] != "implement" {
		t.Fatalf("resolve outcomes %v", got)
	}
	if one := f.row("story-001"); one.State != store.ImplementMerged || one.Guidance != "" {
		t.Fatalf("a story without blocking findings stays merged: %+v", one)
	}
	wants := map[string][]string{
		"story-002": {"finding: story-002:AC-002", "failed criterion: AC-002", "app/app.go:3 Total returns 9; story-002 AC-002 needs 10.", st.Commit, "untrusted data"},
		"story-003": {"finding: story-003:AC-002", "finding: story-003:code", "app/app_test.go:5 the golden-output test is missing.", "classification: blocking"},
	}
	for key, want := range wants {
		row := f.row(key)
		if row.State != store.ImplementWaiting || row.Generation != 1 || row.Attempts != 0 || row.MergeCommit != "" || row.WorkerSession != 0 || row.Branch != "lf/"+key {
			t.Fatalf("%s is reopened on the same generation and branch: %+v", key, row)
		}
		for _, w := range want {
			if !strings.Contains(row.Guidance, w) {
				t.Errorf("%s guidance lacks %q:\n%s", key, w, row.Guidance)
			}
		}
		if strings.Contains(row.Guidance, "story-001") {
			t.Errorf("%s guidance carries another story's findings", key)
		}
	}
	if strings.Join(f.advanced, ",") != "lf/story-002@"+st.Commit+",lf/story-003@"+st.Commit {
		t.Fatalf("each reopened story branch is moved to the tested commit: %v", f.advanced)
	}
	session, err := f.db.GetSession(t.Context(), f.instance.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	status, found, err := f.db.ScheduleStatusBySource(t.Context(), session.ID, validateImplementWorkID(f.instance.ID, st.Commit))
	if err != nil || !found || status != store.SchedulePending || f.schedules() != 1 {
		t.Fatalf("one Implement start is scheduled on the parent: %v %v %v %d", status, found, err, f.schedules())
	}
	if c := ParseCommand(ValidateImplementPayload); c.Kind != KindSpike || c.Name != "/implement" {
		t.Fatalf("the scheduled start is an /implement command: %+v", c)
	}
	after := f.state()
	if !after.Reimplementing || !strings.Contains(after.NextStep(nil), "sent back to Implement") {
		t.Fatalf("the report says the findings went back: %q", after.NextStep(nil))
	}
	body := f.forge.bodies("**Validate report**")[0].Body
	if !strings.Contains(body, "Sent back to Implement:") || !strings.Contains(body, "Reopened story-002, story-003.") {
		t.Fatalf("the report shows the handback:\n%s", body)
	}
}

// C4: replaying the step neither reopens a story twice nor starts a second
// coordinator.
func TestValidateImplementReplayDoesNothingTwice(t *testing.T) {
	f := newImplementBackFixture(t)
	f.toOwner()
	f.reply(f.owner, f.command(ValidateImplementCommand))
	for range 4 {
		if inst, _ := f.db.GetGraphInstance(t.Context(), f.instance.ID); inst.CurrentNode == "owner_implement" {
			break
		}
		if res, err := f.step(); err != nil {
			t.Fatalf("%v (%s)", err, res.Activity.Diagnostic)
		}
	}
	attempt, err := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: f.instance.ID, Node: "owner_implement", Limit: 4, StartedAt: f.clock})
	if err != nil {
		t.Fatal(err)
	}
	req := ActivityRequest{Mode: agent.ModeValidate, Node: "owner_implement", InstanceID: f.instance.ID, NodeAttemptID: attempt.ID}
	first, err := ValidateImplementActivity{f.b}.Execute(t.Context(), f.opts, req)
	if err != nil || first.Outcome != "started" {
		t.Fatalf("first pass: %+v %v", first, err)
	}
	before := f.rows()
	second, err := ValidateImplementActivity{f.b}.Execute(t.Context(), f.opts, req)
	if err != nil || second.Outcome != "started" {
		t.Fatalf("replay: %+v %v", second, err)
	}
	var out validateImplementOutput
	if err := json.Unmarshal(second.Output, &out); err != nil || !out.Replayed {
		t.Fatalf("the replay finds its schedule: %s %v", second.Output, err)
	}
	for i, row := range f.rows() {
		if row.Revision != before[i].Revision || row.State != before[i].State {
			t.Fatalf("a replay moved %s: %+v", row.Key, row)
		}
	}
	if f.schedules() != 1 || len(f.advanced) != 2 {
		t.Fatalf("one coordinator start and one move per story: schedules=%d moves=%v", f.schedules(), f.advanced)
	}
}

// A story whose branch cannot be moved to the tested commit stays merged and
// is named; the others are still reopened.
func TestValidateImplementLeavesAStoryWhoseBranchCannotMove(t *testing.T) {
	f := newImplementBackFixture(t)
	f.advance = func(branch, _ string) (bool, error) { return branch != "lf/story-003", nil }
	f.toOwner()
	f.say(f.command(ValidateImplementCommand))
	if f.row("story-002").State != store.ImplementWaiting || f.row("story-003").State != store.ImplementMerged {
		t.Fatalf("only story-002 is reopened: %+v", f.rows())
	}
	if f.schedules() != 1 || !strings.Contains(f.notice(), "Not reopened: story-003, because its branch cannot be moved forward to the tested commit.") {
		t.Fatalf("notice %q schedules %d", f.notice(), f.schedules())
	}
}

// C3: a reply from someone other than the owner, a stale revision, a moved
// head and a run without blocking findings are refused with a notice, and
// neither the ledger nor the schedules change.
func TestValidateImplementRefusalsChangeNothing(t *testing.T) {
	cases := map[string]struct {
		clean  bool
		author int64
		body   func(f *implementBackFixture) string
		move   bool
		want   string
	}{
		"not the owner":      {author: ownerTestID + 5, want: "Only the owner"},
		"a stale revision":   {body: func(*implementBackFixture) string { return ValidateImplementCommand + " 000000000000" }, want: "current report revision"},
		"a moved head":       {move: true, want: "branch head is now"},
		"no blocking":        {clean: true, want: "Implement has nothing to repair"},
		"an extra argument":  {body: func(f *implementBackFixture) string { return f.command(ValidateImplementCommand, "story-002") }, want: "takes no argument"},
		"already handed off": {want: "already sent back"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newImplementBackFixture(t)
			if c.clean {
				f.reviewers.run = nil
			}
			f.toOwner()
			body := f.command(ValidateImplementCommand)
			if c.body != nil {
				body = c.body(f)
			}
			if name == "already handed off" {
				// The schedule of this commit already exists.
				session, _ := f.db.GetSession(t.Context(), f.instance.SessionID)
				if _, _, err := f.db.CreateOrGetSchedule(t.Context(), store.CreateScheduleParams{SessionID: session.ID, SourceCommentID: validateImplementWorkID(f.instance.ID, f.state().Commit), OwnerHandle: "owner", Payload: ValidateImplementPayload, DueAt: f.clock}); err != nil {
					t.Fatal(err)
				}
				body = f.command(ValidateImplementCommand)
			}
			before, schedules := f.rows(), f.schedules()
			author := f.owner
			if c.author != 0 {
				author = c.author
			}
			f.reply(author, body)
			if c.move {
				f.write("app/extra.go", "package app\n")
				if _, err := f.wt.Commit(t.Context(), gitCommitOptions()); err != nil {
					t.Fatal(err)
				}
			}
			nodes := f.toOwner()
			if visits(nodes, "owner_implement") != 0 {
				t.Fatalf("a refused command reached the implement step: %s", joinNodes(nodes))
			}
			if !strings.Contains(f.notice(), c.want) {
				t.Fatalf("notice %q does not name %q", f.notice(), c.want)
			}
			for i, row := range f.rows() {
				if row.Revision != before[i].Revision || row.State != store.ImplementMerged {
					t.Fatalf("a refused command moved %s: %+v", row.Key, row)
				}
			}
			if f.schedules() != schedules || len(f.advanced) != 0 {
				t.Fatalf("a refused command scheduled or pushed: %d %v", f.schedules(), f.advanced)
			}
		})
	}
}

// The command is parsed exactly.
func TestParseValidateImplementCommand(t *testing.T) {
	c, err := ParseValidateCommand("/validate-implement 0123456789ab")
	if err != nil || c.Name != ValidateImplementCommand || c.Revision != "0123456789ab" {
		t.Fatalf("%+v %v", c, err)
	}
	if c.Guidance != "" {
		t.Fatalf("no guidance without ` -- `: %+v", c)
	}
	c, err = ParseValidateCommand("/validate-implement 0123456789ab -- keep the tooltips, no legend")
	if err != nil || c.Name != ValidateImplementCommand || c.Guidance != "keep the tooltips, no legend" {
		t.Fatalf("guidance: %+v %v", c, err)
	}
	for _, bad := range []string{"/validate-implement 0123456789ab story-002", "/validate-implement 0123456789ab story-002 -- why", "/validate-implement 0123456789ab -- "} {
		if _, err := ParseValidateCommand(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if ParseCommand("/validate-implement 0123456789ab").Kind != KindValidateControl {
		t.Fatal("the daemon routes it to the Validate run")
	}
}

// C5: a story the step reopened restarts through the existing worker with
// its findings as owner guidance, and a completed answer is done again.
func TestValidateImplementReopenedStoryWorkerGetsTheFindings(t *testing.T) {
	var prompt string
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		prompt = req.Prompt
		submitLiveResult(t, req, implementCompletedJSON)
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "done", FinishedAt: time.Now()}, nil
	})
	f, _, req := assembledImplementFixture(t, runner)
	seedImplementStory(t, f)
	ctx := t.Context()
	move := func(from, to string, extra func(*store.ImplementTransition)) {
		t.Helper()
		row := implementWorkerRow(t, f)
		in := store.ImplementTransition{Parent: implementTaskParent, Generation: 1, Key: "story-001", From: from, To: to, ExpectedRevision: row.Revision, At: implementClock}
		if extra != nil {
			extra(&in)
		}
		if _, err := f.db.TransitionImplementStory(ctx, in); err != nil {
			t.Fatal(err)
		}
	}
	move(store.ImplementRunning, store.ImplementDone, nil)
	move(store.ImplementDone, store.ImplementMerged, func(in *store.ImplementTransition) { in.MergeCommit = strings.Repeat("1", 40) })
	guidance := "Validate found blocking problems in story story-001.\n- finding: story-001:AC-002\n  reviewer text:\nhealth.go:1 the handler returns 500."
	move(store.ImplementMerged, store.ImplementWaiting, func(in *store.ImplementTransition) { in.Guidance = guidance })
	// ready starts the waiting story; the daemon gives it its worker session.
	move(store.ImplementWaiting, store.ImplementRunning, nil)
	mustProgress(t, f.db.SetImplementWorkerSession(ctx, implementTaskParent, 1, "story-001", f.session.ID, implementClock))
	out, err := Execute(ctx, f.opts, req)
	if err != nil || !out.Succeeded() {
		t.Fatalf("%+v %v", out, err)
	}
	if !strings.Contains(prompt, `"owner_guidance"`) || !strings.Contains(prompt, "health.go:1 the handler returns 500.") || !strings.Contains(prompt, `"attempt":1`) {
		t.Fatalf("the worker is given the findings as owner guidance:\n%s", prompt[max(0, len(prompt)-1500):])
	}
	if row := implementWorkerRow(t, f); row.State != store.ImplementDone {
		t.Fatalf("the reopened story is done again and waits for its merge: %+v", row)
	}
}

// C5: the story branch is moved forward to the tested commit with the guarded
// push, so the reopened worker builds on a branch that holds it. A branch that
// holds other commits, or none at all, is left alone.
func TestValidateAdvanceStoryFastForwardsOnly(t *testing.T) {
	f := newFlowFixture(t)
	ctx := t.Context()
	f.repo.Git("push", "--quiet", "origin", f.base+":refs/heads/lf/story-002")
	f.repo.Git("checkout", "--quiet", "-b", "lf/story-009", f.base)
	f.repo.WriteFile("app/other.go", "package app\n")
	diverged := f.repo.Commit("another line of work")
	f.repo.Git("push", "--quiet", "origin", "lf/story-009")
	f.repo.Git("checkout", "--quiet", gittest.DefaultBranch)
	f.write("app/app.go", "package app\n\nfunc Total() int { return 10 }\n")
	tested, err := f.wt.Commit(ctx, gitCommitOptions())
	if err != nil {
		t.Fatal(err)
	}
	binary, err := git.Resolve(ctx)
	if err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	clone, err := binary.Open(ctx, f.repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	session, err := f.db.GetSession(ctx, f.instance.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	h := &validateHost{opts: Options{DB: f.db, CloneGate: newTestGate()}, entry: store.ValidateEntry{InstanceID: f.instance.ID, Commit: tested.Head}, session: session, repo: store.Repo{Path: f.repo.Dir}, clone: clone, worktree: f.wt}
	remote := func(branch string) string {
		out := f.repo.Git("ls-remote", "origin", "refs/heads/"+branch)
		if fields := strings.Fields(out); len(fields) > 0 {
			return fields[0]
		}
		return ""
	}
	if moved, err := h.advanceStory(ctx, "lf/story-002", tested.Head); err != nil || !moved || remote("lf/story-002") != tested.Head {
		t.Fatalf("the story branch is fast-forwarded to the tested commit: %v %v %s", moved, err, remote("lf/story-002"))
	}
	if moved, err := h.advanceStory(ctx, "lf/story-002", tested.Head); err != nil || !moved {
		t.Fatalf("a branch already at the tested commit is done: %v %v", moved, err)
	}
	if moved, err := h.advanceStory(ctx, "lf/story-009", tested.Head); err != nil || moved || remote("lf/story-009") != diverged {
		t.Fatalf("a diverged branch is left alone: %v %v %s", moved, err, remote("lf/story-009"))
	}
	if moved, err := h.advanceStory(ctx, "lf/story-404", tested.Head); err != nil || moved || remote("lf/story-404") != "" {
		t.Fatalf("a missing branch is not created: %v %v", moved, err)
	}
}

// #942 C5, C6: the owner's guidance after ` -- ` reaches every reopened
// story's worker, labelled as the owner's, alongside the findings; a replay
// of the step reopens nothing twice.
func TestValidateImplementCarriesTheOwnerGuidance(t *testing.T) {
	f := newImplementBackFixture(t)
	f.toOwner()
	const guidance = "keep the tooltips, no legend"
	f.reply(f.owner, f.command(ValidateImplementCommand, "--", guidance))
	for range 4 {
		if inst, _ := f.db.GetGraphInstance(t.Context(), f.instance.ID); inst.CurrentNode == "owner_implement" {
			break
		}
		if res, err := f.step(); err != nil {
			t.Fatalf("%v (%s)", err, res.Activity.Diagnostic)
		}
	}
	attempt, err := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: f.instance.ID, Node: "owner_implement", Limit: 4, StartedAt: f.clock})
	if err != nil {
		t.Fatal(err)
	}
	req := ActivityRequest{Mode: agent.ModeValidate, Node: "owner_implement", InstanceID: f.instance.ID, NodeAttemptID: attempt.ID}
	if res, err := (ValidateImplementActivity{f.b}).Execute(t.Context(), f.opts, req); err != nil || res.Outcome != "started" {
		t.Fatalf("first pass: %+v %v", res, err)
	}
	for _, key := range []string{"story-002", "story-003"} {
		row := f.row(key)
		if row.State != store.ImplementWaiting || !strings.Contains(row.Guidance, "The owner's guidance for this repair (it overrides the approved design where they conflict):\n"+guidance) || !strings.Contains(row.Guidance, "finding: "+key+":AC-002") {
			t.Fatalf("%s guidance:\n%s", key, row.Guidance)
		}
		if strings.Index(row.Guidance, guidance) > strings.Index(row.Guidance, "- finding:") {
			t.Fatalf("%s: the owner's guidance comes before the findings:\n%s", key, row.Guidance)
		}
	}
	before := f.rows()
	second, err := ValidateImplementActivity{f.b}.Execute(t.Context(), f.opts, req)
	var out validateImplementOutput
	if err != nil || second.Outcome != "started" || json.Unmarshal(second.Output, &out) != nil || !out.Replayed {
		t.Fatalf("replay: %+v %v", second, err)
	}
	for i, row := range f.rows() {
		if row.Revision != before[i].Revision || row.Guidance != before[i].Guidance {
			t.Fatalf("a replay moved %s: %+v", row.Key, row)
		}
	}
	if f.schedules() != 1 || len(f.advanced) != 2 {
		t.Fatalf("schedules=%d moves=%v", f.schedules(), f.advanced)
	}
}

// #974: a suite-only handback. The reviewers are clean, the suite fails and
// the repair round hands back; mergeOf picks each story's merge commit.
func newSuiteHandbackFixture(t *testing.T, mergeOf func(o *ownerFixture, root, key string) string) *implementBackFixture {
	t.Helper()
	f := newImplementBackFixtureMerged(t, func(o *ownerFixture, key string) string {
		root := strings.TrimSpace(o.repo.Git("rev-list", "--max-parents=0", "HEAD"))
		return mergeOf(o, root, key)
	})
	f.reviewers.run = func(req ValidateReviewRequest) (*agentresult.Result, error) {
		// The reviewers find nothing: every criterion is satisfied by reading.
		res := goodReview(req)
		for si := range res.Payload.Validate.Stories {
			s := &res.Payload.Validate.Stories[si]
			for i := range s.Criteria {
				s.Criteria[i].Status = "satisfied"
				s.Criteria[i].Evidence = []agent.EvidenceExplanation{{Summary: "app/app.go:3 implements " + s.Criteria[i].Name + ".", CheckIDs: []string{}}}
			}
		}
		return res, nil
	}
	f.suiteExit = func(string) int { return 1 }
	f.agent = func(int, *flowFixture) (agentresult.ValidateRepair, ActivityResult) {
		return handbackReport("the sum is wrong"), ActivityResult{}
	}
	f.toOwner()
	st := f.state()
	if targets := st.implementTargets(); st.Handback == nil || len(st.Handback.Failures) == 0 || len(targets) != 1 || len(targets[0].Findings) != 0 {
		t.Fatalf("the tested commit is handed back with only a failed suite: %+v %+v", st.Handback, targets)
	}
	return f
}

// #974 BUG-974-1/2/3/5: when only the suite failed and Validate handed it back,
// /validate-implement is accepted and reopens exactly one story, the last
// assessed one when no story's merge changed a failing location, with the
// suite failure and the owner's guidance; Implement is scheduled once.
func TestValidateImplementAcceptsAHandedBackFailedSuite(t *testing.T) {
	f := newSuiteHandbackFixture(t, func(_ *ownerFixture, root, _ string) string { return root })
	st := f.state()
	if next := st.implementLine(); !strings.Contains(next, ValidateImplementCommand+" "+st.Revision) {
		t.Fatalf("the failed-suite Next line names /validate-implement: %q", next)
	}
	before := f.rows()
	const guidance = "skip the node tests when node is missing"
	nodes := f.say(f.command(ValidateImplementCommand, "--", guidance))
	if !strings.HasSuffix(joinNodes(nodes), "owner_resolve>owner_implement>reimplementing") {
		t.Fatalf("path %s (notice %q)", joinNodes(nodes), f.notice())
	}
	if got := f.resolveOutcomes(); got[len(got)-1] != "implement" {
		t.Fatalf("resolve outcomes %v", got)
	}
	row := f.row("story-003")
	if row.State != store.ImplementWaiting || row.Generation != 1 || row.Branch != "lf/story-003" {
		t.Fatalf("the last assessed story is reopened on the same generation and branch: %+v", row)
	}
	for _, w := range []string{"The full suite still fails on commit", "command: full-one", "objective: Change Total so it returns 10.", guidance} {
		if !strings.Contains(row.Guidance, w) {
			t.Errorf("guidance lacks %q:\n%s", w, row.Guidance)
		}
	}
	for i, r := range f.rows() {
		if r.Key != "story-003" && (r.State != store.ImplementMerged || r.Revision != before[i].Revision) {
			t.Fatalf("only one story is reopened: %+v", r)
		}
	}
	if strings.Join(f.advanced, ",") != "lf/story-003@"+st.Commit || f.schedules() != 1 {
		t.Fatalf("one move and one Implement start: %v %d", f.advanced, f.schedules())
	}
}

// #974 BUG-974-6: a failing location in a file that an earlier story's merge
// changed reopens that story only.
func TestValidateImplementFailedSuiteReopensTheStoryThatChangedTheFailingFile(t *testing.T) {
	f := newSuiteHandbackFixture(t, func(o *ownerFixture, root, key string) string {
		if key == "story-001" {
			return o.base // adds app/app.go, the failing location
		}
		return root
	})
	st := f.state()
	f.say(f.command(ValidateImplementCommand))
	if row := f.row("story-001"); row.State != store.ImplementWaiting || !strings.Contains(row.Guidance, "The full suite still fails on commit") {
		t.Fatalf("story-001 is reopened with the suite failure: %+v", row)
	}
	for _, key := range []string{"story-002", "story-003"} {
		if row := f.row(key); row.State != store.ImplementMerged {
			t.Fatalf("%s stays merged: %+v", key, row)
		}
	}
	if strings.Join(f.advanced, ",") != "lf/story-001@"+st.Commit || f.schedules() != 1 {
		t.Fatalf("only story-001 is moved: %v %d", f.advanced, f.schedules())
	}
}
