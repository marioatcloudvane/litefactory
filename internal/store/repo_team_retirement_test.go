package store

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func oldTeamDatabase(t *testing.T) *DB {
	t.Helper()
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "old.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	applyMigrationsThrough(t, db, migrationVersionBefore(t, "remove_repository_team"))
	return db
}

func TestRepoTeamRetirementPreservesRepositoryAndGraphHistory(t *testing.T) {
	db := oldTeamDatabase(t)
	instance := graphTestInstance(t, db)
	attempt := graphTestAttempt(t, db, instance, 3)
	transition, err := db.RecordGraphTransition(t.Context(), GraphTransition{InstanceID: instance.ID, FromNode: "work", ToNode: "done", OnOutcome: "success", At: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetSession(t.Context(), instance.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := db.GetRepo(t.Context(), session.RepoID)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.TouchRepoFetch(t.Context(), repo.ID, graphTestTime()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(t.Context(), `UPDATE repos SET aiteam_team='legacy', aiteam_verified_at=?, external_team='external' WHERE id=?`, FormatTime(graphTestTime()), repo.ID); err != nil {
		t.Fatal(err)
	}
	before, err := db.GetRepo(t.Context(), repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	indexes := indexNames(t, db, "repos")
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, col := range tableColumns(t, db, "repos") {
		if col.name == "aiteam_team" || col.name == "aiteam_verified_at" || col.name == "external_team" {
			t.Errorf("retired column remains: %s", col.name)
		}
	}
	after, err := db.GetRepo(t.Context(), repo.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("repository changed: before=%+v after=%+v err=%v", before, after, err)
	}
	if got := indexNames(t, db, "repos"); !reflect.DeepEqual(indexes, got) {
		t.Fatalf("indexes changed: %v -> %v", indexes, got)
	}
	gotSession, err := db.GetSession(t.Context(), session.ID)
	if err != nil || !reflect.DeepEqual(session, gotSession) {
		t.Fatalf("session changed: %+v err=%v", gotSession, err)
	}
	transitions, err := db.ListGraphTransitions(t.Context(), instance.ID)
	if err != nil || len(transitions) != 1 || !reflect.DeepEqual(transitions[0], transition) {
		t.Fatalf("transition lost: %+v err=%v", transitions, err)
	}
	var count int
	if err := db.sql.QueryRowContext(t.Context(), `SELECT count(*) FROM graph_node_attempts WHERE id=? AND instance_id=?`, attempt.ID, instance.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("attempt lost: count=%d err=%v", count, err)
	}
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("second migration: %v", err)
	}
}

func TestRepoTeamRetirementRollsBackOnDependentView(t *testing.T) {
	db := oldTeamDatabase(t)
	repo := testRepo(t, db, "acme", "rollback")
	if _, err := db.sql.ExecContext(t.Context(), `CREATE VIEW old_team_selection AS SELECT external_team FROM repos`); err != nil {
		t.Fatal(err)
	}
	columns := tableColumns(t, db, "repos")
	err := db.Migrate(t.Context())
	if err == nil || !strings.Contains(err.Error(), "store: migration 0098_") {
		t.Fatalf("migration error=%v", err)
	}
	if got := tableColumns(t, db, "repos"); !reflect.DeepEqual(columns, got) {
		t.Fatalf("partly applied schema: before=%v after=%v", columns, got)
	}
	var applied int
	if err := db.sql.QueryRowContext(t.Context(), `SELECT count(*) FROM schema_migrations WHERE version=98`).Scan(&applied); err != nil || applied != 0 {
		t.Fatalf("failed migration recorded: count=%d err=%v", applied, err)
	}
	if _, err := db.GetRepo(t.Context(), repo.ID); err != nil {
		t.Fatalf("repository lost: %v", err)
	}
}
