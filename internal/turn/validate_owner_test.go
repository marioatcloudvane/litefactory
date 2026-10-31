package turn

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
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
		"malformed":                      {"/validate-check nope", "require"},
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
	if !strings.Contains(o.notice(), "Only the owner") || !strings.Contains(o.notice(), "configured owner") || !strings.Contains(o.notice(), "current report's choices") || len(o.state().Open) == 0 {
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

// The defect sentence and the coverage note of the code finding in
// followupReview: the follow-up issue keeps only the first.
const (
	followupDefect   = "Owner-action log analysis is reported as succeeded when execution was refused. internal/turn/log.go:40 settles it succeeded; it should fail."
	followupCoverage = "No separate formatting, vet or lint pass was supplied."
)

// followupReview is mixedReview with a code finding of two evidence entries:
// the defect first, then an unrelated coverage note with its own check.
func followupReview(req ValidateReviewRequest) (*agentresult.Result, error) {
	res, err := mixedReview(req)
	for si := range res.Payload.Validate.Stories {
		s := &res.Payload.Validate.Stories[si]
		for i := range s.Reviews {
			if s.Reviews[i].Name == "code" {
				s.Reviews[i].Evidence = []agent.EvidenceExplanation{{Summary: followupDefect, CheckIDs: []string{}}, {Summary: followupCoverage, CheckIDs: []string{"suite-1-0"}}}
			}
		}
	}
	return res, err
}

func (o *ownerFixture) followup() {
	o.reviewers.run = followupReview
	o.toOwner()
	o.say(o.command(ValidateCheckCommand, "story-002:AC-002", "pass", "-- ok"))
}

// #1278 C1, C2: a bug follow-up is titled by the defect, carries the bug
// label and a short body with the facts; a replay of an unlabelled issue
// only labels it.
func TestOwnerIssueBugIsTitledByTheDefectLabelledAndShort(t *testing.T) {
	o := newOwnerFixture(t)
	o.followup()
	o.say(o.command(ValidateFindingCommand, "story-002:code", "issue", "bug"))
	if len(o.forge.issues) != 1 || len(o.forge.issueCreates) != 1 {
		t.Fatalf("created %d: %s", len(o.forge.issueCreates), o.notice())
	}
	issue := o.forge.issues[0]
	if want := "Owner-action log analysis is reported as succeeded when execution was refused."; issue.Title != want || strings.HasPrefix(issue.Title, "Validate follow-up") {
		t.Fatalf("title %q", issue.Title)
	}
	if !slices.Equal(o.forge.labelCalls, []string{"900:bug"}) || !slices.Contains(issue.Labels, "bug") {
		t.Fatalf("labels %v %v", o.forge.labelCalls, issue.Labels)
	}
	e := o.effect(store.EffectIssue, "story-002:code")
	if e.State != store.EffectSucceeded {
		t.Fatalf("%+v", e)
	}
	for _, want := range []string{o.target.Parent.display().Reference(), o.state().Commit, "story-002:code", followupDefect, ValidateIssueMarker(o.instance.ID, "story-002:code")} {
		if !strings.Contains(issue.Body, want) {
			t.Fatalf("body lacks %q:\n%s", want, issue.Body)
		}
	}
	for _, unwanted := range []string{followupCoverage, "suite-1-0", "Evidence:"} {
		if strings.Contains(issue.Body, unwanted) {
			t.Fatalf("body keeps %q:\n%s", unwanted, issue.Body)
		}
	}
	// A replay finds the issue without its label: it labels it, creates nothing.
	o.forge.issues[0].Labels = nil
	st := o.state()
	f, _ := st.finding("story-002:code")
	r := &validateRemote{b: o.b, opts: o.opts, req: ActivityRequest{InstanceID: o.instance.ID}, st: st, target: o.target}
	if err := r.createIssue(t.Context(), e, f); err != nil || len(o.forge.issueCreates) != 1 || len(o.forge.labelCalls) != 2 || !slices.Contains(o.forge.issues[0].Labels, "bug") {
		t.Fatalf("replay: %v %d %v", err, len(o.forge.issueCreates), o.forge.labelCalls)
	}
}

// #1278 C4: a label failure after creation fails the effect visibly and a
// retry labels the same issue.
func TestOwnerIssueLabelFailureFailsAndRetryLabelsTheSameIssue(t *testing.T) {
	o := newOwnerFixture(t)
	o.followup()
	o.forge.labelErr = errors.New("labels are down")
	o.say(o.command(ValidateFindingCommand, "story-002:code", "issue", "bug"))
	e := o.effect(store.EffectIssue, "story-002:code")
	if e.State != store.EffectFailed || !strings.Contains(e.Error, "created #900 but could not label it bug") {
		t.Fatalf("%+v", e)
	}
	st := o.state()
	if st.Ready || !strings.Contains(o.forge.bodies("**Validate report**")[0].Body, "issue for story-002:code: failed") {
		t.Fatalf("%v", st.Open)
	}
	o.forge.labelErr = nil
	o.say(o.command(ValidateFindingCommand, "story-002:code", "issue", "bug"))
	if e := o.effect(store.EffectIssue, "story-002:code"); e.State != store.EffectSucceeded {
		t.Fatalf("%+v", e)
	}
	if len(o.forge.issueCreates) != 1 || len(o.forge.issues) != 1 || !slices.Contains(o.forge.issues[0].Labels, "bug") {
		t.Fatalf("created %d labels %v", len(o.forge.issueCreates), o.forge.issues[0].Labels)
	}
}

// #1278 C5: a label that landed despite an error reply settles as labelled.
func TestOwnerIssueLabelLandedDespiteErrorSettlesLabelled(t *testing.T) {
	o := newOwnerFixture(t)
	o.followup()
	o.forge.labelErr, o.forge.labelLandsDespiteErr = errors.New("the reply was lost"), true
	o.say(o.command(ValidateFindingCommand, "story-002:code", "issue", "bug"))
	e := o.effect(store.EffectIssue, "story-002:code")
	if e.State != store.EffectSucceeded {
		t.Fatalf("%+v", e)
	}
	if len(o.forge.issueCreates) != 1 || len(o.forge.labelCalls) != 1 || !slices.Equal(o.forge.issues[0].Labels, []string{"bug"}) {
		t.Fatalf("created %d labels %v %v", len(o.forge.issueCreates), o.forge.labelCalls, o.forge.issues[0].Labels)
	}
	st := o.state()
	f, _ := st.finding("story-002:code")
	r := &validateRemote{b: o.b, opts: o.opts, req: ActivityRequest{InstanceID: o.instance.ID}, st: st, target: o.target}
	if err := r.createIssue(t.Context(), e, f); err != nil || len(o.forge.issueCreates) != 1 || len(o.forge.labelCalls) != 1 {
		t.Fatalf("replay: %v %d %v", err, len(o.forge.issueCreates), o.forge.labelCalls)
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

// #926 c1: a pull request from the branch merged at the tested commit already
// delivered it; the push succeeds without publishing even when the merge
// deleted the remote branch.
func TestOwnerPushOfAnAlreadyMergedCommitPublishesNothing(t *testing.T) {
	o := newOwnerFixture(t)
	o.readyToPush()
	commit := o.state().Commit
	o.forge.remoteHead = ""
	o.forge.prs = []github.DeliveryPR{{Number: 44, Head: commit, Branch: "lf/497-validate", Target: "main", State: "closed", Merged: true}}
	o.say(o.command(ValidatePushCommand))
	if e := o.effect(store.EffectPush, commit); e.State != store.EffectSucceeded || len(o.pushSeen) != 0 || len(o.forge.pushes) != 0 {
		t.Fatalf("%+v published %v", e, o.pushSeen)
	}
	if len(o.forge.prCreates) != 0 || decodeEffectResult(o.state().Effects.PR).Number != 44 {
		t.Fatalf("the merged pull request is the PR record: %v %+v", o.forge.prCreates, o.state().Effects.PR)
	}
}

// #926 c4: a push that fails is logged with its effect kind and cause.
func TestOwnerPushFailureIsLoggedWithItsCause(t *testing.T) {
	o := newOwnerFixture(t)
	o.readyToPush()
	var buf bytes.Buffer
	o.opts.Logger = slog.New(slog.NewTextHandler(&buf, nil))
	o.pushErr = errors.New("remote rejected the update")
	o.say(o.command(ValidatePushCommand))
	if e := o.effect(store.EffectPush, o.state().Commit); e.State != store.EffectFailed {
		t.Fatalf("%+v", e)
	}
	var lines []string
	for _, l := range strings.Split(buf.String(), "\n") {
		if strings.Contains(l, "level=WARN") && strings.Contains(l, "effect=push") {
			lines = append(lines, l)
		}
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "remote rejected the update") {
		t.Fatalf("one warning with the cause: %q", buf.String())
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

// Shipping preserves the displayed failures and retains one exact reasoned
// exception instead of recording passing evidence.
func TestOwnerShipWaivesDisplayedBlockersWithoutChangingEvidence(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*ownerFixture)
	}{
		{"failed criterion", func(o *ownerFixture) {
			o.mixed()
			o.say(o.command(ValidateCheckCommand, "story-002:AC-002", "fail", "-- page broken"))
		}},
		{"missing reviews", func(o *ownerFixture) { o.reviewers.run = deniedReview; o.toOwner() }},
		{"failed suite", func(o *ownerFixture) { o.suiteExit = func(string) int { return 1 }; o.toOwner() }},
		{"mixed suite and missing reviews", func(o *ownerFixture) {
			o.suiteExit = func(string) int { return 1 }
			o.reviewers.run = deniedReview
			o.toOwner()
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := newOwnerFixture(t)
			tc.setup(o)
			before := o.state()
			raw, digest := before.shippingEvidence()
			records := o.records()
			o.say(o.ship())
			after := o.state()
			afterRaw, afterDigest := after.shippingEvidence()
			if o.final() != "delivered" {
				t.Fatalf("not delivered: %s; %s", o.final(), o.notice())
			}
			if string(raw) != string(afterRaw) || digest != afterDigest || records+4 != o.records() {
				t.Fatal("shipping changed factual evidence")
			}
			if after.Waiver == nil || after.Waiver.Reason != "opened the page and clicked through it" || after.Waiver.ActorID != o.owner || len(after.Waiver.Blockers) == 0 {
				t.Fatalf("waiver %+v", after.Waiver)
			}
			if !strings.Contains(o.forge.bodies("**Validate report**")[0].Body, "shipped with owner waiver") {
				t.Fatal("confirmed report lacks waiver outcome")
			}
		})
	}
}

func TestOwnerShipRefusesUndisplayedHeadDrift(t *testing.T) {
	o := newOwnerFixture(t)
	o.mixed()
	o.reply(o.owner, o.ship())
	o.write("app/extra.go", "package app\n")
	if _, err := o.wt.Commit(t.Context(), gitCommitOptions()); err != nil {
		t.Fatal(err)
	}
	o.toOwner()
	if len(o.forge.pushes) != 0 || !strings.Contains(o.notice(), "branch head is now") {
		t.Fatalf("acted on drift: %s", o.notice())
	}
	if o.state().Waiver != nil {
		t.Fatal("retained waiver for undisplayed work")
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
	if f.State != findingAwaitingCheck || f.Verdict != nil {
		t.Fatalf("shipping fabricated criterion verdict: %+v", f)
	}
	if f, _ = st.finding("story-002:code"); f.Choice != nil {
		t.Fatalf("shipping fabricated choice: %+v", f)
	}
	if st.Waiver == nil || st.Waiver.Reason != "opened the page and clicked through it" {
		t.Fatalf("waiver %+v", st.Waiver)
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

// #926 c2: a branch already merged by hand at the tested commit ships from one
// reply: push, PR and merge are recorded as done without acting, and close runs.
func TestOwnerShipOfAnAlreadyMergedBranchClosesTheIssues(t *testing.T) {
	o := newOwnerFixture(t)
	o.mixed()
	commit := o.state().Commit
	o.forge.remoteHead = ""
	o.forge.prs = []github.DeliveryPR{{Number: 44, Head: commit, Branch: "lf/497-validate", Target: "main", State: "closed", Merged: true, MergeSHA: strings.Repeat("d", 40)}}
	o.reply(o.owner, o.ship())
	nodes := o.toOwner()
	if o.final() != "delivered" {
		t.Fatalf("final %s path %s notice %q", o.final(), joinNodes(nodes), o.notice())
	}
	st := o.state()
	for _, e := range []*store.ValidateEffect{st.Effects.Push, st.Effects.PR, st.Effects.Merge, st.Effects.Close} {
		if !effectDone(e) {
			t.Fatalf("effect %+v", e)
		}
	}
	if decodeEffectResult(st.Effects.PR).Number != 44 || o.effect(store.EffectMerge, "44@"+commit).State != store.EffectSucceeded {
		t.Fatalf("pr %+v merge %+v", st.Effects.PR, st.Effects.Merge)
	}
	if len(o.pushSeen) != 0 || len(o.forge.prCreates) != 0 || len(o.forge.merges) != 0 || strings.Join(o.forge.issueCloses, ",") != "520,497" {
		t.Fatalf("pushes %v prs %v merges %v closes %v", o.pushSeen, o.forge.prCreates, o.forge.merges, o.forge.issueCloses)
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
	waiver := o.state().Waiver
	if waiver == nil {
		t.Fatal("missing retry waiver")
	}
	preserved := false
	for _, e := range waiver.DeliveryEffects {
		if e.Kind == store.EffectMerge && e.State == store.EffectFailed && e.Error != "" {
			preserved = true
		}
	}
	if !preserved || !strings.Contains(strings.Join(waiver.Blockers, " "), "merge") {
		t.Fatal("retry erased the displayed operational failure audit")
	}
	if len(o.forge.merges) != 2 || strings.Join(o.forge.issueCloses, ",") != "520,497" {
		t.Fatalf("merges %v closes %v", o.forge.merges, o.forge.issueCloses)
	}
}

// #948: a merge GitHub refuses with 405 "Pull Request has merge conflicts"
// never tells the owner to repeat /validate-ship; it names the pull request and
// the /implement sync-resolver and /validate way out.
func TestOwnerShipMergeConflictOffersTheWayOut(t *testing.T) {
	o := newOwnerFixture(t)
	o.mixed()
	commit := o.state().Commit
	o.forge.mergeErr = errors.New("PUT https://api.github.com/repos/o/r/pulls/601/merge: 405 Method Not Allowed: Pull Request has merge conflicts")
	o.say(o.ship())
	if o.final() == "delivered" || o.effect(store.EffectMerge, "601@"+commit).State != store.EffectFailed || len(o.forge.issueCloses) != 0 {
		t.Fatalf("final %s", o.final())
	}
	st := o.state()
	if !st.mergeConflicted() {
		t.Fatalf("merge effect %+v", st.Effects.Merge)
	}
	notice := o.notice()
	if !strings.Contains(notice, "#601 has merge conflicts") || !strings.Contains(notice, ValidateStartCommand) || strings.Contains(notice, "Repeat") || strings.Contains(notice, ValidateShipCommand) {
		t.Fatalf("notice %q", notice)
	}
	if st.implementLine() == "" && strings.Contains(notice, ValidateImplementCommand) {
		t.Fatalf("notice offers %s the run would refuse: %q", ValidateImplementCommand, notice)
	}
	reports := o.forge.bodies("**Validate report**")
	report := reports[len(reports)-1].Body
	if !strings.Contains(report, st.mergeConflictStep()) || strings.Contains(report, st.shipLine()) || strings.Contains(report, "repeat your command to retry") {
		t.Fatalf("report %s", report)
	}
	// #1262: the way out is the factory's own sync-resolver path through
	// Implement, then a fresh Validate; never a manual merge and push.
	for name, body := range map[string]string{"notice": notice, "report": report} {
		if !strings.Contains(body, ValidateSyncCommand) || !strings.Contains(body, ValidateStartCommand) {
			t.Fatalf("%s does not offer %q and %q: %s", name, ValidateSyncCommand, ValidateStartCommand, body)
		}
		for _, manual := range []string{"Merge the target branch into the feature branch", ", push,", "resolve the conflict and validate again"} {
			if strings.Contains(body, manual) {
				t.Fatalf("%s still tells the owner to %q: %s", name, manual, body)
			}
		}
	}
	if row := effectText("merge", st.Effects.Merge); !strings.Contains(row, ValidateSyncCommand) {
		t.Fatalf("merge row %q", row)
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

// blockingReview leaves AC-002 unmet with the reviewer's file:line text and,
// when code is set, reports the code review as a blocking, unmet finding.
func blockingReview(code bool) func(ValidateReviewRequest) (*agentresult.Result, error) {
	return func(req ValidateReviewRequest) (*agentresult.Result, error) {
		res := goodReview(req)
		for si := range res.Payload.Validate.Stories {
			s := &res.Payload.Validate.Stories[si]
			for i := range s.Criteria {
				if s.Criteria[i].Name == "AC-002" {
					s.Criteria[i].Status = "unmet"
					s.Criteria[i].Evidence = []agent.EvidenceExplanation{{Summary: "app/app.go:3 Total returns 9; AC-002 needs 10.", CheckIDs: []string{}}}
				}
			}
			for i := range s.Reviews {
				if code && s.Reviews[i].Name == "code" {
					s.Reviews[i].Status, s.Reviews[i].Classification = "unmet", "blocking"
					s.Reviews[i].Evidence = []agent.EvidenceExplanation{{Summary: "app/app.go:3 the golden-output test is missing.", CheckIDs: []string{}}}
				}
			}
		}
		return res, nil
	}
}

// resolveOutcomes lists the outcomes of the resolve step, in order.
func (o *ownerFixture) resolveOutcomes() []string {
	o.t.Helper()
	attempts, err := o.db.ListGraphNodeAttempts(o.t.Context(), o.instance.ID)
	if err != nil {
		o.t.Fatal(err)
	}
	var out []string
	for _, a := range attempts {
		if a.Node == "owner_resolve" {
			out = append(out, a.Outcome)
		}
	}
	return out
}

func (o *ownerFixture) rounds() []store.RepairRound {
	o.t.Helper()
	rounds, err := o.db.ListRepairRounds(o.t.Context(), o.instance.ID)
	if err != nil {
		o.t.Fatal(err)
	}
	return rounds
}

// handoffAgent records the repair handoff and runs the given repair.
func (o *ownerFixture) handoffAgent(handoffs *[]string, repair func(f *flowFixture) agentresult.ValidateRepair) {
	o.agent = func(_ int, f *flowFixture) (agentresult.ValidateRepair, ActivityResult) {
		text, err := validateRepairHandoff(f.t.Context(), f.db, f.instance.ID, 0, f.profile, WorkflowPolicy{}, agentresult.Context{})
		if err != nil {
			f.t.Fatalf("handoff: %v", err)
		}
		*handoffs = append(*handoffs, text)
		return repair(f), ActivityResult{}
	}
}

// #617: a repair choice starts a repair agent on the chosen finding; a kept
// change is a new commit that the suite and the reviewers test again.
func TestOwnerRepairChoiceStartsARepairAndTestsTheKeptCommit(t *testing.T) {
	o := newOwnerFixture(t)
	o.reviewers.run = blockingReview(false)
	o.toOwner()
	base := o.state().Commit
	var handoffs []string
	o.handoffAgent(&handoffs, func(f *flowFixture) agentresult.ValidateRepair {
		f.write("app/app.go", "package app\n\nfunc Total() int { return 10 }\n")
		return repaired("app/app.go")
	})
	nodes := o.say(o.command(ValidateFindingCommand, "story-002:AC-002", "repair"))
	if !strings.Contains(joinNodes(nodes), "owner_resolve>repair_budget>repair_agent>repair_check>suite>review>outcome>owner_resolve>owner_report>owner_ask") {
		t.Fatalf("path %s", joinNodes(nodes))
	}
	if got := o.resolveOutcomes(); len(got) < 2 || got[len(got)-2] != "repair" || got[len(got)-1] != "wait" {
		t.Fatalf("resolve outcomes %v", got)
	}
	if len(handoffs) != 1 {
		t.Fatalf("handoffs %d", len(handoffs))
	}
	for _, want := range []string{"finding: story-002:AC-002", "story: story-002", "criterion: AC-002", "app/app.go:3 Total returns 9", "untrusted data"} {
		if !strings.Contains(handoffs[0], want) {
			t.Fatalf("handoff lacks %q:\n%s", want, handoffs[0])
		}
	}
	rounds := o.rounds()
	if len(rounds) != 1 || rounds[0].State != store.RepairKept || rounds[0].PreCommit != base || rounds[0].ResultCommit == base {
		t.Fatalf("rounds %+v", rounds)
	}
	st := o.state()
	if st.Commit != rounds[0].ResultCommit || st.Suite.Outcome != store.SuitePassed {
		t.Fatalf("the suite did not run on the kept commit: %s %+v", st.Commit, st.Suite)
	}
	o.outcome(rounds[0].ResultCommit)
	for _, role := range o.reviewers.started[len(o.reviewers.started)-3:] {
		if role.Commit != rounds[0].ResultCommit {
			t.Fatalf("a reviewer read %s, not the kept commit", role.Commit)
		}
	}
	// The finding comes back on the new commit, without a choice yet.
	if f, _ := st.finding("story-002:AC-002"); f.State != findingOpen || f.Choice != nil {
		t.Fatalf("finding on the new commit %+v", f)
	}
}

// #617: repairs chosen for several findings run together in one round, and a
// pending owner check does not hold the round back.
func TestOwnerRepairChoicesRunTogetherInOneRound(t *testing.T) {
	o := newOwnerFixture(t)
	o.reviewers.run = blockingReview(true)
	o.toOwner()
	var handoffs []string
	o.handoffAgent(&handoffs, func(f *flowFixture) agentresult.ValidateRepair {
		f.write("app/app.go", "package app\n\nfunc Total() int { return 10 }\n")
		return repaired("app/app.go")
	})
	nodes := o.say(o.command(ValidateFindingCommand, "story-002:AC-002", "repair"))
	if visits(nodes, "repair_budget") != 0 || len(o.rounds()) != 0 || o.resolveOutcomes()[len(o.resolveOutcomes())-1] != "wait" {
		t.Fatalf("the first choice must wait: %s", joinNodes(nodes))
	}
	nodes = o.say(o.command(ValidateFindingCommand, "story-002:code", "repair"))
	if visits(nodes, "repair_agent") != 1 || len(o.rounds()) != 1 || len(handoffs) != 1 {
		t.Fatalf("one round: %s rounds %d", joinNodes(nodes), len(o.rounds()))
	}
	for _, want := range []string{"finding: story-002:AC-002", "finding: story-002:code", "review: code", "golden-output test is missing"} {
		if !strings.Contains(handoffs[0], want) {
			t.Fatalf("handoff lacks %q:\n%s", want, handoffs[0])
		}
	}
}

func TestOwnerPendingCheckDoesNotHoldARepairBack(t *testing.T) {
	o := newOwnerFixture(t)
	o.reviewers.run = func(req ValidateReviewRequest) (*agentresult.Result, error) {
		res, _ := mixedReview(req)
		for si := range res.Payload.Validate.Stories {
			for i := range res.Payload.Validate.Stories[si].Reviews {
				if r := &res.Payload.Validate.Stories[si].Reviews[i]; r.Name == "code" {
					r.Classification = "blocking"
				}
			}
		}
		return res, nil
	}
	o.toOwner()
	if f, _ := o.state().finding("story-002:AC-002"); f.State != findingAwaitingCheck {
		t.Fatalf("%+v", f)
	}
	var handoffs []string
	o.handoffAgent(&handoffs, func(f *flowFixture) agentresult.ValidateRepair {
		f.write("app/app.go", "package app\n\nfunc Total() int { return 10 }\n")
		return repaired("app/app.go")
	})
	nodes := o.say(o.command(ValidateFindingCommand, "story-002:code", "repair"))
	if visits(nodes, "repair_agent") != 1 || len(o.rounds()) != 1 || strings.Contains(handoffs[0], "AC-002") {
		t.Fatalf("path %s handoff %q", joinNodes(nodes), handoffs)
	}
}

// #617: a repair that is too big, handed back or out of budget returns to the
// owner report, which shows the round and its reason.
func TestOwnerRejectedOrSpentRepairReturnsToTheReportWithItsReason(t *testing.T) {
	o := newOwnerFixture(t)
	o.reviewers.run = blockingReview(false)
	o.toOwner()
	base := o.state().Commit
	o.agent = func(round int, f *flowFixture) (agentresult.ValidateRepair, ActivityResult) {
		if round == 1 {
			for _, name := range []string{"a.go", "b.go", "c.go", "d.go"} {
				f.write("app/"+name, "package app\n")
			}
			return repaired("app/a.go", "app/b.go", "app/c.go", "app/d.go"), ActivityResult{}
		}
		return handbackReport("the sum needs a new type"), ActivityResult{}
	}
	back := "repair_check>review>outcome>owner_resolve>owner_report>owner_ask"
	report := func() string { return o.forge.bodies("**Validate report**")[0].Body }

	nodes := o.say(o.command(ValidateFindingCommand, "story-002:AC-002", "repair"))
	if !strings.Contains(joinNodes(nodes), back) || o.head() != base || !strings.Contains(report(), "round 1: too_big (4 files changed, at most 3 allowed)") {
		t.Fatalf("too big: %s\n%s", joinNodes(nodes), report())
	}
	nodes = o.say(o.command(ValidateFindingCommand, "story-002:AC-002", "repair"))
	if !strings.Contains(joinNodes(nodes), back) || o.head() != base || !strings.Contains(report(), "round 2: handback (the repair agent reported the fix as too large or uncertain)") {
		t.Fatalf("handback: %s\n%s", joinNodes(nodes), report())
	}
	nodes = o.say(o.command(ValidateFindingCommand, "story-002:AC-002", "repair"))
	if !strings.Contains(joinNodes(nodes), "owner_resolve>repair_budget>review>outcome>owner_resolve>owner_report>owner_ask") || visits(nodes, "repair_agent") != 0 || len(o.rounds()) != 2 {
		t.Fatalf("spent: %s", joinNodes(nodes))
	}
	if inst, _ := o.db.GetGraphInstance(t.Context(), o.instance.ID); inst.Status != store.GraphRunning || o.state().Commit != base {
		t.Fatalf("the run must wait for the owner on the tested commit: %s", inst.Status)
	}
}

// #617: an issue choice still only creates the issue; the repair chosen for
// the last blocking finding then starts the round.
func TestOwnerIssueChoiceStartsNoRepair(t *testing.T) {
	o := newOwnerFixture(t)
	o.reviewers.run = blockingReview(true)
	o.toOwner()
	nodes := o.say(o.command(ValidateFindingCommand, "story-002:AC-002", "issue", "bug"))
	if !strings.Contains(joinNodes(nodes), "owner_resolve>owner_issue>owner_report") || visits(nodes, "repair_budget") != 0 || len(o.rounds()) != 0 || len(o.forge.issues) != 1 {
		t.Fatalf("issue: %s", joinNodes(nodes))
	}
	var handoffs []string
	o.handoffAgent(&handoffs, func(f *flowFixture) agentresult.ValidateRepair {
		f.write("app/app.go", "package app\n\nfunc Total() int { return 10 }\n")
		return repaired("app/app.go")
	})
	nodes = o.say(o.command(ValidateFindingCommand, "story-002:code", "repair"))
	if visits(nodes, "repair_agent") != 1 || strings.Contains(handoffs[0], "story-002:AC-002") || !strings.Contains(handoffs[0], "story-002:code") {
		t.Fatalf("repair: %s %q", joinNodes(nodes), handoffs)
	}
}

func ownerLiveReview(req ValidateReviewRequest) (*agentresult.Result, error) {
	res := goodReview(req)
	for si := range res.Payload.Validate.Stories {
		for i := range res.Payload.Validate.Stories[si].Reviews {
			r := &res.Payload.Validate.Stories[si].Reviews[i]
			if r.Name == "architecture" {
				r.Status, r.Classification = "unverified", "blocking"
				r.Evidence = []agent.EvidenceExplanation{{Summary: "The owner must check installed-provider isolation and exact-session resume.", CheckIDs: []string{}}}
			}
		}
	}
	return res, nil
}

func TestOwnerLiveReviewVerdicts(t *testing.T) {
	o := newOwnerFixture(t)
	o.stories = append(o.stories, ValidateStory{Key: "story-003", Title: "Second story", Revision: strings.Repeat("cd", 32), Criteria: []string{"AC-001"}})
	o.reviewers.run = ownerLiveReview
	o.toOwner()
	before := o.state()
	var ids []string
	for _, f := range before.Findings {
		if f.Name != "architecture" {
			continue
		}
		ids = append(ids, f.ID)
		body := renderValidateStoryComment(before, ValidateIssueRef{Key: f.Story}, 843)
		if f.State != findingAwaitingCheck || (!strings.Contains(body, ValidateCheckCommand+" "+before.Revision+" "+f.ID+" pass -- <what you checked>") || !strings.Contains(body, ValidateCheckCommand+" "+before.Revision+" "+f.ID+" fail -- <what you saw>")) || strings.Contains(body, "failed") || strings.Contains(body, "only on a new tested snapshot") {
			t.Fatalf("awaiting review: %+v\n%s", f, body)
		}
	}
	if len(ids) == 0 || before.Ready {
		t.Fatal("expected pending architecture reviews")
	}
	id := ids[0]
	o.say(o.command(ValidateCheckCommand, id, "fail", "-- provider continuity failed"))
	failed := o.state()
	f, _ := failed.finding(id)
	body := renderValidateStoryComment(failed, ValidateIssueRef{Key: f.Story}, 843)
	if f.State != findingOwnerFailed || failed.Ready || !strings.Contains(body, "provider continuity failed") || !strings.Contains(body, ValidateCheckCommand+" "+failed.Revision+" "+id) || strings.Contains(body, "only on a new tested snapshot") {
		t.Fatalf("failed check: %+v\n%s", f, body)
	}
	for i, target := range ids {
		o.say(o.command(ValidateCheckCommand, target, "pass", "-- both installed providers resumed the exact session"))
		after := o.state()
		got, _ := after.finding(target)
		original, _ := before.finding(target)
		v := got.Verdict
		if got.State != findingOwnerPassed || got.Status != original.Status || got.Summary != original.Summary || v == nil || v.AuthorID != o.owner || v.CommentID == 0 || v.InstanceID != o.instance.ID || v.Commit != before.Commit || v.Evidence != "both installed providers resumed the exact session" || after.Revision == before.Revision {
			t.Fatalf("retained owner check: %+v", got)
		}
		if i+1 < len(ids) {
			next, _ := after.finding(ids[i+1])
			if next.State != findingAwaitingCheck || after.Ready {
				t.Fatal("one story's verdict settled another")
			}
		}
		body = renderValidateStoryComment(after, ValidateIssueRef{Key: got.Story}, 843)
		if !strings.Contains(body, "owner passed") || strings.Contains(body, "failed") || strings.Contains(body, "(blocking") {
			t.Fatalf("passed review report: %s", body)
		}
	}
	after := o.state()
	if !after.Ready || !strings.Contains(after.NextStep(nil), ValidateShipCommand) {
		t.Fatalf("settled reviews still block: %v", after.Open)
	}
	all, err := o.db.ListValidateVerdicts(t.Context(), o.instance.ID, before.Commit, true)
	if err != nil || len(all) != len(ids)+1 {
		t.Fatalf("verdict history: %v %v", all, err)
	}
	other, err := o.db.ListValidateVerdicts(t.Context(), o.instance.ID, strings.Repeat("f", 40), false)
	if err != nil || len(other) != 0 {
		t.Fatalf("verdict leaked to another commit: %v %v", other, err)
	}
	if _, ok := replayedValidateReply(t.Context(), o.opts, after, all[len(all)-1].CommentID, false); !ok {
		t.Fatal("review verdict replay not recognized")
	}
}

func TestOwnerLiveReviewRefusals(t *testing.T) {
	for _, mode := range []string{"non-owner", "stale", "head", "evidence", "unknown", "unmet", "satisfied", "optional", "missing"} {
		t.Run(mode, func(t *testing.T) {
			o := newOwnerFixture(t)
			o.reviewers.run = func(req ValidateReviewRequest) (*agentresult.Result, error) {
				if mode == "missing" && req.Role == agent.ReviewRoleArchitecture {
					return nil, errors.New("review unavailable")
				}
				res, err := ownerLiveReview(req)
				for si := range res.Payload.Validate.Stories {
					for i := range res.Payload.Validate.Stories[si].Reviews {
						r := &res.Payload.Validate.Stories[si].Reviews[i]
						if r.Name == "architecture" {
							if mode == "unmet" || mode == "satisfied" {
								r.Status = mode
							}
							if mode == "optional" {
								r.Classification = "advisory"
							}
						}
					}
				}
				return res, err
			}
			o.toOwner()
			before := o.state()
			body := o.command(ValidateCheckCommand, "story-002:architecture", "pass", "-- checked live")
			switch mode {
			case "stale":
				body = strings.Replace(body, before.Revision, "000000000000", 1)
			case "head":
				o.write("app/extra.go", "package app\n")
				if _, err := o.wt.Commit(t.Context(), gitCommitOptions()); err != nil {
					t.Fatal(err)
				}
			case "evidence":
				body = o.command(ValidateCheckCommand, "story-002:architecture", "pass")
			case "unknown":
				body = o.command(ValidateCheckCommand, "story-002:unknown", "pass", "-- checked live")
			}
			if mode == "non-owner" {
				o.reply(o.owner+1, body)
				o.toOwner()
			} else {
				o.say(body)
			}
			all, err := o.db.ListValidateVerdicts(t.Context(), o.instance.ID, before.Commit, true)
			if err != nil || len(all) != 0 || o.state().Revision != before.Revision || o.notice() == "" || len(o.forge.pushes)+len(o.forge.merges) != 0 {
				t.Fatalf("refusal changed state: %v %v %s", all, err, o.notice())
			}
		})
	}
}

func TestOwnerShipRetainsLiveReviewEvidence(t *testing.T) {
	o := newOwnerFixture(t)
	o.reviewers.run = ownerLiveReview
	o.toOwner()
	before := o.state()
	if len(before.shipBlockers()) != 0 {
		t.Fatal(before.shipBlockers())
	}
	o.say(o.command(ValidateShipCommand, "-- checked both installed providers"))
	after := o.state()
	for _, f := range after.Findings {
		if f.Name == "architecture" && (f.State != findingAwaitingCheck || f.Verdict != nil) {
			t.Fatalf("ship changed review evidence: %+v", f)
		}
	}
	if !effectDone(after.Effects.Close) {
		t.Fatal("ship did not complete")
	}
}

// #922: the verified owner of the task's own Linear ticket connection ships
// from one reply, like the GitHub code owner. Another Linear user, and the
// appointed approver of a GitHub ticket connection, are still refused.
func TestOwnerShipFromTheLinearTicketOwner(t *testing.T) {
	linearOwner := ticket.Identity{Provider: ticket.Linear, Origin: "https://api.linear.app", RemoteID: "owner-uuid"}
	for _, tc := range []struct {
		name     string
		approver ticket.Identity
		author   ticket.Identity
		ships    bool
	}{
		{"linear ticket owner", linearOwner, linearOwner, true},
		{"another linear user", linearOwner, ticket.Identity{Provider: ticket.Linear, Origin: "https://api.linear.app", RemoteID: "other-uuid"}, false},
		{"github ticket approver", ticket.Identity{Provider: ticket.GitHub, Origin: "https://api.github.com", RemoteID: "42"}, ticket.Identity{Provider: ticket.GitHub, Origin: "https://api.github.com", RemoteID: "42"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := newOwnerFixture(t)
			o.mixed()
			author := int64(42)
			if tc.author.Provider == ticket.Linear {
				var err error
				if author, err = o.db.LocalTicketSlot(t.Context(), "actor", tc.author); err != nil {
					t.Fatal(err)
				}
			}
			approver := store.ApproverAppointment{Actor: tc.approver}
			o.opts.ProjectApprovers, o.opts.TicketApprover = true, &approver
			before := o.records()
			o.reply(author, o.ship())
			nodes := o.toOwner()
			st := o.state()
			if !tc.ships {
				if !strings.Contains(o.notice(), "Only the owner can give Validate commands.") || !strings.Contains(o.notice(), "configured code approver") || o.records() != before || len(o.forge.pushes) != 0 {
					t.Fatalf("a refused author shipped: notice %q path %s", o.notice(), joinNodes(nodes))
				}
				return
			}
			if outcomes := o.resolveOutcomes(); !strings.Contains(" "+strings.Join(outcomes, " ")+" ", " push ") {
				t.Fatalf("outcomes %v final %s notice %q", outcomes, o.final(), o.notice())
			}
			if st.Effects.Push == nil || !effectDone(st.Effects.Push) || st.Effects.Push.ConfirmCommentID == 0 || len(o.forge.pushes) != 1 {
				t.Fatalf("push effect %+v pushes %v path %s", st.Effects.Push, o.forge.pushes, joinNodes(nodes))
			}
		})
	}
}

// #922: the turn gate admits the verified Linear ticket owner for each of the
// six delivery commands and refuses everyone else.
func TestTurnCodeAuthorityAdmitsTheLinearTicketOwner(t *testing.T) {
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "authority.db"))
	mustProgress(t, err)
	defer func() { mustProgress(t, db.Close()) }()
	mustProgress(t, db.Migrate(ctx))
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: "/linear-code", Forge: store.ForgeGitHub, Owner: "owner", Name: "code", RemoteURL: "git@github.com:owner/code.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: "/linear-trees"})
	mustProgress(t, err)
	code, err := db.SetConnection(ctx, store.ConnectionVersion{RepoID: repo.ID, Role: store.CodeConnection, Provider: ticket.GitHub, Origin: "https://api.github.com", Destination: repo.Slug(), CredentialSource: "env", CredentialEnv: "TOKEN", Enabled: true, Provenance: "explicit", ClonePath: repo.Path, RemoteURL: repo.RemoteURL, BaseBranch: repo.BaseBranch, FeaturePrefix: repo.FeaturePrefix, WorktreeRoot: repo.WorktreeRoot}, 0)
	mustProgress(t, err)
	tickets, err := db.SetConnection(ctx, store.ConnectionVersion{RepoID: repo.ID, Role: store.TicketConnection, Provider: ticket.Linear, Origin: "https://api.linear.app", Destination: "team/project", CredentialSource: "env", CredentialEnv: "LINEAR", Enabled: true, Provenance: "explicit", Linear: store.LinearConfiguration{WorkspaceID: "workspace", TeamID: "team", ProjectID: "project", AuthKind: "api_key", ModeLabelsJSON: `{"plan":"label-plan"}`, LifecycleStatesJSON: `{"completed":"done","retired":"cancelled"}`}}, 0)
	mustProgress(t, err)
	owner := ticket.Identity{Provider: ticket.Linear, Origin: tickets.Origin, RemoteID: "owner-uuid"}
	ownerSlot, err := db.LocalTicketSlot(ctx, "actor", owner)
	mustProgress(t, err)
	other := ticket.Identity{Provider: ticket.Linear, Origin: tickets.Origin, RemoteID: "other-uuid"}
	otherSlot, err := db.LocalTicketSlot(ctx, "actor", other)
	mustProgress(t, err)
	bot := ticket.Identity{Provider: ticket.Linear, Origin: tickets.Origin, RemoteID: "publisher-uuid"}
	botSlot, err := db.LocalTicketSlot(ctx, "actor", bot)
	mustProgress(t, err)
	a, err := db.SetApprover(ctx, store.ApproverAppointment{RepoID: repo.ID, Actor: owner, Handle: "owner", VerifiedAt: time.Now(), Provenance: "explicit"}, tickets.Revision, 0)
	mustProgress(t, err)
	ref := ticket.Ref{Identity: ticket.Identity{Provider: ticket.Linear, Origin: tickets.Origin, RemoteID: "issue-uuid"}, Locator: ticket.Locator{Provider: ticket.Linear, Origin: tickets.Origin, Container: tickets.Destination, Key: "issue-uuid"}}
	_, err = db.AddTicketBinding(ctx, store.TicketBinding{RepoID: repo.ID, ConnectionVersionID: tickets.ID, Ref: ref, Enabled: true, Provenance: "explicit"})
	mustProgress(t, err)
	session, err := db.AdmitSession(ctx, repo, ref, code.Revision, tickets.Revision, botSlot, "publisher", "linear:team/project#1")
	mustProgress(t, err)
	opts := Options{DB: db, ProjectApprovers: true}
	fresh := a.ActivatedAt.Add(time.Second)
	source := ticket.Identity{Provider: ticket.Linear, Origin: tickets.Origin, RemoteID: "reply-uuid"}
	for _, command := range []string{"/validate-ship rev -- ok", "/validate-push rev", "/validate-merge rev", "/validate-close rev", "/deliver", "/merge"} {
		trigger := Trigger{CommentID: 101, AuthorID: ownerSlot, Actor: owner, Source: source, CreatedAt: fresh, Body: command}
		if err := verifyTurnCodeAuthority(ctx, opts, session, trigger); err != nil {
			t.Fatalf("%s: the Linear ticket owner was refused: %v", command, err)
		}
	}
	trigger := Trigger{CommentID: 101, AuthorID: ownerSlot, Actor: owner, Source: source, CreatedAt: fresh, Body: "/validate-ship rev -- ok"}
	claim, err := db.ClaimProjectCodeAuthority(ctx, store.AuthorityClaim{SessionID: session.ID, RepoID: repo.ID, AppointmentID: a.ID, Revision: a.Revision, Actor: owner, Source: source, CreatedAt: fresh, Kind: "code-control", Key: "turn:101"})
	if err != nil {
		t.Fatalf("the Linear ticket owner's code claim: %v", err)
	}
	mustProgress(t, db.CompleteProjectAuthority(ctx, claim.ID))
	if _, err := db.ClaimProjectCodeAuthority(ctx, store.AuthorityClaim{SessionID: session.ID, RepoID: repo.ID, AppointmentID: a.ID, Revision: a.Revision, Actor: owner, Source: source, CreatedAt: a.ActivatedAt, Kind: "code-control", Key: "turn:102"}); !errors.Is(err, store.ErrApproverReplay) {
		t.Fatalf("a stale Linear code claim was admitted: %v", err)
	}
	if _, err := db.ClaimProjectCodeAuthority(ctx, store.AuthorityClaim{SessionID: session.ID, RepoID: repo.ID, AppointmentID: a.ID, Revision: a.Revision, Actor: other, Source: source, CreatedAt: fresh, Kind: "code-control", Key: "turn:103"}); !errors.Is(err, store.ErrApproverUnauthorized) {
		t.Fatalf("another Linear user's code claim was admitted: %v", err)
	}
	for name, change := range map[string]func(*Trigger){
		"another linear user": func(tr *Trigger) { tr.AuthorID, tr.Actor = otherSlot, other },
		"the publisher":       func(tr *Trigger) { tr.AuthorID, tr.Actor = botSlot, bot },
		"slot mismatch":       func(tr *Trigger) { tr.AuthorID = otherSlot },
		"foreign source":      func(tr *Trigger) { tr.Source.Origin = "https://linear.example" },
		"github source": func(tr *Trigger) {
			tr.Source = ticket.Identity{Provider: ticket.GitHub, Origin: "https://api.github.com", RemoteID: "1"}
		},
	} {
		tr := trigger
		change(&tr)
		if err := verifyTurnCodeAuthority(ctx, opts, session, tr); !errors.Is(err, store.ErrApproverUnauthorized) {
			t.Fatalf("%s gained code authority: %v", name, err)
		}
	}
}

// deviationReview makes the architecture review of every story a blocking
// unmet finding, as the MAR-11 reviewer did.
func deviationReview(req ValidateReviewRequest) (*agentresult.Result, error) {
	res := goodReview(req)
	for si := range res.Payload.Validate.Stories {
		for i := range res.Payload.Validate.Stories[si].Reviews {
			r := &res.Payload.Validate.Stories[si].Reviews[i]
			if r.Name == "architecture" {
				r.Status, r.Classification = "unmet", "blocking"
				r.Evidence = []agent.EvidenceExplanation{{Summary: "The design says to keep the icon legend; the whole legend is removed.", CheckIDs: []string{}}}
			}
		}
	}
	return res, nil
}

// prBodyForge records the body of each pull request it creates.
type prBodyForge struct {
	*ownerForge
	bodies []string
}

func (f *prBodyForge) CreateDeliveryPR(ctx context.Context, repo github.Repo, branch, base string, title, body redact.Body) (github.DeliveryPR, error) {
	f.bodies = append(f.bodies, body.String())
	return f.ownerForge.CreateDeliveryPR(ctx, repo, branch, base, title, body)
}

// #942 C1, C2, C4, C6: the owner accepts a blocking review finding as a
// deliberate deviation with a reason; the run becomes ready, the report and
// the pull request show the acceptance, and a replay writes nothing twice.
func TestOwnerAcceptsABlockingReviewFindingAsADeliberateDeviation(t *testing.T) {
	o := newOwnerFixture(t)
	code := &prBodyForge{ownerForge: o.forge}
	o.delivery.Code = code
	o.registry = ActivityRegistry{}
	o.register()
	o.reviewers.run = deviationReview
	o.toOwner()
	const id, reason = "story-002:architecture", "the owner chose tooltips over the legend in MAR-14"
	before := o.state()
	f, _ := before.finding(id)
	if before.Ready || f.State != findingOpen || !f.acceptable() {
		t.Fatalf("expected an open blocking review: %+v %v", f, before.Open)
	}
	accept := ValidateFindingCommand + " " + before.Revision + " " + id + " accept -- <reason>"
	if next := strings.Join(before.NextCommands(nil), "\n"); !strings.Contains(next, accept) {
		t.Fatalf("NextCommands offers no accept: %s", next)
	}
	if body := renderValidateStoryComment(before, ValidateIssueRef{Key: "story-002"}, 843); !strings.Contains(body, accept) || strings.Contains(body, "|accept -- <reason>") || !strings.Contains(body, "accept it as a deliberate deviation") {
		t.Fatalf("the story comment offers no accept:\n%s", body)
	}
	if !strings.Contains(strings.Join(before.Open, " "), "accept -- <reason>") {
		t.Fatalf("readiness does not name accept: %v", before.Open)
	}

	o.say(o.command(ValidateFindingCommand, id, "accept", "-- "+reason))
	after := o.state()
	f, _ = after.finding(id)
	if f.State != findingOwnerPassed || f.Verdict == nil || f.Verdict.Evidence != reason || f.Verdict.CommentID == 0 || f.Verdict.AuthorID != o.owner || !f.acceptedDeviation() {
		t.Fatalf("accept not recorded: %+v %+v notice %q", f, f.Verdict, o.notice())
	}
	if strings.Contains(strings.Join(after.Open, " "), id) || !after.Ready || len(after.shipBlockers()) != 0 {
		t.Fatalf("the accepted finding still blocks: %v", after.Open)
	}
	body := renderValidateStoryComment(after, ValidateIssueRef{Key: "story-002"}, 843)
	if !strings.Contains(body, "Architecture: 1 finding (owner accepted: "+reason+")") || strings.Contains(body, "**Blocks**") {
		t.Fatalf("story comment:\n%s", body)
	}
	if cell := reviewCell(after, after.Findings, agent.ReviewRoleArchitecture); !strings.Contains(cell, "owner accepted: "+reason) {
		t.Fatalf("review cell %q", cell)
	}

	// A replay of the same reply writes nothing twice.
	all, _ := o.db.ListValidateVerdicts(t.Context(), o.instance.ID, after.Commit, true)
	if outcome, ok := replayedValidateReply(t.Context(), o.opts, after, f.Verdict.CommentID, false); !ok || outcome != "wait" || len(all) != 1 {
		t.Fatalf("replay %q %v verdicts %d", outcome, ok, len(all))
	}

	o.reply(o.owner, o.ship())
	nodes := o.toOwner()
	if o.final() != "delivered" {
		t.Fatalf("final %s path %s notice %q", o.final(), joinNodes(nodes), o.notice())
	}
	if len(code.bodies) != 1 || !strings.Contains(code.bodies[0], "Accepted deviations") || !strings.Contains(code.bodies[0], "`"+id+"`: "+reason) {
		t.Fatalf("pull request body: %q", code.bodies)
	}
}

// #942 C3: accept is refused for criteria, optional findings and non-owners,
// and a refusal records nothing.
func TestOwnerAcceptRefusals(t *testing.T) {
	for _, mode := range []string{"criterion", "optional", "non-owner", "stale"} {
		t.Run(mode, func(t *testing.T) {
			o := newOwnerFixture(t)
			id := "story-002:architecture"
			switch mode {
			case "criterion":
				id = "story-002:AC-002"
				o.reviewers.run = func(req ValidateReviewRequest) (*agentresult.Result, error) {
					res, err := deviationReview(req)
					for si := range res.Payload.Validate.Stories {
						for i := range res.Payload.Validate.Stories[si].Criteria {
							if c := &res.Payload.Validate.Stories[si].Criteria[i]; c.Name == "AC-002" {
								c.Status = "unmet"
							}
						}
					}
					return res, err
				}
			case "optional":
				id = "story-002:code"
				o.reviewers.run = mixedReview
			default:
				o.reviewers.run = deviationReview
			}
			o.toOwner()
			before := o.state()
			if f, _ := before.finding(id); mode != "optional" && mode != "criterion" && !f.acceptable() {
				t.Fatalf("fixture: %+v", f)
			}
			body := o.command(ValidateFindingCommand, id, "accept", "-- deliberate")
			switch mode {
			case "stale":
				body = strings.Replace(body, before.Revision, "000000000000", 1)
			case "criterion":
				if next := strings.Join(before.NextCommands(nil), "\n"); strings.Contains(next, id+" accept") {
					t.Fatalf("accept offered for a criterion: %s", next)
				}
			}
			if mode == "non-owner" {
				o.reply(o.owner+1, body)
				o.toOwner()
			} else {
				o.say(body)
			}
			all, err := o.db.ListValidateVerdicts(t.Context(), o.instance.ID, before.Commit, true)
			if err != nil || len(all) != 0 || o.state().Revision != before.Revision || o.notice() == "" {
				t.Fatalf("refusal changed state: %v %v %q", all, err, o.notice())
			}
			if (mode == "criterion" || mode == "optional") && !strings.Contains(o.notice(), "Accept is only for blocking review findings") {
				t.Fatalf("notice %q", o.notice())
			}
		})
	}
}
