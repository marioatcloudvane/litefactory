package store

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/ticket"
)

func TestProjectMigrationPreservesPopulatedSessionHistory(t *testing.T) {
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	applyMigrationsThrough(t, db, migrationVersionBefore(t, "project_connections"))
	repo := testRepo(t, db, "owner", "repo")
	session, err := db.GetOrCreateSession(t.Context(), repo, 7)
	if err != nil {
		t.Fatal(err)
	}
	session, err = db.FreezeSessionSlug(t.Context(), session.ID, "retained-slug", "lf/7-retained-slug")
	if err != nil {
		t.Fatal(err)
	}
	err = db.SetSessionWorktree(t.Context(), session.ID, filepath.Join(repo.WorktreeRoot, "task-7"))
	if err != nil {
		t.Fatal(err)
	}
	comment, err := db.RecordComment(t.Context(), Comment{SessionID: session.ID, ForgeCommentID: 123, AuthorHandle: "owner", AuthorUserID: 42, Body: "original\ncomment bytes", CreatedAt: time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC), Classification: ClassificationInstruction})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = db.BootstrapWatermark(t.Context(), session.ID, 123); err != nil {
		t.Fatal(err)
	}
	watermark, _, err := db.GetWatermark(t.Context(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	segment, err := db.GetOrCreateSegment(t.Context(), session.ID, 1, "retained-conversation", "claude")
	if err != nil {
		t.Fatal(err)
	}
	run := seedHistoricalRunSnapshot(t, db, AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: "spike", Agent: "claude", TriggerCommentID: 123, LogPath: func(int) string { return "/retained/log.jsonl" }}, RunSucceeded)
	body := "original prepared\nreport bytes <!-- lf:retained -->"
	// Seed historical delivery directly: its current allocator reads the current run schema.
	_, err = db.sql.ExecContext(t.Context(), `INSERT INTO report_deliveries(delivery_key,identity_version,session_id,run_id,trigger_id,task,destination,trigger_namespace,purpose,bot_user_id,body,marker) VALUES('retained-delivery',1,?,?,123,?,?,'comment','run_report',99,?,'<!-- lf:retained -->')`, session.ID, run.ID, session.TaskRef, session.TaskRef, body)
	if err != nil {
		t.Fatal(err)
	}
	delivery, err := db.GetReportDelivery(t.Context(), "retained-delivery")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.BindLegacySessionOrigin(t.Context(), session.ID, "https://api.github.com"); err != nil {
		t.Fatal(err)
	}
	ref := resolvedSessionTicket(repo.Slug(), "700")
	binding, err := db.BindSessionTicket(t.Context(), session.ID, ref)
	if err != nil {
		t.Fatal(err)
	}
	effect, err := db.BindTicketEffect(t.Context(), delivery.Key, ref.Locator, ticket.Identity{Provider: ticket.GitHub, Origin: ref.Identity.Origin, RemoteID: "99"})
	if err != nil {
		t.Fatal(err)
	}
	effect, err = db.ResolveTicketEffect(t.Context(), effect.Key, ref)
	if err != nil {
		t.Fatal(err)
	}
	beforeSession, err := db.GetSession(t.Context(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	beforeSchema := sessionSchemaObjects(t, db)
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if afterSchema := sessionSchemaObjects(t, db); !reflect.DeepEqual(beforeSchema, afterSchema) {
		t.Fatalf("migration changed session indexes or guards: before=%v after=%v", beforeSchema, afterSchema)
	}
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	afterSession, err := db.GetSession(t.Context(), session.ID)
	if err != nil || !reflect.DeepEqual(beforeSession, afterSession) {
		t.Fatalf("session history changed before=%+v after=%+v err=%v", beforeSession, afterSession, err)
	}
	afterComment, err := db.GetComment(t.Context(), session.ID, 123)
	if err != nil || !reflect.DeepEqual(comment, afterComment) {
		t.Fatalf("comment changed %+v %v", afterComment, err)
	}
	afterWatermark, _, err := db.GetWatermark(t.Context(), session.ID)
	if err != nil || !reflect.DeepEqual(watermark, afterWatermark) {
		t.Fatalf("watermark changed %+v %v", afterWatermark, err)
	}
	afterSegment, err := db.GetSegment(t.Context(), segment.ID)
	if err != nil || !reflect.DeepEqual(segment, afterSegment) {
		t.Fatalf("segment changed %+v %v", afterSegment, err)
	}
	afterRun, err := db.GetRun(t.Context(), run.ID)
	if err != nil || !reflect.DeepEqual(run, afterRun) {
		t.Fatalf("run changed %+v %v", afterRun, err)
	}
	afterDelivery, err := db.GetReportDelivery(t.Context(), delivery.Key)
	if err != nil || !reflect.DeepEqual(delivery, afterDelivery) {
		t.Fatalf("delivery changed %+v %v", afterDelivery, err)
	}
	afterBinding, err := db.GetSessionTicket(t.Context(), session.ID)
	if err != nil || !reflect.DeepEqual(binding, afterBinding) {
		t.Fatalf("ticket mapping changed %+v %v", afterBinding, err)
	}
	afterEffect, err := db.GetTicketEffect(t.Context(), effect.Key)
	if err != nil || !reflect.DeepEqual(effect, afterEffect) {
		t.Fatalf("effect mapping changed %+v %v", afterEffect, err)
	}
	var counts [7]int
	if err = db.sql.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM sessions),(SELECT count(*) FROM comments),(SELECT count(*) FROM watermarks),(SELECT count(*) FROM segments),(SELECT count(*) FROM runs),(SELECT count(*) FROM report_deliveries),(SELECT count(*) FROM session_ticket_identities)`).Scan(&counts[0], &counts[1], &counts[2], &counts[3], &counts[4], &counts[5], &counts[6]); err != nil {
		t.Fatal(err)
	}
	if counts != [7]int{1, 1, 1, 1, 1, 1, 1} {
		t.Fatalf("migration changed history counts %v", counts)
	}
	var foreignKeys int
	if err = db.sql.QueryRowContext(t.Context(), `PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil || foreignKeys != 1 {
		t.Fatalf("foreign keys disabled %d %v", foreignKeys, err)
	}
	rows, err := db.sql.QueryContext(t.Context(), `PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("migration left dangling foreign keys")
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func sessionSchemaObjects(t *testing.T, db *DB) map[string]string {
	t.Helper()
	rows, err := db.sql.QueryContext(t.Context(), `SELECT name,sql FROM sqlite_master WHERE tbl_name='sessions' AND type IN ('index','trigger') AND sql IS NOT NULL`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	objects := make(map[string]string)
	for rows.Next() {
		var name, statement string
		if err := rows.Scan(&name, &statement); err != nil {
			t.Fatal(err)
		}
		objects[name] = statement
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return objects
}
