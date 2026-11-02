package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// LatestGraphRecord reads an immutable typed record from shared graph history.
// It supplies no dispatch, approval or delivery authority; callers recheck its
// exact source attempt and current factory binding before using it.
func (db *DB) LatestGraphRecord(ctx context.Context, instanceID int64, record string) (out json.RawMessage, err error) {
	if instanceID < 1 || record == "" || len(record) > 256 {
		return nil, graphInvariant("invalid graph record binding")
	}
	err = db.withTx(ctx, func(tx *sql.Tx) error { return graphRecordTx(ctx, tx, instanceID, record, &out) })
	if err != nil {
		return nil, fmt.Errorf("store: latest graph record: %w", err)
	}
	return out, nil
}

// MaxGraphOutputReadBytes is the largest byte bound a saved-output read may
// request. Writers whose output a later step reads must stay within it.
const MaxGraphOutputReadBytes = 1 << 20

const graphOutputColumns = `id,node_attempt_id,kind,payload,owner_comment_id,created_at`

// RecordGraphOutput appends opaque bytes. CompleteGraphNodeAttempt is preferred
// when this output belongs to the same atomic operation as node completion.
func (db *DB) RecordGraphOutput(ctx context.Context, in GraphOutput) (out GraphOutput, err error) {
	err = db.withTx(ctx, func(tx *sql.Tx) error { var e error; out, e = recordGraphOutputTx(ctx, tx, in); return e })
	if err != nil {
		return GraphOutput{}, fmt.Errorf("store: record graph output: %w", err)
	}
	return out, nil
}

func recordGraphOutputTx(ctx context.Context, tx *sql.Tx, in GraphOutput) (GraphOutput, error) {
	if in.NodeAttemptID <= 0 || in.OwnerCommentID < 0 || in.CreatedAt.IsZero() || (in.Kind != GraphOutputText && in.Kind != GraphOutputJSON && in.Kind != GraphOutputDiagnostic) {
		return GraphOutput{}, graphInvariant("invalid graph output kind, identity or creation time")
	}
	payload := bytes.Clone(in.Payload)
	if payload == nil {
		payload = []byte{}
	}
	return scanGraphOutput(tx.QueryRowContext(ctx, `INSERT INTO graph_outputs(node_attempt_id,kind,payload,owner_comment_id,created_at) VALUES(?,?,?,?,?) RETURNING `+graphOutputColumns, in.NodeAttemptID, in.Kind, payload, nullID(in.OwnerCommentID), FormatTime(in.CreatedAt)))
}

// ListGraphOutputs returns an attempt's outputs in insertion order.
func (db *DB) ListGraphOutputs(ctx context.Context, attemptID int64) ([]GraphOutput, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+graphOutputColumns+` FROM graph_outputs WHERE node_attempt_id=? ORDER BY id`, attemptID)
	if err != nil {
		return nil, fmt.Errorf("store: list graph outputs: %w", err)
	}
	defer rows.Close()
	out := make([]GraphOutput, 0)
	for rows.Next() {
		v, e := scanGraphOutput(rows)
		if e != nil {
			return nil, fmt.Errorf("store: scan graph output: %w", e)
		}
		out = append(out, v)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list graph outputs: %w", err)
	}
	return out, nil
}

func scanGraphOutput(row scanner) (GraphOutput, error) {
	var v GraphOutput
	var owner sql.NullInt64
	var created string
	if err := row.Scan(&v.ID, &v.NodeAttemptID, &v.Kind, &v.Payload, &owner, &created); err != nil {
		return v, err
	}
	v.OwnerCommentID = scanID(owner)
	v.Payload = bytes.Clone(v.Payload)
	var err error
	v.CreatedAt, err = ParseTime(created)
	return v, err
}

// ListGraphOutputsBounded bounds retained bytes and row count inside the same
// short transaction as reading, before the SQL driver allocates payloads. Use
// this for saved output crossing into agent input rather than the unbounded
// foundation inspection accessor.
func (db *DB) ListGraphOutputsBounded(ctx context.Context, attemptID int64, maxBytes, maxEntries int) (out []GraphOutput, err error) {
	return db.listGraphOutputsBounded(ctx, attemptID, maxBytes, maxEntries, false)
}

// graphResultOutputs excludes factory records ({"record": ...}) such as source
// manifests, which grow with the repository and share the agent's attempt.
const (
	graphResultOutputs      = ` AND NOT (kind='json' AND json_valid(CAST(payload AS TEXT)) AND json_extract(CAST(payload AS TEXT),'$.record') IS NOT NULL)`
	graphOutputsSize        = `SELECT COALESCE(SUM(length(payload)),0),COUNT(*) FROM graph_outputs WHERE node_attempt_id=?`
	graphOutputsRows        = `SELECT ` + graphOutputColumns + ` FROM graph_outputs WHERE node_attempt_id=?`
	graphResultOutputsSize  = graphOutputsSize + graphResultOutputs
	graphResultOutputsRows  = graphOutputsRows + graphResultOutputs + ` ORDER BY id`
	graphOutputsRowsOrdered = graphOutputsRows + ` ORDER BY id`
)

// ListGraphResultOutputsBounded is ListGraphOutputsBounded without factory
// records, so the bounds apply to what the agent submitted only.
func (db *DB) ListGraphResultOutputsBounded(ctx context.Context, attemptID int64, maxBytes, maxEntries int) (out []GraphOutput, err error) {
	return db.listGraphOutputsBounded(ctx, attemptID, maxBytes, maxEntries, true)
}

// ListGraphOutputsBoundedByReference is ListGraphOutputsBounded for outputs
// that embed a copy of a record retained elsewhere. A JSON output whose
// top-level reference field is a non-empty string has its top-level embedded field removed
// inside SQL before the bounds apply, so a large embedded copy never blocks
// reading the output. The caller must reload the referenced record from its
// own durable store; outputs without the reference are returned unchanged.
func (db *DB) ListGraphOutputsBoundedByReference(ctx context.Context, attemptID int64, maxBytes, maxEntries int, embedded, reference string) (out []GraphOutput, err error) {
	if !plainJSONKey(embedded) || !plainJSONKey(reference) || embedded == reference {
		return nil, graphInvariant("invalid embedded-output projection")
	}
	payload := `(CASE WHEN kind='json' AND json_valid(CAST(payload AS TEXT)) AND json_type(CAST(payload AS TEXT),?) IS NOT NULL AND json_type(CAST(payload AS TEXT),?)='text' AND json_extract(CAST(payload AS TEXT),?)<>'' THEN CAST(json_remove(CAST(payload AS TEXT),?) AS BLOB) ELSE payload END)`
	e, r := "$."+embedded, "$."+reference
	q := graphOutputsQueries{
		size: `SELECT COALESCE(SUM(length(` + payload + `)),0),COUNT(*) FROM graph_outputs WHERE node_attempt_id=?`,
		rows: `SELECT id,node_attempt_id,kind,` + payload + `,owner_comment_id,created_at FROM graph_outputs WHERE node_attempt_id=? ORDER BY id`,
		args: []any{e, r, r, e},
	}
	return db.listGraphOutputsBoundedQuery(ctx, attemptID, maxBytes, maxEntries, q)
}

func plainJSONKey(k string) bool {
	if k == "" || len(k) > 64 {
		return false
	}
	for _, c := range k {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return true
}

type graphOutputsQueries struct {
	size, rows string
	args       []any // bound before the attempt id in both queries
}

func (db *DB) listGraphOutputsBounded(ctx context.Context, attemptID int64, maxBytes, maxEntries int, resultsOnly bool) (out []GraphOutput, err error) {
	q := graphOutputsQueries{size: graphOutputsSize, rows: graphOutputsRowsOrdered}
	if resultsOnly {
		q = graphOutputsQueries{size: graphResultOutputsSize, rows: graphResultOutputsRows}
	}
	return db.listGraphOutputsBoundedQuery(ctx, attemptID, maxBytes, maxEntries, q)
}

func (db *DB) listGraphOutputsBoundedQuery(ctx context.Context, attemptID int64, maxBytes, maxEntries int, q graphOutputsQueries) (out []GraphOutput, err error) {
	if attemptID < 1 || maxBytes < 1 || maxBytes > MaxGraphOutputReadBytes || maxEntries < 1 || maxEntries > 1024 {
		return nil, graphInvariant("invalid saved-output read limits")
	}
	args := append(append([]any{}, q.args...), attemptID)
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		var size, count int64
		if err := tx.QueryRowContext(ctx, q.size, args...).Scan(&size, &count); err != nil {
			return err
		}
		if size > int64(maxBytes) || count > int64(maxEntries) {
			return graphInvariant("saved graph outputs exceed input bounds")
		}
		rows, err := tx.QueryContext(ctx, q.rows, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		out = make([]GraphOutput, 0, int(count))
		for rows.Next() {
			v, err := scanGraphOutput(rows)
			if err != nil {
				return err
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("store: bounded graph outputs: %w", err)
	}
	return out, nil
}
