package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type ownerActionEffectKey struct{}
type ownerActionEffect struct {
	key   string
	clock func() time.Time
}

// WithOwnerActionEffect carries the claimed execution to domain effect boundaries.
func WithOwnerActionEffect(ctx context.Context, key string, clock func() time.Time) context.Context {
	return context.WithValue(ctx, ownerActionEffectKey{}, ownerActionEffect{key: key, clock: clock})
}

func ownerActionEffectContextTx(ctx context.Context, tx *sql.Tx) error {
	effect, ok := ctx.Value(ownerActionEffectKey{}).(ownerActionEffect)
	if !ok {
		return nil
	}
	if effect.key == "" || effect.clock == nil {
		return errors.New("owner action effect authority unavailable")
	}
	return ownerActionEffectAuthority(ctx, tx, effect.key, effect.clock())
}

// CheckOwnerActionEffect rechecks current durable authority immediately before I/O.
// Existing domain callers without an owner-action context keep their own boundary.
func (db *DB) CheckOwnerActionEffect(ctx context.Context) error {
	if _, ok := ctx.Value(ownerActionEffectKey{}).(ownerActionEffect); !ok {
		return nil
	}
	return db.withTx(ctx, func(tx *sql.Tx) error { return ownerActionEffectContextTx(ctx, tx) })
}

// OwnerActionSessionRevision binds diagnosis review to one retained task session.
func OwnerActionSessionRevision(s Session) string {
	return fmt.Sprintf("session %d; created %s", s.ID, s.CreatedAt.UTC().Format(time.RFC3339Nano))
}
