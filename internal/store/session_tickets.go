package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/ticket"
)

// ErrTicketIdentityConflict means retained evidence disagrees with a proposed
// binding. Callers must keep pending effects and refuse dispatch, never rebind.
var ErrTicketIdentityConflict = errors.New("ticket identity mapping conflicts with retained history")

// SessionTicket holds additive identity metadata; TaskRef remains the frozen
// alias used by historical naming, locks, approvals and publication keys.
type SessionTicket struct {
	SessionID int64
	TaskRef   string
	Locator   ticket.Locator
	RemoteID  string
}

// Ref requires verified immutable identity; an origin-only legacy binding is
// useful for reads but cannot select a mutation destination.
func (s SessionTicket) Ref() (ticket.Ref, error) {
	ref := ticket.Ref{Locator: s.Locator, Identity: ticket.Identity{Provider: s.Locator.Provider, Origin: s.Locator.Origin, RemoteID: s.RemoteID}}
	if err := ref.Validate(); err != nil {
		return ticket.Ref{}, fmt.Errorf("store: unresolved session ticket: %w", err)
	}
	return ref, nil
}

const sessionTicketColumns = `session_id, task_ref, provider, api_origin, container, locator_key, remote_id`

func scanSessionTicket(row scanner) (SessionTicket, error) {
	var s SessionTicket
	err := row.Scan(&s.SessionID, &s.TaskRef, &s.Locator.Provider, &s.Locator.Origin, &s.Locator.Container, &s.Locator.Key, &s.RemoteID)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionTicket{}, ErrNotFound
	}
	if err != nil {
		return SessionTicket{}, err
	}
	if err := s.Locator.Validate(); err != nil {
		return SessionTicket{}, fmt.Errorf("store: invalid retained ticket locator: %w", err)
	}
	if s.RemoteID != "" {
		if _, err := s.Ref(); err != nil {
			return SessionTicket{}, err
		}
	}
	return s, nil
}

// GetSessionTicket reads mapping metadata without resolving or changing history.
func (db *DB) GetSessionTicket(ctx context.Context, sessionID int64) (SessionTicket, error) {
	s, err := scanSessionTicket(db.sql.QueryRowContext(ctx, `SELECT `+sessionTicketColumns+` FROM session_ticket_identities WHERE session_id=?`, sessionID))
	if err != nil {
		return SessionTicket{}, fmt.Errorf("store: read session ticket: %w", err)
	}
	return s, nil
}

// BindLegacySessionOrigin freezes the configured GitHub API origin before a
// legacy locator is read remotely. Origin must come from trusted configuration,
// not a ticket/browser URL. A later configuration change cannot rebind history.
func (db *DB) BindLegacySessionOrigin(ctx context.Context, sessionID int64, origin ticket.Origin) (SessionTicket, error) {
	var binding SessionTicket
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		binding, err = bindLegacySessionOrigin(ctx, tx, sessionID, origin)
		return err
	})
	if err != nil {
		return SessionTicket{}, fmt.Errorf("store: bind legacy session origin: %w", err)
	}
	return binding, nil
}

func bindLegacySessionOrigin(ctx context.Context, tx *sql.Tx, sessionID int64, origin ticket.Origin) (SessionTicket, error) {
	// Canonical origin validation is the same even before a remote ID exists.
	if err := (ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: "validation"}).Validate(); err != nil {
		return SessionTicket{}, err
	}
	session, err := scanSession(tx.QueryRowContext(ctx, `SELECT `+sessionColumns+` FROM sessions WHERE id=?`, sessionID))
	if err != nil {
		return SessionTicket{}, err
	}
	retained, readErr := scanSessionTicket(tx.QueryRowContext(ctx, `SELECT `+sessionTicketColumns+` FROM session_ticket_identities WHERE session_id=?`, sessionID))
	if readErr == nil {
		if retained.TaskRef != session.TaskRef || retained.Locator.Provider != ticket.GitHub || retained.Locator.Origin != origin {
			return SessionTicket{}, ErrTicketIdentityConflict
		}
		return retained, nil
	}
	if !errors.Is(readErr, ErrNotFound) {
		return SessionTicket{}, readErr
	}
	alias, ok := strings.CutPrefix(session.TaskRef, "github:")
	if !ok {
		return SessionTicket{}, ErrTicketIdentityConflict
	}
	container, key, ok := strings.Cut(alias, "#")
	if !ok || key != strconv.Itoa(session.IssueNumber) {
		return SessionTicket{}, ErrTicketIdentityConflict
	}
	proposed := SessionTicket{SessionID: sessionID, TaskRef: session.TaskRef, Locator: ticket.Locator{Provider: ticket.GitHub, Origin: origin, Container: container, Key: key}}
	if err := proposed.Locator.Validate(); err != nil {
		return SessionTicket{}, err
	}
	existing, err := scanSessionTicket(tx.QueryRowContext(ctx, `SELECT `+sessionTicketColumns+` FROM session_ticket_identities WHERE session_id=?`, sessionID))
	if err == nil {
		if existing.TaskRef != proposed.TaskRef || existing.Locator != proposed.Locator {
			return SessionTicket{}, ErrTicketIdentityConflict
		}
		return existing, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return SessionTicket{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO session_ticket_identities(session_id,task_ref,provider,api_origin,container,locator_key) VALUES(?,?,?,?,?,?)`, sessionID, proposed.TaskRef, string(proposed.Locator.Provider), string(origin), container, key)
	if err != nil {
		return SessionTicket{}, fmt.Errorf("%w: %w", ErrTicketIdentityConflict, err)
	}
	return proposed, nil
}

// BindSessionTicket adds the identity returned by a verified remote read. It
// never changes the historical alias, source consumption, prepared bodies or
// delivery keys. A canonical ticket cannot become two independent sessions.
func (db *DB) BindSessionTicket(ctx context.Context, sessionID int64, ref ticket.Ref) (SessionTicket, error) {
	if err := ref.Validate(); err != nil {
		return SessionTicket{}, err
	}
	if ref.Identity.Provider != ticket.GitHub {
		return SessionTicket{}, ErrTicketIdentityConflict
	}
	var binding SessionTicket
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var sessionRepo int64
		if err := tx.QueryRowContext(ctx, `SELECT repo_id FROM sessions WHERE id=?`, sessionID).Scan(&sessionRepo); err != nil {
			return err
		}
		var hasProjectBindings bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name='project_ticket_bindings')`).Scan(&hasProjectBindings); err != nil {
			return err
		}
		if hasProjectBindings {
			owned, err := getBinding(ctx, tx, ref.Identity)
			if err == nil && (owned.RepoID != sessionRepo || owned.Ref != ref) {
				return ErrBindingConflict
			}
			if err != nil && !errors.Is(err, ErrNotFound) {
				return err
			}
		}
		var err error
		binding, err = scanSessionTicket(tx.QueryRowContext(ctx, `SELECT `+sessionTicketColumns+` FROM session_ticket_identities WHERE session_id=?`, sessionID))
		if errors.Is(err, ErrNotFound) {
			binding, err = bindLegacySessionOrigin(ctx, tx, sessionID, ref.Identity.Origin)
		}
		if err != nil {
			return err
		}
		if binding.Locator != ref.Locator || (binding.RemoteID != "" && binding.RemoteID != ref.Identity.RemoteID) {
			return ErrTicketIdentityConflict
		}
		var other int64
		err = tx.QueryRowContext(ctx, `SELECT session_id FROM session_ticket_identities WHERE provider=? AND api_origin=? AND remote_id=?`, string(ref.Identity.Provider), string(ref.Identity.Origin), ref.Identity.RemoteID).Scan(&other)
		if err == nil && other != sessionID {
			return ErrTicketIdentityConflict
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE session_ticket_identities SET remote_id=? WHERE session_id=?`, ref.Identity.RemoteID, sessionID)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrTicketIdentityConflict, err)
		}
		binding.RemoteID = ref.Identity.RemoteID
		return nil
	})
	if err != nil {
		return SessionTicket{}, fmt.Errorf("store: bind session ticket: %w", err)
	}
	return binding, nil
}
