package turn

import (
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
)

// mixedReview satisfies AC-001, leaves AC-002 unverified and reports the code
// review as an unmet optional (advisory) suggestion.
func mixedReview(req ValidateReviewRequest) (*agentresult.Result, error) {
	res := goodReview(req)
	for si := range res.Payload.Validate.Stories {
		s := &res.Payload.Validate.Stories[si]
		for i := range s.Criteria {
			if s.Criteria[i].Name == "AC-002" {
				s.Criteria[i].Status = "unverified"
				s.Criteria[i].Evidence = []agent.EvidenceExplanation{{Summary: "Open the page and look.", CheckIDs: []string{}}}
			}
		}
		for i := range s.Reviews {
			if s.Reviews[i].Name == "code" {
				s.Reviews[i].Status = "unmet"
			}
		}
	}
	return res, nil
}

func (o *ownerFixture) mixed() {
	o.reviewers.run = mixedReview
	o.toOwner()
}

func TestOwnerReportIsPostedOnceEditedInPlaceAndSkipsAnUnchangedDigest(t *testing.T) {
	o := newOwnerFixture(t)
	o.mixed()
	if n := len(o.forge.bodies("**Validate report**")); n != 1 {
		t.Fatalf("one report comment: %d", n)
	}
	first := o.forge.edits
	rev := o.state().Revision
	// A refreshed, unchanged report is not edited.
	if changed, err := o.b.RefreshValidateReport(t.Context(), o.opts, ActivityRequest{InstanceID: o.instance.ID}); err != nil || changed {
		t.Fatalf("unchanged digest must skip: %v %v", changed, err)
	}
	o.say(o.command(ValidateCheckCommand, "story-002:AC-002", "pass", "-- opened it, fine"))
	if n := len(o.forge.bodies("**Validate report**")); n != 1 || o.forge.edits <= first {
		t.Fatalf("the report is edited in place: comments %d edits %d", n, o.forge.edits)
	}
	if o.state().Revision == rev || !strings.Contains(o.forge.bodies("**Validate report**")[0].Body, o.state().Revision) {
		t.Fatal("the edited report shows the new revision")
	}
}

func TestOwnerFailedReportEditDoesNotFailTheStep(t *testing.T) {
	o := newOwnerFixture(t)
	o.mixed()
	o.forge.editErr = errors.New("forge down")
	o.say(o.command(ValidateCheckCommand, "story-002:AC-002", "pass", "-- ok"))
	if o.forge.edits != 0 {
		t.Fatal("the edit failed")
	}
	o.forge.editErr = nil
	o.say(o.command(ValidateFindingCommand, "story-002:code", "defer"))
	if o.forge.edits == 0 {
		t.Fatal("the next refresh catches up")
	}
}

func TestOwnerRefusesWhatItShouldAndChangesNothing(t *testing.T) {
	o := newOwnerFixture(t)
	o.mixed()
	rev := o.state().Revision
	cases := map[string]struct{ body, want string }{
		"malformed":                      {"/validate-check nope", "revision"},
		"stale revision":                 {"/validate-push 000000000000", "revision"},
		"unknown finding":                {o.command(ValidateFindingCommand, "story-002:AC-009", "repair"), "not a finding"},
		"check on a satisfied criterion": {o.command(ValidateCheckCommand, "story-002:AC-001", "pass", "-- x"), "does not await"},
		"push while open":                {o.command(ValidatePushCommand), "not offered"},
		"merge before push":              {o.command(ValidateMergeCommand, "1", strings.Repeat("a", 40)), "not pushed"},
		"close before merge":             {o.command(ValidateCloseCommand, "497"), "not merged"},
	}
	for name, c := range cases {
		o.say(c.body)
		if got := o.notice(); !strings.Contains(strings.ToLower(got), strings.ToLower(c.want)) && !strings.Contains(got, "awaits") && !strings.Contains(got, "not a criterion") {
			t.Errorf("%s: notice %q", name, got)
		}
		if o.state().Revision != rev {
			t.Fatalf("%s changed the records", name)
		}
	}
	// The current command is shown again after a refusal.
	asks := o.forge.bodies("Your last command changed nothing")
	if len(asks) == 0 || !strings.Contains(asks[len(asks)-1].Body, rev) {
		t.Fatal("a refusal shows the current command")
	}
	if len(o.forge.pushes)+len(o.forge.merges)+len(o.forge.issueCreates)+len(o.forge.prCreates) != 0 {
		t.Fatal("a refused command acts on nothing")
	}
}

func TestOwnerNonOwnerReplyChangesNothing(t *testing.T) {
	o := newOwnerFixture(t)
	o.mixed()
	// The store refuses to claim a non-owner reply at all.
	wait, _ := o.db.GetGraphOwnerWait(t.Context(), o.instance.ID)
	delivery, _ := o.db.GetReportDelivery(t.Context(), wait.DeliveryKey)
	session, _ := o.db.GetSession(t.Context(), o.instance.SessionID)
	_, err := o.db.ClaimGraphOwnerReply(t.Context(), store.GraphOwnerReply{InstanceID: o.instance.ID, OwnerID: o.owner, AuthorID: o.owner + 1, CommentID: delivery.CommentID + 5, QuestionCommentID: delivery.CommentID, TaskRef: session.TaskRef, SourceRef: session.TaskRef, Mode: "validate", Workflow: "validate", DeliveryKey: wait.DeliveryKey, Body: o.command(ValidateCheckCommand, "story-002:AC-002", "pass", "-- x"), At: o.clock})
	if err == nil {
		t.Fatal("a non-owner reply must not be claimed")
	}
	// And the resolve step checks the reply author against the resolved owner.
	o.reply(o.owner+5, o.command(ValidateCheckCommand, "story-002:AC-002", "pass", "-- x"))
	o.toOwner()
	if !strings.Contains(o.notice(), "Only the owner") || len(o.state().Open) == 0 {
		t.Fatalf("notice %q", o.notice())
	}
}

func TestOwnerManualVerdictsFindingChoicesAndBlockingRules(t *testing.T) {
	o := newOwnerFixture(t)
	o.mixed()
	st := o.state()
	f, _ := st.finding("story-002:AC-002")
	if st.Ready || f.State != findingAwaitingCheck || !strings.Contains(o.forge.bodies("**Validate report**")[0].Body, "awaiting owner check") {
		t.Fatalf("awaiting: %+v", f)
	}
	// A fail becomes a finding the owner can repair.
	o.say(o.command(ValidateCheckCommand, "story-002:AC-002", "fail", "-- the page is broken"))
	if f, _ = o.state().finding("story-002:AC-002"); f.State != findingOwnerFailed {
		t.Fatalf("%+v", f)
	}
	// A newer verdict replaces the older with history kept.
	o.say(o.command(ValidateCheckCommand, "story-002:AC-002", "pass", "-- fixed in the meantime"))
	all, _ := o.db.ListValidateVerdicts(t.Context(), o.instance.ID, o.state().Commit, true)
	if f, _ = o.state().finding("story-002:AC-002"); f.State != findingOwnerPassed || len(all) != 2 {
		t.Fatalf("%+v history %d", f, len(all))
	}
	// An optional finding needs a choice; defer clears it.
	if st = o.state(); st.Ready || len(st.Open) != 1 || !strings.Contains(st.Open[0], "story-002:code") {
		t.Fatalf("%v", st.Open)
	}
	o.say(o.command(ValidateFindingCommand, "story-002:code", "repair"))
	if !o.state().Ready {
		t.Fatal("an optional finding with a repair choice is handled")
	}
	o.say(o.command(ValidateFindingCommand, "story-002:code", "defer"))
	if !o.state().Ready {
		t.Fatal("the choice is replaceable")
	}
}

func TestOwnerBlockingFindingNeverClearsAndRefusesDefer(t *testing.T) {
	o := newOwnerFixture(t)
	o.reviewers.run = func(req ValidateReviewRequest) (*agentresult.Result, error) {
		res, _ := mixedReview(req)
		for si := range res.Payload.Validate.Stories {
			for i := range res.Payload.Validate.Stories[si].Criteria {
				if res.Payload.Validate.Stories[si].Criteria[i].Name == "AC-002" {
					res.Payload.Validate.Stories[si].Criteria[i].Status = "unmet"
				}
			}
		}
		return res, nil
	}
	o.toOwner()
	o.say(o.command(ValidateFindingCommand, "story-002:AC-002", "defer"))
	if !strings.Contains(o.notice(), "Defer is allowed only") {
		t.Fatalf("%q", o.notice())
	}
	o.say(o.command(ValidateFindingCommand, "story-002:AC-002", "repair"))
	o.say(o.command(ValidateFindingCommand, "story-002:AC-002", "issue", "bug"))
	st := o.state()
	if st.Ready || !strings.Contains(strings.Join(st.Open, " "), "Blocking finding `story-002:AC-002`") {
		t.Fatalf("repair and issue never clear a blocking finding: %v", st.Open)
	}
	o.say(o.command(ValidatePushCommand))
	if len(o.forge.pushes) != 0 {
		t.Fatal("no push while a blocking finding is open")
	}
}

func TestOwnerIssueIsCreatedOnceRecoveredByMarkerAndRetried(t *testing.T) {
	o := newOwnerFixture(t)
	o.mixed()
	o.say(o.command(ValidateCheckCommand, "story-002:AC-002", "pass", "-- ok"))
	// A lost reply: the issue exists but the call reported failure.
	o.forge.createIssueLostReply = true
	o.say(o.command(ValidateFindingCommand, "story-002:code", "issue", "story"))
	if len(o.forge.issueCreates) != 1 || len(o.forge.issues) != 1 {
		t.Fatalf("created %d", len(o.forge.issueCreates))
	}
	e := o.effect(store.EffectIssue, "story-002:code")
	if e.State != store.EffectSucceeded || decodeEffectResult(&e).Issue != 900 || !strings.Contains(o.forge.issues[0].Body, ValidateIssueMarker(o.instance.ID, "story-002:code")) {
		t.Fatalf("%+v", e)
	}
	// Replaying the step creates no second issue.
	st := o.state()
	f, _ := st.finding("story-002:code")
	r := &validateRemote{b: o.b, opts: o.opts, req: ActivityRequest{InstanceID: o.instance.ID}, st: st, target: o.target}
	if err := r.createIssue(t.Context(), e, f); err != nil || len(o.forge.issues) != 1 || len(o.forge.issueCreates) != 1 {
		t.Fatalf("replay: %v %d", err, len(o.forge.issues))
	}
	if !o.state().Ready {
		t.Fatal("a succeeded issue clears the pending work of an optional finding")
	}
}

func TestOwnerIssueFailureKeepsValidateOpenAndRepeatingRetries(t *testing.T) {
	o := newOwnerFixture(t)
	o.mixed()
	o.say(o.command(ValidateCheckCommand, "story-002:AC-002", "pass", "-- ok"))
	o.forge.createIssueErr = errors.New("forge refused")
	o.say(o.command(ValidateFindingCommand, "story-002:code", "issue", "bug"))
	if e := o.effect(store.EffectIssue, "story-002:code"); e.State != store.EffectFailed {
		t.Fatalf("%+v", e)
	}
	st := o.state()
	if st.Ready || !strings.Contains(strings.Join(st.Open, " "), "failed") || !strings.Contains(o.forge.bodies("**Validate report**")[0].Body, "issue for story-002:code: failed") {
		t.Fatalf("%v", st.Open)
	}
	o.forge.createIssueErr = nil
	o.say(o.command(ValidateFindingCommand, "story-002:code", "issue", "bug"))
	if e := o.effect(store.EffectIssue, "story-002:code"); e.State != store.EffectSucceeded || len(o.forge.issues) != 1 || !o.state().Ready {
		t.Fatalf("%+v", e)
	}
	// An uncertain lookup keeps it open too.
	o2 := newOwnerFixture(t)
	o2.mixed()
	o2.say(o2.command(ValidateCheckCommand, "story-002:AC-002", "pass", "-- ok"))
	o2.forge.listIssuesErr = errors.New("search down")
	o2.say(o2.command(ValidateFindingCommand, "story-002:code", "issue", "bug"))
	if e := o2.effect(store.EffectIssue, "story-002:code"); e.State != store.EffectUncertain || len(o2.forge.issueCreates) != 0 || o2.state().Ready {
		t.Fatalf("%+v", e)
	}
}

func TestOwnerDeliveryNeedsEachConfirmationAndEndsOnlyAfterClose(t *testing.T) {
	o := newOwnerFixture(t)
	o.readyToPush()
	if len(o.forge.pushes)+len(o.forge.prCreates) != 0 {
		t.Fatal("nothing happens before the owner's command")
	}
	commit := o.state().Commit
	o.say(o.command(ValidatePushCommand))
	if len(o.forge.pushes) != 1 || o.forge.pushes[0] != commit || len(o.forge.prCreates) != 1 || len(o.forge.merges) != 0 {
		t.Fatalf("push only: %+v", o.forge)
	}
	if o.pushSeen[0] != commit+"<-" {
		t.Fatalf("a new branch is pushed against an empty expected head: %v", o.pushSeen)
	}
	pr := decodeEffectResult(o.state().Effects.PR)
	if pr.Number != 601 || pr.Head != commit {
		t.Fatalf("%+v", pr)
	}
	// A merge naming another head or number is refused.
	o.say(o.command(ValidateMergeCommand, "601", strings.Repeat("b", 40)))
	o.say(o.command(ValidateMergeCommand, "999", commit))
	if len(o.forge.merges) != 0 {
		t.Fatal("a mismatched merge is refused")
	}
	// Close before merge is refused.
	o.say(o.command(ValidateCloseCommand, "497", "520"))
	if len(o.forge.issueCloses) != 0 {
		t.Fatal("close before merge")
	}
	o.say(o.command(ValidateMergeCommand, "601", commit))
	if len(o.forge.merges) != 1 || o.forge.merges[0] != "601@"+commit {
		t.Fatalf("%v", o.forge.merges)
	}
	if o.final() == "delivered" || len(o.forge.issueCloses) != 0 {
		t.Fatal("merge does not close")
	}
	// Close must name exactly the displayed issues.
	o.say(o.command(ValidateCloseCommand, "497"))
	if len(o.forge.issueCloses) != 0 {
		t.Fatal("close names the displayed set")
	}
	o.reply(o.owner, o.command(ValidateCloseCommand, "497 520"))
	nodes := o.toOwner()
	if o.final() != "delivered" || !strings.Contains(joinNodes(nodes), "owner_close") {
		t.Fatalf("final %s %s", o.final(), joinNodes(nodes))
	}
	inst, _ := o.db.GetGraphInstance(t.Context(), o.instance.ID)
	if inst.Status != store.GraphSucceeded && inst.CurrentNode != "delivered" {
		t.Fatalf("%+v", inst)
	}
	if strings.Join(o.forge.issueCloses, ",") != "520,497" {
		t.Fatalf("stories first, the parent last: %v", o.forge.issueCloses)
	}
}

func TestOwnerPushReplayFindsTheRemoteStateAndCreatesNothingTwice(t *testing.T) {
	o := newOwnerFixture(t)
	o.readyToPush()
	commit := o.state().Commit
	// The push happened and the reply was lost: the remote already holds it.
	o.forge.remoteHead = commit
	o.say(o.command(ValidatePushCommand))
	if len(o.forge.pushes) != 0 || len(o.forge.prCreates) != 1 {
		t.Fatalf("no second push: %v %v", o.forge.pushes, o.forge.prCreates)
	}
	// The pull request exists already: replay finds it.
	o2 := newOwnerFixture(t)
	o2.readyToPush()
	c2 := o2.state().Commit
	o2.forge.remoteHead = c2
	o2.forge.prs = []github.DeliveryPR{{Number: 77, Head: c2, Branch: "lf/497-validate", Target: "main", State: "open"}}
	o2.say(o2.command(ValidatePushCommand))
	if len(o2.forge.prCreates) != 0 || decodeEffectResult(o2.state().Effects.PR).Number != 77 {
		t.Fatalf("%v", o2.forge.prCreates)
	}
}

func TestOwnerPushFailureStaysOpenAndRepeatingRetries(t *testing.T) {
	o := newOwnerFixture(t)
	o.readyToPush()
	o.pushErr = errors.New("rejected")
	o.say(o.command(ValidatePushCommand))
	if e := o.effect(store.EffectPush, o.state().Commit); e.State != store.EffectFailed || len(o.forge.prCreates) != 0 {
		t.Fatalf("%+v", e)
	}
	if !strings.Contains(o.forge.bodies("**Validate report**")[0].Body, "push: failed") {
		t.Fatal("the failure shows in the report")
	}
	o.pushErr = nil
	o.say(o.command(ValidatePushCommand))
	if !effectDone(o.state().Effects.Push) || !effectDone(o.state().Effects.PR) {
		t.Fatal("repeating the command retries")
	}
}

func TestOwnerMovedHeadIsStaleAndNothingRemoteHappens(t *testing.T) {
	o := newOwnerFixture(t)
	o.readyToPush()
	o.reply(o.owner, o.command(ValidatePushCommand))
	o.write("app/extra.go", "package app\n")
	if _, err := o.wt.Commit(t.Context(), gitCommitOptions()); err != nil {
		t.Fatal(err)
	}
	o.toOwner()
	if len(o.forge.pushes)+len(o.forge.prCreates) != 0 || !strings.Contains(o.notice(), "stale") {
		t.Fatalf("pushes %v notice %q", o.forge.pushes, o.notice())
	}
}

func TestOwnerMergeReplayAndFailure(t *testing.T) {
	o := newOwnerFixture(t)
	o.readyToPush()
	commit := o.state().Commit
	o.say(o.command(ValidatePushCommand))
	o.forge.mergeErr = errors.New("not mergeable")
	o.say(o.command(ValidateMergeCommand, "601", commit))
	if e := o.effect(store.EffectMerge, "601@"+commit); e.State != store.EffectFailed {
		t.Fatalf("%+v", e)
	}
	// The merge happened but the reply was lost: replay checks merged state first.
	o.forge.mergeErr = nil
	o.forge.prs[0].Merged, o.forge.prs[0].State = true, "closed"
	before := len(o.forge.merges)
	o.say(o.command(ValidateMergeCommand, "601", commit))
	if e := o.effect(store.EffectMerge, "601@"+commit); e.State != store.EffectSucceeded || len(o.forge.merges) != before {
		t.Fatalf("%+v merges %d", e, len(o.forge.merges))
	}
}

func TestOwnerClosePartialFailureKeepsValidateOpen(t *testing.T) {
	o := newOwnerFixture(t)
	o.readyToPush()
	commit := o.state().Commit
	o.say(o.command(ValidatePushCommand))
	o.say(o.command(ValidateMergeCommand, "601", commit))
	o.forge.closeErr = errors.New("forbidden")
	o.say(o.command(ValidateCloseCommand, "497 520"))
	if o.final() == "delivered" || o.effect(store.EffectClose, "497 520").State != store.EffectFailed {
		t.Fatalf("%s", o.final())
	}
	o.forge.closeErr = nil
	o.say(o.command(ValidateCloseCommand, "497 520"))
	if o.final() != "delivered" {
		t.Fatalf("%s", o.final())
	}
}

func TestOwnerLoopLimitStopsTheRunAndApprovesNothing(t *testing.T) {
	o := newOwnerFixture(t)
	o.mixed()
	for range 30 {
		inst, _ := o.db.GetGraphInstance(t.Context(), o.instance.ID)
		if inst.Status != store.GraphRunning {
			break
		}
		o.reply(o.owner, "/validate-push 000000000000")
		o.until(func(w store.GraphOwnerWait, ok bool) bool { return ok && w.State == "waiting" })
	}
	inst, _ := o.db.GetGraphInstance(t.Context(), o.instance.ID)
	if inst.Status == store.GraphRunning || len(o.forge.pushes)+len(o.forge.merges)+len(o.forge.issueCloses) != 0 {
		t.Fatalf("%s", inst.Status)
	}
}

// records counts every verdict, choice and effect of the run, history included.
func (o *ownerFixture) records() int {
	o.t.Helper()
	commit := o.state().Commit
	verdicts, err := o.db.ListValidateVerdicts(o.t.Context(), o.instance.ID, commit, true)
	if err != nil {
		o.t.Fatal(err)
	}
	choices, err := o.db.ListValidateChoices(o.t.Context(), o.instance.ID, commit, true)
	if err != nil {
		o.t.Fatal(err)
	}
	effects, err := o.db.ListValidateEffects(o.t.Context(), o.instance.ID)
	if err != nil {
		o.t.Fatal(err)
	}
	return len(verdicts) + len(choices) + len(effects)
}

func (o *ownerFixture) ship() string {
	return o.command(ValidateShipCommand, "-- opened the page and clicked through it")
}

// #573 c-refuse: a blocked /validate-ship is refused with one notice naming the
// blocker and records nothing.
func TestOwnerShipIsRefusedWhenSomethingBlocksAndChangesNothing(t *testing.T) {
	blockingReview := func(req ValidateReviewRequest) (*agentresult.Result, error) {
		res, _ := mixedReview(req)
		for si := range res.Payload.Validate.Stories {
			for i := range res.Payload.Validate.Stories[si].Criteria {
				if res.Payload.Validate.Stories[si].Criteria[i].Name == "AC-002" {
					res.Payload.Validate.Stories[si].Criteria[i].Status = "unmet"
				}
			}
		}
		return res, nil
	}
	cases := map[string]struct {
		setup   func(o *ownerFixture)
		body    func(o *ownerFixture) string
		move    bool
		outcome string
		want    string
	}{
		"a failed criterion": {setup: func(o *ownerFixture) {
			o.mixed()
			o.say(o.command(ValidateCheckCommand, "story-002:AC-002", "fail", "-- the page is broken"))
		}, want: "Blocking finding `story-002:AC-002`"},
		"an open blocking finding": {setup: func(o *ownerFixture) {
			o.reviewers.run = blockingReview
			o.toOwner()
		}, want: "Blocking finding `story-002:AC-002`"},
		"a reviewer without a usable result": {setup: func(o *ownerFixture) {
			o.reviewers.run = deniedReview
			o.toOwner()
		}, want: "reviewer has no usable result"},
		"a failed suite": {setup: func(o *ownerFixture) {
			o.suiteExit = func(string) int { return 1 }
			o.toOwner()
		}, want: "The suite did not pass"},
		"a stale revision": {setup: func(o *ownerFixture) { o.mixed() },
			body: func(*ownerFixture) string { return "/validate-ship 000000000000 -- checked" }, outcome: "stale", want: "current report revision"},
		"a moved head": {setup: func(o *ownerFixture) { o.mixed() }, move: true, outcome: "stale", want: "branch head is now"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			o := newOwnerFixture(t)
			c.setup(o)
			rev, before := o.state().Revision, o.records()
			body := o.ship()
			if c.body != nil {
				body = c.body(o)
			}
			o.reply(o.owner, body)
			if c.move {
				o.write("app/extra.go", "package app\n")
				if _, err := o.wt.Commit(t.Context(), gitCommitOptions()); err != nil {
					t.Fatal(err)
				}
			}
			nodes := o.toOwner()
			if !strings.Contains(o.notice(), c.want) {
				t.Fatalf("notice %q does not name %q", o.notice(), c.want)
			}
			if c.outcome == "" && !strings.Contains(o.notice(), "/validate-ship is refused") {
				t.Fatalf("notice %q", o.notice())
			}
			if o.records() != before || o.state().Revision != rev {
				t.Fatal("a refused ship recorded something")
			}
			if visits(nodes, "owner_push")+visits(nodes, "owner_merge")+visits(nodes, "owner_close") != 0 || len(o.forge.pushes)+len(o.forge.prCreates)+len(o.forge.merges)+len(o.forge.issueCloses) != 0 {
				t.Fatalf("a refused ship acted: %s", joinNodes(nodes))
			}
		})
	}
}

// #573 c-ship: from a clean, assessed report one /validate-ship records the open
// check as passed, defers the optional finding and delivers.
func TestOwnerShipDeliversFromOneReply(t *testing.T) {
	o := newOwnerFixture(t)
	o.mixed()
	if !strings.Contains(o.forge.bodies("**Validate report**")[0].Body, "Next: `"+ValidateShipCommand+" "+o.state().Revision) {
		t.Fatal("the report names /validate-ship as the next step")
	}
	commit := o.state().Commit
	o.reply(o.owner, o.ship())
	nodes := o.toOwner()
	if o.final() != "delivered" {
		t.Fatalf("final %s path %s notice %q", o.final(), joinNodes(nodes), o.notice())
	}
	st := o.state()
	f, _ := st.finding("story-002:AC-002")
	if f.State != findingOwnerPassed || f.Verdict == nil || f.Verdict.Evidence != "opened the page and clicked through it" {
		t.Fatalf("criterion %+v", f)
	}
	if f, _ = st.finding("story-002:code"); f.Choice == nil || f.Choice.Choice != store.ChoiceDefer {
		t.Fatalf("finding %+v", f)
	}
	for _, e := range []*store.ValidateEffect{st.Effects.Push, st.Effects.PR, st.Effects.Merge, st.Effects.Close} {
		if !effectDone(e) {
			t.Fatalf("effect %+v", e)
		}
	}
	if len(o.forge.pushes) != 1 || len(o.forge.prCreates) != 1 || strings.Join(o.forge.merges, ",") != "601@"+commit || strings.Join(o.forge.issueCloses, ",") != "520,497" {
		t.Fatalf("pushes %v prs %v merges %v closes %v", o.forge.pushes, o.forge.prCreates, o.forge.merges, o.forge.issueCloses)
	}
	if visits(nodes, "owner_ask") != 0 {
		t.Fatalf("the ship did not wait for the owner in between: %s", joinNodes(nodes))
	}
}

// #573 c-resume: after a failed merge a second /validate-ship continues at the
// merge without pushing or opening the pull request again.
func TestOwnerShipResumesAtTheFailedStep(t *testing.T) {
	o := newOwnerFixture(t)
	o.mixed()
	commit := o.state().Commit
	o.forge.mergeErr = errors.New("not mergeable")
	o.say(o.ship())
	if o.final() == "delivered" || o.effect(store.EffectMerge, "601@"+commit).State != store.EffectFailed || len(o.forge.issueCloses) != 0 {
		t.Fatalf("final %s", o.final())
	}
	if !strings.Contains(o.notice(), "stopped at merge") {
		t.Fatalf("notice %q", o.notice())
	}
	if !strings.Contains(o.forge.bodies("**Validate report**")[0].Body, ValidateShipCommand) {
		t.Fatal("the report offers /validate-ship again")
	}
	o.forge.mergeErr = nil
	pushes, prs := len(o.forge.pushes), len(o.forge.prCreates)
	o.reply(o.owner, o.ship())
	nodes := o.toOwner()
	if o.final() != "delivered" || len(o.forge.pushes) != pushes || len(o.forge.prCreates) != prs || visits(nodes, "owner_push") != 0 {
		t.Fatalf("final %s path %s pushes %v prs %v", o.final(), joinNodes(nodes), o.forge.pushes, o.forge.prCreates)
	}
	if len(o.forge.merges) != 2 || strings.Join(o.forge.issueCloses, ",") != "520,497" {
		t.Fatalf("merges %v closes %v", o.forge.merges, o.forge.issueCloses)
	}
}

// #573 c-push-only: an individual /validate-push re-enters resolve after the
// push and waits; it never continues to the merge.
func TestOwnerIndividualPushStillStopsAfterThePush(t *testing.T) {
	o := newOwnerFixture(t)
	o.readyToPush()
	o.reply(o.owner, o.command(ValidatePushCommand))
	nodes := o.toOwner()
	if !effectDone(o.state().Effects.Push) || !effectDone(o.state().Effects.PR) || o.state().Effects.Merge != nil || len(o.forge.merges) != 0 {
		t.Fatalf("push only: %+v", o.state().Effects)
	}
	if !strings.Contains(joinNodes(nodes), "owner_push>owner_resolve>owner_report>owner_ask") {
		t.Fatalf("path %s", joinNodes(nodes))
	}
}
