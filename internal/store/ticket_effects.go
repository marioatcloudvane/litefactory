package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/ticket"
)

// TicketEffect qualifies the destination and optional actor of an existing
// ledger key without changing that ledger's hashes, bytes or retry policy.
type TicketEffect struct {
	Key      string
	Locator  ticket.Locator
	RemoteID string
	Actor    ticket.Identity
}

// Ref refuses unresolved legacy locators before they can select a mutation.
func (e TicketEffect) Ref() (ticket.Ref, error) {
	ref := ticket.Ref{Locator: e.Locator, Identity: ticket.Identity{Provider: e.Locator.Provider, Origin: e.Locator.Origin, RemoteID: e.RemoteID}}
	if err := ref.Validate(); err != nil {
		return ticket.Ref{}, fmt.Errorf("store: unresolved effect destination: %w", err)
	}
	return ref, nil
}

const ticketEffectColumns = `effect_key,provider,api_origin,container,locator_key,remote_id,actor_provider,actor_origin,actor_id`

func scanTicketEffect(row scanner) (TicketEffect, error) {
	var e TicketEffect
	err := row.Scan(&e.Key, &e.Locator.Provider, &e.Locator.Origin, &e.Locator.Container, &e.Locator.Key, &e.RemoteID, &e.Actor.Provider, &e.Actor.Origin, &e.Actor.RemoteID)
	if errors.Is(err, sql.ErrNoRows) {
		return e, ErrNotFound
	}
	if err != nil {
		return e, err
	}
	if err = e.Locator.Validate(); err != nil {
		return e, err
	}
	if e.RemoteID != "" {
		if _, err = e.Ref(); err != nil {
			return e, err
		}
	}
	if e.Actor != (ticket.Identity{}) {
		if err = e.Actor.Validate(); err != nil {
			return e, err
		}
	}
	return e, nil
}

// GetTicketEffect observes metadata without changing the owning ledger.
func (db *DB) GetTicketEffect(ctx context.Context, key string) (TicketEffect, error) {
	e, err := scanTicketEffect(db.sql.QueryRowContext(ctx, `SELECT `+ticketEffectColumns+` FROM ticket_effect_identities WHERE effect_key=?`, key))
	if err != nil {
		return e, fmt.Errorf("store: read ticket effect identity: %w", err)
	}
	return e, nil
}

// BindTicketEffect freezes trusted origin and locator before a remote read.
// A supplied actor must already be qualified by the same trusted API instance.
func (db *DB) BindTicketEffect(ctx context.Context, key string, locator ticket.Locator, actor ticket.Identity) (TicketEffect, error) {
	if strings.TrimSpace(key) == "" {
		return TicketEffect{}, ErrTicketIdentityConflict
	}
	if err := locator.Validate(); err != nil {
		return TicketEffect{}, err
	}
	if actor != (ticket.Identity{}) {
		if err := actor.Validate(); err != nil {
			return TicketEffect{}, err
		}
		if actor.Provider != locator.Provider || actor.Origin != locator.Origin {
			return TicketEffect{}, ErrTicketIdentityConflict
		}
	}
	proposed := TicketEffect{Key: key, Locator: locator, Actor: actor}
	var bound TicketEffect
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		existing, err := scanTicketEffect(tx.QueryRowContext(ctx, `SELECT `+ticketEffectColumns+` FROM ticket_effect_identities WHERE effect_key=?`, key))
		if err == nil {
			if existing.Locator != locator || existing.Actor != actor {
				return ErrTicketIdentityConflict
			}
			bound = existing
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO ticket_effect_identities(effect_key,provider,api_origin,container,locator_key,actor_provider,actor_origin,actor_id) VALUES(?,?,?,?,?,?,?,?)`, key, locator.Provider, locator.Origin, locator.Container, locator.Key, actor.Provider, actor.Origin, actor.RemoteID)
		bound = proposed
		return err
	})
	if err != nil {
		return TicketEffect{}, fmt.Errorf("store: bind ticket effect identity: %w", err)
	}
	return bound, nil
}

// ResolveTicketEffect records the immutable identity returned by a verified
// read. Contradictory identity retains the original metadata and pending intent.
func (db *DB) ResolveTicketEffect(ctx context.Context, key string, ref ticket.Ref) (TicketEffect, error) {
	if err := ref.Validate(); err != nil {
		return TicketEffect{}, err
	}
	var bound TicketEffect
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		bound, err = scanTicketEffect(tx.QueryRowContext(ctx, `SELECT `+ticketEffectColumns+` FROM ticket_effect_identities WHERE effect_key=?`, key))
		if err != nil {
			return err
		}
		if bound.Locator != ref.Locator || (bound.RemoteID != "" && bound.RemoteID != ref.Identity.RemoteID) {
			return ErrTicketIdentityConflict
		}
		_, err = tx.ExecContext(ctx, `UPDATE ticket_effect_identities SET remote_id=? WHERE effect_key=?`, ref.Identity.RemoteID, key)
		bound.RemoteID = ref.Identity.RemoteID
		return err
	})
	if err != nil {
		return TicketEffect{}, fmt.Errorf("store: resolve ticket effect identity: %w", err)
	}
	return bound, nil
}

// ListTicketEffectsForAddress reads every retained effect for an address across
// origins. Reconciliation must reject conflicting qualified evidence rather
// than selecting current configuration. Owning ledger bytes remain untouched.
func (db *DB) ListTicketEffectsForAddress(ctx context.Context, container, key string) ([]TicketEffect, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+ticketEffectColumns+` FROM ticket_effect_identities WHERE container=? AND locator_key=? ORDER BY effect_key`, container, key)
	if err != nil {
		return nil, fmt.Errorf("store: list ticket effect identity evidence: %w", err)
	}
	defer rows.Close()
	var out []TicketEffect
	for rows.Next() {
		e, err := scanTicketEffect(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
