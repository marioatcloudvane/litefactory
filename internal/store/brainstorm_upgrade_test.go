package store

import (
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
)

// TestBrainstormScanJobsMigrationPreservesSharedJobHistory seeds the shared
// job tables at the schema before the brainstorm scan rebuild and proves the
// rebuild keeps every row, id, fence, the attempt sequence high-water mark, the
// worker slot, the notices and the meter children.
func TestBrainstormScanJobsMigrationPreservesSharedJobHistory(t *testing.T) {
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	applyMigrationsThrough(t, db, migrationVersionBefore(t, "brainstorm_scan_jobs"))
	ctx := t.Context()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.sql.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}

	owner := connectorTestOwner(t, db, 42)
	at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	stamp := FormatTime(at)
	for _, id := range []string{"p1", "p2", "p3", "p4"} {
		p := OwnerAction{ID: id, PolicyVersion: 1, Principal: OwnerActionPrincipal{OwnerID: "42", OwnerGeneration: owner.Generation, Transport: "discord", ChannelID: "200", DiscordOwnerID: "100"}, RequestKey: id, RequestDigest: "request", Facts: `{}`, Payload: `{}`, Digest: "digest", Scope: "standards:management", Tier: "conversational", State: "awaiting_confirmation", CreatedAt: at, AvailableAt: at, ConfirmNotBefore: at.Add(5 * time.Second), ExpiresAt: at.Add(15 * time.Minute), ExecutionID: "owner-action:" + id}
		if err := db.CreateOwnerAction(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO task_discussions(id,owner_id,owner_generation,repository_ref,task_ref,client_id,request_key,created_at,updated_at) VALUES('conversation',42,1,'github:acme/api','github:acme/api#1','client','open',?,?)`, stamp, stamp)
	for _, id := range []string{"a1", "a2", "a3"} {
		exec(`INSERT INTO advisory_admissions(id,conversation_id,owner_id,owner_generation,repository_ref,task_ref,operation,request_key,digest,basis,context,question,grant_id,consent_revision,conversation_revision,job_id,execution_id,created_at) VALUES(?,'conversation',42,1,'github:acme/api','github:acme/api#1','ask',?,'digest','{}','[]','Why?','grant','1',0,?,?,?)`, id, id, "job-"+id, "execution-"+id, stamp)
	}
	jobs := []struct{ id, kind, state string }{
		{"job-queued", "confirmed_workflow", "queued"},
		{"job-running", "confirmed_workflow", "running"},
		{"job-done", "confirmed_workflow", "succeeded"},
		{"job-a1", "advisory", "stop_requested"},
		{"job-a2", "advisory", "queued"},
		{"job-a3", "advisory", "cancelled"},
	}
	for i, j := range jobs {
		exec(`INSERT INTO durable_workflow_jobs(id,owner_id,owner_generation,snapshot,state,revision,result,summary,created_at,updated_at,admission_kind) VALUES(?,'42',?,'{"definition":{"ref":"test"}}',?,?,'{"kept":true}','summary',?,?,?)`, j.id, owner.Generation, j.state, i+1, stamp, stamp, j.kind)
	}
	attempts := []struct {
		id                          int
		job, proposal, advisory     any
		state                       string
		fence                       int
		started, deadline, finished string
	}{
		{1, "job-queued", "p1", nil, "queued", 0, "", "", ""},
		{2, "job-running", "p2", nil, "running", 3, stamp, FormatTime(at.Add(time.Hour)), ""},
		{3, "job-done", "p3", nil, "succeeded", 1, stamp, FormatTime(at.Add(time.Hour)), stamp},
		{4, "job-a1", nil, "a1", "stop_requested", 2, stamp, FormatTime(at.Add(time.Hour)), ""},
		{5, "job-a2", nil, "a2", "queued", 0, "", "", ""},
		{6, "job-a3", nil, "a3", "cancelled", 0, "", "", stamp},
		{7, "job-done", "p4", nil, "failed", 4, stamp, stamp, stamp},
	}
	for _, a := range attempts {
		kind := "confirmed_workflow"
		if a.advisory != nil {
			kind = "advisory"
		}
		exec(`INSERT INTO durable_workflow_attempts(id,job_id,proposal_id,execution_id,state,fence,queued_at,started_at,deadline,finished_at,admission_kind,advisory_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
			a.id, a.job, a.proposal, "execution-attempt-"+strconv.Itoa(a.id), a.state, a.fence, stamp, a.started, a.deadline, a.finished, kind, a.advisory)
	}
	// The highest historical attempt is deleted; its id must never be reused.
	exec(`DELETE FROM durable_workflow_attempts WHERE id=7`)
	exec(`UPDATE durable_workflow_worker SET attempt_id=2, generation=11, expires_at=?, heartbeat_at=? WHERE singleton=1`, FormatTime(at.Add(time.Minute)), stamp)
	exec(`INSERT INTO durable_workflow_notices(job_id,revision,event,destination,state,body,message_id,created_at) VALUES('job-done',3,'succeeded','discord:200','delivered','done','message-1',?)`, stamp)
	exec(`INSERT INTO durable_workflow_notices(job_id,revision,event,destination,state,body,created_at) VALUES('job-a1',4,'stop_requested','discord:200','pending','stopping',?)`, stamp)

	session := testSession(t, db, 987)
	run := testRun(t, db, session.ID, 1)
	if _, err = db.SetMeterPrices(ctx, "codex", "model", rateEntry("model", "1.25")); err != nil {
		t.Fatal(err)
	}
	workflow, err := db.StartMeterAttempt(ctx, run.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.StartChatMeterAttempt(ctx, session.RepoID, "codex"); err != nil {
		t.Fatal(err)
	}
	if err = db.PutMeterInvocation(ctx, MeterInvocation{AttemptID: workflow.ID, ID: "root", SourceID: "s", SourceInvocationID: "root", Status: "partial"}); err != nil {
		t.Fatal(err)
	}
	baseline := int64(0)
	checkpoint := meterObservation(workflow, "baseline")
	checkpoint.CounterKey = "counter"
	checkpoint.CounterEpoch = "epoch"
	checkpoint.Semantics = "cumulative/exclusive"
	if err = db.SeedMeterCheckpoint(ctx, checkpoint, MeterTokens{Output: &baseline}); err != nil {
		t.Fatal(err)
	}
	n := int64(7)
	model := "model"
	if err = db.RecordMeterEvent(ctx, workflow.ID, "codex", agent.UsageEvent{Observation: &agent.UsageObservation{AttemptID: workflow.ID, SourceID: "s", EventKey: "one", InvocationID: "root", ActualModel: &model, ModelProvenance: "provider", Mode: agent.CounterIncremental, Scope: agent.UsageExclusive, Tokens: agent.TokenUsage{Output: &n}, Coverage: agent.UsageCoverage{Status: agent.CollectionPartial}}}); err != nil {
		t.Fatal(err)
	}

	tables := []string{"durable_workflow_jobs", "durable_workflow_attempts", "durable_workflow_notices", "durable_workflow_worker", "advisory_admissions", "owner_action_proposals",
		"meter_attempts", "meter_invocations", "meter_observations", "meter_checkpoints", "meter_measurements", "meter_valuations", "meter_valuation_dimensions"}
	type retained struct{ columns, rows []string }
	before := map[string]retained{}
	for _, table := range tables {
		columns, rows := advisoryMeterTableRows(t, db, table, nil)
		if len(rows) == 0 {
			t.Fatalf("empty migration fixture %s", table)
		}
		before[table] = retained{columns, rows}
	}
	schemaBefore := upgradeIndexAndTriggerSchema(t, db)
	sequenceBefore := upgradeSequences(t, db)
	if sequenceBefore["durable_workflow_attempts"] != 7 {
		t.Fatalf("fixture high-water mark: %v", sequenceBefore)
	}

	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	for _, table := range tables {
		_, rows := advisoryMeterTableRows(t, db, table, before[table].columns)
		if !reflect.DeepEqual(rows, before[table].rows) {
			t.Fatalf("migration changed %s rows:\nbefore %v\nafter  %v", table, before[table].rows, rows)
		}
	}
	schemaAfter := upgradeIndexAndTriggerSchema(t, db)
	for name, sql := range schemaBefore {
		if schemaAfter[name] != sql {
			t.Fatalf("migration lost index or trigger %s: %q", name, schemaAfter[name])
		}
	}
	if _, ok := schemaAfter["durable_workflow_active_job"]; !ok {
		t.Fatal("durable_workflow_active_job missing")
	}
	sequenceAfter := upgradeSequences(t, db)
	for name, seq := range sequenceBefore {
		if sequenceAfter[name] != seq {
			t.Fatalf("sequence %s changed from %d to %d", name, seq, sequenceAfter[name])
		}
	}
	var attemptID, generation int64
	if err = db.sql.QueryRowContext(ctx, `SELECT attempt_id,generation FROM durable_workflow_worker WHERE singleton=1`).Scan(&attemptID, &generation); err != nil || attemptID != 2 || generation != 11 {
		t.Fatalf("worker slot moved: %d %d %v", attemptID, generation, err)
	}
	var foreignKeys, legacy int
	if err = db.sql.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil || foreignKeys != 1 {
		t.Fatalf("foreign keys not restored: %d %v", foreignKeys, err)
	}
	if err = db.sql.QueryRowContext(ctx, `PRAGMA legacy_alter_table`).Scan(&legacy); err != nil || legacy != 0 {
		t.Fatalf("legacy_alter_table not restored: %d %v", legacy, err)
	}
	assertNoForeignKeyViolations(t, db)
	var scanned int
	if err = db.sql.QueryRowContext(ctx, `SELECT count(*) FROM durable_workflow_attempts WHERE brainstorm_scan_id IS NOT NULL`).Scan(&scanned); err != nil || scanned != 0 {
		t.Fatalf("historical attempts gained a scan owner: %d %v", scanned, err)
	}
	if err = db.sql.QueryRowContext(ctx, `SELECT count(*) FROM meter_attempts WHERE brainstorm_scan_id IS NOT NULL`).Scan(&scanned); err != nil || scanned != 0 {
		t.Fatalf("historical meter attempts gained a scan owner: %d %v", scanned, err)
	}
	// A new attempt continues after the deleted high-water mark.
	exec(`INSERT INTO durable_workflow_jobs(id,owner_id,owner_generation,snapshot,state,created_at,updated_at) VALUES('job-new','42',1,'{}','queued',?,?)`, stamp, stamp)
	result, err := db.sql.ExecContext(ctx, `INSERT INTO durable_workflow_attempts(job_id,proposal_id,execution_id,state,queued_at) VALUES('job-new','p4','execution-new','queued',?)`, stamp)
	if err != nil {
		t.Fatal(err)
	}
	if id, err := result.LastInsertId(); err != nil || id != 8 {
		t.Fatalf("attempt id reused below the high-water mark: %d %v", id, err)
	}
	// The one-owner rule still refuses a confirmed attempt with a second owner.
	if _, err = db.sql.ExecContext(ctx, `UPDATE durable_workflow_attempts SET advisory_id='a1' WHERE id=8`); err == nil {
		t.Fatal("confirmed attempt acquired a second owner")
	}
	if _, err = db.sql.ExecContext(ctx, `UPDATE meter_attempts SET brainstorm_scan_id='missing-scan' WHERE id=?`, workflow.ID); err == nil {
		t.Fatal("historical meter attempt acquired a second owner")
	}
	// The pending notice still belongs to its job after the parent rebuild.
	if _, err = db.sql.ExecContext(ctx, `DELETE FROM durable_workflow_jobs WHERE id='job-a1'`); err == nil {
		t.Fatal("rebuilt job lost its dependent references")
	}
}

func upgradeIndexAndTriggerSchema(t *testing.T, db *DB) map[string]string {
	t.Helper()
	rows, err := db.sql.QueryContext(t.Context(), `SELECT name,sql FROM sqlite_master WHERE type IN ('index','trigger') AND sql IS NOT NULL AND (tbl_name LIKE 'durable_workflow_%' OR tbl_name LIKE 'meter_%')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	schema := map[string]string{}
	for rows.Next() {
		var name, sql string
		if err := rows.Scan(&name, &sql); err != nil {
			t.Fatal(err)
		}
		schema[name] = sql
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return schema
}

func upgradeSequences(t *testing.T, db *DB) map[string]int64 {
	t.Helper()
	rows, err := db.sql.QueryContext(t.Context(), `SELECT name,seq FROM sqlite_sequence`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	sequences := map[string]int64{}
	for rows.Next() {
		var name string
		var seq int64
		if err := rows.Scan(&name, &seq); err != nil {
			t.Fatal(err)
		}
		sequences[name] = seq
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return sequences
}

func assertNoForeignKeyViolations(t *testing.T, db *DB) {
	t.Helper()
	rows, err := db.sql.QueryContext(t.Context(), `PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("foreign key check reports a violation")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}
