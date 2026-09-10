package turn

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
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

func typedProgressScope(t *testing.T, f *fixture, workflow int) *progressForge {
	t.Helper()
	ctx := t.Context()
	home, err := os.MkdirTemp("/tmp", "lf207-turn-")
	mustProgress(t, err)
	home, err = filepath.EvalSymlinks(home)
	mustProgress(t, err)
	t.Cleanup(func() {
		if err := os.RemoveAll(home); err != nil {
			t.Error(err)
		}
	})
	f.opts.Paths = config.NewPaths(home)
	mustProgress(t, f.db.PutManagedFeature(ctx, store.ManagedFeature{WorkflowVersion: workflow, ParentTask: f.session.TaskRef, Generation: 1, State: "planning"}))
	mustProgress(t, f.db.BindFeatureContract(ctx, store.FeatureContract{Parent: f.session.TaskRef, Generation: 1, ContentVersion: 2, SubmissionVersion: 2}))
	forge := &progressForge{children: map[int]github.Issue{}, comments: map[int][]github.Comment{}}
	revisions := []store.ApprovedStoryRevision{}
	for i, key := range []string{"US-1", "US-2"} {
		content := planbrief.TypedStory{Key: key, Intent: "create", Title: "Story " + key, UserStory: planbrief.UserStoryContent{Actor: "operator", Want: "correct results", Benefit: "reliable work", Notes: []string{}}, AcceptanceCriteria: []planbrief.CriterionContent{{ID: "AC-001", Text: "The scoped result passes."}}, Tasks: []planbrief.TaskContent{{ID: "T-001", Description: "Implement result", OwnerRole: "engineer", CriterionIDs: []string{"AC-001"}, DependsOn: []string{}}}, Architecture: planbrief.ArchitectureContent{Summary: "Keep ownership", Components: []planbrief.ComponentContent{}, Interfaces: []planbrief.InterfaceContent{}, Constraints: []string{}, Decisions: []planbrief.DecisionContent{}}, Dependencies: planbrief.DependencyContent{StoryKeys: []string{}, IssueRefs: []string{}}}
		if i == 1 {
			content.Dependencies.StoryKeys = []string{"US-1"}
		}
		body, err := report.RenderTypedStory(content, planbrief.ContentScope{Roles: []string{"engineer"}})
		mustProgress(t, err)
		hash := planbrief.StoryHash(content.Title, body)
		number := 200 + i
		s := store.ManagedStory{ParentTask: f.session.TaskRef, Generation: 1, Key: key, IssueNumber: number, IssueID: int64(number * 10), BodyHash: hash, Dependencies: content.Dependencies.StoryKeys, State: "planning"}
		mustProgress(t, f.db.PutManagedStory(ctx, s))
		data, err := json.Marshal(content)
		mustProgress(t, err)
		mustProgress(t, f.db.PutSubmissionProjection(ctx, store.SubmissionProjection{Parent: s.ParentTask, Generation: 1, Story: key, IssueID: s.IssueID, Revision: hash, Title: content.Title, Body: body, ContentVersion: 2, Content: data, Criteria: []string{"AC-001"}}))
		revisions = append(revisions, store.ApprovedStoryRevision{ManagedStory: s, ParentID: 1230, Title: content.Title, Body: body, Hash: hash, FetchedAt: time.Now().UTC()})
		forge.children[number] = github.Issue{ID: s.IssueID, Number: number, Title: content.Title, Body: body, State: "open"}
	}
	mustProgress(t, f.db.SetManagedFeatureReady(ctx, f.session.TaskRef, 1, true))
	mustProgress(t, f.db.ApproveManagedStories(ctx, f.session.TaskRef, 1, revisions))
	for _, s := range revisions {
		mustProgress(t, f.db.DeliverStoryScopeNotice(ctx, s.ParentTask, 1, s.Key, "freeze"))
	}
	mustProgress(t, f.db.ActivateManagedScope(ctx, f.session.TaskRef, 1))
	renderer, err := report.New(report.Options{Redactor: redact.New(redact.Options{}), Limit: github.CommentSizeLimit})
	mustProgress(t, err)
	progress, err := NewStoryProgress(f.db, forge, renderer, 9, filepath.Join(filepath.Dir(f.db.Path()), "submission-locks"))
	mustProgress(t, err)
	f.opts.StoryProgress = progress
	f.opts.SubIssueCreator = forge
	return forge
}

func submissionSessionFixture(t *testing.T, workflow int) (*fixture, *progressForge, *SubmissionSession, SubmissionSessionOptions) {
	t.Helper()
	f := newFixture(t, nil)
	forge := typedProgressScope(t, f, workflow)
	seg, err := f.db.GetOrCreateSegment(t.Context(), f.session.ID, 1, "submission", store.AgentClaude)
	mustProgress(t, err)
	run, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: f.session.ID, SegmentID: seg.ID, Mode: "implement", Agent: store.AgentClaude, TriggerCommentID: 1, LogPath: func(int) string { return "run.log" }})
	mustProgress(t, err)
	revisions, err := f.db.ListApprovedStoryRevisions(t.Context(), f.session.TaskRef, 1)
	mustProgress(t, err)
	bundle := &agent.ApprovedBundle{WorkflowVersion: workflow, Parent: f.session.TaskRef, Generation: 1, RunID: run.ID}
	for _, s := range revisions {
		bundle.Stories = append(bundle.Stories, agent.ApprovedStory{Key: s.Key, IssueID: s.IssueID, IssueNumber: s.IssueNumber, ParentID: s.ParentID, Revision: s.Hash, Title: s.Title, Body: s.Body, State: s.State, Dependencies: s.Dependencies})
	}
	worktree := t.TempDir()
	mustProgress(t, f.opts.StoryProgress.Register(t.Context(), bundle, worktree))
	options := SubmissionSessionOptions{DB: f.db, Progress: f.opts.StoryProgress, Paths: f.opts.Paths, RunID: run.ID, Worktree: worktree, Bundle: bundle}
	session, err := StartSubmissionSession(t.Context(), options)
	mustProgress(t, err)
	if session == nil {
		t.Fatal("new contract did not start broker")
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 5*time.Second)
		defer cancel()
		mustProgress(t, session.Close(cleanup))
	})
	return f, forge, session, options
}

func capturedContribution(t *testing.T, worktree, story, stage string) agent.Contribution {
	t.Helper()
	content := []byte("package sample\n")
	path := story + ".go"
	mustProgress(t, os.WriteFile(filepath.Join(worktree, path), content, 0600))
	files := []agent.StorySnapshotFile{{Path: path, Content: content, SHA256: agent.StoryDigest(content)}}
	digest := agent.SnapshotDigest(files)
	c := agent.Contribution{Stage: stage, Story: story, Outcome: "engineering_complete", Engineering: &agent.EngineeringContribution{SnapshotID: "snapshot-" + story, CheckIDs: []string{"check-" + story}}, Capture: &agent.ContributionCapture{ImplementationDigest: digest, Files: []agent.ContributionCaptureFile{{Path: path, Content: base64.StdEncoding.EncodeToString(content), SHA256: files[0].SHA256, Deleted: false}}, Checks: []agent.ContributionCaptureCheck{{ID: "check-" + story, Name: "scoped test " + story, Passed: true, Evidence: "ok scoped fixture", ImplementationDigest: digest}}}}
	if stage == "legacy" {
		c.Outcome = "verified"
		c.Assessment = &agent.AssessmentContribution{SnapshotID: c.Engineering.SnapshotID, Criteria: []agent.ContributionFinding{{Name: "AC-001", Status: "satisfied", Classification: "blocking", Evidence: []agent.EvidenceExplanation{{CheckIDs: c.Engineering.CheckIDs, Summary: "Captured passing scoped result."}}}}, Reviews: []agent.ContributionFinding{}}
		for _, name := range []string{"code", "architecture", "plan"} {
			c.Assessment.Reviews = append(c.Assessment.Reviews, agent.ContributionFinding{Name: name, Status: "satisfied", Classification: "blocking", Evidence: []agent.EvidenceExplanation{{CheckIDs: []string{}, Summary: "Reviewed retained fixture source."}}})
		}
	}
	return c
}

func callContribution(t *testing.T, session *SubmissionSession, method string, c agent.Contribution) submission.Response {
	t.Helper()
	data, err := json.Marshal(c)
	mustProgress(t, err)
	response, err := submission.Call(t.Context(), session.DescriptorPath(), method, data)
	mustProgress(t, err)
	if response.Error != "" {
		t.Fatal(response.Error)
	}
	return response
}

func TestSubmissionLiveCorrectionAndFactoryEnvelope(t *testing.T) {
	f, forge, session, o := submissionSessionFixture(t, store.WorkflowSeparated)
	c := capturedContribution(t, o.Worktree, "US-1", "engineering")
	data, err := json.Marshal(c)
	mustProgress(t, err)
	broken := strings.Replace(string(data), `"evidence":"ok scoped fixture"`, `"evidence":["wrong container"]`, 1)
	broken = strings.TrimSuffix(broken, "}") + `,"parent":5399889036}`
	response, err := submission.Call(t.Context(), session.DescriptorPath(), "preflight", json.RawMessage(broken))
	mustProgress(t, err)
	var diagnostics agent.ContributionDiagnostics
	mustProgress(t, json.Unmarshal(response.Result, &diagnostics))
	if diagnostics.Valid || len(diagnostics.Diagnostics) < 2 {
		t.Fatalf("independent live defects missing: %+v", diagnostics)
	}
	_, err = f.db.GetStoryProgressEvent(t.Context(), o.RunID, 1)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatal("preflight allocated event")
	}
	valid := callContribution(t, session, "submit", c)
	var receipt store.SubmissionReceipt
	mustProgress(t, json.Unmarshal(valid.Result, &receipt))
	if receipt.InputState != "valid" || receipt.EngineeringState != "recorded" || receipt.AcceptanceState == "accepted" || receipt.RequestID != valid.RequestID {
		t.Fatalf("incorrect live decision: %+v", receipt)
	}
	retained, err := f.db.EngineeringEvidence(t.Context(), f.session.TaskRef, 1, "US-1")
	mustProgress(t, err)
	event, err := agent.ParseStoryEvent(retained.Payload)
	mustProgress(t, err)
	if event.Parent != f.session.TaskRef || event.RunID != o.RunID || event.Generation != 1 || event.IssueID != 2000 || event.Revision != o.Bundle.Stories[0].Revision || event.Sequence != receipt.Sequence || event.Attempt != receipt.StoryAttempt {
		t.Fatalf("factory envelope wrong: %+v", event)
	}
	stories, err := f.db.ListManagedStories(t.Context(), f.session.TaskRef, 1)
	mustProgress(t, err)
	if !stories[0].EngineeringComplete || stories[0].State == "completed" || forge.children[200].State != "open" {
		t.Fatal("engineering became acceptance or did not release dependency")
	}
	replay := callContribution(t, session, "submit", c)
	var repeated store.SubmissionReceipt
	mustProgress(t, json.Unmarshal(replay.Result, &repeated))
	if repeated.ID != receipt.ID {
		t.Fatal("lost acknowledgment replay duplicated receipt")
	}
}

func TestSubmissionRejectsChangedWorktreeAndUnauthorizedSelector(t *testing.T) {
	f, _, session, o := submissionSessionFixture(t, store.WorkflowSeparated)
	c := capturedContribution(t, o.Worktree, "US-1", "engineering")
	mustProgress(t, os.WriteFile(filepath.Join(o.Worktree, "US-1.go"), []byte("package changed\n"), 0600))
	response := callContribution(t, session, "preflight", c)
	var diagnostic agent.ContributionDiagnostics
	mustProgress(t, json.Unmarshal(response.Result, &diagnostic))
	if diagnostic.Valid {
		t.Fatal("changed snapshot passed preflight")
	}
	c.Story = "US-foreign"
	data, err := json.Marshal(c)
	mustProgress(t, err)
	response, err = submission.Call(t.Context(), session.DescriptorPath(), "submit", data)
	if err == nil && response.Error == "" {
		t.Fatal("foreign selector accepted")
	}
	_, err = f.db.GetStoryProgressEvent(t.Context(), o.RunID, 1)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatal("invalid request created event")
	}
}

func TestSubmissionLegacyDecisionRecovery(t *testing.T) {
	f, forge, session, o := submissionSessionFixture(t, store.WorkflowLegacy)
	forge.loseClose = true
	c := capturedContribution(t, o.Worktree, "US-1", "legacy")
	response := callContribution(t, session, "submit", c)
	var receipt store.SubmissionReceipt
	mustProgress(t, json.Unmarshal(response.Result, &receipt))
	if receipt.EngineeringState != "recorded" {
		t.Fatalf("forge failure lost engineering: %+v", receipt)
	}
	mustProgress(t, f.opts.StoryProgress.RecoverSubmissions(t.Context()))
	if err := f.opts.StoryProgress.Poll(t.Context()); err == nil {
		t.Fatal("injected lost closure response did not surface")
	}
	pending, err := f.db.GetSubmissionReceipt(t.Context(), o.RunID, response.RequestID)
	mustProgress(t, err)
	if pending.EngineeringState != "recorded" || pending.EffectState == "delivered" {
		t.Fatalf("lost forge acknowledgment claimed delivered: %+v", pending)
	}
	mustProgress(t, f.opts.StoryProgress.Poll(t.Context()))
	mustProgress(t, f.db.ReconcileSubmissionEffects(t.Context()))
	got, err := f.db.GetSubmissionReceipt(t.Context(), o.RunID, response.RequestID)
	mustProgress(t, err)
	if got.EngineeringState != "recorded" || got.EffectState != "delivered" || forge.children[200].State != "closed" {
		t.Fatalf("legacy recovery failed: %+v child %+v", got, forge.children[200])
	}
	changes := len(forge.changes)
	mustProgress(t, f.opts.StoryProgress.RecoverSubmissions(t.Context()))
	mustProgress(t, f.opts.StoryProgress.Poll(t.Context()))
	if len(forge.changes) != changes {
		t.Fatal("recovery duplicated closure")
	}
}

func TestSubmissionReplacementAttemptRejectsOldAuthority(t *testing.T) {
	f, _, old, o := submissionSessionFixture(t, store.WorkflowSeparated)
	c := capturedContribution(t, o.Worktree, "US-1", "engineering")
	replacement, err := StartSubmissionSession(t.Context(), o)
	mustProgress(t, err)
	defer replacement.Close(t.Context())
	data, err := json.Marshal(c)
	mustProgress(t, err)
	response, err := submission.Call(t.Context(), old.DescriptorPath(), "submit", data)
	if err == nil && response.Error == "" {
		t.Fatal("stale attempt accepted")
	}
	current := callContribution(t, replacement, "submit", c)
	var receipt store.SubmissionReceipt
	mustProgress(t, json.Unmarshal(current.Result, &receipt))
	if receipt.EngineeringState != "recorded" {
		t.Fatalf("current attempt failed: %+v", receipt)
	}
	_, err = f.db.GetStoryProgressEvent(t.Context(), o.RunID, 2)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("stale attempt created progress: %v", err)
	}
}

func TestSubmissionBothSyntheticRunnerPathsObserveDecisionBeforeExit(t *testing.T) {
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
				c := capturedContribution(t, req.WorktreePath, "US-1", "engineering")
				data, err := json.Marshal(c)
				if err != nil {
					return nil, err
				}
				broken := strings.TrimSuffix(string(data), "}") + `,"parent":5399889036}`
				broken = strings.Replace(broken, `"evidence":"ok scoped fixture"`, `"evidence":["wrong container"]`, 1)
				preflight, err := submission.Call(ctx, access.Descriptor, "preflight", json.RawMessage(broken))
				if err != nil {
					return nil, err
				}
				var diagnostics agent.ContributionDiagnostics
				if err := json.Unmarshal(preflight.Result, &diagnostics); err != nil {
					return nil, err
				}
				if diagnostics.Valid || len(diagnostics.Diagnostics) < 2 {
					return nil, fmt.Errorf("held runner did not receive independent correction diagnostics")
				}
				response, err := submission.Call(ctx, access.Descriptor, "submit", data)
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
					t.Fatalf("runner lacked authoritative decision: %+v", receipt)
				}
				run, err := f.db.GetRun(t.Context(), receipt.RunID)
				mustProgress(t, err)
				if run.Status != store.RunRunning || forge.children[200].State != "open" {
					t.Fatal("receipt arrived only after exit or premature closure")
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

func TestSubmissionValidationConsumesRetainedEvidenceOnly(t *testing.T) {
	f, forge, engineering, o := submissionSessionFixture(t, store.WorkflowSeparated)
	c := capturedContribution(t, o.Worktree, "US-1", "engineering")
	_ = callContribution(t, engineering, "submit", c)
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
	options := SubmissionSessionOptions{DB: f.db, Progress: f.opts.StoryProgress, Paths: f.opts.Paths, RunID: next.ID, Worktree: o.Worktree, Validation: validation}
	session, err := StartSubmissionSession(t.Context(), options)
	mustProgress(t, err)
	defer session.Close(t.Context())
	// A changed worktree cannot replace the retained snapshot or trigger fresh checks.
	changed := []byte("package changed_after_engineering\n")
	mustProgress(t, os.WriteFile(filepath.Join(o.Worktree, "US-1.go"), changed, 0600))
	assessment := agent.Contribution{Stage: "validate", Story: "US-1", Outcome: "assessed", Assessment: &agent.AssessmentContribution{SnapshotID: event.Evidence.ImplementationDigest, Criteria: []agent.ContributionFinding{{Name: "AC-001", Status: "satisfied", Classification: "blocking", Evidence: []agent.EvidenceExplanation{{CheckIDs: []string{event.Evidence.Tests[0].Name}, Summary: "Retained scoped execution passed."}}}}, Reviews: []agent.ContributionFinding{}}}
	for _, name := range []string{"code", "architecture", "plan"} {
		assessment.Assessment.Reviews = append(assessment.Assessment.Reviews, agent.ContributionFinding{Name: name, Status: "satisfied", Classification: "blocking", Evidence: []agent.EvidenceExplanation{{CheckIDs: []string{}, Summary: "Reviewed retained source."}}})
	}
	response := callContribution(t, session, "submit", assessment)
	var receipt store.SubmissionReceipt
	mustProgress(t, json.Unmarshal(response.Result, &receipt))
	if receipt.AcceptanceState != "accepted" || receipt.EngineeringState == "recorded" || receipt.EffectState == "delivered" {
		t.Fatalf("retained assessment states conflated: %+v", receipt)
	}
	current, err := os.ReadFile(filepath.Join(o.Worktree, "US-1.go"))
	mustProgress(t, err)
	if string(current) != string(changed) {
		t.Fatal("Validate modified the worktree")
	}
	if forge.children[200].State != "open" {
		t.Fatal("assessment receipt prematurely claimed issue closure")
	}
	assessment.Capture = c.Capture
	invalid := callContribution(t, session, "preflight", assessment)
	var diagnostics agent.ContributionDiagnostics
	mustProgress(t, json.Unmarshal(invalid.Result, &diagnostics))
	if diagnostics.Valid {
		t.Fatal("Validate accepted fresh capture")
	}
}

func TestSubmissionReferenceReworkChecksCurrentBytes(t *testing.T) {
	for _, recovery := range []bool{false, true} {
		t.Run(fmt.Sprintf("recovery=%t", recovery), func(t *testing.T) {
			f, _, session, o := submissionSessionFixture(t, store.WorkflowSeparated)
			c := capturedContribution(t, o.Worktree, "US-1", "engineering")
			callContribution(t, session, "submit", c)
			callContribution(t, session, "submit", agent.Contribution{Stage: "engineering", Story: "US-1", Outcome: "invalidated", Reason: "Rework before acceptance"})
			c.Capture = nil
			payload, err := json.Marshal(c)
			mustProgress(t, err)
			var pending store.SubmissionReceipt
			if recovery {
				pending, err = f.db.AllocateSubmission(t.Context(), store.SubmissionRequest{RunID: o.RunID, Attempt: session.binding.Attempt, RequestID: "crash-reference", Story: c.Story, Revision: o.Bundle.Stories[0].Revision, InputState: "valid", Payload: payload, Diagnostics: []byte(`{"valid":true}`)})
				mustProgress(t, err)
			}
			mustProgress(t, os.WriteFile(filepath.Join(o.Worktree, "US-1.go"), []byte("package changed\n"), 0600))
			if recovery {
				mustProgress(t, f.opts.StoryProgress.RecoverSubmissions(t.Context()))
				r, err := f.db.GetSubmissionReceipt(t.Context(), o.RunID, pending.RequestID)
				mustProgress(t, err)
				if r.EngineeringState != "rejected" {
					t.Fatalf("changed reference recovered as engineering: %+v", r)
				}
			} else {
				for _, method := range []string{"preflight", "submit"} {
					response, err := submission.Call(t.Context(), session.DescriptorPath(), method, payload)
					mustProgress(t, err)
					if method == "preflight" {
						var d agent.ContributionDiagnostics
						mustProgress(t, json.Unmarshal(response.Result, &d))
						if d.Valid {
							t.Fatal("changed retained snapshot passed preflight")
						}
					} else {
						var r store.SubmissionReceipt
						mustProgress(t, json.Unmarshal(response.Result, &r))
						if r.InputState != "rejected" || r.EngineeringState != "rejected" {
							t.Fatalf("changed retained snapshot recorded: %+v", r)
						}
					}
				}
			}
			stories, err := f.db.ListManagedStories(t.Context(), f.session.TaskRef, 1)
			mustProgress(t, err)
			if stories[0].EngineeringComplete {
				t.Fatal("rejected rework restored engineering")
			}
		})
	}
}

func TestSubmissionConcurrentStoriesKeepCorrelation(t *testing.T) {
	f, forge, session, o := submissionSessionFixture(t, store.WorkflowSeparated)
	// Independent intake is concurrent; the second story still waits for its
	// predecessor's engineering before its recorded evidence becomes actionable.
	payloads := make([][]byte, 2)
	for i, story := range []string{"US-1", "US-2"} {
		var err error
		payloads[i], err = json.Marshal(capturedContribution(t, o.Worktree, story, "engineering"))
		mustProgress(t, err)
	}
	type result struct {
		response submission.Response
		err      error
	}
	results := make(chan result, 2)
	for _, payload := range payloads {
		go func() {
			response, err := submission.Call(t.Context(), session.DescriptorPath(), "submit", payload)
			results <- result{response, err}
		}()
	}
	seen := map[int]bool{}
	for range payloads {
		r := <-results
		mustProgress(t, r.err)
		if r.response.Error != "" {
			t.Fatal(r.response.Error)
		}
		var receipt store.SubmissionReceipt
		mustProgress(t, json.Unmarshal(r.response.Result, &receipt))
		if seen[receipt.Sequence] || receipt.RunID != o.RunID || receipt.RequestID != r.response.RequestID {
			t.Fatalf("concurrent correlation lost: %+v", receipt)
		}
		seen[receipt.Sequence] = true
		if receipt.EngineeringState != "recorded" {
			t.Fatalf("concurrent engineering failed: %+v", receipt)
		}
	}
	stories, err := f.db.ListManagedStories(t.Context(), f.session.TaskRef, 1)
	mustProgress(t, err)
	for _, story := range stories {
		if !story.EngineeringComplete || forge.children[story.IssueNumber].State != "open" {
			t.Fatalf("incorrect concurrent story state: %+v", story)
		}
	}
}

func TestSubmissionFinalDrainRetainsPendingDecision(t *testing.T) {
	f, _, session, o := submissionSessionFixture(t, store.WorkflowSeparated)
	c := capturedContribution(t, o.Worktree, "US-1", "engineering")
	payload, err := json.Marshal(c)
	mustProgress(t, err)
	pending, err := f.db.AllocateSubmission(t.Context(), store.SubmissionRequest{RunID: o.RunID, Attempt: session.binding.Attempt, RequestID: "lost-before-record", Story: c.Story, Revision: o.Bundle.Stories[0].Revision, InputState: "valid", Payload: payload, Diagnostics: []byte(`{"valid":true}`)})
	mustProgress(t, err)
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	cleanup, stop := context.WithTimeout(context.WithoutCancel(cancelled), 5*time.Second)
	defer stop()
	mustProgress(t, session.Close(cleanup))
	r, err := f.db.GetSubmissionReceipt(t.Context(), o.RunID, pending.RequestID)
	mustProgress(t, err)
	if r.ID != pending.ID || r.EngineeringState != "recorded" || r.AcceptanceState == "accepted" {
		t.Fatalf("final drain lost decision: %+v", r)
	}
	mustProgress(t, f.opts.StoryProgress.RecoverSubmissions(t.Context()))
	_, err = f.db.GetStoryProgressEvent(t.Context(), o.RunID, r.Sequence+1)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("drain replay duplicated event: %v", err)
	}
	response, err := submission.Call(t.Context(), session.DescriptorPath(), "submit", payload)
	if err == nil && response.Error == "" {
		t.Fatal("closed attempt retained authority")
	}
}

func TestSubmissionTerminalValidationRequiresRecordedReceipt(t *testing.T) {
	for _, rejected := range []bool{false, true} {
		t.Run(fmt.Sprintf("rejected_receipt=%t", rejected), func(t *testing.T) {
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
			requestID := fmt.Sprintf("terminal-%s-0", session.binding.Attempt)
			if rejected {
				// Simulate an already durable rejected reply under this request
				// identity. A clean terminal process must not promote it to success.
				r, err := f.db.AllocateSubmission(t.Context(), store.SubmissionRequest{RunID: next.ID, Attempt: session.binding.Attempt, RequestID: requestID, Story: c.Story, Revision: o.Bundle.Stories[0].Revision, InputState: "rejected", Payload: payload, Diagnostics: []byte(`{"valid":false}`)})
				mustProgress(t, err)
				mustProgress(t, f.db.DecideSubmission(t.Context(), r.ID, store.SubmissionDecision{Engineering: "not_applicable", Acceptance: "rejected", Effects: "not_applicable", Plan: "not_applicable"}))
			}
			output := `{"contributions":[` + string(payload) + `]}`
			err = session.consumeTerminalValidation(t.Context(), output)
			if rejected {
				if err == nil {
					t.Fatal("rejected durable receipt became terminal success")
				}
			} else {
				mustProgress(t, err)
				r, err := f.db.GetSubmissionReceipt(t.Context(), next.ID, requestID)
				mustProgress(t, err)
				if r.AcceptanceState != "unverified" || r.InputState != "valid" {
					t.Fatalf("honest unverified findings lost: %+v", r)
				}
			}
		})
	}
}

func TestSubmissionRecoveryFromFreshStoreConnection(t *testing.T) {
	f, forge, session, o := submissionSessionFixture(t, store.WorkflowSeparated)
	c := capturedContribution(t, o.Worktree, "US-1", "engineering")
	payload, err := json.Marshal(c)
	mustProgress(t, err)
	pending, err := f.db.AllocateSubmission(t.Context(), store.SubmissionRequest{RunID: o.RunID, Attempt: session.binding.Attempt, RequestID: "crashed-before-decision", Story: c.Story, Revision: o.Bundle.Stories[0].Revision, InputState: "valid", Payload: payload, Diagnostics: []byte(`{"valid":true}`)})
	mustProgress(t, err)
	// Simulate loss of the serving broker after durable allocation, before its
	// normal final drain. Recovery has a fresh store connection and service.
	mustProgress(t, session.broker.Close())
	reopened, err := store.Open(t.Context(), f.db.Path())
	mustProgress(t, err)
	defer reopened.Close()
	mustProgress(t, reopened.Migrate(t.Context()))
	renderer, err := report.New(report.Options{Redactor: redact.New(redact.Options{}), Limit: github.CommentSizeLimit})
	mustProgress(t, err)
	progress, err := NewStoryProgress(reopened, forge, renderer, 9, filepath.Join(filepath.Dir(f.db.Path()), "submission-locks"))
	mustProgress(t, err)
	mustProgress(t, progress.RecoverSubmissions(t.Context()))
	r, err := reopened.GetSubmissionReceipt(t.Context(), o.RunID, pending.RequestID)
	mustProgress(t, err)
	if r.ID != pending.ID || r.Sequence != pending.Sequence || r.EngineeringState != "recorded" || r.EffectState != "pending" || forge.children[200].State != "open" {
		t.Fatalf("fresh recovery lost correlation or overstated remote effects: %+v", r)
	}
	mustProgress(t, progress.RecoverSubmissions(t.Context()))
	_, err = reopened.GetStoryProgressEvent(t.Context(), o.RunID, r.Sequence+1)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("fresh recovery duplicated event: %v", err)
	}
}
