package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// CITransportState is isolated from accepted observations and alert episodes.
type CITransportState struct {
	NotBefore  time.Time
	Provenance string
	Failures   int
	Lease      string
	LeaseUntil time.Time
}

func ciTransportRead(ctx context.Context, tx *sql.Tx, key string) (CITransportState, error) {
	var out CITransportState
	var bound, leaseUntil string
	err := tx.QueryRowContext(ctx, `SELECT not_before,provenance,failures,lease,lease_until FROM ci_transport WHERE key=?`, key).Scan(&bound, &out.Provenance, &out.Failures, &out.Lease, &leaseUntil)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if bound != "" {
		out.NotBefore, err = ParseTime(bound)
		if err != nil {
			return out, err
		}
	}
	if leaseUntil != "" {
		out.LeaseUntil, err = ParseTime(leaseUntil)
	}
	return out, err
}
func ciTransportWrite(ctx context.Context, tx *sql.Tx, key string, s CITransportState) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO ci_transport(key,not_before,provenance,failures,lease,lease_until) VALUES(?,?,?,?,?,?) ON CONFLICT(key) DO UPDATE SET not_before=excluded.not_before,provenance=excluded.provenance,failures=excluded.failures,lease=excluded.lease,lease_until=excluded.lease_until`, key, ciTransportTime(s.NotBefore), s.Provenance, s.Failures, s.Lease, ciTransportTime(s.LeaseUntil))
	return err
}
func ciTransportTime(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return FormatTime(at)
}

// ClaimCITransport atomically fences requests across independent callers. The
// lease is retained if the process exits during its HTTP request. Expiry alone
// cannot establish whether an unrecorded server wait has elapsed.
func (db *DB) ClaimCITransport(ctx context.Context, key, lease string, now, until time.Time) (CITransportState, bool, error) {
	var out CITransportState
	var admitted bool
	if key == "" || lease == "" || !until.After(now) {
		return out, false, fmt.Errorf("store: invalid CI transport claim")
	}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = ciTransportRead(ctx, tx, key)
		if err != nil {
			return err
		}
		if out.NotBefore.After(now) || out.Lease != "" {
			return nil
		}
		out.Lease = lease
		out.LeaseUntil = until
		if err = ciTransportWrite(ctx, tx, key, out); err != nil {
			return err
		}
		admitted = true
		return nil
	})
	return out, admitted, err
}

// FinishCITransport merges a server wait without shortening an existing bound.
// Only the lease holder may update this transport record.
func (db *DB) FinishCITransport(ctx context.Context, key, lease string, bound time.Time, provenance string, limited bool) (CITransportState, error) {
	var out CITransportState
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = ciTransportRead(ctx, tx, key)
		if err != nil {
			return err
		}
		if out.Lease != lease || lease == "" {
			return fmt.Errorf("store: stale CI transport lease")
		}
		if bound.After(out.NotBefore) {
			out.NotBefore = bound
			out.Provenance = provenance
		}
		if limited {
			out.Failures++
		} else {
			out.Failures = 0
		}
		out.Lease = ""
		out.LeaseUntil = time.Time{}
		return ciTransportWrite(ctx, tx, key, out)
	})
	return out, err
}

// CITransport reads diagnostic wait state without changing observation state.
func (db *DB) CITransport(ctx context.Context, key string) (CITransportState, error) {
	var out CITransportState
	err := db.withTx(ctx, func(tx *sql.Tx) error { var err error; out, err = ciTransportRead(ctx, tx, key); return err })
	return out, err
}
