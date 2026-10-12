package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

func typedApplyFixture(t *testing.T, twoSteps bool, withStop ...bool) (*Service, ApplyRequest, store.Schedule) {
	t.Helper()
	s := doctorIOService(t)
	ctx := t.Context()
	repo, err := s.DB.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: "git@github.com:acme/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.DB.GetOrCreateSession(ctx, repo, 406)
	if err != nil {
		t.Fatal(err)
	}
	schedule, _, err := s.DB.CreateOrGetSchedule(ctx, store.CreateScheduleParams{SessionID: session.ID, SourceCommentID: 7, OwnerHandle: "owner", Payload: "work", DueAt: time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.DB.BeginDoctorAttempt(ctx, session.TaskRef, store.DoctorAttempt{ID: "typed-proposer", SourceID: "typed-source", OwnerText: "Diagnose", PromptVersion: PromptVersion, GuideVersion: GuideVersion})
	if err != nil {
		t.Fatal(err)
	}
	input := ResultInput{Diagnosis: Diagnosis{Proposal: []RepairStep{{Number: 1, Target: fmt.Sprintf("schedule:%d", schedule.ID), Action: &store.DoctorRepairAction{Kind: "cancel_schedule", Before: "pending", After: "failed"}}}}}
	if twoSteps {
		second, _, err := s.DB.CreateOrGetSchedule(ctx, store.CreateScheduleParams{SessionID: session.ID, SourceCommentID: 8, OwnerHandle: "owner", Payload: "second", DueAt: time.Now().UTC()})
		if err != nil {
			t.Fatal(err)
		}
		input.Diagnosis.Proposal = append(input.Diagnosis.Proposal, RepairStep{Number: 2, Target: fmt.Sprintf("schedule:%d", second.ID), Action: &store.DoctorRepairAction{Kind: "cancel_schedule", Before: "pending", After: "failed"}})
	}
	if len(withStop) > 0 && withStop[0] {
		segment, err := s.DB.GetOrCreateSegment(ctx, session.ID, store.FirstSegmentOrdinal, "stop-session", store.AgentClaude)
		if err != nil {
			t.Fatal(err)
		}
		run, err := s.DB.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: store.ModeSpike, Agent: store.AgentClaude, TriggerCommentID: 9, LogPath: func(int) string { return "/logs/stop" }})
		if err != nil {
			t.Fatal(err)
		}
		for i := range input.Diagnosis.Proposal {
			input.Diagnosis.Proposal[i].Number++
		}
		input.Diagnosis.Proposal = append([]RepairStep{{Number: 1, Target: fmt.Sprintf("run:%d", run.ID), Action: &store.DoctorRepairAction{Kind: "stop_run", Before: "running", After: "failed"}}}, input.Diagnosis.Proposal...)
	}
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := s.DB.ReadDoctorEvidence(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DB.PrepareDoctorAttempt(ctx, store.DoctorAttempt{ID: "typed-proposer", Provider: "claude", ConfiguredModel: "fixture", Evidence: string(raw)}); err != nil {
		t.Fatal(err)
	}
	results, err := BindResults(ctx, s.DB, "typed-proposer", evidence, s.Redactor)
	if err != nil {
		t.Fatal(err)
	}
	for i := range input.Diagnosis.Proposal {
		step := &input.Diagnosis.Proposal[i]
		step.Precondition = "The recorded before-state still holds."
		step.Change = "Apply the exact typed repair."
		step.Reason = "Owner approved this diagnosed step."
		step.Reversibility = "The affected state is backed up first."
		step.Rollback = "Retain the backup for owner review."
	}
	input.Diagnosis.Findings = []Finding{{Explanation: "The approved schedule needs cancellation.", Uncertainty: "Only the retained task evidence was inspected.", Evidence: []string{fmt.Sprintf("schedule:%d", schedule.ID)}}}
	input.Diagnosis.CoverageGaps = evidence.Gaps
	retainLegacyRepairDiagnosis(t, results, "typed-proposal", input)
	pid, _, err := s.DB.DoctorProposalReference(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DB.PresentDoctorProposal(ctx, session.TaskRef, "typed-presentation", pid); err != nil {
		t.Fatal(err)
	}
	return s, ApplyRequest{Target: ref, SourceID: "typed-apply", Principal: "local-owner", PresentationID: "typed-presentation"}, schedule
}

type typedApplyRunner struct {
	mode    string
	runs    int
	receipt bool
}

func (*typedApplyRunner) IsolationEvidence(context.Context, string) (string, error) {
	return "scoped-only", nil
}
func (r *typedApplyRunner) RunRepair(ctx context.Context, in Invocation, tools *RepairTools, results *ResultService) (InvocationOutcome, error) {
	r.runs++
	steps, err := tools.service.DB.DoctorRepairSteps(ctx, tools.operation)
	if err != nil {
		return InvocationOutcome{}, err
	}
	first := 1
	completed := []int{1}
	if steps[0].Kind == "stop_run" {
		first = 2
		completed = []int{1, 2}
	}
	if _, err := tools.ApplyTyped(ctx, first, "cancel_schedule"); err != nil {
		return InvocationOutcome{}, err
	}
	if r.mode == "failure" {
		_, err := tools.ApplyTyped(ctx, 2, "requeue_schedule")
		return InvocationOutcome{}, err
	}
	if r.mode == "interrupted" {
		return InvocationOutcome{}, nil
	}
	input := ResultInput{Diagnosis: Diagnosis{Findings: []Finding{{Explanation: "Approved repair steps were rechecked.", Uncertainty: "No wider repair was attempted.", Evidence: []string{EvidenceHandle(results.evidence.Records[0])}}}, CoverageGaps: results.evidence.Gaps, Repair: &RepairSummary{OperationID: tools.operation, CompletedSteps: completed, Observations: []string{"Step reread"}}}}
	if r.mode == "notes" {
		input.Diagnosis.Repair.Unresolved = []string{"post-apply state not observable"}
		input.Diagnosis.Questions = []string{"Does the next turn start as intended?"}
	}
	data, err := json.Marshal(input)
	if err != nil {
		return InvocationOutcome{}, err
	}
	preflight, err := results.Preflight(ctx, data)
	if err != nil {
		return InvocationOutcome{}, err
	}
	input.ExpectedRevision = preflight.CurrentRevision
	data, err = json.Marshal(input)
	if err != nil {
		return InvocationOutcome{}, err
	}
	if _, err = results.Submit(ctx, "typed-final", data); err != nil {
		return InvocationOutcome{}, err
	}
	if _, err = results.Receipt(ctx, "typed-final"); err != nil {
		return InvocationOutcome{}, err
	}
	r.receipt = true
	return InvocationOutcome{RequestID: "typed-final", ObservedModel: in.ConfiguredModel, ProviderVersion: "controlled"}, nil
}

func TestTypedApplySettlesWithoutRestart(t *testing.T) {
	for _, mode := range []string{"success", "notes", "failure", "interrupted"} {
		t.Run(mode, func(t *testing.T) {
			s, request, schedule := typedApplyFixture(t, mode == "failure")
			runner := &typedApplyRunner{mode: mode}
			s.RepairRunner = runner
			s.RepairEnvironment = repairFixtureEnvironment{}
			operation, err := s.Apply(t.Context(), request)
			want := "failed"
			if mode == "success" || mode == "notes" {
				want = "completed"
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("failed run accepted")
			}
			if operation.State != want {
				t.Fatal(operation, err)
			}
			held, err := s.DB.DoctorRepairHeld(t.Context(), request.Target.String())
			if err != nil || held {
				t.Fatal("hold retained", held, err)
			}
			steps, err := s.DB.DoctorRepairSteps(t.Context(), operation.ID)
			if err != nil || steps[0].State != "verified" || steps[0].BackupReference == "" {
				t.Fatal(steps, err)
			}
			current, err := s.DB.GetSchedule(t.Context(), schedule.ID)
			if err != nil || string(current.Status) != "failed" {
				t.Fatal(current, err)
			}
			if (mode == "success" || mode == "notes") && !runner.receipt {
				t.Fatal("no durable result receipt")
			}
			if _, err = s.Apply(t.Context(), request); err == nil || runner.runs != 1 {
				t.Fatal("execution replayed", runner.runs, err)
			}
		})
	}
}

func TestTypedApplyUnapprovedPrincipalLeavesStateUntouched(t *testing.T) {
	for _, principal := range []string{"", "non-owner"} {
		name := "foreign-principal"
		if principal == "" {
			name = "missing-principal"
		}
		t.Run(name, func(t *testing.T) {
			s, request, schedule := typedApplyFixture(t, false)
			request.Principal = principal
			runner := &typedApplyRunner{}
			s.RepairRunner = runner
			s.RepairEnvironment = repairFixtureEnvironment{}
			if _, err := s.Apply(t.Context(), request); err == nil {
				t.Fatal("unapproved request accepted")
			}
			held, err := s.DB.DoctorRepairHeld(t.Context(), request.Target.String())
			if err != nil || held || runner.runs != 0 {
				t.Fatal(held, runner.runs, err)
			}
			current, err := s.DB.GetSchedule(t.Context(), schedule.ID)
			if err != nil || string(current.Status) != "pending" {
				t.Fatal(current, err)
			}
		})
	}
}

type typedStopTimeoutEnvironment struct{ calls []string }

func (*typedStopTimeoutEnvironment) Preflight(context.Context, naming.Ref) error { return nil }
func (e *typedStopTimeoutEnvironment) StopRun(context.Context, naming.Ref, string, int) error {
	e.calls = append(e.calls, "stop")
	return context.DeadlineExceeded
}
func (e *typedStopTimeoutEnvironment) Exclude(context.Context, naming.Ref, string) (string, error) {
	e.calls = append(e.calls, "exclude")
	return "drained", nil
}
func TestTypedApplyStopTimeoutFailsBeforeDrain(t *testing.T) {
	s, request, _ := typedApplyFixture(t, false, true)
	env := &typedStopTimeoutEnvironment{}
	runner := &typedApplyRunner{}
	s.RepairEnvironment = env
	s.RepairRunner = runner
	op, err := s.Apply(t.Context(), request)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) && op.Failure == "" {
		t.Fatal(op, err)
	}
	if op.State != "failed" || len(env.calls) != 1 || env.calls[0] != "stop" || runner.runs != 0 {
		t.Fatal(op, env.calls, runner.runs)
	}
	held, err := s.DB.DoctorRepairHeld(t.Context(), request.Target.String())
	if err != nil || held {
		t.Fatal(held, err)
	}
}

type typedStopEnvironment struct {
	db    *store.DB
	calls []string
}

func (*typedStopEnvironment) Preflight(context.Context, naming.Ref) error { return nil }
func (e *typedStopEnvironment) StopRun(ctx context.Context, _ naming.Ref, id string, number int) error {
	e.calls = append(e.calls, "stop")
	if _, err := e.db.BeginDoctorStopRun(ctx, id, number); err != nil {
		return err
	}
	// This fake joins the invocation synchronously before factory settlement.
	if err := e.db.CompleteDoctorStopRun(ctx, id, number); err != nil {
		return err
	}
	e.calls = append(e.calls, "joined")
	return nil
}
func (e *typedStopEnvironment) Exclude(ctx context.Context, ref naming.Ref, _ string) (string, error) {
	e.calls = append(e.calls, "exclude")
	if err := e.db.CheckDoctorTargetStopped(ctx, ref.String()); err != nil {
		return "", err
	}
	return "drained", nil
}
func TestTypedApplyStopsAndJoinsBeforeDrain(t *testing.T) {
	s, request, _ := typedApplyFixture(t, false, true)
	env := &typedStopEnvironment{db: s.DB}
	runner := &typedApplyRunner{}
	s.RepairEnvironment = env
	s.RepairRunner = runner
	op, err := s.Apply(t.Context(), request)
	if err != nil || op.State != "completed" {
		t.Fatal(op, err)
	}
	if len(env.calls) != 3 || env.calls[0] != "stop" || env.calls[1] != "joined" || env.calls[2] != "exclude" {
		t.Fatal(env.calls)
	}
	steps, err := s.DB.DoctorRepairSteps(t.Context(), op.ID)
	if err != nil || steps[0].State != "verified" || steps[0].ObservedAfter != "failed" || steps[0].BackupReference == "" {
		t.Fatal(steps, err)
	}
}

// refusingSyncEffects refuses sync_branch with a fixed-category error.
type refusingSyncEffects struct {
	calls int
	err   error
}

func (e *refusingSyncEffects) Execute(_ context.Context, _ naming.Ref, _ store.DoctorRepair, step store.DoctorRepairStep, _ string) (store.DoctorRepairStep, error) {
	e.calls++
	return step, e.err
}
func (*refusingSyncEffects) Verify(context.Context, naming.Ref, store.DoctorRepairStep) error {
	return nil
}

// cancelledSyncRunner calls sync_branch and then ends the way a provider does
// once the refusal cancelled its run.
type cancelledSyncRunner struct{}

func (cancelledSyncRunner) IsolationEvidence(context.Context, string) (string, error) {
	return "scoped-only", nil
}
func (cancelledSyncRunner) RunRepair(ctx context.Context, _ Invocation, tools *RepairTools, _ *ResultService) (InvocationOutcome, error) {
	_, err := tools.ApplyTyped(ctx, 1, "sync_branch")
	if ctx.Err() != nil {
		return InvocationOutcome{}, ctx.Err()
	}
	return InvocationOutcome{}, err
}

func TestTypedApplyRetainsToolRefusalReason(t *testing.T) {
	paths := []string{"docs/stack-profiles.md", "docs/setup.md"}
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"base rewritten", fmt.Errorf("sync refused: %w", ErrRepairBaseRewritten), ErrRepairBaseRewritten.Error()},
		{"before-head changed", fmt.Errorf("sync refused: %w", ErrRepairBeforeHeadChanged), ErrRepairBeforeHeadChanged.Error()},
		{"advisor failed", errors.Join(&RepairConflictError{Category: ErrRepairAdvisorFailed, Paths: paths[:1], Err: errors.New("untrusted advisor stderr")}, errors.New("reset noise")), "conflict advisor refused or failed: docs/stack-profiles.md"},
		{"advisor timed out", &RepairConflictError{Category: ErrRepairAdvisorTimedOut, Paths: paths[:1], Err: context.DeadlineExceeded}, "conflict advisor timed out: docs/stack-profiles.md"},
		{"confirmation failed", &RepairConflictError{Category: ErrRepairConfirmationFailed, Paths: paths, Err: errors.New("marker in resolution")}, "conflict resolution confirmation failed: docs/stack-profiles.md, docs/setup.md"},
		{"merge failed", fmt.Errorf("%w: %w", ErrRepairMergeFailed, errors.New("git output")), "merge failed"},
		{"recheck failed", fmt.Errorf("%w: behind", ErrRepairRecheckFailed), "recheck failed"},
		{"push rejected", fmt.Errorf("%w: %w", ErrRepairPushRejected, errors.New("remote: denied")), "push rejected"},
		{"uncategorized", errors.New("raw failure"), repairGenericFailure},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { typedApplyRefusal(t, c.err, c.want) })
	}
}

func typedApplyRefusal(t *testing.T, refusal error, want string) {
	s := doctorIOService(t)
	ctx := t.Context()
	repo, err := s.DB.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: "git@github.com:acme/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.DB.GetOrCreateSession(ctx, repo, 685)
	if err != nil {
		t.Fatal(err)
	}
	branch := "lf/685-case"
	if _, err = s.DB.FreezeSessionSlug(ctx, session.ID, "case", branch); err != nil {
		t.Fatal(err)
	}
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	head, baseHead := strings.Repeat("a", 40), strings.Repeat("b", 40)
	evidence := store.DoctorEvidence{TaskRef: ref.String(), Records: []store.DoctorEvidenceRecord{{Kind: "branch", ID: branch, State: head, Identity: baseHead}}}
	c, err := s.DB.BeginDoctorAttempt(ctx, ref.String(), store.DoctorAttempt{ID: "refusal-proposer", SourceID: "refusal-source", OwnerText: "Diagnose", PromptVersion: PromptVersion, GuideVersion: GuideVersion})
	if err != nil {
		t.Fatal(err)
	}
	captured, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DB.PrepareDoctorAttempt(ctx, store.DoctorAttempt{ID: "refusal-proposer", Provider: "claude", ConfiguredModel: "fixture", Evidence: string(captured)}); err != nil {
		t.Fatal(err)
	}
	results, err := BindResults(ctx, s.DB, "refusal-proposer", evidence, s.Redactor)
	if err != nil {
		t.Fatal(err)
	}
	input := ResultInput{Diagnosis: Diagnosis{Findings: []Finding{{Explanation: "Branch needs synchronization.", Uncertainty: "Local refs only.", Evidence: []string{"branch:" + branch}}}, Proposal: []RepairStep{{Number: 1, Target: "branch:" + branch, Precondition: "Captured head", Change: "Merge captured base", Reason: "Restore synchronization", Reversibility: "Backed up head", Rollback: "Restore head", Action: &store.DoctorRepairAction{Kind: "sync_branch", Before: head, After: baseHead}}}}}
	retainLegacyRepairDiagnosis(t, results, "refusal-proposal", input)
	pid, _, err := s.DB.DoctorProposalReference(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DB.PresentDoctorProposal(ctx, ref.String(), "refusal-presentation", pid); err != nil {
		t.Fatal(err)
	}
	effects := &refusingSyncEffects{err: refusal}
	s.RepairEffects = effects
	s.RepairRunner = cancelledSyncRunner{}
	s.RepairEnvironment = repairFixtureEnvironment{}
	operation, err := s.Apply(ctx, ApplyRequest{Target: ref, SourceID: "refusal-apply", Principal: "local-owner", PresentationID: "refusal-presentation"})
	if err == nil || effects.calls != 1 || !strings.HasPrefix(err.Error(), want) || strings.Contains(err.Error(), "untrusted advisor stderr") {
		t.Fatalf("refused sync: calls %d err %v, want reason %q", effects.calls, err, want)
	}
	saved, err := s.DB.ReadDoctorRepair(ctx, ref.String())
	if err != nil {
		t.Fatal(err)
	}
	if saved.State != "failed" || saved.Failure != want || operation.Failure != want || strings.Contains(saved.Failure, "context canceled") {
		t.Fatalf("repair %s failure %q (returned %q), want %q", saved.State, saved.Failure, operation.Failure, want)
	}
}

// writeIntentSyncEffects records the approved head the way the factory sync
// does before any git work, then refuses or completes the step.
type writeIntentSyncEffects struct {
	db     *store.DB
	refuse bool
}

func (e *writeIntentSyncEffects) Execute(ctx context.Context, _ naming.Ref, operation store.DoctorRepair, step store.DoctorRepairStep, _ string) (store.DoctorRepairStep, error) {
	if err := e.db.BeginDoctorBranchSync(ctx, operation.ID, step.Number, step.Before); err != nil {
		return step, err
	}
	if e.refuse {
		return step, &RepairConflictError{Category: ErrRepairAdvisorFailed, Paths: []string{"docs/setup.md"}, Err: errors.New("docs conflict")}
	}
	return e.db.FinishDoctorBranchSync(ctx, operation.ID, step.Number, step.After)
}
func (*writeIntentSyncEffects) Verify(context.Context, naming.Ref, store.DoctorRepairStep) error {
	return nil
}

// finishingSyncRunner applies the approved sync step and submits its result.
type finishingSyncRunner struct{}

func (finishingSyncRunner) IsolationEvidence(context.Context, string) (string, error) {
	return "scoped-only", nil
}
func (finishingSyncRunner) RunRepair(ctx context.Context, in Invocation, tools *RepairTools, results *ResultService) (InvocationOutcome, error) {
	if _, err := tools.ApplyTyped(ctx, 1, "sync_branch"); err != nil {
		return InvocationOutcome{}, err
	}
	input := ResultInput{Diagnosis: Diagnosis{Findings: []Finding{{Explanation: "Approved repair steps were rechecked.", Uncertainty: "No wider repair was attempted.", Evidence: []string{EvidenceHandle(results.evidence.Records[0])}}}, CoverageGaps: results.evidence.Gaps, Repair: &RepairSummary{OperationID: tools.operation, CompletedSteps: []int{1}, Observations: []string{"Step reread"}}}}
	data, err := json.Marshal(input)
	if err != nil {
		return InvocationOutcome{}, err
	}
	preflight, err := results.Preflight(ctx, data)
	if err != nil {
		return InvocationOutcome{}, err
	}
	input.ExpectedRevision = preflight.CurrentRevision
	if data, err = json.Marshal(input); err != nil {
		return InvocationOutcome{}, err
	}
	if _, err = results.Submit(ctx, "sync-final", data); err != nil {
		return InvocationOutcome{}, err
	}
	if _, err = results.Receipt(ctx, "sync-final"); err != nil {
		return InvocationOutcome{}, err
	}
	return InvocationOutcome{RequestID: "sync-final", ObservedModel: in.ConfiguredModel, ProviderVersion: "controlled"}, nil
}

func presentSyncProposal(t *testing.T, s *Service, ref naming.Ref, branch, head, baseHead, name string) {
	t.Helper()
	ctx := t.Context()
	evidence := store.DoctorEvidence{TaskRef: ref.String(), Records: []store.DoctorEvidenceRecord{{Kind: "branch", ID: branch, State: head, Identity: baseHead}}}
	c, err := s.DB.BeginDoctorAttempt(ctx, ref.String(), store.DoctorAttempt{ID: name + "-proposer", SourceID: name + "-source", OwnerText: "Diagnose", PromptVersion: PromptVersion, GuideVersion: GuideVersion})
	if err != nil {
		t.Fatal(err)
	}
	captured, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DB.PrepareDoctorAttempt(ctx, store.DoctorAttempt{ID: name + "-proposer", Provider: "claude", ConfiguredModel: "fixture", Evidence: string(captured)}); err != nil {
		t.Fatal(err)
	}
	results, err := BindResults(ctx, s.DB, name+"-proposer", evidence, s.Redactor)
	if err != nil {
		t.Fatal(err)
	}
	input := ResultInput{Diagnosis: Diagnosis{Findings: []Finding{{Explanation: "Branch needs synchronization.", Uncertainty: "Local refs only.", Evidence: []string{"branch:" + branch}}}, Proposal: []RepairStep{{Number: 1, Target: "branch:" + branch, Precondition: "Captured head", Change: "Merge captured base", Reason: "Restore synchronization", Reversibility: "Backed up head", Rollback: "Restore head", Action: &store.DoctorRepairAction{Kind: "sync_branch", Before: head, After: baseHead}}}}}
	// A later proposal in the same conversation revises its retained result.
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	preflight, err := results.Preflight(ctx, data)
	if err != nil {
		t.Fatal(err)
	}
	input.ExpectedRevision = preflight.CurrentRevision
	retainLegacyRepairDiagnosis(t, results, name+"-proposal", input)
	pid, _, err := s.DB.DoctorProposalReference(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DB.PresentDoctorProposal(ctx, ref.String(), name+"-presentation", pid); err != nil {
		t.Fatal(err)
	}
}

// A refused sync whose step already reached write_intent must not leave a
// needs_review repair that blocks every later repair of the task (#708).
func TestTypedSyncRefusalAfterWriteIntentAllowsNewRepair(t *testing.T) {
	s := doctorIOService(t)
	ctx := t.Context()
	repo, err := s.DB.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: "git@github.com:acme/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.DB.GetOrCreateSession(ctx, repo, 629)
	if err != nil {
		t.Fatal(err)
	}
	branch := "lf/629-case"
	if _, err = s.DB.FreezeSessionSlug(ctx, session.ID, "case", branch); err != nil {
		t.Fatal(err)
	}
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	head, baseHead := strings.Repeat("a", 40), strings.Repeat("b", 40)
	presentSyncProposal(t, s, ref, branch, head, baseHead, "first")
	s.RepairEffects = &writeIntentSyncEffects{db: s.DB, refuse: true}
	s.RepairRunner = cancelledSyncRunner{}
	s.RepairEnvironment = repairFixtureEnvironment{}
	first, err := s.Apply(ctx, ApplyRequest{Target: ref, SourceID: "first-apply", Principal: "local-owner", PresentationID: "first-presentation"})
	if err == nil {
		t.Fatal("refused sync accepted")
	}
	// A later owner stop must not reopen the settled repair as needs_review.
	if err = s.DB.StopDoctorRepair(ctx, ref.String(), "Owner stopped repair"); err != nil {
		t.Fatal(err)
	}
	saved, err := s.DB.ReadDoctorRepair(ctx, ref.String())
	if err != nil {
		t.Fatal(err)
	}
	steps, err := s.DB.DoctorRepairSteps(ctx, saved.ID)
	if err != nil || steps[0].State != "write_intent" {
		t.Fatal("step did not reach write_intent", steps, err)
	}
	if saved.State != "failed" || !saved.Fenced {
		t.Fatalf("refused repair %s fenced %v, want failed", saved.State, saved.Fenced)
	}
	if held, err := s.DB.DoctorRepairHeld(ctx, ref.String()); err != nil || held {
		t.Fatal("hold retained", held, err)
	}

	presentSyncProposal(t, s, ref, branch, head, baseHead, "second")
	s.RepairEffects = &writeIntentSyncEffects{db: s.DB}
	s.RepairRunner = finishingSyncRunner{}
	second, err := s.Apply(ctx, ApplyRequest{Target: ref, SourceID: "second-apply", Principal: "local-owner", PresentationID: "second-presentation"})
	if err != nil {
		t.Fatal("new repair refused", err)
	}
	if second.ID == first.ID || second.State != "completed" {
		t.Fatalf("second repair %s state %s, first %s", second.ID, second.State, first.ID)
	}
}
