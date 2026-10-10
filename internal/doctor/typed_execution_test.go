package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

func typedExecutionFixture(t *testing.T) (*RepairTools, store.Schedule) {
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
	evidence := store.DoctorEvidence{TaskRef: session.TaskRef}
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
	input := ResultInput{Diagnosis: Diagnosis{Proposal: []RepairStep{{Number: 1, Target: fmt.Sprintf("schedule:%d", schedule.ID), Action: &store.DoctorRepairAction{Kind: "cancel_schedule", Before: "pending", After: "failed"}}}}}
	retainLegacyRepairDiagnosis(t, results, "typed-proposal", input)
	pid, _, err := s.DB.DoctorProposalReference(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DB.PresentDoctorProposal(ctx, session.TaskRef, "typed-presentation", pid); err != nil {
		t.Fatal(err)
	}
	op, err := s.DB.ReserveDoctorRepair(ctx, store.DoctorApproval{ID: "typed-execution", SourceID: "typed-apply", Principal: "local-owner", TaskRef: session.TaskRef, PresentationID: "typed-presentation"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DB.AcknowledgeDoctorExclusion(ctx, op.ID, "fixture-daemon"); err != nil {
		t.Fatal(err)
	}
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	return &RepairTools{service: s, target: ref, operation: op.ID}, schedule
}

func TestTypedToolRefusalFencesFollowingCalls(t *testing.T) {
	for _, mode := range []string{"wrong-kind", "unknown-step", "after-success"} {
		t.Run(mode, func(t *testing.T) {
			tools, schedule := typedExecutionFixture(t)
			var backup string
			if mode == "after-success" {
				step, err := tools.ApplyTyped(t.Context(), 1, "cancel_schedule")
				if err != nil {
					t.Fatal(err)
				}
				backup = step.BackupReference
				if err = tools.verifyAll(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			number, kind := 1, "requeue_schedule"
			if mode != "wrong-kind" {
				number, kind = 2, "cancel_schedule"
			}
			if _, err := tools.ApplyTyped(t.Context(), number, kind); err == nil {
				t.Fatal("invalid tool accepted")
			}
			if _, err := tools.ApplyTyped(t.Context(), 1, "cancel_schedule"); err == nil {
				t.Fatal("tool continued after refusal")
			}
			operation, err := tools.service.DB.ReadDoctorRepair(t.Context(), tools.target.String())
			if err != nil || !operation.Fenced {
				t.Fatalf("missing failure fence: %+v %v", operation, err)
			}
			rows, err := tools.service.DB.DoctorRepairSteps(t.Context(), tools.operation)
			if err != nil {
				t.Fatal(err)
			}
			got, err := tools.service.DB.GetSchedule(t.Context(), schedule.ID)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "after-success" {
				if rows[0].BackupReference != backup || rows[0].State != "verified" || got.Status != store.ScheduleFailed {
					t.Fatalf("earlier backup or effect lost: %+v %+v", rows, got)
				}
			} else if rows[0].BackupReference != "" || rows[0].State != "pending" || got.Status != store.SchedulePending {
				t.Fatalf("refusal changed state: %+v %+v", rows, got)
			}
		})
	}
}

func TestTypedToolWaitsForSharedEffectLock(t *testing.T) {
	tools, schedule := typedExecutionFixture(t)
	unlock, err := tools.service.repairEffectLock(t.Context(), tools.target)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	_, callErr := tools.ApplyTyped(ctx, 1, "cancel_schedule")
	if err = unlock(); err != nil {
		t.Fatal(err)
	}
	if callErr == nil {
		t.Fatal("typed tool crossed effect lock")
	}
	got, err := tools.service.DB.GetSchedule(t.Context(), schedule.ID)
	if err != nil || got.Status != store.SchedulePending {
		t.Fatal(got, err)
	}
	if _, err = tools.ApplyTyped(t.Context(), 1, "cancel_schedule"); err != nil {
		t.Fatal(err)
	}
}

func TestStaleTypedToolCannotFenceCurrentRepair(t *testing.T) {
	tools, _ := typedExecutionFixture(t)
	stale := &RepairTools{service: tools.service, target: tools.target, operation: "older-operation"}
	if _, err := stale.ApplyTyped(t.Context(), 1, "cancel_schedule"); err == nil {
		t.Fatal("stale operation accepted")
	}
	operation, err := tools.service.DB.ReadDoctorRepair(t.Context(), tools.target.String())
	if err != nil || operation.Fenced {
		t.Fatalf("stale call fenced current repair: %+v %v", operation, err)
	}
	if _, err = tools.ApplyTyped(t.Context(), 1, "cancel_schedule"); err != nil {
		t.Fatal(err)
	}
}
