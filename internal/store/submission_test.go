package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func submissionTestDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func submissionFixture(t *testing.T, stage string) (*DB, SubmissionAttempt) {
	t.Helper()
	db, session, revisions := approvedFixture(t)
	if err := db.BindFeatureContract(t.Context(), FeatureContract{Parent: session.TaskRef, Generation: 1, ContentVersion: 2, SubmissionVersion: 2}); err != nil {
		t.Fatal(err)
	}
	for i := range revisions {
		revisions[i].Hash = submissionTestDigest([]byte(revisions[i].Title + "\x00" + revisions[i].Body))
	}
	if err := db.ApproveManagedStories(t.Context(), session.TaskRef, 1, revisions); err != nil {
		t.Fatal(err)
	}
	segment, err := db.GetOrCreateSegment(t.Context(), session.ID, FirstSegmentOrdinal, "submission-test", AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	mode := "implement"
	if stage == "validate" {
		mode = "validate"
	}
	run, err := db.AllocateRun(t.Context(), AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: mode, Agent: AgentClaude, TriggerCommentID: 456, LogPath: func(n int) string { return fmt.Sprintf("/logs/%d.jsonl", n) }})
	if err != nil {
		t.Fatal(err)
	}
	a := SubmissionAttempt{RunID: run.ID, Attempt: "attempt-1", Parent: session.TaskRef, Generation: 1, Stage: stage}
	for _, r := range revisions {
		a.Stories = append(a.Stories, SubmissionStoryBinding{Story: r.Key, IssueID: r.IssueID, Revision: r.Hash})
	}
	if err := db.BindSubmissionAttempt(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	return db, a
}

func submissionRequest(a SubmissionAttempt, id string) SubmissionRequest {
	return SubmissionRequest{RunID: a.RunID, Attempt: a.Attempt, RequestID: id, Story: a.Stories[0].Story, Revision: a.Stories[0].Revision, InputState: "valid", Payload: []byte(`{"outcome":"engineering_complete"}`), Diagnostics: []byte(`[]`)}
}

func submissionSnapshot(a SubmissionAttempt, ref string) SubmissionSnapshot {
	files := []SubmissionSnapshotFile{{Path: "internal/example.go", Content: []byte("package example\n"), SHA256: submissionTestDigest([]byte("package example\n"))}, {Path: "internal/removed.go", Content: []byte{}, SHA256: submissionTestDigest(nil), Deleted: true}}
	var manifest strings.Builder
	for _, f := range files {
		fmt.Fprintf(&manifest, "%s\x00%s\x00%t\n", f.Path, f.SHA256, f.Deleted)
	}
	return SubmissionSnapshot{RunID: a.RunID, Attempt: a.Attempt, Reference: ref, Parent: a.Parent, Story: a.Stories[0].Story, Revision: a.Stories[0].Revision, Generation: a.Generation, Digest: submissionTestDigest([]byte(manifest.String())), Files: files}
}

func TestSubmissionMigrationPreservesLegacyVersions(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "upgrade.db")
	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	applyMigrationsThrough(t, db, 27)
	for _, workflow := range []int{1, 2} {
		if _, err := db.sql.ExecContext(t.Context(), `INSERT INTO managed_features(parent_task,generation,state,ready,workflow_version) VALUES(?,3,'completed',1,?)`, fmt.Sprintf("parent-%d", workflow), workflow); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, workflow := range []int{1, 2} {
		parent := fmt.Sprintf("parent-%d", workflow)
		c, err := db.GetFeatureContract(t.Context(), parent)
		if err != nil {
			t.Fatal(err)
		}
		f, err := db.GetManagedFeature(t.Context(), parent)
		if err != nil {
			t.Fatal(err)
		}
		if c.ContentVersion != 1 || c.SubmissionVersion != 1 || c.Generation != 3 || f.WorkflowVersion != workflow || f.State != "completed" {
			t.Fatalf("migration changed legacy facts: %+v %+v", c, f)
		}
	}
}

func TestSubmissionContractAndProjectionImmutability(t *testing.T) {
	t.Parallel()
	db, session, _ := approvedFixture(t)
	contract := FeatureContract{Parent: session.TaskRef, Generation: 1, ContentVersion: 2, SubmissionVersion: 2}
	if err := db.BindFeatureContract(t.Context(), contract); err != nil {
		t.Fatal(err)
	}
	if err := db.BindFeatureContract(t.Context(), contract); err != nil {
		t.Fatal(err)
	}
	conflict := contract
	conflict.ContentVersion = 1
	conflict.SubmissionVersion = 1
	if err := db.BindFeatureContract(t.Context(), conflict); err == nil {
		t.Fatal("immutable contract changed")
	}
	p := SubmissionProjection{Parent: session.TaskRef, Generation: 1, Story: "US-1", IssueID: 2000, ContentVersion: 2, Title: "Story", Body: "First requirement", Content: []byte(`{"id":"AC-001"}`), Criteria: []string{"AC-001"}}
	p.Revision = submissionTestDigest([]byte(p.Title + "\x00" + p.Body))
	if err := db.PutSubmissionProjection(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	if err := db.PutSubmissionProjection(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	old := p
	p.Body = "Replacement requirement"
	p.Revision = submissionTestDigest([]byte(p.Title + "\x00" + p.Body))
	p.Content = []byte(`{"id":"AC-002"}`)
	p.Criteria = []string{"AC-002"}
	p.RetiredCriteria = []string{"AC-001"}
	if err := db.PutSubmissionProjection(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	history, err := db.ListCriterionHistory(t.Context(), p.Parent, p.Story)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0].ID != "AC-001" || history[0].FirstRevision != old.Revision || history[0].RetiredRevision != p.Revision || history[1].ID != "AC-002" {
		t.Fatalf("lost identity history: %+v", history)
	}
	got, err := db.GetSubmissionProjection(t.Context(), old.Parent, 1, old.Story, old.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != old.Body || string(got.Content) != string(old.Content) {
		t.Fatal("old projection rewritten")
	}
	p.Body = "Attempted reuse"
	p.Revision = submissionTestDigest([]byte(p.Title + "\x00" + p.Body))
	p.Criteria = []string{"AC-001"}
	p.RetiredCriteria = nil
	if err := db.PutSubmissionProjection(t.Context(), p); err == nil {
		t.Fatal("retired ID reused")
	}
	if _, err := db.GetSubmissionProjection(t.Context(), p.Parent, 1, p.Story, p.Revision); !errors.Is(err, ErrNotFound) {
		t.Fatalf("failed projection partially persisted: %v", err)
	}
}

func TestSubmissionConcurrentAllocationAndIdempotency(t *testing.T) {
	t.Parallel()
	db, a := submissionFixture(t, "engineering")
	const count = 16
	results := make(chan SubmissionReceipt, count)
	faults := make(chan error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, err := db.AllocateSubmission(t.Context(), submissionRequest(a, fmt.Sprintf("request-%02d", i)))
			if err != nil {
				faults <- err
			} else {
				results <- r
			}
		}(i)
	}
	wg.Wait()
	close(results)
	close(faults)
	for err := range faults {
		t.Error(err)
	}
	sequences := map[int]bool{}
	attempts := map[int]bool{}
	for r := range results {
		if sequences[r.Sequence] || attempts[r.StoryAttempt] {
			t.Fatalf("reused counter: %+v", r)
		}
		sequences[r.Sequence] = true
		attempts[r.StoryAttempt] = true
		replay, err := db.AllocateSubmission(t.Context(), submissionRequest(a, r.RequestID))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(r, replay) {
			t.Fatalf("retry changed receipt: %+v %+v", r, replay)
		}
	}
	if len(sequences) != count {
		t.Fatalf("only %d allocations", len(sequences))
	}
	q := submissionRequest(a, "request-00")
	q.Payload = []byte(`{"outcome":"blocked"}`)
	if _, err := db.AllocateSubmission(t.Context(), q); err == nil {
		t.Fatal("changed request content reused identity")
	}
	next, err := db.AllocateSubmission(t.Context(), submissionRequest(a, "next"))
	if err != nil {
		t.Fatal(err)
	}
	if next.Sequence != count+1 {
		t.Fatalf("rejected replay allocated counter: %d", next.Sequence)
	}
}

func TestSubmissionStaleAuthorityAndRollback(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"attempt", "revision", "generation", "run", "story"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			db, a := submissionFixture(t, "engineering")
			q := submissionRequest(a, "stale")
			switch kind {
			case "attempt":
				q.Attempt = "other"
			case "revision":
				q.Revision = strings.Repeat("f", 64)
			case "run":
				q.RunID++
			case "story":
				q.Story = "US-foreign"
			case "generation":
				bad := a
				bad.Generation++
				if err := db.BindSubmissionAttempt(t.Context(), bad); err == nil {
					t.Fatal("stale generation bound")
				}
				q.Attempt = "foreign"
			}
			if _, err := db.AllocateSubmission(t.Context(), q); err == nil {
				t.Fatal("stale authority allocated receipt")
			}
			r, err := db.AllocateSubmission(t.Context(), submissionRequest(a, "first"))
			if err != nil {
				t.Fatal(err)
			}
			if r.Sequence != 1 || r.StoryAttempt != 1 {
				t.Fatalf("failed allocation left counters: %+v", r)
			}
		})
	}
}

func TestSubmissionReceiptRecoveryKeepsIndependentStates(t *testing.T) {
	t.Parallel()
	db, a := submissionFixture(t, "engineering")
	q := submissionRequest(a, "receipt")
	r, err := db.AllocateSubmission(t.Context(), q)
	if err != nil {
		t.Fatal(err)
	}
	decision := SubmissionDecision{Engineering: "recorded", Acceptance: "not_applicable", Effects: "pending", Plan: "not_applicable"}
	if err := db.DecideSubmission(t.Context(), r.ID, decision); err != nil {
		t.Fatal(err)
	}
	if err := db.EndSubmissionAttempt(t.Context(), a.RunID, a.Attempt); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSubmissionEffects(t.Context(), r.ID, "failed"); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetSubmissionReceipt(t.Context(), a.RunID, q.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	if got.EngineeringState != "recorded" || got.AcceptanceState != "not_applicable" || got.EffectState != "failed" || got.InputState != "valid" || string(got.Diagnostics) != "[]" {
		t.Fatalf("receipt facts conflated: %+v", got)
	}
	if err := db.DecideSubmission(t.Context(), r.ID, decision); err == nil {
		t.Fatal("ended attempt made local decision")
	}
	if err := db.SetSubmissionEffects(t.Context(), r.ID, "delivered"); err != nil {
		t.Fatal(err)
	}
	got, err = db.GetSubmissionReceipt(t.Context(), a.RunID, q.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	if got.EngineeringState != "recorded" || got.EffectState != "delivered" {
		t.Fatalf("effect recovery changed engineering: %+v", got)
	}
}

func TestSubmissionRetainsImmutableReferencedEvidence(t *testing.T) {
	t.Parallel()
	db, a := submissionFixture(t, "engineering")
	snapshot := submissionSnapshot(a, "snapshot-1")
	if err := db.RetainSubmissionSnapshot(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	before, err := db.SubmissionRetainedBytes(t.Context(), a.RunID)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 256; i++ {
		if err := db.RetainSubmissionSnapshot(t.Context(), snapshot); err != nil {
			t.Fatal(err)
		}
	}
	after, err := db.SubmissionRetainedBytes(t.Context(), a.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("reference reuse duplicated retention: %d -> %d", before, after)
	}
	got, err := db.GetSubmissionSnapshot(t.Context(), a.RunID, snapshot.Reference)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, snapshot) {
		t.Fatalf("snapshot bytes/provenance lost: %+v %+v", got, snapshot)
	}
	check := SubmissionCheck{RunID: a.RunID, Attempt: a.Attempt, Reference: "check-1", SnapshotRef: snapshot.Reference, Name: "scoped test", Executed: true, Passed: false, Provenance: []byte(`{"command":"go test ./internal/example"}`), Result: []byte("FAIL actual check output\n")}
	if err := db.RetainSubmissionCheck(t.Context(), check); err != nil {
		t.Fatal(err)
	}
	if err := db.RetainSubmissionCheck(t.Context(), check); err != nil {
		t.Fatal(err)
	}
	stored, err := db.GetSubmissionCheck(t.Context(), a.RunID, check.SnapshotRef, check.Reference)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stored, check) {
		t.Fatal("failed check facts lost")
	}
	check.Passed = true
	if err := db.RetainSubmissionCheck(t.Context(), check); err == nil {
		t.Fatal("immutable failed check replaced with pass")
	}
	snapshot.Files[0].Content = []byte("package different\n")
	if err := db.RetainSubmissionSnapshot(t.Context(), snapshot); err == nil {
		t.Fatal("changed snapshot accepted")
	}
}

func TestSubmissionRejectsUnsafeSnapshotFacts(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"traversal", "absolute", "transport", "digest", "deletion", "parent", "generation", "attempt", "revision"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			db, a := submissionFixture(t, "engineering")
			s := submissionSnapshot(a, "bad")
			switch kind {
			case "traversal":
				s.Files[0].Path = "../secret"
			case "absolute":
				s.Files[0].Path = "/etc/secret"
			case "transport":
				s.Files[0].Path = ".litefactory-story-events/1/000001.json"
			case "digest":
				s.Digest = strings.Repeat("f", 64)
			case "deletion":
				s.Files[0].Deleted = true
			case "parent":
				s.Parent = "github:foreign/repo#1"
			case "generation":
				s.Generation++
			case "attempt":
				s.Attempt = "other"
			case "revision":
				s.Revision = strings.Repeat("e", 64)
			}
			if kind == "traversal" || kind == "absolute" || kind == "transport" || kind == "deletion" {
				var manifest strings.Builder
				for _, file := range s.Files {
					fmt.Fprintf(&manifest, "%s\x00%s\x00%t\n", file.Path, file.SHA256, file.Deleted)
				}
				s.Digest = submissionTestDigest([]byte(manifest.String()))
			}
			if err := db.RetainSubmissionSnapshot(t.Context(), s); err == nil {
				t.Fatal("unsafe/misattributed snapshot accepted")
			}
			count, err := db.SubmissionRetainedBytes(t.Context(), a.RunID)
			if err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatalf("failed snapshot charged bytes: %d", count)
			}
		})
	}
}

func TestSubmissionRestartRetainsDiagnosticsAndPendingEffects(t *testing.T) {
	t.Parallel()
	db, a := submissionFixture(t, "engineering")
	q := submissionRequest(a, "durable")
	q.Diagnostics = []byte(`[{"path":"$.evidence","expected":"array"}]`)
	q.InputState = "rejected"
	r, err := db.AllocateSubmission(t.Context(), q)
	if err != nil {
		t.Fatal(err)
	}
	before, err := db.SubmissionRetainedBytes(t.Context(), a.RunID)
	if err != nil {
		t.Fatal(err)
	}
	path := db.Path()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err := reopened.GetSubmissionReceipt(t.Context(), a.RunID, q.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r, got) {
		t.Fatalf("restart altered receipt: %+v %+v", r, got)
	}
	pending, err := reopened.ListPendingSubmissions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].ID != r.ID {
		t.Fatalf("pending receipt lost: %+v", pending)
	}
	after, err := reopened.SubmissionRetainedBytes(t.Context(), a.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("retained accounting lost on restart")
	}
	if err := reopened.DecideSubmission(t.Context(), r.ID, SubmissionDecision{Engineering: "recorded", Acceptance: "not_applicable", Effects: "pending", Plan: "not_applicable"}); err == nil {
		t.Fatal("rejected input obtained engineering success")
	}
}

func TestSubmissionRollsBackCounterAndBudgetAfterInsertFailure(t *testing.T) {
	t.Parallel()
	db, a := submissionFixture(t, "engineering")
	if _, err := db.sql.ExecContext(t.Context(), `CREATE TRIGGER fail_submission BEFORE INSERT ON submission_receipts BEGIN SELECT RAISE(ABORT,'injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AllocateSubmission(t.Context(), submissionRequest(a, "failed")); err == nil {
		t.Fatal("injected insert failure ignored")
	}
	count, err := db.SubmissionRetainedBytes(t.Context(), a.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rolled-back request retained %d bytes", count)
	}
	if _, err := db.sql.ExecContext(t.Context(), `DROP TRIGGER fail_submission`); err != nil {
		t.Fatal(err)
	}
	next, err := db.AllocateSubmission(t.Context(), submissionRequest(a, "after"))
	if err != nil {
		t.Fatal(err)
	}
	if next.Sequence != 1 || next.StoryAttempt != 1 {
		t.Fatalf("rollback consumed sequence: %+v", next)
	}
}

func TestSubmissionRunBudgetRejectsOverflowAtomically(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"request", "snapshot", "check"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			db, a := submissionFixture(t, "engineering")
			snapshot := submissionSnapshot(a, "snapshot")
			if kind == "check" {
				if err := db.RetainSubmissionSnapshot(t.Context(), snapshot); err != nil {
					t.Fatal(err)
				}
			}
			// Seed the durable accounting boundary; large content need not be allocated to
			// exercise the actual SQLite transaction's last-byte overflow behavior.
			if _, err := db.sql.ExecContext(t.Context(), `UPDATE submission_run_counters SET retained_bytes=? WHERE run_id=?`, MaxSubmissionRetainedBytes-1, a.RunID); err != nil {
				t.Fatal(err)
			}
			var err error
			switch kind {
			case "request":
				_, err = db.AllocateSubmission(t.Context(), submissionRequest(a, "oversized"))
			case "snapshot":
				err = db.RetainSubmissionSnapshot(t.Context(), snapshot)
			case "check":
				err = db.RetainSubmissionCheck(t.Context(), SubmissionCheck{RunID: a.RunID, Attempt: a.Attempt, Reference: "check", SnapshotRef: snapshot.Reference, Name: "test", Executed: true, Passed: true, Provenance: []byte("command"), Result: []byte("passed")})
			}
			if err == nil {
				t.Fatal("accepted run-budget overflow")
			}
			size, err := db.SubmissionRetainedBytes(t.Context(), a.RunID)
			if err != nil {
				t.Fatal(err)
			}
			if size != MaxSubmissionRetainedBytes-1 {
				t.Fatalf("overflow mutated accounting: %d", size)
			}
		})
	}
}

// submissionAssessmentDecision allocates a valid validate-stage receipt and the
// matching accepted decision for the fixture's first story.
func submissionAssessmentDecision(t *testing.T, db *DB, a SubmissionAttempt) (SubmissionReceipt, AcceptanceDecision) {
	t.Helper()
	r, err := db.AllocateSubmission(t.Context(), submissionRequest(a, "assessment"))
	if err != nil {
		t.Fatal(err)
	}
	revisions, err := db.ListApprovedStoryRevisions(t.Context(), a.Parent, a.Generation)
	if err != nil {
		t.Fatal(err)
	}
	var s ApprovedStoryRevision
	for _, candidate := range revisions {
		if candidate.Key == a.Stories[0].Story {
			s = candidate
		}
	}
	if s.Key == "" {
		t.Fatal("approved revision missing")
	}
	findings := AcceptanceFindings{Criteria: []AcceptanceFinding{{Name: "AC-001", Status: "satisfied", Classification: "blocking", Evidence: `["retained"]`}}}
	for _, name := range []string{"code", "architecture", "plan"} {
		findings.Reviews = append(findings.Reviews, AcceptanceFinding{Name: name, Status: "satisfied", Classification: "blocking", Evidence: `["reviewed"]`})
	}
	encoded, err := json.Marshal(findings)
	if err != nil {
		t.Fatal(err)
	}
	d := AcceptanceDecision{RunID: a.RunID, Parent: a.Parent, Generation: a.Generation, Story: s.Key, IssueNumber: s.IssueNumber, IssueID: s.IssueID, ParentID: s.ParentID, Revision: s.Hash, Snapshot: "snapshot-1", FrozenBody: s.Body, FindingsJSON: string(encoded), Accepted: findings.Accepted()}
	if !d.Accepted {
		t.Fatal("fixture findings not accepted")
	}
	return r, d
}

func followupMaterialized(t *testing.T, db *DB, runID int64) bool {
	t.Helper()
	var n int
	if err := db.sql.QueryRowContext(t.Context(), `SELECT count(*) FROM followup_materializations WHERE run_id=?`, runID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n == 1
}

func followupDebt(t *testing.T, db *DB, runID int64, parent string) (assessment, parentPending bool) {
	t.Helper()
	assessments, err := db.ListFollowupAssessments(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range assessments {
		if a.RunID == runID {
			assessment = true
		}
	}
	parents, err := db.ListPendingFollowupParents(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range parents {
		if p == parent {
			parentPending = true
		}
	}
	return assessment, parentPending
}

// Issue #370 (C1): a submission-v2 marker is recorded as follow-up-materialized
// in the same transaction, so the legacy follow-up parser never sees it.
func TestSubmissionAssessmentMarkerIsFollowupMaterialized(t *testing.T) {
	t.Parallel()
	db, a := submissionFixture(t, "validate")
	r, d := submissionAssessmentDecision(t, db, a)
	if err := db.CommitSubmissionAssessment(t.Context(), r.ID, d); err != nil {
		t.Fatal(err)
	}
	marker, err := db.GetValidationAssessment(t.Context(), a.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if marker.ContextJSON != SubmissionAssessmentMarker || marker.ResultJSON != SubmissionAssessmentMarker {
		t.Fatalf("marker row not written: %+v", marker)
	}
	if !followupMaterialized(t, db, a.RunID) {
		t.Fatal("v2 marker run not recorded in followup_materializations")
	}
	if assessment, pending := followupDebt(t, db, a.RunID, a.Parent); assessment || pending {
		t.Fatalf("v2 marker still reported as follow-up debt: assessment=%t parent=%t", assessment, pending)
	}
	got, err := db.GetSubmissionReceipt(t.Context(), a.RunID, r.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	if got.AcceptanceState != "accepted" {
		t.Fatalf("assessment decision changed: %+v", got)
	}
}

// Issue #370 (C2): a pre-existing v1 assessment for the same run keeps its
// follow-up debt; only a genuine marker row is marked.
func TestSubmissionAssessmentKeepsExistingV1FollowupDebt(t *testing.T) {
	t.Parallel()
	db, a := submissionFixture(t, "validate")
	v1 := ValidationAssessment{RunID: a.RunID, Parent: a.Parent, Generation: a.Generation, ContextJSON: `{"version":1}`, ResultJSON: `{"stories":[]}`}
	if err := db.RetainValidationAssessment(t.Context(), v1); err != nil {
		t.Fatal(err)
	}
	r, d := submissionAssessmentDecision(t, db, a)
	if err := db.CommitSubmissionAssessment(t.Context(), r.ID, d); err != nil {
		t.Fatal(err)
	}
	if followupMaterialized(t, db, a.RunID) {
		t.Fatal("v2 commit suppressed a pre-existing v1 assessment")
	}
	got, err := db.GetValidationAssessment(t.Context(), a.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if got != v1 {
		t.Fatalf("v1 assessment rewritten: %+v", got)
	}
	if assessment, pending := followupDebt(t, db, a.RunID, a.Parent); !assessment || !pending {
		t.Fatalf("v1 follow-up debt lost: assessment=%t parent=%t", assessment, pending)
	}
}

// Issue #370 (C3): migration 0077 backfills existing unmaterialized marker rows
// (production runs 430 and 564), leaves v1 rows alone and is idempotent.
func TestSubmissionMarkerFollowupMigrationBackfills(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 370)
	var backfill migration
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range migrations {
		if m.version == 77 {
			backfill = m
		}
	}
	if backfill.name != "submission_marker_followups" {
		t.Fatalf("migration 0077 missing: %+v", backfill)
	}
	if !strings.Contains(backfill.sql, SubmissionAssessmentMarker) {
		t.Fatal("migration marker literal differs from SubmissionAssessmentMarker")
	}
	markerA := historicalRun(t, db, session.ID, 4300, "validate")
	markerB := historicalRun(t, db, session.ID, 5640, "validate")
	legacy := historicalRun(t, db, session.ID, 3700, "validate")
	for _, runID := range []int64{markerA.ID, markerB.ID} {
		if _, err := db.sql.ExecContext(t.Context(), `INSERT INTO validation_assessments(run_id,parent_task,generation,context_json,result_json,materialized) VALUES(?,?,1,?,?,1)`, runID, session.TaskRef, SubmissionAssessmentMarker, SubmissionAssessmentMarker); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.sql.ExecContext(t.Context(), `INSERT INTO validation_assessments(run_id,parent_task,generation,context_json,result_json,materialized) VALUES(?,?,1,'{"version":1}','{"stories":[]}',0)`, legacy.ID, session.TaskRef); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		t.Helper()
		var out string
		if err := db.sql.QueryRowContext(t.Context(), `SELECT group_concat(run_id||'|'||parent_task||'|'||generation||'|'||context_json||'|'||result_json||'|'||materialized, ';') FROM (SELECT * FROM validation_assessments ORDER BY run_id)`).Scan(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	before := snapshot()
	for i := 0; i < 2; i++ {
		if _, err := db.sql.ExecContext(t.Context(), backfill.sql); err != nil {
			t.Fatalf("migration run %d: %v", i+1, err)
		}
		for _, runID := range []int64{markerA.ID, markerB.ID} {
			if !followupMaterialized(t, db, runID) {
				t.Fatalf("run %d: marker %d not backfilled", i+1, runID)
			}
		}
		if followupMaterialized(t, db, legacy.ID) {
			t.Fatalf("run %d: v1 assessment backfilled", i+1)
		}
		var n int
		if err := db.sql.QueryRowContext(t.Context(), `SELECT count(*) FROM followup_materializations`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 2 {
			t.Fatalf("run %d: followup_materializations rows=%d, want 2", i+1, n)
		}
		if after := snapshot(); after != before {
			t.Fatalf("run %d: assessment rows changed:\n%s\n%s", i+1, before, after)
		}
	}
	if assessment, _ := followupDebt(t, db, markerA.ID, session.TaskRef); assessment {
		t.Fatal("backfilled marker still listed")
	}
	if assessment, pending := followupDebt(t, db, legacy.ID, session.TaskRef); !assessment || !pending {
		t.Fatal("v1 follow-up debt lost after backfill")
	}
}

// Issue #370 defense in depth: a marker row without a materialization record is
// still never offered to the legacy follow-up parser.
func TestFollowupListsExcludeSubmissionMarkerRows(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 371)
	run := historicalRun(t, db, session.ID, 4301, "validate")
	if _, err := db.sql.ExecContext(t.Context(), `INSERT INTO validation_assessments(run_id,parent_task,generation,context_json,result_json,materialized) VALUES(?,?,1,?,?,1)`, run.ID, session.TaskRef, SubmissionAssessmentMarker, SubmissionAssessmentMarker); err != nil {
		t.Fatal(err)
	}
	if assessment, pending := followupDebt(t, db, run.ID, session.TaskRef); assessment || pending {
		t.Fatalf("unmaterialized marker offered to follow-ups: assessment=%t parent=%t", assessment, pending)
	}
}

func TestSubmissionAttemptReplacementRevokesOldAuthority(t *testing.T) {
	t.Parallel()
	db, a := submissionFixture(t, "validate")
	first, err := db.AllocateSubmission(t.Context(), submissionRequest(a, "old"))
	if err != nil {
		t.Fatal(err)
	}
	replacement := a
	replacement.Attempt = "attempt-2"
	if err := db.BindSubmissionAttempt(t.Context(), replacement); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AllocateSubmission(t.Context(), submissionRequest(a, "stale")); err == nil {
		t.Fatal("old attempt still allocates")
	}
	if err := db.DecideSubmission(t.Context(), first.ID, SubmissionDecision{Engineering: "not_applicable", Acceptance: "accepted", Effects: "pending", Plan: "not_applicable"}); err == nil {
		t.Fatal("old attempt granted acceptance")
	}
	if err := db.BindSubmissionAttempt(t.Context(), a); err == nil {
		t.Fatal("revoked attempt revived")
	}
	if _, err := db.GetSubmissionReceipt(t.Context(), a.RunID, "old"); err != nil {
		t.Fatal("old correlation lost", err)
	}
	current, err := db.AllocateSubmission(t.Context(), submissionRequest(replacement, "current"))
	if err != nil {
		t.Fatal(err)
	}
	if current.Sequence != 2 {
		t.Fatalf("replacement reset run counter: %+v", current)
	}
	decision := SubmissionDecision{Engineering: "not_applicable", Acceptance: "unverified", Effects: "pending", Plan: "not_applicable"}
	if err := db.DecideSubmission(t.Context(), current.ID, decision); err != nil {
		t.Fatal(err)
	}
	decision.Acceptance = "accepted"
	if err := db.DecideSubmission(t.Context(), current.ID, decision); err == nil {
		t.Fatal("terminal decision rewritten")
	}
}
