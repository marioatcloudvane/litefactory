// Package creationfixture seeds historical storage fixtures for recovery tests.
// It is test support only and must never be imported by production code.
package creationfixture

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// CreationHistory records a legacy uncertain operation after its proposal,
// intent, lease and optional family request have been retained by the fixture.
// It returns only an operation ID, never a dispatch capability.
func CreationHistory(t *testing.T, dsn string, intent, run int64, attempt, request string, ordinals ...int) int64 {
	t.Helper()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }() // Cleanup after failures; commit already ends a successful transaction.
	r, err := tx.ExecContext(t.Context(), `INSERT INTO mcp_creation_grants(intent_id,run_id,attempt,instance,expires_ns) SELECT ?,run_id,attempt,instance,expires_ns FROM mcp_attempt_leases WHERE run_id=? AND attempt=?`, intent, run, attempt)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := r.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	r, err = tx.ExecContext(t.Context(), `INSERT INTO mcp_creation_operations(grant_id,repository,proposal_hash,artifacts,owner_digest,state,dispatched,created_ns) SELECT ?,p.repository,p.hash,p.artifacts,'historical-test-fixture','uncertain',1,i.created_ns FROM mcp_creation_intents i JOIN mcp_creation_proposals p ON p.hash=i.proposal_hash WHERE i.id=?`, grant, intent)
	if err != nil {
		t.Fatal(err)
	}
	id, err := r.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	for _, ordinal := range ordinals {
		if _, err = tx.ExecContext(t.Context(), `INSERT INTO creation_publication_artifacts(operation_id,ordinal,dispatched) VALUES(?,?,1)`, id, ordinal); err != nil {
			t.Fatal(err)
		}
	}
	if request != "" {
		if _, err = tx.ExecContext(t.Context(), `INSERT INTO creation_publication_requests(run_id,attempt,request_id,operation_id) VALUES(?,?,?,?)`, run, attempt, request, id); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return id
}

// NoCreationIntents checks that drafting or refusal did not admit owner intent.
func NoCreationIntents(t *testing.T, dsn, task string) {
	t.Helper()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var count int
	if err = db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM mcp_creation_intents WHERE source_task=?`, task).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("unexpected creation intents: %d", count)
	}
}
