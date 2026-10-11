package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/ticket"
)

// ErrSubscriptionConflict rejects stale intent and operation responses.
var ErrSubscriptionConflict = errors.New("ticket subscription revision conflict")

// ErrSubscriptionBusy means a durable operation already owns this revision.
var ErrSubscriptionBusy = errors.New("ticket subscription operation already claimed")

// TicketSubscription retains desired configuration separately from observations.
// Secret fields are references, never resolved signing material.
type TicketSubscription struct {
	ID                                          string
	RepoID, ConnectionVersionID, Revision       int64
	Provider                                    ticket.Provider
	Origin                                      ticket.Origin
	Container, ContainerID, CallbackURL         string
	Events                                      []string
	SecretService, SecretAccount, SecretVersion string
	DesiredEnabled                              bool
	RemoteHookID, ObservedState, Failure        string
	CheckedAt, DeliveredAt                      time.Time
}

func readSubscription(row scanner) (TicketSubscription, error) {
	var raw string
	if err := row.Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return TicketSubscription{}, ErrNotFound
		}
		return TicketSubscription{}, err
	}
	var out TicketSubscription
	err := json.Unmarshal([]byte(raw), &out)
	return out, err
}

// GetTicketSubscription resolves an opaque callback selector locally.
func (db *DB) GetTicketSubscription(ctx context.Context, id string) (TicketSubscription, error) {
	return readSubscription(db.sql.QueryRowContext(ctx, `SELECT document FROM ticket_subscriptions WHERE id=?`, id))
}

// ListTicketSubscriptions includes historical source versions for reconciliation.
func (db *DB) ListTicketSubscriptions(ctx context.Context, repoID int64) ([]TicketSubscription, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT document FROM ticket_subscriptions WHERE (?=0 OR repo_id=?) ORDER BY id`, repoID, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TicketSubscription
	for rows.Next() {
		s, err := readSubscription(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// PutTicketSubscription saves revision-checked intent before remote dispatch.
func (db *DB) PutTicketSubscription(ctx context.Context, s TicketSubscription, expectedRevision int64) (TicketSubscription, error) {
	if s.ID == "" || s.ContainerID == "" || s.CallbackURL == "" || strings.ContainsAny(s.SecretService+s.SecretAccount+s.SecretVersion, "\r\n\x00") {
		return s, errors.New("store: invalid subscription configuration")
	}
	var out TicketSubscription
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		v, err := scanConnection(tx.QueryRowContext(ctx, `SELECT `+connectionColumns+` FROM project_connection_versions WHERE id=?`, s.ConnectionVersionID))
		if err != nil {
			return err
		}
		if v.Role != TicketConnection || v.RepoID != s.RepoID || v.Provider != s.Provider || v.Origin != s.Origin || v.Destination != s.Container || (v.RemoteID != "" && v.RemoteID != s.ContainerID) {
			return errors.New("store: subscription source does not match connection")
		}
		old, err := readSubscription(tx.QueryRowContext(ctx, `SELECT document FROM ticket_subscriptions WHERE id=?`, s.ID))
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		if (err == nil && old.Revision != expectedRevision) || (errors.Is(err, ErrNotFound) && expectedRevision != 0) {
			return ErrSubscriptionConflict
		}
		if err == nil {
			if old.ConnectionVersionID != s.ConnectionVersionID || old.ContainerID != s.ContainerID || old.RepoID != s.RepoID {
				return ErrSubscriptionConflict
			}
			s.RemoteHookID = old.RemoteHookID
			s.CheckedAt = old.CheckedAt
			s.DeliveredAt = old.DeliveredAt
		}
		s.Revision = expectedRevision + 1
		s.ObservedState = "pending"
		s.Failure = ""
		raw, err := json.Marshal(s)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO ticket_subscriptions(id,repo_id,connection_version_id,revision,provider,api_origin,container_id,remote_hook_id,document) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET revision=excluded.revision,document=excluded.document`, s.ID, s.RepoID, s.ConnectionVersionID, s.Revision, s.Provider, s.Origin, s.ContainerID, s.RemoteHookID, string(raw))
		out = s
		return err
	})
	return out, err
}

// SubscriptionOperation is a durable remote operation claim. Its specification
// remains the original revision even if newer local intent replaces it.
type SubscriptionOperation struct {
	SubscriptionID            string
	Revision                  int64
	Kind, State, RemoteHookID string
	Specification             TicketSubscription
}

// ClaimSubscriptionOperation marks dispatch before the caller performs remote I/O.
// Existing claims are never stolen: lost responses require reconciliation.
func (db *DB) ClaimSubscriptionOperation(ctx context.Context, id string, revision int64, kind string) (SubscriptionOperation, error) {
	var out SubscriptionOperation
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		s, err := readSubscription(tx.QueryRowContext(ctx, `SELECT document FROM ticket_subscriptions WHERE id=?`, id))
		if err != nil {
			return err
		}
		if s.Revision != revision {
			return ErrSubscriptionConflict
		}
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM ticket_subscription_operations WHERE subscription_id=? AND revision=?`, id, revision).Scan(&exists); err != nil {
			return err
		}
		if exists != 0 {
			return ErrSubscriptionBusy
		}
		out = SubscriptionOperation{SubscriptionID: id, Revision: revision, Kind: kind, State: "dispatched", RemoteHookID: s.RemoteHookID, Specification: s}
		raw, err := json.Marshal(out)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO ticket_subscription_operations(subscription_id,revision,state,document) VALUES(?,?,?,?)`, id, revision, out.State, string(raw))
		return err
	})
	return out, err
}

// GetSubscriptionOperation returns retained uncertainty without dispatching again.
func (db *DB) GetSubscriptionOperation(ctx context.Context, id string, revision int64) (SubscriptionOperation, error) {
	var raw string
	err := db.sql.QueryRowContext(ctx, `SELECT document FROM ticket_subscription_operations WHERE subscription_id=? AND revision=?`, id, revision).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return SubscriptionOperation{}, ErrNotFound
	}
	if err != nil {
		return SubscriptionOperation{}, err
	}
	var out SubscriptionOperation
	err = json.Unmarshal([]byte(raw), &out)
	return out, err
}

// FinishSubscriptionOperation preserves the operation outcome but applies it to
// observed state only when its intent revision is still current.
func (db *DB) FinishSubscriptionOperation(ctx context.Context, op SubscriptionOperation, state, remoteID, observed, failure string, at time.Time) error {
	if state != "confirmed" && state != "uncertain" && state != "failed" {
		return errors.New("store: invalid subscription operation outcome")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var raw string
		if err := tx.QueryRowContext(ctx, `SELECT document FROM ticket_subscription_operations WHERE subscription_id=? AND revision=?`, op.SubscriptionID, op.Revision).Scan(&raw); err != nil {
			return err
		}
		var saved SubscriptionOperation
		if err := json.Unmarshal([]byte(raw), &saved); err != nil {
			return err
		}
		if saved.State == "confirmed" {
			return ErrSubscriptionConflict
		}
		saved.State = state
		saved.RemoteHookID = remoteID
		data, err := json.Marshal(saved)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE ticket_subscription_operations SET state=?,document=? WHERE subscription_id=? AND revision=?`, state, string(data), op.SubscriptionID, op.Revision); err != nil {
			return err
		}
		s, err := readSubscription(tx.QueryRowContext(ctx, `SELECT document FROM ticket_subscriptions WHERE id=?`, op.SubscriptionID))
		if err != nil {
			return err
		}
		if s.Revision != op.Revision {
			return nil
		}
		s.RemoteHookID = remoteID
		s.ObservedState = observed
		s.Failure = failure
		s.CheckedAt = at
		data, err = json.Marshal(s)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE ticket_subscriptions SET remote_hook_id=?,document=? WHERE id=? AND revision=?`, remoteID, string(data), s.ID, s.Revision)
		if err != nil {
			return fmt.Errorf("store: record subscription outcome: %w", err)
		}
		return nil
	})
}

// RecordSubscriptionDelivery records only a successfully authenticated revision.
func (db *DB) RecordSubscriptionDelivery(ctx context.Context, id string, revision int64, at time.Time) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		s, err := readSubscription(tx.QueryRowContext(ctx, `SELECT document FROM ticket_subscriptions WHERE id=?`, id))
		if err != nil {
			return err
		}
		if s.Revision != revision || !s.DesiredEnabled {
			return ErrSubscriptionConflict
		}
		s.DeliveredAt = at
		raw, err := json.Marshal(s)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE ticket_subscriptions SET document=? WHERE id=? AND revision=?`, string(raw), id, revision)
		return err
	})
}

// ListSubscriptionOperations exposes prior unresolved dispatches so a newer
// revision cannot blindly create another hook after a lost response.
func (db *DB) ListSubscriptionOperations(ctx context.Context, id string) ([]SubscriptionOperation, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT document FROM ticket_subscription_operations WHERE subscription_id=? ORDER BY revision`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SubscriptionOperation
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var op SubscriptionOperation
		if err := json.Unmarshal([]byte(raw), &op); err != nil {
			return nil, err
		}
		out = append(out, op)
	}
	return out, rows.Err()
}

// ClearSubscriptionRemoteHook forgets an owned hook only after the management
// caller has confirmed that exact remote ID no longer exists. Revision checks
// prevent that result from clearing a replacement recorded in the meantime.
func (db *DB) ClearSubscriptionRemoteHook(ctx context.Context, id string, revision int64, expectedRemoteID string) error {
	if expectedRemoteID == "" {
		return ErrSubscriptionConflict
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		s, err := readSubscription(tx.QueryRowContext(ctx, `SELECT document FROM ticket_subscriptions WHERE id=?`, id))
		if err != nil {
			return err
		}
		if s.Revision != revision || s.RemoteHookID != expectedRemoteID {
			return ErrSubscriptionConflict
		}
		s.RemoteHookID = ""
		s.ObservedState = "missing"
		data, err := json.Marshal(s)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE ticket_subscriptions SET remote_hook_id='',document=? WHERE id=? AND revision=?`, string(data), id, revision)
		return err
	})
}

// ImportLegacyTicketSubscription preserves a previously owned remote hook and
// its original keychain account only when the current legacy source is proven.
// Historical owner/name rows never establish ownership on a replacement source.
func (db *DB) ImportLegacyTicketSubscription(ctx context.Context, v ConnectionVersion, containerID string) (TicketSubscription, error) {
	var out TicketSubscription
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		existing, err := readSubscription(tx.QueryRowContext(ctx, `SELECT document FROM ticket_subscriptions WHERE connection_version_id=?`, v.ID))
		if err == nil {
			out = existing
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		trusted, err := currentConnection(ctx, tx, v.RepoID, TicketConnection)
		if err != nil {
			return err
		}
		if trusted != v || v.Provenance != "legacy" || containerID == "" || (v.RemoteID != "" && v.RemoteID != containerID) {
			return ErrSubscriptionConflict
		}
		var count int
		var destination string
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM project_connection_versions WHERE repo_id=? AND role='ticket'`, v.RepoID).Scan(&count); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT owner || '/' || name FROM repos WHERE id=?`, v.RepoID).Scan(&destination); err != nil {
			return err
		}
		if count != 1 || destination != v.Destination {
			return ErrSubscriptionConflict
		}
		var hookID int64
		var callback, checked string
		err = tx.QueryRowContext(ctx, `SELECT hook_id,url,updated_at FROM repo_webhooks WHERE repo_id=?`, v.RepoID).Scan(&hookID, &callback, &checked)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		at, err := ParseTime(checked)
		if err != nil {
			return err
		}
		digest := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%d\x00%s\x00%s\x00%s", v.RepoID, v.ID, v.Provider, v.Origin, containerID)))
		out = TicketSubscription{ID: fmt.Sprintf("legacy-%x", digest[:16]), RepoID: v.RepoID, ConnectionVersionID: v.ID, Revision: 1, Provider: v.Provider, Origin: v.Origin, Container: v.Destination, ContainerID: containerID, CallbackURL: callback, Events: []string{"issues", "issue_comment", "sub_issues"}, SecretService: "litefactory-webhook", SecretAccount: destination, SecretVersion: "legacy", DesiredEnabled: true, RemoteHookID: fmt.Sprintf("%d", hookID), ObservedState: "legacy_unverified", CheckedAt: at}
		data, err := json.Marshal(out)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO ticket_subscriptions(id,repo_id,connection_version_id,revision,provider,api_origin,container_id,remote_hook_id,document) VALUES(?,?,?,?,?,?,?,?,?)`, out.ID, out.RepoID, out.ConnectionVersionID, out.Revision, out.Provider, out.Origin, out.ContainerID, out.RemoteHookID, string(data))
		return err
	})
	return out, err
}

// AdoptSubscriptionRemoteHook records a uniquely verified owned hook before a
// newer revision dispatches an update, so losing that response retains its ID.
func (db *DB) AdoptSubscriptionRemoteHook(ctx context.Context, id string, revision int64, expectedRemoteID, remoteID string) error {
	if remoteID == "" {
		return ErrSubscriptionConflict
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		s, err := readSubscription(tx.QueryRowContext(ctx, `SELECT document FROM ticket_subscriptions WHERE id=?`, id))
		if err != nil {
			return err
		}
		if s.Revision != revision || s.RemoteHookID != expectedRemoteID {
			return ErrSubscriptionConflict
		}
		s.RemoteHookID = remoteID
		data, err := json.Marshal(s)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE ticket_subscriptions SET remote_hook_id=?,document=? WHERE id=? AND revision=?`, remoteID, string(data), id, revision)
		return err
	})
}
