package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/codex"
	"github.com/cloudvane/litefactory/internal/meter"
)

// The source counters, identities and windows are authentic extracted facts;
// JSONL envelopes and database allocation are reconstructed, not a runtime capture.
type recoveryFacts struct {
	Rollouts []struct {
		Metadata struct {
			ID        string `json:"id"`
			Parent    string `json:"parent_thread_id"`
			Version   string `json:"cli_version"`
			Timestamp string `json:"timestamp"`
		} `json:"metadata"`
		Responses []struct {
			Timestamp string          `json:"timestamp"`
			ID        string          `json:"response_id"`
			Model     string          `json:"model"`
			Usage     json.RawMessage `json:"usage"`
		} `json:"responses"`
	} `json:"rollouts"`
	Attempts []struct {
		Start  string `json:"started_at"`
		End    string `json:"finished_at"`
		Format string `json:"source_format"`
	} `json:"attempts"`
	Measurements []struct {
		Input     *int64 `json:"input_uncached_tokens"`
		Cache     *int64 `json:"cache_read_tokens"`
		Write     *int64 `json:"cache_write_tokens"`
		Output    *int64 `json:"output_tokens"`
		Reasoning *int64 `json:"reasoning_tokens"`
	} `json:"measurements"`
}

func recoverySource(t *testing.T) (string, recoveryFacts) {
	t.Helper()
	var facts recoveryFacts
	data, err := os.ReadFile("testdata/meter_recovery_history.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &facts); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	dir := filepath.Join(home, ".codex", "sessions", "2026", "09", "09")
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	write := func(id string, records []any) {
		t.Helper()
		var b strings.Builder
		for _, record := range records {
			raw, e := json.Marshal(record)
			if e != nil {
				t.Fatal(e)
			}
			b.Write(raw)
			b.WriteByte('\n')
		}
		if e := os.WriteFile(filepath.Join(dir, "rollout-"+id+".jsonl"), []byte(b.String()), 0600); e != nil {
			t.Fatal(e)
		}
	}
	root := facts.Rollouts[0].Metadata.Parent
	write(root, []any{map[string]any{"type": "session_meta", "timestamp": facts.Attempts[0].Start, "payload": map[string]any{"id": root, "cli_version": "0.153.4"}}})
	for _, child := range facts.Rollouts {
		records := []any{map[string]any{"type": "session_meta", "timestamp": child.Metadata.Timestamp, "payload": map[string]any{"id": child.Metadata.ID, "parent_thread_id": child.Metadata.Parent, "cli_version": child.Metadata.Version}}}
		for i, response := range child.Responses {
			turn := fmt.Sprintf("synthetic-turn-%d", i)
			records = append(records, map[string]any{"type": "turn_context", "timestamp": response.Timestamp, "payload": map[string]any{"model": response.Model, "turn_id": turn}}, map[string]any{"type": "token_usage_record", "timestamp": response.Timestamp, "payload": map[string]any{"thread_id": child.Metadata.ID, "turn_id": turn, "response_id": response.ID, "usage": response.Usage}})
		}
		write(child.Metadata.ID, records)
	}
	return home, facts
}

func recoveryObservationForTest(o agent.UsageObservation) MeterObservation {
	o.Mode, o.Scope = agent.CounterIncremental, agent.UsageExclusive
	o.Coverage = agent.UsageCoverage{Status: agent.CollectionPartial}
	raw, _ := json.Marshal(o)
	hash := sha256.Sum256(raw)
	key, _ := json.Marshal([]*string{o.ActualModel})
	return MeterObservation{AttemptID: o.AttemptID, SourceID: o.SourceID, EventKey: o.EventKey, InvocationID: o.InvocationID, Semantics: "incremental/exclusive", CounterKey: meter.CounterIdentity(o), ModelKey: string(key), ModelProvenance: o.ModelProvenance, Digest: hex.EncodeToString(hash[:]), Status: "partial", ActualModel: o.ActualModel, Tokens: MeterTokens(o.Tokens), Diagnostics: []string{"historical_reconstruction"}}
}

// Exercise the real accounting engine through the store's pure callback contract.
func recoveryReconcileForTest(o MeterObservation, s MeterLedgerState) (MeterMutation, error) {
	observation := agent.UsageObservation{AttemptID: o.AttemptID, SourceID: o.SourceID, EventKey: o.EventKey, InvocationID: o.InvocationID, ActualModel: o.ActualModel, ModelProvenance: o.ModelProvenance, Mode: agent.CounterIncremental, Scope: agent.UsageExclusive, Tokens: agent.TokenUsage(o.Tokens), Coverage: agent.UsageCoverage{Status: agent.CollectionPartial}}
	transition, err := meter.Reconcile(observation, nil, s.AcceptedDigest)
	if err != nil {
		return MeterMutation{}, err
	}
	if transition.Duplicate {
		return MeterMutation{Duplicate: true}, nil
	}
	tokens := transition.Delta
	if s.Measurement != nil {
		tokens, err = meter.Add(agent.TokenUsage(*s.Measurement), tokens)
		if err != nil {
			return MeterMutation{}, err
		}
	}
	var rates []meter.PriceRate
	for _, rate := range s.Catalog.Rates {
		if rate.CodingAgent == "codex" && o.ActualModel != nil && rate.Model == *o.ActualModel {
			rates = append(rates, meter.PriceRate{Dimension: rate.Dimension, USDPerMillion: rate.USDPerMillion})
		}
	}
	valued, err := meter.Value(tokens, rates)
	if err != nil {
		return MeterMutation{}, err
	}
	v := MeterValuation{EstimatedUSD: valued.EstimatedUSD, PricedSubtotalUSD: valued.PricedSubtotalUSD, Status: valued.Status}
	for _, d := range valued.Dimensions {
		v.Dimensions = append(v.Dimensions, MeterValuationDimension{Dimension: d.Dimension, Quantity: d.Quantity, USDPerMillion: d.USDPerMillion, AmountUSD: d.AmountUSD, Status: d.Status})
	}
	m := MeterTokens(tokens)
	return MeterMutation{Measurement: &m, Valuation: &v}, nil
}

func recoveryLedger(t *testing.T) (*DB, string, []MeterAttempt, MeterRecoveryBatch, codex.HistoricalUsage) {
	t.Helper()
	ctx := t.Context()
	home, facts := recoverySource(t)
	db := newTestDB(t)
	rates := []MeterPriceRate{{CodingAgent: "codex", Model: "gpt-6-astra", Dimension: "input_uncached", USDPerMillion: "10"}, {CodingAgent: "codex", Model: "gpt-6-astra", Dimension: "cache_read", USDPerMillion: "1"}, {CodingAgent: "codex", Model: "gpt-6-astra", Dimension: "output", USDPerMillion: "50"}}
	if _, err := db.SetMeterPrices(ctx, "codex", "gpt-6-astra", rates); err != nil {
		t.Fatal(err)
	}
	session := testSession(t, db, 195)
	root := facts.Rollouts[0].Metadata.Parent
	var attempts []MeterAttempt
	for i, fact := range facts.Attempts {
		run := testRun(t, db, session.ID, int64(i+500))
		a, err := db.StartMeterAttempt(ctx, run.ID, "codex")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.sql.ExecContext(ctx, `UPDATE segments SET resume_token=? WHERE id=?`, root, run.SegmentID); err != nil {
			t.Fatal(err)
		}
		if _, err = db.sql.ExecContext(ctx, `UPDATE runs SET mode='implement' WHERE id=?`, run.ID); err != nil {
			t.Fatal(err)
		}
		inv := root
		if i == 0 {
			inv = "historic:273"
		}
		if err = db.PutMeterInvocation(ctx, MeterInvocation{AttemptID: a.ID, ID: inv, SourceID: root, SourceInvocationID: inv, Status: "partial"}); err != nil {
			t.Fatal(err)
		}
		m := facts.Measurements[i]
		model := "gpt-6-astra"
		o := recoveryObservationForTest(agent.UsageObservation{AttemptID: a.ID, SourceID: root, InvocationID: inv, EventKey: fmt.Sprintf("original-%d", i), ActualModel: &model, ModelProvenance: "historical_log", Tokens: agent.TokenUsage{InputUncached: m.Input, CacheRead: m.Cache, CacheWrite: m.Write, Output: m.Output, Reasoning: m.Reasoning}})
		if err = db.ApplyMeterObservation(ctx, o, func(s MeterLedgerState) (MeterMutation, error) { return recoveryReconcileForTest(o, s) }); err != nil {
			t.Fatal(err)
		}
		if err = db.FinishMeterAttempt(ctx, a.ID, "success", "partial", "0.153.4", fact.Format, []string{"historical_reconstruction"}); err != nil {
			t.Fatal(err)
		}
		if _, err = db.sql.ExecContext(ctx, `UPDATE meter_attempts SET started_at=?,finished_at=? WHERE id=?`, fact.Start, fact.End, a.ID); err != nil {
			t.Fatal(err)
		}
		attempts = append(attempts, a)
	}
	batch, evidence := recoveryBatch(t, db, home, attempts[0].ID)
	return db, home, attempts, batch, evidence
}
func recoveryBatch(t *testing.T, db *DB, home, id string) (MeterRecoveryBatch, codex.HistoricalUsage) {
	t.Helper()
	ctx := t.Context()
	scope, err := db.MeterRecoveryScope(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := codex.CollectHistoricalUsage(ctx, home, id, scope.RootID, codex.HistoricalWindow{From: scope.From, To: scope.To})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range evidence.Diagnostics {
		if d != "capability_evidence_missing" && d != "inherited_usage_excluded" {
			t.Fatalf("unexpected source diagnostic %s", d)
		}
	}
	b := MeterRecoveryBatch{Scope: scope}
	for _, v := range evidence.Invocations {
		if v.ID == scope.RootID {
			continue
		}
		parent := v.ParentID
		if parent == scope.RootID {
			parent = scope.RootInvocationID
		}
		b.Invocations = append(b.Invocations, MeterInvocation{AttemptID: id, ID: v.ID, ParentID: parent, SourceID: v.SourceID, SourceInvocationID: v.SourceInvocationID, Status: "partial"})
	}
	for _, o := range evidence.Observations {
		b.Observations = append(b.Observations, recoveryObservationForTest(o))
	}
	return b, evidence
}
func recoveryDump(t *testing.T, db *DB) string {
	t.Helper()
	var out strings.Builder
	// Entire logical meter ledger plus schema: catches writes to checkpoint,
	// coverage, valuation or schema even when the presented subtotal is unchanged.
	for _, table := range []string{"sqlite_schema", "schema_migrations", "meter_attempts", "meter_invocations", "meter_observations", "meter_measurements", "meter_checkpoints", "meter_valuations", "meter_valuation_dimensions", "meter_price_revisions", "meter_price_rates"} {
		rows, err := db.sql.QueryContext(t.Context(), `SELECT * FROM `+table)
		if err != nil {
			t.Fatal(err)
		}
		cols, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			v := make([]any, len(cols))
			dest := make([]any, len(cols))
			for i := range v {
				dest[i] = &v[i]
			}
			if err = rows.Scan(dest...); err != nil {
				t.Fatal(err)
			}
			raw, e := json.Marshal(v)
			if e != nil {
				t.Fatal(e)
			}
			out.Write(raw)
		}
		if err = errors.Join(rows.Err(), rows.Close()); err != nil {
			t.Fatal(err)
		}
	}
	return out.String()
}
func recoverySubtotal(t *testing.T, db *DB, id string) string {
	t.Helper()
	s, err := db.MeterRecoveryScope(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	sum := new(big.Rat)
	for _, v := range s.ExistingSubtotals {
		n, ok := new(big.Rat).SetString(v)
		if !ok {
			t.Fatal(v)
		}
		sum.Add(sum, n)
	}
	return sum.FloatString(6)
}
func recoveryOriginalRows(t *testing.T, db *DB) string {
	t.Helper()
	var out strings.Builder
	for _, query := range []string{
		`SELECT id,run_id,ordinal,coding_agent,started_at,finished_at,outcome,price_revision_id,source_format FROM meter_attempts ORDER BY id`,
		`SELECT m.* FROM meter_measurements m JOIN meter_invocations i ON i.attempt_id=m.attempt_id AND i.id=m.invocation_id WHERE i.parent_id IS NULL ORDER BY m.id`,
		`SELECT v.* FROM meter_valuations v JOIN meter_measurements m ON m.id=v.measurement_id JOIN meter_invocations i ON i.attempt_id=m.attempt_id AND i.id=m.invocation_id WHERE i.parent_id IS NULL ORDER BY v.measurement_id,v.measurement_revision`,
	} {
		rows, err := db.sql.QueryContext(t.Context(), query)
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			values := make([]any, len(columns))
			dest := make([]any, len(values))
			for i := range values {
				dest[i] = &values[i]
			}
			if err = rows.Scan(dest...); err != nil {
				t.Fatal(err)
			}
			raw, e := json.Marshal(values)
			if e != nil {
				t.Fatal(e)
			}
			out.Write(raw)
		}
		if err = errors.Join(rows.Err(), rows.Close()); err != nil {
			t.Fatal(err)
		}
	}
	return out.String()
}

func TestMeterRecoveryHistoricalPreviewApplyReplay(t *testing.T) {
	db, home, attempts, batch, _ := recoveryLedger(t)
	ctx := t.Context()
	before := recoveryDump(t, db)
	original := recoveryOriginalRows(t, db)
	reader, err := OpenMeterReader(ctx, db.path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := reader.Close(); err != nil {
			t.Error(err)
		}
	}()
	result, err := reader.RecoverMeterUsage(ctx, batch, false, recoveryReconcileForTest)
	if err != nil {
		t.Fatal(err)
	}
	if result.Applied || result.AddedObservations != len(batch.Observations) || len(batch.Invocations) != 3 {
		t.Fatalf("preview %+v children=%d", result, len(batch.Invocations))
	}
	sum := new(big.Rat)
	for _, v := range result.Valuations {
		n, ok := new(big.Rat).SetString(v.PricedSubtotalUSD)
		if !ok {
			t.Fatal(v)
		}
		sum.Add(sum, n)
	}
	if got := sum.FloatString(6); got != "6.754276" {
		t.Fatalf("additional %s", got)
	}
	if after := recoveryDump(t, db); after != before {
		t.Fatal("preview mutated ledger or schema")
	}
	// New catalog must not revalue the historical attempt.
	if _, err = db.SetMeterPrices(ctx, "codex", "gpt-6-astra", []MeterPriceRate{{CodingAgent: "codex", Model: "gpt-6-astra", Dimension: "input_uncached", USDPerMillion: "999"}, {CodingAgent: "codex", Model: "gpt-6-astra", Dimension: "output", USDPerMillion: "999"}}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.RecoverMeterUsage(ctx, batch, true, recoveryReconcileForTest); err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"11.903026", "1.746696", "0.326984", "0.558352"} {
		if got := recoverySubtotal(t, db, attempts[i].ID); got != want {
			t.Fatalf("run %d subtotal %s want %s", i+5, got, want)
		}
	}
	var count int
	if err = db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM meter_invocations WHERE attempt_id=?`, attempts[0].ID).Scan(&count); err != nil || count != 4 {
		t.Fatalf("invocations %d: %v", count, err)
	}
	if recoveryOriginalRows(t, db) != original {
		t.Fatal("recovery altered original attempts, root measurements or valuations")
	}
	var attributed int
	if err = db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM meter_invocations i JOIN meter_measurements m ON m.attempt_id=i.attempt_id AND m.invocation_id=i.id WHERE i.attempt_id=? AND i.parent_id=? AND m.actual_model='gpt-6-astra'`, attempts[0].ID, batch.Scope.RootInvocationID).Scan(&attributed); err != nil || attributed != 3 {
		t.Fatalf("correctly attributed children %d: %v", attributed, err)
	}
	fresh, _ := recoveryBatch(t, db, home, attempts[0].ID)
	after := recoveryDump(t, db)
	reopened, err := Open(ctx, db.path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := reopened.Close(); err != nil {
			t.Error(err)
		}
	}()
	result, err = reopened.RecoverMeterUsage(ctx, fresh, true, recoveryReconcileForTest)
	if err != nil || result.AddedObservations != 0 {
		t.Fatalf("replay %+v %v", result, err)
	}
	if recoveryDump(t, db) != after {
		t.Fatal("replay mutated ledger")
	}
}
func TestMeterRecoveryRollbackAndConflicts(t *testing.T) {
	for _, kind := range []string{"interruption", "stale_ledger", "overlap", "active", "ambiguous_existing", "foreign_response"} {
		t.Run(kind, func(t *testing.T) {
			db, home, attempts, batch, _ := recoveryLedger(t)
			ctx := t.Context()
			callback := recoveryReconcileForTest
			switch kind {
			case "interruption":
				n := 0
				callback = func(o MeterObservation, s MeterLedgerState) (MeterMutation, error) {
					n++
					if n == 3 {
						return MeterMutation{}, context.Canceled
					}
					return recoveryReconcileForTest(o, s)
				}
			case "stale_ledger":
				concurrent := batch
				concurrent.Observations = batch.Observations[:1]
				if _, err := db.RecoverMeterUsage(ctx, concurrent, true, recoveryReconcileForTest); err != nil {
					t.Fatal(err)
				}

			case "overlap":
				if _, err := db.sql.ExecContext(ctx, `UPDATE meter_attempts SET started_at=? WHERE id=?`, batch.Scope.From.Format(time.RFC3339Nano), attempts[1].ID); err != nil {
					t.Fatal(err)
				}
			case "active":
				if _, err := db.sql.ExecContext(ctx, `UPDATE meter_attempts SET finished_at=NULL,outcome='running' WHERE id=?`, attempts[0].ID); err != nil {
					t.Fatal(err)
				}
			case "ambiguous_existing", "foreign_response":
				v := batch.Invocations[0]
				o := batch.Observations[0]
				for _, candidate := range batch.Observations {
					if candidate.InvocationID == v.ID {
						o = candidate
						break
					}
				}
				id := attempts[0].ID
				if kind == "foreign_response" {
					id = attempts[1].ID
					v.ParentID = batch.Scope.RootID
				}
				v.AttemptID = id
				o.AttemptID = id
				if _, err := db.sql.ExecContext(ctx, `UPDATE meter_attempts SET finished_at=NULL,outcome='running' WHERE id=?`, id); err != nil {
					t.Fatal(err)
				}
				if err := db.PutMeterInvocation(ctx, v); err != nil {
					t.Fatal(err)
				}
				if kind == "ambiguous_existing" {
					o.EventKey = "legacy-unknown"
				}
				if err := db.ApplyMeterObservation(ctx, o, func(s MeterLedgerState) (MeterMutation, error) { return recoveryReconcileForTest(o, s) }); err != nil {
					t.Fatal(err)
				}
				if err := db.FinishMeterAttempt(ctx, id, "success", "partial", "0.153.4", "", nil); err != nil {
					t.Fatal(err)
				}
				end := "2026-09-09T18:28:32.127Z"
				if kind == "foreign_response" {
					end = "2026-09-09T20:10:49.831Z"
				}
				if _, err := db.sql.ExecContext(ctx, `UPDATE meter_attempts SET finished_at=? WHERE id=?`, end, id); err != nil {
					t.Fatal(err)
				}
				batch.Scope, _ = db.MeterRecoveryScope(ctx, attempts[0].ID)
			}
			before := recoveryDump(t, db)
			if _, err := db.RecoverMeterUsage(ctx, batch, true, callback); err == nil {
				t.Fatal("unsafe repair succeeded")
			}
			if recoveryDump(t, db) != before {
				t.Fatal("failed repair partially committed")
			}
			if kind == "interruption" {
				fresh, _ := recoveryBatch(t, db, home, attempts[0].ID)
				if _, err := db.RecoverMeterUsage(ctx, fresh, true, recoveryReconcileForTest); err != nil {
					t.Fatal(err)
				}
				if got := recoverySubtotal(t, db, attempts[0].ID); got != "11.903026" {
					t.Fatal(got)
				}
			}
		})
	}
}
func TestMeterRecoverySourceRevalidation(t *testing.T) {
	for _, kind := range []string{"modified", "replaced", "missing", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			db, home, _, _, evidence := recoveryLedger(t)
			before := recoveryDump(t, db)
			matches, err := filepath.Glob(filepath.Join(home, ".codex/sessions/2026/09/09/rollout-*.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			path := matches[0]
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "modified":
				err = os.WriteFile(path, append(raw, '\n'), 0600)
			case "replaced":
				err = os.Rename(path, path+".old")
				if err == nil {
					err = os.WriteFile(path, raw, 0600)
				}
			case "missing":
				err = os.Remove(path)
			case "symlink":
				err = os.Rename(path, path+".old")
				if err == nil {
					err = os.Symlink(path+".old", path)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = evidence.Revalidate(t.Context()); err == nil {
				t.Fatal("changed source accepted")
			}
			if recoveryDump(t, db) != before {
				t.Fatal("source validation mutated ledger")
			}
		})
	}
}
func TestMeterRecoveryInvalidSelectorsReadOnly(t *testing.T) {
	db, _, attempts, _, _ := recoveryLedger(t)
	before := recoveryDump(t, db)
	for _, id := range []string{"", "missing", attempts[0].ID} {
		if err := db.MatchMeterRecoveryAttempt(t.Context(), id, -1, 195); err == nil {
			t.Fatal("invalid selector accepted")
		}
	}
	if !reflect.DeepEqual(before, recoveryDump(t, db)) {
		t.Fatal("selector changed ledger")
	}
	missing := filepath.Join(t.TempDir(), "absent.db")
	if reader, err := OpenMeterReader(t.Context(), missing); err == nil {
		_ = reader.Close()
		t.Fatal("missing ledger opened")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("preview created database")
	}
}
