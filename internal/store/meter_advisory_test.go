package store

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
)

func TestAdvisoryMeterCanonicalOwnershipPricesAndCrashSettlement(t *testing.T) {
	db, b := taskAdvisoryResultFixture(t)
	repo, err := db.GetRepoBySlug(t.Context(), "github", "acme", "api")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.GetOrCreateSession(t.Context(), repo, 1); err != nil {
		t.Fatal(err)
	}
	original, err := db.SetMeterPrices(t.Context(), "codex", "model", rateEntry("model", "1.25"))
	if err != nil {
		t.Fatal(err)
	}
	if err = db.StartAdvisoryInvocation(t.Context(), b, "codex", "model", "high", time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err = db.SetMeterPrices(t.Context(), "codex", "model", rateEntry("model", "9")); err != nil {
		t.Fatal(err)
	}
	meter, err := db.StartAdvisoryMeterAttempt(t.Context(), b.ExecutionID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if meter.AdvisoryExecutionID != b.ExecutionID || meter.RunID != 0 || meter.JudgeAttemptID != "" || meter.DoctorAttemptID != "" || meter.AuthoringSessionID != "" || meter.ChatRepoID != 0 || meter.PriceRevisionID != original {
		t.Fatalf("wrong canonical owner or catalog: %+v", meter)
	}
	if _, err = db.StartAdvisoryMeterAttempt(t.Context(), b.ExecutionID, "codex"); err == nil {
		t.Fatal("same private invocation created two canonical meter attempts")
	}
	if err = db.RecordMeterEvent(t.Context(), meter.ID, "codex", agent.UsageEvent{Invocation: &agent.UsageInvocation{AttemptID: meter.ID, ID: "root", SourceID: "s", SourceInvocationID: "root", Coverage: agent.UsageCoverage{Status: agent.CollectionPartial}}}); err != nil {
		t.Fatal(err)
	}
	n := int64(7)
	model := "model"
	if err = db.RecordMeterEvent(t.Context(), meter.ID, "codex", agent.UsageEvent{Observation: &agent.UsageObservation{AttemptID: meter.ID, SourceID: "s", EventKey: "one", InvocationID: "root", ActualModel: &model, ModelProvenance: "provider", Mode: agent.CounterIncremental, Scope: agent.UsageExclusive, Tokens: agent.TokenUsage{Output: &n}, Coverage: agent.UsageCoverage{Status: agent.CollectionPartial}}}); err != nil {
		t.Fatal(err)
	}
	attempt, err := db.WorkflowAttemptByExecution(t.Context(), b.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.ReconcileWorkflowAttempt(t.Context(), attempt, WorkflowFinish{State: "uncertain", Result: `{"answer":"Provider completion remains unknown"}`, Questions: `[]`}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	recovered, err := db.AdvisoryMeterAttempt(t.Context(), b.ExecutionID)
	if err != nil || recovered.ID != meter.ID || recovered.Outcome != "failure" {
		t.Fatalf("crash recovery lost canonical identity: %+v %v", recovered, err)
	}
	snapshot, err := db.NewMeterSnapshot(t.Context(), MeterFilter{RepoID: repo.ID, Issue: 1, Mode: "advisory"}, MeterSnapshotOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = snapshot.Close(t.Context()) }()
	rows, err := snapshot.Next(t.Context(), 100)
	if err != nil || len(rows) != 1 {
		t.Fatalf("advisory omitted from canonical task ledger: %+v %v", rows, err)
	}
	row := rows[0]
	if row.Project != "github:acme/api" || row.Issue != 1 || row.RunID != 0 || row.ExecutionPath != "advisory_ask" || row.Tokens.Output == nil || *row.Tokens.Output != 7 || row.PriceRevisionID != original || row.CollectionStatus != "partial" || row.EstimatedUSD != nil || row.PricedSubtotalUSD == nil {
		t.Fatalf("incorrect canonical snapshot: %+v", row)
	}
	if _, err = db.StartAdvisoryMeterAttempt(t.Context(), b.ExecutionID, "codex"); !errors.Is(err, ErrResultAuthority) {
		t.Fatalf("recovery relaunched canonical attempt: %v", err)
	}
}

func TestAdvisoryMeterMigrationPreservesAllExistingLedgerEvidence(t *testing.T) {
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	applyMigrationsThrough(t, db, migrationVersionBefore(t, "task_advisory_meter"))
	session := testSession(t, db, 987)
	run := testRun(t, db, session.ID, 1)
	if _, err = db.SetMeterPrices(t.Context(), "codex", "model", rateEntry("model", "1.25")); err != nil {
		t.Fatal(err)
	}
	workflow, err := db.StartMeterAttempt(t.Context(), run.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.StartChatMeterAttempt(t.Context(), session.RepoID, "codex"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.StartPresentationMeterAttempt(t.Context(), session.RepoID, "old-presentation", "generation", "codex"); err != nil {
		t.Fatal(err)
	}
	authoring := authoringTestSession(t, db)
	if _, err = db.BeginStandardsAuthoringAttempt(t.Context(), StandardsAuthoringAttempt{ID: "authoring-meter", Token: "token", SessionID: authoring.ID, InputVersion: authoring.Version, LeaseUntil: time.Now().UTC().Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.StartAuthoringMeterAttempt(t.Context(), authoring.ID, "codex", "model", "input", "authoring-meter", "token"); err != nil {
		t.Fatal(err)
	}
	doctor := doctorIOAttempt("old-doctor")
	doctor.Provider = "codex"
	if _, err = db.BeginDoctorAttempt(t.Context(), session.TaskRef, doctor); err != nil {
		t.Fatal(err)
	}
	doctor.Evidence = "retained"
	if err = db.PrepareDoctorAttempt(t.Context(), doctor); err != nil {
		t.Fatal(err)
	}
	if _, err = db.StartDoctorMeterAttempt(t.Context(), doctor.ID, "codex"); err != nil {
		t.Fatal(err)
	}
	sourceAttempt := resultRun(t, db, session, "research", "source", 2)
	binding, err := db.BindResultAttempt(t.Context(), sourceAttempt)
	if err != nil {
		t.Fatal(err)
	}
	record := resultRecord(binding, "saved", 0)
	record.Snapshot = []byte(`{"result":{"outcome":"completed","payload":{"research":{"findings":[{"text":"saved"}]}}}}`)
	if _, err = db.RecordResult(t.Context(), record); err != nil {
		t.Fatal(err)
	}
	source, err := db.ResolveJudgeSource(t.Context(), session.TaskRef, "")
	if err != nil {
		t.Fatal(err)
	}
	request, err := db.AdmitJudgeRequest(t.Context(), JudgeRequest{SessionID: session.ID, TriggerCommentID: 9, Task: session.TaskRef, TicketKey: "ticket", CommentID: "comment", Source: source, Configuration: []byte(`{}`), Evidence: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	judge, err := db.StartJudgeAttempt(t.Context(), request.ID, "codex", "model", "high")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.StartJudgeMeterAttempt(t.Context(), judge.ID, "codex"); err != nil {
		t.Fatal(err)
	}
	if err = db.PutMeterInvocation(t.Context(), MeterInvocation{AttemptID: workflow.ID, ID: "root", SourceID: "s", SourceInvocationID: "root", Status: "partial"}); err != nil {
		t.Fatal(err)
	}
	baseline := int64(0)
	checkpoint := meterObservation(workflow, "baseline")
	checkpoint.CounterKey = "counter"
	checkpoint.CounterEpoch = "epoch"
	checkpoint.Semantics = "cumulative/exclusive"
	if err = db.SeedMeterCheckpoint(t.Context(), checkpoint, MeterTokens{Output: &baseline}); err != nil {
		t.Fatal(err)
	}
	n := int64(7)
	model := "model"
	if err = db.RecordMeterEvent(t.Context(), workflow.ID, "codex", agent.UsageEvent{Observation: &agent.UsageObservation{AttemptID: workflow.ID, SourceID: "s", EventKey: "one", InvocationID: "root", ActualModel: &model, ModelProvenance: "provider", Mode: agent.CounterIncremental, Scope: agent.UsageExclusive, Tokens: agent.TokenUsage{Output: &n}, Coverage: agent.UsageCoverage{Status: agent.CollectionPartial}}}); err != nil {
		t.Fatal(err)
	}
	tables := []string{"meter_attempts", "meter_invocations", "meter_observations", "meter_checkpoints", "meter_measurements", "meter_valuations", "meter_valuation_dimensions"}
	type retained struct {
		columns []string
		rows    []string
	}
	before := map[string]retained{}
	for _, table := range tables {
		columns, rows := advisoryMeterTableRows(t, db, table, nil)
		if len(rows) == 0 {
			t.Fatalf("empty migration fixture %s", table)
		}
		before[table] = retained{columns, rows}
	}
	// Retain every existing explicit index and trigger as well as row bytes.
	schemaRows, err := db.sql.QueryContext(t.Context(), `SELECT name,sql FROM sqlite_master WHERE type IN ('index','trigger') AND sql IS NOT NULL AND tbl_name LIKE 'meter_%' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	schema := map[string]string{}
	for schemaRows.Next() {
		var name, sql string
		if err = schemaRows.Scan(&name, &sql); err != nil {
			t.Fatal(err)
		}
		schema[name] = sql
	}
	if err = schemaRows.Close(); err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		_, rows := advisoryMeterTableRows(t, db, table, before[table].columns)
		if !reflect.DeepEqual(rows, before[table].rows) {
			t.Fatalf("migration changed %s rows", table)
		}
	}
	for name, want := range schema {
		var got string
		if err = db.sql.QueryRowContext(t.Context(), `SELECT sql FROM sqlite_master WHERE name=?`, name).Scan(&got); err != nil || got != want {
			t.Fatalf("migration lost schema %s: %q %v", name, got, err)
		}
	}
	foreignKeys, err := db.sql.QueryContext(t.Context(), `PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	if foreignKeys.Next() {
		t.Fatal("migration broke retained foreign keys")
	}
	if err = foreignKeys.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetMeterAttempt(t.Context(), workflow.ID)
	if err != nil || got.RunID != run.ID || got.AdvisoryExecutionID != "" {
		t.Fatalf("old attempt reclassified: %+v %v", got, err)
	}
	if _, err = db.sql.ExecContext(t.Context(), `UPDATE meter_attempts SET advisory_execution_id='missing-execution' WHERE id=?`, workflow.ID); err == nil {
		t.Fatal("historical workflow acquired a second owner")
	}
}

func advisoryMeterTableRows(t *testing.T, db *DB, table string, columns []string) ([]string, []string) {
	t.Helper()
	selection := "*"
	if len(columns) > 0 {
		selection = strings.Join(columns, ",")
	}
	rows, err := db.sql.QueryContext(t.Context(), `SELECT `+selection+` FROM `+table)
	if err != nil {
		t.Fatal(err)
	}
	names, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var encoded []string
	for rows.Next() {
		values := make([]any, len(names))
		pointers := make([]any, len(names))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err = rows.Scan(pointers...); err != nil {
			t.Fatal(err)
		}
		raw, e := json.Marshal(values)
		if e != nil {
			t.Fatal(e)
		}
		encoded = append(encoded, string(raw))
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if err = rows.Close(); err != nil {
		t.Fatal(err)
	}
	sort.Strings(encoded)
	return names, encoded
}
