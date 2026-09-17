package turn

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

func TestValidationCheckpointReconcilesTerminal(t *testing.T) {
	for _, scenario := range []string{"duplicate", "lost_reply", "conflict", "missing", "concurrent", "broker", "wrong_snapshot"} {
		t.Run(scenario, func(t *testing.T) {
			f, _, engineering, o := submissionSessionFixture(t, store.WorkflowSeparated)
			callContribution(t, engineering, "submit", capturedContribution(t, o.Worktree, "US-1", "engineering"))
			retained, err := f.db.EngineeringEvidence(t.Context(), f.session.TaskRef, 1, "US-1")
			mustProgress(t, err)
			event, err := agent.ParseStoryEvent(retained.Payload)
			mustProgress(t, err)
			mustProgress(t, engineering.Close(t.Context()))
			exitCode := int64(0)
			_, err = f.db.FinishRun(t.Context(), o.RunID, store.RunOutcome{Status: store.RunSucceeded, ExitCode: &exitCode})
			mustProgress(t, err)
			run, err := f.db.GetRun(t.Context(), o.RunID)
			mustProgress(t, err)
			next, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: f.session.ID, SegmentID: run.SegmentID, Mode: "validate", Agent: store.AgentClaude, TriggerCommentID: 2, LogPath: func(int) string { return "validate.log" }})
			mustProgress(t, err)
			validation := &agent.ValidationContext{Version: 1, Parent: f.session.TaskRef, Generation: 1, Stories: []agent.ValidationStory{{Story: o.Bundle.Stories[0], Criteria: []string{"AC-001"}, Snapshot: event.Evidence.ImplementationDigest, Evidence: event.Evidence, EvidenceStatus: "satisfied"}}}
			session, err := StartSubmissionSession(t.Context(), SubmissionSessionOptions{DB: f.db, Progress: f.opts.StoryProgress, Paths: f.opts.Paths, RunID: next.ID, Worktree: o.Worktree, Validation: validation})
			mustProgress(t, err)
			defer session.Close(t.Context())
			finding := agent.ContributionFinding{Name: "AC-001", Status: "unverified", Classification: "blocking", Evidence: []agent.EvidenceExplanation{{CheckIDs: []string{}, Summary: "Execution alone does not establish this requirement."}}}
			c := agent.Contribution{Stage: "validate", Story: "US-1", Outcome: "assessed", Assessment: &agent.AssessmentContribution{SnapshotID: event.Evidence.ImplementationDigest, Criteria: []agent.ContributionFinding{finding}, Reviews: []agent.ContributionFinding{}}}
			for _, name := range []string{"code", "architecture", "plan"} {
				f := finding
				f.Name = name
				c.Assessment.Reviews = append(c.Assessment.Reviews, f)
			}
			payload, err := json.Marshal(c)
			mustProgress(t, err)

			var live store.SubmissionReceipt
			switch scenario {
			case "lost_reply":
				live, err = f.db.AllocateSubmission(t.Context(), store.SubmissionRequest{RunID: next.ID, Attempt: session.binding.Attempt, RequestID: "live-checkpoint", Story: c.Story, Revision: o.Bundle.Stories[0].Revision, InputState: "valid", Payload: payload, Diagnostics: []byte(`{"valid":true}`)})
				mustProgress(t, err)
			case "broker":
				response, err := submission.Call(t.Context(), session.DescriptorPath(), "submit", payload)
				mustProgress(t, err)
				if response.Error != "" {
					t.Fatal(response.Error)
				}
				mustProgress(t, json.Unmarshal(response.Result, &live))
			default:
				result, err := session.checkpoint(t.Context(), submission.Request{Method: "submit", RequestID: "live-checkpoint", Payload: payload})
				mustProgress(t, err)
				mustProgress(t, json.Unmarshal(result, &live))
			}
			// Formatting differences do not create a second canonical contribution.
			terminal, err := json.MarshalIndent(c, "", "  ")
			mustProgress(t, err)
			if scenario == "conflict" {
				c.Assessment.Criteria[0].Evidence[0].Summary = "Different assessment"
				terminal, err = json.Marshal(c)
				mustProgress(t, err)
			}
			if scenario == "wrong_snapshot" {
				c.Assessment.SnapshotID = "foreign-snapshot"
				terminal, err = json.Marshal(c)
				mustProgress(t, err)
			}
			output := `{"contributions":[` + string(terminal) + `]}`
			if scenario == "missing" {
				output = `{"contributions":[]}`
			}
			if scenario == "concurrent" {
				wait, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				start := make(chan struct{})
				done := make(chan error, 1)
				go func() {
					<-start
					_, err := session.checkpoint(t.Context(), submission.Request{Method: "submit", RequestID: "live-checkpoint", Payload: payload})
					done <- err
				}()
				close(start)
				mustProgress(t, session.consumeTerminalValidation(t.Context(), output))
				select {
				case err := <-done:
					mustProgress(t, err)
				case <-wait.Done():
					t.Fatal("concurrent checkpoint did not finish")
				}
			} else {
				err = session.consumeTerminalValidation(t.Context(), output)
				if scenario == "conflict" || scenario == "missing" || scenario == "wrong_snapshot" {
					if err == nil {
						t.Fatal("inconsistent final output accepted")
					}
					if scenario == "conflict" && !strings.Contains(err.Error(), "conflict") {
						t.Fatal(err)
					}
				} else {
					mustProgress(t, err)
					mustProgress(t, session.consumeTerminalValidation(t.Context(), output))
				}
			}
			// Recovery and repeated final output leave exactly the original correlation.
			mustProgress(t, f.opts.StoryProgress.RecoverSubmissions(context.WithoutCancel(t.Context())))
			if scenario == "duplicate" {
				if _, err := f.db.ValidationCheckpoints(t.Context(), next.ID, "foreign-attempt", c.Story, o.Bundle.Stories[0].Revision); err == nil {
					t.Fatal("foreign attempt could read checkpoint decisions")
				}
				if _, err := f.db.ValidationCheckpoints(t.Context(), next.ID, session.binding.Attempt, c.Story, "foreign-revision"); err == nil {
					t.Fatal("foreign revision could read checkpoint decisions")
				}
			}
			receipts, err := f.db.ValidationCheckpoints(t.Context(), next.ID, session.binding.Attempt, c.Story, o.Bundle.Stories[0].Revision)
			mustProgress(t, err)
			if len(receipts) != 1 || receipts[0].ID != live.ID || receipts[0].RequestID != live.RequestID || receipts[0].AcceptanceState != "unverified" || receipts[0].EngineeringState != "not_applicable" || receipts[0].InputState != "valid" {
				t.Fatalf("checkpoint correlation/decision changed: %+v", receipts)
			}
			_, err = f.db.GetSubmissionReceipt(t.Context(), next.ID, "terminal-"+session.binding.Attempt+"-0")
			if !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("terminal allocated duplicate receipt: %v", err)
			}
		})
	}
}
