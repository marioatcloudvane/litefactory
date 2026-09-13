package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// RetirementSweep is a bounded pass over the durable owner inventory. Completed
// passes retain their counters until the next due pass replaces them.
type RetirementSweep struct {
	ID        string
	StartedAt time.Time
	NextDue   time.Time
	Cursor    int64
	Boundary  int64
	Complete  bool
	Owners    int
	Deferred  int
	Counts    map[string]int
}

// GetRetirementSweep returns zero state before the first automatic pass.
func (db *DB) GetRetirementSweep(ctx context.Context) (RetirementSweep, error) {
	var s RetirementSweep
	var data string
	err := db.sql.QueryRowContext(ctx, `SELECT state FROM retirement_sweep WHERE singleton=1`).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("read retirement sweep: %w", err)
	}
	if err := json.Unmarshal([]byte(data), &s); err != nil {
		return s, fmt.Errorf("decode retirement sweep: %w", err)
	}
	return s, nil
}

// SaveRetirementSweep checkpoints only local state; no transaction spans I/O.
func (db *DB) SaveRetirementSweep(ctx context.Context, s RetirementSweep) error {
	if s.ID == "" || s.StartedAt.IsZero() || s.Cursor < 0 || s.Boundary < s.Cursor {
		return fmt.Errorf("invalid retirement sweep")
	}
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("encode retirement sweep: %w", err)
	}
	_, err = db.sql.ExecContext(ctx, `INSERT INTO retirement_sweep(singleton,state) VALUES(1,?) ON CONFLICT(singleton) DO UPDATE SET state=excluded.state`, string(data))
	if err != nil {
		return fmt.Errorf("save retirement sweep: %w", err)
	}
	return nil
}
