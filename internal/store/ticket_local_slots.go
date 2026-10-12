package store

import (
	"context"
	"database/sql"
	"errors"
	"strconv"

	"github.com/cloudvane/litefactory/internal/ticket"
)

func validSlotKind(kind string) bool { return kind == "issue" || kind == "comment" || kind == "actor" }

func localTicketSlot(ctx context.Context, tx *sql.Tx, kind string, identity ticket.Identity) (int64, error) {
	if err := identity.Validate(); err != nil {
		return 0, err
	}
	if !validSlotKind(kind) {
		return 0, errors.New("store: invalid local ticket slot kind")
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO ticket_local_slots(kind,provider,api_origin,remote_id) VALUES(?,?,?,?) ON CONFLICT(kind,provider,api_origin,remote_id) DO NOTHING`, kind, identity.Provider, identity.Origin, identity.RemoteID)
	if err != nil {
		return 0, err
	}
	var slot int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM ticket_local_slots WHERE kind=? AND provider=? AND api_origin=? AND remote_id=?`, kind, identity.Provider, identity.Origin, identity.RemoteID).Scan(&slot)
	return slot, err
}

// LocalTicketSlot assigns a durable compatibility key to a qualified object.
// It is a local database key, never a remote ID or authority evidence. Provider
// transports must resolve the original qualified identity before remote I/O.
func (db *DB) LocalTicketSlot(ctx context.Context, kind string, identity ticket.Identity) (int64, error) {
	var slot int64
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		slot, err = localTicketSlot(ctx, tx, kind, identity)
		return err
	})
	return slot, err
}

// TicketIdentityForSlot resolves a local key without consulting configuration.
func (db *DB) TicketIdentityForSlot(ctx context.Context, kind string, slot int64) (ticket.Identity, error) {
	if !validSlotKind(kind) || slot <= 0 {
		return ticket.Identity{}, errors.New("store: invalid local ticket slot")
	}
	var identity ticket.Identity
	err := db.sql.QueryRowContext(ctx, `SELECT provider,api_origin,remote_id FROM ticket_local_slots WHERE kind=? AND id=?`, kind, slot).Scan(&identity.Provider, &identity.Origin, &identity.RemoteID)
	if errors.Is(err, sql.ErrNoRows) {
		return identity, ErrNotFound
	}
	return identity, err
}

func sessionIssueNumber(ctx context.Context, tx *sql.Tx, ref ticket.Ref) (int, error) {
	if ref.Identity.Provider == ticket.GitHub {
		number, err := strconv.Atoi(ref.Locator.Key)
		if err != nil || number <= 0 {
			return 0, errors.New("store: invalid GitHub ticket number")
		}
		return number, nil
	}
	slot, err := localTicketSlot(ctx, tx, "issue", ref.Identity)
	return int(slot), err
}

// PinExecutionCodePublisher separates retained GitHub code publication identity
// from the ticket publisher. Once recorded it cannot follow a configuration edit.
func (db *DB) PinExecutionCodePublisher(ctx context.Context, sessionID, actorID int64, login string) error {
	if actorID <= 0 || login == "" {
		return errors.New("store: verified code publisher required")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		s, err := scanSnapshot(tx.QueryRowContext(ctx, `SELECT `+snapshotColumns+` FROM session_execution_snapshots WHERE session_id=?`, sessionID))
		if err != nil {
			return err
		}
		if s.CodeActorID != 0 {
			if s.CodeActorID != actorID || s.CodeActorLogin != login {
				return ErrBindingConflict
			}
			return nil
		}
		_, err = tx.ExecContext(ctx, `UPDATE session_execution_snapshots SET code_actor_id=?,code_actor_login=? WHERE session_id=? AND code_actor_id=0`, actorID, login, sessionID)
		return err
	})
}
