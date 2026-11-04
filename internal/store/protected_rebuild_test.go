package store

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// rebuildFixtureDB returns an unmigrated database holding a small sessions
// parent with an index, a trigger and a cascading child, the shape the
// protected rebuild has to keep.
func rebuildFixtureDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "rebuild.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.ensureMigrationTable(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(t.Context(), `
CREATE TABLE sessions (id INTEGER PRIMARY KEY, name TEXT NOT NULL);
CREATE INDEX sessions_name ON sessions(name);
CREATE TABLE session_audit (name TEXT NOT NULL);
CREATE TRIGGER sessions_audit AFTER INSERT ON sessions BEGIN INSERT INTO session_audit(name) VALUES(NEW.name); END;
CREATE TABLE session_notes (id INTEGER PRIMARY KEY, session_id INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE, body TEXT NOT NULL);
INSERT INTO sessions(id,name) VALUES(1,'one'),(2,'two'),(3,'three');
INSERT INTO session_notes(id,session_id,body) VALUES(1,1,'kept'),(2,2,'kept');
DELETE FROM session_audit;`); err != nil {
		t.Fatal(err)
	}
	return db
}

func rebuildFixtureState(t *testing.T, db *DB) map[string][]string {
	t.Helper()
	state := map[string][]string{}
	for _, table := range []string{"sessions", "session_notes", "schema_migrations"} {
		_, rows := advisoryMeterTableRows(t, db, table, nil)
		state[table] = rows
	}
	rows, err := db.sql.QueryContext(t.Context(), `SELECT type||':'||name||':'||COALESCE(sql,'') FROM sqlite_master ORDER BY type,name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var entry string
		if err := rows.Scan(&entry); err != nil {
			t.Fatal(err)
		}
		state["schema"] = append(state["schema"], entry)
	}
	return state
}

func assertRebuildPragmasRestored(t *testing.T, db *DB) {
	t.Helper()
	var foreignKeys, legacy int
	if err := db.sql.QueryRowContext(t.Context(), `PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil || foreignKeys != 1 {
		t.Fatalf("foreign_keys not restored: %d %v", foreignKeys, err)
	}
	if err := db.sql.QueryRowContext(t.Context(), `PRAGMA legacy_alter_table`).Scan(&legacy); err != nil || legacy != 0 {
		t.Fatalf("legacy_alter_table not restored: %d %v", legacy, err)
	}
}

const rebuildFixtureCopy = `
CREATE TABLE sessions_rebuild (id INTEGER PRIMARY KEY, name TEXT NOT NULL, extra TEXT NOT NULL DEFAULT '');
INSERT INTO sessions_rebuild(id,name) SELECT id,name FROM sessions;
DROP TABLE sessions;
ALTER TABLE sessions_rebuild RENAME TO sessions;`

func TestProtectedRebuildKeepsDependentsIndexesAndTriggers(t *testing.T) {
	for _, marker := range []string{"-- litefactory:rebuild-tables sessions\n", "-- litefactory:rebuild-sessions\n"} {
		db := rebuildFixtureDB(t)
		if err := db.applyMigration(t.Context(), migration{version: 1, name: "rebuild", sql: marker + rebuildFixtureCopy}); err != nil {
			t.Fatalf("%q: %v", marker, err)
		}
		assertRebuildPragmasRestored(t, db)
		assertNoForeignKeyViolations(t, db)
		_, notes := advisoryMeterTableRows(t, db, "session_notes", nil)
		if len(notes) != 2 {
			t.Fatalf("%q: cascading children lost: %v", marker, notes)
		}
		var index, trigger int
		if err := db.sql.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM sqlite_master WHERE name='sessions_name'),(SELECT count(*) FROM sqlite_master WHERE name='sessions_audit')`).Scan(&index, &trigger); err != nil || index != 1 || trigger != 1 {
			t.Fatalf("%q: index or trigger not restored: %d %d %v", marker, index, trigger, err)
		}
		var extra string
		if err := db.sql.QueryRowContext(t.Context(), `SELECT extra FROM sessions WHERE id=3`).Scan(&extra); err != nil {
			t.Fatalf("%q: rebuilt shape absent: %v", marker, err)
		}
		var recorded int
		if err := db.sql.QueryRowContext(t.Context(), `SELECT count(*) FROM schema_migrations WHERE version=1`).Scan(&recorded); err != nil || recorded != 1 {
			t.Fatalf("%q: migration not recorded: %d %v", marker, recorded, err)
		}
	}
}

func TestProtectedRebuildRefusesAndRollsBack(t *testing.T) {
	cases := map[string]struct{ body, refusal string }{
		// Renumbering a parent keeps every count but orphans its child rows.
		"broken dependent": {refusal: "violates retained foreign keys", body: `
CREATE TABLE sessions_rebuild (id INTEGER PRIMARY KEY, name TEXT NOT NULL);
INSERT INTO sessions_rebuild(id,name) SELECT id+100,name FROM sessions;
DROP TABLE sessions;
ALTER TABLE sessions_rebuild RENAME TO sessions;`},
		// Dropping a parent row nothing references keeps foreign keys valid.
		"changed parent count": {refusal: "row count of sessions", body: `
CREATE TABLE sessions_rebuild (id INTEGER PRIMARY KEY, name TEXT NOT NULL);
INSERT INTO sessions_rebuild(id,name) SELECT id,name FROM sessions WHERE id<>3;
DROP TABLE sessions;
ALTER TABLE sessions_rebuild RENAME TO sessions;`},
		"changed dependent count": {refusal: "row count of session_notes", body: rebuildFixtureCopy + `
DELETE FROM session_notes WHERE id=2;`},
		"failing statement": {refusal: "no such table", body: rebuildFixtureCopy + `
INSERT INTO missing_table VALUES(1);`},
	}
	for name, c := range cases {
		body := c.body
		db := rebuildFixtureDB(t)
		before := rebuildFixtureState(t, db)
		err := db.applyMigration(t.Context(), migration{version: 1, name: "rebuild", sql: "-- litefactory:rebuild-tables sessions\n" + body})
		if err == nil || !strings.Contains(err.Error(), c.refusal) {
			t.Fatalf("%s: rebuild not refused for %q: %v", name, c.refusal, err)
		}
		if after := rebuildFixtureState(t, db); !reflect.DeepEqual(after, before) {
			t.Fatalf("%s: refused rebuild changed history:\nbefore %v\nafter  %v", name, before, after)
		}
		assertRebuildPragmasRestored(t, db)
		assertNoForeignKeyViolations(t, db)
	}
}

func TestProtectedRebuildRefusesUnlistedTableBeforeRunning(t *testing.T) {
	for _, marker := range []string{"-- litefactory:rebuild-tables sessions widgets\n", "-- litefactory:rebuild-tables session_notes\n", "-- litefactory:rebuild-tables \n"} {
		db := rebuildFixtureDB(t)
		before := rebuildFixtureState(t, db)
		err := db.applyMigration(t.Context(), migration{version: 1, name: "rebuild", sql: marker + `CREATE TABLE ran (id INTEGER);` + rebuildFixtureCopy})
		if err == nil {
			t.Fatalf("%q: unlisted rebuild ran", marker)
		}
		if marker != "-- litefactory:rebuild-tables \n" && !errors.Is(err, ErrRebuildTableRefused) {
			t.Fatalf("%q: wrong refusal: %v", marker, err)
		}
		if after := rebuildFixtureState(t, db); !reflect.DeepEqual(after, before) {
			t.Fatalf("%q: refused rebuild changed the database", marker)
		}
		assertRebuildPragmasRestored(t, db)
	}
}
