package daemon_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// These tests drive the real Tick, the real store, the real reporter and an
// httptest GitHub; only the executor is fake. The fake executor retains
// proposals through the store seam the issues_create_prepare tool uses, so the
// hook is exercised end to end from bookkeeping row to forge comment.
//
// What they prove is the factory's behaviour against the offline fake forge. A
// real GitHub outage and its retry path are not observed here and remain an
// unverified obligation.

const (
	presentationTrigger = int64(40)
	presentationRepo    = testOwnerName + "/" + testRepoName
)

type presentationRig struct {
	*fixture
	now      time.Time
	creation *store.CreationStore
}

// newPresentationRig plants the cursor and registers the issue. The recovery
// interval is one minute so a test can prove an ineligible tick by not
// advancing the clock: the test clock moves a second per reading, well inside it.
func newPresentationRig(t *testing.T) *presentationRig {
	t.Helper()
	r := &presentationRig{fixture: newFixture(t), now: time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)}
	creation, err := store.NewCreationStore(r.db, func() time.Time { return r.now })
	if err != nil {
		t.Fatal(err)
	}
	r.creation = creation
	r.daemon = r.newDaemon(daemon.Options{Interval: time.Minute})
	r.plantCursor()
	r.issue("Draft a proposal")
	r.settle()
	return r
}

func (r *presentationRig) proposal(task, label string) store.CreationProposal {
	raw, err := json.Marshal(map[string]any{
		"version": 1, "repository": presentationRepo, "source_task": task, "relationship": "linked",
		"artifacts": []map[string]string{{"title": label, "body": "Exact publishable content"}},
	})
	if err != nil {
		r.t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	return store.CreationProposal{Hash: hex.EncodeToString(sum[:]), Repository: presentationRepo, SourceTask: task, Relationship: "linked", Artifacts: 1, Envelope: raw}
}

// hashOf is the hash of the proposal a turn with this label retains.
func (r *presentationRig) hashOf(label string) string {
	return r.proposal(r.session().TaskRef, label).Hash
}

type proposalTurn struct {
	graph   bool
	outcome harness.Outcome
	labels  []string
	after   func(ctx context.Context, session store.Session, run store.Run) error
}

// executor is a turn that finishes a real run and retains one proposal per
// label, exactly as the tool does, before returning its result.
func (r *presentationRig) executor(p proposalTurn) func(context.Context, turn.Request) (turn.Result, error) {
	return func(ctx context.Context, req turn.Request) (turn.Result, error) {
		// The zero outcome is a success.
		result, err := r.turnResult(ctx, req, turn.DispositionRan, p.outcome)
		if err != nil {
			return turn.Result{}, err
		}
		result.ResearchGraph = p.graph
		for _, label := range p.labels {
			if err := r.creation.RetainProposalForRun(ctx, r.proposal(result.Session.TaskRef, label), store.CreationPresentation{RunID: result.Run.ID}); err != nil {
				return turn.Result{}, err
			}
		}
		if p.after != nil {
			if err := p.after(ctx, result.Session, result.Run); err != nil {
				return turn.Result{}, err
			}
		}
		return result, nil
	}
}

// commentsContaining counts comments on the forge holding text: posted bodies
// and comments the test placed there.
func (r *presentationRig) commentsContaining(text string) int {
	posted, _, _ := r.forge.snapshot()
	n := 0
	for _, body := range posted {
		if strings.Contains(body, text) {
			n++
		}
	}
	r.forge.mu.Lock()
	defer r.forge.mu.Unlock()
	for _, c := range r.forge.state(testOwnerName, testRepoName).comments {
		if c.User.ID == botID && strings.Contains(c.Body, text) {
			n++
		}
	}
	return n
}

func (r *presentationRig) presentationComments() int { return r.commentsContaining("/mcp-create ") }

func (r *presentationRig) pending() (store.CreationPresentation, bool) {
	r.t.Helper()
	row, err := r.creation.PendingCreationPresentation(r.t.Context(), r.runs()[0].ID)
	if errors.Is(err, store.ErrNotFound) {
		return store.CreationPresentation{}, false
	}
	if err != nil {
		r.t.Fatal(err)
	}
	return row, true
}

func (r *presentationRig) creationRecovery() store.ReportRecovery {
	r.t.Helper()
	row, err := r.db.GetReportRecovery(r.t.Context(), fmt.Sprintf("creation:source:%d:%d", r.session().ID, presentationTrigger))
	if err != nil {
		r.t.Fatalf("no creation recovery row: %v\nlogs:\n%s", err, r.logs)
	}
	return row
}

func (r *presentationRig) comment() {
	r.forge.addComment(testIssue, presentationTrigger, owner(), "/research inspect the boundaries")
}

// AC-016 and AC-044 (legacy branch): one presentation, after the run's own
// result, showing the retained envelope, hash and approval command.
func TestCreationPresentationLegacyRunPostsOnce(t *testing.T) {
	r := newPresentationRig(t)
	r.turns.set(r.executor(proposalTurn{labels: []string{"legacy"}}))
	r.comment()
	r.settle()

	hash := r.hashOf("legacy")
	posted, _, _ := r.forge.snapshot()
	if len(posted) != 2 {
		t.Fatalf("posts = %d, want the run result and one presentation\nlogs:\n%s", len(posted), r.logs)
	}
	if strings.Contains(posted[0], "/mcp-create ") {
		t.Fatalf("first post is the presentation, want the run result first: %q", posted[0])
	}
	for _, want := range []string{"/mcp-create " + hash + " linked", hash, `"title":"legacy"`} {
		if !strings.Contains(posted[1], want) {
			t.Fatalf("presentation lacks %q:\n%s", want, posted[1])
		}
	}
	if _, owed := r.pending(); owed {
		t.Fatal("presentation still owed after it was posted")
	}
	runs := r.runs()
	if len(runs) != 1 || runs[0].Status != store.RunReported || runs[0].ResultCommentID == 0 {
		t.Fatalf("the presentation disturbed the run result: %+v", runs)
	}
	if r.watermark() != presentationTrigger {
		t.Fatalf("watermark = %d, want %d", r.watermark(), presentationTrigger)
	}
	r.settle()
	r.settle()
	if got := r.presentationComments(); got != 1 {
		t.Fatalf("presentation comments after later ticks = %d, want 1", got)
	}
}

// AC-017 and AC-044 (graph branch): the graph run's trigger is consumed with no
// whole-turn report, and the node's proposal is then presented once.
func TestCreationPresentationGraphRunPostsOnce(t *testing.T) {
	r := newPresentationRig(t)
	r.turns.set(r.executor(proposalTurn{graph: true, labels: []string{"graph"}}))
	r.comment()
	r.settle()

	posted, _, _ := r.forge.snapshot()
	if len(posted) != 1 || !strings.Contains(posted[0], "/mcp-create "+r.hashOf("graph")+" linked") {
		t.Fatalf("posts = %q, want exactly the presentation\nlogs:\n%s", posted, r.logs)
	}
	if _, owed := r.pending(); owed {
		t.Fatal("presentation still owed")
	}
	runs := r.runs()
	if len(runs) != 1 || runs[0].Status != store.RunSucceeded || runs[0].ResultCommentID != 0 {
		t.Fatalf("the presentation became the run's result: %+v", runs)
	}
	if r.watermark() != presentationTrigger {
		t.Fatalf("watermark = %d, want %d", r.watermark(), presentationTrigger)
	}
	r.settle()
	if got := r.presentationComments(); got != 1 {
		t.Fatalf("presentation comments after a later tick = %d, want 1", got)
	}
}

// AC-019: nothing retained, or a row already presented, posts nothing.
func TestCreationPresentationNothingOwedPostsNothing(t *testing.T) {
	t.Run("no proposal retained", func(t *testing.T) {
		r := newPresentationRig(t)
		r.comment()
		r.settle()
		posted, _, _ := r.forge.snapshot()
		if len(posted) != 1 || r.presentationComments() != 0 {
			t.Fatalf("posts = %q, want only the run result", posted)
		}
	})
	t.Run("already presented", func(t *testing.T) {
		r := newPresentationRig(t)
		r.turns.set(r.executor(proposalTurn{labels: []string{"seen"}, after: func(ctx context.Context, _ store.Session, run store.Run) error {
			if err := r.creation.BindCreationPresentationDelivery(ctx, run.ID, "earlier-delivery"); err != nil {
				return err
			}
			return r.creation.MarkCreationPresented(ctx, run.ID)
		}}))
		r.comment()
		r.settle()
		posted, _, _ := r.forge.snapshot()
		if len(posted) != 1 || r.presentationComments() != 0 {
			t.Fatalf("posts = %q, want only the run result", posted)
		}
	})
}

// AC-024: two proposals in one run produce one comment showing the later one.
func TestCreationPresentationTwoProposalsShowTheLater(t *testing.T) {
	r := newPresentationRig(t)
	r.turns.set(r.executor(proposalTurn{labels: []string{"first draft", "second draft"}}))
	r.comment()
	r.settle()

	if got := r.presentationComments(); got != 1 {
		t.Fatalf("presentation comments = %d, want 1\nlogs:\n%s", got, r.logs)
	}
	if r.commentsContaining(r.hashOf("second draft")) != 1 || r.commentsContaining(r.hashOf("first draft")) != 0 {
		t.Fatal("the presentation does not show exactly the later proposal")
	}
}

// AC-044: a run that retained a proposal and then failed still presents, and
// the failure stays the run's outcome.
func TestCreationPresentationFailedRunStillPresents(t *testing.T) {
	r := newPresentationRig(t)
	r.turns.set(r.executor(proposalTurn{outcome: harness.OutcomeFailed, labels: []string{"before failure"}}))
	r.comment()
	r.settle()

	if got := r.commentsContaining("/mcp-create " + r.hashOf("before failure") + " linked"); got != 1 {
		t.Fatalf("presentations = %d, want 1\nlogs:\n%s", got, r.logs)
	}
	runs := r.runs()
	if len(runs) != 1 || runs[0].ExitCode == nil || *runs[0].ExitCode != 1 || runs[0].Status != store.RunReported {
		t.Fatalf("the presentation rewrote the failed outcome: %+v", runs)
	}
	if r.watermark() != presentationTrigger {
		t.Fatalf("watermark = %d, want %d", r.watermark(), presentationTrigger)
	}
}

// AC-020: a forge failure at presentation leaves presented = 0 with recovery
// debt under creation:<session>:<trigger>, does not disturb the run or its
// consumed trigger, and the next eligible tick completes exactly one comment.
//
// The POST is failed, then the comment is placed on the forge as though it had
// been accepted with its response lost: the existing delivery journal never
// re-sends an uncertain comment and finishes it from the receipt it finds.
func TestCreationPresentationForgeFailureRetriesOnTheNextEligibleTick(t *testing.T) {
	r := newPresentationRig(t)
	r.turns.set(r.executor(proposalTurn{labels: []string{"outage"}}))
	r.forge.onPostComment = func(w http.ResponseWriter, call int) bool {
		if call != 2 { // the run result is call 1; the presentation is call 2
			return false
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "outage"})
		return true
	}
	r.comment()
	r.settle()

	hash := r.hashOf("outage")
	row, owed := r.pending()
	if !owed || row.DeliveryKey == "" {
		t.Fatalf("row after failure = %+v owed=%v, want owed with its delivery bound", row, owed)
	}
	debt := r.creationRecovery()
	if debt.State == "resolved" || debt.RunID != r.runs()[0].ID || debt.NextAt.IsZero() || debt.DeliveryKey != row.DeliveryKey {
		t.Fatalf("recovery debt = %+v, want unresolved, this run, due later, delivery %s", debt, row.DeliveryKey)
	}
	if runs := r.runs(); len(runs) != 1 || runs[0].Status != store.RunReported || r.watermark() != presentationTrigger {
		t.Fatalf("the failed presentation disturbed the run or its trigger: %+v watermark=%d", runs, r.watermark())
	}
	if !r.session().NeedsPoll {
		t.Fatal("the session lost its backlog flag while a presentation is owed")
	}

	// Not yet due: the attempt is not repeated.
	r.forge.onPostComment = nil
	r.settle()
	if again := r.creationRecovery(); again.Attempts != debt.Attempts || !again.NextAt.Equal(debt.NextAt) {
		t.Fatalf("an ineligible tick retried: %+v then %+v", debt, again)
	}
	if _, owed := r.pending(); !owed || r.commentsContaining(hash) != 0 {
		t.Fatal("presentation changed before it was due")
	}

	delivery, found, err := r.db.FindPendingReportDelivery(t.Context(), r.session().ID, presentationTrigger)
	if err != nil || !found {
		t.Fatalf("pending presentation delivery: found=%v err=%v", found, err)
	}
	r.forge.addCommentAt(testIssue, 900040, bot(), delivery.Body, r.clock.Now())
	r.clock.advance(2 * time.Minute)
	r.settle()

	if _, owed := r.pending(); owed {
		t.Fatalf("presentation still owed after the eligible tick\nlogs:\n%s", r.logs)
	}
	if got := r.commentsContaining(hash); got != 1 {
		t.Fatalf("comments carrying the proposal = %d, want exactly 1", got)
	}
	if resolved := r.creationRecovery(); resolved.State != "resolved" {
		t.Fatalf("recovery debt after success = %+v", resolved)
	}
	r.settle()
	if got := r.commentsContaining(hash); got != 1 {
		t.Fatalf("comments carrying the proposal after a later tick = %d, want 1", got)
	}
}

// seedingReporter delegates to the real reporter and, once the run's own result
// is reported, opens a competing unresolved delivery on the same trigger. It
// cannot be seeded earlier: the run's report resumes any pending purpose on its
// trigger, so an earlier one would be delivered as the run's result instead.
type seedingReporter struct {
	*turn.Reporter
	seed func(ctx context.Context, result turn.Result) error
}

func (s seedingReporter) ReportTurn(ctx context.Context, result turn.Result) (turn.Posted, error) {
	posted, err := s.Reporter.ReportTurn(ctx, result)
	if err != nil {
		return posted, err
	}
	return posted, s.seed(ctx, result)
}

// AC-022: a second unresolved delivery on the same session and trigger defers
// the presentation instead of minting a second open purpose, and the
// presentation posts on a later eligible tick once the first is resolved.
func TestCreationPresentationDefersToAnUnresolvedDelivery(t *testing.T) {
	r := newPresentationRig(t)
	const competing = "competing-delivery"
	seeded := false
	r.daemon = r.newDaemon(daemon.Options{Interval: time.Minute, Reporter: seedingReporter{Reporter: r.reporter, seed: func(ctx context.Context, result turn.Result) error {
		if seeded {
			return nil
		}
		seeded = true
		marker := "<!-- lf:delivery v=1 key=" + competing + " -->"
		_, err := r.db.EnsureReportDelivery(ctx, store.ReportDelivery{Key: competing, Version: 1, SessionID: result.SessionID, TriggerID: presentationTrigger, Task: result.Session.TaskRef, Destination: result.Session.TaskRef, TriggerNamespace: "comment", Purpose: "reply", BotUserID: botID, Body: "Competing explanation\n\n" + marker, Marker: marker})
		return err
	}}})
	r.turns.set(r.executor(proposalTurn{labels: []string{"deferred"}}))
	r.comment()
	r.settle()

	if got := r.presentationComments(); got != 0 {
		t.Fatalf("presentation minted beside an unresolved delivery: %d comments\nlogs:\n%s", got, r.logs)
	}
	row, owed := r.pending()
	if !owed || row.DeliveryKey != "" {
		t.Fatalf("row = %+v owed=%v, want owed and not yet bound to a delivery", row, owed)
	}
	if debt := r.creationRecovery(); debt.State == "resolved" || !strings.Contains(debt.Reason, "unresolved") {
		t.Fatalf("deferral recorded no debt: %+v", debt)
	}
	pending, found, err := r.db.FindPendingReportDelivery(t.Context(), r.session().ID, presentationTrigger)
	if err != nil || !found || pending.Key != competing {
		t.Fatalf("the unresolved purposes are not exactly the competing one: %+v found=%v err=%v", pending, found, err)
	}

	if _, err := r.db.MarkReportDeliverySent(t.Context(), competing); err != nil {
		t.Fatal(err)
	}
	if _, err := r.db.ConfirmReportDelivery(t.Context(), competing, 901); err != nil {
		t.Fatal(err)
	}
	if _, err := r.db.CompleteReportDelivery(t.Context(), competing); err != nil {
		t.Fatal(err)
	}
	r.clock.advance(2 * time.Minute)
	r.settle()

	if got := r.presentationComments(); got != 1 {
		t.Fatalf("presentation comments after the deferral cleared = %d, want 1\nlogs:\n%s", got, r.logs)
	}
	if _, owed := r.pending(); owed {
		t.Fatal("presentation still owed")
	}
}

// plainReporter carries only the daemon's own Reporter interface, so it lacks
// the presentation capability.
type plainReporter struct{ daemon.Reporter }

// AC-045: a bookkeeping row with a reporter that cannot present fails loudly.
func TestCreationPresentationWithoutCapabilityFailsLoudly(t *testing.T) {
	r := newPresentationRig(t)
	r.daemon = r.newDaemon(daemon.Options{Interval: time.Minute, Reporter: plainReporter{r.reporter}})
	r.turns.set(r.executor(proposalTurn{labels: []string{"stranded"}}))
	r.comment()
	r.settle()

	if got := r.presentationComments(); got != 0 {
		t.Fatalf("a presentation was posted without the capability: %d", got)
	}
	if _, owed := r.pending(); !owed {
		t.Fatal("the row was dropped instead of left owed")
	}
	logs := r.logs.String()
	if !strings.Contains(logs, "owed but the reporter cannot present it") || !strings.Contains(logs, "level=ERROR") {
		t.Fatalf("the missing capability was not reported as an error:\n%s", logs)
	}
	if runs := r.runs(); len(runs) != 1 || runs[0].Status != store.RunReported {
		t.Fatalf("the run result was disturbed: %+v", runs)
	}
}

// presentedProposal runs a turn whose proposal is presented, and returns its
// hash. The store clock is left at the retention instant.
func presentedProposal(t *testing.T) (*presentationRig, string) {
	t.Helper()
	r := newPresentationRig(t)
	r.turns.set(r.executor(proposalTurn{labels: []string{"approve me"}}))
	r.comment()
	r.settle()
	hash := r.hashOf("approve me")
	if r.commentsContaining("/mcp-create "+hash+" linked") != 1 {
		t.Fatalf("proposal was not presented\nlogs:\n%s", r.logs)
	}
	return r, hash
}

// AC-026: approval still records intent exactly as before, within 30 minutes of
// retention only and only for the current hash. Nothing here launches a run.
// That the recorded intent gives the next Research attempt a bound grant and
// issues_create_batch is proven by the unchanged turn-level grant tests.
func TestCreationPresentationApprovalRules(t *testing.T) {
	authorizer := func(t *testing.T, r *presentationRig) *turn.CreationAuthorizer {
		t.Helper()
		a, err := turn.NewCreationAuthorizer(turn.CreationAuthorizerOptions{Store: r.creation, Enabled: true, OwnerID: ownerID, BotID: botID})
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	repo := github.Repo{Owner: testOwnerName, Name: testRepoName}
	approve := func(r *presentationRig, hash string, id int64, at time.Time) (int64, error) {
		return daemon.AdmitMCPOwnerCommand(t.Context(), authorizer(t, r), repo, testIssue, "research", github.Comment{ID: id, Body: "/mcp-create " + hash + " linked", Author: github.User{ID: ownerID, Login: ownerLogin}, CreatedAt: at})
	}

	t.Run("within 30 minutes", func(t *testing.T) {
		r, hash := presentedProposal(t)
		retained := r.now
		r.now = retained.Add(10 * time.Minute)
		calls := len(r.turns.calls())
		intent, err := approve(r, hash, 60, retained.Add(time.Minute))
		if err != nil || intent < 1 {
			t.Fatalf("approval of the presented hash: %d %v", intent, err)
		}
		if got := len(r.turns.calls()); got != calls {
			t.Fatalf("approval launched a run: %d executor calls, want %d", got, calls)
		}
	})
	t.Run("after 30 minutes", func(t *testing.T) {
		r, hash := presentedProposal(t)
		retained := r.now
		r.now = retained.Add(31 * time.Minute)
		if intent, err := approve(r, hash, 61, retained.Add(time.Minute)); err == nil {
			t.Fatalf("stale approval recorded intent %d", intent)
		}
	})
	t.Run("superseded hash", func(t *testing.T) {
		r, hash := presentedProposal(t)
		retained := r.now
		r.now = retained.Add(5 * time.Minute)
		later := r.proposal(r.session().TaskRef, "superseding draft")
		if err := r.creation.RetainProposal(t.Context(), later); err != nil {
			t.Fatal(err)
		}
		if intent, err := approve(r, hash, 62, retained.Add(time.Minute)); err == nil {
			t.Fatalf("superseded hash recorded intent %d", intent)
		}
		if intent, err := approve(r, later.Hash, 63, r.now); err != nil || intent < 1 {
			t.Fatalf("the current hash was refused: %d %v", intent, err)
		}
	})
}
