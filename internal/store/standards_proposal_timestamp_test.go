package store

import (
	"reflect"
	"testing"
	"time"
)

func TestStandardsProposalEditUsesFreshStoreTimestamp(t *testing.T) {
	db := newTestDB(t)
	stale := time.Date(2001, time.January, 1, 0, 0, 0, 0, time.UTC)
	initial, err := db.SaveStandardsProposal(t.Context(), StandardsProposalVersion{ProposalID: "timestamped", SchemaVersion: 1, ChangeDigest: "original-change", ResultDigest: "original-result", Author: "reviewer", Operations: []StandardsOperation{}, CreatedAt: stale}, 0)
	if err != nil {
		t.Fatal(err)
	}
	fetched, err := db.GetStandardsProposal(t.Context(), initial.ProposalID, initial.Version)
	if err != nil {
		t.Fatal(err)
	}
	if !fetched.CreatedAt.After(stale) {
		t.Fatalf("caller owned initial timestamp: %s", fetched.CreatedAt)
	}
	fetched.CreatedAt = stale
	fetched.ChangeDigest = "edited-change"
	fetched.ResultDigest = "edited-result"
	earliest := nowUTC().Truncate(time.Millisecond)
	edited, err := db.SaveStandardsProposal(t.Context(), fetched, initial.Version)
	if err != nil {
		t.Fatal(err)
	}
	if edited.Version != 2 || edited.CreatedAt.Before(earliest) || edited.CreatedAt.Equal(stale) {
		t.Fatalf("edit reused caller timestamp: %#v", edited)
	}
	unchanged, err := db.GetStandardsProposal(t.Context(), initial.ProposalID, initial.Version)
	if err != nil || !reflect.DeepEqual(initial, unchanged) {
		t.Fatalf("edit changed immutable original version: %#v %v", unchanged, err)
	}
	saved, err := db.GetStandardsProposal(t.Context(), edited.ProposalID, edited.Version)
	if err != nil || !saved.CreatedAt.Equal(edited.CreatedAt) {
		t.Fatalf("fresh timestamp not retained: %#v %v", saved, err)
	}
}
