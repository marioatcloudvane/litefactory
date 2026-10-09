package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/cloudvane/litefactory/internal/naming"
)

// DoctorPathSources reads routing metadata only, without logs, row contents,
// inventories or process observations. It does not create a workflow session.
type DoctorPathSources struct {
	Provider       string
	Worktree       string
	RunLogs        []string
	LogsTruncated  bool
	SessionMissing bool
}

// ReadDoctorPathSources supplies bounded location metadata for a diagnosis.
func (db *DB) ReadDoctorPathSources(ctx context.Context, ref naming.Ref) (out DoctorPathSources, err error) {
	if err := ref.Validate(); err != nil {
		return out, err
	}
	var id int64
	err = db.sql.QueryRowContext(ctx, `SELECT id,COALESCE(worktree_path,''),COALESCE((SELECT agent FROM segments WHERE session_id=s.id ORDER BY ordinal DESC LIMIT 1),'') FROM sessions s WHERE task_ref=?`, ref.String()).Scan(&id, &out.Worktree, &out.Provider)
	if errors.Is(err, sql.ErrNoRows) {
		out.SessionMissing = true
		return out, nil
	}
	if err != nil {
		return out, err
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT COALESCE(log_path,'') FROM runs WHERE session_id=? ORDER BY ordinal DESC LIMIT 101`, id)
	if err != nil {
		return out, err
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return out, err
		}
		if len(out.RunLogs) == 100 {
			out.LogsTruncated = true
			break
		}
		out.RunLogs = append(out.RunLogs, path)
	}
	return out, rows.Err()
}
