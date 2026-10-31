package store

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

func managedPlanDecisionFixture(t *testing.T) (*DB, StageDecisionTarget, OwnerAction, context.Context, time.Time) {
	t.Helper()
	db, in := ownerReplyFixture(t, true, "plan", "shape", "review", "review")
	captureDecisionProfile(t, db, in.InstanceID, decisionResearchProfile)
	state := planbrief.PlanState{SharedRevision: "initial"}
	if _, _, err := state.ProposeStorySet([]planbrief.PlanStorySetEntry{{ID: "s1", Name: "Storage", Intent: "Retain exact state"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SavePlanWorkflowState(t.Context(), in.TaskRef, 0, state); err != nil {
		t.Fatal(err)
	}
	if err := db.PutManagedFeature(t.Context(), ManagedFeature{ParentTask: in.TaskRef, Generation: 1, State: "planning"}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetManagedFeatureReady(t.Context(), in.TaskRef, 1, true); err != nil {
		t.Fatal(err)
	}
	target, err := db.ResolveStageDecisionTarget(t.Context(), in.TaskRef, "set", "revise_task")
	if err != nil {
		t.Fatal(err)
	}
	p, ctx, at := admitStageDecisionFixture(t, db, target, in.At)
	return db, target, p, ctx, at
}

func TestStageDecisionManagedBindingRejectsFreezeOrGenerationChange(t *testing.T) {
	for _, change := range []string{"freeze", "generation", "removed"} {
		t.Run(change, func(t *testing.T) {
			db, target, p, ctx, at := managedPlanDecisionFixture(t)
			before, revision, err := db.GetPlanWorkflowState(t.Context(), target.PlanParent)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "freeze":
				err = db.CompareManagedFeature(t.Context(), ManagedFeature{ParentTask: target.PlanParent, Generation: 1, State: "freezing"}, "planning")
			case "generation":
				_, err = db.sql.ExecContext(t.Context(), `UPDATE managed_features SET generation=2 WHERE parent_task=?`, target.PlanParent)
			case "removed":
				_, err = db.sql.ExecContext(t.Context(), `DELETE FROM managed_features WHERE parent_task=?`, target.PlanParent)
			}
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := db.ApplyStageDecision(ctx, target, "Clarify storage responsibilities", p.ExecutionID, at)
			if err != nil || receipt.State != "refused" {
				t.Fatalf("lifecycle change admitted: %+v %v", receipt, err)
			}
			after, currentRevision, err := db.GetPlanWorkflowState(t.Context(), target.PlanParent)
			if err != nil || revision != currentRevision || !reflect.DeepEqual(before, after) {
				t.Fatalf("refusal changed Plan: %+v %v", after, err)
			}
		})
	}
}

func TestStageDecisionManagedRevisionClearsReadinessAndBlocksScopeFreeze(t *testing.T) {
	db, target, p, ctx, at := managedPlanDecisionFixture(t)
	receipt, err := db.ApplyStageDecision(ctx, target, "Clarify storage responsibilities", p.ExecutionID, at)
	if err != nil || receipt.State != "applied" {
		t.Fatalf("revision: %+v %v", receipt, err)
	}
	feature, err := db.GetManagedFeature(t.Context(), target.PlanParent)
	if err != nil || feature.Ready {
		t.Fatalf("revision retained readiness: %+v %v", feature, err)
	}
	if err = db.SetManagedFeatureReady(t.Context(), target.PlanParent, 1, true); err != nil {
		t.Fatal(err)
	}
	if err = db.ApproveManagedStories(t.Context(), target.PlanParent, 1, nil); !errors.Is(err, ErrPlanNotReady) {
		t.Fatalf("scope freeze overtook queued revision: %v", err)
	}
	feature, err = db.GetManagedFeature(t.Context(), target.PlanParent)
	if err != nil || feature.State != "planning" {
		t.Fatalf("scope froze: %+v %v", feature, err)
	}
}

func TestStageDecisionQueuedPlanRevisionRejectsSupersededBindings(t *testing.T) {
	for _, change := range []string{"generation", "candidate"} {
		t.Run(change, func(t *testing.T) {
			db, target, p, ctx, at := managedPlanDecisionFixture(t)
			receipt, err := db.ApplyStageDecision(ctx, target, "Clarify storage responsibilities", p.ExecutionID, at)
			if err != nil || receipt.State != "applied" {
				t.Fatalf("revision: %+v %v", receipt, err)
			}
			id, err := strconv.ParseInt(receipt.ContinuationRef, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			if _, found, err := db.StageDecisionForDispatch(t.Context(), id); err != nil || !found {
				t.Fatalf("fresh continuation missing: %v %v", found, err)
			}
			if change == "generation" {
				_, err = db.sql.ExecContext(t.Context(), `UPDATE managed_features SET generation=2 WHERE parent_task=?`, target.PlanParent)
			} else {
				state, revision, e := db.GetPlanWorkflowState(t.Context(), target.PlanParent)
				if e != nil {
					t.Fatal(e)
				}
				_, _, err = state.ProposeStorySet([]planbrief.PlanStorySetEntry{{ID: "s1", Name: "Revised storage", Intent: "New owner decision"}})
				if err == nil {
					_, err = db.SavePlanWorkflowState(t.Context(), target.PlanParent, revision, state)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, found, err := db.StageDecisionForDispatch(t.Context(), id); !errors.Is(err, ErrStageDecisionTarget) || found {
				t.Fatalf("superseded continuation reached worker: %v %v", found, err)
			}
		})
	}
}
