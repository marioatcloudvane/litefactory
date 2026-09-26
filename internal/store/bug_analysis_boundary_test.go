package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/buganalysis"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func bugBoundaryIdentity(t *testing.T, db *DB, issue int) BugIdentity {
	t.Helper()
	s := testSession(t, db, issue)
	if _, err := db.AdmitWorkflow(t.Context(), TaskWorkflowAdmission{SessionID: s.ID, Version: 1, Family: "standalone", Workflow: "bug"}); err != nil {
		t.Fatal(err)
	}
	identity := BugIdentity{SessionID: s.ID, Repository: "acme/api", IssueID: int64(10000 + issue), IssueNumber: int64(issue), OwnerID: 42}
	if err := db.EnsureBugRecord(t.Context(), identity); err != nil {
		t.Fatal(err)
	}
	return identity
}

func bugBoundaryPass(t *testing.T, db *DB, session int64, n int) (BugAnalysisPass, Run) {
	t.Helper()
	definition, err := db.CreateGraphDefinition(t.Context(), GraphDefinition{Team: "builtin", Workflow: "bug", ProfileVersion: 1, Path: "bug/bug.dot", Digest: strings.Repeat("a", 64), CreatedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	instance, err := db.CreateGraphInstance(t.Context(), GraphInstance{SessionID: session, DefinitionID: definition.ID, CurrentNode: "analyst", StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	segment, err := db.GetOrCreateSegment(t.Context(), session, FirstSegmentOrdinal, "bug-boundary", AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	run, err := db.AllocateRun(t.Context(), AllocateRunParams{SessionID: session, SegmentID: segment.ID, Workflow: "bug", Agent: AgentClaude, TriggerCommentID: int64(n + 1), LogPath: func(ordinal int) string { return fmt.Sprintf("/logs/bug-%d", ordinal) }})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: instance.ID, Node: "analyst", RunID: run.ID, StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	return BugAnalysisPass{SessionID: session, Invocation: fmt.Sprintf("analysis-%d", n), InputDigest: strings.Repeat("b", 64), GraphAttemptID: attempt.ID}, run
}

func bugBoundaryState(t *testing.T, db *DB, session int64) BugExecution {
	t.Helper()
	state, _, approval, err := db.CurrentBugAnalysis(t.Context(), session)
	if err != nil {
		t.Fatal(err)
	}
	if approval != nil {
		t.Fatal("pass bookkeeping created approval")
	}
	return state
}

func TestBugBoundaryIdentityIsImmutableAndTaskBound(t *testing.T) {
	db := newTestDB(t)
	identity := bugBoundaryIdentity(t, db, 391)
	if err := db.EnsureBugRecord(t.Context(), identity); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"owner", "issue", "repository", "number"} {
		changed := identity
		switch field {
		case "owner":
			changed.OwnerID++
		case "issue":
			changed.IssueID++
		case "repository":
			changed.Repository = "other/api"
		case "number":
			changed.IssueNumber++
		}
		if err := db.EnsureBugRecord(t.Context(), changed); err == nil {
			t.Errorf("changed %s overwrote identity", field)
		}
	}
	if state := bugBoundaryState(t, db, identity.SessionID); state.Version != 1 || state.Revision != 1 || state.AnalysisUsed != 0 {
		t.Fatalf("identity replay changed state: %+v", state)
	}
	other := testSession(t, db, 392)
	if err := db.EnsureBugRecord(t.Context(), BugIdentity{SessionID: other.ID, Repository: "acme/api", IssueID: 10392, IssueNumber: 392, OwnerID: 42}); err == nil {
		t.Fatal("record admitted without Bug workflow")
	}
}

func TestBugBoundaryConcurrentReservationAndDispatch(t *testing.T) {
	db := newTestDB(t)
	identity := bugBoundaryIdentity(t, db, 391)
	input, run := bugBoundaryPass(t, db, identity.SessionID, 0)
	const count = 16
	var wg sync.WaitGroup
	errs := make(chan error, count)
	passes := make(chan BugAnalysisPass, count)
	for range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pass, err := db.ReserveBugAnalysisPass(t.Context(), input, 1)
			passes <- pass
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	close(passes)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for pass := range passes {
		if pass.Ordinal != 1 || pass.Invocation != input.Invocation {
			t.Fatalf("duplicate reservation: %+v", pass)
		}
	}
	next, _ := bugBoundaryPass(t, db, identity.SessionID, 1)
	if _, err := db.ReserveBugAnalysisPass(t.Context(), next, bugBoundaryState(t, db, identity.SessionID).Revision); !errors.Is(err, ErrBugAnalysisActive) {
		t.Fatalf("second active invocation: %v", err)
	}
	claims := make(chan bool, count)
	errs = make(chan error, count)
	for range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			claimed, err := db.ClaimBugAnalysisDispatch(t.Context(), input.SessionID, input.Invocation, input.InputDigest, run.ID)
			claims <- claimed
			errs <- err
		}()
	}
	wg.Wait()
	close(claims)
	close(errs)
	total := 0
	for claimed := range claims {
		if claimed {
			total++
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if total != 1 {
		t.Fatalf("launch claims=%d want 1", total)
	}
	conflict := input
	conflict.InputDigest = strings.Repeat("c", 64)
	if _, err := db.ReserveBugAnalysisPass(t.Context(), conflict, 1); err == nil {
		t.Fatal("changed replay accepted")
	}
	if err := db.FinishBugAnalysisPass(t.Context(), input.SessionID, input.Invocation, "succeeded", ""); err == nil {
		t.Fatal("unfinished run certified success")
	}
	if state := bugBoundaryState(t, db, input.SessionID); state.AnalysisUsed != 1 {
		t.Fatalf("concurrency spent extra passes: %+v", state)
	}
}

func TestBugBoundaryEightPassesSurviveReopenAndFreshGraphs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bug.db")
	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	identity := bugBoundaryIdentity(t, db, 391)
	var inputs []BugAnalysisPass
	for n := range 8 {
		input, run := bugBoundaryPass(t, db, identity.SessionID, n)
		state := bugBoundaryState(t, db, identity.SessionID)
		pass, err := db.ReserveBugAnalysisPass(t.Context(), input, state.Revision)
		if err != nil {
			t.Fatal(err)
		}
		if pass.Ordinal != n+1 {
			t.Fatalf("ordinal=%d want %d", pass.Ordinal, n+1)
		}
		if claimed, err := db.ClaimBugAnalysisDispatch(t.Context(), input.SessionID, input.Invocation, input.InputDigest, run.ID); err != nil || !claimed {
			t.Fatalf("claim=%v err=%v", claimed, err)
		}
		outcome := []string{"failed", "cancelled", "uncertain"}[n%3]
		if err := db.FinishBugAnalysisPass(t.Context(), input.SessionID, input.Invocation, outcome, "retained process cause"); err != nil {
			t.Fatal(err)
		}
		if err := db.FinishBugAnalysisPass(t.Context(), input.SessionID, input.Invocation, outcome, "retained process cause"); err != nil {
			t.Fatal(err)
		}
		if err := db.FinishBugAnalysisPass(t.Context(), input.SessionID, input.Invocation, "succeeded", ""); err == nil {
			t.Fatal("terminal failure revived")
		}
		inputs = append(inputs, input)
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		db, err = Open(t.Context(), path)
		if err != nil {
			t.Fatal(err)
		}
	}
	state := bugBoundaryState(t, db, identity.SessionID)
	if state.AnalysisUsed != 8 {
		t.Fatalf("recovery refunded budget: %+v", state)
	}
	ninth, _ := bugBoundaryPass(t, db, identity.SessionID, 8)
	if _, err := db.ReserveBugAnalysisPass(t.Context(), ninth, state.Revision); !errors.Is(err, ErrBugAnalysisExhausted) {
		t.Fatalf("ninth invocation: %v", err)
	}
	for n, input := range inputs {
		pass, err := db.ReserveBugAnalysisPass(t.Context(), input, 1)
		if err != nil || pass.Ordinal != n+1 || pass.Reason != "retained process cause" {
			t.Fatalf("exhausted replay: %+v %v", pass, err)
		}
	}
	other := bugBoundaryIdentity(t, db, 392)
	if _, err := db.GetBugAnalysisPass(t.Context(), other.SessionID, inputs[0].Invocation); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-task pass leaked: %v", err)
	}
}

func TestBugBoundaryCancellationAndAtomicWriteFailure(t *testing.T) {
	db := newTestDB(t)
	identity := bugBoundaryIdentity(t, db, 391)
	input, _ := bugBoundaryPass(t, db, identity.SessionID, 0)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := db.ReserveBugAnalysisPass(ctx, input, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled reservation: %v", err)
	}
	if _, err := db.sql.ExecContext(t.Context(), `CREATE TRIGGER reject_bug_event BEFORE INSERT ON bug_analysis_events BEGIN SELECT RAISE(ABORT,'injected event failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ReserveBugAnalysisPass(t.Context(), input, 1); err == nil {
		t.Fatal("failed event write reported reservation")
	}
	if _, err := db.GetBugAnalysisPass(t.Context(), input.SessionID, input.Invocation); !errors.Is(err, ErrNotFound) {
		t.Fatalf("failed transaction retained partial pass: %v", err)
	}
	if state := bugBoundaryState(t, db, input.SessionID); state.AnalysisUsed != 0 || state.Revision != 1 {
		t.Fatalf("failed transaction spent budget: %+v", state)
	}
	if _, err := db.sql.ExecContext(t.Context(), "DROP TRIGGER reject_bug_event"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ReserveBugAnalysisPass(t.Context(), input, 1); err != nil {
		t.Fatal(err)
	}
}

func TestBugBoundaryRejectsOtherTaskAttemptAndRun(t *testing.T) {
	db := newTestDB(t)
	first := bugBoundaryIdentity(t, db, 391)
	second := bugBoundaryIdentity(t, db, 392)
	input, run := bugBoundaryPass(t, db, first.SessionID, 0)
	other, otherRun := bugBoundaryPass(t, db, second.SessionID, 0)
	mixed := input
	mixed.GraphAttemptID = other.GraphAttemptID
	if _, err := db.ReserveBugAnalysisPass(t.Context(), mixed, 1); err == nil {
		t.Fatal("reserved another task's graph attempt")
	}
	if _, err := db.ReserveBugAnalysisPass(t.Context(), input, 1); err != nil {
		t.Fatal(err)
	}
	if claimed, err := db.ClaimBugAnalysisDispatch(t.Context(), input.SessionID, input.Invocation, input.InputDigest, otherRun.ID); err == nil || claimed {
		t.Fatalf("claimed other task's run: %v %v", claimed, err)
	}
	if claimed, err := db.ClaimBugAnalysisDispatch(t.Context(), input.SessionID, input.Invocation, input.InputDigest, run.ID); err != nil || !claimed {
		t.Fatalf("valid dispatch after refusal: %v %v", claimed, err)
	}
	if state := bugBoundaryState(t, db, second.SessionID); state.AnalysisUsed != 0 {
		t.Fatalf("cross-task attempt changed other ledger: %+v", state)
	}
}

func TestBugBoundaryCompetingFreshInvocationsSpendOnePass(t *testing.T) {
	db := newTestDB(t)
	identity := bugBoundaryIdentity(t, db, 391)
	var inputs []BugAnalysisPass
	for n := range 12 {
		input, _ := bugBoundaryPass(t, db, identity.SessionID, n)
		inputs = append(inputs, input)
	}
	var wg sync.WaitGroup
	errs := make(chan error, len(inputs))
	for _, input := range inputs {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := db.ReserveBugAnalysisPass(t.Context(), input, 1); errs <- err }()
	}
	wg.Wait()
	close(errs)
	successes := 0
	for err := range errs {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrBugRevisionConflict) {
			t.Fatalf("unexpected competing reservation error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("fresh reservations admitted=%d want 1", successes)
	}
	if state := bugBoundaryState(t, db, identity.SessionID); state.AnalysisUsed != 1 {
		t.Fatalf("competing fresh invocations spent more than one slot: %+v", state)
	}
}

// The result and process observations below are controlled factory-bound inputs
// to public storage APIs. This fixture does not execute an agent or MCP transport.
func bugBoundaryApply(t *testing.T, db *DB, identity BugIdentity, n int, response string, analysis buganalysis.BugAnalysis) (BugAnalysisRevision, BugAnalysisApplication, BugAnalysisPass) {
	t.Helper()
	input, run := bugBoundaryPass(t, db, identity.SessionID, n)
	state, _, _, err := db.CurrentBugAnalysis(t.Context(), identity.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ReserveBugAnalysisPass(t.Context(), input, state.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ClaimBugAnalysisDispatch(t.Context(), input.SessionID, input.Invocation, input.InputDigest, run.ID); err != nil {
		t.Fatal(err)
	}
	outcome := "completed"
	if response == "ask" {
		outcome = "incomplete"
	}
	result := agentresult.Result{Outcome: outcome, Comment: "Retained analysis for review", Payload: agentresult.Payload{Bug: &agentresult.Bug{Response: response, Invocation: input.Invocation, BaseRevision: state.CurrentAnalysis, Analysis: &analysis}}}
	if response == "ask" {
		result.Reason = "Owner reproduction evidence needed"
		for _, q := range analysis.Questions {
			result.Questions = append(result.Questions, agentresult.Question{ID: q.ID, Audience: "technical", Text: q.Text})
		}
	}
	context := agentresult.Context{Workflow: agent.WorkflowBug, SchemaVersion: 1, WorkflowVersion: 2, QuestionsAllowed: true, Bug: &agentresult.BugBinding{Role: "bug-analyst", Invocation: input.Invocation, BaseRevision: state.CurrentAnalysis}}
	if d := agentresult.ValidateResult(result, context); !d.Valid {
		t.Fatalf("invalid boundary result: %+v", d)
	}
	task := fmt.Sprintf("github:%s#%d", identity.Repository, identity.IssueNumber)
	binding, err := db.BindResultAttempt(t.Context(), ResultAttempt{Task: task, Workflow: "bug", WorkContext: input.Invocation, Attempt: input.Invocation, RunID: run.ID, SchemaVersion: 1, WorkflowVersion: 2, SubmissionVersion: 2})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(struct {
		Result agentresult.Result `json:"result"`
	}{result})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := db.RecordResult(t.Context(), ResultRecord{Binding: binding, RequestID: input.Invocation, Payload: raw, Snapshot: raw, Diagnostics: []byte(`{"valid":true}`), Retired: []byte(`{}`), Accepted: true})
	if err != nil {
		t.Fatal(err)
	}
	exit := int64(0)
	if _, err = db.FinishRun(t.Context(), run.ID, RunOutcome{Status: RunSucceeded, ExitCode: &exit}); err != nil {
		t.Fatal(err)
	}
	if err = db.FinishBugAnalysisPass(t.Context(), identity.SessionID, input.Invocation, "succeeded", ""); err != nil {
		t.Fatal(err)
	}
	if err = db.CompleteGraphNodeAttempt(t.Context(), input.GraphAttemptID, GraphCompletion{Status: GraphSucceeded, Outcome: "success", FinishedAt: graphTestTime(), Outputs: []GraphOutput{{Kind: "json", Payload: []byte(fmt.Sprintf(`{"result_id":%d,"result_revision":%d}`, binding.ResultID, receipt.AppliedRevision))}}}); err != nil {
		t.Fatal(err)
	}
	state, _, _, err = db.CurrentBugAnalysis(t.Context(), identity.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	application := BugAnalysisApplication{SessionID: identity.SessionID, ExpectedRevision: state.Revision, Invocation: input.Invocation, Operation: input.Invocation, InputDigest: submissionDigest(raw), ResultID: binding.ResultID, ResultRevision: receipt.AppliedRevision}
	revision, err := db.ApplyBugAnalysis(t.Context(), application)
	if err != nil {
		t.Fatal(err)
	}
	return revision, application, input
}

func bugBoundaryCompleteAnalysis() buganalysis.BugAnalysis {
	return buganalysis.BugAnalysis{Expected: "Lookup returns saved value", Actual: "Lookup misses saved value", Reproduction: buganalysis.BugReproduction{Steps: []string{"Save then read"}, Environment: "disposable fixture", Observation: "owner_reported"}, Cause: "Mismatched key normalization", Evidence: []string{"owner reproduction"}, Remedy: "Normalize lookup key", Exclusions: []string{"No schema changes"}, Criteria: []buganalysis.BugCriterion{{ID: "AC-1", Requirement: "Read saved value", Measurement: "Lookup regression returns saved value"}}}
}

func TestBugBoundaryAnalysisHistoryAndAuthenticatedAnswers(t *testing.T) {
	db := newTestDB(t)
	identity := bugBoundaryIdentity(t, db, 391)
	sections := bugBoundarySeedSections(t, db, identity.SessionID)
	defer func() {
		got, err := db.ReadBugExecutionSections(t.Context(), identity.SessionID)
		if err != nil || !reflect.DeepEqual(got, sections) {
			t.Errorf("analysis/approval altered downstream sections: %+v %v", got, err)
		}
	}()
	partial := bugBoundaryCompleteAnalysis()
	partial.Questions = []buganalysis.BugQuestion{{ID: "Q-1", Text: "Which input triggers the miss?", Material: true}}
	first, application, _ := bugBoundaryApply(t, db, identity, 0, "ask", partial)
	answer := BugAnalysisAnswer{Observation: "answer-1", QuestionID: "Q-1", CommentID: 101, AuthorID: 42, Body: "Uppercase input", ObservedAt: graphTestTime()}
	unauthorized := answer
	unauthorized.AuthorID = 43
	if err := db.RecordBugAnalysisAnswer(t.Context(), identity.SessionID, unauthorized); err == nil {
		t.Fatal("nonowner answer accepted")
	}
	if err := db.RecordBugAnalysisAnswer(t.Context(), identity.SessionID, answer); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordBugAnalysisAnswer(t.Context(), identity.SessionID, answer); err != nil {
		t.Fatal(err)
	}
	conflicting := answer
	conflicting.Body = "Changed answer"
	if err := db.RecordBugAnalysisAnswer(t.Context(), identity.SessionID, conflicting); err == nil {
		t.Fatal("same observation overwrote answer")
	}
	edited := conflicting
	edited.Observation = "answer-2"
	if err := db.RecordBugAnalysisAnswer(t.Context(), identity.SessionID, edited); err != nil {
		t.Fatal(err)
	}
	answers, err := db.BugAnalysisAnswers(t.Context(), identity.SessionID, "", 100)
	if err != nil || len(answers) != 2 || answers[0].Body != answer.Body || answers[1].Body != edited.Body {
		t.Fatalf("answer history: %+v %v", answers, err)
	}
	complete := bugBoundaryCompleteAnalysis()
	complete.AnswerReferences = []string{edited.Observation}
	second, _, _ := bugBoundaryApply(t, db, identity, 1, "done", complete)
	if second.Revision != 2 || !second.ReviewReady {
		t.Fatalf("second revision: %+v", second)
	}
	old, err := db.GetBugAnalysisRevision(t.Context(), identity.SessionID, 1)
	if err != nil || !reflect.DeepEqual(first, old) {
		t.Fatalf("history rewritten: %+v %v", old, err)
	}
	replay, err := db.ApplyBugAnalysis(t.Context(), application)
	if err != nil || !reflect.DeepEqual(replay, first) {
		t.Fatalf("application replay: %+v %v", replay, err)
	}
	application.InputDigest = strings.Repeat("f", 64)
	if _, err := db.ApplyBugAnalysis(t.Context(), application); err == nil {
		t.Fatal("conflicting application accepted")
	}
	if state := bugBoundaryState(t, db, identity.SessionID); state.AnalysisUsed != 2 || state.CurrentAnalysis != 2 {
		t.Fatalf("replay changed progress: %+v", state)
	}
}

func bugBoundaryApproval(t *testing.T, passes int) {
	t.Helper()
	db := newTestDB(t)
	identity := bugBoundaryIdentity(t, db, 391)
	sections := bugBoundarySeedSections(t, db, identity.SessionID)
	defer func() {
		got, err := db.ReadBugExecutionSections(t.Context(), identity.SessionID)
		if err != nil || !reflect.DeepEqual(got, sections) {
			t.Errorf("analysis/approval altered downstream sections: %+v %v", got, err)
		}
	}()
	for n := range passes - 1 {
		input, _ := bugBoundaryPass(t, db, identity.SessionID, n)
		if _, err := db.ReserveBugAnalysisPass(t.Context(), input, bugBoundaryState(t, db, identity.SessionID).Revision); err != nil {
			t.Fatal(err)
		}
		if err := db.FinishBugAnalysisPass(t.Context(), input.SessionID, input.Invocation, "failed", "retained investigation failure"); err != nil {
			t.Fatal(err)
		}
	}
	revision, _, input := bugBoundaryApply(t, db, identity, passes-1, "done", bugBoundaryCompleteAnalysis())
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
	task := "github:acme/api#391"
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
	state := bugBoundaryState(t, db, identity.SessionID)
	for _, fault := range []string{"owner", "task", "stale", "mixed", "comment"} {
		bad := reply
		switch fault {
		case "owner":
			bad.AuthorID = 43
		case "task":
			bad.TaskRef = "github:acme/api#392"
			bad.SourceRef = bad.TaskRef
		case "stale":
			bad.Body = "/approve-bug analysis 2"
		case "mixed":
			bad.Body += " but change remedy"
		case "comment":
			bad.QuestionCommentID = 100
		}
		if _, err := db.ApproveBugAnalysis(t.Context(), identity.SessionID, review.Review, bad, state.Revision); err == nil {
			t.Errorf("%s approval accepted", fault)
		}
	}
	before, err := db.ListRuns(t.Context(), identity.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	approval, err := db.ApproveBugAnalysis(t.Context(), identity.SessionID, review.Review, reply, state.Revision)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := db.ApproveBugAnalysis(t.Context(), identity.SessionID, review.Review, reply, state.Revision)
	if err != nil || !reflect.DeepEqual(approval, replay) {
		t.Fatalf("approval replay: %+v %v", replay, err)
	}
	after, err := db.ListRuns(t.Context(), identity.SessionID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("approval allocated run: %v", err)
	}
	final, _, applicable, err := db.CurrentBugAnalysis(t.Context(), identity.SessionID)
	if err != nil || final.AnalysisUsed != passes || applicable == nil {
		t.Fatalf("eighth pass approval: %+v %+v %v", final, applicable, err)
	}
	wait, err := db.GetGraphOwnerWait(t.Context(), attempt.InstanceID)
	if err != nil || wait.State != "allocated" {
		t.Fatalf("reply not consumed: %+v %v", wait, err)
	}
	if passes == 8 {
		ninth, _ := bugBoundaryPass(t, db, identity.SessionID, 8)
		if _, err := db.ReserveBugAnalysisPass(t.Context(), ninth, final.Revision); !errors.Is(err, ErrBugAnalysisExhausted) {
			t.Fatalf("approval reset budget: %v", err)
		}
	} else {
		changed := bugBoundaryCompleteAnalysis()
		changed.Remedy = "Normalize both input and stored key"
		second, _, _ := bugBoundaryApply(t, db, identity, passes, "done", changed)
		_, current, applicable, err := db.CurrentBugAnalysis(t.Context(), identity.SessionID)
		if err != nil || applicable != nil || current.Revision != second.Revision {
			t.Fatalf("old approval transferred: %+v %+v %v", current, applicable, err)
		}
		old, err := db.GetBugAnalysisRevision(t.Context(), identity.SessionID, revision.Revision)
		if err != nil || !reflect.DeepEqual(old, revision) {
			t.Fatalf("superseding erased history: %+v %v", old, err)
		}
		historical, err := db.ApproveBugAnalysis(t.Context(), identity.SessionID, review.Review, reply, state.Revision)
		if err != nil || !reflect.DeepEqual(historical, approval) {
			t.Fatalf("historical replay lost evidence: %+v %v", historical, err)
		}
		_, _, applicable, err = db.CurrentBugAnalysis(t.Context(), identity.SessionID)
		if err != nil || applicable != nil {
			t.Fatalf("historical replay restored authority: %+v %v", applicable, err)
		}
	}
}

func TestBugBoundaryEighthPassPublishedApprovalConsumesNoInvocation(t *testing.T) {
	bugBoundaryApproval(t, 8)
}
func TestBugBoundarySupersededApprovalRemainsHistorical(t *testing.T) { bugBoundaryApproval(t, 1) }

func TestBugBoundarySurrogateModeCannotDispatchOrBindResult(t *testing.T) {
	db := newTestDB(t)
	identity := bugBoundaryIdentity(t, db, 391)
	input, _ := bugBoundaryPass(t, db, identity.SessionID, 0)
	original, err := db.GetGraphNodeAttempt(t.Context(), input.GraphAttemptID)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := db.GetGraphInstance(t.Context(), original.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	other, err := db.CreateGraphInstance(t.Context(), GraphInstance{SessionID: identity.SessionID, DefinitionID: instance.DefinitionID, CurrentNode: "analyst", StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	modeRun := testRun(t, db, identity.SessionID, 100)
	attempt, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: other.ID, Node: "analyst", RunID: modeRun.ID, StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	input.GraphAttemptID = attempt.ID
	if _, err := db.ReserveBugAnalysisPass(t.Context(), input, 1); err != nil {
		t.Fatal(err)
	}
	if claimed, err := db.ClaimBugAnalysisDispatch(t.Context(), input.SessionID, input.Invocation, input.InputDigest, modeRun.ID); err == nil || claimed {
		t.Fatalf("surrogate mode dispatch: %v %v", claimed, err)
	}
	if _, err := db.BindResultAttempt(t.Context(), ResultAttempt{Task: "github:acme/api#391", Workflow: "bug", WorkContext: "analysis", Attempt: "surrogate", RunID: modeRun.ID, SchemaVersion: 1, WorkflowVersion: 2, SubmissionVersion: 2}); err == nil {
		t.Fatal("surrogate mode granted Bug result authority")
	}
	pass, err := db.GetBugAnalysisPass(t.Context(), input.SessionID, input.Invocation)
	if err != nil || pass.State != "reserved" || pass.RunID != 0 {
		t.Fatalf("refusal mutated dispatch: %+v %v", pass, err)
	}
}

func bugBoundarySeedSections(t *testing.T, db *DB, session int64) BugExecutionSections {
	t.Helper()
	out := BugExecutionSections{
		Repair:   &BugRepairReferences{Reservations: []string{"reservation-old"}, SourceCommit: strings.Repeat("a", 40), SourceTree: strings.Repeat("b", 40), SaveOperations: []string{"save-pending"}},
		Checks:   &BugCheckReferences{TargetRepository: "acme/api", TargetBranch: "main", TargetCommit: strings.Repeat("c", 40), RecipeDigest: strings.Repeat("d", 64), ChildCalls: []string{"child-old"}, Evidence: []string{"check-old"}},
		Delivery: &BugDeliveryReferences{PRID: 123, PRNumber: 456, Reviews: []string{"delivery-review-old"}, Intents: []string{"merge-intent"}, Observations: []string{"remote-pending"}, PendingEffects: []string{"merge-unknown"}},
		Failures: &BugFailureReferences{Reasons: []string{"retained failure"}, Evidence: []string{"failure-old"}, PendingPublications: []string{"report-unknown"}},
	}
	revision, err := db.UpdateBugRepairReferences(t.Context(), session, bugBoundaryState(t, db, session).Revision, "seed-repair", *out.Repair)
	if err != nil {
		t.Fatal(err)
	}
	revision, err = db.UpdateBugCheckReferences(t.Context(), session, revision, "seed-checks", *out.Checks)
	if err != nil {
		t.Fatal(err)
	}
	revision, err = db.UpdateBugDeliveryReferences(t.Context(), session, revision, "seed-delivery", *out.Delivery)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.UpdateBugFailureReferences(t.Context(), session, revision, "seed-failures", *out.Failures); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestBugBoundarySectionsCASReplayAndNoAuthority(t *testing.T) {
	db := newTestDB(t)
	identity := bugBoundaryIdentity(t, db, 391)
	empty, err := db.ReadBugExecutionSections(t.Context(), identity.SessionID)
	if err != nil || !reflect.DeepEqual(empty, BugExecutionSections{}) {
		t.Fatalf("absent sections imply success: %+v %v", empty, err)
	}
	original := bugBoundarySeedSections(t, db, identity.SessionID)
	state := bugBoundaryState(t, db, identity.SessionID)
	firstRevision, err := db.UpdateBugRepairReferences(t.Context(), identity.SessionID, 1, "seed-repair", *original.Repair)
	if err != nil || firstRevision != 2 {
		t.Fatalf("stale exact replay: %d %v", firstRevision, err)
	}
	changed := *original.Repair
	changed.SourceCommit = strings.Repeat("e", 40)
	if _, err := db.UpdateBugRepairReferences(t.Context(), identity.SessionID, state.Revision, "seed-repair", changed); err == nil {
		t.Fatal("changed section replay accepted")
	}
	if _, err := db.UpdateBugFailureReferences(t.Context(), identity.SessionID, state.Revision, "seed-repair", *original.Failures); err == nil {
		t.Fatal("same operation crossed section ownership")
	}
	if _, err := db.UpdateBugRepairReferences(t.Context(), identity.SessionID, state.Revision-1, "new-stale", changed); !errors.Is(err, ErrBugRevisionConflict) {
		t.Fatalf("stale CAS accepted: %v", err)
	}
	const count = 12
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for n := range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := db.UpdateBugRepairReferences(t.Context(), identity.SessionID, state.Revision, fmt.Sprintf("new-%d", n), changed)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	winners := 0
	for err := range errs {
		if err == nil {
			winners++
		} else if !errors.Is(err, ErrBugRevisionConflict) {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatalf("CAS winners=%d", winners)
	}
	got, err := db.ReadBugExecutionSections(t.Context(), identity.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	original.Repair = &changed
	if !reflect.DeepEqual(got, original) {
		t.Fatalf("section mutation replaced unrelated fields: %+v", got)
	}
	final := bugBoundaryState(t, db, identity.SessionID)
	if final.AnalysisUsed != 0 || final.CurrentAnalysis != 0 || final.Revision != state.Revision+1 {
		t.Fatalf("reference mutation created authority or changed counters: %+v", final)
	}
	// Replaying an old revision returns its receipt, without restoring old data.
	if _, err := db.UpdateBugRepairReferences(t.Context(), identity.SessionID, 1, "seed-repair", BugRepairReferences{Reservations: []string{"reservation-old"}, SourceCommit: strings.Repeat("a", 40), SourceTree: strings.Repeat("b", 40), SaveOperations: []string{"save-pending"}}); err != nil {
		t.Fatal(err)
	}
	current, err := db.ReadBugExecutionSections(t.Context(), identity.SessionID)
	if err != nil || !reflect.DeepEqual(current, got) {
		t.Fatalf("old replay reverted current state: %+v %v", current, err)
	}
}

func TestBugBoundaryAgentCannotPatchExecutionAuthority(t *testing.T) {
	for _, field := range []string{"analysis_used", "repair_usage", "approvals", "execution", "recipe", "delivery"} {
		t.Run(field, func(t *testing.T) {
			raw := []byte(fmt.Sprintf(`{"result":{"outcome":"completed","comment":"attempted patch","payload":{"bug":{"response":"done","invocation":"analysis-1","base_revision":0,"%s":1}}}}`, field))
			var snapshot agentresult.Snapshot
			if diagnostic := agentresult.Decode(raw, &snapshot); diagnostic.Valid {
				t.Fatalf("agent execution patch %q admitted", field)
			}
		})
	}
}
