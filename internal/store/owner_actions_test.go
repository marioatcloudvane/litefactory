package store

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func ownerActionStoreFixture(t *testing.T) (*DB, OwnerAction) {
	t.Helper()
	db := newTestDB(t)
	o := connectorTestOwner(t, db, 42)
	at := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	p := OwnerAction{ID: "proposal", PolicyVersion: 1, Principal: OwnerActionPrincipal{OwnerID: "42", OwnerGeneration: o.Generation, Transport: "discord", ChannelID: "200", DiscordOwnerID: "100"}, RequestKey: "key", RequestDigest: "request", Facts: `{"target_revision":"reviewed"}`, Payload: `{"exact":"reviewed"}`, Digest: "digest", Scope: "standards:management", Tier: "conversational", State: "awaiting_confirmation", CreatedAt: at, AvailableAt: at, ConfirmNotBefore: at.Add(5 * time.Second), ExpiresAt: at.Add(15 * time.Minute), ExecutionID: "owner-action:proposal"}
	if err := db.CreateOwnerAction(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	return db, p
}

func TestOwnerActionAdmissionCancellationRace(t *testing.T) {
	db, p := ownerActionStoreFixture(t)
	start := make(chan struct{})
	results := make(chan error, 10)
	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		method := "confirm"
		if i%2 == 0 {
			method = "cancel"
		}
		// All ten goroutines join before test cleanup and wait only on this closed gate.
		go func() {
			defer wg.Done()
			<-start
			_, err := db.MutateOwnerAction(t.Context(), p.Principal, p.ID, method, "", p.ConfirmNotBefore)
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	current, err := db.ReadOwnerAction(t.Context(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	execution, executionErr := db.ReadNaturalExecution(t.Context(), p.ExecutionID)
	switch current.State {
	case "cancelled":
		if !errors.Is(executionErr, ErrNotFound) {
			t.Fatal("cancellation left an execution")
		}
	case "admitted":
		if executionErr != nil || execution.Payload != p.Payload {
			t.Fatalf("admission lost exact journal: %+v %v", execution, executionErr)
		}
	default:
		t.Fatalf("race left %s", current.State)
	}
	attempts, err := db.OwnerActionAttempts(t.Context(), p.ID)
	if err != nil || len(attempts) != 10 {
		t.Fatalf("lost race decisions: %d %v", len(attempts), err)
	}
}

func TestOwnerActionMultipleProposalsShareJournalWithoutCollision(t *testing.T) {
	db, p := ownerActionStoreFixture(t)
	for i := range 3 {
		proposal := p
		if i > 0 {
			proposal.ID = fmt.Sprint("proposal-", i)
			proposal.RequestKey = proposal.ID
			proposal.ExecutionID = "owner-action:" + proposal.ID
			if err := db.CreateOwnerAction(t.Context(), proposal); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := db.MutateOwnerAction(t.Context(), proposal.Principal, proposal.ID, "confirm", "", proposal.ConfirmNotBefore); err != nil {
			t.Fatal(err)
		}
		e, err := db.ReadNaturalExecution(t.Context(), proposal.ExecutionID)
		if err != nil || e.Revision != int64(i+1) {
			t.Fatalf("journal collision: %+v %v", e, err)
		}
	}
}

func TestOwnerActionGrantRevocationAndOriginFence(t *testing.T) {
	db, p := ownerActionStoreFixture(t)
	at := p.CreatedAt
	connectorTestClient(t, db, "client")
	if _, err := db.sql.ExecContext(t.Context(), `INSERT INTO connector_grants(id,client_id,resource,scope,owner_id,generation,created_at,expires_at) VALUES(?,?,?,?,?,?,?,?)`, "grant", "client", connectorTestResource, "standards:management", 42, p.Principal.OwnerGeneration, FormatTime(at), FormatTime(at.Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	p.ID = "mcp-proposal"
	p.RequestKey = "mcp-key"
	p.ExecutionID = "owner-action:mcp-proposal"
	p.Principal.Transport = "mcp"
	p.Principal.ClientID = "client"
	p.Principal.GrantID = "grant"
	if err := db.CreateOwnerAction(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	other := p.Principal
	other.GrantID = "replacement"
	if _, err := db.MutateOwnerAction(t.Context(), other, p.ID, "confirm", "", p.ConfirmNotBefore); !errors.Is(err, ErrConnectorInvalid) {
		t.Fatalf("other grant obtained proposal: %v", err)
	}
	if err := db.RevokeConnectorGrant(t.Context(), 42, "grant", p.ConfirmNotBefore); err != nil {
		t.Fatal(err)
	}
	if _, err := db.MutateOwnerAction(t.Context(), p.Principal, p.ID, "confirm", "", p.ConfirmNotBefore); !errors.Is(err, ErrConnectorInvalid) {
		t.Fatalf("revoked grant admitted: %v", err)
	}
	if _, err := db.ReadNaturalExecution(t.Context(), p.ExecutionID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("refused grant created journal: %v", err)
	}
	attempts, err := db.OwnerActionAttempts(t.Context(), p.ID)
	if err != nil || len(attempts) != 2 || attempts[0].Decision != "origin_mismatch" || attempts[1].Decision != "authority_revoked" {
		t.Fatalf("refusals lost: %+v %v", attempts, err)
	}
}

func TestOwnerActionHistoryRetentionKeepsRecoveryAndCompletionClock(t *testing.T) {
	db, p := ownerActionStoreFixture(t)
	admitted, err := db.MutateOwnerAction(t.Context(), p.Principal, p.ID, "confirm", "", p.ConfirmNotBefore)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ClaimOwnerActionExecution(t.Context(), p.ID, p.ConfirmNotBefore); err != nil {
		t.Fatal(err)
	}
	finished := p.CreatedAt.Add(10 * time.Hour)
	if err = db.FinishOwnerAction(t.Context(), p.ID, "uncertain", `{"state":"uncertain"}`, "interrupted", finished); err != nil {
		t.Fatal(err)
	}
	if err = db.PurgeNaturalChat(t.Context(), finished.Add(25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ReadNaturalExecution(t.Context(), admitted.ExecutionID); err != nil {
		t.Fatalf("chat purge removed recovery: %v", err)
	}
	if err = db.PurgeOwnerActionHistory(t.Context(), finished.Add(100*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ReadOwnerAction(t.Context(), p.ID); err != nil {
		t.Fatalf("unresolved history removed: %v", err)
	}
	if err = db.FinishOwnerAction(t.Context(), p.ID, "succeeded", `{"state":"succeeded"}`, "receipt", finished); err != nil {
		t.Fatal(err)
	}
	if err = db.PurgeOwnerActionHistory(t.Context(), finished.Add(90*24*time.Hour-time.Nanosecond)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ReadOwnerAction(t.Context(), p.ID); err != nil {
		t.Fatalf("history purged early: %v", err)
	}
	if err = db.PurgeOwnerActionHistory(t.Context(), finished.Add(90*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ReadOwnerAction(t.Context(), p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("history retained past terminal boundary: %v", err)
	}
}

func TestOwnerActionReactionCancellationRace(t *testing.T) {
	db, p := ownerActionStoreFixture(t)
	binding := p
	binding.ID = "binding"
	binding.RequestKey = "binding"
	binding.ExecutionID = "owner-action:binding"
	binding.Tier = "binding"
	binding.ApprovalChannel = "200"
	if err := db.CreateOwnerAction(t.Context(), binding); err != nil {
		t.Fatal(err)
	}
	if _, err := db.MutateOwnerAction(t.Context(), binding.Principal, binding.ID, "confirm", "", binding.ConfirmNotBefore); err != nil {
		t.Fatal(err)
	}
	if claimed, err := db.ClaimOwnerActionDelivery(t.Context(), binding.ID, binding.ConfirmNotBefore); err != nil || !claimed {
		t.Fatalf("claim: %v %v", claimed, err)
	}
	if err := db.FinishOwnerActionDelivery(t.Context(), binding.ID, "confirmed-message", "", binding.ConfirmNotBefore); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 10)
	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		method := "reaction"
		if i%2 == 0 {
			method = "cancel"
		}
		// Ten contenders are joined below; the gate is closed before waiting.
		go func() {
			defer wg.Done()
			<-start
			_, err := db.MutateOwnerAction(t.Context(), binding.Principal, binding.ID, method, `{"message":"confirmed-message","emoji":"✅"}`, binding.ConfirmNotBefore)
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	current, err := db.ReadOwnerAction(t.Context(), binding.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, executionErr := db.ReadNaturalExecution(t.Context(), binding.ExecutionID)
	if current.State == "cancelled" && !errors.Is(executionErr, ErrNotFound) {
		t.Fatal("cancelled tap admitted an execution")
	}
	if current.State == "admitted" && executionErr != nil {
		t.Fatalf("winning tap lost journal: %v", executionErr)
	}
	if current.State != "cancelled" && current.State != "admitted" {
		t.Fatalf("race left %s", current.State)
	}
	var count int
	if err = db.sql.QueryRowContext(t.Context(), `SELECT count(*) FROM chat_natural_executions WHERE identity=?`, binding.ExecutionID).Scan(&count); err != nil || count > 1 {
		t.Fatalf("multiple execution identities: %d %v", count, err)
	}
}

func TestOwnerActionImmutableFactsAndPayload(t *testing.T) {
	db, p := ownerActionStoreFixture(t)
	for _, change := range []string{`UPDATE owner_action_proposals SET payload='{"changed":true}' WHERE id=?`, `UPDATE owner_action_proposals SET payload='{}' WHERE id=?`, `UPDATE owner_action_proposals SET facts='{"target":"changed"}' WHERE id=?`, `UPDATE owner_action_proposals SET digest='changed' WHERE id=?`} {
		if _, err := db.sql.ExecContext(t.Context(), change, p.ID); err == nil {
			t.Fatal("immutable proposal content changed")
		}
	}
	if _, err := db.MutateOwnerAction(t.Context(), p.Principal, p.ID, "cancel", "", p.CreatedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(t.Context(), `UPDATE owner_action_proposals SET payload='{}' WHERE id=?`, p.ID); err != nil {
		t.Fatalf("terminal payload clear refused: %v", err)
	}
}
