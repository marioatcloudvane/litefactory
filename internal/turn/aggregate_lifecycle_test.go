package turn

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/cloudvane/litefactory/internal/testutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// This file is T-012.1's own aggregate/full-lifecycle coverage. Every dimension
// exercised below already has a narrower single-purpose test elsewhere in this
// package (submissions_test.go, story_progress_test.go, plan_outcome_test.go);
// what is missing before this story, and what US-012's AC-001 actually asks
// for, is offline proof that all of those dimensions compose correctly in one
// continuous run: both persisted workflow versions, both synthetic runner
// adapters, every new contract stage as a genuinely distinct durable state,
// 256-criterion capacity, an in-run correction before the authoritative
// decision, and recovery after a simulated process restart. Nothing here calls
// a network, a real forge or a real agent CLI; the forge is the existing fake
// progressForge and the two "adapters" are stub Runners keyed by agent.Kind.

// correctedEngineeringContribution captures different worktree bytes than
// capturedContribution's fixed "package sample\n", so a test can prove the
// retained evidence after a same-run correction is the corrected content and
// not a replay of the original defective submission.
func correctedEngineeringContribution(t *testing.T, worktree, story string) agent.Contribution {
	t.Helper()
	content := []byte("package sample\n\n// corrected before the authoritative decision\n")
	path := story + ".go"
	if err := os.WriteFile(filepath.Join(worktree, path), content, 0600); err != nil {
		t.Fatalf("write corrected worktree content: %v", err)
	}
	files := []agent.StorySnapshotFile{{Path: path, Content: content, SHA256: agent.StoryDigest(content)}}
	digest := agent.SnapshotDigest(files)
	return agent.Contribution{
		Stage:   "engineering",
		Story:   story,
		Outcome: "engineering_complete",
		Engineering: &agent.EngineeringContribution{
			SnapshotID: "snapshot-corrected-" + story,
			CheckIDs:   []string{"check-corrected-" + story},
		},
		Capture: &agent.ContributionCapture{
			ImplementationDigest: digest,
			Files:                []agent.ContributionCaptureFile{{Path: path, Content: base64.StdEncoding.EncodeToString(content), SHA256: files[0].SHA256, Deleted: false}},
			Checks:               []agent.ContributionCaptureCheck{{ID: "check-corrected-" + story, Name: "corrected scoped test " + story, Passed: true, Evidence: "ok corrected fixture", ImplementationDigest: digest}},
		},
	}
}

// brokenPreflightPayload reintroduces the two independent defects the narrower
// per-story tests already use (a wrong-container evidence field and an
// agent-supplied envelope field), so preflight's diagnostic stage stays
// provably separate from and prior to any durable submission.
func brokenPreflightPayload(t *testing.T, c agent.Contribution) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(c)
	mustProgress(t, err)
	broken := strings.Replace(string(data), `"evidence":"ok scoped fixture"`, `"evidence":["wrong container"]`, 1)
	broken = strings.TrimSuffix(broken, "}") + `,"parent":5399889036}`
	return json.RawMessage(broken)
}

func TestAggregateLifecycle_BothWorkflowVersionsAllStagesCorrectionAndRestartRecovery(t *testing.T) {
	for _, tt := range []struct {
		name             string
		workflow         int
		firstStage       string
		wantAcceptance   string // acceptance state immediately after the first authoritative engineering submit
		terminalOnSubmit bool   // legacy: engineering submit is itself the authoritative decision
	}{
		{name: "legacy_immediate_verification", workflow: store.WorkflowLegacy, firstStage: "legacy", wantAcceptance: "accepted", terminalOnSubmit: true},
		{name: "separated_engineering_then_acceptance", workflow: store.WorkflowSeparated, firstStage: "engineering", wantAcceptance: "not_applicable", terminalOnSubmit: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f, forge, session, o := submissionSessionFixture(t, tt.workflow)
			ctx := t.Context()

			// --- Stage 1: validation is a distinct, non-mutating stage. ---
			c := capturedContribution(t, o.Worktree, "US-1", tt.firstStage)
			preflight, err := submission.Call(ctx, session.DescriptorPath(), "preflight", brokenPreflightPayload(t, c))
			mustProgress(t, err)
			var diagnostics agent.ContributionDiagnostics
			mustProgress(t, json.Unmarshal(preflight.Result, &diagnostics))
			if diagnostics.Valid || len(diagnostics.Diagnostics) < 2 {
				t.Fatalf("validation stage did not report independent defects: %+v", diagnostics)
			}
			if _, err := f.db.GetStoryProgressEvent(ctx, o.RunID, 1); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("preflight (validation stage) mutated durable state: %v", err)
			}

			// --- Stage 2: same-run correction, submitting the valid contribution
			// the agent should have sent the first time. ---
			first := callContribution(t, session, "submit", c)
			var receipt store.SubmissionReceipt
			mustProgress(t, json.Unmarshal(first.Result, &receipt))
			if receipt.EngineeringState != "recorded" || receipt.AcceptanceState != tt.wantAcceptance {
				t.Fatalf("engineering stage produced wrong distinct state: %+v (want acceptance=%s)", receipt, tt.wantAcceptance)
			}

			// --- Stage 3: remote effects are a distinct, initially pending state
			// regardless of workflow version. ---
			if receipt.EffectState == "delivered" {
				t.Fatalf("remote effects were not left as a distinct pending stage: %+v", receipt)
			}
			if forge.children[200].State != "open" {
				t.Fatal("remote effect fired before its own distinct stage ran")
			}

			// --- Stage 4: restart recovery. Simulate a process crash between
			// durable allocation and decision for the fixture's second story,
			// then recover from a completely fresh store connection/service the
			// way a restarted daemon would. This has to happen here, while the
			// run is still "running" and this attempt is still current: ending
			// the run (as the later acceptance/effects stages below do for
			// "US-1") would make a fresh allocation against it correctly refuse
			// as stale, which is a different, already-covered behavior.
			crashStage := "engineering"
			if tt.workflow == store.WorkflowLegacy {
				crashStage = "legacy"
			}
			crashed := capturedContribution(t, o.Worktree, "US-2", crashStage)
			crashedPayload, err := json.Marshal(crashed)
			mustProgress(t, err)
			pending, err := f.db.AllocateSubmission(ctx, store.SubmissionRequest{RunID: o.RunID, Attempt: session.binding.Attempt, RequestID: "restart-crash-" + tt.name, Story: "US-2", Revision: o.Bundle.Stories[1].Revision, InputState: "valid", Payload: crashedPayload, Diagnostics: []byte(`{"valid":true}`)})
			mustProgress(t, err)
			recoveryDB, err := store.Open(ctx, f.db.Path())
			mustProgress(t, err)
			t.Cleanup(func() { mustProgress(t, recoveryDB.Close()) })
			mustProgress(t, recoveryDB.Migrate(ctx))
			renderer, err := report.New(report.Options{Redactor: redact.New(redact.Options{}), Limit: github.CommentSizeLimit})
			mustProgress(t, err)
			// A fresh service against a fresh connection, exactly as a
			// restarted process would construct: nothing here is shared with
			// the still-running session/broker above.
			recoveredProgress, err := NewStoryProgress(recoveryDB, forge, renderer, 9, filepath.Join(filepath.Dir(f.db.Path()), "restart-locks-"+tt.name))
			mustProgress(t, err)
			mustProgress(t, recoveredProgress.RecoverSubmissions(ctx))

			wantRecoveredAcceptance := "not_applicable"
			if tt.workflow == store.WorkflowLegacy {
				wantRecoveredAcceptance = "accepted"
			}
			recovered, err := recoveryDB.GetSubmissionReceipt(ctx, o.RunID, pending.RequestID)
			mustProgress(t, err)
			if recovered.ID != pending.ID || recovered.EngineeringState != "recorded" || recovered.AcceptanceState != wantRecoveredAcceptance {
				t.Fatalf("restart recovery produced the wrong durable decision: %+v", recovered)
			}
			// Recovering again from the same fresh connection must be
			// idempotent: no duplicated event, no duplicated remote effect.
			mustProgress(t, recoveredProgress.RecoverSubmissions(ctx))
			if _, err := recoveryDB.GetStoryProgressEvent(ctx, o.RunID, recovered.Sequence+1); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("repeated restart recovery duplicated a story event: %v", err)
			}

			if !tt.terminalOnSubmit {
				// Separated workflow: engineering recorded is not yet the
				// authoritative decision (acceptance is still pending), so a
				// same-run correction remains legitimate here too. Invalidate
				// the recorded engineering, then submit genuinely different
				// corrected content before acceptance ever runs.
				invalidate := agent.Contribution{Stage: "engineering", Story: "US-1", Outcome: "invalidated", Reason: "Correcting before the acceptance decision"}
				invalidated := callContribution(t, session, "submit", invalidate)
				var invalidatedReceipt store.SubmissionReceipt
				mustProgress(t, json.Unmarshal(invalidated.Result, &invalidatedReceipt))
				if invalidatedReceipt.EngineeringState != "rejected" {
					t.Fatalf("invalidation did not retire the prior engineering record: %+v", invalidatedReceipt)
				}
				stories, err := f.db.ListManagedStories(ctx, f.session.TaskRef, 1)
				mustProgress(t, err)
				for _, s := range stories {
					if s.Key == "US-1" && s.EngineeringComplete {
						t.Fatal("invalidated story still reported engineering complete")
					}
				}

				corrected := correctedEngineeringContribution(t, o.Worktree, "US-1")
				correctedResponse := callContribution(t, session, "submit", corrected)
				var correctedReceipt store.SubmissionReceipt
				mustProgress(t, json.Unmarshal(correctedResponse.Result, &correctedReceipt))
				if correctedReceipt.EngineeringState != "recorded" || correctedReceipt.AcceptanceState != "not_applicable" {
					t.Fatalf("corrected engineering was not durably recorded: %+v", correctedReceipt)
				}

				retained, err := f.db.EngineeringEvidence(ctx, f.session.TaskRef, 1, "US-1")
				mustProgress(t, err)
				event, err := agent.ParseStoryEvent(retained.Payload)
				mustProgress(t, err)
				if event.Evidence == nil || len(event.Evidence.Files) != 1 || string(event.Evidence.Files[0].Content) != "package sample\n\n// corrected before the authoritative decision\n" {
					t.Fatalf("retained evidence is the original defective submission, not the correction: %+v", event.Evidence)
				}

				// --- Stage 4: acceptance is a distinct stage from engineering,
				// consuming only the retained corrected evidence. ---
				mustProgress(t, session.Close(ctx))
				exitCode := int64(0)
				_, err = f.db.FinishRun(ctx, o.RunID, store.RunOutcome{Status: store.RunSucceeded, ExitCode: &exitCode})
				mustProgress(t, err)
				run, err := f.db.GetRun(ctx, o.RunID)
				mustProgress(t, err)
				next, err := f.db.AllocateRun(ctx, store.AllocateRunParams{SessionID: f.session.ID, SegmentID: run.SegmentID, Mode: "validate", Agent: store.AgentClaude, TriggerCommentID: 2, LogPath: func(int) string { return "validate.log" }})
				mustProgress(t, err)
				validation := &agent.ValidationContext{Version: 1, Parent: f.session.TaskRef, Generation: 1, Stories: []agent.ValidationStory{{Story: o.Bundle.Stories[0], Criteria: []string{"AC-001"}, Snapshot: event.Evidence.ImplementationDigest, Evidence: event.Evidence, EvidenceStatus: "satisfied"}}}
				validate, err := StartSubmissionSession(ctx, SubmissionSessionOptions{DB: f.db, Progress: f.opts.StoryProgress, Paths: f.opts.Paths, RunID: next.ID, Worktree: o.Worktree, Validation: validation})
				mustProgress(t, err)
				t.Cleanup(func() {
					cleanup, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), testutil.Wait)
					defer cancel()
					mustProgress(t, validate.Close(cleanup))
				})

				assessment := agent.Contribution{Stage: "validate", Story: "US-1", Outcome: "assessed", Assessment: &agent.AssessmentContribution{SnapshotID: event.Evidence.ImplementationDigest, Criteria: []agent.ContributionFinding{{Name: "AC-001", Status: "satisfied", Classification: "blocking", Evidence: []agent.EvidenceExplanation{{CheckIDs: []string{event.Evidence.Tests[0].Name}, Summary: "Retained corrected execution covers this obligation."}}}}, Reviews: []agent.ContributionFinding{}}}
				for _, name := range []string{"code", "architecture", "plan"} {
					assessment.Assessment.Reviews = append(assessment.Assessment.Reviews, agent.ContributionFinding{Name: name, Status: "satisfied", Classification: "blocking", Evidence: []agent.EvidenceExplanation{{CheckIDs: []string{}, Summary: "Reviewed retained corrected source."}}})
				}
				acceptanceResponse := callContribution(t, validate, "submit", assessment)
				var acceptanceReceipt store.SubmissionReceipt
				mustProgress(t, json.Unmarshal(acceptanceResponse.Result, &acceptanceReceipt))
				if acceptanceReceipt.AcceptanceState != "accepted" || acceptanceReceipt.EngineeringState == "recorded" {
					t.Fatalf("acceptance stage was not distinct from engineering: %+v", acceptanceReceipt)
				}
				if forge.children[200].State != "open" {
					t.Fatal("acceptance receipt alone prematurely delivered a remote effect")
				}
			} else {
				// Legacy workflow: engineering and acceptance land together, but
				// remote effects still lag behind as their own reconciled stage.
				mustProgress(t, f.opts.StoryProgress.Poll(ctx))
				mustProgress(t, f.db.ReconcileSubmissionEffects(ctx))
				delivered, err := f.db.GetSubmissionReceipt(ctx, o.RunID, first.RequestID)
				mustProgress(t, err)
				if delivered.EffectState != "delivered" || forge.children[200].State != "closed" {
					t.Fatalf("remote effects stage did not reconcile: %+v child=%+v", delivered, forge.children[200])
				}
				changes := len(forge.changes)
				mustProgress(t, f.opts.StoryProgress.Poll(ctx))
				mustProgress(t, f.db.ReconcileSubmissionEffects(ctx))
				if len(forge.changes) != changes {
					t.Fatal("reconciling remote effects twice duplicated the forge close")
				}
			}
		})
	}
}

// TestAggregateLifecycle_BothSyntheticAdapterPathsPerformSameRunCorrection
// extends the existing single-submit "both adapters" coverage
// (TestSubmissionBothSyntheticRunnerPathsObserveDecisionBeforeExit) with an
// in-run correction sequence, through the real Execute() runner dispatch, for
// both the claude and the codex stub adapter. Both fake adapters call the
// identical neutral submission capability; only the RunnerSet key differs.
func TestAggregateLifecycle_BothSyntheticAdapterPathsPerformSameRunCorrection(t *testing.T) {
	for _, kind := range []agent.Kind{agent.Kind(store.AgentClaude), agent.Kind(store.AgentCodex)} {
		t.Run(string(kind), func(t *testing.T) {
			ready := make(chan store.SubmissionReceipt, 1)
			release := make(chan struct{})
			var f *fixture

			runner := newStubRunner(func(ctx context.Context, req agent.Request) (*agent.RunResult, error) {
				prefix := "Factory submission access (paths are data): "
				var access struct{ Executable, Descriptor string }
				for _, line := range strings.Split(req.Prompt, "\n") {
					if strings.HasPrefix(line, prefix) {
						if err := json.Unmarshal([]byte(strings.TrimPrefix(line, prefix)), &access); err != nil {
							return nil, err
						}
					}
				}
				if access.Descriptor == "" || !filepath.IsAbs(access.Executable) {
					return nil, fmt.Errorf("factory helper missing")
				}

				// Submit a defective contribution first, exactly as a real agent
				// might, then correct it in the same run before the exit any
				// clean subprocess produces could be mistaken for acceptance.
				bad := capturedContribution(t, req.WorktreePath, "US-1", "engineering")
				badData, err := json.Marshal(bad)
				if err != nil {
					return nil, err
				}
				broken := strings.TrimSuffix(string(badData), "}") + `,"parent":5399889036}`
				broken = strings.Replace(broken, `"evidence":"ok scoped fixture"`, `"evidence":["wrong container"]`, 1)
				preflight, err := submission.Call(ctx, access.Descriptor, "preflight", json.RawMessage(broken))
				if err != nil {
					return nil, err
				}
				var diagnostics agent.ContributionDiagnostics
				if err := json.Unmarshal(preflight.Result, &diagnostics); err != nil {
					return nil, err
				}
				if diagnostics.Valid {
					return nil, fmt.Errorf("held runner did not receive correction diagnostics")
				}

				corrected := correctedEngineeringContribution(t, req.WorktreePath, "US-1")
				correctedData, err := json.Marshal(corrected)
				if err != nil {
					return nil, err
				}
				response, err := submission.Call(ctx, access.Descriptor, "submit", correctedData)
				if err != nil {
					return nil, err
				}
				if response.Error != "" {
					return nil, fmt.Errorf("submit: %s", response.Error)
				}
				var receipt store.SubmissionReceipt
				if err := json.Unmarshal(response.Result, &receipt); err != nil {
					return nil, err
				}
				ready <- receipt
				select {
				case <-release:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
				return succeeded(req, f.paths.Home()), nil
			})
			f = newFixture(t, runner)
			forge := typedProgressScope(t, f, store.WorkflowSeparated)
			f.opts.Runners = RunnerSet{kind: runner}
			req := f.request()
			req.Mode = agent.ModeImplement
			req.Agent = kind
			req.Command = ParseCommand("/implement work")
			req.Trigger.Body = "/implement work"
			done := make(chan error, 1)
			ended := false
			t.Cleanup(func() {
				close(release)
				if !ended {
					select {
					case <-done:
					case <-time.After(20 * time.Second):
						t.Error("runner cleanup did not join")
					}
				}
			})
			go func() { _, err := Execute(t.Context(), f.opts, req); done <- err }()
			select {
			case receipt := <-ready:
				if receipt.EngineeringState != "recorded" || receipt.AcceptanceState == "accepted" {
					t.Fatalf("corrected submission lacked a distinct durable decision: %+v", receipt)
				}
				run, err := f.db.GetRun(t.Context(), receipt.RunID)
				mustProgress(t, err)
				if run.Status != store.RunRunning || forge.children[200].State != "open" {
					t.Fatal("receipt arrived only after exit or premature closure")
				}
				retained, err := f.db.EngineeringEvidence(t.Context(), f.session.TaskRef, 1, "US-1")
				mustProgress(t, err)
				event, err := agent.ParseStoryEvent(retained.Payload)
				mustProgress(t, err)
				if event.Evidence == nil || len(event.Evidence.Files) != 1 || string(event.Evidence.Files[0].Content) != "package sample\n\n// corrected before the authoritative decision\n" {
					t.Fatalf("live runner correction did not retain the corrected evidence: %+v", event.Evidence)
				}
			case err := <-done:
				ended = true
				t.Fatalf("runner failed before decision: %v", err)
			case <-time.After(20 * time.Second):
				t.Fatal("runner never observed durable decision")
			}
		})
	}
}

// TestAggregateLifecycle_256CriterionAssessmentThroughBroker exercises the
// 256-criterion capacity bound end to end through the real store/broker path,
// not only the pure agent-package schema check
// (TestContributionCapacity256CriteriaWithSharedProof), so the aggregate gate
// also proves durable persistence, correlation and acceptance succeed at that
// bound rather than only that the JSON shape validates in isolation.
func TestAggregateLifecycle_256CriterionAssessmentThroughBroker(t *testing.T) {
	f := newFixture(t, nil)
	ctx := t.Context()
	// A short, resolved, private temp home: submission.Start refuses long or
	// symlinked directories, and t.TempDir() on macOS can resolve through a
	// symlinked /var, so this mirrors typedProgressScope's own fixture home.
	home, err := os.MkdirTemp("/tmp", "lf207-turn-")
	mustProgress(t, err)
	home, err = filepath.EvalSymlinks(home)
	mustProgress(t, err)
	t.Cleanup(func() {
		if rmErr := os.RemoveAll(home); rmErr != nil {
			t.Error(rmErr)
		}
	})
	f.opts.Paths = config.NewPaths(home)

	mustProgress(t, f.db.PutManagedFeature(ctx, store.ManagedFeature{WorkflowVersion: store.WorkflowSeparated, ParentTask: f.session.TaskRef, Generation: 1, State: "planning"}))
	// The broker only ever starts under submission version 2, and the store
	// requires content and submission versions to match, so this exercises the
	// typed (content version 2) 256-criterion story, one task referencing every
	// criterion, through the real projection/hash/store round trip.
	mustProgress(t, f.db.BindFeatureContract(ctx, store.FeatureContract{Parent: f.session.TaskRef, Generation: 1, ContentVersion: 2, SubmissionVersion: 2}))

	criteria := make([]planbrief.CriterionContent, 0, 256)
	criterionIDs := make([]string, 0, 256)
	for i := 1; i <= 256; i++ {
		id := fmt.Sprintf("AC-%03d", i)
		criterionIDs = append(criterionIDs, id)
		criteria = append(criteria, planbrief.CriterionContent{ID: id, Text: fmt.Sprintf("The scoped result satisfies obligation %d.", i)})
	}
	content := planbrief.TypedStory{
		Key: "US-1", Intent: "create", Title: "Story US-1",
		UserStory:          planbrief.UserStoryContent{Actor: "operator", Want: "correct results", Benefit: "reliable work", Notes: []string{}},
		AcceptanceCriteria: criteria,
		Tasks:              []planbrief.TaskContent{{ID: "T-001", Description: "Implement every scoped obligation", OwnerRole: "engineer", CriterionIDs: criterionIDs, DependsOn: []string{}}},
		Architecture:       planbrief.ArchitectureContent{Summary: "Keep ownership", Components: []planbrief.ComponentContent{}, Interfaces: []planbrief.InterfaceContent{}, Constraints: []string{}, Decisions: []planbrief.DecisionContent{}},
		Dependencies:       planbrief.DependencyContent{StoryKeys: []string{}, IssueRefs: []string{}},
	}
	body, err := report.RenderTypedStory(content, planbrief.ContentScope{Roles: []string{"engineer"}})
	mustProgress(t, err)
	hash := planbrief.StoryHash(content.Title, body)
	managed := store.ManagedStory{ParentTask: f.session.TaskRef, Generation: 1, Key: "US-1", IssueNumber: 200, IssueID: 2000, BodyHash: hash, Dependencies: content.Dependencies.StoryKeys, State: "planning"}
	mustProgress(t, f.db.PutManagedStory(ctx, managed))
	encodedContent, err := json.Marshal(content)
	mustProgress(t, err)
	mustProgress(t, f.db.PutSubmissionProjection(ctx, store.SubmissionProjection{Parent: managed.ParentTask, Generation: 1, Story: "US-1", IssueID: managed.IssueID, Revision: hash, Title: content.Title, Body: body, ContentVersion: 2, Content: encodedContent, Criteria: criterionIDs}))
	revision := store.ApprovedStoryRevision{ManagedStory: managed, ParentID: 1230, Title: content.Title, Body: body, Hash: hash, FetchedAt: time.Now().UTC()}
	forge := &progressForge{children: map[int]github.Issue{200: {ID: managed.IssueID, Number: 200, Title: content.Title, Body: body, State: "open"}}, comments: map[int][]github.Comment{}}
	mustProgress(t, f.db.SetManagedFeatureReady(ctx, f.session.TaskRef, 1, true))
	mustProgress(t, f.db.ApproveManagedStories(ctx, f.session.TaskRef, 1, []store.ApprovedStoryRevision{revision}))
	mustProgress(t, f.db.DeliverStoryScopeNotice(ctx, revision.ParentTask, 1, revision.Key, "freeze"))
	mustProgress(t, f.db.ActivateManagedScope(ctx, f.session.TaskRef, 1))
	renderer, err := report.New(report.Options{Redactor: redact.New(redact.Options{}), Limit: github.CommentSizeLimit})
	mustProgress(t, err)
	progress, err := NewStoryProgress(f.db, forge, renderer, 9, filepath.Join(filepath.Dir(f.db.Path()), "submission-locks-256"))
	mustProgress(t, err)
	f.opts.StoryProgress = progress
	f.opts.SubIssueCreator = forge

	seg, err := f.db.GetOrCreateSegment(ctx, f.session.ID, 1, "submission", store.AgentClaude)
	mustProgress(t, err)
	run, err := f.db.AllocateRun(ctx, store.AllocateRunParams{SessionID: f.session.ID, SegmentID: seg.ID, Mode: "implement", Agent: store.AgentClaude, TriggerCommentID: 1, LogPath: func(int) string { return "run.log" }})
	mustProgress(t, err)
	bundle := &agent.ApprovedBundle{WorkflowVersion: store.WorkflowSeparated, Parent: f.session.TaskRef, Generation: 1, RunID: run.ID, Stories: []agent.ApprovedStory{{Key: "US-1", IssueID: managed.IssueID, IssueNumber: managed.IssueNumber, ParentID: revision.ParentID, Revision: hash, Title: content.Title, Body: body, State: managed.State}}}
	worktree := t.TempDir()
	mustProgress(t, f.opts.StoryProgress.Register(ctx, bundle, worktree))
	engineering, err := StartSubmissionSession(ctx, SubmissionSessionOptions{DB: f.db, Progress: f.opts.StoryProgress, Paths: f.opts.Paths, RunID: run.ID, Worktree: worktree, Bundle: bundle})
	mustProgress(t, err)
	if engineering == nil {
		t.Fatal("typed submission version did not start a broker for content version 1")
	}

	engineeringResponse := callContribution(t, engineering, "submit", capturedContribution(t, worktree, "US-1", "engineering"))
	var engineeringReceipt store.SubmissionReceipt
	mustProgress(t, json.Unmarshal(engineeringResponse.Result, &engineeringReceipt))
	if engineeringReceipt.EngineeringState != "recorded" {
		t.Fatalf("engineering with a 256-criterion story failed: %+v", engineeringReceipt)
	}
	retained, err := f.db.EngineeringEvidence(ctx, f.session.TaskRef, 1, "US-1")
	mustProgress(t, err)
	event, err := agent.ParseStoryEvent(retained.Payload)
	mustProgress(t, err)
	mustProgress(t, engineering.Close(ctx))

	exitCode := int64(0)
	_, err = f.db.FinishRun(ctx, run.ID, store.RunOutcome{Status: store.RunSucceeded, ExitCode: &exitCode})
	mustProgress(t, err)
	runRow, err := f.db.GetRun(ctx, run.ID)
	mustProgress(t, err)
	next, err := f.db.AllocateRun(ctx, store.AllocateRunParams{SessionID: f.session.ID, SegmentID: runRow.SegmentID, Mode: "validate", Agent: store.AgentClaude, TriggerCommentID: 2, LogPath: func(int) string { return "validate.log" }})
	mustProgress(t, err)
	validation := &agent.ValidationContext{Version: 1, Parent: f.session.TaskRef, Generation: 1, Stories: []agent.ValidationStory{{Story: bundle.Stories[0], Criteria: criterionIDs, Snapshot: event.Evidence.ImplementationDigest, Evidence: event.Evidence, EvidenceStatus: "satisfied"}}}
	session, err := StartSubmissionSession(ctx, SubmissionSessionOptions{DB: f.db, Progress: f.opts.StoryProgress, Paths: f.opts.Paths, RunID: next.ID, Worktree: worktree, Validation: validation})
	mustProgress(t, err)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), testutil.Wait)
		defer cancel()
		mustProgress(t, session.Close(cleanup))
	})

	findings := make([]agent.ContributionFinding, 0, len(criterionIDs))
	for _, id := range criterionIDs {
		findings = append(findings, agent.ContributionFinding{Name: id, Status: "satisfied", Classification: "blocking", Evidence: []agent.EvidenceExplanation{{CheckIDs: []string{event.Evidence.Tests[0].Name}, Summary: "Retained scoped execution covers obligation " + id + "."}}})
	}
	assessment := agent.Contribution{Stage: "validate", Story: "US-1", Outcome: "assessed", Assessment: &agent.AssessmentContribution{SnapshotID: event.Evidence.ImplementationDigest, Criteria: findings, Reviews: []agent.ContributionFinding{}}}
	for _, name := range []string{"code", "architecture", "plan"} {
		assessment.Assessment.Reviews = append(assessment.Assessment.Reviews, agent.ContributionFinding{Name: name, Status: "satisfied", Classification: "blocking", Evidence: []agent.EvidenceExplanation{{CheckIDs: []string{}, Summary: "Reviewed retained source."}}})
	}
	data, err := json.Marshal(assessment)
	mustProgress(t, err)
	if len(data) > agent.MaxContributionBytes {
		t.Fatalf("256-criterion assessment exceeds the shared metadata budget: %d bytes", len(data))
	}

	response := callContribution(t, session, "submit", assessment)
	var receipt store.SubmissionReceipt
	mustProgress(t, json.Unmarshal(response.Result, &receipt))
	if receipt.AcceptanceState != "accepted" {
		t.Fatalf("256-criterion assessment was not durably accepted: %+v", receipt)
	}
}

// TestAggregateLifecycle_LegacyContentVersionSkipsTypedBroker checks the
// backward-compatibility half of AC-001: an unfinished/legacy scope still
// bound to submission version 1 must never spin up the new typed broker at
// all, so old dispatch is completely unaffected by every new stage introduced
// above. This is the aggregate boundary; the corresponding old-contract Plan
// lifecycle itself is already covered by plan_outcome_test.go's legacy fixtures.
func TestAggregateLifecycle_LegacyContentVersionSkipsTypedBroker(t *testing.T) {
	f := newFixture(t, nil)
	ctx := t.Context()
	mustProgress(t, f.db.PutManagedFeature(ctx, store.ManagedFeature{WorkflowVersion: store.WorkflowLegacy, ParentTask: f.session.TaskRef, Generation: 1, State: "planning"}))
	mustProgress(t, f.db.BindFeatureContract(ctx, store.FeatureContract{Parent: f.session.TaskRef, Generation: 1, ContentVersion: 1, SubmissionVersion: 1}))

	seg, err := f.db.GetOrCreateSegment(ctx, f.session.ID, 1, "submission", store.AgentClaude)
	mustProgress(t, err)
	run, err := f.db.AllocateRun(ctx, store.AllocateRunParams{SessionID: f.session.ID, SegmentID: seg.ID, Mode: "implement", Agent: store.AgentClaude, TriggerCommentID: 1, LogPath: func(int) string { return "run.log" }})
	mustProgress(t, err)
	bundle := &agent.ApprovedBundle{WorkflowVersion: store.WorkflowLegacy, Parent: f.session.TaskRef, Generation: 1, RunID: run.ID, Stories: []agent.ApprovedStory{{Key: "US-1", State: "planning"}}}

	session, err := StartSubmissionSession(ctx, SubmissionSessionOptions{DB: f.db, Progress: nil, Paths: f.opts.Paths, RunID: run.ID, Worktree: t.TempDir(), Bundle: bundle})
	mustProgress(t, err)
	if session != nil {
		t.Fatal("submission version 1 (legacy contract) unexpectedly started the new typed broker")
	}
}
