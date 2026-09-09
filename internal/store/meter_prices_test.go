package store

import (
	"context"
	"strings"
	"testing"
)

func rateEntry(model, price string) []MeterPriceRate {
	return []MeterPriceRate{{CodingAgent: "codex", Model: model, Dimension: "input_uncached", USDPerMillion: price}, {CodingAgent: "codex", Model: model, Dimension: "output", USDPerMillion: price}}
}
func TestMeterPriceCatalogImmutableAndAtomic(t *testing.T) {
	db, a := meterFixture(t)
	ctx := t.Context()
	first, err := db.SetMeterPrices(ctx, "codex", "m", rateEntry("m", "1.2500"))
	if err != nil {
		t.Fatal(err)
	}
	if first != 2 {
		t.Fatal(first)
	}
	second, err := db.SetMeterPrices(ctx, "codex", "other", rateEntry("other", "0"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SetMeterPrices(ctx, "codex", "m", rateEntry("m", "-1")); err == nil {
		t.Fatal("invalid edit accepted")
	}
	catalog, err := db.ListMeterPrices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.RevisionID != second || len(catalog.Rates) != 4 {
		t.Fatal(catalog)
	}
	later, err := db.StartMeterAttempt(ctx, a.RunID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if later.PriceRevisionID != second {
		t.Fatal(later)
	}
	original, err := db.GetMeterAttempt(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if original.PriceRevisionID != 1 {
		t.Fatal("in-flight attempt repinned")
	}
	var count int
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM meter_price_rates WHERE revision_id=1`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("empty baseline backfilled")
	}
	if _, err := db.SetMeterPrices(ctx, "codex", "m", rateEntry("m", "3")); err != nil {
		t.Fatal(err)
	}
	var rate string
	if err := db.sql.QueryRowContext(ctx, `SELECT usd_per_million FROM meter_price_rates WHERE revision_id=? AND model='m' AND dimension='output'`, first).Scan(&rate); err != nil {
		t.Fatal(err)
	}
	if rate != "1.25" {
		t.Fatal("historical price changed", rate)
	}
}
func TestMeterValuationSnapshotAndPin(t *testing.T) {
	db, a := meterFixture(t)
	ctx := t.Context()
	if _, err := db.SetMeterPrices(ctx, "codex", "test-model", rateEntry("test-model", "100")); err != nil {
		t.Fatal(err)
	}
	a, err := db.StartMeterAttempt(ctx, a.RunID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.PutMeterInvocation(ctx, MeterInvocation{AttemptID: a.ID, ID: "root", SourceID: "s", SourceInvocationID: "root"}); err != nil {
		t.Fatal(err)
	}
	o := meterObservation(a, "first")
	n := int64(10)
	amount := "0.001"
	rate := "100"
	write := func(ctx context.Context, event string) error {
		o.EventKey = event
		return db.ApplyMeterObservation(ctx, o, func(state MeterLedgerState) (MeterMutation, error) {
			if state.Catalog.RevisionID != 2 || len(state.Catalog.Rates) != 2 {
				t.Fatal("late model used latest catalog")
			}
			return MeterMutation{Measurement: &MeterTokens{Output: &n}, Valuation: partialOutputValuation(&n, &rate, &amount)}, nil
		})
	}
	if err := write(ctx, "one"); err != nil {
		t.Fatal(err)
	}
	snapshot, err := db.NewMeterSnapshot(ctx, MeterFilter{CodingAgent: "codex", Model: "test-model"}, MeterSnapshotOptions{})
	if err != nil {
		t.Fatal(err)
	}
	n = 20
	amount = "0.002"
	if err := write(ctx, "two"); err != nil {
		t.Fatal(err)
	}
	rows, err := snapshot.Next(ctx, 256)
	if err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Close(ctx); err != nil {
		t.Fatal(err)
	}
	var measured []MeterRow
	for _, r := range rows {
		if r.MeasurementID != 0 {
			measured = append(measured, r)
		}
	}
	rows = measured
	if len(rows) != 1 || rows[0].PricedSubtotalUSD == nil || *rows[0].PricedSubtotalUSD != "0.001" || *rows[0].Tokens.Output != 10 {
		t.Fatal(rows)
	}
	if rows[0].EstimatedUSD != nil {
		t.Fatal("incomplete collection exposed all-in estimate")
	}
	var count int
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM meter_valuations`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatal("valuation revisions not append-only")
	}
}
func TestMeterConcurrentCatalogEdits(t *testing.T) {
	db, _ := meterFixture(t)
	ctx := t.Context()
	results := make(chan error, 2)
	for _, model := range []string{"a", "b"} {
		go func() { _, err := db.SetMeterPrices(ctx, "codex", model, rateEntry(model, "1")); results <- err }()
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	catalog, err := db.ListMeterPrices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Rates) != 4 {
		t.Fatal("lost concurrent edit", catalog)
	}
}
func TestMeterDiagnosticDedupBeforeBound(t *testing.T) {
	codes := []string{}
	for i := 0; i < 33; i++ {
		codes = append(codes, "diagnostic_"+strings.Repeat("a", i))
	}
	encoded, err := meterDiagnostics(codes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mergeMeterDiagnostics(encoded, codes); err != nil {
		t.Fatal(err)
	}
}

func partialOutputValuation(n *int64, rate, amount *string) *MeterValuation {
	v := &MeterValuation{Status: "partial", PricedSubtotalUSD: *amount}
	for _, dimension := range []string{"input_uncached", "cache_read", "cache_write_5m", "cache_write_1h", "cache_write_other"} {
		v.Dimensions = append(v.Dimensions, MeterValuationDimension{Dimension: dimension, Status: "unknown"})
	}
	v.Dimensions = append(v.Dimensions, MeterValuationDimension{Dimension: "output", Quantity: n, USDPerMillion: rate, AmountUSD: amount, Status: "priced"})
	return v
}
func TestMeterRejectsMalformedFrozenValuations(t *testing.T) {
	for _, kind := range []string{"missing_dimension", "wrong_quantity", "invalid_status", "oversize_amount", "wrong_amount"} {
		t.Run(kind, func(t *testing.T) {
			db, base := meterFixture(t)
			ctx := t.Context()
			if _, err := db.SetMeterPrices(ctx, "codex", "test-model", rateEntry("test-model", "100")); err != nil {
				t.Fatal(err)
			}
			a, err := db.StartMeterAttempt(ctx, base.RunID, "codex")
			if err != nil {
				t.Fatal(err)
			}
			if err := db.PutMeterInvocation(ctx, MeterInvocation{AttemptID: a.ID, ID: "root", SourceID: "s", SourceInvocationID: "root"}); err != nil {
				t.Fatal(err)
			}
			o := meterObservation(a, "one")
			n := int64(10)
			rate, amount := "100", "0.001"
			v := partialOutputValuation(&n, &rate, &amount)
			switch kind {
			case "missing_dimension":
				v.Dimensions = v.Dimensions[1:]
			case "wrong_quantity":
				other := int64(99)
				v.Dimensions[5].Quantity = &other
			case "invalid_status":
				v.Dimensions[5].Status = "bogus"
			case "oversize_amount":
				v.PricedSubtotalUSD = strings.Repeat("9", 10000)
			case "wrong_amount":
				other := "0.002"
				v.Dimensions[5].AmountUSD = &other
				v.PricedSubtotalUSD = other
			}
			if err := db.ApplyMeterObservation(ctx, o, func(MeterLedgerState) (MeterMutation, error) {
				return MeterMutation{Measurement: &MeterTokens{Output: &n}, Valuation: v}, nil
			}); err == nil {
				t.Fatal("invalid frozen valuation accepted")
			}
			var count int
			if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM meter_observations WHERE attempt_id=?`, a.ID).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatal("invalid valuation partially committed")
			}
		})
	}
}
