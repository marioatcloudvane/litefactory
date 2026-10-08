package store

import (
	"fmt"
	"testing"

	"github.com/cloudvane/litefactory/internal/naming"
)

func TestDoctorTypedEvidenceIncludesOnlyIssueOwnedLedger(t *testing.T) {
	db := newTestDB(t)
	selected := testSession(t, db, 651)
	foreign := testSession(t, db, 652)
	ctx := t.Context()
	for _, session := range []Session{selected, foreign} {
		if err := db.EnsureImplementStories(ctx, session.TaskRef, 1, []ImplementStorySeed{{Key: "one", Branch: "lf/one"}}, implementT0); err != nil {
			t.Fatal(err)
		}
		if _, err := db.TransitionImplementStory(ctx, ImplementTransition{Parent: session.TaskRef, Generation: 1, Key: "one", From: ImplementWaiting, To: ImplementRunning, ExpectedRevision: 1, At: implementT0}); err != nil {
			t.Fatal(err)
		}
	}
	var selectedID, foreignID int64
	if err := db.sql.QueryRowContext(ctx, `SELECT rowid FROM implement_stories WHERE parent=?`, selected.TaskRef).Scan(&selectedID); err != nil {
		t.Fatal(err)
	}
	if err := db.sql.QueryRowContext(ctx, `SELECT rowid FROM implement_stories WHERE parent=?`, foreign.TaskRef).Scan(&foreignID); err != nil {
		t.Fatal(err)
	}
	ref, err := naming.ParseRef(selected.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := db.ReadDoctorEvidence(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, record := range evidence.Records {
		if record.Kind != "story" {
			continue
		}
		if record.ID == fmt.Sprint(foreignID) {
			t.Fatal("foreign ledger leaked", record)
		}
		if record.ID == fmt.Sprint(selectedID) {
			found = true
			if record.State != ImplementRunning || record.CapturedAt.IsZero() {
				t.Fatal("incorrect typed evidence", record)
			}
		}
	}
	if !found {
		t.Fatal("selected ledger missing numeric repair target", evidence.Records)
	}
}

func TestDoctorTypedEvidenceManagedChildKeepsLegacyProjection(t *testing.T) {
	db := newTestDB(t)
	parent := testSession(t, db, 653)
	ctx := t.Context()
	repo, err := db.GetRepo(ctx, parent.RepoID)
	if err != nil {
		t.Fatal(err)
	}
	child, err := db.GetOrCreateSession(ctx, repo, 654)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.PutManagedFeature(ctx, ManagedFeature{ParentTask: parent.TaskRef, Generation: 1, State: "planning"}); err != nil {
		t.Fatal(err)
	}
	if err := db.PutManagedStory(ctx, ManagedStory{ParentTask: parent.TaskRef, Generation: 1, Key: "one", IssueNumber: 654, IssueID: 6540, BodyHash: "body", State: "planning"}); err != nil {
		t.Fatal(err)
	}
	if err := db.EnsureImplementStories(ctx, parent.TaskRef, 1, []ImplementStorySeed{{Key: "one", Branch: "lf/one"}, {Key: "sibling", Position: 1, Branch: "lf/sibling"}}, implementT0); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ReservePlanStory(ctx, parent.TaskRef, "one"); err != nil {
		t.Fatal(err)
	}
	if err := db.BindPlanStoryIssue(ctx, parent.TaskRef, "one", 6540, 654); err != nil {
		t.Fatal(err)
	}
	var ownedID, siblingID int64
	if err := db.sql.QueryRowContext(ctx, `SELECT rowid FROM implement_stories WHERE parent=? AND story_key='one'`, parent.TaskRef).Scan(&ownedID); err != nil {
		t.Fatal(err)
	}
	if err := db.sql.QueryRowContext(ctx, `SELECT rowid FROM implement_stories WHERE parent=? AND story_key='sibling'`, parent.TaskRef).Scan(&siblingID); err != nil {
		t.Fatal(err)
	}
	ref, err := naming.ParseRef(child.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := db.ReadDoctorEvidence(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	typed, legacy := false, false
	for _, record := range evidence.Records {
		if record.Kind != "story" {
			continue
		}
		if record.ID == fmt.Sprint(siblingID) {
			t.Fatal("sibling ledger leaked", record)
		}
		if record.ID == fmt.Sprint(ownedID) && record.State == ImplementWaiting {
			typed = true
		}
		if record.ID == "one" && record.State == "story" {
			legacy = true
		}
	}
	if !typed || !legacy {
		t.Fatal("typed or legacy child projection missing", evidence.Records)
	}
}
