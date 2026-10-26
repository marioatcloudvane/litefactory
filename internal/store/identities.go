package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ForgeGitHub is the only forge litefactory supports at M0 (AC-0.8). It is a
// column value rather than an assumption baked into queries, so LF-18 can add
// a second forge without a migration.
const ForgeGitHub = "github"

// IdentityRole distinguishes the two identities litefactory holds: the human
// whose instructions it acts on, and the account it posts as.
type IdentityRole string

// The identity roles. There are exactly two, and they must never be the same
// account — a bot that reads its own comments as instructions is an infinite
// loop (AC-0.6).
const (
	// RoleOwner is Mario: the author whose comments become instructions.
	RoleOwner IdentityRole = "owner"
	// RoleBot is the account litefactory posts reports as.
	RoleBot IdentityRole = "bot"
)

// IdentitySource records how an identity was discovered, so a stale row can be
// explained later.
type IdentitySource string

// The identity sources.
const (
	// SourceGH means the identity came from the `gh` CLI. Only `init` uses it,
	// and only for the owner.
	SourceGH IdentitySource = "gh"
	// SourceForgeAPI means the identity came from the forge's own API, which
	// is how the bot resolves its own handle from its token.
	SourceForgeAPI IdentitySource = "forge-api"
)

// Identity is one (forge, role) identity.
type Identity struct {
	// Forge is the forge this identity lives on, [ForgeGitHub] at M0.
	Forge string
	// Role is owner or bot.
	Role IdentityRole
	// Handle is the login, for example marioatcloudvane or litefactory[bot].
	// It is for display only: GitHub handles can be renamed.
	Handle string
	// UserID is the numeric forge id. This is the durable identity, and the
	// one the self-filter and the instruction/third_party classification
	// compare (D-I). Zero means it is not known.
	UserID int64
	// Source is how the identity was discovered.
	Source IdentitySource
	// DerivedAt is when the store last wrote this row.
	DerivedAt time.Time
}

// UpsertIdentity writes id, replacing any existing row for the same
// (forge, role). Re-running `init` therefore refreshes both handles and both
// numeric ids without creating a duplicate row (AC-0.7).
//
// DerivedAt is set by the store; whatever the caller put in that field is
// ignored. The stored value is returned. A numeric GitHub owner change also
// advances connector authority and invalidates its grants and pending browser
// authorizations in the same write, through the connector migration triggers.
func (db *DB) UpsertIdentity(ctx context.Context, id Identity) (Identity, error) {
	if id.Forge == "" || id.Role == "" {
		return Identity{}, errors.New("store: upsert identity: forge and role are required")
	}
	id.DerivedAt = nowUTC()

	const stmt = `
INSERT INTO identities (forge, role, handle, user_id, source, derived_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (forge, role) DO UPDATE SET
    handle     = excluded.handle,
    user_id    = excluded.user_id,
    source     = excluded.source,
    derived_at = excluded.derived_at`

	_, err := db.sql.ExecContext(ctx, stmt,
		id.Forge, string(id.Role), id.Handle, nullID(id.UserID), string(id.Source), FormatTime(id.DerivedAt))
	if err != nil {
		return Identity{}, fmt.Errorf("store: upsert identity %s/%s: %w", id.Forge, id.Role, err)
	}
	return id, nil
}

// GetIdentity reads the identity for (forge, role), returning [ErrNotFound]
// when it has not been derived yet. A missing bot identity is normal before
// the bot credential is configured (AC-0.5).
func (db *DB) GetIdentity(ctx context.Context, forge string, role IdentityRole) (Identity, error) {
	const query = `
SELECT forge, role, handle, user_id, source, derived_at
FROM identities
WHERE forge = ? AND role = ?`

	id, err := scanIdentity(db.sql.QueryRowContext(ctx, query, forge, string(role)))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Identity{}, fmt.Errorf("store: identity %s/%s: %w", forge, role, ErrNotFound)
		}
		return Identity{}, fmt.Errorf("store: read identity %s/%s: %w", forge, role, err)
	}
	return id, nil
}

// ListIdentities returns every identity on forge, ordered by role.
func (db *DB) ListIdentities(ctx context.Context, forge string) ([]Identity, error) {
	const query = `
SELECT forge, role, handle, user_id, source, derived_at
FROM identities
WHERE forge = ?
ORDER BY role`

	rows, err := db.sql.QueryContext(ctx, query, forge)
	if err != nil {
		return nil, fmt.Errorf("store: list identities: %w", err)
	}
	defer rows.Close()

	var out []Identity
	for rows.Next() {
		id, err := scanIdentity(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list identities: %w", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list identities: %w", err)
	}
	return out, nil
}

// scanner is the shared shape of *sql.Row and *sql.Rows, so one scan function
// serves both a single-row read and a listing.
type scanner interface {
	Scan(dest ...any) error
}

func scanIdentity(s scanner) (Identity, error) {
	var (
		id        Identity
		role      string
		source    string
		userID    sql.NullInt64
		derivedAt sql.NullString
	)
	if err := s.Scan(&id.Forge, &role, &id.Handle, &userID, &source, &derivedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Identity{}, ErrNotFound
		}
		return Identity{}, err
	}
	id.Role = IdentityRole(role)
	id.Source = IdentitySource(source)
	id.UserID = scanID(userID)

	var err error
	if id.DerivedAt, err = scanTime(derivedAt); err != nil {
		return Identity{}, err
	}
	return id, nil
}
