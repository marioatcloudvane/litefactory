package store

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

func sizingFixture(t *testing.T) (*DB, Session, []ApprovedStoryRevision, planbrief.PlanState, int64) {
	t.Helper()
	db, session, approvals := approvedFixture(t)
	ctx := t.Context()
	if err := db.BindFeatureContract(ctx, FeatureContract{Parent: session.TaskRef, Generation: 1, ContentVersion: 2, SubmissionVersion: 2}); err != nil {
		t.Fatal(err)
	}
	state := planbrief.PlanState{SharedRevision: "shared", StorySet: &planbrief.PlanStorySetState{Revision: "set", Promoted: true, PromotedRevision: "set", Keys: map[string]string{"entry": "US-1"}, Entries: []planbrief.PlanStorySetEntry{{ID: "entry", Size: "M", SizeReason: "Several parts"}}}, Stories: []planbrief.PlanStoryState{{Key: "US-1"}, {Key: "US-2"}}}
	state.StorySet.Revision = planbrief.PlanStorySetRevision(state.StorySet.Entries)
	state.StorySet.PromotedRevision = state.StorySet.Revision
	state.AssembledApproval = state.PlanSetRevision()
	state.PublishedRevision = state.AssembledApproval
	state.SavedRevision = state.AssembledApproval
	revision, err := db.SavePlanWorkflowState(ctx, session.TaskRef, 0, state)
	if err != nil {
		t.Fatal(err)
	}
	for i := range approvals {
		a := &approvals[i]
		a.Hash = submissionTestDigest([]byte(a.Title + "\x00" + a.Body))
		a.BodyHash = a.Hash
		if err = db.PutManagedStory(ctx, a.ManagedStory); err != nil {
			t.Fatal(err)
		}
		if err = db.PutSubmissionProjection(ctx, SubmissionProjection{Parent: session.TaskRef, Generation: 1, Story: a.Key, IssueID: a.IssueID, Revision: a.Hash, Title: a.Title, Body: a.Body, ContentVersion: 2, Content: json.RawMessage(`{}`)}); err != nil {
			t.Fatal(err)
		}
		if _, err = db.ReservePlanStory(ctx, session.TaskRef, a.Key); err != nil {
			t.Fatal(err)
		}
		if _, err = db.sql.ExecContext(ctx, `UPDATE plan_story_drafts SET issue_id=?,issue_number=?,revision=? WHERE parent=? AND story_key=?`, a.IssueID, a.IssueNumber, a.Hash, session.TaskRef, a.Key); err != nil {
			t.Fatal(err)
		}
	}
	return db, session, approvals, state, revision
}

func TestPublishedStorySizingFrozenAndReplay(t *testing.T) {
	db, s, as, state, revision := sizingFixture(t)
	ctx := t.Context()
	a := as[0]
	retain := func() error {
		return db.RetainPublishedStorySizing(ctx, s.TaskRef, 1, a.Key, a.IssueID, a.Hash, revision)
	}
	if err := retain(); err != nil {
		t.Fatal(err)
	}
	if err := retain(); err != nil {
		t.Fatalf("identical replay: %v", err)
	}
	if err := db.ApproveManagedStories(ctx, s.TaskRef, 1, as); err != nil {
		t.Fatal(err)
	}
	if err := retain(); err != nil {
		t.Fatalf("frozen identical replay: %v", err)
	}
	got, err := db.GetApprovedStorySizing(ctx, s.TaskRef, 1, a.Key, a.Hash)
	if err != nil || got == nil || !got.Available || got.Size != "M" || got.EntryID != "entry" {
		t.Fatalf("facts=%+v err=%v", got, err)
	}
	state.StorySet.Entries[0].Size = "L"
	if _, err = db.SavePlanWorkflowState(ctx, s.TaskRef, revision, state); err != nil {
		t.Fatal(err)
	}
	again, err := db.GetApprovedStorySizing(ctx, s.TaskRef, 1, a.Key, a.Hash)
	if err != nil || !reflect.DeepEqual(got, again) {
		t.Fatalf("replay changed: %+v %v", again, err)
	}
	if _, err = db.GetApprovedStorySizing(ctx, s.TaskRef, 1, a.Key, "other"); err == nil {
		t.Fatal("digest mismatch accepted")
	}
	if _, err = db.GetApprovedStorySizing(ctx, s.TaskRef, 2, a.Key, a.Hash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("generation mismatch: %v", err)
	}
	absent, err := db.GetApprovedStorySizing(ctx, s.TaskRef, 1, as[1].Key, as[1].Hash)
	if err != nil || absent != nil {
		t.Fatalf("old absence: %+v %v", absent, err)
	}
}

func TestPublishedStorySizingRevisionAndIdentityGuards(t *testing.T) {
	for _, name := range []string{"stale revision", "wrong issue", "wrong projection", "wrong generation", "assembled changed", "immutable conflict", "unproved mapping", "Plan changed before freeze", "Sizing changed before freeze"} {
		t.Run(name, func(t *testing.T) {
			db, s, as, state, revision := sizingFixture(t)
			ctx := t.Context()
			a := as[0]
			key, issue, projection, generation := a.Key, a.IssueID, a.Hash, 1
			wantError := true
			switch name {
			case "stale revision":
				revision++
			case "wrong issue":
				issue++
				wantError = false
			case "wrong projection":
				projection = "other"
				wantError = false
			case "wrong generation":
				generation++
				wantError = false
			case "assembled changed":
				state.AssembledApproval = "changed"
			case "immutable conflict":
				if err := db.RetainPublishedStorySizing(ctx, s.TaskRef, 1, key, issue, projection, revision); err != nil {
					t.Fatal(err)
				}
				state.StorySet.Entries[0].Size = "L"
				state.StorySet.Revision = planbrief.PlanStorySetRevision(state.StorySet.Entries)
				state.StorySet.PromotedRevision = state.StorySet.Revision
				state.AssembledApproval = state.PlanSetRevision()
			case "unproved mapping":
				state.StorySet.Keys = map[string]string{}
				state.AssembledApproval = state.PlanSetRevision()
				state.PublishedRevision = state.AssembledApproval
				state.SavedRevision = state.AssembledApproval
				wantError = false
			case "Plan changed before freeze", "Sizing changed before freeze":
				wantError = false
			}
			if name == "assembled changed" || name == "immutable conflict" || name == "unproved mapping" {
				var err error
				revision, err = db.SavePlanWorkflowState(ctx, s.TaskRef, revision, state)
				if err != nil {
					t.Fatal(err)
				}
			}
			err := db.RetainPublishedStorySizing(ctx, s.TaskRef, generation, key, issue, projection, revision)
			if (err != nil) != wantError {
				t.Fatalf("retain error=%v", err)
			}
			if !wantError {
				if generation != 1 || projection != a.Hash {
					return
				}
				if name == "Plan changed before freeze" || name == "Sizing changed before freeze" {
					if name == "Plan changed before freeze" {
						state.SharedRevision = "new plan"
					} else {
						state.StorySet.Entries[0].Size = "L"
						state.StorySet.Revision = planbrief.PlanStorySetRevision(state.StorySet.Entries)
						state.StorySet.PromotedRevision = state.StorySet.Revision
					}
					if _, err = db.SavePlanWorkflowState(ctx, s.TaskRef, revision, state); err != nil {
						t.Fatal(err)
					}
				}
				if err = db.ApproveManagedStories(ctx, s.TaskRef, 1, as); err != nil {
					t.Fatal(err)
				}
				facts, err := db.GetApprovedStorySizing(ctx, s.TaskRef, 1, key, projection)
				if err != nil || facts == nil || facts.Available {
					t.Fatalf("unavailable facts=%+v err=%v", facts, err)
				}
			}
		})
	}
}

func TestApprovedStorySizingReadFailures(t *testing.T) {
	db, s, as, _, revision := sizingFixture(t)
	ctx := t.Context()
	a := as[0]
	if err := db.RetainPublishedStorySizing(ctx, s.TaskRef, 1, a.Key, a.IssueID, a.Hash, revision); err != nil {
		t.Fatal(err)
	}
	if err := db.ApproveManagedStories(ctx, s.TaskRef, 1, as); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := db.GetApprovedStorySizing(canceled, s.TaskRef, 1, a.Key, a.Hash); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	if _, err := db.sql.ExecContext(ctx, `UPDATE approved_story_revisions SET sizing_json='{' WHERE parent_task=?`, s.TaskRef); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetApprovedStorySizing(ctx, s.TaskRef, 1, a.Key, a.Hash); err == nil {
		t.Fatal("malformed facts hidden")
	}
	if err := db.sql.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetApprovedStorySizing(ctx, s.TaskRef, 1, a.Key, a.Hash); err == nil {
		t.Fatal("closed database hidden")
	}
}

func TestStorySizingFreezeRollsBackAllStoriesOnReadError(t *testing.T) {
	db, s, as, _, revision := sizingFixture(t)
	for _, a := range as {
		if err := db.RetainPublishedStorySizing(t.Context(), s.TaskRef, 1, a.Key, a.IssueID, a.Hash, revision); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.sql.ExecContext(t.Context(), `UPDATE published_story_sizing SET facts_json='{' WHERE story_key=?`, as[1].Key); err != nil {
		t.Fatal(err)
	}
	if err := db.ApproveManagedStories(t.Context(), s.TaskRef, 1, as); err == nil {
		t.Fatal("malformed publication accepted")
	}
	revisions, err := db.ListApprovedStoryRevisions(t.Context(), s.TaskRef, 1)
	if err != nil || len(revisions) != 0 {
		t.Fatalf("partial approval persisted: %+v %v", revisions, err)
	}
	feature, err := db.GetManagedFeature(t.Context(), s.TaskRef)
	if err != nil || feature.State != "planning" || !feature.Ready {
		t.Fatalf("freeze did not roll back: %+v %v", feature, err)
	}
}

func TestPublishedStorySizingConcurrentPlanRevisionGuard(t *testing.T) {
	db, s, as, state, revision := sizingFixture(t)
	// The writer completes after the publisher obtained its revision and before
	// its transaction starts; a stale captured revision cannot borrow new facts.
	changed := make(chan error, 1)
	go func() {
		state.StorySet.Entries[0].Size = "L"
		_, err := db.SavePlanWorkflowState(t.Context(), s.TaskRef, revision, state)
		changed <- err
	}()
	if err := <-changed; err != nil {
		t.Fatal(err)
	}
	a := as[0]
	if err := db.RetainPublishedStorySizing(t.Context(), s.TaskRef, 1, a.Key, a.IssueID, a.Hash, revision); !errors.Is(err, ErrPlanStateConflict) {
		t.Fatalf("stale publication: %v", err)
	}
	var count int
	if err := db.sql.QueryRowContext(t.Context(), `SELECT count(*) FROM published_story_sizing`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("stale facts persisted: %d %v", count, err)
	}
}

func TestPublishedStorySizingReadFailures(t *testing.T) {
	db, s, as, _, revision := sizingFixture(t)
	a := as[0]
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := db.RetainPublishedStorySizing(canceled, s.TaskRef, 1, a.Key, a.IssueID, a.Hash, revision); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	if _, err := db.sql.ExecContext(t.Context(), `UPDATE plan_workflow_states SET state_json='{' WHERE parent=?`, s.TaskRef); err != nil {
		t.Fatal(err)
	}
	if err := db.RetainPublishedStorySizing(t.Context(), s.TaskRef, 1, a.Key, a.IssueID, a.Hash, revision); err == nil {
		t.Fatal("malformed Plan hidden")
	}
}

func TestStorySizingMalformedSourceMetadataUnavailable(t *testing.T) {
	for _, stage := range []string{"publication", "approved"} {
		for _, field := range []string{"version", "plan_revision", "set_revision", "entry_id", "null"} {
			t.Run(stage+"/"+field, func(t *testing.T) {
				db, s, as, _, revision := sizingFixture(t)
				a := as[0]
				ctx := t.Context()
				if err := db.RetainPublishedStorySizing(ctx, s.TaskRef, 1, a.Key, a.IssueID, a.Hash, revision); err != nil {
					t.Fatal(err)
				}
				var original string
				if err := db.sql.QueryRowContext(ctx, `SELECT facts_json FROM published_story_sizing WHERE story_key=?`, a.Key).Scan(&original); err != nil {
					t.Fatal(err)
				}
				var facts ApprovedStorySizing
				if err := json.Unmarshal([]byte(original), &facts); err != nil {
					t.Fatal(err)
				}
				switch field {
				case "version":
					facts.Version = 99
				case "plan_revision":
					facts.PlanRevision = " \t"
				case "set_revision":
					facts.SetRevision = ""
				case "entry_id":
					facts.EntryID = ""
				}
				raw, err := json.Marshal(facts)
				if err != nil {
					t.Fatal(err)
				}
				if field == "null" {
					raw = []byte("null")
				}
				if stage == "publication" {
					if _, err = db.sql.ExecContext(ctx, `UPDATE published_story_sizing SET facts_json=? WHERE story_key=?`, string(raw), a.Key); err != nil {
						t.Fatal(err)
					}
				}
				if err = db.ApproveManagedStories(ctx, s.TaskRef, 1, as); err != nil {
					t.Fatal(err)
				}
				if stage == "approved" {
					if _, err = db.sql.ExecContext(ctx, `UPDATE approved_story_revisions SET sizing_json=? WHERE story_key=?`, string(raw), a.Key); err != nil {
						t.Fatal(err)
					}
				}
				got, err := db.GetApprovedStorySizing(ctx, s.TaskRef, 1, a.Key, a.Hash)
				if err != nil || got == nil || got.Available || got.Explanation == "" {
					t.Fatalf("malformed source facts=%+v err=%v", got, err)
				}
			})
		}
	}
}
