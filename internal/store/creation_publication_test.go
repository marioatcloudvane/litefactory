package store

import (
	"errors"
	"testing"
)

func TestCreationPublicationDBDispatchAndMonotonicFacts(t *testing.T) {
	f := newCreationDBFixture(t)
	lease, proposal, _ := f.approved(t, "api", 273, 2)
	reservation, err := f.c.ReserveCreation(t.Context(), lease, proposal)
	if err != nil {
		t.Fatal(err)
	}
	id := reservation.Operation.ID
	if err = f.db.BeginCreationArtifact(t.Context(), id, 0); !errors.Is(err, ErrCreationConflict) {
		t.Fatalf("reserved operation dispatched artifact: %v", err)
	}
	if _, err = f.c.BeginDispatch(t.Context(), reservation.Permit); err != nil {
		t.Fatal(err)
	}
	if err = f.db.BeginCreationArtifact(t.Context(), id, 2); !errors.Is(err, ErrCreationConflict) {
		t.Fatalf("out of bounds ordinal: %v", err)
	}
	if err = f.db.BeginCreationArtifact(t.Context(), id, 0); err != nil {
		t.Fatal(err)
	}
	if err = f.db.BeginCreationArtifact(t.Context(), id, 0); !errors.Is(err, ErrCreationConflict) {
		t.Fatalf("duplicate dispatch: %v", err)
	}
	before, err := f.db.CreationPublicationArtifacts(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 1 || !before[0].Dispatched || before[0].IssueID != 0 {
		t.Fatalf("intent %+v", before)
	}
	fact := CreationPublicationArtifact{Ordinal: 0, IssueID: 900, IssueNumber: 42, Relationship: "uncertain"}
	if err = f.db.SaveCreationArtifact(t.Context(), id, fact); err != nil {
		t.Fatal(err)
	}
	fact.Relationship = "attached"
	if err = f.db.SaveCreationArtifact(t.Context(), id, fact); err != nil {
		t.Fatal(err)
	}
	for _, changed := range []CreationPublicationArtifact{{Ordinal: 0, IssueID: 901, IssueNumber: 42, Relationship: "attached"}, {Ordinal: 0, IssueID: 900, IssueNumber: 43, Relationship: "attached"}, {Ordinal: 0, IssueID: 900, IssueNumber: 42, Relationship: "uncertain"}, {Ordinal: 1, IssueID: 901, IssueNumber: 43, Relationship: "attached"}} {
		if err = f.db.SaveCreationArtifact(t.Context(), id, changed); !errors.Is(err, ErrCreationConflict) {
			t.Fatalf("changed retained fact %+v: %v", changed, err)
		}
	}
	reopened, err := Open(t.Context(), f.db.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	details, err := reopened.CreationPublicationArtifacts(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if len(details) != 1 || details[0].IssueID != 900 || details[0].Relationship != "attached" {
		t.Fatalf("reopen %+v", details)
	}
	retained, err := reopened.CreationPublicationContext(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if string(retained.Envelope) != string(proposal.Envelope) || retained.Repository != proposal.Repository {
		t.Fatalf("retained proposal %+v", retained)
	}
}
