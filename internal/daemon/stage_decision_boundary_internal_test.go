package daemon

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
)

type stageBoundaryProbe struct {
	target    store.StageDecisionTarget
	apply     func(context.Context, store.StageDecisionTarget, string, string, time.Time) (store.StageDecisionReceipt, error)
	calls     int
	recovered int
}

func (p *stageBoundaryProbe) ResolveStageDecisionTarget(context.Context, string, string, string) (store.StageDecisionTarget, error) {
	return p.target, nil
}
func (p *stageBoundaryProbe) ApplyStageDecision(ctx context.Context, target store.StageDecisionTarget, change, key string, at time.Time) (store.StageDecisionReceipt, error) {
	p.calls++
	if p.apply != nil {
		return p.apply(ctx, target, change, key, at)
	}
	return store.StageDecisionReceipt{ExecutionID: key, State: "applied"}, nil
}
func (p *stageBoundaryProbe) RecoverStageDecision(context.Context, string, time.Time) (store.StageDecisionReceipt, bool, error) {
	p.recovered++
	return store.StageDecisionReceipt{State: "applied"}, true, nil
}

type stageBoundaryForge struct {
	*doctorTestForge
	calls   int
	failure error
}

func (f *stageBoundaryForge) GetIssue(context.Context, github.Repo, int) (github.Issue, error) {
	f.calls++
	return f.issue, f.failure
}

func newStageBoundaryFixture(t *testing.T) (stageDecisionBoundary, *stageBoundaryProbe, *stageBoundaryForge, *researchRecoveryFixture) {
	t.Helper()
	f := newResearchRecoveryFixture(t)
	source := &stageBoundaryForge{doctorTestForge: &doctorTestForge{issue: github.Issue{Number: f.session.IssueNumber, State: github.StateOpen, Labels: []string{"Mode: Research"}}}}
	probe := &stageBoundaryProbe{target: store.StageDecisionTarget{TaskRef: f.session.TaskRef, SessionID: f.session.ID, RepositoryID: f.session.RepoID, Workflow: "research", Stage: "investigation", Operation: "revise_task", Guard: "exact-reviewed-binding"}}
	boundary := stageDecisionBoundary{daemon: &Daemon{db: f.db, forge: source}, local: probe}
	return boundary, probe, source, f
}

func TestStageDecisionAvailabilityRejectsClosedChangedOrUnreadableTask(t *testing.T) {
	for _, scenario := range []string{"closed", "changed mode", "ambiguous mode", "unreadable"} {
		t.Run(scenario, func(t *testing.T) {
			boundary, probe, source, _ := newStageBoundaryFixture(t)
			switch scenario {
			case "closed":
				source.issue.State = "closed"
			case "changed mode":
				source.issue.Labels = []string{"Mode: Plan"}
			case "ambiguous mode":
				source.issue.Labels = []string{"Mode: Research", "Mode: Plan"}
			case "unreadable":
				source.failure = errors.New("source unavailable")
			}
			if _, err := boundary.ResolveStageDecisionTarget(t.Context(), probe.target.TaskRef, "investigation", "revise_task"); err == nil {
				t.Fatal("unavailable task produced a proposal")
			}
			if _, err := boundary.ApplyStageDecision(t.Context(), probe.target, "reviewed change", "owner-action:stage", time.Now()); err == nil {
				t.Fatal("unavailable task admitted an effect")
			}
			if probe.calls != 0 {
				t.Fatal("unavailable task reached atomic transition")
			}
		})
	}
}

func TestStageDecisionAvailabilityRechecksAfterPreviewAndReleasesLock(t *testing.T) {
	boundary, probe, source, f := newStageBoundaryFixture(t)
	target, err := boundary.ResolveStageDecisionTarget(t.Context(), probe.target.TaskRef, "investigation", "revise_task")
	researchRecoveryCheck(t, err)
	source.issue.State = "closed"
	if _, err = boundary.ApplyStageDecision(t.Context(), target, "reviewed change", "owner-action:stage", time.Now()); !errors.Is(err, store.ErrStageDecisionTarget) {
		t.Fatalf("closed-after-preview refusal: %v", err)
	}
	if probe.calls != 0 || source.calls != 2 {
		t.Fatalf("effect or cached availability: effects=%d reads=%d", probe.calls, source.calls)
	}
	locked, err := f.db.AcquireSessionLock(t.Context(), f.session.ID, "following-request")
	researchRecoveryCheck(t, err)
	if !locked {
		t.Fatal("refusal retained the task lock")
	}
	researchRecoveryCheck(t, f.db.ReleaseSessionLock(t.Context(), f.session.ID, "following-request"))
}

func TestStageDecisionAvailabilityPreservesBusyTaskAndExactTarget(t *testing.T) {
	boundary, probe, source, f := newStageBoundaryFixture(t)
	locked, err := f.db.AcquireSessionLock(t.Context(), f.session.ID, "other-owner-turn")
	researchRecoveryCheck(t, err)
	if !locked {
		t.Fatal("fixture lock unavailable")
	}
	if _, err = boundary.ApplyStageDecision(t.Context(), probe.target, "reviewed change", "owner-action:stage", time.Now()); !errors.Is(err, store.ErrStageDecisionTarget) {
		t.Fatalf("busy task accepted: %v", err)
	}
	if probe.calls != 0 || source.calls != 0 {
		t.Fatal("busy task reached provider or transition")
	}
	researchRecoveryCheck(t, f.db.ReleaseSessionLock(t.Context(), f.session.ID, "other-owner-turn"))
	probe.apply = func(ctx context.Context, target store.StageDecisionTarget, change, key string, _ time.Time) (store.StageDecisionReceipt, error) {
		if !reflect.DeepEqual(target, probe.target) || change != "reviewed change" || key != "owner-action:stage" {
			t.Fatal("adapter substituted the reviewed input")
		}
		held, err := f.db.AcquireSessionLock(ctx, f.session.ID, "competing-request")
		researchRecoveryCheck(t, err)
		if held {
			t.Fatal("atomic transition ran without the task lock")
		}
		return store.StageDecisionReceipt{State: "applied"}, nil
	}
	receipt, err := boundary.ApplyStageDecision(t.Context(), probe.target, "reviewed change", "owner-action:stage", time.Now())
	researchRecoveryCheck(t, err)
	if receipt.State != "applied" || probe.calls != 1 {
		t.Fatalf("open transition not forwarded: %+v", receipt)
	}
}

func TestStageDecisionAvailabilityKeepsHistoricalAndStandaloneModeRules(t *testing.T) {
	for _, scenario := range []string{"missing label", "Bug mode-free"} {
		t.Run(scenario, func(t *testing.T) {
			boundary, probe, source, _ := newStageBoundaryFixture(t)
			source.issue.Labels = nil
			if scenario == "Bug mode-free" {
				probe.target.Workflow = "bug"
				probe.target.Stage = "analysis"
				source.issue.Labels = []string{"Mode: Plan"}
			}
			if _, err := boundary.ResolveStageDecisionTarget(t.Context(), probe.target.TaskRef, probe.target.Stage, "revise_task"); err != nil {
				t.Fatalf("compatible current stage refused: %v", err)
			}
		})
	}
}

func TestStageDecisionReceiptRecoveryDoesNotRequireOpenIssue(t *testing.T) {
	boundary, probe, source, _ := newStageBoundaryFixture(t)
	source.issue.State = "closed"
	receipt, found, err := boundary.RecoverStageDecision(t.Context(), "owner-action:stage", time.Now())
	researchRecoveryCheck(t, err)
	if !found || receipt.State != "applied" || probe.recovered != 1 || source.calls != 0 {
		t.Fatalf("known receipt depended on task availability: %+v %v reads=%d", receipt, found, source.calls)
	}
}
