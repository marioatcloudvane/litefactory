package store

import "testing"

// MigrateBefore stands db at the schema immediately preceding the named
// migration so external tests can build pre-migration history.
func MigrateBefore(t *testing.T, db *DB, name string) {
	t.Helper()
	applyMigrationsThrough(t, db, migrationVersionBefore(t, name))
}

// SeedHistoricalReportDelivery inserts the columns present before confirmation
// metadata existed, so migration fixtures do not depend on current accessors.
func SeedHistoricalReportDelivery(t *testing.T, db *DB, d ReportDelivery) error {
	t.Helper()
	_, err := db.sql.ExecContext(t.Context(), `INSERT INTO report_deliveries(delivery_key,identity_version,session_id,trigger_id,run_id,task,destination,trigger_namespace,purpose,bot_user_id,body,marker,possibly_sent,comment_id,completed,legacy) VALUES(?,1,?,?,NULLIF(?,0),?,?,?,?,?,?,?, ?,NULLIF(?,0),?,?)`, d.Key, d.SessionID, d.TriggerID, d.RunID, d.Task, d.Destination, d.TriggerNamespace, d.Purpose, d.BotUserID, d.Body, d.Marker, d.PossiblySent, d.CommentID, d.Completed, d.Legacy)
	return err
}
