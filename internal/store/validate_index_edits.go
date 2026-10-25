package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// SupersedeValidateIndexEdit cancels an unattempted local index edit whose
// retained report binding has moved. No provider receipt is recorded. The
// immutable source parts and manifest remain available for audit.
func (db *DB) SupersedeValidateIndexEdit(ctx context.Context, key string, instanceID int64, revision, partialDigest, finalDigest string) error {
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		d, err := scanReportDelivery(tx.QueryRowContext(ctx, `SELECT `+reportDeliveryColumns+` FROM report_deliveries WHERE delivery_key=?`, key))
		if err != nil {
			return err
		}
		if !strings.HasPrefix(d.Purpose, "validate-guidance-index:") || d.TriggerNamespace != PlanNoticeNamespace || d.CommentID != 0 || d.PossiblySent {
			return errors.New("only unattempted local Validate index edits can be superseded")
		}
		var latest int64
		if err = tx.QueryRowContext(ctx, `SELECT id FROM graph_instances WHERE session_id=? AND definition_id IN (SELECT id FROM graph_definitions WHERE mode='validate') ORDER BY id DESC LIMIT 1`, d.SessionID).Scan(&latest); err != nil {
			return err
		}
		var currentRevision, currentDigest string
		if err = tx.QueryRowContext(ctx, `SELECT revision,digest FROM validate_reports WHERE instance_id=?`, instanceID).Scan(&currentRevision, &currentDigest); err != nil {
			return err
		}
		if latest == instanceID && currentRevision == revision && (currentDigest == partialDigest || currentDigest == finalDigest) {
			return errors.New("current Validate index edit cannot be superseded")
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM report_deliveries WHERE delivery_key=? AND comment_id IS NULL AND completed=0 AND possibly_sent=0`, key)
		return err
	})
	if err != nil {
		return fmt.Errorf("store: supersede Validate index edit: %w", err)
	}
	return nil
}
