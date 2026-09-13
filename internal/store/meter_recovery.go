package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"time"
)

// OpenMeterReader opens an existing ledger without migrations or persistent
// pragmas. Preview must not create schema, measurements or coverage records.
func OpenMeterReader(ctx context.Context, path string) (*DB, error) {
	if path == "" {
		return nil, errors.New("store: missing database path")
	}
	u := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)"}
	handle, err := sql.Open(driverName, u.String())
	if err != nil {
		return nil, err
	}
	handle.SetMaxOpenConns(1)
	db := &DB{sql: handle, path: path}
	if err = handle.PingContext(ctx); err != nil {
		return nil, errors.Join(err, db.Close())
	}
	return db, nil
}

// MeterRecoveryScope binds provider discovery to an existing finalized attempt.
type MeterRecoveryScope struct {
	AttemptID, RootID, RootInvocationID, Fingerprint string
	From, To                                         time.Time
	Catalog                                          MeterCatalog
	ExistingSubtotals                                []string
}

// MeterRecoveryScope snapshots the finalized attempt and ledger state needed
// to preview and atomically revalidate a bounded historical repair.
func (db *DB) MeterRecoveryScope(ctx context.Context, attempt string) (scope MeterRecoveryScope, err error) {
	err = db.withTx(ctx, func(tx *sql.Tx) error { var e error; scope, e = readMeterRecoveryScope(ctx, tx, attempt); return e })
	return scope, err
}
func readMeterRecoveryScope(ctx context.Context, tx *sql.Tx, attempt string) (s MeterRecoveryScope, err error) {
	s.AttemptID = attempt
	var from, to, agentName, outcome, format, token string
	var segment int64
	err = tx.QueryRowContext(ctx, `SELECT a.started_at,COALESCE(a.finished_at,''),a.coding_agent,a.outcome,COALESCE(a.source_format,''),a.price_revision_id,r.segment_id,COALESCE(g.resume_token,'') FROM meter_attempts a JOIN runs r ON r.id=a.run_id JOIN segments g ON g.id=r.segment_id WHERE a.id=?`, attempt).Scan(&from, &to, &agentName, &outcome, &format, &s.Catalog.RevisionID, &segment, &token)
	if err != nil {
		return s, err
	}
	if agentName != "codex" || to == "" || outcome == "running" {
		return s, errors.New("store: recovery requires a finalized Codex attempt")
	}
	s.From, err = time.Parse(time.RFC3339Nano, from)
	if err != nil {
		return s, err
	}
	s.To, err = time.Parse(time.RFC3339Nano, to)
	if err != nil {
		return s, err
	}
	if !s.From.Before(s.To) {
		return s, errors.New("store: invalid historical window")
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,source_invocation_id FROM meter_invocations WHERE attempt_id=? AND parent_id IS NULL ORDER BY id LIMIT 2`, attempt)
	if err != nil {
		return s, err
	}
	count := 0
	for rows.Next() {
		count++
		err = rows.Scan(&s.RootInvocationID, &s.RootID)
		if err != nil {
			break
		}
	}
	err = errors.Join(err, rows.Err(), rows.Close())
	if err != nil {
		return s, err
	}
	if count != 1 {
		return s, errors.New("store: historical root is missing or ambiguous")
	}
	// Legacy root totals predate provider identities. Only their retained segment
	// binding can associate a real rollout; never derive an identity from a name.
	if format == "historical-log-reconstruction-v1" {
		s.RootID = token
	}
	if meterIdentity(s.RootID) != nil {
		return s, errors.New("store: historical provider identity unavailable")
	}
	var overlap int
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM meter_attempts a JOIN runs r ON r.id=a.run_id WHERE r.segment_id=? AND a.id<>? AND a.started_at<? AND (a.finished_at IS NULL OR a.finished_at>?)`, segment, attempt, to, from).Scan(&overlap)
	if err != nil {
		return s, err
	}
	if overlap > 0 {
		return s, errors.New("store: historical attempt windows overlap")
	}
	s.Catalog.Rates, err = readMeterRates(ctx, tx, s.Catalog.RevisionID)
	if err != nil {
		return s, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT v.priced_subtotal_usd FROM meter_measurements m JOIN meter_valuations v ON v.measurement_id=m.id AND v.measurement_revision=m.revision WHERE m.attempt_id=? AND v.priced_subtotal_usd IS NOT NULL ORDER BY m.id`, attempt)
	if err != nil {
		return s, err
	}
	for rows.Next() {
		var amount string
		if err = rows.Scan(&amount); err != nil {
			break
		}
		s.ExistingSubtotals = append(s.ExistingSubtotals, amount)
	}
	err = errors.Join(err, rows.Err(), rows.Close())
	if err != nil {
		return s, err
	}
	hash := sha256.New()
	for _, q := range []string{
		`SELECT json_group_array(json_array(a.id,a.started_at,a.finished_at,a.outcome,a.price_revision_id,g.resume_token)) FROM meter_attempts a JOIN runs r ON r.id=a.run_id JOIN segments g ON g.id=r.segment_id WHERE r.segment_id=(SELECT r.segment_id FROM runs r JOIN meter_attempts a ON a.run_id=r.id WHERE a.id=?) ORDER BY a.id`,
		`SELECT json_group_array(json_array(id,parent_id,source_id,source_invocation_id,collection_status,diagnostic_codes_json)) FROM meter_invocations WHERE attempt_id=? ORDER BY id`,
		`SELECT json_group_array(json_array(id,source_id,event_key,payload_sha256,validation_status)) FROM meter_observations WHERE attempt_id=? ORDER BY id`,
		`SELECT json_group_array(json_array(id,revision,input_uncached_tokens,cache_read_tokens,cache_write_tokens,output_tokens,reasoning_tokens)) FROM meter_measurements WHERE attempt_id=? ORDER BY id`,
	} {
		var encoded string
		if err = tx.QueryRowContext(ctx, q, attempt).Scan(&encoded); err != nil {
			return s, err
		}
		_, _ = hash.Write([]byte(encoded))
		_, _ = hash.Write([]byte{0})
	}
	s.Fingerprint = hex.EncodeToString(hash.Sum(nil))
	return s, nil
}

// MeterRecoveryBatch is an append-only candidate set. It cannot replace an
// existing invocation's measurements; replay of its exact responses is a no-op.
type MeterRecoveryBatch struct {
	Scope        MeterRecoveryScope
	Invocations  []MeterInvocation
	Observations []MeterObservation
}

// MeterRecoveryResult describes additions and original-rate valuations from
// either a read-only preview or an atomic historical append.
type MeterRecoveryResult struct {
	Applied           bool
	AddedObservations int
	Valuations        []MeterValuation
}

// RecoverMeterUsage computes preview without writes, or atomically applies the
// same reconciled batch. Reconciliation callbacks must be pure and bounded.
func (db *DB) RecoverMeterUsage(ctx context.Context, b MeterRecoveryBatch, apply bool, reconcile func(MeterObservation, MeterLedgerState) (MeterMutation, error)) (result MeterRecoveryResult, err error) {
	if len(b.Invocations) > 1024 || len(b.Observations) > 100000 || reconcile == nil {
		return result, errors.New("store: invalid historical batch")
	}
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		current, e := readMeterRecoveryScope(ctx, tx, b.Scope.AttemptID)
		if e != nil {
			return e
		}
		if current.Fingerprint != b.Scope.Fingerprint {
			return errors.New("store: historical ledger changed; preview again")
		}
		nodes := map[string]MeterInvocation{}
		for _, v := range b.Invocations {
			if v.AttemptID != current.AttemptID || v.ID == current.RootInvocationID || v.ParentID == "" || meterIdentity(v.ID, v.SourceID, v.SourceInvocationID) != nil {
				return errors.New("store: invalid historical descendant")
			}
			if _, exists := nodes[v.ID]; exists {
				return errors.New("store: duplicate historical invocation")
			}
			nodes[v.ID] = v
		}
		// Refuse any legacy/partial measurement we cannot prove disjoint. A previously
		// applied response set is recognizable by exact event identities and digests.
		for _, o := range b.Observations {
			if o.AttemptID != current.AttemptID || o.Semantics != "incremental/exclusive" || o.ActualModel == nil {
				return errors.New("store: unsupported historical observation")
			}
			if _, ok := nodes[o.InvocationID]; !ok {
				return errors.New("store: historical invocation missing")
			}
			if e = validateMeterObservation(o); e != nil {
				return e
			}
			var foreign int
			e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM meter_observations WHERE source_id=? AND event_key=? AND attempt_id<>?`, o.SourceID, o.EventKey, o.AttemptID).Scan(&foreign)
			if e != nil {
				return e
			}
			if foreign != 0 {
				return errors.New("store: historical response belongs to another attempt")
			}
		}
		for id := range nodes {
			var measured int
			e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM meter_measurements WHERE attempt_id=? AND invocation_id=?`, current.AttemptID, id).Scan(&measured)
			if e != nil {
				return e
			}
			if measured > 0 {
				for _, o := range b.Observations {
					if o.InvocationID != id {
						continue
					}
					var digest string
					e = tx.QueryRowContext(ctx, `SELECT payload_sha256 FROM meter_observations WHERE attempt_id=? AND source_id=? AND event_key=? AND validation_status='accepted'`, o.AttemptID, o.SourceID, o.EventKey).Scan(&digest)
					if e != nil || digest != o.Digest {
						return errors.New("store: existing descendant usage cannot be proved exclusive")
					}
				}
			}
		}
		pending := slices.Clone(b.Invocations)
		known := map[string]bool{current.RootInvocationID: true}
		for len(pending) > 0 {
			var rest []MeterInvocation
			for _, v := range pending {
				if !known[v.ParentID] {
					rest = append(rest, v)
					continue
				}
				known[v.ID] = true
				if apply {
					if e = putMeterInvocation(ctx, tx, v); e != nil {
						return e
					}
				}
			}
			if len(rest) == len(pending) {
				return errors.New("store: unresolved historical ancestry")
			}
			pending = rest
		}
		states := map[string]MeterLedgerState{}
		values := map[string]MeterValuation{}
		seen := map[string]string{}
		for _, o := range b.Observations {
			key := o.InvocationID + "\x00" + o.ModelKey
			state, ok := states[key]
			if !ok {
				state, e = readMeterLedger(ctx, tx, o)
				if e != nil {
					return e
				}
			} else {
				fresh, e := readMeterLedger(ctx, tx, o)
				if e != nil {
					return e
				}
				state.AcceptedDigest = fresh.AcceptedDigest
			}
			identity := o.SourceID + "\x00" + o.EventKey
			if digest, ok := seen[identity]; ok {
				if digest != o.Digest {
					return errors.New("store: conflicting historical response")
				}
				continue
			}
			seen[identity] = o.Digest
			next, e := reconcile(o, state)
			if e != nil {
				return e
			}
			if next.Rejection != "" {
				return fmt.Errorf("store: historical reconciliation refused: %s", next.Rejection)
			}
			if next.Duplicate {
				continue
			}
			if next.Measurement == nil || next.Valuation == nil {
				return errors.New("store: historical observation has no valuation")
			}
			result.AddedObservations++
			state.Measurement = next.Measurement
			state.Checkpoint = next.Checkpoint
			states[key] = state
			values[key] = *next.Valuation
			if apply {
				diagnostics, e := meterDiagnostics(o.Diagnostics)
				if e != nil {
					return e
				}
				if e = applyMeterObservation(ctx, tx, o, diagnostics, func(actual MeterLedgerState) (MeterMutation, error) { return reconcile(o, actual) }); e != nil {
					return e
				}
			}
		}
		keys := make([]string, 0, len(values))
		for key := range values {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, key := range keys {
			result.Valuations = append(result.Valuations, values[key])
		}
		result.Applied = apply
		return nil
	})
	return result, err
}

// MatchMeterRecoveryAttempt verifies selectors without creating any records.
func (db *DB) MatchMeterRecoveryAttempt(ctx context.Context, id string, repo, issue int64) error {
	var count int
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM meter_attempts a JOIN runs r ON r.id=a.run_id JOIN sessions s ON s.id=r.session_id WHERE a.id=? AND s.repo_id=? AND s.issue_number=?`, id, repo, issue).Scan(&count); err != nil {
		return err
	}
	if count != 1 {
		return errors.New("store: attempt does not match repository and task")
	}
	return nil
}
