package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrArchiveRowsLeft reports that the family still has rows of an archived
// table, so its skeleton must stay.
var ErrArchiveRowsLeft = errors.New("archive family still has archivable rows")

// forgetStep is one skeleton delete: the table and a selector template taking
// the family's session ids or task refs.
type forgetStep struct {
	table string
	by    string
}

const (
	forgetBySessions = `session_id IN (%s)`
	forgetByTasks    = `parent_task IN (%s)`
	forgetBySegments = `segment_id IN (SELECT id FROM segments WHERE session_id IN (%s))`
	forgetByID       = `id IN (%s)`
)

// forgetSteps delete the family skeleton children first. Every table that holds
// a foreign key into sessions, runs, segments, managed_stories or
// managed_features, and the tables keyed by the same identity, is listed, so the
// final sessions delete cannot hit a reference.
var forgetSteps = []forgetStep{
	{"segment_deliveries", forgetBySegments},
	{"story_refinement_acks", forgetBySessions},
	{"story_refinements", forgetBySessions},
	{"story_discovery", forgetBySessions},
	{"story_discovery_pending", forgetBySessions},
	{"story_discovery_rotation", forgetBySessions},
	{"mode_selections", forgetBySessions},
	{"workflow_admissions", forgetBySessions},
	{"watermarks", forgetBySessions},
	{"retirement_intents", forgetBySessions},
	{"runs", forgetBySessions},
	{"segments", forgetBySessions},
	{"approved_story_revisions", forgetByTasks},
	{"story_scope_notices", forgetByTasks},
	{"story_publications", forgetByTasks},
	{"story_revision_operations", forgetByTasks},
	{"managed_stories", forgetByTasks},
	{"story_contract_upgrade_items", forgetByTasks},
	{"story_contract_upgrades", forgetByTasks},
	{"managed_parent_closures", forgetByTasks},
	{"managed_features", forgetByTasks},
	{"sessions", forgetByID},
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// ArchiveFamilyIssues returns the issue numbers of every session in the family,
// the owner's first.
func (db *DB) ArchiveFamilyIssues(ctx context.Context, f ArchiveFamily) ([]int, error) {
	rows, err := db.sql.QueryContext(ctx, archiveCTE+`SELECT issue_number FROM sessions WHERE id IN (SELECT id FROM fs) ORDER BY (id=:sid) DESC, issue_number`, archiveNamed(f)...)
	if err != nil {
		return nil, fmt.Errorf("read family issues: %w", err)
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("read family issue: %w", err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ForgetArchivedFamily deletes the family's skeleton in one transaction, children
// first, once nothing is left to archive. It rechecks that the family is still
// settled and that no row of an archived table remains (ErrArchiveRowsLeft), and
// it returns the rows deleted per table. Any failure rolls everything back. The
// caller proves, under the same locks, that no resource, archive file or run log
// of the family is left.
func (db *DB) ForgetArchivedFamily(ctx context.Context, f ArchiveFamily, now time.Time) (map[string]int64, error) {
	deleted := make(map[string]int64)
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var fk int
		if err := tx.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&fk); err != nil {
			return fmt.Errorf("read foreign_keys pragma: %w", err)
		}
		if fk != 1 {
			return errors.New("foreign key enforcement is off")
		}
		if err := recheckArchiveSettled(ctx, tx, f, now); err != nil {
			return err
		}
		left, err := countArchivable(ctx, tx, f)
		if err != nil {
			return err
		}
		if left != 0 {
			return fmt.Errorf("%w: %d rows", ErrArchiveRowsLeft, left)
		}
		ids, err := forgetFamilyIDs(ctx, tx, f)
		if err != nil {
			return err
		}
		tasks, err := planArchiveParents(ctx, tx, f)
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return fmt.Errorf("%w: family has no sessions", ErrNotFound)
		}
		for _, s := range forgetSteps {
			var args []any
			var list string
			switch s.by {
			case forgetByTasks:
				list = placeholders(len(tasks))
				for _, t := range tasks {
					args = append(args, t)
				}
			default:
				list = placeholders(len(ids))
				for _, id := range ids {
					args = append(args, id)
				}
			}
			res, err := tx.ExecContext(ctx, `DELETE FROM `+s.table+` WHERE `+fmt.Sprintf(s.by, list), args...)
			if err != nil {
				return fmt.Errorf("forget %s: %w", s.table, err)
			}
			n, err := res.RowsAffected()
			if err != nil {
				return fmt.Errorf("forget %s: %w", s.table, err)
			}
			if n > 0 {
				deleted[s.table] = n
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return deleted, nil
}

func forgetFamilyIDs(ctx context.Context, q archiveQuerier, f ArchiveFamily) ([]int64, error) {
	rows, err := q.QueryContext(ctx, archiveCTE+`SELECT id FROM fs ORDER BY id`, archiveNamed(f)...)
	if err != nil {
		return nil, fmt.Errorf("read family sessions: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("read family session: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
