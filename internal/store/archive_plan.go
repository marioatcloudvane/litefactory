package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

// planArchiveGrantsTable names the table whose rows let the archive's delete
// transaction pass the protective delete triggers of the Plan history tables.
//
// This constant is the only place the table name appears in Go source.
// [writePlanArchiveGrants], in this file, is the only function that reads or
// writes the table through it; the archived-class registry only classifies it.
// Every other mention, including comments, must say "grant table" instead.
const planArchiveGrantsTable = "plan_archive_grants"

type planGrantOp int

const (
	planGrantAdd planGrantOp = iota
	planGrantRemove
	planGrantCount
	planGrantPurge
)

type planGrantExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// writePlanArchiveGrants is the single store function that touches the grant
// table. Add inserts a grant per parent, Remove deletes them, Count returns how
// many rows exist and Purge deletes every row, tolerating a database that has
// not been migrated yet. It returns the count for Count and rows affected
// otherwise.
func writePlanArchiveGrants(ctx context.Context, x planGrantExecer, op planGrantOp, parents []string) (int64, error) {
	switch op {
	case planGrantAdd, planGrantRemove:
		var total int64
		for _, p := range parents {
			q := `INSERT OR IGNORE INTO ` + planArchiveGrantsTable + `(parent) VALUES(?)`
			if op == planGrantRemove {
				q = `DELETE FROM ` + planArchiveGrantsTable + ` WHERE parent=?`
			}
			res, err := x.ExecContext(ctx, q, p)
			if err != nil {
				return total, fmt.Errorf("write Plan archive grant: %w", err)
			}
			n, err := res.RowsAffected()
			if err != nil {
				return total, fmt.Errorf("write Plan archive grant: %w", err)
			}
			total += n
		}
		return total, nil
	case planGrantCount:
		var n int64
		if err := x.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+planArchiveGrantsTable).Scan(&n); err != nil {
			return 0, fmt.Errorf("count Plan archive grants: %w", err)
		}
		return n, nil
	case planGrantPurge:
		var present int64
		if err := x.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+planArchiveGrantsTable).Scan(&present); err != nil {
			if strings.Contains(err.Error(), "no such table") {
				return 0, nil
			}
			return 0, fmt.Errorf("purge Plan archive grants: %w", err)
		}
		if present == 0 {
			return 0, nil
		}
		res, err := x.ExecContext(ctx, `DELETE FROM `+planArchiveGrantsTable)
		if err != nil {
			return 0, fmt.Errorf("purge Plan archive grants: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("purge Plan archive grants: %w", err)
		}
		return n, nil
	}
	return 0, errors.New("unknown Plan archive grant operation")
}

// purgeStaleArchiveGrants runs when the store opens. A grant only ever lives
// inside the delete transaction, so any committed row means the protection was
// bypassed or a crash left it behind; the rows are removed and the event logged.
func (db *DB) purgeStaleArchiveGrants(ctx context.Context) error {
	n, err := writePlanArchiveGrants(ctx, db.sql, planGrantPurge, nil)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	if n > 0 {
		slog.Default().ErrorContext(ctx, "stale Plan archive grants removed", slog.Int64("rows", n), slog.String("database", db.path))
	}
	return nil
}

// planArchiveParents lists the Plan parents of the family: its task refs.
func planArchiveParents(ctx context.Context, q archiveQuerier, f ArchiveFamily) ([]string, error) {
	rows, err := q.QueryContext(ctx, archiveCTE+`SELECT task_ref FROM ft ORDER BY task_ref`, archiveNamed(f)...)
	if err != nil {
		return nil, fmt.Errorf("read Plan parents: %w", err)
	}
	defer rows.Close()
	var parents []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("read Plan parent: %w", err)
		}
		parents = append(parents, p)
	}
	return parents, rows.Err()
}
