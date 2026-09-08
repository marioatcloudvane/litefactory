package store

import (
	"reflect"
	"testing"
)

func TestRegionProposalRoundTripIsolationAndLegacy(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 400)
	legacy, _, err := db.CreateOrGetConflictProposal(t.Context(), CreateConflictProposalParams{SessionID: session.ID, TriggerCommentID: 10, Model: "codex", BaseSHA: "base", BranchSHA: "branch", Files: []ConflictProposalFile{{Path: "old.go", Resolution: "old whole file"}}})
	if err != nil {
		t.Fatal(err)
	}
	params := CreateConflictProposalParams{SessionID: session.ID, TriggerCommentID: 11, Model: "codex", Format: ConflictRegionFormat, BaseSHA: "base", BranchSHA: "branch",
		Files: []ConflictProposalFile{{Path: "new.go", Details: ConflictFileDetails{OriginalDigest: "original", ResultDigest: "result", Mode: "100644", Regions: []ConflictRegionResolution{{ID: "one", Resolution: ""}, {ID: "two", Resolution: "field bool\r\n"}}, Metrics: ConflictFileMetrics{BeforeBytes: 100, AfterBytes: 90, BeforeLines: 10, AfterLines: 9, Additions: 1, Deletions: 2}}}}}
	proposal, created, err := db.CreateOrGetConflictProposal(t.Context(), params)
	if err != nil || !created {
		t.Fatalf("create: %v", err)
	}
	if proposal.Format != 1 || proposal.BranchSHA != "branch" || proposal.BaseSHA != "base" || !reflect.DeepEqual(proposal.Files[0].Details, params.Files[0].Details) {
		t.Fatalf("round trip lost values: %+v", proposal)
	}
	params.Files[0].Details.Regions[0].Resolution = "mutated caller"
	proposal.Files[0].Details.Regions[1].Resolution = "mutated returned"
	fetched, err := db.GetConflictProposal(t.Context(), proposal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fetched.Files[0].Details.Regions[0].Resolution != "" || fetched.Files[0].Details.Regions[1].Resolution != "field bool\r\n" {
		t.Fatal("nested aliases changed storage")
	}
	replay, created, err := db.CreateOrGetConflictProposal(t.Context(), params)
	if err != nil || created || !reflect.DeepEqual(replay.Files, fetched.Files) {
		t.Fatalf("replay changed regions: %v", err)
	}
	old, err := db.GetConflictProposal(t.Context(), legacy.ID)
	if err != nil || old.Format != 0 || old.Status != ConflictProposalSuperseded || old.Files[0].Resolution != "old whole file" {
		t.Fatalf("legacy upgraded or lifecycle lost: %+v %v", old, err)
	}
	if fetched.Files[0].Resolution != "" {
		t.Fatal("whole file newly persisted")
	}
}
