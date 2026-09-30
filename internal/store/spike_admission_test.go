package store

import (
	"errors"
	"strings"
	"testing"
)

func recordedPath(t *testing.T, db *DB, sessionID int64) (string, bool) {
	t.Helper()
	var path string
	err := db.sql.QueryRowContext(t.Context(), `SELECT path FROM research_execution_paths WHERE session_id=?`, sessionID).Scan(&path)
	if err != nil {
		return "", false
	}
	return path, true
}

func TestSpikePathFreshSessionIsGraphAndSticky(t *testing.T) {
	db := newTestDB(t)
	session := testSession(t, db, 601)
	if err := db.AdmitSpikePath(t.Context(), session.ID); err != nil {
		t.Fatalf("fresh session: %v", err)
	}
	if path, ok := recordedPath(t, db, session.ID); !ok || path != "graph" {
		t.Fatalf("recorded path %q %v", path, ok)
	}
	if err := db.AdmitSpikePath(t.Context(), session.ID); err != nil {
		t.Fatalf("second admission: %v", err)
	}
	if err := db.AdmitSpikePath(t.Context(), 0); err == nil {
		t.Fatal("invalid session accepted")
	}
}

func TestSpikePathLegacyHistoryIsRefusedWithoutRewriting(t *testing.T) {
	db := newTestDB(t)
	session := testSession(t, db, 602)
	testRun(t, db, session.ID, 1)
	err := db.AdmitSpikePath(t.Context(), session.ID)
	if !errors.Is(err, ErrSpikeLegacyRetired) {
		t.Fatalf("legacy history admitted or wrongly refused: %v", err)
	}
	if path, ok := recordedPath(t, db, session.ID); ok {
		t.Fatalf("refusal wrote route %q", path)
	}
	var runs int
	if e := db.sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM runs WHERE session_id=?`, session.ID).Scan(&runs); e != nil || runs != 1 {
		t.Fatalf("history changed: %d %v", runs, e)
	}
}

func TestSpikePathKnownGraphHistoryStaysGraph(t *testing.T) {
	db := newTestDB(t)
	session := testSession(t, db, 603)
	definition, err := db.CreateGraphDefinition(t.Context(), GraphDefinition{Team: "builtin", Mode: "spike", ProfileVersion: 1, Path: "spike/spike.dot", Digest: strings.Repeat("a", 64), CreatedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(t.Context(), `INSERT INTO graph_instances(session_id,definition_id,status,current_node,started_at) VALUES(?,?,'running','start',?)`, session.ID, definition.ID, FormatTime(nowUTC())); err != nil {
		t.Fatal(err)
	}
	if err := db.AdmitSpikePath(t.Context(), session.ID); err != nil {
		t.Fatalf("graph history refused: %v", err)
	}
}

func TestSpikePathConflictingProvenanceIsRefused(t *testing.T) {
	db := newTestDB(t)
	legacy := testSession(t, db, 604)
	testRun(t, db, legacy.ID, 1)
	if _, err := db.AdmitWorkflowPath(t.Context(), legacy.ID, "graph"); err != nil {
		t.Fatal(err)
	}
	if err := db.AdmitSpikePath(t.Context(), legacy.ID); !errors.Is(err, ErrSpikeLegacyRetired) {
		t.Fatalf("graph path over legacy-only history: %v", err)
	}

	graph := testSession(t, db, 605)
	definition, err := db.CreateGraphDefinition(t.Context(), GraphDefinition{Team: "builtin", Mode: "spike", ProfileVersion: 1, Path: "spike/spike.dot", Digest: strings.Repeat("b", 64), CreatedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(t.Context(), `INSERT INTO graph_instances(session_id,definition_id,status,current_node,started_at) VALUES(?,?,'running','start',?)`, graph.ID, definition.ID, FormatTime(nowUTC())); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AdmitWorkflowPath(t.Context(), graph.ID, "legacy"); err != nil {
		t.Fatal(err)
	}
	if err := db.AdmitSpikePath(t.Context(), graph.ID); !errors.Is(err, ErrSpikeLegacyRetired) {
		t.Fatalf("legacy path over graph history: %v", err)
	}
	if path, _ := recordedPath(t, db, graph.ID); path != "legacy" {
		t.Fatalf("refusal rewrote the recorded route to %q", path)
	}
}

func TestSpikePathCrossModeLegacyRouteIsRefused(t *testing.T) {
	db := newTestDB(t)
	session := testSession(t, db, 606)
	if _, err := db.AdmitWorkflowPath(t.Context(), session.ID, "legacy"); err != nil {
		t.Fatal(err)
	}
	if err := db.AdmitSpikePath(t.Context(), session.ID); !errors.Is(err, ErrSpikeLegacyRetired) {
		t.Fatalf("a legacy route recorded by another mode was admitted: %v", err)
	}
	if path, _ := recordedPath(t, db, session.ID); path != "legacy" {
		t.Fatalf("route rewritten to %q", path)
	}
}
