package store

import (
	"reflect"
	"testing"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

func TestPlanSetSizingSurvivesSavedStateAndManagedReplan(t *testing.T) {
	for _, legacy := range []bool{true, false} {
		name := "new"
		if legacy {
			name = "legacy"
		}
		t.Run(name, func(t *testing.T) {
			db, session, approvals := approvedFixture(t)
			ctx := t.Context()
			state := planbrief.PlanState{SharedRevision: "initial"}
			var revision int64
			if legacy {
				// Historical wire shape: neither sizing fields nor creation policy.
				const saved = `{"shared_revision":"initial","story_set":{"revision":"d2b17434620faa3b99a4b0ef2f6db49c05eec897382b9106efc9592a9a04a7a4","entries":[{"id":"a","name":"A","intent":"Do A","depends_on":["b","c"]},{"id":"b","name":"B","intent":"Do B"},{"id":"c","name":"C","intent":"Do C"}]}}`
				if _, err := db.sql.ExecContext(ctx, `INSERT INTO plan_workflow_states(parent,revision,state_json) VALUES(?,?,?)`, session.TaskRef, 1, saved); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, _, err := state.ProposeStorySet([]planbrief.PlanStorySetEntry{{ID: "a", Name: "A", Intent: "Do A", Size: "M", SizeReason: "Storage | Unicode 日本語"}}); err != nil {
					t.Fatal(err)
				}
				if _, err := db.SavePlanWorkflowState(ctx, session.TaskRef, 0, state); err != nil {
					t.Fatal(err)
				}
			}
			state, revision, err := db.GetPlanWorkflowState(ctx, session.TaskRef)
			if err != nil {
				t.Fatal(err)
			}
			if state.StorySet.SizingApplicable() == legacy || planbrief.PlanStorySetRevision(state.StorySet.Entries) != state.StorySet.Revision {
				t.Fatalf("loaded set lost creation policy or historical revision: %+v", state.StorySet)
			}
			if _, err = state.PromoteStorySet(state.StorySet.Revision); err != nil {
				t.Fatal(err)
			}
			if _, err = db.SavePlanWorkflowState(ctx, session.TaskRef, revision, state); err != nil {
				t.Fatal(err)
			}
			before := state.StorySet.Clone()
			if err = db.ApproveManagedStories(ctx, session.TaskRef, 1, approvals); err != nil {
				t.Fatal(err)
			}
			if err = db.ReplanManagedFeature(ctx, session.TaskRef, 1); err != nil {
				t.Fatal(err)
			}
			state, revision, err = db.GetPlanWorkflowState(ctx, session.TaskRef)
			if err != nil || !reflect.DeepEqual(state.StorySet, before) {
				t.Fatalf("replanning changed saved set: %+v, %v", state.StorySet, err)
			}
			state.SharedRevision = "changed after replan"
			if _, err = db.SavePlanWorkflowState(ctx, session.TaskRef, revision, state); err != nil {
				t.Fatal(err)
			}
			again, _, err := db.GetPlanWorkflowState(ctx, session.TaskRef)
			if err != nil || !reflect.DeepEqual(again.StorySet, before) {
				t.Fatalf("save after replan changed saved set: %+v, %v", again.StorySet, err)
			}
		})
	}
}
