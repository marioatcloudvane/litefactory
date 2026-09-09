package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"strings"
)

func validateMeterTokens(t MeterTokens) error {
	for _, p := range []*int64{t.InputUncached, t.CacheRead, t.CacheWrite, t.CacheWrite5m, t.CacheWrite1h, t.Output, t.Reasoning} {
		if p != nil && *p < 0 {
			return errors.New("store: negative meter counter")
		}
	}
	if t.Output != nil && t.Reasoning != nil && *t.Reasoning > *t.Output {
		return errors.New("store: reasoning exceeds output")
	}
	if t.CacheWrite != nil {
		remaining := *t.CacheWrite
		for _, p := range []*int64{t.CacheWrite5m, t.CacheWrite1h} {
			if p != nil {
				if *p > remaining {
					return errors.New("store: cache duration exceeds writes")
				}
				remaining -= *p
			}
		}
	}
	return nil
}
func validateMeterObservation(o MeterObservation) error {
	if len(o.Digest) != 64 {
		return errors.New("store: invalid evidence digest")
	}
	for _, r := range o.Digest {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return errors.New("store: invalid evidence digest")
		}
	}
	switch o.Semantics {
	case "incremental/exclusive", "incremental/descendant_inclusive", "cumulative/exclusive", "cumulative/descendant_inclusive":
	default:
		return errors.New("store: invalid meter semantics")
	}
	if len(o.ModelProvenance) > 1024 || len(o.CounterEpoch) > 1024 || len(o.CounterKey) > 8192 || len(o.InvocationID) > 1024 {
		return errors.New("store: excessive meter metadata")
	}
	if o.ActualModel != nil && (*o.ActualModel == "" || len(*o.ActualModel) > 1024 || o.ModelProvenance == "") {
		return errors.New("store: invalid actual model")
	}
	encoded, _ := json.Marshal([]*string{o.ActualModel})
	if o.ModelKey != string(encoded) {
		return errors.New("store: inconsistent meter model key")
	}
	return nil
}

// SeedMeterCheckpoint installs a source-proven lifetime baseline before its
// first observation. It cannot replace a previously accepted counter baseline.
func (db *DB) SeedMeterCheckpoint(ctx context.Context, o MeterObservation, baseline MeterTokens) error {
	if err := validateMeterObservation(o); err != nil {
		return err
	}
	if err := validateMeterTokens(baseline); err != nil {
		return err
	}
	if o.CounterKey == "" || o.CounterEpoch == "" {
		return errors.New("store: missing counter scope")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		if err := meterOpen(ctx, tx, o.AttemptID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO meter_checkpoints(attempt_id,source_id,counter_key,counter_epoch,invocation_id,model_key,baseline_dimensions_json,last_dimensions_json,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, o.AttemptID, o.SourceID, o.CounterKey, o.CounterEpoch, nullString(o.InvocationID), o.ModelKey, meterJSON(baseline), meterJSON(baseline), FormatTime(nowUTC()))
		return err
	})
}
func validateMeterMutation(ctx context.Context, tx *sql.Tx, o MeterObservation, next MeterMutation) error {
	if next.Measurement != nil {
		if o.Semantics != "incremental/exclusive" && o.Semantics != "cumulative/exclusive" {
			return errors.New("store: inclusive measurement refused")
		}
		if err := validateMeterTokens(*next.Measurement); err != nil {
			return err
		}
		var revision int64
		err := tx.QueryRowContext(ctx, `SELECT revision FROM meter_measurements WHERE attempt_id=? AND invocation_id=? AND model_key=?`, o.AttemptID, o.InvocationID, o.ModelKey).Scan(&revision)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if revision == math.MaxInt64 {
			return errors.New("store: meter revision overflow")
		}
	}
	if next.Checkpoint != nil {
		return validateMeterTokens(*next.Checkpoint)
	}
	return nil
}
