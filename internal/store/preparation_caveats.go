package store

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
)

// RecordPreparationCaveats retains bounded, factory-authored conditions before
// execution so failed and replayed results disclose the same preparation facts.
func (db *DB) RecordPreparationCaveats(ctx context.Context, runID int64, caveats []string) (Run, error) {
	if len(caveats) > 16 {
		return Run{}, fmt.Errorf("too many preparation caveats")
	}
	unique := make([]string, 0, len(caveats))
	for _, c := range caveats {
		if c == "" || len(c) > 1024 {
			return Run{}, fmt.Errorf("invalid preparation caveat")
		}
		if !slices.Contains(unique, c) {
			unique = append(unique, c)
		}
	}
	data, err := json.Marshal(unique)
	if err != nil {
		return Run{}, err
	}
	run, err := scanRun(db.sql.QueryRowContext(ctx, `UPDATE runs SET preparation_caveats = ? WHERE id = ? RETURNING `+runColumns, string(data), runID))
	if err != nil {
		return Run{}, fmt.Errorf("retain preparation caveats: %w", err)
	}
	return run, nil
}
