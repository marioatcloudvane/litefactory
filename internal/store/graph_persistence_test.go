package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func graphTestTime() time.Time { return time.Date(2026, 9, 18, 10, 0, 0, 123000000, time.UTC) }

func graphTestDefinition(t *testing.T, db *DB) GraphDefinition {
	t.Helper()
	d, err := db.CreateGraphDefinition(t.Context(), GraphDefinition{Team: "core", Mode: "implement", ProfileVersion: 1, Path: "core/implement.dot", Digest: strings.Repeat("a", 64), CreatedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func graphTestInstance(t *testing.T, db *DB) GraphInstance {
	t.Helper()
	s := testSession(t, db, 315)
	d := graphTestDefinition(t, db)
	i, err := db.CreateGraphInstance(t.Context(), GraphInstance{SessionID: s.ID, DefinitionID: d.ID, CurrentNode: "work", StartedAt: graphTestTime(), NextDueAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	return i
}

func graphTestAttempt(t *testing.T, db *DB, i GraphInstance, limit int) GraphNodeAttempt {
	t.Helper()
	a, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: i.ID, Node: i.CurrentNode, StartedAt: graphTestTime(), Limit: limit})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestGraphSchemaMigration(t *testing.T) {
	db := newTestDB(t)
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	version, err := db.SchemaVersion(t.Context())
	if err != nil {
		t.Fatalf("version=%d err=%v", version, err)
	}
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if want := migrations[len(migrations)-1].version; version != want {
		t.Fatalf("schema version=%d, latest embedded migration=%d", version, want)
	}
	var applied int
	if err := db.sql.QueryRowContext(t.Context(), "SELECT count(*) FROM schema_migrations").Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != len(migrations) {
		t.Fatalf("applied migrations=%d, embedded migrations=%d", applied, len(migrations))
	}
	tables := []string{"graph_definitions", "graph_instances", "graph_node_attempts", "graph_outputs", "graph_transitions"}
	for _, table := range tables {
		var ddl string
		if err := db.sql.QueryRowContext(t.Context(), "SELECT sql FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&ddl); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.ToLower(ddl), "workflow_") {
			t.Errorf("forbidden schema naming: %s", ddl)
		}
	}
	// Include implicit UNIQUE indexes as well as named lookup indexes.
	for _, tc := range []struct {
		table, columns string
		unique         int
	}{
		{"graph_definitions", "team,mode,digest", 1}, {"graph_instances", "next_due_at,id", 0},
		{"graph_node_attempts", "instance_id,node,attempt", 1}, {"graph_transitions", "instance_id,id", 0}, {"graph_outputs", "node_attempt_id,id", 0},
	} {
		var count int
		err := db.sql.QueryRowContext(t.Context(), `SELECT count(*) FROM pragma_index_list(?) AS i WHERE i."unique"=? AND (SELECT group_concat(name, ',') FROM pragma_index_info(i.name))=?`, tc.table, tc.unique, tc.columns).Scan(&count)
		if err != nil || count != 1 {
			t.Errorf("index %s(%s): count=%d err=%v", tc.table, tc.columns, count, err)
		}
	}
	for _, tc := range []struct{ table, column, target string }{
		{"graph_instances", "session_id", "sessions"}, {"graph_instances", "definition_id", "graph_definitions"},
		{"graph_node_attempts", "instance_id", "graph_instances"}, {"graph_node_attempts", "run_id", "runs"},
		{"graph_outputs", "node_attempt_id", "graph_node_attempts"}, {"graph_transitions", "instance_id", "graph_instances"},
	} {
		var count int
		err := db.sql.QueryRowContext(t.Context(), `SELECT count(*) FROM pragma_foreign_key_list(?) WHERE "from"=? AND "table"=?`, tc.table, tc.column, tc.target).Scan(&count)
		if err != nil || count != 1 {
			t.Errorf("FK %s.%s -> %s: count=%d err=%v", tc.table, tc.column, tc.target, count, err)
		}
	}
	var fk int
	if err := db.sql.QueryRowContext(t.Context(), "PRAGMA foreign_keys").Scan(&fk); err != nil || fk != 1 {
		t.Fatalf("foreign keys=%d err=%v", fk, err)
	}
	d := graphTestDefinition(t, db)
	var snapshot sql.NullString
	if err := db.sql.QueryRowContext(t.Context(), "SELECT team_snapshot_digest FROM graph_definitions WHERE id=?", d.ID).Scan(&snapshot); err != nil || snapshot.Valid {
		t.Fatalf("snapshot=%v err=%v", snapshot, err)
	}
	if _, err := db.CreateGraphInstance(t.Context(), GraphInstance{SessionID: 999999, DefinitionID: d.ID, CurrentNode: "work", StartedAt: graphTestTime()}); err == nil {
		t.Fatal("missing session FK accepted")
	}
}

func TestGraphDefinitionPinsAndPaths(t *testing.T) {
	db := newTestDB(t)
	i := graphTestInstance(t, db)
	d, err := db.GetGraphDefinition(t.Context(), i.DefinitionID)
	if err != nil {
		t.Fatal(err)
	}
	changed := d
	changed.Path = "other/implement.dot"
	changed.ProfileVersion = 2
	changed.TeamSnapshotDigest = "later"
	same, err := db.CreateGraphDefinition(t.Context(), changed)
	if err != nil || !reflect.DeepEqual(d, same) {
		t.Fatalf("pin changed: %+v err=%v", same, err)
	}
	changed = d
	changed.Digest = strings.Repeat("b", 64)
	next, err := db.CreateGraphDefinition(t.Context(), changed)
	if err != nil || next.ID == d.ID {
		t.Fatalf("new digest: %+v err=%v", next, err)
	}
	pinned, err := db.GetGraphInstance(t.Context(), i.ID)
	if err != nil || pinned.DefinitionID != d.ID {
		t.Fatalf("instance repinned: %+v err=%v", pinned, err)
	}
	original, err := db.GetGraphDefinition(t.Context(), pinned.DefinitionID)
	if err != nil || original.Digest != d.Digest {
		t.Fatalf("digest changed: %+v err=%v", original, err)
	}
	for _, path := range []string{"", "/core/implement.dot", "C:/core/implement.dot", "C:implement.dot", `core\implement.dot`, "./core.dot", "core/../x.dot", "core/./x.dot", "core//x.dot"} {
		t.Run(path, func(t *testing.T) {
			bad := d
			bad.Path = path
			_, err := db.CreateGraphDefinition(t.Context(), bad)
			var typed *GraphDefinitionPathError
			if !errors.Is(err, ErrGraphDefinitionPath) || !errors.As(err, &typed) || typed.Path != path {
				t.Fatalf("path error=%v", err)
			}
		})
	}
	for _, field := range []string{"team", "mode"} {
		isolated := d
		if field == "team" {
			isolated.Team = "other"
		} else {
			isolated.Mode = "research"
		}
		got, err := db.CreateGraphDefinition(t.Context(), isolated)
		if err != nil || got.ID == d.ID {
			t.Fatalf("%s isolation: %+v %v", field, got, err)
		}
	}
}

func TestGraphAttemptsInvariants(t *testing.T) {
	db := newTestDB(t)
	i := graphTestInstance(t, db)
	a := graphTestAttempt(t, db, i, 2)
	in := GraphAttemptStart{InstanceID: i.ID, Node: "work", StartedAt: graphTestTime(), Limit: 2}
	if _, err := db.StartGraphNodeAttempt(t.Context(), in); !errors.Is(err, ErrGraphAttemptRunning) {
		t.Fatalf("running refusal=%v", err)
	}
	if err := db.FinishGraphNodeAttempt(t.Context(), a.ID, GraphFailed, "bad", graphTestTime()); !errors.Is(err, ErrGraphInvariant) {
		t.Fatalf("failed outcome=%v", err)
	}
	got, err := db.GetGraphNodeAttempt(t.Context(), a.ID)
	if err != nil || !reflect.DeepEqual(got, a) {
		t.Fatalf("refusal modified attempt: %+v %v", got, err)
	}
	if err := db.FinishGraphNodeAttempt(t.Context(), a.ID, GraphSucceeded, "again", graphTestTime()); err != nil {
		t.Fatal(err)
	}
	before, err := db.GetGraphNodeAttempt(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.FinishGraphNodeAttempt(t.Context(), a.ID, GraphFailed, "", graphTestTime()); !errors.Is(err, ErrGraphAttemptTerminal) {
		t.Fatalf("terminal rewrite=%v", err)
	}
	after, err := db.GetGraphNodeAttempt(t.Context(), a.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("terminal changed: %+v %v", after, err)
	}
	// A completed row's dispatch identity remains unique independently of running status.
	if _, err := db.sql.ExecContext(t.Context(), `INSERT INTO graph_node_attempts(instance_id,node,attempt,status,started_at,finished_at) VALUES(?,?,?,'failed',?,?)`, i.ID, "work", a.Attempt, FormatTime(graphTestTime()), FormatTime(graphTestTime())); err == nil {
		t.Fatal("duplicate identity accepted")
	}
	second := graphTestAttempt(t, db, i, 2)
	if a.Attempt != 1 || second.Attempt != 2 {
		t.Fatalf("dispatch counts: %d %d", a.Attempt, second.Attempt)
	}
	if err := db.FinishGraphNodeAttempt(t.Context(), second.ID, GraphFailed, "", graphTestTime()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.StartGraphNodeAttempt(t.Context(), in); !errors.Is(err, ErrGraphAttemptLimit) {
		t.Fatalf("dispatch limit=%v", err)
	}
	other, err := db.CreateGraphInstance(t.Context(), GraphInstance{SessionID: i.SessionID, DefinitionID: i.DefinitionID, CurrentNode: "work", StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	independent := graphTestAttempt(t, db, other, 2)
	if independent.Attempt != 1 {
		t.Fatal("attempt counter leaked across instances")
	}
	wrongSession := testSession(t, db, 999)
	run := testRun(t, db, wrongSession.ID, 0)
	in.RunID = run.ID
	if _, err := db.StartGraphNodeAttempt(t.Context(), in); !errors.Is(err, ErrGraphInvariant) {
		t.Fatalf("cross-session run=%v", err)
	}
}

func TestGraphCompletionAtomicityAndTrace(t *testing.T) {
	db := newTestDB(t)
	i := graphTestInstance(t, db)
	a := graphTestAttempt(t, db, i, 3)
	transition := GraphTransition{InstanceID: i.ID, FromNode: "work", ToNode: "work", OnOutcome: "again", At: graphTestTime()}
	good := GraphCompletion{Status: GraphSucceeded, Outcome: "again", FinishedAt: graphTestTime(), Outputs: []GraphOutput{{Kind: GraphOutputText, Payload: []byte("first"), OwnerCommentID: 77}}, Transition: &transition, NextDueAt: graphTestTime().Add(time.Minute)}
	for _, failure := range []string{"output", "instance", "transition"} {
		t.Run(failure, func(t *testing.T) {
			bad := good
			switch failure {
			case "output":
				bad.Outputs = []GraphOutput{{Kind: GraphOutputText, Payload: []byte("rolled back")}, {Kind: "invalid"}}
			case "instance":
				bad.InstanceStatus = "invalid"
			case "transition":
				tr := transition
				tr.InstanceID++
				bad.Transition = &tr
			}
			if err := db.CompleteGraphNodeAttempt(t.Context(), a.ID, bad); err == nil {
				t.Fatal("invalid completion accepted")
			}
			got, err := db.GetGraphNodeAttempt(t.Context(), a.ID)
			if err != nil || !reflect.DeepEqual(got, a) {
				t.Fatalf("attempt not rolled back: %+v %v", got, err)
			}
			progress, err := db.GetGraphInstance(t.Context(), i.ID)
			if err != nil || !reflect.DeepEqual(progress, i) {
				t.Fatalf("progress not rolled back: %+v %v", progress, err)
			}
			outputs, err := db.ListGraphOutputs(t.Context(), a.ID)
			if err != nil || len(outputs) != 0 {
				t.Fatalf("outputs not rolled back: %v %v", outputs, err)
			}
			trace, err := db.ListGraphTransitions(t.Context(), i.ID)
			if err != nil || len(trace) != 0 {
				t.Fatalf("trace not rolled back: %v %v", trace, err)
			}
		})
	}
	if err := db.CompleteGraphNodeAttempt(t.Context(), a.ID, good); err != nil {
		t.Fatal(err)
	}
	first, err := db.ListGraphTransitions(t.Context(), i.ID)
	if err != nil || len(first) != 1 {
		t.Fatalf("trace=%v %v", first, err)
	}
	outputs, err := db.ListGraphOutputs(t.Context(), a.ID)
	if err != nil || len(outputs) != 1 || string(outputs[0].Payload) != "first" || outputs[0].OwnerCommentID != 77 {
		t.Fatalf("output=%v %v", outputs, err)
	}
	second := graphTestAttempt(t, db, i, 3)
	end := transition
	end.ToNode = "done"
	end.OnOutcome = "done"
	completion := GraphCompletion{Status: GraphSucceeded, Outcome: "done", FinishedAt: graphTestTime(), Transition: &end, InstanceStatus: GraphSucceeded}
	if err := db.CompleteGraphNodeAttempt(t.Context(), second.ID, completion); err != nil {
		t.Fatal(err)
	}
	trace, err := db.ListGraphTransitions(t.Context(), i.ID)
	if err != nil || len(trace) != 2 || !reflect.DeepEqual(trace[0], first[0]) || trace[1].ToNode != "done" {
		t.Fatalf("append history=%v %v", trace, err)
	}
	final, err := db.GetGraphInstance(t.Context(), i.ID)
	if err != nil || final.CurrentNode != "done" || final.Status != GraphSucceeded || !final.NextDueAt.IsZero() || final.FinishedAt.IsZero() {
		t.Fatalf("final=%+v %v", final, err)
	}
	if err := db.CompleteGraphNodeAttempt(t.Context(), second.ID, completion); !errors.Is(err, ErrGraphAttemptTerminal) {
		t.Fatalf("duplicate complete=%v", err)
	}
	if err := db.SetGraphInstanceStatus(t.Context(), i.ID, GraphRunning, graphTestTime(), time.Time{}); !errors.Is(err, ErrGraphInstanceTerminal) {
		t.Fatalf("revival=%v", err)
	}
}

func TestGraphFailedCompletionKeepsNode(t *testing.T) {
	db := newTestDB(t)
	i := graphTestInstance(t, db)
	a := graphTestAttempt(t, db, i, 0)
	err := db.CompleteGraphNodeAttempt(t.Context(), a.ID, GraphCompletion{Status: GraphFailed, FinishedAt: graphTestTime(), Outputs: []GraphOutput{{Kind: GraphOutputDiagnostic, Payload: []byte("undeclared outcome")}}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := db.GetGraphInstance(t.Context(), i.ID)
	if err != nil || got.CurrentNode != i.CurrentNode {
		t.Fatalf("node advanced: %+v %v", got, err)
	}
	attempt, err := db.GetGraphNodeAttempt(t.Context(), a.ID)
	if err != nil || attempt.Status != GraphFailed || attempt.Outcome != "" {
		t.Fatalf("attempt=%+v %v", attempt, err)
	}
	trace, err := db.ListGraphTransitions(t.Context(), i.ID)
	if err != nil || len(trace) != 0 {
		t.Fatalf("trace=%v %v", trace, err)
	}
}

func TestGraphDueSelection(t *testing.T) {
	db := newTestDB(t)
	base := graphTestInstance(t, db)
	now := graphTestTime()
	newInstance := func(due time.Time) GraphInstance {
		t.Helper()
		in := base
		in.ID = 0
		in.NextDueAt = due
		got, err := db.CreateGraphInstance(t.Context(), in)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	earlier := newInstance(now.Add(-time.Second))
	equal := newInstance(now)
	newInstance(time.Time{})
	newInstance(now.Add(time.Millisecond))
	for _, status := range []string{GraphSucceeded, GraphFailed, GraphCancelled} {
		terminal := newInstance(now.Add(-time.Hour))
		if err := db.SetGraphInstanceStatus(t.Context(), terminal.ID, status, time.Time{}, now); err != nil {
			t.Fatal(err)
		}
	}
	got, err := db.ListDueGraphInstances(t.Context(), now, 2)
	if err != nil || len(got) != 2 || got[0].ID != earlier.ID || got[1].ID != base.ID {
		t.Fatalf("bounded selection=%v %v", got, err)
	}
	all, err := db.ListDueGraphInstances(t.Context(), now, 20)
	if err != nil || len(all) != 3 || all[2].ID != equal.ID {
		t.Fatalf("selection=%v %v", all, err)
	}
	for _, limit := range []int{0, -1} {
		if _, err := db.ListDueGraphInstances(t.Context(), now, limit); !errors.Is(err, ErrGraphInvariant) {
			t.Fatalf("limit %d: %v", limit, err)
		}
	}
}

func TestGraphAttemptDurability(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	i := graphTestInstance(t, db)
	run := testRun(t, db, i.SessionID, 0)
	a, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: i.ID, Node: "work", RunID: run.ID, StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.FinishGraphNodeAttempt(t.Context(), a.ID, GraphSucceeded, "again", graphTestTime().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	graphTestAttempt(t, db, i, 0)
	before, err := db.ListGraphNodeAttempts(t.Context(), i.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	after, err := db.ListGraphNodeAttempts(t.Context(), i.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("reopen: before=%+v after=%+v err=%v", before, after, err)
	}
}

func TestGraphSchemaRejectsInvalidRows(t *testing.T) {
	db := newTestDB(t)
	i := graphTestInstance(t, db)
	a := graphTestAttempt(t, db, i, 0)
	for _, tc := range []struct {
		name, query string
		args        []any
	}{
		{"unknown instance status", `UPDATE graph_instances SET status='unknown' WHERE id=?`, []any{i.ID}},
		{"terminal without finish", `UPDATE graph_instances SET status='failed' WHERE id=?`, []any{i.ID}},
		{"invalid profile version", `UPDATE graph_definitions SET profile_version=0 WHERE id=?`, []any{i.DefinitionID}},
		{"nonpositive attempt", `UPDATE graph_node_attempts SET attempt=0 WHERE id=?`, []any{a.ID}},
		{"running outcome", `UPDATE graph_node_attempts SET outcome='ok' WHERE id=?`, []any{a.ID}},
		{"unknown attempt status", `UPDATE graph_node_attempts SET status='unknown' WHERE id=?`, []any{a.ID}},
		{"invalid attempt FK", `UPDATE graph_node_attempts SET instance_id=999999 WHERE id=?`, []any{a.ID}},
		{"invalid run FK", `UPDATE graph_node_attempts SET run_id=999999 WHERE id=?`, []any{a.ID}},
		{"invalid definition FK", `UPDATE graph_instances SET definition_id=999999 WHERE id=?`, []any{i.ID}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := db.sql.ExecContext(t.Context(), tc.query, tc.args...); err == nil {
				t.Fatal("invalid row accepted")
			}
		})
	}
	var run sql.NullInt64
	var outcome, finished sql.NullString
	var started string
	if err := db.sql.QueryRowContext(t.Context(), `SELECT run_id,outcome,finished_at,started_at FROM graph_node_attempts WHERE id=?`, a.ID).Scan(&run, &outcome, &finished, &started); err != nil {
		t.Fatal(err)
	}
	if run.Valid || outcome.Valid || finished.Valid || started != FormatTime(graphTestTime()) {
		t.Fatalf("optional NULL/timestamp values: %v %v %v %s", run, outcome, finished, started)
	}
	if _, err := db.RecordGraphOutput(t.Context(), GraphOutput{NodeAttemptID: 999999, Kind: GraphOutputText, CreatedAt: graphTestTime()}); err == nil {
		t.Fatal("output FK accepted")
	}
	if _, err := db.RecordGraphTransition(t.Context(), GraphTransition{InstanceID: 999999, FromNode: "work", ToNode: "done", OnOutcome: "ok", At: graphTestTime()}); err == nil {
		t.Fatal("transition FK accepted")
	}
}

func TestGraphDirectAppendAccessors(t *testing.T) {
	db := newTestDB(t)
	i := graphTestInstance(t, db)
	a := graphTestAttempt(t, db, i, 0)
	in := GraphTransition{InstanceID: i.ID, FromNode: "work", ToNode: "work", OnOutcome: "again", At: graphTestTime()}
	first, err := db.RecordGraphTransition(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	in.At = in.At.Add(time.Second)
	second, err := db.RecordGraphTransition(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	trace, err := db.ListGraphTransitions(t.Context(), i.ID)
	if err != nil || !reflect.DeepEqual(trace, []GraphTransition{first, second}) {
		t.Fatalf("trace=%+v err=%v", trace, err)
	}
	for _, kind := range []string{GraphOutputText, GraphOutputJSON, GraphOutputDiagnostic} {
		if _, err := db.RecordGraphOutput(t.Context(), GraphOutput{NodeAttemptID: a.ID, Kind: kind, CreatedAt: graphTestTime()}); err != nil {
			t.Fatal(err)
		}
	}
	outputs, err := db.ListGraphOutputs(t.Context(), a.ID)
	if err != nil || len(outputs) != 3 {
		t.Fatalf("outputs=%v err=%v", outputs, err)
	}
	for j, o := range outputs {
		if o.NodeAttemptID != a.ID || o.OwnerCommentID != 0 || (j > 0 && outputs[j-1].ID >= o.ID) {
			t.Fatalf("unordered/corrupt output=%+v", o)
		}
	}
	if _, err := db.RecordGraphOutput(t.Context(), GraphOutput{NodeAttemptID: a.ID, Kind: "executable", CreatedAt: graphTestTime()}); !errors.Is(err, ErrGraphInvariant) {
		t.Fatalf("unknown output kind=%v", err)
	}
}
