package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/ticket"
)

// TicketBinding gives one qualified immutable ticket durable project ownership.
type TicketBinding struct {
	ID, RepoID, ConnectionVersionID, Revision int64
	Ref                                       ticket.Ref
	Enabled                                   bool
	Provenance                                string
}

const bindingColumns = `id,repo_id,connection_version_id,revision,provider,api_origin,remote_id,container,locator_key,enabled,provenance`

func scanBinding(row scanner) (TicketBinding, error) {
	var b TicketBinding
	err := row.Scan(&b.ID, &b.RepoID, &b.ConnectionVersionID, &b.Revision, &b.Ref.Identity.Provider, &b.Ref.Identity.Origin, &b.Ref.Identity.RemoteID, &b.Ref.Locator.Container, &b.Ref.Locator.Key, &b.Enabled, &b.Provenance)
	if errors.Is(err, sql.ErrNoRows) {
		return b, ErrNotFound
	}
	if err != nil {
		return b, err
	}
	b.Ref.Locator.Provider = b.Ref.Identity.Provider
	b.Ref.Locator.Origin = b.Ref.Identity.Origin
	return b, b.Ref.Validate()
}
func getBinding(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, identity ticket.Identity) (TicketBinding, error) {
	return scanBinding(q.QueryRowContext(ctx, `SELECT `+bindingColumns+` FROM project_ticket_bindings WHERE provider=? AND api_origin=? AND remote_id=?`, identity.Provider, identity.Origin, identity.RemoteID))
}

// GetTicketBinding resolves full provider, origin and immutable remote identity.
func (db *DB) GetTicketBinding(ctx context.Context, identity ticket.Identity) (TicketBinding, error) {
	if err := identity.Validate(); err != nil {
		return TicketBinding{}, err
	}
	return getBinding(ctx, db.sql, identity)
}

// GetTicketBindingByID reads retained ownership including disabled tombstones.
func (db *DB) GetTicketBindingByID(ctx context.Context, id int64) (TicketBinding, error) {
	return scanBinding(db.sql.QueryRowContext(ctx, `SELECT `+bindingColumns+` FROM project_ticket_bindings WHERE id=?`, id))
}

// ListTicketBindings includes tombstones for inspection.
func (db *DB) ListTicketBindings(ctx context.Context, repoID int64) ([]TicketBinding, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+bindingColumns+` FROM project_ticket_bindings WHERE repo_id=? ORDER BY id`, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TicketBinding
	for rows.Next() {
		b, err := scanBinding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
func validateBinding(ctx context.Context, tx *sql.Tx, b TicketBinding) error {
	if err := b.Ref.Validate(); err != nil {
		return err
	}
	// A qualified address cannot name competing immutable tickets. Include
	// disabled bindings and prerequisite-era identities so a tombstone cannot
	// be bypassed by claiming a different remote ID at its original address.
	var conflicts int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM project_ticket_bindings WHERE provider=? AND api_origin=? AND container=? AND locator_key=? AND remote_id<>? AND id<>?`, b.Ref.Locator.Provider, b.Ref.Locator.Origin, b.Ref.Locator.Container, b.Ref.Locator.Key, b.Ref.Identity.RemoteID, b.ID).Scan(&conflicts); err != nil {
		return err
	}
	if conflicts > 0 {
		return ErrBindingConflict
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM session_ticket_identities WHERE provider=? AND api_origin=? AND container=? AND locator_key=? AND remote_id<>'' AND remote_id<>?`, b.Ref.Locator.Provider, b.Ref.Locator.Origin, b.Ref.Locator.Container, b.Ref.Locator.Key, b.Ref.Identity.RemoteID).Scan(&conflicts); err != nil {
		return err
	}
	if conflicts > 0 {
		return ErrBindingConflict
	}
	if b.Provenance == "" {
		return errors.New("store: binding provenance required")
	}
	v, err := scanConnection(tx.QueryRowContext(ctx, `SELECT `+connectionColumns+` FROM project_connection_versions WHERE id=?`, b.ConnectionVersionID))
	if err != nil {
		return err
	}
	if v.Role != TicketConnection || v.RepoID != b.RepoID || v.Provider != b.Ref.Identity.Provider || v.Origin != b.Ref.Identity.Origin || v.Destination != b.Ref.Locator.Container {
		return ErrBindingConflict
	}
	return nil
}
func addBinding(ctx context.Context, tx *sql.Tx, b TicketBinding) (TicketBinding, error) {
	b.ID = 0
	if err := validateBinding(ctx, tx, b); err != nil {
		return b, err
	}
	var historicalRepo int64
	err := tx.QueryRowContext(ctx, `SELECT sessions.repo_id FROM sessions JOIN session_ticket_identities ON sessions.id=session_ticket_identities.session_id WHERE provider=? AND api_origin=? AND remote_id=?`, b.Ref.Identity.Provider, b.Ref.Identity.Origin, b.Ref.Identity.RemoteID).Scan(&historicalRepo)
	if err == nil && historicalRepo != b.RepoID {
		return b, ErrBindingConflict
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return b, err
	}
	old, err := getBinding(ctx, tx, b.Ref.Identity)
	if err == nil {
		if old.RepoID != b.RepoID || old.ConnectionVersionID != b.ConnectionVersionID || old.Ref != b.Ref || old.Enabled != b.Enabled {
			return b, ErrBindingConflict
		}
		return old, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return b, err
	}
	return scanBinding(tx.QueryRowContext(ctx, `INSERT INTO project_ticket_bindings(repo_id,connection_version_id,revision,provider,api_origin,remote_id,container,locator_key,enabled,provenance) VALUES(?,?,1,?,?,?,?,?,?,?) RETURNING `+bindingColumns, b.RepoID, b.ConnectionVersionID, b.Ref.Identity.Provider, b.Ref.Identity.Origin, b.Ref.Identity.RemoteID, b.Ref.Locator.Container, b.Ref.Locator.Key, b.Enabled, b.Provenance))
}

// AddTicketBinding is idempotent only when all routing and ownership data agree.
func (db *DB) AddTicketBinding(ctx context.Context, b TicketBinding) (TicketBinding, error) {
	var out TicketBinding
	err := db.withTx(ctx, func(tx *sql.Tx) error { var err error; out, err = addBinding(ctx, tx, b); return err })
	return out, err
}

// CorrectTicketBinding changes an unused binding with a revision check. A used
// binding retains its identity and ownership permanently.
func (db *DB) CorrectTicketBinding(ctx context.Context, b TicketBinding, expectedRevision int64) (TicketBinding, error) {
	var out TicketBinding
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		old, err := scanBinding(tx.QueryRowContext(ctx, `SELECT `+bindingColumns+` FROM project_ticket_bindings WHERE id=?`, b.ID))
		if err != nil {
			return err
		}
		if old.Revision != expectedRevision {
			return ErrConnectionConflict
		}
		var used int
		err = tx.QueryRowContext(ctx, `SELECT count(*) FROM session_execution_snapshots WHERE binding_id=?`, b.ID).Scan(&used)
		if err != nil {
			return err
		}
		if used > 0 {
			return ErrBindingConflict
		}
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM session_ticket_identities WHERE provider=? AND api_origin=? AND remote_id=?`, old.Ref.Identity.Provider, old.Ref.Identity.Origin, old.Ref.Identity.RemoteID).Scan(&used); err != nil {
			return err
		}
		if used > 0 {
			return ErrBindingConflict
		}
		if err := validateBinding(ctx, tx, b); err != nil {
			return err
		}
		// The proposed identity may already have prerequisite-era saved work
		// without a project binding. Neither another project nor this project
		// may replace that historical ownership through an unused binding.
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM session_ticket_identities WHERE provider=? AND api_origin=? AND remote_id=?`, b.Ref.Identity.Provider, b.Ref.Identity.Origin, b.Ref.Identity.RemoteID).Scan(&used); err != nil {
			return err
		}
		if used > 0 {
			return ErrBindingConflict
		}
		out, err = scanBinding(tx.QueryRowContext(ctx, `UPDATE project_ticket_bindings SET repo_id=?,connection_version_id=?,revision=revision+1,provider=?,api_origin=?,remote_id=?,container=?,locator_key=?,enabled=?,provenance=? WHERE id=? RETURNING `+bindingColumns, b.RepoID, b.ConnectionVersionID, b.Ref.Identity.Provider, b.Ref.Identity.Origin, b.Ref.Identity.RemoteID, b.Ref.Locator.Container, b.Ref.Locator.Key, b.Enabled, b.Provenance, b.ID))
		if err != nil {
			return fmt.Errorf("%w: %w", ErrBindingConflict, err)
		}
		return nil
	})
	return out, err
}

// DisableTicketBinding preserves a tombstone and all historical session links.
func (db *DB) DisableTicketBinding(ctx context.Context, id, expectedRevision int64) (TicketBinding, error) {
	var out TicketBinding
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		old, err := scanBinding(tx.QueryRowContext(ctx, `SELECT `+bindingColumns+` FROM project_ticket_bindings WHERE id=?`, id))
		if err != nil {
			return err
		}
		if old.Revision != expectedRevision {
			return ErrConnectionConflict
		}
		out, err = scanBinding(tx.QueryRowContext(ctx, `UPDATE project_ticket_bindings SET enabled=0,revision=revision+1 WHERE id=? RETURNING `+bindingColumns, id))
		return err
	})
	return out, err
}

// CheckTicketBindingSource explains whether a retained binding can be admitted
// through this version. It does not change binding provenance or enrollment.
func (db *DB) CheckTicketBindingSource(ctx context.Context, b TicketBinding, current ConnectionVersion) error {
	return checkTicketBindingSource(ctx, db.sql, b, current, "")
}

func checkTicketBindingSource(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, b TicketBinding, current ConnectionVersion, verifiedID string) error {
	conflict := func(reason string) error { return fmt.Errorf("%w: %s", ErrBindingConflict, reason) }
	if b.RepoID != current.RepoID || current.Role != TicketConnection || b.Ref.Identity.Provider != current.Provider || b.Ref.Identity.Origin != current.Origin || b.Ref.Locator.Container != current.Destination {
		return conflict("project, provider, origin or destination mismatch")
	}
	if b.ConnectionVersionID == current.ID {
		return nil
	}
	old, err := scanConnection(q.QueryRowContext(ctx, `SELECT `+connectionColumns+` FROM project_connection_versions WHERE id=?`, b.ConnectionVersionID))
	if err != nil {
		return err
	}
	if old.RepoID != current.RepoID || old.Role != TicketConnection || old.Provider != current.Provider || old.Origin != current.Origin || old.Destination != current.Destination {
		return conflict("retained project, provider, origin or destination mismatch")
	}
	linearProven := false
	if current.Provider == ticket.Linear {
		if old.Linear.WorkspaceID != current.Linear.WorkspaceID || old.Linear.TeamID != current.Linear.TeamID || old.Linear.ProjectID != current.Linear.ProjectID {
			return conflict("Linear workspace, team or project mismatch")
		}
		linearProven = old.Linear.WorkspaceID != "" && old.Linear.TeamID != "" && old.Linear.ProjectID != ""
	}
	// Recovery may have verified a container whose immutable ID was not supplied
	// at configuration time. Check every available piece of evidence for conflict.
	ids := [2]string{}
	for i, v := range []ConnectionVersion{old, current} {
		ids[i] = v.RemoteID
		position, err := readRecovery(q.QueryRowContext(ctx, `SELECT document FROM ticket_recovery_positions WHERE connection_version_id=?`, v.ID))
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		if err == nil && position.ContainerID != "" {
			if ids[i] != "" && ids[i] != position.ContainerID {
				return conflict("immutable container evidence conflicts")
			}
			ids[i] = position.ContainerID
		}
	}
	if verifiedID != "" {
		if ids[1] != "" && ids[1] != verifiedID {
			return conflict("verified container evidence conflicts")
		}
		ids[1] = verifiedID
	}
	if linearProven && ((ids[0] != "" && ids[0] != old.Linear.ProjectID) || (ids[1] != "" && ids[1] != current.Linear.ProjectID)) {
		return conflict("Linear project and immutable container evidence conflict")
	}
	if ids[0] != "" && ids[1] != "" && ids[0] != ids[1] {
		return conflict("immutable container mismatch")
	}
	if !linearProven && (ids[0] == "" || ids[1] == "") {
		return conflict("unproven immutable source identity")
	}
	return nil
}
