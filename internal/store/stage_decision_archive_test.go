package store

import (
	"database/sql"
	"testing"
	"time"
)

func TestStageDecisionArchiveRetainsTransitionAndSummaryRecovery(t *testing.T) {
	db := newTestDB(t)
	family := archiveITSeed(t, db)
	at := time.Now().UTC()
	target := StageDecisionTarget{TaskRef: family.TaskRef, Workflow: "research", Operation: "revise_task"}
	receipt := StageDecisionReceipt{ExecutionID: "owner-action:archived-decision", TaskRef: family.TaskRef, Operation: "revise_task", State: "applied", Transition: "Revision admitted", AppliedAt: at}
	if err := db.withTx(t.Context(), func(tx *sql.Tx) error {
		return retainStageDecisionReceiptTx(t.Context(), tx, target, "Reviewed guidance", receipt, 0, at)
	}); err != nil {
		t.Fatal(err)
	}
	publication := StageDecisionDelivery{ExecutionID: receipt.ExecutionID, ProposalID: "archived-decision", TaskRef: family.TaskRef, Rendered: "Reviewed decision summary", Digest: "frozen-summary", Principal: OwnerActionPrincipal{OwnerID: "42", OwnerGeneration: 1, Transport: "mcp", GrantID: "original", ClientID: "client"}}
	if err := db.EnsureStageDecisionDelivery(t.Context(), publication, at); err != nil {
		t.Fatal(err)
	}
	trailer := archiveITTrailer(t, db, family)
	if _, err := db.DeleteArchivedFamily(t.Context(), family, trailer, at); err != nil {
		t.Fatal(err)
	}
	saved, found, err := db.RecoverStageDecision(t.Context(), receipt.ExecutionID, at)
	if err != nil || !found || saved != receipt {
		t.Fatalf("archived decision was lost or replayed: %+v %v %v", saved, found, err)
	}
	delivery, err := db.ReadStageDecisionDelivery(t.Context(), receipt.ExecutionID)
	if err != nil || delivery.Rendered != publication.Rendered || delivery.State != "pending" {
		t.Fatalf("archived summary recovery: %+v %v", delivery, err)
	}
	progress, err := db.StageDecisionRevisionProgress(t.Context(), receipt.ExecutionID)
	if err != nil || progress == "admitted; replacement work pending" {
		t.Fatalf("archived details appeared active: %q %v", progress, err)
	}
}
