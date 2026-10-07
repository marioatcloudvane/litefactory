package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// SetSegmentDelivery pins a newly allocated conversation's immutable bundle.
// Repeating the same assignment is harmless; a different assignment is refused.
func (db *DB) SetSegmentDelivery(ctx context.Context, id int64, identity, root string) error {
	if id == 0 || identity == "" || root == "" {
		return fmt.Errorf("store: segment delivery requires id, identity and root")
	}
	result, err := db.sql.ExecContext(ctx, `INSERT INTO segment_deliveries(segment_id, identity, root) VALUES(?, ?, ?) ON CONFLICT(segment_id) DO UPDATE SET identity = excluded.identity, root = excluded.root WHERE segment_deliveries.identity = excluded.identity AND segment_deliveries.root = excluded.root`, id, identity, root)
	if err != nil {
		return fmt.Errorf("store: pin segment delivery: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: pin segment delivery result: %w", err)
	}
	if count != 1 {
		return fmt.Errorf("store: segment delivery is absent or already pinned differently")
	}
	return nil
}

// SegmentDelivery returns an empty pair for a legacy/unassigned conversation.
// Keeping delivery in its own table leaves the original segment schema intact.
func (db *DB) SegmentDelivery(ctx context.Context, id int64) (identity, root string, err error) {
	err = db.sql.QueryRowContext(ctx, `SELECT identity, root FROM segment_deliveries WHERE segment_id = ?`, id).Scan(&identity, &root)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("store: read segment delivery: %w", err)
	}
	return identity, root, nil
}
