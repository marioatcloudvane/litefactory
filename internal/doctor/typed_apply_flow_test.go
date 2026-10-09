package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	for _, mode := range []string{"success", "failure", "interrupted"} {
		t.Run(mode, func(t *testing.T) {
			s, request, schedule := typedApplyFixture(t, mode == "failure")
			runner := &typedApplyRunner{mode: mode}
			s.RepairRunner = runner
			s.RepairEnvironment = repairFixtureEnvironment{}
			operation, err := s.Apply(t.Context(), request)
			want := "failed"
			if mode == "success" {
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
			if mode == "success" && !runner.receipt {
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
