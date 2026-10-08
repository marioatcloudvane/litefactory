package store

import (
	"errors"
	"github.com/cloudvane/litefactory/internal/store/creationfixture"
	"testing"
)

func TestCreationPublicationDBDispatchAndMonotonicFacts(t *testing.T) {
	f := newCreationDBFixture(t)
	lease, proposal, intent := f.approved(t, "api", 273, 2)
	id := creationfixture.CreationHistory(t, DSN(f.db.Path()), intent, lease.Binding.RunID, lease.Binding.Attempt, "", 0)
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
