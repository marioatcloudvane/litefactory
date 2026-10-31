package store

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

func TestStageDecisionMigrationPreservesPendingProposalAndApprovalBindings(t *testing.T) {
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "migration.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if e := db.Close(); e != nil {
			t.Error(e)
		}
	})
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	var prior, additive []migration
	for _, m := range migrations {
		if strings.HasPrefix(m.name, "stage_decision") {
			additive = append(additive, m)
		} else {
			prior = append(prior, m)
		}
	}
	if len(additive) < 1 {
		t.Fatal("decision migration absent")
	}
	if err = db.migrateAll(t.Context(), prior); err != nil {
		t.Fatal(err)
	}
	owner := connectorTestOwner(t, db, 42)
	at := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	proposal := OwnerAction{ID: "existing-pending-id", PolicyVersion: 1, Principal: OwnerActionPrincipal{OwnerID: "42", OwnerGeneration: owner.Generation, Transport: "discord", ChannelID: "200", DiscordOwnerID: "100"}, RequestKey: "existing-request", RequestDigest: "existing-digest", Facts: `{"target_revision":"historical-revision"}`, Payload: `{"exact":"historical reviewed text"}`, Digest: "historical-preview", Scope: "standards:management", Tier: "conversational", State: "awaiting_confirmation", CreatedAt: at, AvailableAt: at, ConfirmNotBefore: at.Add(5 * time.Second), ExpiresAt: at.Add(15 * time.Minute), ExecutionID: "owner-action:existing-pending-id"}
	if err = db.CreateOwnerAction(t.Context(), proposal); err != nil {
		t.Fatal(err)
	}
	oldProposal, err := db.ReadOwnerAction(t.Context(), proposal.ID)
	if err != nil {
		t.Fatal(err)
	}
	session := testSession(t, db, 1)
	historical := planbrief.PlanState{SharedRevision: "retained-shared", ConsumedReplies: []int64{31}, Stories: []planbrief.PlanStoryState{{Key: "story-001", Stage: "technical", Candidate: "historical-candidate", Approvals: map[string]string{"story": "retained-binding"}, Draft: planbrief.PlanDraft{Key: "story-001", Name: "Existing story"}}}}
	if _, err = db.SavePlanWorkflowState(t.Context(), session.TaskRef, 0, historical); err != nil {
		t.Fatal(err)
	}
	oldPlan, oldRevision, err := db.GetPlanWorkflowState(t.Context(), session.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range additive {
		if err = db.applyMigration(t.Context(), m); err != nil {
			t.Fatal(err)
		}
	}
	current, err := db.ReadOwnerAction(t.Context(), proposal.ID)
	if err != nil || !reflect.DeepEqual(current, oldProposal) {
		t.Fatalf("pending proposal changed: %+v %v", current, err)
	}
	plan, revision, err := db.GetPlanWorkflowState(t.Context(), session.TaskRef)
	if err != nil || revision != oldRevision || !reflect.DeepEqual(plan, oldPlan) {
		t.Fatalf("historical approval changed: %+v %v", plan, err)
	}
}
