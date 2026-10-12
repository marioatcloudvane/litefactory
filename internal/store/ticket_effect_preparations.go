package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
)

// TicketEffectPreparation supplements an existing owner ledger with immutable
// authored bytes and provider correlation. It grants no dispatch or retry right.
// ObservedContent is saved only after the caller verifies contract equivalence.
type TicketEffectPreparation struct {
	Key, CreationUUID, Representation, AuthoredContent, ObservedContent string
	Observed                                                            bool
}

const ticketPreparationColumns = `effect_key,creation_uuid,representation,authored_content,observed_content,observed`

var creationUUIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func newCreationUUID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	bytes[6] = bytes[6]&0x0f | 0x40
	bytes[8] = bytes[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16]), nil
}

func scanTicketPreparation(row scanner) (TicketEffectPreparation, error) {
	var p TicketEffectPreparation
	err := row.Scan(&p.Key, &p.CreationUUID, &p.Representation, &p.AuthoredContent, &p.ObservedContent, &p.Observed)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

// PrepareTicketEffect freezes provider representation and original authored
// bytes before dispatch. Creation operations allocate their UUID exactly once,
// inside the same transaction that retains it. Repeated preparation returns it.
func (db *DB) PrepareTicketEffect(ctx context.Context, key, representation, authored string, creation bool) (TicketEffectPreparation, error) {
	if key == "" || representation == "" || authored == "" {
		return TicketEffectPreparation{}, errors.New("store: incomplete ticket effect preparation")
	}
	var out TicketEffectPreparation
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		old, err := scanTicketPreparation(tx.QueryRowContext(ctx, `SELECT `+ticketPreparationColumns+` FROM ticket_effect_preparations WHERE effect_key=?`, key))
		if err == nil {
			if old.Representation != representation || old.AuthoredContent != authored || (old.CreationUUID != "") != creation {
				return ErrTicketIdentityConflict
			}
			out = old
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		out = TicketEffectPreparation{Key: key, Representation: representation, AuthoredContent: authored}
		if creation {
			out.CreationUUID, err = newCreationUUID()
			if err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO ticket_effect_preparations(effect_key,creation_uuid,representation,authored_content) VALUES(?,?,?,?)`, key, out.CreationUUID, representation, authored)
		return err
	})
	return out, err
}

// GetTicketEffectPreparation recovers original prepared content and UUID.
func (db *DB) GetTicketEffectPreparation(ctx context.Context, key string) (TicketEffectPreparation, error) {
	return scanTicketPreparation(db.sql.QueryRowContext(ctx, `SELECT `+ticketPreparationColumns+` FROM ticket_effect_preparations WHERE effect_key=?`, key))
}

// ConfirmTicketEffectRepresentation retains exact provider-returned bytes after
// caller verification. Conflicting readbacks cannot replace a verified base.
func (db *DB) ConfirmTicketEffectRepresentation(ctx context.Context, key, observed string) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		p, err := scanTicketPreparation(tx.QueryRowContext(ctx, `SELECT `+ticketPreparationColumns+` FROM ticket_effect_preparations WHERE effect_key=?`, key))
		if err != nil {
			return err
		}
		if p.Observed && p.ObservedContent != observed {
			return ErrTicketIdentityConflict
		}
		_, err = tx.ExecContext(ctx, `UPDATE ticket_effect_preparations SET observed_content=?,observed=1 WHERE effect_key=?`, observed, key)
		return err
	})
}
