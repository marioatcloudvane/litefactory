package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func upgradeStoreFixture(t *testing.T) (*DB, Session, ContractUpgrade, []ApprovedStoryRevision) {
	t.Helper()
	db, session, revisions := approvedFixture(t)
	if err := db.BindFeatureContract(t.Context(), FeatureContract{Parent: session.TaskRef, Generation: 1, ContentVersion: 1, SubmissionVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if err := db.ApproveManagedStories(t.Context(), session.TaskRef, 1, revisions); err != nil {
		t.Fatal(err)
	}
	done := revisions[0].ManagedStory
	done.State = "completed"
	if err := db.CompareManagedStory(t.Context(), done, "frozen"); err != nil {
		t.Fatal(err)
	}
	if err := db.ReplanManagedFeature(t.Context(), session.TaskRef, 1); err != nil {
		t.Fatal(err)
	}
	if err := db.BindFeatureContract(t.Context(), FeatureContract{Parent: session.TaskRef, Generation: 2, ContentVersion: 1, SubmissionVersion: 1}); err != nil {
		t.Fatal(err)
	}
	item := ContractUpgradeItem{Story: revisions[1].Key, IssueID: revisions[1].IssueID, IssueNumber: revisions[1].IssueNumber, Title: "Current story", SourceBody: "Legacy content", Body: "Typed content", Content: json.RawMessage(`{"acceptance_criteria":[{"id":"AC-001","text":"Requirement"}]}`), Criteria: []string{"AC-001"}, Dependencies: []string{revisions[0].Key}}
	item.SourceRevision = submissionTestDigest([]byte(item.Title + "\x00" + item.SourceBody))
	item.Revision = submissionTestDigest([]byte(item.Title + "\x00" + item.Body))
	u := ContractUpgrade{Parent: session.TaskRef, SourceGeneration: 2, TargetGeneration: 3, OwnerID: 42, State: "pending", Items: []ContractUpgradeItem{item}}
	upgradeStoreHash(t, &u)
	return db, session, u, revisions
}

func upgradeStoreHash(t *testing.T, u *ContractUpgrade) {
	t.Helper()
	u.ScopeHash = ""
	data, err := json.Marshal(u)
	if err != nil {
		t.Fatal(err)
	}
	u.ScopeHash = submissionTestDigest(data)
}

func TestContractUpgradeAtomicGenerationPreservesHistoryAndLocksAdmission(t *testing.T) {
	t.Parallel()
	db, session, u, original := upgradeStoreFixture(t)
	before, err := db.ListApprovedStoryRevisions(t.Context(), u.Parent, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.BeginContractUpgrade(t.Context(), u); err != nil {
		t.Fatal(err)
	}
	if err := db.BeginContractUpgrade(t.Context(), u); err != nil {
		t.Fatal(err)
	}
	if err := db.SetManagedFeatureReady(t.Context(), u.Parent, 2, true); err == nil {
		t.Fatal("pending upgrade allowed readiness mutation")
	}
	if err := db.CompareManagedFeature(t.Context(), ManagedFeature{ParentTask: u.Parent, Generation: 2, State: "implementing"}, "planning"); err == nil {
		t.Fatal("pending upgrade allowed mode admission")
	}
	stories, err := db.ListManagedStories(t.Context(), u.Parent, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.PutManagedStory(t.Context(), stories[1]); err == nil {
		t.Fatal("pending upgrade allowed story mutation")
	}
	segment, err := db.GetOrCreateSegment(t.Context(), session.ID, FirstSegmentOrdinal, "upgrade-test", AgentCodex)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.AllocateRun(t.Context(), AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: "plan", Agent: AgentCodex, TriggerCommentID: 99, LogPath: func(int) string { return "/logs/upgrade.jsonl" }})
	if err == nil || !strings.Contains(err.Error(), "upgrade") {
		t.Fatalf("pending upgrade admitted agent run: %v", err)
	}
	changed := u
	changed.OwnerID++
	upgradeStoreHash(t, &changed)
	if err := db.BeginContractUpgrade(t.Context(), changed); err == nil {
		t.Fatal("retained intent was replaced")
	}
	if err := db.FinishContractUpgrade(t.Context(), u.Parent, u.ScopeHash); err == nil {
		t.Fatal("unconfirmed issues selected typed contract")
	}
	if err := db.ConfirmContractUpgradeItem(t.Context(), u.Parent, u.ScopeHash, u.Items[0].Story); err != nil {
		t.Fatal(err)
	}
	if err := db.FinishContractUpgrade(t.Context(), u.Parent, u.ScopeHash); err != nil {
		t.Fatal(err)
	}
	if err := db.FinishContractUpgrade(t.Context(), u.Parent, u.ScopeHash); err != nil {
		t.Fatal(err)
	}
	feature, err := db.GetManagedFeature(t.Context(), u.Parent)
	if err != nil || feature.Generation != 3 || feature.Ready || feature.State != "planning" || feature.WorkflowVersion != WorkflowSeparated {
		t.Fatalf("unexpected upgraded feature: %+v %v", feature, err)
	}
	for _, generation := range []int{1, 2, 3} {
		c, err := db.GetGenerationContract(t.Context(), u.Parent, generation)
		if err != nil {
			t.Fatal(err)
		}
		want := 1
		if generation == 3 {
			want = 2
		}
		if c.ContentVersion != want || c.SubmissionVersion != want {
			t.Fatalf("generation %d contract changed: %+v", generation, c)
		}
	}
	after, err := db.ListApprovedStoryRevisions(t.Context(), u.Parent, 1)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("historical frozen evidence changed: %+v %v", after, err)
	}
	current, err := db.ListManagedStories(t.Context(), u.Parent, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(current) != 2 || current[0].State != "completed" || current[0].BodyHash != original[0].Hash || current[1].State != "planning" || current[1].EngineeringComplete {
		t.Fatalf("completed or unfinished identity changed: %+v", current)
	}
	if _, err := db.GetSubmissionProjection(t.Context(), u.Parent, 3, current[0].Key, current[0].BodyHash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("completed legacy story acquired typed requirements: %v", err)
	}
	if err := db.ApproveManagedStories(t.Context(), u.Parent, 3, nil); err == nil {
		t.Fatal("upgrade silently granted next approval")
	}
}

func TestContractUpgradeSafetyRefusesWithoutChangingLegacyScope(t *testing.T) {
	for _, testCase := range []string{"active run", "frozen", "mixed contract", "wrong generation", "partial cohort", "legacy effect"} {
		t.Run(testCase, func(t *testing.T) {
			db, session, u, _ := upgradeStoreFixture(t)
			switch testCase {
			case "active run":
				testRun(t, db, session.ID, 77)
			case "frozen":
				if err := db.CompareManagedFeature(t.Context(), ManagedFeature{ParentTask: u.Parent, Generation: 2, State: "freezing"}, "planning"); err != nil {
					t.Fatal(err)
				}
			case "mixed contract":
				if _, err := db.sql.ExecContext(t.Context(), `UPDATE managed_features SET submission_version=2 WHERE parent_task=?`, u.Parent); err != nil {
					t.Fatal(err)
				}
			case "wrong generation":
				u.SourceGeneration = 1
				u.TargetGeneration = 2
				upgradeStoreHash(t, &u)
			case "partial cohort":
				u.Items = nil
				upgradeStoreHash(t, &u)
			case "legacy effect":
				run := testRun(t, db, session.ID, 78)
				if _, err := db.sql.ExecContext(t.Context(), `UPDATE runs SET status='failed' WHERE id=?`, run.ID); err != nil {
					t.Fatal(err)
				}
				if _, err := db.sql.ExecContext(t.Context(), `INSERT INTO story_progress_runs(run_id,parent_task,generation,worktree,active) VALUES(?,?,1,'/historical',0)`, run.ID, u.Parent); err != nil {
					t.Fatal(err)
				}
				if err := db.withTx(t.Context(), func(tx *sql.Tx) error {
					return insertStoryEffect(t.Context(), tx, StoryProgressEffect{RunID: run.ID, Sequence: 1, Parent: u.Parent, Generation: 1, Story: "US-1", IssueNumber: 200, IssueID: 2000, ParentID: 1230, Revision: "approved", Kind: "verified", Detail: "historical legacy effect"})
				}); err != nil {
					t.Fatal(err)
				}
			}
			before, err := db.GetManagedFeature(t.Context(), u.Parent)
			if err != nil {
				t.Fatal(err)
			}
			if err := db.BeginContractUpgrade(t.Context(), u); err == nil {
				t.Fatalf("%s accepted", testCase)
			}
			after, err := db.GetManagedFeature(t.Context(), u.Parent)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("refusal changed feature: %+v %v", after, err)
			}
			if _, err := db.GetContractUpgrade(t.Context(), u.Parent); !errors.Is(err, ErrNotFound) {
				t.Fatalf("refusal left pending intent: %v", err)
			}
			if testCase == "legacy effect" {
				effects, err := db.ListStoryProgressEffects(t.Context())
				if err != nil || len(effects) != 1 || effects[0].Detail != "historical legacy effect" {
					t.Fatalf("legacy effect changed: %+v %v", effects, err)
				}
			}
		})
	}
}
