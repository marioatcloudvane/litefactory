package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// SpikeRecord kinds. Each is a Spike-owned record of one graph instance and
// node; none reuses another mode's tables.
const (
	SpikeRecordBaseline = "baseline"
	SpikeRecordCheck    = "check"
	SpikeRecordIssue    = "issue"
)

// MaxSpikeRecordBytes bounds one retained record.
const MaxSpikeRecordBytes = 1 << 20

// SpikeRecord is one retained payload, written by the node named in Node.
type SpikeRecord struct {
	InstanceID int64
	Node, Kind string
	Payload    []byte
	UpdatedAt  time.Time
}

func validSpikeRecordKind(kind string) bool {
	return kind == SpikeRecordBaseline || kind == SpikeRecordCheck || kind == SpikeRecordIssue
}

// PutSpikeRecord creates or replaces the record of one instance, node and kind.
func (db *DB) PutSpikeRecord(ctx context.Context, in SpikeRecord) error {
	if in.InstanceID < 1 || in.Node == "" || !validSpikeRecordKind(in.Kind) || in.UpdatedAt.IsZero() || len(in.Payload) == 0 || len(in.Payload) > MaxSpikeRecordBytes {
		return fmt.Errorf("store: invalid spike record")
	}
	_, err := db.sql.ExecContext(ctx, `INSERT INTO spike_records(instance_id,node,kind,payload,updated_at) VALUES(?,?,?,?,?)
 ON CONFLICT(instance_id,node,kind) DO UPDATE SET payload=excluded.payload,updated_at=excluded.updated_at`,
		in.InstanceID, in.Node, in.Kind, string(in.Payload), FormatTime(in.UpdatedAt))
	if err != nil {
		return fmt.Errorf("store: put spike record: %w", err)
	}
	return nil
}

// GetSpikeRecord returns one record or ErrNotFound.
func (db *DB) GetSpikeRecord(ctx context.Context, instanceID int64, node, kind string) (SpikeRecord, error) {
	return scanSpikeRecord(db.sql.QueryRowContext(ctx, `SELECT instance_id,node,kind,payload,updated_at FROM spike_records WHERE instance_id=? AND node=? AND kind=?`, instanceID, node, kind))
}

// LatestSpikeRecord returns the most recently written record of a kind in one
// instance, whichever node wrote it, or ErrNotFound.
func (db *DB) LatestSpikeRecord(ctx context.Context, instanceID int64, kind string) (SpikeRecord, error) {
	return scanSpikeRecord(db.sql.QueryRowContext(ctx, `SELECT instance_id,node,kind,payload,updated_at FROM spike_records WHERE instance_id=? AND kind=? ORDER BY updated_at DESC,rowid DESC LIMIT 1`, instanceID, kind))
}

func scanSpikeRecord(row scanner) (SpikeRecord, error) {
	var out SpikeRecord
	var payload, updated string
	if err := row.Scan(&out.InstanceID, &out.Node, &out.Kind, &payload, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SpikeRecord{}, ErrNotFound
		}
		return SpikeRecord{}, fmt.Errorf("store: read spike record: %w", err)
	}
	out.Payload = []byte(payload)
	var err error
	out.UpdatedAt, err = ParseTime(updated)
	if err != nil {
		return SpikeRecord{}, fmt.Errorf("store: read spike record: %w", err)
	}
	return out, nil
}
