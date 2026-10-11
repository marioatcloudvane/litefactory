package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/ticket"
)

// ErrConnectionConflict indicates a stale configuration revision.
var ErrConnectionConflict = errors.New("project connection revision conflict")

// ErrBindingConflict indicates incompatible or historically used ownership.
var ErrBindingConflict = errors.New("ticket binding ownership conflict")

// ErrConnectionDisabled prevents admission through a disabled connection.
var ErrConnectionDisabled = errors.New("project connection is disabled")

// ConnectionRole keeps code delivery independent of ticket routing.
type ConnectionRole string

// Project connection roles select independent code and ticket configuration.
const (
	CodeConnection   ConnectionRole = "code"
	TicketConnection ConnectionRole = "ticket"
)

// ConnectionVersion is immutable trusted configuration. Credential fields are
// references only; secret values never enter persistence.
type ConnectionVersion struct {
	ID, RepoID, Revision                                                  int64
	Role                                                                  ConnectionRole
	Provider                                                              ticket.Provider
	Origin                                                                ticket.Origin
	Destination, RemoteID                                                 string
	CredentialSource, CredentialService, CredentialAccount, CredentialEnv string
	Enabled                                                               bool
	Provenance                                                            string
	ClonePath, RemoteURL, BaseBranch, FeaturePrefix, WorktreeRoot         string
	CreatedAt                                                             time.Time
}

const connectionColumns = `id,repo_id,revision,role,provider,api_origin,destination,remote_id,credential_source,credential_service,credential_account,credential_env,enabled,provenance,clone_path,remote_url,base_branch,feature_prefix,worktree_root,created_at`

func scanConnection(row scanner) (ConnectionVersion, error) {
	var v ConnectionVersion
	var created string
	err := row.Scan(&v.ID, &v.RepoID, &v.Revision, &v.Role, &v.Provider, &v.Origin, &v.Destination, &v.RemoteID, &v.CredentialSource, &v.CredentialService, &v.CredentialAccount, &v.CredentialEnv, &v.Enabled, &v.Provenance, &v.ClonePath, &v.RemoteURL, &v.BaseBranch, &v.FeaturePrefix, &v.WorktreeRoot, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	v.CreatedAt, err = ParseTime(created)
	return v, err
}
func currentConnection(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, repoID int64, role ConnectionRole) (ConnectionVersion, error) {
	return scanConnection(q.QueryRowContext(ctx, `SELECT `+connectionColumns+` FROM project_connection_versions WHERE id=(SELECT version_id FROM project_connections WHERE repo_id=? AND role=?)`, repoID, role))
}

// CurrentConnection returns the current immutable version for a project role.
func (db *DB) CurrentConnection(ctx context.Context, repoID int64, role ConnectionRole) (ConnectionVersion, error) {
	return currentConnection(ctx, db.sql, repoID, role)
}

// GetConnectionVersion reads retained configuration, including disabled history.
func (db *DB) GetConnectionVersion(ctx context.Context, id int64) (ConnectionVersion, error) {
	return scanConnection(db.sql.QueryRowContext(ctx, `SELECT `+connectionColumns+` FROM project_connection_versions WHERE id=?`, id))
}

// ListConnections returns both configured project roles.
func (db *DB) ListConnections(ctx context.Context, repoID int64) ([]ConnectionVersion, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+connectionColumns+` FROM project_connection_versions WHERE id IN(SELECT version_id FROM project_connections WHERE repo_id=?) ORDER BY role`, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ConnectionVersion
	for rows.Next() {
		v, err := scanConnection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

var credentialName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func validateConnection(v ConnectionVersion) error {
	if v.RepoID <= 0 || (v.Role != CodeConnection && v.Role != TicketConnection) || v.Provider != ticket.GitHub {
		return errors.New("store: invalid project connection role or provider")
	}
	if err := (ticket.Locator{Provider: v.Provider, Origin: v.Origin, Container: v.Destination, Key: "1"}).Validate(); err != nil {
		return err
	}
	parts := strings.Split(v.Destination, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.ContainsAny(v.Destination, "?#@:\\ \n\r\t") {
		return errors.New("store: destination must be owner/repository")
	}
	switch v.CredentialSource {
	case "env":
		if !credentialName.MatchString(v.CredentialEnv) || v.CredentialService != "" || v.CredentialAccount != "" {
			return errors.New("store: invalid environment credential reference")
		}
	case "keychain":
		if v.CredentialService == "" || v.CredentialAccount == "" || v.CredentialEnv != "" || strings.ContainsAny(v.CredentialService+v.CredentialAccount, "\n\r\x00") {
			return errors.New("store: invalid keychain credential reference")
		}
	case "":
		if v.CredentialService != "" || v.CredentialAccount != "" || v.CredentialEnv != "" {
			return errors.New("store: invalid missing credential reference")
		}
	default:
		return errors.New("store: unsupported credential reference")
	}
	if v.RemoteURL != "" && strings.Contains(v.RemoteURL, "://") {
		remote, err := url.Parse(v.RemoteURL)
		if err != nil {
			return errors.New("store: invalid code remote URL")
		}
		if remote.RawQuery != "" || remote.Fragment != "" || remote.ForceQuery {
			return errors.New("store: code remote URL must not contain credentials, query or fragment")
		}
		if remote.User != nil {
			_, hasPassword := remote.User.Password()
			if hasPassword || remote.Scheme == "http" || remote.Scheme == "https" {
				return errors.New("store: code remote URL must not contain credentials")
			}
		}
	}
	if v.Provenance == "" {
		return errors.New("store: connection provenance is required")
	}
	return nil
}
func setConnection(ctx context.Context, tx *sql.Tx, v ConnectionVersion, expected int64) (ConnectionVersion, error) {
	if err := validateConnection(v); err != nil {
		return ConnectionVersion{}, err
	}
	old, err := currentConnection(ctx, tx, v.RepoID, v.Role)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return ConnectionVersion{}, err
	}
	if (err == nil && old.Revision != expected) || (errors.Is(err, ErrNotFound) && expected != 0) {
		return ConnectionVersion{}, ErrConnectionConflict
	}
	v, err = retainConnection(ctx, tx, v)
	if err != nil {
		return v, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO project_connections(repo_id,role,version_id) VALUES(?,?,?) ON CONFLICT(repo_id,role) DO UPDATE SET version_id=excluded.version_id`, v.RepoID, v.Role, v.ID)
	return v, err
}

func retainConnection(ctx context.Context, tx *sql.Tx, v ConnectionVersion) (ConnectionVersion, error) {
	if err := validateConnection(v); err != nil {
		return ConnectionVersion{}, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(revision),0)+1 FROM project_connection_versions WHERE repo_id=? AND role=?`, v.RepoID, v.Role).Scan(&v.Revision); err != nil {
		return ConnectionVersion{}, err
	}
	v.CreatedAt = nowUTC()
	return scanConnection(tx.QueryRowContext(ctx, `INSERT INTO project_connection_versions(repo_id,revision,role,provider,api_origin,destination,remote_id,credential_source,credential_service,credential_account,credential_env,enabled,provenance,clone_path,remote_url,base_branch,feature_prefix,worktree_root,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) RETURNING `+connectionColumns, v.RepoID, v.Revision, v.Role, v.Provider, v.Origin, v.Destination, v.RemoteID, v.CredentialSource, v.CredentialService, v.CredentialAccount, v.CredentialEnv, v.Enabled, v.Provenance, v.ClonePath, v.RemoteURL, v.BaseBranch, v.FeaturePrefix, v.WorktreeRoot, FormatTime(v.CreatedAt)))
}

// RetainConnectionVersion saves historical evidence without changing current routing.
func (db *DB) RetainConnectionVersion(ctx context.Context, v ConnectionVersion) (ConnectionVersion, error) {
	var out ConnectionVersion
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if err := validateConnection(v); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT `+connectionColumns+` FROM project_connection_versions WHERE repo_id=? AND role=? ORDER BY id`, v.RepoID, v.Role)
		if err != nil {
			return err
		}
		wanted := v
		wanted.ID = 0
		wanted.Revision = 0
		wanted.CreatedAt = time.Time{}
		for rows.Next() {
			existing, err := scanConnection(rows)
			if err != nil {
				return errors.Join(err, rows.Close())
			}
			candidate := existing
			candidate.ID = 0
			candidate.Revision = 0
			candidate.CreatedAt = time.Time{}
			if candidate == wanted {
				out = existing
				return rows.Close()
			}
		}
		if err := rows.Err(); err != nil {
			return errors.Join(err, rows.Close())
		}
		if err := rows.Close(); err != nil {
			return err
		}
		out, err = retainConnection(ctx, tx, v)
		return err
	})
	return out, err
}

// SetConnection creates a version and atomically moves its revision-checked pointer.
func (db *DB) SetConnection(ctx context.Context, v ConnectionVersion, expectedRevision int64) (ConnectionVersion, error) {
	var out ConnectionVersion
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = setConnection(ctx, tx, v, expectedRevision)
		return err
	})
	return out, err
}

// DisableConnection retains original versions for running work.
func (db *DB) DisableConnection(ctx context.Context, repoID int64, role ConnectionRole, expectedRevision int64) (ConnectionVersion, error) {
	v, err := db.CurrentConnection(ctx, repoID, role)
	if err != nil {
		return v, err
	}
	v.Enabled = false
	v.Provenance = "explicit"
	return db.SetConnection(ctx, v, expectedRevision)
}

// ConnectionObservation is sanitized evidence from a bounded read-only check.
type ConnectionObservation struct {
	VersionID              int64
	CheckedAt              time.Time
	Status, Reason, Remedy string
	Capabilities           []string
}

// SaveConnectionObservation rejects a response for a superseded version.
func (db *DB) SaveConnectionObservation(ctx context.Context, o ConnectionObservation) error {
	if o.VersionID <= 0 || o.CheckedAt.IsZero() || len(o.Status) > 128 || len(o.Reason) > 128 || len(o.Remedy) > 512 || len(o.Capabilities) > 16 {
		return errors.New("store: invalid connection observation")
	}
	for _, c := range o.Capabilities {
		if len(c) > 128 {
			return errors.New("store: invalid connection capability")
		}
	}
	capabilities, err := json.Marshal(o.Capabilities)
	if err != nil {
		return err
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var current int64
		err := tx.QueryRowContext(ctx, `SELECT version_id FROM project_connections WHERE version_id=?`, o.VersionID).Scan(&current)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrConnectionConflict
		}
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO project_connection_observations(version_id,checked_at,status,reason,remedy,capabilities) VALUES(?,?,?,?,?,?) ON CONFLICT(version_id) DO UPDATE SET checked_at=excluded.checked_at,status=excluded.status,reason=excluded.reason,remedy=excluded.remedy,capabilities=excluded.capabilities`, o.VersionID, FormatTime(o.CheckedAt), o.Status, o.Reason, o.Remedy, string(capabilities))
		return err
	})
}

// GetConnectionObservation reads the latest observation for exactly this version.
func (db *DB) GetConnectionObservation(ctx context.Context, versionID int64) (ConnectionObservation, error) {
	var o ConnectionObservation
	var at, caps string
	err := db.sql.QueryRowContext(ctx, `SELECT version_id,checked_at,status,reason,remedy,capabilities FROM project_connection_observations WHERE version_id=?`, versionID).Scan(&o.VersionID, &at, &o.Status, &o.Reason, &o.Remedy, &caps)
	if errors.Is(err, sql.ErrNoRows) {
		return o, ErrNotFound
	}
	if err != nil {
		return o, err
	}
	o.CheckedAt, err = ParseTime(at)
	if err != nil {
		return o, err
	}
	err = json.Unmarshal([]byte(caps), &o.Capabilities)
	return o, err
}

// ProjectImportState records unresolved historical evidence separately from health.
type ProjectImportState struct {
	RepoID         int64
	Status, Reason string
	UpdatedAt      time.Time
}

// SetProjectImportState persists a bounded reason code, never a remote error body.
func (db *DB) SetProjectImportState(ctx context.Context, s ProjectImportState) error {
	if s.RepoID <= 0 || len(s.Status) > 64 || len(s.Reason) > 128 {
		return errors.New("store: invalid import state")
	}
	_, err := db.sql.ExecContext(ctx, `INSERT INTO project_connection_imports(repo_id,status,reason,updated_at) VALUES(?,?,?,?) ON CONFLICT(repo_id) DO UPDATE SET status=excluded.status,reason=excluded.reason,updated_at=excluded.updated_at`, s.RepoID, s.Status, s.Reason, FormatTime(nowUTC()))
	return err
}

// GetProjectImportState reads initialization progress for this project.
func (db *DB) GetProjectImportState(ctx context.Context, repoID int64) (ProjectImportState, error) {
	var s ProjectImportState
	var at string
	err := db.sql.QueryRowContext(ctx, `SELECT repo_id,status,reason,updated_at FROM project_connection_imports WHERE repo_id=?`, repoID).Scan(&s.RepoID, &s.Status, &s.Reason, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return s, ErrNotFound
	}
	if err != nil {
		return s, err
	}
	s.UpdatedAt, err = ParseTime(at)
	return s, err
}

// ImportLegacyConnections seeds only absent roles; explicit and disabled settings survive repeated import.
func (db *DB) ImportLegacyConnections(ctx context.Context, code, tickets ConnectionVersion) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		for _, v := range []ConnectionVersion{code, tickets} {
			_, err := currentConnection(ctx, tx, v.RepoID, v.Role)
			if err == nil {
				continue
			}
			if !errors.Is(err, ErrNotFound) {
				return err
			}
			v.Provenance = "legacy"
			if _, err = setConnection(ctx, tx, v, 0); err != nil {
				return fmt.Errorf("store: import project connection: %w", err)
			}
		}
		return nil
	})
}
