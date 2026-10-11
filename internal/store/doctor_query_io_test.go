package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func queryFixture(t *testing.T) *DB {
	t.Helper()
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "doctor-query.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := db.sql.ExecContext(t.Context(), `CREATE TABLE source(id INTEGER, value TEXT); INSERT INTO source VALUES(1,'one'),(2,'two'),(3,'three')`); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestDoctorQuerySelectAndWriteBarriers(t *testing.T) {
	db := queryFixture(t)
	for _, q := range []string{`SELECT id,value FROM source ORDER BY id`, `/* header */ SELECT ';--/*' AS quoted; -- final`, `SELECT "value" FROM source; /* final */`} {
		got, err := db.QueryDoctor(t.Context(), q)
		if err != nil || len(got.Columns) == 0 || len(got.Rows) == 0 || got.Truncated {
			t.Fatalf("%s: %+v %v", q, got, err)
		}
	}
	for _, q := range []string{`INSERT INTO source VALUES(4,'changed')`, `UPDATE source SET value='changed'`, `DELETE FROM source`, `CREATE TABLE changed(x)`, `DROP TABLE source`, `PRAGMA user_version=10`, `PRAGMA query_only=OFF`, `ATTACH DATABASE ':memory:' AS other`, `VACUUM`, `SELECT 1; SELECT 2`, `SELECT 1; DELETE FROM source`, `SELECT 1;;`, `SELECT 'unterminated`, `SELECT 1 /* unterminated`, `-- only comment`, `WITH x AS (SELECT 1) SELECT * FROM x`} {
		if _, err := db.QueryDoctor(t.Context(), q); err == nil {
			t.Fatalf("accepted forbidden query %q", q)
		}
	}
	var count int
	if err := db.sql.QueryRowContext(t.Context(), `SELECT count(*) FROM source`).Scan(&count); err != nil || count != 3 {
		t.Fatalf("database changed: count=%d err=%v", count, err)
	}
}

func TestDoctorQueryLimitsTimeoutAndPlainErrors(t *testing.T) {
	db := queryFixture(t)
	row, err := queryDoctor(t.Context(), db.path, `SELECT * FROM source`, time.Second, 2, 4096)
	if err != nil || len(row.Rows) != 2 || !row.Truncated {
		t.Fatalf("row bound: %+v %v", row, err)
	}
	exact, err := queryDoctor(t.Context(), db.path, `SELECT * FROM source`, time.Second, 3, 4096)
	if err != nil || exact.Truncated {
		t.Fatalf("exact row bound: %+v %v", exact, err)
	}
	bytes, err := queryDoctor(t.Context(), db.path, `SELECT hex(zeroblob(1000))`, time.Second, 200, 256)
	if err != nil || !bytes.Truncated || len(bytes.Rows) != 0 {
		t.Fatalf("byte bound: %+v %v", bytes, err)
	}
	slow, err := queryDoctor(t.Context(), db.path, `SELECT sum(x) FROM (WITH RECURSIVE seq(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM seq WHERE x<300000) SELECT x FROM seq)`, DoctorQueryTimeout, 200, 4096)
	if err != nil || slow.Truncated || len(slow.Rows) != 1 || slow.Rows[0][0] != int64(45000150000) {
		t.Fatalf("slow finite query under generous deadline: %+v %v", slow, err)
	}
	_, err = queryDoctor(t.Context(), db.path, `SELECT sum(x) FROM (WITH RECURSIVE seq(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM seq WHERE x<100000000) SELECT x FROM seq)`, 20*time.Millisecond, 200, 4096)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("query did not time out: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := db.QueryDoctor(ctx, `SELECT * FROM source`); err == nil {
		t.Fatal("cancelled query succeeded")
	}
	if _, err := db.QueryDoctor(t.Context(), `SELECT secret_error_canary FROM missing_table`); err == nil || strings.Contains(err.Error(), "secret_error_canary") || strings.Contains(err.Error(), db.path) {
		t.Fatalf("bad query leaked details: %v", err)
	}
}

func TestDoctorQueryBusyAndIndependentReadOnlyConnection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "busy.db")
	// DELETE journaling makes an exclusive writer block even a read-only reader.
	writer, err := sql.Open(driverName, "file:"+path+"?_pragma=journal_mode(DELETE)")
	if err != nil {
		t.Fatal(err)
	}
	writer.SetMaxOpenConns(1)
	defer func() {
		if err := writer.Close(); err != nil {
			t.Error(err)
		}
	}()
	if _, err = writer.ExecContext(t.Context(), `CREATE TABLE source(x); INSERT INTO source VALUES(1)`); err != nil {
		t.Fatal(err)
	}
	conn, err := writer.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			t.Error(err)
		}
	}()
	if _, err = conn.ExecContext(t.Context(), `BEGIN EXCLUSIVE`); err != nil {
		t.Fatal(err)
	}
	_, err = queryDoctor(t.Context(), path, `SELECT * FROM source`, time.Second, 200, 4096)
	if err == nil || !strings.Contains(err.Error(), "busy") {
		t.Fatalf("busy query: %v", err)
	}
	if _, err = conn.ExecContext(t.Context(), `ROLLBACK`); err != nil {
		t.Fatal(err)
	}
	// Check both independent SQLite barriers, even without the lexical validator.
	reader, err := sql.Open(driverName, "file:"+path+"?mode=ro&_pragma=query_only(ON)")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := reader.Close(); err != nil {
			t.Error(err)
		}
	}()
	if _, err = reader.ExecContext(t.Context(), `DELETE FROM source`); err == nil {
		t.Fatal("read-only query-only reader wrote")
	}
	if _, err = reader.ExecContext(t.Context(), `PRAGMA query_only=OFF; DELETE FROM source`); err == nil {
		t.Fatal("read-only mode was bypassed")
	}
}
