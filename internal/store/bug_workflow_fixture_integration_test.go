package store

// This file is shared scaffolding for the Bug-workflow integration tests
// (bug_report_delivery_integration_test.go, bug_policy_downgrade_integration_test.go,
// bug_self_checks_integration_test.go, bug_exhaustion_workflow_integration_test.go).
// It builds a real Bug task end to end against the real embedded sqlite
// migration/store engine ([newTestDB]): a real session, a real "bug" workflow
// admission, real graph definitions/instances/attempts and one real approved
// analysis revision that later repair reservations authorize against. Nothing
// here is mocked; every row is written through the same store methods
// production code calls.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/buganalysis"
)

// bugFixture is one admitted Bug task with a pinned parent/child graph
// definition pair, ready for a caller to drive attempts, repairs, checks and
// delivery against.
type bugFixture struct {
	t       *testing.T
	db      *DB
	session Session
	ownerID int64

	parentDef GraphDefinition
	childDef  GraphDefinition
}

// newBugFixture admits a fresh session onto the standalone Bug workflow and
// records its canonical identity, exactly as the real admission path does
// before any analysis or repair work begins.
func newBugFixture(t *testing.T, issue int) *bugFixture {
	t.Helper()
	db := newTestDB(t)
	session := testSession(t, db, issue)
	const owner = int64(9001)

	if _, err := db.AdmitWorkflow(t.Context(), TaskWorkflowAdmission{SessionID: session.ID, Version: 1, Family: "standalone", Workflow: "bug", SourceTrigger: 1}); err != nil {
		t.Fatalf("admit bug workflow: %v", err)
	}
	if err := db.EnsureBugRecord(t.Context(), BugIdentity{SessionID: session.ID, Repository: "acme/api", IssueID: 5000 + int64(issue), IssueNumber: int64(issue), OwnerID: owner}); err != nil {
		t.Fatalf("ensure bug record: %v", err)
	}

	f := &bugFixture{t: t, db: db, session: session, ownerID: owner}
	f.parentDef = f.graphDefinition("bug-parent", "core/bug.dot")
	f.childDef = f.graphDefinition("bug-child", "core/bug_child.dot")
	return f
}

// graphDefinition pins one Bug-workflow graph definition under a distinct
// digest, mirroring how the real parent/child graphs are registered.
func (f *bugFixture) graphDefinition(seed, path string) GraphDefinition {
	f.t.Helper()
	def, err := f.db.CreateGraphDefinition(f.t.Context(), GraphDefinition{
		Team: "core", Workflow: "bug", ProfileVersion: 1, Path: path, Digest: submissionDigest([]byte(seed)),
	})
	if err != nil {
		f.t.Fatalf("create graph definition %s: %v", seed, err)
	}
	return def
}

// newInstance starts a fresh running parent graph instance at node, the same
// shape [EnsureBugLifecycle] pins the task's envelope against.
func (f *bugFixture) newInstance(node string) GraphInstance {
	f.t.Helper()
	instance, err := f.db.CreateGraphInstance(f.t.Context(), GraphInstance{
		SessionID: f.session.ID, DefinitionID: f.parentDef.ID, CurrentNode: node, StartedAt: f.now(),
	})
	if err != nil {
		f.t.Fatalf("create graph instance: %v", err)
	}
	return instance
}

func (f *bugFixture) now() time.Time { return time.Now().UTC() }

// ensureLifecycle pins instance as the task's parent envelope, as the real
// bug parent binding does once at admission.
func (f *bugFixture) ensureLifecycle(instance GraphInstance) {
	f.t.Helper()
	if _, err := f.db.EnsureBugLifecycle(f.t.Context(), f.session.ID, instance.ID, f.childDef.ID); err != nil {
		f.t.Fatalf("ensure bug lifecycle: %v", err)
	}
}

// revision reads the task's current optimistic revision directly, the same
// column every domain write bumps.
func (f *bugFixture) revision() int64 {
	f.t.Helper()
	var revision int64
	if err := f.db.sql.QueryRowContext(f.t.Context(), `SELECT revision FROM bug_tasks WHERE session_id=?`, f.session.ID).Scan(&revision); err != nil {
		f.t.Fatalf("read bug revision: %v", err)
	}
	return revision
}

// startAttempt reserves the next attempt on instance's current node, failing
// the test if the node does not match (the same invariant
// [DB.StartGraphNodeAttempt] enforces).
func (f *bugFixture) startAttempt(instance GraphInstance, node string) GraphNodeAttempt {
	return f.startAttemptForRun(instance, node, 0)
}

// startAttemptForRun is [bugFixture.startAttempt] with the attempt bound to
// an already-allocated run from its reservation, the shape
// [DB.ClaimBugAnalysisDispatch] and [DB.ClaimBugRepairDispatch] require:
// graph_node_attempts.run_id must already equal the dispatching run's id.
func (f *bugFixture) startAttemptForRun(instance GraphInstance, node string, runID int64) GraphNodeAttempt {
	f.t.Helper()
	attempt, err := f.db.StartGraphNodeAttempt(f.t.Context(), GraphAttemptStart{InstanceID: instance.ID, Node: node, RunID: runID, StartedAt: f.now()})
	if err != nil {
		f.t.Fatalf("start graph node attempt: %v", err)
	}
	return attempt
}

// finishAttemptFailed finalizes attempt as failed and, when terminalInstance
// is true, also terminalizes the owning graph instance in the same call —
// the shape a Bug parent that just exhausted its budget needs before a
// continuation may create its successor.
func (f *bugFixture) finishAttemptFailed(attempt GraphNodeAttempt, terminalInstance bool) {
	f.t.Helper()
	completion := GraphCompletion{Status: GraphFailed, FinishedAt: f.now()}
	if terminalInstance {
		completion.InstanceStatus = GraphFailed
	}
	if err := f.db.CompleteGraphNodeAttempt(f.t.Context(), attempt.ID, completion); err != nil {
		f.t.Fatalf("finish failed graph node attempt: %v", err)
	}
}

// approvedAnalysis is one fully-applied, owner-approved analysis revision:
// the exact shape [DB.ReserveBugRepair] and [DB.EnsureBugCheckPhase]'s
// callers require before any repair or check work may reserve against it.
type approvedAnalysis struct {
	Revision int64
	Digest   string
	Review   string
}

// bugAnalysisResultSnapshot is the retained application_result snapshot shape
// [DB.ApplyBugAnalysis] decodes: a completed result carrying one "done"
// analyst proposal.
type bugAnalysisResultSnapshot struct {
	Result struct {
		Outcome string `json:"outcome"`
		Payload struct {
			Bug struct {
				Response     string                  `json:"response"`
				Invocation   string                  `json:"invocation"`
				BaseRevision int64                   `json:"base_revision"`
				Analysis     buganalysis.BugAnalysis `json:"analysis"`
			} `json:"bug"`
		} `json:"payload"`
	} `json:"result"`
}

// buildApprovedAnalysis drives one real analyst pass end to end through the
// real analysis/result-binding pipeline: reserve, dispatch, a genuinely
// accepted application_results revision, a successful run, and a completed
// graph attempt bound to that result. The owner's review and approval
// columns are then planted directly (the same way store_test.go plants
// pre-migration schema state in TestMigration0003BackfillsAgentAndLeavesResumeTokenNull)
// because the multi-table owner-reply/publish machinery those columns come
// from in production is its own separately tested feature, not what these
// Bug-repair/check/exhaustion integration tests exist to cover.
func (f *bugFixture) buildApprovedAnalysis(instance GraphInstance) approvedAnalysis {
	return f.buildApprovedAnalysisWith(instance, nil)
}

// buildApprovedAnalysisWith is [bugFixture.buildApprovedAnalysis] with an
// optional mutator applied to the proposed analysis document before it is
// retained — used to exercise a genuine policy-class notice (a duplicated
// or over-count remedy boundary) through the real
// [buganalysis.BugAnalysis.Validate] path inside [DB.ApplyBugAnalysis],
// rather than calling [DB.RecordBugFact] directly.
func (f *bugFixture) buildApprovedAnalysisWith(instance GraphInstance, mutate func(*buganalysis.BugAnalysis)) approvedAnalysis {
	f.t.Helper()
	ctx := f.t.Context()

	segment, err := f.db.GetOrCreateSegment(ctx, f.session.ID, FirstSegmentOrdinal, "bug-analyst", AgentClaude)
	if err != nil {
		f.t.Fatalf("segment: %v", err)
	}
	run, err := f.db.AllocateRun(ctx, AllocateRunParams{
		SessionID: f.session.ID, SegmentID: segment.ID, Workflow: "bug", Agent: AgentClaude,
		TriggerCommentID: 1, StartedAt: f.now(), LogPath: func(int) string { return "/logs/bug-analyst.jsonl" },
	})
	if err != nil {
		f.t.Fatalf("allocate analyst run: %v", err)
	}
	attempt := f.startAttemptForRun(instance, instance.CurrentNode, run.ID)

	expected := f.revision()
	invocation := "analyst-1"
	inputDigest := submissionDigest([]byte("analyst-input-1"))
	if _, err = f.db.ReserveBugAnalysisPass(ctx, BugAnalysisPass{SessionID: f.session.ID, Invocation: invocation, InputDigest: inputDigest, GraphAttemptID: attempt.ID}, expected); err != nil {
		f.t.Fatalf("reserve analysis pass: %v", err)
	}
	if _, err = f.db.ClaimBugAnalysisDispatch(ctx, f.session.ID, invocation, inputDigest, run.ID); err != nil {
		f.t.Fatalf("claim analysis dispatch: %v", err)
	}

	var snapshot bugAnalysisResultSnapshot
	snapshot.Result.Outcome = "completed"
	snapshot.Result.Payload.Bug.Response = "done"
	snapshot.Result.Payload.Bug.Invocation = invocation
	snapshot.Result.Payload.Bug.BaseRevision = 0
	snapshot.Result.Payload.Bug.Analysis = buganalysis.BugAnalysis{
		Expected: "the fix works", Actual: "it does not",
		Reproduction: buganalysis.BugReproduction{Steps: []string{"run the failing case"}, Environment: "ci", Observation: "observed"},
		Cause:        "off-by-one",
		Evidence:     []string{"log line 42"},
		Remedy:       "adjust the boundary check",
		Exclusions:   []string{"unrelated module untouched"},
		Criteria:     []buganalysis.BugCriterion{{ID: "AC1", Requirement: "fix the boundary", Measurement: "test passes"}},
	}
	if mutate != nil {
		mutate(&snapshot.Result.Payload.Bug.Analysis)
	}
	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		f.t.Fatalf("marshal analysis snapshot: %v", err)
	}

	binding, err := f.db.BindResultAttempt(ctx, ResultAttempt{
		Workflow: "bug", Task: f.session.TaskRef, WorkContext: "bug-analysis", Attempt: "analyst-1",
		RunID: run.ID, SchemaVersion: 1, WorkflowVersion: 2, SubmissionVersion: 2,
	})
	if err != nil {
		f.t.Fatalf("bind result attempt: %v", err)
	}
	receipt, err := f.db.RecordResult(ctx, ResultRecord{
		Binding: binding, RequestID: "apply-analyst-1", ExpectedRevision: 0,
		Payload: []byte(`{"comment":"analyst result"}`), Diagnostics: []byte(`{"valid":true}`),
		Snapshot: snapshotJSON, Retired: []byte(`{}`), Accepted: true,
	})
	if err != nil {
		f.t.Fatalf("record analyst result: %v", err)
	}

	exitCode := int64(0)
	if _, err = f.db.FinishRun(ctx, run.ID, RunOutcome{Status: RunSucceeded, ExitCode: &exitCode}); err != nil {
		f.t.Fatalf("finish analyst run: %v", err)
	}
	if err = f.db.FinishBugAnalysisPass(ctx, f.session.ID, invocation, "succeeded", ""); err != nil {
		f.t.Fatalf("finish analysis pass: %v", err)
	}

	bindingPayload, err := json.Marshal(struct {
		ResultID       int64 `json:"result_id"`
		ResultRevision int64 `json:"result_revision"`
	}{receipt.ResultID, 1})
	if err != nil {
		f.t.Fatalf("marshal graph binding output: %v", err)
	}
	if err = f.db.CompleteGraphNodeAttempt(ctx, attempt.ID, GraphCompletion{
		Status: GraphSucceeded, Outcome: "success", FinishedAt: f.now(),
		Outputs: []GraphOutput{{Kind: GraphOutputJSON, Payload: bindingPayload, CreatedAt: f.now()}},
	}); err != nil {
		f.t.Fatalf("complete analyst graph attempt: %v", err)
	}

	applyExpected := f.revision()
	revision, err := f.db.ApplyBugAnalysis(ctx, BugAnalysisApplication{
		SessionID: f.session.ID, ExpectedRevision: applyExpected, Invocation: invocation, Operation: "apply-analyst-1",
		InputDigest: submissionDigest([]byte("apply-analyst-1")), ResultID: receipt.ResultID, ResultRevision: 1,
	})
	if err != nil {
		f.t.Fatalf("apply bug analysis: %v", err)
	}

	review := "review-1"
	if _, err = f.db.sql.ExecContext(ctx, `UPDATE bug_analyses SET review=?,review_active=1,approval_owner_id=?,approval_reply_id=?,approval_approved_at=? WHERE session_id=? AND revision=?`,
		review, f.ownerID, 42, FormatTime(f.now()), f.session.ID, revision.Revision); err != nil {
		f.t.Fatalf("plant analysis approval: %v", err)
	}

	return approvedAnalysis{Revision: revision.Revision, Digest: revision.Digest, Review: review}
}

// bugSourceSHA returns a fixed-shape 40-hex-character source identity, the
// git-SHA shape [ReserveBugRepair] and [EnsureBugCheckPhase] validate.
func bugSourceSHA(seed byte) string {
	sha := make([]byte, 40)
	for i := range sha {
		sha[i] = "0123456789abcdef"[(int(seed)+i)%16]
	}
	return string(sha)
}

// requireBugDotUnchangedSinceMainline asserts that both on-disk copies of the
// Bug graph's DOT source are byte-identical to each other and to the copy on
// the repository's main branch — the "before this feature" baseline a lean
// bug-workflow rewrite must not have added or removed a single graph edge to.
// It skips (rather than fails) when git or the main branch cannot be resolved
// in the checkout the test runs in, since that is an environment limitation,
// not evidence the graph changed.
func requireBugDotUnchangedSinceMainline(t *testing.T) {
	t.Helper()

	root, err := gitRepoRoot(t)
	if err != nil {
		t.Skipf("cannot resolve git repository root, skipping bug.dot baseline check: %v", err)
	}

	const teamsourcePath = "internal/teamsource/bug/bug.dot"
	const releasePath = "teams/source/bug/bug.dot"

	teamsource, err := os.ReadFile(filepath.Join(root, teamsourcePath))
	if err != nil {
		t.Fatalf("read %s: %v", teamsourcePath, err)
	}
	release, err := os.ReadFile(filepath.Join(root, releasePath))
	if err != nil {
		t.Fatalf("read %s: %v", releasePath, err)
	}
	if string(teamsource) != string(release) {
		t.Fatalf("%s and %s have diverged; the Bug graph's two retained copies must stay identical", teamsourcePath, releasePath)
	}

	baseline, err := gitShow(root, "main", teamsourcePath)
	if err != nil {
		t.Skipf("cannot read %s from the main branch, skipping bug.dot baseline check: %v", teamsourcePath, err)
	}
	if string(teamsource) != baseline {
		t.Fatalf("%s differs from the main branch's copy: this feature changed the Bug graph's edges", teamsourcePath)
	}
}

// gitRepoRoot resolves the checkout's repository root via git, so the test
// works regardless of the working directory `go test` happens to run from.
func gitRepoRoot(t *testing.T) (string, error) {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// gitShow reads path as it stood on ref, via the real local git history —
// nothing here reaches a network or an external service.
func gitShow(root, ref, path string) (string, error) {
	cmd := exec.Command("git", "show", ref+":"+path)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
