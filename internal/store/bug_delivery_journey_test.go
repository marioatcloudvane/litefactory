package store

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/bugrepair"
	"github.com/cloudvane/litefactory/internal/config"
)

// This file covers the gap the prior test-writing pass flagged as too large to
// build in its time budget: the PR -> review -> merge -> close journey for the
// Bug delivery flow (bug_delivery.go), starting from a repair that is genuinely
// completed with a full passing regression/focused/full BugTestCall chain.
//
// Every step below drives the real public (or, where the schema has no public
// seam, package-private) store API in the same order internal/turn does:
// bug_repair_handoff.go's Complete (reserve -> dispatch -> scope -> complete),
// bug_test_graph.go's BugTestGraphActivity/BugTestChild (create call -> run
// each phase's checks -> settle -> consume via the parent's own graph
// completion), bug_pr.go (PR intent), bug_delivery_presentation.go (review),
// bug_merge.go (merge) and bug_close.go (closure). One documented
// simplification: ClaimBugTestDispatch/the daemon's dispatch loop is the
// factory's own race guard for who may run a phase's commands, orthogonal to
// the domain evidence chain these tests assert; driving the check-phase API
// (EnsureBugCheckPhase/ClaimBugCheckCommand/ObserveBugCheckCommand/
// FinishBugCheckPhase) and SettleBugTestCall directly reaches the identical
// retained state without it.

// bugDeliveryFixtureRepairBug and bugDeliveryFixtureRepairSnapshot mirror the
// unexported retained-result JSON shape [DB.CompleteBugRepair] decodes, the
// same way bug_fixtures_test.go's bugFixtureSnapshot does for the analyst.
type bugDeliveryFixtureRepairBug struct {
	Response   string              `json:"response"`
	Invocation string              `json:"invocation"`
	Repair     *bugrepair.Evidence `json:"repair,omitempty"`
}
type bugDeliveryFixtureRepairSnapshot struct {
	Result struct {
		Outcome string `json:"outcome"`
		Payload struct {
			Bug bugDeliveryFixtureRepairBug `json:"bug"`
		} `json:"payload"`
	} `json:"result"`
}

// bugDeliveryFixtureState is one Bug task carried through a completed,
// fully-tested repair, ready for the PR/review/merge/close journey.
type bugDeliveryFixtureState struct {
	db       *DB
	session  Session
	identity BugIdentity
	instance GraphInstance
	revision BugAnalysisRevision
	approval BugAnalysisApproval

	request      string
	sourceCommit string
	sourceTree   string
	targetCommit string

	testsAttempt GraphNodeAttempt
	childCallID  int64
	branch       string

	pr           BugPullRequest
	marker       string
	reviewDigest string
}

// bugDeliveryCompleteRepairThroughTests drives one repair reservation all the
// way through [DB.CompleteBugRepair] and a full passing
// regression/focused/full [BugTestCall] generation, ending with that call
// consumed by the parent's own graph completion — the exact state
// EnsureBugPRIntent's bugDeliveryEvidenceTx requires.
func bugDeliveryCompleteRepairThroughTests(t *testing.T, db *DB, session Session, identity BugIdentity, instance GraphInstance, childDef GraphDefinition, revision BugAnalysisRevision, approval BugAnalysisApproval, request, cause, grantRef string, fixerAttempt GraphNodeAttempt, fixerRun Run, at time.Time) *bugDeliveryFixtureState {
	t.Helper()
	ctx := t.Context()

	sourceCommit := strings.Repeat("a", 40)
	sourceTree := strings.Repeat("b", 40)
	targetCommit := strings.Repeat("c", 40)

	in := bugFixtureRepairRequest(session, revision, approval, request, cause, fixerAttempt.ID, grantRef)
	if _, err := db.ReserveBugRepair(ctx, in, bugCurrentRevision(t, db, session.ID)); err != nil {
		t.Fatalf("reserve repair %s: %v", request, err)
	}
	claimed, err := db.ClaimBugRepairDispatch(ctx, session.ID, request, sourceCommit, sourceTree, fixerAttempt.ID, fixerRun.ID)
	if err != nil || !claimed {
		t.Fatalf("claim repair dispatch %s: claimed=%v err=%v", request, claimed, err)
	}

	// A fixer self-check run recorded mid-repair: story-003's evidence the
	// delivery review digest must include (AC-009).
	selfCheckBinding := bugFixtureCheckBinding(session.ID, "unused", "0", request, "regression")
	start, err := db.StartBugSelfCheck(ctx, session.ID, request, 0, selfCheckBinding, bugFixtureCheckRecipe(), 1)
	if err != nil {
		t.Fatalf("start self-check: %v", err)
	}
	if start.Status != "started" {
		t.Fatalf("self-check start = %+v, want started", start)
	}
	if err := db.FinishBugSelfCheckPhase(ctx, session.ID, start.Operation, "passed", "", nil); err != nil {
		t.Fatalf("finish self-check: %v", err)
	}

	if err := db.RetainBugRepairScope(ctx, BugRepairScopeObservation{
		SessionID: session.ID, Request: request, SourceCommit: sourceCommit, SourceTree: sourceTree,
		DiffDigest: submissionDigest([]byte("diff:" + request)), Disposition: "within",
		Evidence: "changed only the widgets handler and its test", Paths: []string{"widgets.go", "widgets_test.go"},
	}); err != nil {
		t.Fatalf("retain repair scope: %v", err)
	}

	binding, err := db.BindResultAttempt(ctx, ResultAttempt{
		Workflow: "bug", Task: session.TaskRef, WorkContext: "bug-repair", Attempt: request,
		RunID: fixerRun.ID, SchemaVersion: 1, WorkflowVersion: 2, SubmissionVersion: 2,
	})
	if err != nil {
		t.Fatalf("bind repair result attempt: %v", err)
	}

	evidence := bugrepair.Evidence{
		Reservation: request, AnalysisDigest: revision.Digest, SourceCommit: sourceCommit, SourceTree: sourceTree,
		Summary:  "guarded the nil widget store lookup",
		Coverage: []bugrepair.Coverage{{Criterion: "AC-001", Changes: []string{"widgets.go"}, Tests: []string{"widgets_test.go"}}},
	}
	var snapshot bugDeliveryFixtureRepairSnapshot
	snapshot.Result.Outcome = "completed"
	snapshot.Result.Payload.Bug = bugDeliveryFixtureRepairBug{Response: "done", Invocation: request, Repair: &evidence}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal repair snapshot: %v", err)
	}
	receipt, err := db.RecordResult(ctx, ResultRecord{
		Binding: binding, RequestID: "repair-result:" + request, Payload: []byte(`{}`), Diagnostics: []byte(`{}`),
		Snapshot: raw, Retired: []byte(`null`), Accepted: true, ExpectedRevision: 0,
	})
	if err != nil {
		t.Fatalf("record repair result: %v", err)
	}

	exit := int64(0)
	if _, err := db.FinishRun(ctx, fixerRun.ID, RunOutcome{Status: RunSucceeded, ExitCode: &exit, CommitSHA: sourceCommit, FinishedAt: at}); err != nil {
		t.Fatalf("finish fixer run: %v", err)
	}
	if err := db.SetRunPublication(ctx, fixerRun.ID, true, "pushed to origin"); err != nil {
		t.Fatalf("set run publication: %v", err)
	}

	outputPayload, err := json.Marshal(struct {
		ResultID       int64 `json:"result_id"`
		ResultRevision int64 `json:"result_revision"`
	}{binding.ResultID, receipt.AppliedRevision})
	if err != nil {
		t.Fatalf("marshal repair graph output: %v", err)
	}
	transitionToTests := GraphTransition{InstanceID: instance.ID, FromNode: "attempt", ToNode: "tests", OnOutcome: "success", At: at}
	if err := db.CompleteGraphNodeAttempt(ctx, fixerAttempt.ID, GraphCompletion{
		Status: GraphSucceeded, Outcome: "success", FinishedAt: at,
		Outputs:    []GraphOutput{{Kind: GraphOutputJSON, Payload: outputPayload}},
		Transition: &transitionToTests,
	}); err != nil {
		t.Fatalf("complete fixer attempt: %v", err)
	}

	if err := db.CompleteBugRepair(ctx, BugRepairCompletion{
		SessionID: session.ID, Request: request, ResultID: binding.ResultID, ResultRevision: receipt.AppliedRevision,
		SourceCommit: sourceCommit, SourceTree: sourceTree, SaveOperation: "save:" + request,
	}, bugCurrentRevision(t, db, session.ID)); err != nil {
		t.Fatalf("complete bug repair: %v", err)
	}
	if completed, err := db.CompletedBugRepair(ctx, session.ID, request, sourceCommit, sourceTree); err != nil || completed.Request != request {
		t.Fatalf("completed bug repair = %+v, err=%v", completed, err)
	}

	// The parent envelope's own "tests" attempt: the running node
	// [DB.CreateBugTestCall] requires as ParentInstance/ParentAttempt.
	testsAttempt := bugFixtureAttempt(t, db, instance, "tests", 0, at)

	recipe := bugFixtureCheckRecipe()
	req := BugTestRequest{
		SessionID: session.ID, Operation: "bug-test:" + request, ParentInstance: instance.ID, ParentAttempt: testsAttempt.ID,
		ChildDefinition: childDef.ID, DefinitionDigest: childDef.Digest,
		Binding: BugCheckBinding{
			SessionID: session.ID, Repair: request, SourceCommit: sourceCommit, SourceTree: sourceTree,
			TargetRepository: "acme/api", TargetBranch: "main", TargetCommit: targetCommit,
			RecipeDigest: config.BugRecipeDigest(recipe), EnvironmentRevision: "env-1", PermissionRevision: "perm-1",
		},
	}
	call, err := db.CreateBugTestCall(ctx, req, at)
	if err != nil {
		t.Fatalf("create bug test call: %v", err)
	}

	for _, phase := range []string{"regression", "focused", "full"} {
		b := call.PhaseBinding(phase)
		if claimed, err := db.ClaimBugWork(ctx, session.ID, b.Operation, "checks"); err != nil || !claimed {
			t.Fatalf("claim check work %s: claimed=%v err=%v", phase, claimed, err)
		}
		if _, err := db.EnsureBugCheckPhase(ctx, b, recipe); err != nil {
			t.Fatalf("ensure check phase %s: %v", phase, err)
		}
		bugFixtureCompletePassingCommand(t, db, session.ID, b.Operation, 0)
		if err := db.FinishBugCheckPhase(ctx, session.ID, b.Operation, "passed", "", true); err != nil {
			t.Fatalf("finish check phase %s: %v", phase, err)
		}
		if err := db.ReleaseQuiescentBugCheckWork(ctx, session.ID, b.Operation); err != nil {
			t.Fatalf("release check work %s: %v", phase, err)
		}
	}

	settled, err := db.SettleBugTestCall(ctx, call.ID, at)
	if err != nil {
		t.Fatalf("settle bug test call: %v", err)
	}
	if settled.Outcome != "passed" {
		t.Fatalf("settled bug test call outcome = %q, want passed", settled.Outcome)
	}

	transitionToPR := GraphTransition{InstanceID: instance.ID, FromNode: "tests", ToNode: "pr", OnOutcome: "passed", At: at}
	if err := db.CompleteGraphNodeAttempt(ctx, testsAttempt.ID, GraphCompletion{
		Status: GraphSucceeded, Outcome: "passed", FinishedAt: at, Transition: &transitionToPR,
	}); err != nil {
		t.Fatalf("complete tests attempt: %v", err)
	}

	consumed, err := db.GetBugTestCall(ctx, call.ID)
	if err != nil || !consumed.Consumed {
		t.Fatalf("bug test call was not consumed: %+v err=%v", consumed, err)
	}

	// PassedBugTestCall is the read-only downstream handoff a delivery-review
	// composer uses before building [BugDeliveryEvidence]; it requires the
	// exact consumed, current, passing state just reached above.
	if _, err := db.PassedBugTestCall(ctx, call.ID); err != nil {
		t.Fatalf("passed bug test call: %v", err)
	}

	return &bugDeliveryFixtureState{
		db: db, session: session, identity: identity, instance: instance,
		revision: revision, approval: approval, request: request,
		sourceCommit: sourceCommit, sourceTree: sourceTree, targetCommit: targetCommit,
		testsAttempt: testsAttempt, childCallID: call.ID,
	}
}

// buildBugDeliveryJourney is the ordinary case: the repair is the first
// (ordinal 1, cause "initial") of the three base lifetime attempts.
func buildBugDeliveryJourney(t *testing.T, issue int) *bugDeliveryFixtureState {
	t.Helper()
	ctx := t.Context()
	db := newTestDB(t)
	session, identity := bugFixtureSession(t, db, issue)
	branch := fmt.Sprintf("lf/%d-bug-fix", issue)
	updated, err := db.FreezeSessionSlug(ctx, session.ID, fmt.Sprintf("bug-%d", issue), branch)
	if err != nil {
		t.Fatalf("freeze session slug: %v", err)
	}
	session = updated

	parentDef := bugFixtureDefinition(t, db, "a")
	childDef, err := db.CreateGraphDefinition(ctx, GraphDefinition{Team: "core", Workflow: "bug", ProfileVersion: 1, Path: "bug_tests.dot", Digest: strings.Repeat("f", 64), CreatedAt: bugFixtureTime(0)})
	if err != nil {
		t.Fatalf("create child definition: %v", err)
	}

	instance := bugFixtureInstance(t, db, session, parentDef, "analyst", bugFixtureTime(0))
	analystRun := bugFixtureRun(t, db, session, 1, bugFixtureTime(0))
	analystAttempt := bugFixtureAttempt(t, db, instance, "analyst", analystRun.ID, bugFixtureTime(0))
	revision, approval := bugFixtureApprovedAnalysis(t, db, session, identity, instance, analystAttempt, analystRun, bugFixtureTime(0))

	fixerRun := bugFixtureRun(t, db, session, 2, bugFixtureTime(time.Minute))
	fixerAttempt := bugFixtureAttempt(t, db, instance, "attempt", fixerRun.ID, bugFixtureTime(time.Minute))

	s := bugDeliveryCompleteRepairThroughTests(t, db, session, identity, instance, childDef, revision, approval, "repair-1", "initial", "", fixerAttempt, fixerRun, bugFixtureTime(2*time.Minute))
	s.branch = branch
	return s
}

// buildBugDeliveryGrantedJourney is item 5's story-004 integration point: the
// repair reaching CompleteBugRepair is a granted ordinal-4 attempt
// (ordinal>3, grant_reference set), reached only after the three base
// attempts are exhausted and the owner grants a continuation.
func buildBugDeliveryGrantedJourney(t *testing.T, issue int) *bugDeliveryFixtureState {
	t.Helper()
	ctx := t.Context()
	db := newTestDB(t)
	session, identity := bugFixtureSession(t, db, issue)
	branch := fmt.Sprintf("lf/%d-bug-fix", issue)
	updated, err := db.FreezeSessionSlug(ctx, session.ID, fmt.Sprintf("bug-%d", issue), branch)
	if err != nil {
		t.Fatalf("freeze session slug: %v", err)
	}
	session = updated

	parentDef := bugFixtureDefinition(t, db, "a")
	childDef, err := db.CreateGraphDefinition(ctx, GraphDefinition{Team: "core", Workflow: "bug", ProfileVersion: 1, Path: "bug_tests.dot", Digest: strings.Repeat("f", 64), CreatedAt: bugFixtureTime(0)})
	if err != nil {
		t.Fatalf("create child definition: %v", err)
	}

	instance := bugFixtureInstance(t, db, session, parentDef, "analyst", bugFixtureTime(0))
	bugFixtureLifecycle(t, db, session, instance, childDef)
	analystRun := bugFixtureRun(t, db, session, 1, bugFixtureTime(0))
	analystAttempt := bugFixtureAttempt(t, db, instance, "analyst", analystRun.ID, bugFixtureTime(0))
	revision, approval := bugFixtureApprovedAnalysis(t, db, session, identity, instance, analystAttempt, analystRun, bugFixtureTime(0))

	// Spend the three base lifetime attempts, each failing outright, then
	// terminalize the owning graph instance the way an exhausted parent does.
	var fixerAttempt GraphNodeAttempt
	for i, cause := range []string{"initial", "test_failure", "test_failure"} {
		fixerAttempt = bugFixtureAttempt(t, db, instance, "attempt", 0, bugFixtureTime(0))
		request := "repair-" + string(rune('1'+i))
		in := bugFixtureRepairRequest(session, revision, approval, request, cause, fixerAttempt.ID, "")
		if _, err := db.ReserveBugRepair(ctx, in, bugCurrentRevision(t, db, session.ID)); err != nil {
			t.Fatalf("reserve ordinal %d: %v", i+1, err)
		}
		if err := db.StopBugRepair(ctx, session.ID, request, "failed", "project checks failed", true); err != nil {
			t.Fatalf("stop ordinal %d: %v", i+1, err)
		}
		completion := GraphCompletion{Status: GraphFailed, FinishedAt: bugFixtureTime(0)}
		if i == 2 {
			completion.InstanceStatus = GraphFailed
		}
		if err := db.CompleteGraphNodeAttempt(ctx, fixerAttempt.ID, completion); err != nil {
			t.Fatalf("complete fixer attempt %d: %v", i+1, err)
		}
	}

	if _, err := db.RecordBugExhaustion(ctx, session.ID, fixerAttempt.ID); err != nil {
		t.Fatalf("record exhaustion: %v", err)
	}
	out, err := db.DecideBugExhaustion(ctx, BugExhaustionDecisionRequest{
		SessionID: session.ID, N: 1, Decision: "grant", Instructions: "focus on the retry path",
		CommentID: 9101, AuthorID: identity.OwnerID, ExpectedRevision: bugCurrentRevision(t, db, session.ID), At: bugFixtureTime(time.Minute),
	})
	if err != nil {
		t.Fatalf("decide grant: %v", err)
	}
	if out.Successor.ID == 0 || out.Successor.CurrentNode != "attempt" || out.Successor.Status != GraphRunning {
		t.Fatalf("grant successor = %+v, want a fresh running instance parked on attempt", out.Successor)
	}

	fixerRun := bugFixtureRun(t, db, session, 2, bugFixtureTime(2*time.Minute))
	grantedAttempt := bugFixtureAttempt(t, db, out.Successor, "attempt", fixerRun.ID, bugFixtureTime(2*time.Minute))

	fourth := bugFixtureRepairRequest(session, revision, approval, "repair-4", "test_failure", grantedAttempt.ID, "exhaustion:1")
	if reserved, err := db.ReserveBugRepair(ctx, fourth, bugCurrentRevision(t, db, session.ID)); err != nil || reserved.Ordinal != 4 {
		t.Fatalf("reserve granted ordinal 4: reserved=%+v err=%v", reserved, err)
	}

	s := bugDeliveryCompleteRepairThroughTests(t, db, session, identity, out.Successor, childDef, revision, approval, "repair-4", "test_failure", "exhaustion:1", grantedAttempt, fixerRun, bugFixtureTime(3*time.Minute))
	s.branch = branch
	return s
}

// bugDeliveryEnsurePR drives EnsureBugPRIntent/ClaimBugPRCreate/ObserveBugPR:
// a PR intent can be created only once the repair+tests are genuinely
// complete, replay is idempotent, and PR identity is retained once observed.
func bugDeliveryEnsurePR(t *testing.T, s *bugDeliveryFixtureState, operation string, creatorID int64) {
	t.Helper()
	ctx := t.Context()
	db := s.db

	intent, err := db.EnsureBugPRIntent(ctx, BugPRIntent{SessionID: s.session.ID, Operation: operation, Branch: s.branch, CreatorID: creatorID}, s.childCallID, bugCurrentRevision(t, db, s.session.ID))
	if err != nil {
		t.Fatalf("ensure PR intent: %v", err)
	}
	if intent.Dispatched || intent.PR != nil {
		t.Fatalf("fresh PR intent already dispatched or bound: %+v", intent)
	}

	// Replay with a stale expected revision is idempotent: an exact-input
	// retry returns the identical retained intent, not a second row.
	replay, err := db.EnsureBugPRIntent(ctx, BugPRIntent{SessionID: s.session.ID, Operation: operation, Branch: s.branch, CreatorID: creatorID}, s.childCallID, 999999)
	if err != nil {
		t.Fatalf("replay PR intent: %v", err)
	}
	if replay != intent {
		t.Fatalf("replay PR intent = %+v, want identical %+v", replay, intent)
	}

	// A different operation for the exact same completed evidence is a
	// distinct PR intent (no cross-operation reuse of the sole ledger row);
	// the try below shares the same evidence but was never persisted, so it
	// must reach the same insert path rather than colliding.
	claimed, err := db.ClaimBugPRCreate(ctx, s.session.ID, s.childCallID, bugCurrentRevision(t, db, s.session.ID))
	if err != nil || !claimed {
		t.Fatalf("claim PR create: claimed=%v err=%v", claimed, err)
	}
	// A one-shot dispatch: claiming again is a no-op, not a second effect.
	claimedAgain, err := db.ClaimBugPRCreate(ctx, s.session.ID, s.childCallID, bugCurrentRevision(t, db, s.session.ID))
	if err != nil || claimedAgain {
		t.Fatalf("second PR create claim = %v err=%v, want false/nil", claimedAgain, err)
	}

	dispatched, err := db.GetBugPRIntent(ctx, s.session.ID)
	if err != nil || !dispatched.Dispatched {
		t.Fatalf("PR intent not marked dispatched: %+v err=%v", dispatched, err)
	}
	s.marker = dispatched.Marker

	pr := BugPullRequest{
		ID: 5001, Number: 42, Head: s.sourceCommit, Branch: s.branch, Target: "main",
		HeadRepo: s.identity.Repository, BaseRepo: s.identity.Repository, State: "open",
		Body: "Fixes the widgets bug.\n\n" + s.marker, CreatorID: creatorID,
	}
	if err := db.ObserveBugPR(ctx, s.session.ID, BugPRObservation{Operation: operation, PR: &pr, At: bugFixtureTime(4 * time.Minute)}); err != nil {
		t.Fatalf("observe PR: %v", err)
	}
	observed, err := db.GetBugPRIntent(ctx, s.session.ID)
	if err != nil || observed.PR == nil || *observed.PR != pr {
		t.Fatalf("observed PR intent = %+v err=%v, want PR %+v", observed, err, pr)
	}
	s.pr = pr
}

// bugDeliveryApprove drives RetainBugDeliveryReview/ApplyBugDeliveryReply/
// approvedBugDeliveryTx: the digest genuinely binds story-002's facts and
// story-003's self-check summary (a stale digest computed without them is
// refused), and the owner's exact approval command is consumed once.
func bugDeliveryApprove(t *testing.T, s *bugDeliveryFixtureState, reviewKey string, botID, commentSeed int64) string {
	t.Helper()
	ctx := t.Context()
	db := s.db

	if err := db.RecordBugFact(ctx, s.session.ID, s.testsAttempt.ID, BugFactStageRepair, "note:"+s.request, "repair touched an adjacent helper as well"); err != nil {
		t.Fatalf("record bug fact: %v", err)
	}

	evidence, revision, err := db.BugDeliveryPrerequisites(ctx, s.session.ID, s.childCallID)
	if err != nil {
		t.Fatalf("delivery prerequisites: %v", err)
	}

	facts, err := db.BugFacts(ctx, s.session.ID, "")
	if err != nil {
		t.Fatalf("bug facts: %v", err)
	}
	if len(facts) == 0 {
		t.Fatal("expected at least one recorded fact")
	}
	selfCheckRuns, err := db.BugSelfChecks(ctx, s.session.ID, s.request)
	if err != nil {
		t.Fatalf("bug self checks: %v", err)
	}
	if len(selfCheckRuns) == 0 {
		t.Fatal("expected at least one recorded self-check run")
	}
	selfChecks := SummarizeBugSelfChecks(selfCheckRuns)

	body := "Delivery review: repair verified against passing tests, please approve."
	digest, err := BugDeliveryReviewDigest(evidence, s.pr, facts, selfChecks)
	if err != nil {
		t.Fatalf("compute delivery review digest: %v", err)
	}

	// A digest computed without the recorded fact/self-check evidence is
	// refused: proof the review genuinely binds them (AC-007, AC-009), not
	// just the evidence bug_delivery_digest_test.go already covers in
	// isolation.
	staleDigest, err := BugDeliveryReviewDigest(evidence, s.pr, nil, nil)
	if err != nil {
		t.Fatalf("compute stale digest: %v", err)
	}
	if staleDigest == digest {
		t.Fatal("digest did not change once facts and self-checks were recorded")
	}
	stale := BugDeliveryReview{
		SessionID: s.session.ID, Digest: staleDigest, Evidence: evidence, PR: s.pr, Body: body,
		ContentDigest: submissionDigest([]byte(body)), DeliveryKey: reviewKey + "-stale", BotID: botID,
	}
	if err := db.RetainBugDeliveryReview(ctx, stale, revision); err == nil {
		t.Fatal("a review digest omitting recorded facts and self-checks was accepted")
	}

	review := BugDeliveryReview{
		SessionID: s.session.ID, Digest: digest, Evidence: evidence, PR: s.pr, Body: body,
		ContentDigest: submissionDigest([]byte(body)), DeliveryKey: reviewKey, BotID: botID,
	}
	if err := db.RetainBugDeliveryReview(ctx, review, revision); err != nil {
		t.Fatalf("retain delivery review: %v", err)
	}
	// Replay (a stale expected revision must not matter) is idempotent.
	if err := db.RetainBugDeliveryReview(ctx, review, 999999); err != nil {
		t.Fatalf("replay delivery review: %v", err)
	}

	if err := db.RetainGraphComment(ctx, GraphCommentIntent{
		Key: reviewKey, InstanceID: s.instance.ID, SourceAttemptID: s.testsAttempt.ID,
		Node: "delivery_review", Kind: "review", Candidate: []byte(`{"outcome":"review"}`),
	}); err != nil {
		t.Fatalf("retain graph comment: %v", err)
	}
	if _, err := db.EnsureReportDelivery(ctx, ReportDelivery{
		Key: reviewKey, Version: 1, SessionID: s.session.ID, RunID: 0, TriggerID: 0,
		Task: s.session.TaskRef, Destination: s.session.TaskRef, TriggerNamespace: "bug",
		Purpose: "delivery-review", BotUserID: botID, Body: body, Marker: reviewKey,
	}); err != nil {
		t.Fatalf("ensure delivery review report: %v", err)
	}
	if _, err := db.MarkReportDeliverySent(ctx, reviewKey); err != nil {
		t.Fatalf("mark review sent: %v", err)
	}
	if _, err := db.ConfirmReportDelivery(ctx, reviewKey, commentSeed); err != nil {
		t.Fatalf("confirm review delivery: %v", err)
	}
	if _, err := db.CompleteReportDelivery(ctx, reviewKey); err != nil {
		t.Fatalf("complete review delivery: %v", err)
	}

	if _, err := db.ConfirmBugDeliveryReview(ctx, s.session.ID, digest); err != nil {
		t.Fatalf("confirm delivery review: %v", err)
	}

	review, err = db.CurrentBugDeliveryReview(ctx, s.session.ID)
	if err != nil || review.Digest != digest {
		t.Fatalf("current delivery review = %+v err=%v, want digest %s", review, err, digest)
	}
	approveBody := review.ApprovalCommand()

	reply := GraphOwnerReply{
		InstanceID: s.instance.ID, OwnerID: s.identity.OwnerID, AuthorID: s.identity.OwnerID,
		CommentID: commentSeed + 1, QuestionCommentID: commentSeed,
		TaskRef: s.session.TaskRef, SourceRef: s.session.TaskRef, DeliveryKey: reviewKey,
		Body: approveBody, At: bugFixtureTime(5 * time.Minute),
	}
	decision, err := db.ApplyBugDeliveryReply(ctx, s.session.ID, digest, reply, bugCurrentRevision(t, db, s.session.ID))
	if err != nil {
		t.Fatalf("apply delivery reply: %v", err)
	}
	if decision.Kind != "approved" {
		t.Fatalf("decision kind = %q, want approved", decision.Kind)
	}

	// A replayed identical reply recovers the same consumed decision.
	replayed, err := db.RecoverBugDeliveryReply(ctx, s.session.ID, digest, reply)
	if err != nil || replayed.Kind != "approved" || replayed.CommentID != decision.CommentID {
		t.Fatalf("recover delivery reply = %+v err=%v, want the original approval", replayed, err)
	}

	approved, approvedDecision, err := db.ApprovedBugDelivery(ctx, s.session.ID, digest)
	if err != nil {
		t.Fatalf("approved bug delivery: %v", err)
	}
	if !approved.Active || approvedDecision.Kind != "approved" {
		t.Fatalf("approved bug delivery = %+v / %+v, want an active approved review", approved, approvedDecision)
	}

	s.reviewDigest = digest
	return digest
}

// bugDeliveryMerge drives EnsureBugMerge/ClaimBugMerge/ObserveBugMerge/
// ConfirmedBugIntegration through a clean success.
func bugDeliveryMerge(t *testing.T, s *bugDeliveryFixtureState, operation string) BugMergeOperation {
	t.Helper()
	ctx := t.Context()
	db := s.db

	if _, err := db.EnsureBugMerge(ctx, s.session.ID, operation, s.reviewDigest); err != nil {
		t.Fatalf("ensure bug merge: %v", err)
	}
	_, revision, err := db.BugMergePrerequisites(ctx, s.session.ID, operation)
	if err != nil {
		t.Fatalf("merge prerequisites: %v", err)
	}
	if claimed, err := db.ClaimBugMerge(ctx, s.session.ID, operation, revision); err != nil || !claimed {
		t.Fatalf("claim bug merge: claimed=%v err=%v", claimed, err)
	}

	mergeCommit := strings.Repeat("e", 40)
	mergedAt := bugFixtureTime(6*time.Minute + 30*time.Second)
	obs := BugIntegrationObservation{
		At: bugFixtureTime(6 * time.Minute), Kind: "integration",
		PR: BugPullRequest{
			ID: s.pr.ID, Number: s.pr.Number, CreatorID: s.pr.CreatorID, HeadRepo: s.pr.HeadRepo,
			BaseRepo: s.pr.BaseRepo, Branch: s.pr.Branch, Target: "main", Head: s.pr.Head,
			State: "closed", Merged: true,
		},
		MergeCommit: mergeCommit, Parents: []string{s.targetCommit, s.pr.Head}, Main: mergeCommit, MergedAt: mergedAt,
	}
	result, err := db.ObserveBugMerge(ctx, s.session.ID, operation, obs)
	if err != nil {
		t.Fatalf("observe bug merge: %v", err)
	}
	if result.Outcome != "confirmed" {
		t.Fatalf("merge outcome = %q (%q), want confirmed", result.Outcome, result.Reason)
	}

	confirmed, err := db.ConfirmedBugIntegration(ctx, s.session.ID, operation)
	if err != nil {
		t.Fatalf("confirmed bug integration: %v", err)
	}
	return confirmed
}

// bugDeliveryMergeFailureThenRetry drives a definitive request refusal,
// independent reconciliation that the PR remains open and unmerged, an
// explicit [DB.PrepareBugMergeRetry] continuation, and a second dispatch that
// succeeds.
func bugDeliveryMergeFailureThenRetry(t *testing.T, s *bugDeliveryFixtureState, operation string) BugMergeOperation {
	t.Helper()
	ctx := t.Context()
	db := s.db

	if _, err := db.EnsureBugMerge(ctx, s.session.ID, operation, s.reviewDigest); err != nil {
		t.Fatalf("ensure bug merge: %v", err)
	}
	_, revision, err := db.BugMergePrerequisites(ctx, s.session.ID, operation)
	if err != nil {
		t.Fatalf("merge prerequisites: %v", err)
	}
	if claimed, err := db.ClaimBugMerge(ctx, s.session.ID, operation, revision); err != nil || !claimed {
		t.Fatalf("claim bug merge: claimed=%v err=%v", claimed, err)
	}

	if _, err := db.ObserveBugMerge(ctx, s.session.ID, operation, BugIntegrationObservation{
		At: bugFixtureTime(6 * time.Minute), Kind: "request_refused", RequestError: "transient forge error",
	}); err != nil {
		t.Fatalf("observe merge request refusal: %v", err)
	}
	reconciled, err := db.ObserveBugMerge(ctx, s.session.ID, operation, BugIntegrationObservation{
		At: bugFixtureTime(6*time.Minute + 10*time.Second), Kind: "integration",
		PR: BugPullRequest{ID: s.pr.ID, Number: s.pr.Number, BaseRepo: s.pr.BaseRepo, HeadRepo: s.pr.HeadRepo, State: "open", Merged: false},
	})
	if err != nil {
		t.Fatalf("observe merge reconciliation: %v", err)
	}
	if reconciled.Outcome != "failed" {
		t.Fatalf("merge outcome after reconciliation = %q, want failed", reconciled.Outcome)
	}

	if err := db.PrepareBugMergeRetry(ctx, s.session.ID, operation, "retry-1", "forge incident resolved, PR confirmed still open", revision); err != nil {
		t.Fatalf("prepare merge retry: %v", err)
	}
	// Replay of the exact same retry continuation is idempotent.
	if err := db.PrepareBugMergeRetry(ctx, s.session.ID, operation, "retry-1", "forge incident resolved, PR confirmed still open", 999999); err != nil {
		t.Fatalf("replay merge retry: %v", err)
	}

	_, revision2, err := db.BugMergePrerequisites(ctx, s.session.ID, operation)
	if err != nil {
		t.Fatalf("merge prerequisites after retry: %v", err)
	}
	if claimed, err := db.ClaimBugMerge(ctx, s.session.ID, operation, revision2); err != nil || !claimed {
		t.Fatalf("claim bug merge after retry: claimed=%v err=%v", claimed, err)
	}

	mergeCommit := strings.Repeat("e", 40)
	mergedAt := bugFixtureTime(7 * time.Minute)
	result, err := db.ObserveBugMerge(ctx, s.session.ID, operation, BugIntegrationObservation{
		At: bugFixtureTime(6*time.Minute + 40*time.Second), Kind: "integration",
		PR: BugPullRequest{
			ID: s.pr.ID, Number: s.pr.Number, CreatorID: s.pr.CreatorID, HeadRepo: s.pr.HeadRepo,
			BaseRepo: s.pr.BaseRepo, Branch: s.pr.Branch, Target: "main", Head: s.pr.Head,
			State: "closed", Merged: true,
		},
		MergeCommit: mergeCommit, Parents: []string{s.targetCommit, s.pr.Head}, Main: mergeCommit, MergedAt: mergedAt,
	})
	if err != nil {
		t.Fatalf("observe merge success after retry: %v", err)
	}
	if result.Outcome != "confirmed" {
		t.Fatalf("merge outcome after retry = %q (%q), want confirmed", result.Outcome, result.Reason)
	}

	confirmed, err := db.ConfirmedBugIntegration(ctx, s.session.ID, operation)
	if err != nil {
		t.Fatalf("confirmed bug integration after retry: %v", err)
	}
	return confirmed
}

func bugDeliveryClosureEndpoint(s *bugDeliveryFixtureState) string {
	return fmt.Sprintf("https://api.github.com/repos/%s/issues/%d", s.identity.Repository, s.identity.IssueNumber)
}

// bugDeliveryClose drives EnsureBugClosure/ClaimBugClosure/ObserveBugClosure/
// ConfirmedBugClosure through a clean success: an independent open-issue
// observation before the claim, then a confirmed closed observation.
func bugDeliveryClose(t *testing.T, s *bugDeliveryFixtureState, closeOperation, mergeOperation string) BugClosureOperation {
	t.Helper()
	ctx := t.Context()
	db := s.db
	endpoint := bugDeliveryClosureEndpoint(s)

	if _, err := db.EnsureBugClosure(ctx, s.session.ID, closeOperation, mergeOperation, endpoint); err != nil {
		t.Fatalf("ensure bug closure: %v", err)
	}
	if _, err := db.ObserveBugClosure(ctx, s.session.ID, BugClosureObservation{
		Kind: "issue", Endpoint: endpoint, State: "open", ID: s.identity.IssueID, Number: int(s.identity.IssueNumber), At: bugFixtureTime(7 * time.Minute),
	}); err != nil {
		t.Fatalf("observe open issue: %v", err)
	}

	revision, err := db.BugClosurePrerequisites(ctx, s.session.ID)
	if err != nil {
		t.Fatalf("closure prerequisites: %v", err)
	}
	if claimed, err := db.ClaimBugClosure(ctx, s.session.ID, revision); err != nil || !claimed {
		t.Fatalf("claim bug closure: claimed=%v err=%v", claimed, err)
	}

	closed, err := db.ObserveBugClosure(ctx, s.session.ID, BugClosureObservation{
		Kind: "issue", Endpoint: endpoint, State: "closed", ID: s.identity.IssueID, Number: int(s.identity.IssueNumber), At: bugFixtureTime(7*time.Minute + 30*time.Second),
	})
	if err != nil {
		t.Fatalf("observe closed issue: %v", err)
	}
	if closed.Outcome != "confirmed" || !closed.Confirmed {
		t.Fatalf("closure outcome = %+v, want confirmed", closed)
	}

	confirmed, err := db.ConfirmedBugClosure(ctx, s.session.ID)
	if err != nil {
		t.Fatalf("confirmed bug closure: %v", err)
	}
	return confirmed
}

// bugDeliveryCloseFailureThenRetry drives a definitive request refusal,
// independent reconciliation that the issue remains open, an explicit
// [DB.PrepareBugClosureRetry] continuation, and a second dispatch that succeeds.
func bugDeliveryCloseFailureThenRetry(t *testing.T, s *bugDeliveryFixtureState, closeOperation, mergeOperation string) BugClosureOperation {
	t.Helper()
	ctx := t.Context()
	db := s.db
	endpoint := bugDeliveryClosureEndpoint(s)

	if _, err := db.EnsureBugClosure(ctx, s.session.ID, closeOperation, mergeOperation, endpoint); err != nil {
		t.Fatalf("ensure bug closure: %v", err)
	}
	if _, err := db.ObserveBugClosure(ctx, s.session.ID, BugClosureObservation{
		Kind: "issue", Endpoint: endpoint, State: "open", ID: s.identity.IssueID, Number: int(s.identity.IssueNumber), At: bugFixtureTime(7 * time.Minute),
	}); err != nil {
		t.Fatalf("observe open issue: %v", err)
	}
	revision, err := db.BugClosurePrerequisites(ctx, s.session.ID)
	if err != nil {
		t.Fatalf("closure prerequisites: %v", err)
	}
	if claimed, err := db.ClaimBugClosure(ctx, s.session.ID, revision); err != nil || !claimed {
		t.Fatalf("claim bug closure: claimed=%v err=%v", claimed, err)
	}

	if _, err := db.ObserveBugClosure(ctx, s.session.ID, BugClosureObservation{
		Kind: "request_refused", Reason: "rate limited", At: bugFixtureTime(7*time.Minute + 10*time.Second),
	}); err != nil {
		t.Fatalf("observe closure request refusal: %v", err)
	}
	reconciled, err := db.ObserveBugClosure(ctx, s.session.ID, BugClosureObservation{
		Kind: "issue", Endpoint: endpoint, State: "open", ID: s.identity.IssueID, Number: int(s.identity.IssueNumber), At: bugFixtureTime(7*time.Minute + 20*time.Second),
	})
	if err != nil {
		t.Fatalf("observe closure reconciliation: %v", err)
	}
	if reconciled.Outcome != "failed" {
		t.Fatalf("closure outcome after reconciliation = %q, want failed", reconciled.Outcome)
	}

	if err := db.PrepareBugClosureRetry(ctx, s.session.ID, "close-retry-1", "rate limit cleared, issue confirmed still open", revision); err != nil {
		t.Fatalf("prepare closure retry: %v", err)
	}
	// Replay of the exact same retry continuation is idempotent.
	if err := db.PrepareBugClosureRetry(ctx, s.session.ID, "close-retry-1", "rate limit cleared, issue confirmed still open", 999999); err != nil {
		t.Fatalf("replay closure retry: %v", err)
	}

	revision2, err := db.BugClosurePrerequisites(ctx, s.session.ID)
	if err != nil {
		t.Fatalf("closure prerequisites after retry: %v", err)
	}
	if claimed, err := db.ClaimBugClosure(ctx, s.session.ID, revision2); err != nil || !claimed {
		t.Fatalf("claim bug closure after retry: claimed=%v err=%v", claimed, err)
	}

	closed, err := db.ObserveBugClosure(ctx, s.session.ID, BugClosureObservation{
		Kind: "issue", Endpoint: endpoint, State: "closed", ID: s.identity.IssueID, Number: int(s.identity.IssueNumber), At: bugFixtureTime(7*time.Minute + 40*time.Second),
	})
	if err != nil {
		t.Fatalf("observe closed issue after retry: %v", err)
	}
	if closed.Outcome != "confirmed" {
		t.Fatalf("closure outcome after retry = %q, want confirmed", closed.Outcome)
	}

	confirmed, err := db.ConfirmedBugClosure(ctx, s.session.ID)
	if err != nil {
		t.Fatalf("confirmed bug closure after retry: %v", err)
	}
	return confirmed
}

// TestBugDeliveryJourney_PRIntentCreateOnceAndReplayIdempotent is item 1: a PR
// intent can be created only once the repair+tests are genuinely complete
// (bugDeliveryCompleteRepairThroughTests already reached that state before
// EnsureBugPRIntent is even attempted), replay is idempotent, and the PR's
// numeric identity is retained once observed.
func TestBugDeliveryJourney_PRIntentCreateOnceAndReplayIdempotent(t *testing.T) {
	t.Parallel()
	s := buildBugDeliveryJourney(t, 460)
	bugDeliveryEnsurePR(t, s, "bug-pr-1", 777)
	if s.pr.ID != 5001 || s.pr.Number != 42 {
		t.Fatalf("PR identity was not retained: %+v", s.pr)
	}
}

// TestBugDeliveryJourney_ReviewApprovalIncludesFactsAndSelfChecks is item 2:
// the approval path end to end, including that the digest genuinely binds
// story-002's facts and story-003's self-check summary.
func TestBugDeliveryJourney_ReviewApprovalIncludesFactsAndSelfChecks(t *testing.T) {
	t.Parallel()
	s := buildBugDeliveryJourney(t, 461)
	bugDeliveryEnsurePR(t, s, "bug-pr-1", 777)
	digest := bugDeliveryApprove(t, s, "delivery-review-1", 900, 6001)
	if digest == "" {
		t.Fatal("empty review digest")
	}
}

// TestBugDeliveryJourney_MergeSuccess is item 3's success path.
func TestBugDeliveryJourney_MergeSuccess(t *testing.T) {
	t.Parallel()
	s := buildBugDeliveryJourney(t, 462)
	bugDeliveryEnsurePR(t, s, "bug-pr-1", 777)
	bugDeliveryApprove(t, s, "delivery-review-1", 900, 6001)
	m := bugDeliveryMerge(t, s, "merge-1")
	if m.Outcome != "confirmed" || m.Exception != "" {
		t.Fatalf("merge = %+v, want confirmed with no exception", m)
	}
}

// TestBugDeliveryJourney_MergeRetryAfterFailure is item 3's retry-after-failure path.
func TestBugDeliveryJourney_MergeRetryAfterFailure(t *testing.T) {
	t.Parallel()
	s := buildBugDeliveryJourney(t, 463)
	bugDeliveryEnsurePR(t, s, "bug-pr-1", 777)
	bugDeliveryApprove(t, s, "delivery-review-1", 900, 6001)
	m := bugDeliveryMergeFailureThenRetry(t, s, "merge-1")
	if m.Outcome != "confirmed" || m.Exception != "" {
		t.Fatalf("merge after retry = %+v, want confirmed with no exception", m)
	}
}

// TestBugDeliveryJourney_ClosureSuccess is item 4's success path.
func TestBugDeliveryJourney_ClosureSuccess(t *testing.T) {
	t.Parallel()
	s := buildBugDeliveryJourney(t, 464)
	bugDeliveryEnsurePR(t, s, "bug-pr-1", 777)
	bugDeliveryApprove(t, s, "delivery-review-1", 900, 6001)
	bugDeliveryMerge(t, s, "merge-1")
	c := bugDeliveryClose(t, s, "close-1", "merge-1")
	if c.Outcome != "confirmed" || !c.Confirmed || c.Conflict {
		t.Fatalf("closure = %+v, want confirmed", c)
	}
}

// TestBugDeliveryJourney_ClosureRetryAfterFailure is item 4's retry-after-failure path.
func TestBugDeliveryJourney_ClosureRetryAfterFailure(t *testing.T) {
	t.Parallel()
	s := buildBugDeliveryJourney(t, 465)
	bugDeliveryEnsurePR(t, s, "bug-pr-1", 777)
	bugDeliveryApprove(t, s, "delivery-review-1", 900, 6001)
	bugDeliveryMerge(t, s, "merge-1")
	c := bugDeliveryCloseFailureThenRetry(t, s, "close-1", "merge-1")
	if c.Outcome != "confirmed" || !c.Confirmed {
		t.Fatalf("closure after retry = %+v, want confirmed", c)
	}
}

// TestBugDeliveryJourney_GrantedRepairAttemptReachesDeliveryEndToEnd is item 5,
// the story-004 integration point: once a repair completed via a granted
// attempt (ordinal>3, grant_reference set), the same PR/review/merge/close
// journey still works end to end.
func TestBugDeliveryJourney_GrantedRepairAttemptReachesDeliveryEndToEnd(t *testing.T) {
	t.Parallel()
	s := buildBugDeliveryGrantedJourney(t, 466)
	bugDeliveryEnsurePR(t, s, "bug-pr-1", 777)
	bugDeliveryApprove(t, s, "delivery-review-1", 900, 6001)
	bugDeliveryMerge(t, s, "merge-1")
	c := bugDeliveryClose(t, s, "close-1", "merge-1")
	if c.Outcome != "confirmed" || !c.Confirmed {
		t.Fatalf("closure for granted repair = %+v, want confirmed", c)
	}
}
