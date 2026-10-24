package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/naming"
)

const doctorIndexTemplate = "Doctor complete-content index pending confirmed part receipts."

// PrepareDoctorDelivery leaves an index pending until all content parts have
// confirmed receipts. It freezes the complete linked index before dispatch;
// subsequent attempts recover exactly those bytes.
func (db *DB) PrepareDoctorDelivery(ctx context.Context, key string) (out ReportDelivery, ready bool, err error) {
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = scanReportDelivery(tx.QueryRowContext(ctx, `SELECT `+reportDeliveryColumns+` FROM report_deliveries WHERE delivery_key=? AND doctor_command_id IS NOT NULL`, key))
		if err != nil {
			return err
		}
		if out.Purpose != "doctor-response-index" || out.Body != doctorIndexTemplate {
			ready = true
			return nil
		}
		if out.PossiblySent || out.CommentID != 0 || out.Completed {
			return errors.New("doctor index dispatched before finalization")
		}
		ref, err := naming.ParseRef(out.Destination)
		if err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT p.part,p.total,d.comment_id,d.completed FROM doctor_presentations p JOIN report_deliveries d ON d.delivery_key=p.delivery_key WHERE p.command_id=(SELECT doctor_command_id FROM report_deliveries WHERE delivery_key=?) AND d.purpose='doctor-response' ORDER BY p.part`, key)
		if err != nil {
			return err
		}
		var links strings.Builder
		count, total := 0, 0
		pending := false
		for rows.Next() {
			var part int
			var id sql.NullInt64
			var complete bool
			if err := rows.Scan(&part, &total, &id, &complete); err != nil {
				_ = rows.Close()
				return err
			}
			count++
			if part != count || !id.Valid || id.Int64 <= 0 || !complete {
				pending = true
			}
			fmt.Fprintf(&links, "- [Part %d/%d](%s#issuecomment-%d)\n", part, total-1, ref.IssueURL(), id.Int64)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if pending || count == 0 || count != total-1 {
			return nil
		}
		out.Body = "Doctor complete retained response\n\nAll numbered content parts are confirmed. Review content does not change diagnosis acceptance, proposal adoption or Apply requirements.\n\n" + links.String() + "\n" + out.Marker
		if _, err := tx.ExecContext(ctx, `UPDATE report_deliveries SET body=? WHERE delivery_key=? AND body=? AND possibly_sent=0 AND comment_id IS NULL AND completed=0`, out.Body, key, doctorIndexTemplate); err != nil {
			return err
		}
		ready = true
		return nil
	})
	return out, ready, err
}
