package store

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func TestStoryRefinementACKClaimsOnceAndPreservesPreparedBytes(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	repo := testRepo(t, db, "owner", "repo")
	session, err := db.GetOrCreateSession(t.Context(), repo, 7)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AdmitStoryRefinement(t.Context(), StoryRefinement{SessionID: session.ID, CommentID: 41, ChildNumber: 8, ChildID: 800, Generation: 1, ActorID: 42, Body: "refine"}); err != nil {
		t.Fatal(err)
	}
	first, err := db.PrepareStoryRefinementACK(t.Context(), session.ID, 41, "original redacted body")
	if err != nil {
		t.Fatal(err)
	}
	second, err := db.PrepareStoryRefinementACK(t.Context(), session.ID, 41, "changed mutable renderer")
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatal("prepared bytes changed on replay")
	}
	claimed, err := db.ClaimStoryRefinementACK(t.Context(), session.ID, 41, "changed mutable renderer")
	if err != nil || claimed {
		t.Fatal("different body claimed dispatch")
	}
	claimed, err = db.ClaimStoryRefinementACK(t.Context(), session.ID, 41, first.PreparedBody)
	if err != nil || !claimed {
		t.Fatal("initial dispatch failed")
	}
	claimed, err = db.ClaimStoryRefinementACK(t.Context(), session.ID, 41, first.PreparedBody)
	if err != nil || claimed {
		t.Fatal("retry reclaimed dispatch")
	}
	pending, err := db.GetStoryRefinementACK(t.Context(), session.ID, 41)
	if err != nil || !pending.DispatchAttempted || pending.AcknowledgementID != 0 || pending.PreparedBody != first.PreparedBody {
		t.Fatal("uncertain intent lost")
	}
	if err = db.AcknowledgeStoryRefinement(t.Context(), session.ID, 41, 900); err != nil {
		t.Fatal(err)
	}
	ack, err := db.StoryRefinementAcknowledgement(t.Context(), session.ID, 41)
	if err != nil || ack != 900 {
		t.Fatal("pending receipt did not confirm")
	}
	source, err := db.GetStoryRefinement(t.Context(), session.ID, 41)
	if err != nil || source.Consumed {
		t.Fatal("ack consumed original source")
	}
	if _, err = db.PrepareStoryRefinementACK(t.Context(), session.ID, 99, "bad"); err == nil {
		t.Fatal("unadmitted source acquired ACK")
	}
	if _, err = db.GetStoryRefinementACK(t.Context(), session.ID, 99); !errors.Is(err, ErrNotFound) {
		t.Fatal("bad admission left pending intent")
	}
}

func TestStoryRefinementACKMigrationPreservesLegacyConfirmedReceipt(t *testing.T) {
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	var prior []migration
	for _, m := range migrations {
		if m.name != "story_ack_dispatch" {
			prior = append(prior, m)
		}
	}
	if err = db.migrateAll(t.Context(), prior); err != nil {
		t.Fatal(err)
	}
	repo := testRepo(t, db, "owner", "repo")
	session, err := db.GetOrCreateSession(t.Context(), repo, 7)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AdmitStoryRefinement(t.Context(), StoryRefinement{SessionID: session.ID, CommentID: 41, ChildNumber: 8, ChildID: 800, Generation: 1, ActorID: 42, Body: "refine"}); err != nil {
		t.Fatal(err)
	}
	if err = db.AdmitStoryRefinement(t.Context(), StoryRefinement{SessionID: session.ID, CommentID: 42, ChildNumber: 8, ChildID: 800, Generation: 1, ActorID: 42, Body: "possibly sent legacy ACK"}); err != nil {
		t.Fatal(err)
	}
	if err = db.AcknowledgeStoryRefinement(t.Context(), session.ID, 41, 900); err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	pending, err := db.GetStoryRefinementACK(t.Context(), session.ID, 42)
	if err != nil || !pending.DispatchAttempted || pending.PreparedBody != "" || pending.AcknowledgementID != 0 {
		t.Fatalf("legacy dispatch ambiguity lost: %+v %v", pending, err)
	}
	attempt, err := db.PrepareStoryRefinementACK(t.Context(), session.ID, 42, "current renderer cannot reconstruct original bytes")
	if err != nil || !reflect.DeepEqual(pending, attempt) {
		t.Fatal("legacy uncertain ACK gained new prepared bytes")
	}
	claimed, err := db.ClaimStoryRefinementACK(t.Context(), session.ID, 42, "current renderer cannot reconstruct original bytes")
	if err != nil || claimed {
		t.Fatal("legacy pending admission acquired unauthorized dispatch")
	}
	source, err := db.GetStoryRefinement(t.Context(), session.ID, 42)
	if err != nil || source.Consumed || source.Body != "possibly sent legacy ACK" {
		t.Fatal("legacy pending source changed or advanced")
	}
	legacy, err := db.GetStoryRefinementACK(t.Context(), session.ID, 41)
	if err != nil || legacy.AcknowledgementID != 900 || legacy.PreparedBody != "" || legacy.DispatchAttempted {
		t.Fatalf("legacy receipt changed during migration: %+v %v", legacy, err)
	}
	replayed, err := db.PrepareStoryRefinementACK(t.Context(), session.ID, 41, "new renderer bytes")
	if err != nil || !reflect.DeepEqual(legacy, replayed) {
		t.Fatal("confirmed legacy acknowledgement regenerated intent")
	}
	if err = db.AcknowledgeStoryRefinement(t.Context(), session.ID, 41, 901); err != nil {
		t.Fatal(err)
	}
	retained, err := db.GetStoryRefinementACK(t.Context(), session.ID, 41)
	if err != nil || !reflect.DeepEqual(legacy, retained) {
		t.Fatal("legacy confirmed receipt was replaced")
	}
}
