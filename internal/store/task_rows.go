package store

import (
	"context"
	"fmt"
)

// taskRowQueries are the rows of one task the Doctor reads: the task, its
// latest runs and its workflow graphs. Each is bounded; none reads credentials.
var taskRowQueries = []struct{ name, query string }{
	{"session", `SELECT * FROM sessions WHERE id=?1`},
	{"runs", `SELECT * FROM runs WHERE session_id=?1 ORDER BY id DESC LIMIT 20`},
	{"graph_instances", `SELECT i.*, d.mode, d.team FROM graph_instances i JOIN graph_definitions d ON d.id=i.definition_id WHERE i.session_id=?1 ORDER BY i.id DESC LIMIT 20`},
	{"graph_node_attempts", `SELECT a.* FROM graph_node_attempts a JOIN graph_instances i ON i.id=a.instance_id WHERE i.session_id=?1 ORDER BY a.id DESC LIMIT 60`},
	{"graph_dispatches", `SELECT * FROM graph_dispatches WHERE session_id=?1 ORDER BY id DESC LIMIT 40`},
	{"graph_owner_waits", `SELECT w.* FROM graph_owner_waits w JOIN graph_instances i ON i.id=w.instance_id WHERE i.session_id=?1 ORDER BY w.rowid DESC LIMIT 20`},
}

// TaskRows exports the task's recorded rows, newest first, as plain values
// keyed by column name. It is the read-only evidence the factory hands the
// Doctor, which cannot read the factory's database itself.
func (db *DB) TaskRows(ctx context.Context, sessionID int64) (map[string][]map[string]any, error) {
	out := make(map[string][]map[string]any, len(taskRowQueries))
	for _, q := range taskRowQueries {
		rows, err := db.sql.QueryContext(ctx, q.query, sessionID)
		if err != nil {
			return nil, fmt.Errorf("store: task rows %s: %w", q.name, err)
		}
		columns, err := rows.Columns()
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		list := []map[string]any{}
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for i := range values {
				pointers[i] = &values[i]
			}
			if err := rows.Scan(pointers...); err != nil {
				_ = rows.Close()
				return nil, err
			}
			row := make(map[string]any, len(columns))
			for i, column := range columns {
				if raw, ok := values[i].([]byte); ok {
					values[i] = string(raw)
				}
				row[column] = values[i]
			}
			list = append(list, row)
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		out[q.name] = list
	}
	return out, nil
}
