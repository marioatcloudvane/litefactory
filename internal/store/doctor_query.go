package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"modernc.org/sqlite"
	sqlitelib "modernc.org/sqlite/lib"
)

const (
	// DoctorQueryRows caps one live read without paginating implicitly.
	DoctorQueryRows = 200
	// DoctorQueryBytes caps encoded output and SQLite's individual values.
	DoctorQueryBytes = 65536
	// DoctorQueryTimeout includes connection acquisition and query execution.
	// The doctor runs on the factory host while agents and test suites load
	// it, so the bound must stay above opening a fresh read-only connection there.
	DoctorQueryTimeout = 5 * time.Second
)

// DoctorQueryResult is bounded readback, never a writable database capability.
type DoctorQueryResult struct {
	Columns   []string `json:"columns"`
	Rows      [][]any  `json:"rows"`
	Truncated bool     `json:"truncated"`
}

// QueryDoctor opens a separate read-only, query-only connection per call. It
// never borrows the daemon's writable connection or changes its pragmas.
func (db *DB) QueryDoctor(ctx context.Context, statement string) (DoctorQueryResult, error) {
	return queryDoctor(ctx, db.path, statement, DoctorQueryTimeout, DoctorQueryRows, DoctorQueryBytes)
}

func queryDoctor(ctx context.Context, path, statement string, timeout time.Duration, maxRows, maxBytes int) (out DoctorQueryResult, err error) {
	out.Columns, out.Rows = []string{}, [][]any{}
	if !singleDoctorSelect(statement) {
		return out, errors.New("database query requires exactly one SELECT statement")
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	u := url.URL{Scheme: "file", Path: path}
	pool, err := sql.Open(driverName, u.String()+"?mode=ro&_pragma=query_only(ON)&_pragma=busy_timeout(100)")
	if err != nil {
		return out, errors.New("database reader unavailable")
	}
	pool.SetMaxOpenConns(1)
	defer func() { err = errors.Join(err, pool.Close()) }()
	conn, err := pool.Conn(ctx)
	if err != nil {
		return out, doctorQueryError(ctx)
	}
	defer func() { err = errors.Join(err, conn.Close()) }()
	// Stop huge scalar/row allocations inside SQLite, before Scan can allocate
	// an agent-requested gigabyte blob. The output budget remains stricter.
	if _, err := sqlite.Limit(conn, sqlitelib.SQLITE_LIMIT_LENGTH, maxBytes); err != nil {
		return out, errors.New("database reader limit unavailable")
	}
	rows, err := conn.QueryContext(ctx, statement)
	if err != nil {
		if doctorQueryTooBig(err) {
			out.Truncated = true
			return out, nil
		}
		return out, doctorQueryError(ctx)
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	out.Columns, err = rows.Columns()
	if err != nil {
		return out, doctorQueryError(ctx)
	}
	header, err := json.Marshal(out)
	if err != nil || len(header) > maxBytes {
		return DoctorQueryResult{Columns: []string{}, Rows: [][]any{}, Truncated: true}, nil
	}
	size := len(header)
	for rows.Next() {
		if len(out.Rows) >= maxRows {
			out.Truncated = true
			break
		}
		values := make([]any, len(out.Columns))
		pointers := make([]any, len(values))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return out, doctorQueryError(ctx)
		}
		for i, v := range values {
			if b, ok := v.([]byte); ok {
				values[i] = string(b)
			}
		}
		encoded, err := json.Marshal(values)
		if err != nil {
			return out, errors.New("database result cannot be encoded")
		}
		if len(encoded)+1 > maxBytes-size {
			out.Truncated = true
			break
		}
		size += len(encoded) + 1
		out.Rows = append(out.Rows, values)
	}
	if doctorQueryTooBig(rows.Err()) {
		out.Truncated = true
		return out, nil
	}
	if rows.Err() != nil || ctx.Err() != nil {
		return out, doctorQueryError(ctx)
	}
	return out, nil
}

func doctorQueryTooBig(err error) bool {
	var native *sqlite.Error
	return errors.As(err, &native) && native.Code() == sqlitelib.SQLITE_TOOBIG
}

func doctorQueryError(ctx context.Context) error {
	if ctx.Err() != nil {
		return errors.New("database query timed out or was cancelled")
	}
	return errors.New("database query failed: invalid query or database busy")
}

// The lexical barrier handles SQLite quotes and comments, so a semicolon in a
// string is data, while a second statement (including a second SELECT) fails.
// WITH and PRAGMA are deliberately not accepted; the contract is one SELECT.
func singleDoctorSelect(s string) bool {
	if len(s) == 0 || len(s) > 16384 || strings.ContainsRune(s, 0) {
		return false
	}
	first, ended := false, false
	for i := 0; i < len(s); {
		c := s[i]
		if strings.ContainsRune(" \t\r\n\f", rune(c)) {
			i++
			continue
		}
		if i+1 < len(s) && s[i:i+2] == "--" {
			if n := strings.IndexByte(s[i:], '\n'); n >= 0 {
				i += n + 1
			} else {
				break
			}
			continue
		}
		if i+1 < len(s) && s[i:i+2] == "/*" {
			n := strings.Index(s[i+2:], "*/")
			if n < 0 {
				return false
			}
			i += n + 4
			continue
		}
		if ended {
			return false
		}
		if !first {
			start := i
			for i < len(s) && (s[i] >= 'a' && s[i] <= 'z' || s[i] >= 'A' && s[i] <= 'Z' || s[i] == '_') {
				i++
			}
			if !strings.EqualFold(s[start:i], "SELECT") {
				return false
			}
			first = true
			continue
		}
		if c == ';' {
			ended = true
			i++
			continue
		}
		if c == '\'' || c == '"' || c == '`' || c == '[' {
			quoteEnd := c
			if c == '[' {
				quoteEnd = ']'
			}
			i++
			closed := false
			for i < len(s) {
				if s[i] == quoteEnd {
					i++
					if quoteEnd != ']' && i < len(s) && s[i] == quoteEnd {
						i++
						continue
					}
					closed = true
					break
				}
				i++
			}
			if !closed {
				return false
			}
			continue
		}
		i++
	}
	return first
}
