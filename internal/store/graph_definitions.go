package store

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
)

const graphDefinitionColumns = `id,team,mode,workflow,profile_version,path,digest,team_snapshot_digest,created_at`

// CreateGraphDefinition returns the existing immutable pin for team/mode/digest,
// or inserts it transactionally. A changed path cannot rewrite an existing pin.
func (db *DB) CreateGraphDefinition(ctx context.Context, in GraphDefinition) (out GraphDefinition, err error) {
	if in.Workflow != "" && in.Workflow != "bug" && in.Workflow != in.Mode {
		return out, graphInvariant("conflicting graph workflow binding")
	}
	if in.Workflow == "bug" && in.Mode != "" {
		return out, graphInvariant("Bug definition cannot have a mode")
	}
	if in.Workflow == "bug" {
		in.Mode = standaloneGraphStorageKey
	}
	if !validGraphPath(in.Path) {
		return out, &GraphDefinitionPathError{Path: in.Path}
	}
	digest, decodeErr := hex.DecodeString(in.Digest)
	if in.Team == "" || in.Mode == "" || in.ProfileVersion < 1 || decodeErr != nil || len(digest) != 32 || in.Digest != hex.EncodeToString(digest) {
		return out, graphInvariant("definition needs team, mode, version and lowercase SHA256 digest")
	}
	if in.CreatedAt.IsZero() {
		in.CreatedAt = nowUTC()
	}
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `INSERT INTO graph_definitions(team,mode,workflow,profile_version,path,digest,team_snapshot_digest,created_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(team,mode,digest) DO NOTHING`, in.Team, in.Mode, in.Workflow, in.ProfileVersion, in.Path, in.Digest, nullString(in.TeamSnapshotDigest), FormatTime(in.CreatedAt))
		if e != nil {
			return e
		}
		out, e = scanGraphDefinition(tx.QueryRowContext(ctx, `SELECT `+graphDefinitionColumns+` FROM graph_definitions WHERE team=? AND mode=? AND digest=?`, in.Team, in.Mode, in.Digest))
		return e
	})
	if err != nil {
		return GraphDefinition{}, fmt.Errorf("store: create graph definition: %w", err)
	}
	return out, nil
}

// GetGraphDefinition reads an immutable definition by ID.
func (db *DB) GetGraphDefinition(ctx context.Context, id int64) (GraphDefinition, error) {
	out, err := scanGraphDefinition(db.sql.QueryRowContext(ctx, `SELECT `+graphDefinitionColumns+` FROM graph_definitions WHERE id=?`, id))
	if err != nil {
		return GraphDefinition{}, fmt.Errorf("store: get graph definition: %w", err)
	}
	return out, nil
}

func scanGraphDefinition(row scanner) (GraphDefinition, error) {
	var d GraphDefinition
	var snapshot sql.NullString
	var created string
	if err := row.Scan(&d.ID, &d.Team, &d.Mode, &d.Workflow, &d.ProfileVersion, &d.Path, &d.Digest, &snapshot, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return d, ErrNotFound
		}
		return d, err
	}
	if d.Mode == standaloneGraphStorageKey {
		if d.Workflow != "bug" {
			return d, graphInvariant("invalid standalone graph ownership")
		}
		d.Mode = ""
	} else if d.Workflow != "" && d.Workflow != d.Mode {
		return d, graphInvariant("conflicting graph workflow ownership")
	}
	d.TeamSnapshotDigest = snapshot.String
	var err error
	d.CreatedAt, err = ParseTime(created)
	return d, err
}

// The historical SQL mode column requires a nonempty value. This reserved storage
// key is never exposed as a Mode; standalone ownership lives in workflow.
const standaloneGraphStorageKey = "workflow:bug"
