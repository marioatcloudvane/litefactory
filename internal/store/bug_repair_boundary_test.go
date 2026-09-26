package store

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// These fixtures execute public storage operations against real SQLite. Runner
// and publication observations are controlled inputs, not provider/forge proof.
func repairBoundaryApproved(t *testing.T, db *DB) (BugIdentity, BugAnalysisRevision) {
	t.Helper()
	identity := bugBoundaryIdentity(t, db, 392)
	revision, _, input := bugBoundaryApply(t, db, identity, 0, "done", bugBoundaryCompleteAnalysis())
	attempt, err := db.GetGraphNodeAttempt(t.Context(), input.GraphAttemptID)
	if err != nil {
		t.Fatal(err)
	}
	key := "bug-analysis-review"
	body := "Exact analysis review: " + revision.Digest
	if err := db.RetainGraphComment(t.Context(), GraphCommentIntent{Key: key, InstanceID: attempt.InstanceID, SourceAttemptID: attempt.ID, Node: "review", Kind: "review", Candidate: []byte(`{"review":"analysis"}`)}); err != nil {
		t.Fatal(err)
	}
	review := BugAnalysisReview{SessionID: identity.SessionID, Review: "review-1", AnalysisRevision: revision.Revision, AnalysisDigest: revision.Digest, DeliveryKey: key, ContentDigest: submissionDigest([]byte(body))}
	if err := db.RetainBugAnalysisReview(t.Context(), review, bugBoundaryState(t, db, identity.SessionID).Revision); err != nil {
		t.Fatal(err)
	}
	task := "github:acme/api#392"
	reply := GraphOwnerReply{InstanceID: attempt.InstanceID, OwnerID: 42, AuthorID: 42, CommentID: 102, QuestionCommentID: 101, TaskRef: task, SourceRef: task, DeliveryKey: key, Body: "/approve-bug analysis 1", At: graphTestTime()}
	if _, err := db.ApproveBugAnalysis(t.Context(), identity.SessionID, review.Review, reply, bugBoundaryState(t, db, identity.SessionID).Revision); err == nil {
		t.Fatal("unpublished review approved")
	}
	run, err := db.GetRun(t.Context(), attempt.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.EnsureReportDelivery(t.Context(), ReportDelivery{Key: key, SessionID: identity.SessionID, RunID: run.ID, TriggerID: run.TriggerCommentID, Task: task, Destination: task, TriggerNamespace: "graph", Purpose: "review", BotUserID: 99, Body: body, Marker: "fixture"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ConfirmReportDelivery(t.Context(), key, 101); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CompleteReportDelivery(t.Context(), key); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ConfirmBugAnalysisReview(t.Context(), identity.SessionID, review.Review, 100); err == nil {
		t.Fatal("wrong publisher confirmed review")
	}
	if _, err := db.ConfirmBugAnalysisReview(t.Context(), identity.SessionID, review.Review, 99); err != nil {
		t.Fatal(err)
	}

	state, _, _, err := db.CurrentBugAnalysis(t.Context(), identity.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ApproveBugAnalysis(t.Context(), identity.SessionID, review.Review, reply, state.Revision); err != nil {
		t.Fatal(err)
	}
	return identity, revision
}

func repairBoundaryRevision(t *testing.T, db *DB, session int64) int64 {
	t.Helper()
	state, _, _, err := db.CurrentBugAnalysis(t.Context(), session)
	if err != nil {
		t.Fatal(err)
	}
	return state.Revision
}

func repairBoundaryRequest(t *testing.T, db *DB, identity BugIdentity, analysis BugAnalysisRevision, n int, cause string) (BugRepairRequest, Run) {
	t.Helper()
	pass, run := bugBoundaryPass(t, db, identity.SessionID, n+10)
	return BugRepairRequest{SessionID: identity.SessionID, Request: fmt.Sprintf("repair-%d", n), AnalysisRevision: analysis.Revision, AnalysisDigest: analysis.Digest, ApprovalReview: "review-1", Cause: cause, Evidence: "retained cause", SourceCommit: strings.Repeat("a", 40), SourceTree: strings.Repeat("b", 40), GraphAttemptID: pass.GraphAttemptID}, run
}

func TestBugRepairBoundaryLifetimeBudgetSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "repair.db")
	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	identity, analysis := repairBoundaryApproved(t, db)
	var requests []BugRepairRequest
	for n, cause := range []string{"initial", "test_failure", "pr_feedback"} {
		req, _ := repairBoundaryRequest(t, db, identity, analysis, n, cause)
		r, err := db.ReserveBugRepair(t.Context(), req, repairBoundaryRevision(t, db, identity.SessionID))
		if err != nil || r.Ordinal != n+1 {
			t.Fatalf("reserve: %+v %v", r, err)
		}
		if err = db.StopBugRepair(t.Context(), identity.SessionID, req.Request, []string{"failed", "blocked", "failed"}[n], "retained obstacle", true); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, req)
		if err = db.Close(); err != nil {
			t.Fatal(err)
		}
		db, err = Open(t.Context(), path)
		if err != nil {
			t.Fatal(err)
		}
	}
	fourth, _ := repairBoundaryRequest(t, db, identity, analysis, 3, "test_failure")
	if _, err := db.ReserveBugRepair(t.Context(), fourth, repairBoundaryRevision(t, db, identity.SessionID)); !errors.Is(err, ErrBugRepairExhausted) {
		t.Fatalf("fourth reservation: %v", err)
	}
	for n, req := range requests {
		r, err := db.ReserveBugRepair(t.Context(), req, 1)
		if err != nil || r.Ordinal != n+1 || r.Reason != "retained obstacle" {
			t.Fatalf("replay: %+v %v", r, err)
		}
		changed := req
		changed.Evidence = "conflicting input"
		if _, err := db.ReserveBugRepair(t.Context(), changed, 1); err == nil {
			t.Fatal("conflicting replay admitted")
		}
	}
	state, _, _, err := db.CurrentBugAnalysis(t.Context(), identity.SessionID)
	if err != nil || state.AnalysisUsed != 1 {
		t.Fatalf("repair changed analysis usage: %+v %v", state, err)
	}
}

func TestBugRepairBoundaryConcurrentClaimAndUncertainGuard(t *testing.T) {
	db := newTestDB(t)
	identity, analysis := repairBoundaryApproved(t, db)
	req, run := repairBoundaryRequest(t, db, identity, analysis, 0, "initial")
	revision := repairBoundaryRevision(t, db, identity.SessionID)
	const count = 12
	var wg sync.WaitGroup
	errs := make(chan error, count)
	for range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := db.ReserveBugRepair(t.Context(), req, revision)
			if err == nil && r.Ordinal != 1 {
				err = fmt.Errorf("ordinal %d", r.Ordinal)
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, kind := range []string{"save", "checks", "child"} {
		if ok, err := db.ClaimBugWork(t.Context(), identity.SessionID, kind, kind); err != nil || ok {
			t.Fatalf("overlap %s: %v %v", kind, ok, err)
		}
	}
	claims := make(chan bool, count)
	errs = make(chan error, count)
	for range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := db.ClaimBugRepairDispatch(t.Context(), identity.SessionID, req.Request, req.SourceCommit, req.SourceTree, req.GraphAttemptID, run.ID)
			claims <- ok
			errs <- err
		}()
	}
	wg.Wait()
	close(claims)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	total := 0
	for ok := range claims {
		if ok {
			total++
		}
	}
	if total != 1 {
		t.Fatalf("dispatch count=%d", total)
	}
	if err := db.StopBugRepair(t.Context(), identity.SessionID, req.Request, "uncertain", "process cleanup unknown", false); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"save", "checks", "child"} {
		if ok, err := db.ClaimBugWork(t.Context(), identity.SessionID, kind, kind); err != nil || ok {
			t.Fatalf("uncertain overlap %s: %v %v", kind, ok, err)
		}
	}
	next, _ := repairBoundaryRequest(t, db, identity, analysis, 1, "test_failure")
	if _, err := db.ReserveBugRepair(t.Context(), next, repairBoundaryRevision(t, db, identity.SessionID)); !errors.Is(err, ErrBugWorkActive) {
		t.Fatalf("uncertain replacement: %v", err)
	}
	if ok, err := db.ClaimBugRepairDispatch(t.Context(), identity.SessionID, req.Request, req.SourceCommit, req.SourceTree, req.GraphAttemptID, run.ID); err != nil || ok {
		t.Fatalf("uncertain redispatch: %v %v", ok, err)
	}
}

func TestBugRepairBoundaryAuthorizationDriftAndScopeHistory(t *testing.T) {
	db := newTestDB(t)
	identity, analysis := repairBoundaryApproved(t, db)
	req, run := repairBoundaryRequest(t, db, identity, analysis, 0, "initial")
	invalid := req
	invalid.AnalysisDigest = strings.Repeat("f", 64)
	revision := repairBoundaryRevision(t, db, identity.SessionID)
	if _, err := db.ReserveBugRepair(t.Context(), invalid, revision); !errors.Is(err, ErrBugRepairUnauthorized) {
		t.Fatalf("wrong approval: %v", err)
	}
	if got := repairBoundaryRevision(t, db, identity.SessionID); got != revision {
		t.Fatal("unauthorized reservation changed state")
	}
	if _, err := db.ReserveBugRepair(t.Context(), req, revision); err != nil {
		t.Fatal(err)
	}
	if ok, err := db.ClaimBugRepairDispatch(t.Context(), identity.SessionID, req.Request, strings.Repeat("c", 40), req.SourceTree, req.GraphAttemptID, run.ID); err == nil || ok {
		t.Fatalf("source drift: %v %v", ok, err)
	}
	observation := BugRepairScopeObservation{SessionID: identity.SessionID, Request: req.Request, SourceCommit: strings.Repeat("c", 40), SourceTree: strings.Repeat("d", 40), DiffDigest: strings.Repeat("e", 64), Disposition: "indeterminate", Evidence: "scope not established", Paths: []string{"lookup.go"}}
	if err := db.RetainBugRepairScope(t.Context(), observation); err != nil {
		t.Fatal(err)
	}
	before := repairBoundaryRevision(t, db, identity.SessionID)
	if err := db.RetainBugRepairScope(t.Context(), observation); err != nil {
		t.Fatal(err)
	}
	if after := repairBoundaryRevision(t, db, identity.SessionID); after != before {
		t.Fatal("scope replay changed revision")
	}
	changed := observation
	changed.Disposition = "within"
	if err := db.RetainBugRepairScope(t.Context(), changed); err == nil {
		t.Fatal("scope uncertainty overwritten")
	}
	if _, err := db.CompletedBugRepair(t.Context(), identity.SessionID, req.Request, observation.SourceCommit, observation.SourceTree); err == nil {
		t.Fatal("scope observation certified completed repair")
	}
	newer := bugBoundaryCompleteAnalysis()
	newer.Remedy = "Different remedy requiring approval"
	bugBoundaryApply(t, db, identity, 1, "done", newer)
	if ok, err := db.ClaimBugRepairDispatch(t.Context(), identity.SessionID, req.Request, req.SourceCommit, req.SourceTree, req.GraphAttemptID, run.ID); !errors.Is(err, ErrBugRepairUnauthorized) || ok {
		t.Fatalf("superseded dispatch: %v %v", ok, err)
	}
	retained, err := db.GetBugRepair(t.Context(), identity.SessionID, req.Request)
	if err != nil || retained.Ordinal != 1 || !reflect.DeepEqual(retained.BugRepairRequest, req) {
		t.Fatalf("reservation lost: %+v %v", retained, err)
	}
}

func TestBugRepairBoundaryCrossTaskAndAtomicFailure(t *testing.T) {
	db := newTestDB(t)
	identity, analysis := repairBoundaryApproved(t, db)
	req, run := repairBoundaryRequest(t, db, identity, analysis, 0, "initial")
	other := bugBoundaryIdentity(t, db, 493)
	otherPass, otherRun := bugBoundaryPass(t, db, other.SessionID, 0)
	revision := repairBoundaryRevision(t, db, identity.SessionID)
	crossed := req
	crossed.GraphAttemptID = otherPass.GraphAttemptID
	if _, err := db.ReserveBugRepair(t.Context(), crossed, revision); err == nil {
		t.Fatal("foreign attempt authorized repair")
	}
	if _, err := db.sql.ExecContext(t.Context(), `CREATE TRIGGER reject_repair_event BEFORE INSERT ON bug_analysis_events BEGIN SELECT RAISE(ABORT,'injected repair persistence failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ReserveBugRepair(t.Context(), req, revision); err == nil {
		t.Fatal("event failure reported success")
	}
	if _, err := db.GetBugRepair(t.Context(), identity.SessionID, req.Request); !errors.Is(err, ErrNotFound) {
		t.Fatalf("partial reservation survived: %v", err)
	}
	if got := repairBoundaryRevision(t, db, identity.SessionID); got != revision {
		t.Fatal("failed transaction spent slot")
	}
	if _, err := db.sql.ExecContext(t.Context(), `DROP TRIGGER reject_repair_event`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ReserveBugRepair(t.Context(), req, revision); err != nil {
		t.Fatal(err)
	}
	if ok, err := db.ClaimBugRepairDispatch(t.Context(), identity.SessionID, req.Request, req.SourceCommit, req.SourceTree, otherPass.GraphAttemptID, otherRun.ID); err == nil || ok {
		t.Fatalf("foreign dispatch: %v %v", ok, err)
	}
	if ok, err := db.ClaimBugRepairDispatch(t.Context(), identity.SessionID, req.Request, req.SourceCommit, req.SourceTree, req.GraphAttemptID, run.ID); err != nil || !ok {
		t.Fatalf("valid dispatch: %v %v", ok, err)
	}
	if _, err := db.GetBugRepair(t.Context(), other.SessionID, req.Request); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign repair exposed: %v", err)
	}
}

func TestBugRepairReshapeRequiresRenewedApproval(t *testing.T) {
	db := newTestDB(t)
	identity, analysis := repairBoundaryApproved(t, db)
	initial, _ := repairBoundaryRequest(t, db, identity, analysis, 0, "initial")
	if _, err := db.ReserveBugRepair(t.Context(), initial, repairBoundaryRevision(t, db, identity.SessionID)); err != nil {
		t.Fatal(err)
	}
	if err := db.StopBugRepair(t.Context(), identity.SessionID, initial.Request, "reshape", "remedy must change", true); err != nil {
		t.Fatal(err)
	}
	next, _ := repairBoundaryRequest(t, db, identity, analysis, 1, "reshape")
	next.Evidence = initial.Request
	if _, err := db.ReserveBugRepair(t.Context(), next, repairBoundaryRevision(t, db, identity.SessionID)); !errors.Is(err, ErrBugRepairUnauthorized) {
		t.Fatalf("old approval authorized reshape: %v", err)
	}
	latest, err := db.LatestBugRepair(t.Context(), identity.SessionID)
	if err != nil || latest.Ordinal != 1 || latest.State != "reshape" {
		t.Fatalf("refusal changed lifetime reservation: %+v %v", latest, err)
	}
	replay, err := db.ReserveBugRepair(t.Context(), initial, 1)
	if err != nil || replay.Ordinal != 1 || replay.State != "reshape" {
		t.Fatalf("spent reshape replay: %+v %v", replay, err)
	}
}
