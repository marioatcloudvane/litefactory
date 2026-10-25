package turn

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/store"
)

// stepTo runs the graph until the instance rests on the named node.
func (f *flowFixture) stepTo(node string) {
	f.t.Helper()
	for range 40 {
		if f.currentNode() == node {
			return
		}
		if _, err := f.step(); err != nil {
			f.t.Fatal(err)
		}
	}
	f.t.Fatalf("never reached %s", node)
}

func (f *flowFixture) reviewRows() map[string]store.ValidateReview {
	f.t.Helper()
	rows, err := f.db.ListValidateReviews(f.t.Context(), f.instance.ID, f.base)
	if err != nil {
		f.t.Fatal(err)
	}
	out := map[string]store.ValidateReview{}
	for _, r := range rows {
		out[r.Role] = r
	}
	return out
}

// AC-005: a reviewer without a slot makes the step return retry, keeps the
// recorded results and starts only the missing reviewers on the next dispatch.
func TestValidateReviewWithLimitedSlotsRetriesAndNeverStartsAReviewerTwice(t *testing.T) {
	f := newFlowFixture(t)
	f.slots.free = 1
	f.stepTo("review")
	var order []string
	for range 3 {
		res, err := f.step()
		if err != nil {
			t.Fatal(err)
		}
		order = append(order, res.Activity.Outcome)
	}
	if strings.Join(order, ",") != "retry,retry,done" {
		t.Fatalf("outcomes %v", order)
	}
	// One slot, three reviewers: one per dispatch, in order, none twice.
	if got := strings.Join(f.reviewers.roles(), ","); got != "acceptance,code,architecture" {
		t.Fatalf("started %s", got)
	}
	for role, row := range f.reviewRows() {
		if row.State != store.ReviewAccepted {
			t.Fatalf("%s is %s", role, row.State)
		}
	}
	if f.slots.free != 1 {
		t.Fatalf("slots leaked: %d free", f.slots.free)
	}
	f.runToEnd()
	if f.final() != "assessed" || len(f.reviewers.roles()) != 3 {
		t.Fatalf("final %s started %v", f.final(), f.reviewers.roles())
	}
}

// AC-005: with no free slot at all nothing starts and no checkout is spent.
func TestValidateReviewWithNoSlotStartsNothing(t *testing.T) {
	f := newFlowFixture(t)
	f.slots.free = 0
	f.stepTo("review")
	opened := f.suiteRepo.begun
	res, err := f.step()
	if err != nil || res.Activity.Outcome != "retry" {
		t.Fatalf("%+v %v", res.Activity, err)
	}
	if len(f.reviewers.roles()) != 0 || f.suiteRepo.begun != opened || f.currentNode() != "review" {
		t.Fatalf("started %v checkouts %d->%d at %s", f.reviewers.roles(), opened, f.suiteRepo.begun, f.currentNode())
	}
}

// AC-009: a lost reply is resolved through the retained result before any retry.
func TestValidateReviewLostReplyIsRecoveredNotRerun(t *testing.T) {
	f := newFlowFixture(t)
	f.stepTo("review")
	// The code reviewer submitted but its reply was lost: the run returned an error.
	attempt := validateReviewAttempt(f.instance.ID, "code", f.base)
	var codeRuns int
	f.reviewers.run = func(req ValidateReviewRequest) (*agentresult.Result, error) {
		if req.Role == "code" {
			codeRuns++
			f.reviewers.mu.Lock()
			f.reviewers.retained[req.Attempt] = goodReview(req)
			f.reviewers.mu.Unlock()
			return nil, errors.New("connection reset after the submit")
		}
		return goodReview(req), nil
	}
	res, err := f.step()
	if err != nil || res.Activity.Outcome != "done" {
		t.Fatalf("%+v %v", res.Activity, err)
	}
	rows := f.reviewRows()
	if rows["code"].State != store.ReviewAccepted || rows["code"].Attempt != attempt || codeRuns != 1 {
		t.Fatalf("code row %+v runs %d", rows["code"], codeRuns)
	}
	if len(f.reviewers.recovered) != 1 || f.reviewers.recovered[0] != attempt {
		t.Fatalf("recovered %v", f.reviewers.recovered)
	}
}

// AC-009: after a restart a pending row with a retained result is recorded and
// only the missing reviewers start.
func TestValidateReviewAfterRestartReturnsRecordedAndStartsOnlyMissing(t *testing.T) {
	f := newFlowFixture(t)
	f.stepTo("review")
	// Before the restart the acceptance reviewer was recorded, the code reviewer
	// was pending with a retained result, the architecture reviewer never began.
	acc, _, err := f.db.BeginValidateReview(t.Context(), store.ValidateReview{InstanceID: f.instance.ID, Role: "acceptance", Commit: f.base, Attempt: "a-acc", StartedAt: f.clock})
	if err != nil {
		t.Fatal(err)
	}
	findings, _ := json.Marshal([]agentresult.AssessmentStory{{Story: "story-002", SnapshotID: "commit-" + f.base}})
	if err := f.db.FinishValidateReview(t.Context(), acc.ID, store.ReviewAccepted, "", findings, f.clock); err != nil {
		t.Fatal(err)
	}
	attempt := validateReviewAttempt(f.instance.ID, "code", f.base)
	if _, _, err := f.db.BeginValidateReview(t.Context(), store.ValidateReview{InstanceID: f.instance.ID, Role: "code", Commit: f.base, Attempt: attempt, StartedAt: f.clock}); err != nil {
		t.Fatal(err)
	}
	f.reviewers.retained[attempt] = goodReview(ValidateReviewRequest{Role: "code", Stories: f.stories, SnapshotID: "commit-" + f.base})
	res, err := f.step()
	if err != nil || res.Activity.Outcome != "done" {
		t.Fatalf("%+v %v", res.Activity, err)
	}
	if got := strings.Join(f.reviewers.roles(), ","); got != "architecture" {
		t.Fatalf("started %q, want only the architecture reviewer", got)
	}
	rows := f.reviewRows()
	if rows["code"].State != store.ReviewAccepted || rows["acceptance"].State != store.ReviewAccepted {
		t.Fatalf("rows %+v", rows)
	}
}

// AC-006: a missing, failed or malformed reviewer result stays unresolved and is
// never satisfied; the other parts are kept.
func TestValidateUnusableReviewerResultsStayUnresolved(t *testing.T) {
	f := newFlowFixture(t)
	f.suiteExit = func(string) int { return 1 }
	f.agent = func(int, *flowFixture) (agentresult.ValidateRepair, ActivityResult) {
		return handbackReport("x"), ActivityResult{}
	}
	f.reviewers.run = func(req ValidateReviewRequest) (*agentresult.Result, error) {
		switch req.Role {
		case "acceptance":
			// Claims proof from a command that failed: refused as malformed.
			bad := goodReview(req)
			for i := range bad.Payload.Validate.Stories[0].Criteria {
				bad.Payload.Validate.Stories[0].Criteria[i] = agent.ContributionFinding{Name: bad.Payload.Validate.Stories[0].Criteria[i].Name, Status: "satisfied", Classification: "blocking", Evidence: []agent.EvidenceExplanation{{Summary: "It passes.", CheckIDs: []string{req.Evidence[0].ID}}}}
			}
			return bad, nil
		case "code":
			return nil, errors.New("the reviewer crashed")
		}
		return goodReview(req), nil
	}
	f.runToEnd()
	rows := f.reviewRows()
	if rows["acceptance"].State != store.ReviewMalformed || rows["code"].State != store.ReviewFailed || rows["architecture"].State != store.ReviewAccepted {
		t.Fatalf("rows %+v", rows)
	}
	if string(rows["acceptance"].Findings) != "[]" || string(rows["code"].Findings) != "[]" {
		t.Fatal("an unusable reviewer kept findings")
	}
	var joined []ValidateJoinedStory
	if err := json.Unmarshal(f.outcome(f.base).Assessment, &joined); err != nil {
		t.Fatal(err)
	}
	j := joined[0]
	if strings.Join(j.Unresolved, ",") != "acceptance,code" {
		t.Fatalf("unresolved %v", j.Unresolved)
	}
	for _, c := range j.Criteria {
		if c.Status != "unverified" || !strings.Contains(c.Evidence[0].Summary, "Unresolved") {
			t.Fatalf("criterion %+v", c)
		}
	}
	byName := map[string]agent.ContributionFinding{}
	for _, r := range j.Reviews {
		byName[r.Name] = r
	}
	if !strings.Contains(byName["plan"].Evidence[0].Summary, "Unresolved") || !strings.Contains(byName["code"].Evidence[0].Summary, "Unresolved") || strings.Contains(byName["architecture"].Evidence[0].Summary, "Unresolved") {
		t.Fatalf("reviews %+v", byName)
	}
}

// AC-006: a reviewer that cannot finish records failed and leaves its part open.
func TestValidateReviewerThatCannotFinishIsFailed(t *testing.T) {
	f := newFlowFixture(t)
	f.reviewers.run = func(req ValidateReviewRequest) (*agentresult.Result, error) {
		return &agentresult.Result{Outcome: "blocked", Comment: "no", Reason: "the checkout is empty", Payload: agentresult.Payload{Validate: &agentresult.Validate{}}}, nil
	}
	f.runToEnd()
	for role, row := range f.reviewRows() {
		if row.State != store.ReviewFailed || !strings.Contains(row.Reason, "checkout is empty") {
			t.Fatalf("%s %+v", role, row)
		}
	}
	if f.final() != "assessed" {
		t.Fatalf("final %s", f.final())
	}
}

// AC-005: a source change between the suite and the review is drift: nothing is
// reviewed, every part stays unresolved.
func TestValidateReviewOnADriftedCheckoutStartsNobody(t *testing.T) {
	f := newFlowFixture(t)
	f.stepTo("review")
	f.suiteRepo.trees = []string{"tree-other"}
	f.suiteRepo.observed = 0
	res, err := f.step()
	if err != nil || res.Activity.Outcome != "done" {
		t.Fatalf("%+v %v", res.Activity, err)
	}
	if len(f.reviewers.roles()) != 0 {
		t.Fatalf("reviewers started on a drifted checkout: %v", f.reviewers.roles())
	}
	for role, row := range f.reviewRows() {
		if row.State != store.ReviewFailed || !strings.Contains(row.Reason, "drift") {
			t.Fatalf("%s %+v", role, row)
		}
	}
}

// #914: a symlink refusal while observing the review checkout is a repository
// finding: every reviewer is recorded as failed with the reason, nobody runs,
// and Validate ends blocked with that reason instead of an unconductable turn.
func TestValidateReviewSymlinkRefusalEndsBlocked(t *testing.T) {
	f := newFlowFixture(t)
	refusal := &git.SourceSymlinkError{Path: "alias/file", Reason: "has a symlink source parent"}
	f.suiteRepo.observeErr = refusal
	nodes := f.runToEnd()
	if f.final() != "blocked" || visits(nodes, "review") != 1 {
		t.Fatalf("path %s final %s", joinNodes(nodes), f.final())
	}
	if len(f.reviewers.roles()) != 0 {
		t.Fatalf("reviewers started on a refused checkout: %v", f.reviewers.roles())
	}
	rows := f.reviewRows()
	if len(rows) != len(ValidateReviewRoles) {
		t.Fatalf("rows %+v", rows)
	}
	for role, row := range rows {
		if row.State != store.ReviewFailed || row.Reason != refusal.Error() {
			t.Fatalf("%s %+v", role, row)
		}
	}
	o := f.outcome(f.base)
	if o.Outcome != store.ValidateBlocked || o.Reason != refusal.Error() {
		t.Fatalf("outcome %+v", o)
	}
}

// Observation failures other than a symlink refusal stay fatal for the review.
func TestValidateReviewObservationFailureStaysFatal(t *testing.T) {
	f := newFlowFixture(t)
	f.stepTo("review")
	cause := errors.New("I/O failure")
	f.suiteRepo.observeErr = cause
	if _, err := f.step(); !errors.Is(err, cause) {
		t.Fatalf("got %v", err)
	}
	if len(f.reviewers.roles()) != 0 {
		t.Fatalf("reviewers started: %v", f.reviewers.roles())
	}
}

// AC-005: a shutdown during the review is not the reviewer's failure.
func TestValidateReviewShutdownLeavesTheRowPending(t *testing.T) {
	f := newFlowFixture(t)
	f.stepTo("review")
	ctx, cancel := context.WithCancel(t.Context())
	f.reviewers.run = func(req ValidateReviewRequest) (*agentresult.Result, error) {
		cancel()
		return nil, ctx.Err()
	}
	_, err := ValidateReviewActivity{b: f.bindings()}.Execute(ctx, f.opts, f.reviewRequest())
	if err == nil {
		t.Fatal("a shutdown was swallowed")
	}
	for role, row := range f.reviewRows() {
		if row.State != store.ReviewPending {
			t.Fatalf("%s was recorded as %s", role, row.State)
		}
	}
}

// #1182 C1: after a failed suite every reviewer gets the failed command's
// output tail under output_tail; passed commands carry none.
func TestValidateReviewEvidenceCarriesTheFailedOutputTail(t *testing.T) {
	f := newFlowFixture(t)
	f.suiteExit = func(string) int { return 1 }
	f.agent = func(int, *flowFixture) (agentresult.ValidateRepair, ActivityResult) {
		return handbackReport("the sum is wrong"), ActivityResult{}
	}
	f.runToEnd()
	if len(f.reviewers.started) == 0 {
		t.Fatal("no reviewer started")
	}
	for _, req := range f.reviewers.started {
		failed := 0
		for _, e := range req.Evidence {
			if e.State == store.SuitePassed {
				if e.OutputTail != "" {
					t.Fatalf("%s: a passed command carries output: %+v", req.Role, e)
				}
				continue
			}
			failed++
			if !strings.Contains(e.OutputTail, "TestTotal") {
				t.Fatalf("%s: the failed command's tail is missing: %+v", req.Role, e)
			}
			raw, err := json.Marshal(e)
			if err != nil || !strings.Contains(string(raw), `"output_tail":`) {
				t.Fatalf("%s: evidence JSON %s %v", req.Role, raw, err)
			}
		}
		if failed == 0 {
			t.Fatalf("%s: no failed evidence: %+v", req.Role, req.Evidence)
		}
	}
	long := strings.Repeat("ä", validateEvidenceTailBytes) + "TestX failed"
	tail := tailText(long, validateEvidenceTailBytes)
	if len(tail) > validateEvidenceTailBytes || !strings.HasSuffix(tail, "TestX failed") || !utf8ValidPrefix(tail) {
		t.Fatalf("tail is not bounded or not valid UTF-8: %d bytes", len(tail))
	}
}
