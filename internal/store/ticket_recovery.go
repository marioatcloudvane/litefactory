package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/ticket"
)

// ErrRecoveryMigrationAmbiguous leaves uncertain legacy coverage unmapped.
var ErrRecoveryMigrationAmbiguous = errors.New("legacy ticket recovery source is ambiguous")

// ErrRecoveryConflict rejects a stale page or an unrelated source checkpoint.
var ErrRecoveryConflict = errors.New("ticket recovery revision conflict")

// TicketRecoveryPosition separates completed coverage from partial pages.
// Each immutable connection has its own source position, never relabelled.
type TicketRecoveryPosition struct {
	ConnectionVersionID, RepoID, Revision       int64
	Provider                                    ticket.Provider
	Origin                                      ticket.Origin
	Container, ContainerID, CursorFormat        string
	EnrollmentAt, CompletedThrough, ScanThrough time.Time
	TicketCursor, ReplyCursor, ReconcileCursor  string
	TicketsComplete, RepliesComplete            bool
	ReconcileCompletedAt                        time.Time
}

// PendingTicketObservation is a qualified durable reread signal. Generation
// prevents an older worker from removing a signal observed while it worked.
type PendingTicketObservation struct {
	RelatedSubscriptionID                       string
	RelatedSubscriptionRevision                 int64
	SubscriptionID                              string
	SubscriptionRevision                        int64
	ConnectionVersionID, RepoID, Generation     int64
	Provider                                    ticket.Provider
	Origin                                      ticket.Origin
	Container, ContainerID, TicketID, TicketKey string
	ObservedAt                                  time.Time
}

func readRecovery(row scanner) (TicketRecoveryPosition, error) {
	var raw string
	if err := row.Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return TicketRecoveryPosition{}, ErrNotFound
		}
		return TicketRecoveryPosition{}, err
	}
	var out TicketRecoveryPosition
	err := json.Unmarshal([]byte(raw), &out)
	return out, err
}

// GetTicketRecoveryPosition returns the durable position for an exact version.
func (db *DB) GetTicketRecoveryPosition(ctx context.Context, versionID int64) (TicketRecoveryPosition, error) {
	return readRecovery(db.sql.QueryRowContext(ctx, `SELECT document FROM ticket_recovery_positions WHERE connection_version_id=?`, versionID))
}

// InitTicketRecoveryPosition keeps existing positions and imports legacy
// coverage only for the single proven legacy GitHub source. Registration time
// remains enrollment; a replacement connection never inherits legacy coverage.
func (db *DB) InitTicketRecoveryPosition(ctx context.Context, v ConnectionVersion, enrollment time.Time) (TicketRecoveryPosition, error) {
	var out TicketRecoveryPosition
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		existing, err := readRecovery(tx.QueryRowContext(ctx, `SELECT document FROM ticket_recovery_positions WHERE connection_version_id=?`, v.ID))
		if err == nil {
			if existing.RepoID != v.RepoID || existing.Provider != v.Provider || existing.Origin != v.Origin || existing.Container != v.Destination || (v.RemoteID != "" && existing.ContainerID != v.RemoteID) {
				return ErrRecoveryConflict
			}
			out = existing
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		trusted, err := scanConnection(tx.QueryRowContext(ctx, `SELECT `+connectionColumns+` FROM project_connection_versions WHERE id=?`, v.ID))
		if err != nil {
			return err
		}
		if trusted != v || v.Role != TicketConnection || enrollment.IsZero() {
			return ErrRecoveryConflict
		}
		out = TicketRecoveryPosition{ConnectionVersionID: v.ID, RepoID: v.RepoID, Revision: 1, Provider: v.Provider, Origin: v.Origin, Container: v.Destination, ContainerID: v.RemoteID, CursorFormat: "github-time-v1", EnrollmentAt: enrollment, CompletedThrough: enrollment}
		if v.Provider == ticket.GitHub && v.Provenance == "legacy" {
			var destination string
			if err := tx.QueryRowContext(ctx, `SELECT owner || '/' || name FROM repos WHERE id=?`, v.RepoID).Scan(&destination); err != nil {
				return err
			}
			// Revisions of the same legacy source (such as the RemoteID-pinned
			// import revision) are not a second candidate.
			rows, err := tx.QueryContext(ctx, `SELECT id, destination FROM project_connection_versions WHERE repo_id=? AND role='ticket' AND NOT (provenance='legacy' AND provider=? AND api_origin=? AND destination=?) ORDER BY id`, v.RepoID, v.Provider, v.Origin, v.Destination)
			if err != nil {
				return err
			}
			var candidates []string
			for rows.Next() {
				var id int64
				var other string
				if err := rows.Scan(&id, &other); err != nil {
					_ = rows.Close()
					return err
				}
				candidates = append(candidates, fmt.Sprintf("version %d %s", id, other))
			}
			if err := errors.Join(rows.Err(), rows.Close()); err != nil {
				return err
			}
			if len(candidates) > 0 || destination != v.Destination {
				return fmt.Errorf("%w: repo %s, legacy version %d %s, other candidates [%s]", ErrRecoveryMigrationAmbiguous, destination, v.ID, v.Destination, strings.Join(candidates, ", "))
			}
			var raw sql.NullString
			err = tx.QueryRowContext(ctx, `SELECT listed_up_to FROM repo_poll_cursors WHERE repo_id=?`, v.RepoID).Scan(&raw)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if err == nil {
				at, err := scanTime(raw)
				if err != nil {
					return err
				}
				if !at.IsZero() {
					out.CompletedThrough = at
				}
			}
		}
		data, err := json.Marshal(out)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO ticket_recovery_positions(connection_version_id,revision,document) VALUES(?,?,?)`, v.ID, out.Revision, string(data))
		return err
	})
	return out, err
}

func handoffObservation(ctx context.Context, tx *sql.Tx, p PendingTicketObservation) error {
	if p.ConnectionVersionID <= 0 || p.TicketID == "" || p.TicketKey == "" {
		return errors.New("store: invalid pending ticket observation")
	}
	v, err := scanConnection(tx.QueryRowContext(ctx, `SELECT `+connectionColumns+` FROM project_connection_versions WHERE id=?`, p.ConnectionVersionID))
	if err != nil {
		return err
	}
	if v.RepoID != p.RepoID || v.Role != TicketConnection || v.Provider != p.Provider || v.Origin != p.Origin || v.Destination != p.Container || (v.RemoteID != "" && v.RemoteID != p.ContainerID) {
		return ErrRecoveryConflict
	}
	if v.RemoteID == "" {
		position, err := readRecovery(tx.QueryRowContext(ctx, `SELECT document FROM ticket_recovery_positions WHERE connection_version_id=?`, v.ID))
		if err != nil {
			return err
		}
		if position.ContainerID == "" || position.ContainerID != p.ContainerID {
			return ErrRecoveryConflict
		}
	}
	var generation int64
	if err := tx.QueryRowContext(ctx, `INSERT INTO ticket_observation_generations(connection_version_id,ticket_id,generation) VALUES(?,?,1) ON CONFLICT(connection_version_id,ticket_id) DO UPDATE SET generation=generation+1 RETURNING generation`, p.ConnectionVersionID, p.TicketID).Scan(&generation); err != nil {
		return err
	}
	p.Generation = generation
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO pending_ticket_observations(connection_version_id,ticket_id,generation,document) VALUES(?,?,?,?) ON CONFLICT(connection_version_id,ticket_id) DO UPDATE SET generation=excluded.generation,document=excluded.document`, p.ConnectionVersionID, p.TicketID, p.Generation, string(raw))
	return err
}

// HandoffTicketObservation persists authenticated hints through restart.
func (db *DB) HandoffTicketObservation(ctx context.Context, p PendingTicketObservation) error {
	return db.withTx(ctx, func(tx *sql.Tx) error { return handoffObservation(ctx, tx, p) })
}

// CommitTicketRecoveryPage atomically hands off every observation and the page
// continuation. Completed coverage requires both discovery streams to finish.
func (db *DB) CommitTicketRecoveryPage(ctx context.Context, p TicketRecoveryPosition, expectedRevision int64, observations []PendingTicketObservation) (TicketRecoveryPosition, error) {
	var out TicketRecoveryPosition
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		old, err := readRecovery(tx.QueryRowContext(ctx, `SELECT document FROM ticket_recovery_positions WHERE connection_version_id=?`, p.ConnectionVersionID))
		if err != nil {
			return err
		}
		if old.Revision != expectedRevision || old.RepoID != p.RepoID || old.Provider != p.Provider || old.Origin != p.Origin || old.Container != p.Container || old.ContainerID != p.ContainerID || !old.EnrollmentAt.Equal(p.EnrollmentAt) || p.CompletedThrough.Before(old.CompletedThrough) {
			return ErrRecoveryConflict
		}
		completedCycle := old.TicketsComplete && old.RepliesComplete && old.CompletedThrough.Equal(old.ScanThrough)
		if !completedCycle && !old.ScanThrough.IsZero() && (old.TicketCursor != "" || old.ReplyCursor != "" || old.TicketsComplete || old.RepliesComplete) && !old.ScanThrough.Equal(p.ScanThrough) {
			return ErrRecoveryConflict
		}
		if p.CompletedThrough.After(old.CompletedThrough) && (!p.TicketsComplete || !p.RepliesComplete || p.ScanThrough.IsZero() || !p.CompletedThrough.Equal(p.ScanThrough)) {
			return ErrRecoveryConflict
		}
		for _, observation := range observations {
			if observation.ConnectionVersionID != p.ConnectionVersionID || observation.ContainerID != p.ContainerID {
				return ErrRecoveryConflict
			}
			if err := handoffObservation(ctx, tx, observation); err != nil {
				return err
			}
		}
		p.Revision = old.Revision + 1
		raw, err := json.Marshal(p)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE ticket_recovery_positions SET revision=?,document=? WHERE connection_version_id=? AND revision=?`, p.Revision, string(raw), p.ConnectionVersionID, old.Revision)
		out = p
		return err
	})
	return out, err
}

// ListPendingTicketObservations returns durable signals without consuming them.
func (db *DB) ListPendingTicketObservations(ctx context.Context, versionID int64) ([]PendingTicketObservation, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT document FROM pending_ticket_observations WHERE (?=0 OR connection_version_id=?) ORDER BY connection_version_id,ticket_id`, versionID, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PendingTicketObservation
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var p PendingTicketObservation
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ClearPendingTicketObservation clears only the generation actually drained.
func (db *DB) ClearPendingTicketObservation(ctx context.Context, versionID int64, ticketID string, generation int64) error {
	_, err := db.sql.ExecContext(ctx, `DELETE FROM pending_ticket_observations WHERE connection_version_id=? AND ticket_id=? AND generation=?`, versionID, ticketID, generation)
	return err
}

// PinTicketRecoveryContainer records the authoritative immutable container ID
// once for legacy connections that previously retained only a locator.
func (db *DB) PinTicketRecoveryContainer(ctx context.Context, versionID, expectedRevision int64, containerID string) (TicketRecoveryPosition, error) {
	var out TicketRecoveryPosition
	if containerID == "" {
		return out, ErrRecoveryConflict
	}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		p, err := readRecovery(tx.QueryRowContext(ctx, `SELECT document FROM ticket_recovery_positions WHERE connection_version_id=?`, versionID))
		if err != nil {
			return err
		}
		if p.Revision != expectedRevision || (p.ContainerID != "" && p.ContainerID != containerID) {
			return ErrRecoveryConflict
		}
		v, err := scanConnection(tx.QueryRowContext(ctx, `SELECT `+connectionColumns+` FROM project_connection_versions WHERE id=?`, versionID))
		if err != nil {
			return err
		}
		if v.RemoteID != "" && v.RemoteID != containerID {
			return ErrRecoveryConflict
		}
		original := p
		if v.Enabled && p.ScanThrough.IsZero() && p.TicketCursor == "" && p.ReplyCursor == "" && p.ReconcileCursor == "" && p.ReconcileCompletedAt.IsZero() && !p.TicketsComplete && !p.RepliesComplete && p.CompletedThrough.Equal(p.EnrollmentAt) {
			enrollment, err := continuousTicketEnrollment(ctx, tx, v, containerID, p.EnrollmentAt)
			if err != nil {
				return err
			}
			p.EnrollmentAt = enrollment
			p.CompletedThrough = enrollment
		}
		if p.ContainerID == containerID && p.EnrollmentAt.Equal(original.EnrollmentAt) {
			out = p
			return nil
		}
		p.ContainerID = containerID
		p.Revision++
		data, err := json.Marshal(p)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE ticket_recovery_positions SET revision=?,document=? WHERE connection_version_id=? AND revision=?`, p.Revision, string(data), versionID, expectedRevision)
		out = p
		return err
	})
	return out, err
}

// HandoffSubscriptionObservation admits a webhook signal only while the
// authenticated subscription revision and its current connection remain enabled.
// The check and pending generation write share one local transaction.
func (db *DB) HandoffSubscriptionObservation(ctx context.Context, id string, revision int64, p PendingTicketObservation) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		sub, err := readSubscription(tx.QueryRowContext(ctx, `SELECT document FROM ticket_subscriptions WHERE id=?`, id))
		if err != nil {
			return err
		}
		current, err := currentConnection(ctx, tx, sub.RepoID, TicketConnection)
		if err != nil {
			return err
		}
		if !sub.DesiredEnabled || sub.Revision != revision || !current.Enabled || current.ID != sub.ConnectionVersionID || p.ConnectionVersionID != sub.ConnectionVersionID || p.RepoID != sub.RepoID || p.Provider != sub.Provider || p.Origin != sub.Origin || p.Container != sub.Container || p.ContainerID != sub.ContainerID {
			return ErrSubscriptionConflict
		}
		p.SubscriptionID = id
		p.SubscriptionRevision = revision
		return handoffObservation(ctx, tx, p)
	})
}

// HandoffRelatedTicketObservation preserves the authenticated originating
// subscription while independently checking a related ticket's configured
// destination. Its target may use polling only; explicit target disable still
// blocks new webhook admission.
func (db *DB) HandoffRelatedTicketObservation(ctx context.Context, authID string, authRevision int64, targetID string, targetRevision int64, p PendingTicketObservation) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		auth, err := readSubscription(tx.QueryRowContext(ctx, `SELECT document FROM ticket_subscriptions WHERE id=?`, authID))
		if err != nil {
			return err
		}
		source, err := currentConnection(ctx, tx, auth.RepoID, TicketConnection)
		if err != nil {
			return err
		}
		if !auth.DesiredEnabled || auth.Revision != authRevision || !source.Enabled || source.ID != auth.ConnectionVersionID || auth.Provider != p.Provider || auth.Origin != p.Origin {
			return ErrSubscriptionConflict
		}
		target, err := currentConnection(ctx, tx, p.RepoID, TicketConnection)
		if err != nil {
			return err
		}
		if !target.Enabled || target.ID != p.ConnectionVersionID || target.Provider != p.Provider || target.Origin != p.Origin || target.Destination != p.Container || (target.RemoteID != "" && target.RemoteID != p.ContainerID) {
			return ErrSubscriptionConflict
		}
		sub, err := readSubscription(tx.QueryRowContext(ctx, `SELECT document FROM ticket_subscriptions WHERE connection_version_id=?`, target.ID))
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		p.SubscriptionID = auth.ID
		p.SubscriptionRevision = auth.Revision
		p.RelatedSubscriptionID = ""
		p.RelatedSubscriptionRevision = 0
		if errors.Is(err, ErrNotFound) {
			if targetID != "" {
				return ErrSubscriptionConflict
			}
		} else {
			if !sub.DesiredEnabled || sub.Provider != p.Provider || sub.Origin != p.Origin || sub.Container != p.Container || sub.ContainerID != p.ContainerID || (targetID != "" && (sub.ID != targetID || sub.Revision != targetRevision)) {
				return ErrSubscriptionConflict
			}
			if sub.ID != auth.ID {
				p.RelatedSubscriptionID = sub.ID
				p.RelatedSubscriptionRevision = sub.Revision
			}
		}
		return handoffObservation(ctx, tx, p)
	})
}

// A credential edit keeps the enrolled source when every intervening version
// names the same proven immutable container. Coverage and continuations belong
// to their own version and are deliberately never copied.
func continuousTicketEnrollment(ctx context.Context, tx *sql.Tx, current ConnectionVersion, containerID string, enrollment time.Time) (time.Time, error) {
	rows, err := tx.QueryContext(ctx, `SELECT `+connectionColumns+` FROM project_connection_versions WHERE repo_id=? AND role='ticket' AND revision<? ORDER BY revision DESC`, current.RepoID, current.Revision)
	if err != nil {
		return enrollment, err
	}
	var previous []ConnectionVersion
	for rows.Next() {
		v, err := scanConnection(rows)
		if err != nil {
			return enrollment, errors.Join(err, rows.Close())
		}
		previous = append(previous, v)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return enrollment, err
	}
	for _, v := range previous {
		if !v.Enabled || v.Provider != current.Provider || v.Origin != current.Origin || v.Destination != current.Destination {
			break
		}
		position, err := readRecovery(tx.QueryRowContext(ctx, `SELECT document FROM ticket_recovery_positions WHERE connection_version_id=?`, v.ID))
		if err != nil && !errors.Is(err, ErrNotFound) {
			return enrollment, err
		}
		provenID := v.RemoteID
		if err == nil && position.ContainerID != "" {
			if provenID != "" && provenID != position.ContainerID {
				break
			}
			provenID = position.ContainerID
		}
		if provenID == "" || provenID != containerID {
			break
		}
		oldEnrollment := v.CreatedAt
		if err == nil {
			oldEnrollment = position.EnrollmentAt
		} else if v.Provenance == "legacy" {
			var registered string
			if err := tx.QueryRowContext(ctx, `SELECT created_at FROM repos WHERE id=?`, v.RepoID).Scan(&registered); err != nil {
				return enrollment, err
			}
			oldEnrollment, err = ParseTime(registered)
			if err != nil {
				return enrollment, err
			}
		}
		if oldEnrollment.Before(enrollment) {
			enrollment = oldEnrollment
		}
	}
	return enrollment, nil
}
