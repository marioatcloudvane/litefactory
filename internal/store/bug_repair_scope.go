package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// BugRepairScopeObservation is factory evidence over actual immutable changes,
// never an agent result. It records uncertainty as well as positive findings.
type BugRepairScopeObservation struct {
	SessionID                                                            int64
	Request, SourceCommit, SourceTree, DiffDigest, Disposition, Evidence string
	Paths                                                                []string
}

// RetainBugRepairScope stores the immutable diff and approved-boundary assessment for a repair.
func (d *DB) RetainBugRepairScope(ctx context.Context, in BugRepairScopeObservation) error {
	if in.SessionID <= 0 || in.Request == "" || !validSubmissionDigest(in.DiffDigest) || in.SourceCommit == "" || in.SourceTree == "" || strings.TrimSpace(in.Evidence) == "" || len(in.Evidence) > 65536 || len(in.Paths) > 256 {
		return errors.New("store: malformed Bug repair scope observation")
	}
	switch in.Disposition {
	case "within", "outside", "indeterminate":
	default:
		return errors.New("store: unknown repair scope disposition")
	}
	raw, err := json.Marshal(in.Paths)
	if err != nil {
		return err
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		r, err := bugRepairTx(ctx, tx, in.SessionID, in.Request)
		if err != nil {
			return err
		}
		if err = bugRepairApprovalTx(ctx, tx, r.BugRepairRequest); err != nil {
			return err
		}
		var tree, digest, disposition, evidence string
		var paths []byte
		err = tx.QueryRowContext(ctx, `SELECT source_tree,diff_digest,disposition,evidence,paths FROM bug_repair_scope_observations WHERE session_id=? AND request=? AND source_commit=?`, in.SessionID, in.Request, in.SourceCommit).Scan(&tree, &digest, &disposition, &evidence, &paths)
		if err == nil {
			if tree != in.SourceTree || digest != in.DiffDigest || disposition != in.Disposition || evidence != in.Evidence || string(paths) != string(raw) {
				return errors.New("store: immutable repair scope observation conflict")
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO bug_repair_scope_observations(session_id,request,source_commit,source_tree,diff_digest,disposition,evidence,paths) VALUES(?,?,?,?,?,?,?,?)`, in.SessionID, in.Request, in.SourceCommit, in.SourceTree, in.DiffDigest, in.Disposition, in.Evidence, raw); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE bug_execution SET revision=revision+1 WHERE session_id=?`, in.SessionID); err != nil {
			return err
		}
		return bugEventTx(ctx, tx, in.SessionID, "repair_scope", in.Request, in.Disposition)
	})
}
