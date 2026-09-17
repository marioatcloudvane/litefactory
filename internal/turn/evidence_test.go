package turn

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
)

func evidenceFixtureService(t *testing.T, db *store.DB, session *SubmissionSession, mode string, workflow int) (*EvidenceService, *ReceiptService) {
	t.Helper()
	checkpoint := session.CheckpointBinding()
	b, err := db.BindResultAttempt(t.Context(), store.ResultAttempt{Task: checkpoint.Parent, RunID: checkpoint.RunID, Attempt: checkpoint.Attempt, Mode: mode, WorkContext: "evidence-fixture", SchemaVersion: 1, WorkflowVersion: workflow, SubmissionVersion: 2})
	mustProgress(t, err)
	receipts, err := NewReceiptService(db, b, nil, map[string]FamilyProjection{"submission": EvidenceReceiptProjection(db), "tool:evidence_submit": EvidenceReceiptProjection(db)})
	mustProgress(t, err)
	service, err := NewEvidenceService(session, receipts)
	mustProgress(t, err)
	return service, receipts
}

func evidenceSubmitFixture(t *testing.T, service *EvidenceService, receipts *ReceiptService, id string, contribution agent.Contribution) FamilyReceipt {
	t.Helper()
	payload, err := json.Marshal(contribution)
	mustProgress(t, err)
	_, _, err = receipts.Begin(t.Context(), id, "evidence_submit", payload)
	mustProgress(t, err)
	raw, err := service.HandleSubmit(t.Context(), id, payload)
	mustProgress(t, err)
	var got FamilyReceipt
	mustProgress(t, json.Unmarshal(raw, &got))
	if strings.Contains(string(raw), "package sample") || strings.Contains(string(raw), "capture") {
		t.Fatal("receipt exposed snapshot content", string(raw))
	}
	return got
}

func TestEvidenceDurableCheckpointReplayAndLostLink(t *testing.T) {
	for _, workflow := range []int{store.WorkflowSeparated, store.WorkflowLegacy} {
		name, stage := "separated", "engineering"
		if workflow == store.WorkflowLegacy {
			name, stage = "legacy", "legacy"
		}
		t.Run(name, func(t *testing.T) {
			f, forge, session, options := submissionSessionFixture(t, workflow)
			service, receipts := evidenceFixtureService(t, f.db, session, "implement", workflow)
			c := capturedContribution(t, options.Worktree, "US-1", stage)
			payload, err := json.Marshal(c)
			mustProgress(t, err)
			preflight, err := service.Preflight(t.Context(), payload)
			mustProgress(t, err)
			if !preflight.Valid {
				t.Fatalf("preflight: %+v", preflight)
			}
			if _, err = f.db.EngineeringEvidence(t.Context(), f.session.TaskRef, 1, "US-1"); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("preflight recorded proof: %v", err)
			}
			const id = "evidence-lost-link"
			_, _, err = receipts.Begin(t.Context(), id, "evidence_submit", payload)
			mustProgress(t, err)
			// Crash boundary: checkpoint is durable, but the family ledger has no link.
			raw, err := session.SubmitCheckpoint(t.Context(), id, payload)
			mustProgress(t, err)
			var original store.SubmissionReceipt
			mustProgress(t, json.Unmarshal(raw, &original))
			if original.EngineeringState != "recorded" {
				t.Fatalf("checkpoint: %+v", original)
			}
			reopened, err := store.Open(t.Context(), f.db.Path())
			mustProgress(t, err)
			defer reopened.Close()
			mustProgress(t, reopened.Migrate(t.Context()))
			reader, err := NewReceiptService(reopened, receipts.Binding(), nil, map[string]FamilyProjection{"submission": EvidenceReceiptProjection(reopened), "tool:evidence_submit": EvidenceReceiptProjection(reopened)})
			mustProgress(t, err)
			before, beforeErr := f.db.EngineeringEvidence(t.Context(), f.session.TaskRef, 1, "US-1")
			if workflow == store.WorkflowSeparated {
				mustProgress(t, beforeErr)
			}
			for range 3 {
				got, e := reader.Get(t.Context(), id)
				mustProgress(t, e)
				if got.States.Engineering != "recorded" || got.Disposition != "pending" {
					t.Fatalf("lost link observation: %+v", got)
				}
			}
			// Changed current bytes prove replay does not attempt a second capture.
			mustProgress(t, os.WriteFile(filepath.Join(options.Worktree, "US-1.go"), []byte("package changed\n"), 0600))
			for range 3 {
				got := evidenceSubmitFixture(t, service, receipts, id, c)
				if got.States.Engineering != "recorded" || got.Reference.ID != original.ID {
					t.Fatalf("replay: %+v", got)
				}
			}
			after, afterErr := f.db.EngineeringEvidence(t.Context(), f.session.TaskRef, 1, "US-1")
			if !errors.Is(afterErr, beforeErr) || !reflect.DeepEqual(before, after) {
				t.Fatal("retry changed retained engineering evidence")
			}
			final, err := session.CheckpointReceipt(t.Context(), id)
			mustProgress(t, err)
			if final.ID != original.ID || final.Sequence != original.Sequence {
				t.Fatal("retry allocated another checkpoint", final)
			}
			run, err := f.db.GetRun(t.Context(), options.RunID)
			mustProgress(t, err)
			if run.Mode != "implement" {
				t.Fatal("evidence changed mode", run.Mode)
			}
			if workflow == store.WorkflowSeparated && (final.AcceptanceState == "accepted" || forge.children[200].State != "open") {
				t.Fatal("engineering completed acceptance", final)
			}
		})
	}
}

func TestEvidencePreflightRepeatsCurrentSnapshotValidation(t *testing.T) {
	f, _, session, options := submissionSessionFixture(t, store.WorkflowSeparated)
	service, receipts := evidenceFixtureService(t, f.db, session, "implement", store.WorkflowSeparated)
	c := capturedContribution(t, options.Worktree, "US-1", "engineering")
	payload, err := json.Marshal(c)
	mustProgress(t, err)
	d, err := service.Preflight(t.Context(), payload)
	mustProgress(t, err)
	if !d.Valid {
		t.Fatalf("preflight: %+v", d)
	}
	mustProgress(t, os.WriteFile(filepath.Join(options.Worktree, "US-1.go"), []byte("package different\n"), 0600))
	got := evidenceSubmitFixture(t, service, receipts, "changed-after-preflight", c)
	if got.States.Engineering == "recorded" || got.Disposition != "rejected" {
		t.Fatalf("stale capture accepted: %+v", got)
	}
	if _, err = f.db.EngineeringEvidence(t.Context(), f.session.TaskRef, 1, "US-1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("invalid capture retained proof: %v", err)
	}
	mustProgress(t, session.Close(t.Context()))
	if _, err = service.Preflight(t.Context(), payload); !errors.Is(err, store.ErrResultAuthority) {
		t.Fatalf("closed checkpoint accepted: %v", err)
	}
}

func TestEvidenceAssessmentRetainsHistoricalSnapshot(t *testing.T) {
	f, forge, engineering, options := submissionSessionFixture(t, store.WorkflowSeparated)
	service, receipts := evidenceFixtureService(t, f.db, engineering, "implement", store.WorkflowSeparated)
	c := capturedContribution(t, options.Worktree, "US-1", "engineering")
	evidenceSubmitFixture(t, service, receipts, "engineering", c)
	retained, err := f.db.EngineeringEvidence(t.Context(), f.session.TaskRef, 1, "US-1")
	mustProgress(t, err)
	event, err := agent.ParseStoryEvent(retained.Payload)
	mustProgress(t, err)
	mustProgress(t, engineering.Close(t.Context()))
	code := int64(0)
	_, err = f.db.FinishRun(t.Context(), options.RunID, store.RunOutcome{Status: store.RunSucceeded, ExitCode: &code})
	mustProgress(t, err)
	run, err := f.db.GetRun(t.Context(), options.RunID)
	mustProgress(t, err)
	next, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: f.session.ID, SegmentID: run.SegmentID, Mode: "validate", Agent: store.AgentClaude, TriggerCommentID: 2, LogPath: func(int) string { return "validate.log" }})
	mustProgress(t, err)
	validation := &agent.ValidationContext{Version: 1, Parent: f.session.TaskRef, Generation: 1, Stories: []agent.ValidationStory{{Story: options.Bundle.Stories[0], Criteria: []string{"AC-001"}, Snapshot: event.Evidence.ImplementationDigest, Evidence: event.Evidence, EvidenceStatus: "satisfied"}}}
	session, err := StartSubmissionSession(t.Context(), SubmissionSessionOptions{DB: f.db, Progress: f.opts.StoryProgress, Paths: f.opts.Paths, RunID: next.ID, Worktree: options.Worktree, Validation: validation})
	mustProgress(t, err)
	defer session.Close(t.Context())
	assess, ledger := evidenceFixtureService(t, f.db, session, "validate", store.WorkflowSeparated)
	contribution := agent.Contribution{Stage: "validate", Story: "US-1", Outcome: "assessed", Assessment: &agent.AssessmentContribution{SnapshotID: event.Evidence.ImplementationDigest, Criteria: []agent.ContributionFinding{{Name: "AC-001", Status: "satisfied", Classification: "blocking", Evidence: []agent.EvidenceExplanation{{CheckIDs: []string{event.Evidence.Tests[0].Name}, Summary: "Retained fixture execution."}}}}, Reviews: []agent.ContributionFinding{}}}
	for _, name := range []string{"code", "architecture", "plan"} {
		contribution.Assessment.Reviews = append(contribution.Assessment.Reviews, agent.ContributionFinding{Name: name, Status: "satisfied", Classification: "blocking", Evidence: []agent.EvidenceExplanation{{CheckIDs: []string{}, Summary: "Fixture retained source."}}})
	}
	for _, name := range []string{"capture", "snapshot", "check", "stage", "story"} {
		t.Run(name, func(t *testing.T) {
			raw, e := json.Marshal(contribution)
			mustProgress(t, e)
			var bad agent.Contribution
			mustProgress(t, json.Unmarshal(raw, &bad))
			switch name {
			case "capture":
				bad.Capture = c.Capture
			case "snapshot":
				bad.Assessment.SnapshotID = strings.Repeat("a", 64)
			case "check":
				bad.Assessment.Criteria[0].Evidence[0].CheckIDs = []string{"unretained"}
			case "stage":
				bad.Stage = "engineering"
			case "story":
				bad.Story = "foreign-story"
			}
			raw, e = json.Marshal(bad)
			mustProgress(t, e)
			d, e := assess.Preflight(t.Context(), raw)
			mustProgress(t, e)
			if d.Valid {
				t.Fatalf("accepted invalid %s: %+v", name, d)
			}
		})
	}
	changed := []byte("package changed_after_capture\n")
	mustProgress(t, os.WriteFile(filepath.Join(options.Worktree, "US-1.go"), changed, 0600))
	got := evidenceSubmitFixture(t, assess, ledger, "assessment", contribution)
	if got.States.Acceptance != "accepted" || got.States.Engineering == "recorded" || got.States.Effects == "delivered" {
		t.Fatalf("assessment states conflated: %+v", got)
	}
	for range 2 {
		replay := evidenceSubmitFixture(t, assess, ledger, "assessment", contribution)
		if !reflect.DeepEqual(got, replay) {
			t.Fatal("assessment replay changed outcome")
		}
	}
	current, err := os.ReadFile(filepath.Join(options.Worktree, "US-1.go"))
	mustProgress(t, err)
	if string(current) != string(changed) {
		t.Fatal("Validate altered worktree")
	}
	after, err := f.db.EngineeringEvidence(t.Context(), f.session.TaskRef, 1, "US-1")
	mustProgress(t, err)
	if !reflect.DeepEqual(retained, after) {
		t.Fatal("assessment altered engineering snapshot")
	}
	currentRun, err := f.db.GetRun(t.Context(), next.ID)
	mustProgress(t, err)
	if currentRun.Mode != "validate" || forge.children[200].State != "open" {
		t.Fatal("assessment chained mode or published closure")
	}
}

func TestEvidencePermittedInvalidationKeepsProofAndMode(t *testing.T) {
	f, _, session, options := submissionSessionFixture(t, store.WorkflowSeparated)
	service, receipts := evidenceFixtureService(t, f.db, session, "implement", store.WorkflowSeparated)
	c := capturedContribution(t, options.Worktree, "US-1", "engineering")
	evidenceSubmitFixture(t, service, receipts, "complete", c)
	before, err := f.db.EngineeringEvidence(t.Context(), f.session.TaskRef, 1, "US-1")
	mustProgress(t, err)
	invalidation := agent.Contribution{Stage: "engineering", Story: "US-1", Outcome: "invalidated", Reason: "Rework before assessment"}
	got := evidenceSubmitFixture(t, service, receipts, "invalidate", invalidation)
	if got.States.Input != "valid" || got.States.Acceptance == "accepted" {
		t.Fatalf("invalidation states: %+v", got)
	}
	stories, err := f.db.ListManagedStories(t.Context(), f.session.TaskRef, 1)
	mustProgress(t, err)
	if stories[0].EngineeringComplete {
		t.Fatal("invalidation retained current engineering completion")
	}
	again := evidenceSubmitFixture(t, service, receipts, "invalidate", invalidation)
	if !reflect.DeepEqual(got, again) {
		t.Fatal("invalidation replay changed outcome")
	}
	// Captured files remain available for observation even though completion was invalidated.
	snapshot, err := f.db.GetSubmissionSnapshot(t.Context(), options.RunID, c.Engineering.SnapshotID)
	mustProgress(t, err)
	if snapshot.Digest == "" || len(before.Payload) == 0 {
		t.Fatal("invalidation lost retained proof")
	}
	run, err := f.db.GetRun(t.Context(), options.RunID)
	mustProgress(t, err)
	if run.Mode != "implement" {
		t.Fatal("invalidation changed mode")
	}
}
