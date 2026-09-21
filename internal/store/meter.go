package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
)

// MeterTokens preserves unknown counters separately from measured zero.
type MeterTokens struct {
	InputUncached *int64 `json:"input_uncached"`
	CacheRead     *int64 `json:"cache_read"`
	CacheWrite    *int64 `json:"cache_write"`
	CacheWrite5m  *int64 `json:"cache_write_5m"`
	CacheWrite1h  *int64 `json:"cache_write_1h"`
	Output        *int64 `json:"output"`
	Reasoning     *int64 `json:"reasoning"`
}

// MeterAttempt identifies one execution, including an unsuccessful launch.
type MeterAttempt struct {
	ProviderReportedZeroCostRefusal bool
	Outcome                         string
	ID                              string
	RunID                           int64
	Ordinal                         int
	CodingAgent                     string
	PriceRevisionID                 int64
}

// MeterInvocation is a node in an attempt-local ancestry tree.
type MeterInvocation struct {
	AttemptID, ID, ParentID, SourceID, SourceInvocationID, Status string
	Diagnostics                                                   []string
}

// MeterObservation is canonical evidence; provider payloads never enter storage.
type MeterObservation struct {
	AttemptID, SourceID, EventKey, InvocationID, Semantics, CounterEpoch, CounterKey, ModelKey, ModelProvenance, Digest, Status string
	ActualModel                                                                                                                 *string
	Tokens                                                                                                                      MeterTokens
	Diagnostics                                                                                                                 []string
}

// MeterLedgerState is read inside the same transaction as its replacement.
type MeterLedgerState struct {
	Catalog        MeterCatalog
	AcceptedDigest string
	Checkpoint     *MeterTokens
	Measurement    *MeterTokens
}

// MeterMutation is calculated without I/O by the consumer. Rejected evidence is
// retained diagnostically without advancing a counter or measurement.
type MeterMutation struct {
	Valuation   *MeterValuation
	Duplicate   bool
	Checkpoint  *MeterTokens
	Measurement *MeterTokens
	Rejection   string
}

var meterCode = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func meterDiagnostics(codes []string) (string, error) {
	if len(codes) > 64 {
		return "", errors.New("store: too many metering diagnostics")
	}
	codes = slices.Clone(codes)
	slices.Sort(codes)
	codes = slices.Compact(codes)
	for _, code := range codes {
		if !meterCode.MatchString(code) {
			return "", errors.New("store: invalid metering diagnostic")
		}
	}
	data, err := json.Marshal(codes)
	return string(data), err
}
func meterStatus(s string) string {
	if s == "complete" || s == "partial" {
		return s
	}
	return "unknown"
}
func meterIdentity(values ...string) error {
	for _, v := range values {
		if v == "" || len(v) > 1024 {
			return errors.New("store: missing or excessive meter identity")
		}
	}
	return nil
}
func meterJSON(t MeterTokens) string { b, _ := json.Marshal(t); return string(b) }

// StartMeterAttempt pins the entire current catalog atomically before execution.
func (db *DB) StartMeterAttempt(ctx context.Context, runID int64, codingAgent string) (MeterAttempt, error) {
	return db.startMeterAttempt(ctx, runID, codingAgent, nil)
}

// MeterGraphLink binds an actual provider invocation to a reserved graph attempt.
// Multiple invocations (for example a refused resume and cold fallback) retain
// independent meter identities against this same logical graph attempt.
type MeterGraphLink struct {
	InstanceID, AttemptID int64
	Node                  string
}

// StartGraphMeterAttempt validates the authoritative attempt link and pins prices
// atomically before launching the provider process.
func (db *DB) StartGraphMeterAttempt(ctx context.Context, runID int64, codingAgent string, link MeterGraphLink) (MeterAttempt, error) {
	if link.InstanceID < 1 || link.AttemptID < 1 || link.Node == "" {
		return MeterAttempt{}, errors.New("store: invalid graph meter link")
	}
	return db.startMeterAttempt(ctx, runID, codingAgent, &link)
}

func (db *DB) startMeterAttempt(ctx context.Context, runID int64, codingAgent string, link *MeterGraphLink) (MeterAttempt, error) {
	if runID <= 0 || meterIdentity(codingAgent) != nil {
		return MeterAttempt{}, errors.New("store: invalid meter attempt")
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return MeterAttempt{}, err
	}
	a := MeterAttempt{ID: hex.EncodeToString(random[:]), RunID: runID, CodingAgent: codingAgent}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if link == nil {
			return tx.QueryRowContext(ctx, `INSERT INTO meter_attempts(id,run_id,ordinal,coding_agent,started_at,outcome,collection_status,diagnostic_codes_json,price_revision_id)
 VALUES(?,?,(SELECT COALESCE(MAX(ordinal),0)+1 FROM meter_attempts WHERE run_id=?),?,?,'running','unknown','[]',(SELECT MAX(id) FROM meter_price_revisions)) RETURNING ordinal,price_revision_id`, a.ID, runID, runID, codingAgent, FormatTime(nowUTC())).Scan(&a.Ordinal, &a.PriceRevisionID)
		}
		var graphAttempt *int64
		if link != nil {
			var matches bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM graph_node_attempts n JOIN runs r ON r.id=n.run_id JOIN graph_instances i ON i.id=n.instance_id JOIN graph_definitions d ON d.id=i.definition_id WHERE n.id=? AND n.instance_id=? AND n.node=? AND n.run_id=? AND n.status='running' AND r.status='running' AND r.mode=d.mode AND r.agent=?)`, link.AttemptID, link.InstanceID, link.Node, runID, codingAgent).Scan(&matches); err != nil {
				return err
			}
			if !matches {
				return errors.New("store: graph meter attempt binding mismatch")
			}
			graphAttempt = &link.AttemptID
		}
		return tx.QueryRowContext(ctx, `INSERT INTO meter_attempts(id,run_id,ordinal,coding_agent,started_at,outcome,collection_status,diagnostic_codes_json,price_revision_id,graph_node_attempt_id)
 VALUES(?,?,(SELECT COALESCE(MAX(ordinal),0)+1 FROM meter_attempts WHERE run_id=?),?,?,'running','unknown','[]',(SELECT MAX(id) FROM meter_price_revisions),?) RETURNING ordinal,price_revision_id`, a.ID, runID, runID, codingAgent, FormatTime(nowUTC()), graphAttempt).Scan(&a.Ordinal, &a.PriceRevisionID)
	})
	return a, err
}

// PutMeterInvocation validates ancestry and refuses reassignment after discovery.
func (db *DB) PutMeterInvocation(ctx context.Context, v MeterInvocation) error {
	return db.PutMeterInvocations(ctx, []MeterInvocation{v})
}

// PutMeterInvocations registers a bounded batch in parent-first order in one
// transaction. A provider may report children before parents within the batch;
// missing parents and cycles never create extra roots.
func (db *DB) PutMeterInvocations(ctx context.Context, values []MeterInvocation) error {
	if len(values) == 0 || len(values) > 1024 {
		return errors.New("store: invocation batch must contain 1..1024 nodes")
	}
	pending := slices.Clone(values)
	for _, v := range pending {
		if err := meterIdentity(v.AttemptID, v.ID, v.SourceID, v.SourceInvocationID); err != nil {
			return err
		}
		if v.AttemptID != pending[0].AttemptID {
			return errors.New("store: invocation batch crosses attempts")
		}
		if _, err := meterDiagnostics(v.Diagnostics); err != nil {
			return err
		}
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		if err := meterOpen(ctx, tx, pending[0].AttemptID); err != nil {
			return err
		}
		for len(pending) > 0 {
			remaining := make([]MeterInvocation, 0, len(pending))
			progress := false
			for _, v := range pending {
				if v.ParentID != "" {
					var exists int
					if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM meter_invocations WHERE attempt_id=? AND id=?`, v.AttemptID, v.ParentID).Scan(&exists); err != nil {
						return err
					}
					if exists == 0 {
						remaining = append(remaining, v)
						continue
					}
				}
				if err := putMeterInvocation(ctx, tx, v); err != nil {
					return err
				}
				progress = true
			}
			if !progress {
				return errors.New("store: missing or cyclic invocation parents")
			}
			pending = remaining
		}
		return nil
	})
}
func putMeterInvocation(ctx context.Context, tx *sql.Tx, v MeterInvocation) error {
	var diagnostics string
	var err error
	var parent sql.NullString
	var source, sourceInvocation, oldDiagnostics, oldStatus string
	err = tx.QueryRowContext(ctx, `SELECT parent_id,source_id,source_invocation_id,diagnostic_codes_json,collection_status FROM meter_invocations WHERE attempt_id=? AND id=?`, v.AttemptID, v.ID).Scan(&parent, &source, &sourceInvocation, &oldDiagnostics, &oldStatus)
	if err == nil && (parent.String != v.ParentID || source != v.SourceID || sourceInvocation != v.SourceInvocationID) {
		return errors.New("store: invocation identity or ancestry changed")
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	diagnostics, err = mergeMeterDiagnostics(oldDiagnostics, v.Diagnostics)
	if err != nil {
		return err
	}
	if oldStatus == "partial" || oldDiagnostics != "" && oldDiagnostics != "[]" && oldDiagnostics != "null" {
		v.Status = "partial"
	}
	ancestor := v.ParentID
	for depth := 0; ancestor != ""; depth++ {
		if ancestor == v.ID || depth >= 128 {
			return errors.New("store: cyclic or excessive invocation ancestry")
		}
		if err := tx.QueryRowContext(ctx, `SELECT parent_id FROM meter_invocations WHERE attempt_id=? AND id=?`, v.AttemptID, ancestor).Scan(&parent); err != nil {
			return err
		}
		ancestor = parent.String
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO meter_invocations(attempt_id,id,parent_id,source_id,source_invocation_id,lifecycle,collection_status,diagnostic_codes_json) VALUES(?,?,?,?,?,'observed',?,?) ON CONFLICT(attempt_id,id) DO UPDATE SET collection_status=excluded.collection_status,diagnostic_codes_json=excluded.diagnostic_codes_json`, v.AttemptID, v.ID, nullString(v.ParentID), v.SourceID, v.SourceInvocationID, meterStatus(v.Status), diagnostics)
	return err
}

func meterOpen(ctx context.Context, tx *sql.Tx, id string) error {
	var finished sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT finished_at FROM meter_attempts WHERE id=?`, id).Scan(&finished); err != nil {
		return err
	}
	if finished.Valid {
		return errors.New("store: finalized meter attempt")
	}
	return nil
}

// ApplyMeterObservation reconciles evidence under a single transaction. The
// callback must be pure: no database access, network, subprocess or writer I/O.
func (db *DB) ApplyMeterObservation(ctx context.Context, o MeterObservation, reconcile func(MeterLedgerState) (MeterMutation, error)) error {
	if err := meterIdentity(o.AttemptID, o.SourceID, o.EventKey, o.ModelKey, o.Digest); err != nil {
		return err
	}
	if reconcile == nil {
		return errors.New("store: nil meter reconciliation")
	}
	if err := validateMeterObservation(o); err != nil {
		return err
	}
	diagnostics, err := meterDiagnostics(o.Diagnostics)
	if err != nil {
		return err
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		if err := meterOpen(ctx, tx, o.AttemptID); err != nil {
			return err
		}
		return applyMeterObservation(ctx, tx, o, diagnostics, reconcile)
	})
}

// applyMeterObservation is shared by live collection and atomic historical
// append. Callers must enforce their respective lifecycle fence first.
func applyMeterObservation(ctx context.Context, tx *sql.Tx, o MeterObservation, diagnostics string, reconcile func(MeterLedgerState) (MeterMutation, error)) error {
	state, err := readMeterLedger(ctx, tx, o)
	if err != nil {
		return err
	}
	next, err := reconcile(state)
	if err != nil {
		return err
	}
	if err := validateMeterValuation(ctx, tx, o, state, next); err != nil {
		return err
	}
	if err := validateMeterMutation(ctx, tx, o, next); err != nil {
		return err
	}
	if next.Duplicate {
		if state.AcceptedDigest == "" || state.AcceptedDigest != o.Digest {
			return errors.New("store: invalid replay classification")
		}
		return nil
	}
	if state.AcceptedDigest != "" {
		return meterAttemptDiagnostic(ctx, tx, o.AttemptID, "conflicting_event")
	}
	validation := "accepted"
	if next.Rejection != "" {
		validation = "rejected"
		diagnostics, err = meterDiagnostics([]string{next.Rejection})
		if err != nil {
			return err
		}
	}
	var observationID int64
	err = tx.QueryRowContext(ctx, `INSERT INTO meter_observations(attempt_id,source_id,event_key,invocation_id,recorded_at,semantics,counter_epoch,actual_model,model_provenance,dimensions_json,payload_sha256,validation_status,diagnostic_codes_json) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) RETURNING id`, o.AttemptID, o.SourceID, o.EventKey, nullString(o.InvocationID), FormatTime(nowUTC()), o.Semantics, o.CounterEpoch, o.ActualModel, o.ModelProvenance, meterJSON(o.Tokens), o.Digest, validation, diagnostics).Scan(&observationID)
	if err != nil {
		return err
	}
	if next.Rejection != "" {
		return meterAttemptDiagnostic(ctx, tx, o.AttemptID, next.Rejection)
	}
	if next.Checkpoint != nil {
		_, err = tx.ExecContext(ctx, `INSERT INTO meter_checkpoints(attempt_id,source_id,counter_key,counter_epoch,invocation_id,model_key,baseline_dimensions_json,last_dimensions_json,last_observation_id,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(attempt_id,source_id,counter_key,counter_epoch) DO UPDATE SET last_dimensions_json=excluded.last_dimensions_json,last_observation_id=excluded.last_observation_id,updated_at=excluded.updated_at`, o.AttemptID, o.SourceID, o.CounterKey, o.CounterEpoch, nullString(o.InvocationID), o.ModelKey, meterJSON(o.Tokens), meterJSON(*next.Checkpoint), observationID, FormatTime(nowUTC()))
		if err != nil {
			return err
		}
	}
	if next.Measurement == nil {
		return nil
	}
	var previousProvenance, previousCodes, previousStatus string
	err = tx.QueryRowContext(ctx, `SELECT model_provenance,diagnostic_codes_json,collection_status FROM meter_measurements WHERE attempt_id=? AND invocation_id=? AND model_key=?`, o.AttemptID, o.InvocationID, o.ModelKey).Scan(&previousProvenance, &previousCodes, &previousStatus)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	provenance, err := mergeMeterProvenance(previousProvenance, o.ModelProvenance)
	if err != nil {
		return err
	}
	diagnostics, err = mergeMeterDiagnostics(previousCodes, o.Diagnostics)
	if err != nil {
		return err
	}
	if previousStatus == "partial" || previousCodes != "" && previousCodes != "[]" && previousCodes != "null" {
		o.Status = "partial"
	}
	t := *next.Measurement
	_, err = tx.ExecContext(ctx, `INSERT INTO meter_measurements(attempt_id,invocation_id,model_key,actual_model,model_provenance,input_uncached_tokens,cache_read_tokens,cache_write_tokens,cache_write_5m_tokens,cache_write_1h_tokens,output_tokens,reasoning_tokens,collection_status,diagnostic_codes_json,last_observation_id,revision,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,1,?) ON CONFLICT(attempt_id,invocation_id,model_key) DO UPDATE SET model_provenance=excluded.model_provenance,input_uncached_tokens=excluded.input_uncached_tokens,cache_read_tokens=excluded.cache_read_tokens,cache_write_tokens=excluded.cache_write_tokens,cache_write_5m_tokens=excluded.cache_write_5m_tokens,cache_write_1h_tokens=excluded.cache_write_1h_tokens,output_tokens=excluded.output_tokens,reasoning_tokens=excluded.reasoning_tokens,collection_status=excluded.collection_status,diagnostic_codes_json=excluded.diagnostic_codes_json,last_observation_id=excluded.last_observation_id,revision=meter_measurements.revision+1,updated_at=excluded.updated_at`, o.AttemptID, o.InvocationID, o.ModelKey, o.ActualModel, provenance, t.InputUncached, t.CacheRead, t.CacheWrite, t.CacheWrite5m, t.CacheWrite1h, t.Output, t.Reasoning, meterStatus(o.Status), diagnostics, observationID, FormatTime(nowUTC()))
	if err != nil {
		return err
	}
	if next.Valuation != nil {
		var measurementID, revision int64
		if err := tx.QueryRowContext(ctx, `SELECT id,revision FROM meter_measurements WHERE attempt_id=? AND invocation_id=? AND model_key=?`, o.AttemptID, o.InvocationID, o.ModelKey).Scan(&measurementID, &revision); err != nil {
			return err
		}
		return writeMeterValuation(ctx, tx, measurementID, revision, state.Catalog.RevisionID, *next.Valuation)
	}
	return nil
}

func readMeterLedger(ctx context.Context, tx *sql.Tx, o MeterObservation) (MeterLedgerState, error) {
	var s MeterLedgerState
	if err := tx.QueryRowContext(ctx, `SELECT price_revision_id FROM meter_attempts WHERE id=?`, o.AttemptID).Scan(&s.Catalog.RevisionID); err != nil {
		return s, err
	}
	var rateErr error
	s.Catalog.Rates, rateErr = readMeterRates(ctx, tx, s.Catalog.RevisionID)
	if rateErr != nil {
		return s, rateErr
	}
	err := tx.QueryRowContext(ctx, `SELECT payload_sha256 FROM meter_observations WHERE attempt_id=? AND source_id=? AND event_key=?`, o.AttemptID, o.SourceID, o.EventKey).Scan(&s.AcceptedDigest)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return s, err
	}
	var encoded, invocation, model string
	err = tx.QueryRowContext(ctx, `SELECT last_dimensions_json,COALESCE(invocation_id,''),model_key FROM meter_checkpoints WHERE attempt_id=? AND source_id=? AND counter_key=? AND counter_epoch=?`, o.AttemptID, o.SourceID, o.CounterKey, o.CounterEpoch).Scan(&encoded, &invocation, &model)
	if err == nil {
		if invocation != o.InvocationID || model != o.ModelKey {
			return s, errors.New("store: checkpoint scope mismatch")
		}
		s.Checkpoint = &MeterTokens{}
		if err = json.Unmarshal([]byte(encoded), s.Checkpoint); err != nil {
			return s, err
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return s, err
	}
	var t MeterTokens
	err = tx.QueryRowContext(ctx, `SELECT input_uncached_tokens,cache_read_tokens,cache_write_tokens,cache_write_5m_tokens,cache_write_1h_tokens,output_tokens,reasoning_tokens FROM meter_measurements WHERE attempt_id=? AND invocation_id=? AND model_key=?`, o.AttemptID, o.InvocationID, o.ModelKey).Scan(&t.InputUncached, &t.CacheRead, &t.CacheWrite, &t.CacheWrite5m, &t.CacheWrite1h, &t.Output, &t.Reasoning)
	if err == nil {
		s.Measurement = &t
	} else if !errors.Is(err, sql.ErrNoRows) {
		return s, err
	}
	return s, nil
}

// GetMeterAttempt reads an attempt outcome without reconstructing run logs.
func (db *DB) GetMeterAttempt(ctx context.Context, id string) (MeterAttempt, error) {
	var a MeterAttempt
	err := db.sql.QueryRowContext(ctx, `SELECT id,run_id,ordinal,coding_agent,price_revision_id,outcome,provider_reported_zero_cost_refusal FROM meter_attempts WHERE id=?`, id).Scan(&a.ID, &a.RunID, &a.Ordinal, &a.CodingAgent, &a.PriceRevisionID, &a.Outcome, &a.ProviderReportedZeroCostRefusal)
	return a, err
}
