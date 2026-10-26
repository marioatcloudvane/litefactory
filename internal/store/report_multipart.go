package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ReportMultipart is a durable manifest in the existing report delivery ledger.
// Its source and part identities are immutable; remote receipts stay in each
// existing delivery row. No manifest is ever sent as an owner comment.
type ReportMultipart struct {
	Summary   string           `json:"summary"`
	Parts     []ReportDelivery `json:"parts"`
	FinalBody string           `json:"final_body,omitempty"`
}

// graphMultipartPartTx extends a graph's publication authority only to the
// exact child bytes and identity already retained in its multipart manifest.
// A namespace or claimed parent key alone never authorizes a running run.
func graphMultipartPartTx(ctx context.Context, tx *sql.Tx, child ReportDelivery) (bool, error) {
	if child.TriggerNamespace != "report-part" {
		return false, nil
	}
	identity, ok := strings.CutPrefix(child.Purpose, "run-report-part:")
	separator := strings.LastIndex(identity, ":")
	if !ok || separator <= 0 {
		return false, errors.New("multipart graph child lacks a parent identity")
	}
	parentKey := identity[:separator]
	parent, err := scanReportDelivery(tx.QueryRowContext(ctx, `SELECT `+reportDeliveryColumns+` FROM report_deliveries WHERE delivery_key=? AND trigger_namespace='report-multipart'`, parentKey))
	if err != nil {
		return false, err
	}
	graph, err := graphReportDeliveryTx(ctx, tx, parent)
	if err != nil || !graph {
		return false, err
	}
	if child.SessionID != parent.SessionID || child.RunID != parent.RunID || child.TriggerID != parent.TriggerID || child.Task != parent.Task || child.Destination != parent.Destination || child.BotUserID != parent.BotUserID || child.Legacy || child.PossiblySent {
		return false, errors.New("multipart graph child differs from parent authority")
	}
	var manifest ReportMultipart
	if err := json.Unmarshal([]byte(parent.Body), &manifest); err != nil {
		return false, err
	}
	if len(manifest.Parts) == 0 || len(manifest.Parts) > 32768 {
		return false, errors.New("invalid multipart graph manifest")
	}
	for i, part := range manifest.Parts {
		if part.Key != child.Key {
			continue
		}
		if part != child || child.Purpose != fmt.Sprintf("run-report-part:%s:%d/%d", parentKey, i+1, len(manifest.Parts)) {
			return false, errors.New("multipart graph child differs from retained part")
		}
		return true, nil
	}
	return false, errors.New("multipart graph child is absent from retained manifest")
}

func completeMultipartReportTx(ctx context.Context, tx *sql.Tx, d ReportDelivery) error {
	var manifest ReportMultipart
	if err := json.Unmarshal([]byte(d.Body), &manifest); err != nil {
		return err
	}
	if len(manifest.Parts) == 0 {
		return errors.New("multipart report has no parts")
	}
	for _, part := range manifest.Parts {
		saved, err := scanReportDelivery(tx.QueryRowContext(ctx, `SELECT `+reportDeliveryColumns+` FROM report_deliveries WHERE delivery_key=?`, part.Key))
		if err != nil {
			return err
		}
		if saved.TriggerNamespace != "report-part" || saved.Task != d.Task || saved.RunID != d.RunID || saved.SessionID != d.SessionID || saved.TriggerID != d.TriggerID || !saved.Completed || saved.CommentID <= 0 {
			return errors.New("multipart report has an unconfirmed or mismatched part")
		}
	}
	return nil
}

// FinalizeMultipartReport freezes the redacted linked summary before its first
// dispatch. A retry always uses the recorded bytes, even if provider display
// links or renderer configuration subsequently change.
func (db *DB) FinalizeMultipartReport(ctx context.Context, key, body string) (ReportDelivery, error) {
	var out ReportDelivery
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		d, err := scanReportDelivery(tx.QueryRowContext(ctx, `SELECT `+reportDeliveryColumns+` FROM report_deliveries WHERE delivery_key=? AND trigger_namespace='report-multipart'`, key))
		if err != nil {
			return err
		}
		var manifest ReportMultipart
		if err := json.Unmarshal([]byte(d.Body), &manifest); err != nil {
			return err
		}
		if manifest.FinalBody != "" {
			out = d
			return nil
		}
		if body == "" || d.PossiblySent || d.CommentID != 0 || d.Completed {
			return errors.New("multipart summary cannot be finalized after dispatch")
		}
		if err := completeMultipartReportTx(ctx, tx, d); err != nil {
			return err
		}
		manifest.FinalBody = body
		encoded, err := json.Marshal(manifest)
		if err != nil {
			return err
		}
		out, err = scanReportDelivery(tx.QueryRowContext(ctx, `UPDATE report_deliveries SET body=? WHERE delivery_key=? RETURNING `+reportDeliveryColumns, string(encoded), key))
		return err
	})
	return out, err
}
