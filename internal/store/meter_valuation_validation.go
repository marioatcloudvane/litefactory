package store

import (
	"context"
	"database/sql"
	"errors"
	"math/big"
	"regexp"
)

var meterAmountSyntax = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]{1,12})?$`)

func meterAmount(value string) (*big.Rat, error) {
	if len(value) > 128 || !meterAmountSyntax.MatchString(value) {
		return nil, errors.New("store: invalid bounded monetary amount")
	}
	amount, ok := new(big.Rat).SetString(value)
	if !ok {
		return nil, errors.New("store: invalid monetary amount")
	}
	return amount, nil
}
func validateMeterValuation(ctx context.Context, tx *sql.Tx, o MeterObservation, state MeterLedgerState, next MeterMutation) error {
	if next.Valuation == nil {
		return nil
	}
	if next.Measurement == nil {
		return errors.New("store: valuation without measurement")
	}
	v := next.Valuation
	if len(v.Dimensions) < 1 || len(v.Dimensions) > 6 {
		return errors.New("store: invalid valuation dimensions")
	}
	if _, err := meterAmount(v.PricedSubtotalUSD); err != nil {
		return err
	}
	if v.EstimatedUSD != nil {
		if _, err := meterAmount(*v.EstimatedUSD); err != nil {
			return err
		}
	}
	var codingAgent string
	if err := tx.QueryRowContext(ctx, `SELECT coding_agent FROM meter_attempts WHERE id=?`, o.AttemptID).Scan(&codingAgent); err != nil {
		return err
	}
	prices := map[string]string{}
	if o.ActualModel != nil {
		for _, r := range state.Catalog.Rates {
			if r.CodingAgent == codingAgent && r.Model == *o.ActualModel {
				prices[r.Dimension] = r.USDPerMillion
			}
		}
	}
	expected, fallback := meterFrozenQuantities(*next.Measurement, prices)
	if len(v.Dimensions) != len(expected) {
		return errors.New("store: missing billable valuation dimensions")
	}
	seen := map[string]bool{}
	subtotal := new(big.Rat)
	missing, known, priced := 0, 0, 0
	for _, d := range v.Dimensions {
		quantity, present := expected[d.Dimension]
		if !present || seen[d.Dimension] {
			return errors.New("store: invalid or duplicate valuation dimension")
		}
		seen[d.Dimension] = true
		if quantity == nil != (d.Quantity == nil) || quantity != nil && *quantity != *d.Quantity {
			return errors.New("store: frozen quantity differs from measurement")
		}
		key := d.Dimension
		if key == "cache_write_other" {
			key = "cache_write"
		}
		if key == "cache_write_5m" || key == "cache_write_1h" {
			if _, ok := prices[key]; !ok {
				key = "cache_write"
			}
		}
		if d.Quantity == nil {
			if d.AmountUSD != nil || d.USDPerMillion != nil || d.Status != "unknown" {
				return errors.New("store: invalid unknown quantity")
			}
			missing++
			continue
		}
		known++
		if *d.Quantity < 0 {
			return errors.New("store: negative frozen quantity")
		}
		rate, hasRate := prices[key]
		if !hasRate {
			if d.USDPerMillion != nil || d.AmountUSD != nil || d.Status != "unpriced" {
				return errors.New("store: invalid unpriced quantity")
			}
			missing++
			continue
		}
		expectedStatus := "priced"
		if fallback && d.Dimension == "cache_write_other" {
			expectedStatus = "priced_total_fallback"
		}
		if d.Status != expectedStatus || d.USDPerMillion == nil || rate != *d.USDPerMillion || d.AmountUSD == nil {
			return errors.New("store: valuation rate/status differs from pinned catalog")
		}
		amount, err := meterAmount(*d.AmountUSD)
		if err != nil {
			return err
		}
		if !meterRoundedAmount(*d.Quantity, rate, amount) {
			return errors.New("store: frozen amount differs from half-even valuation")
		}
		subtotal.Add(subtotal, amount)
		priced++
	}
	stored, err := meterAmount(v.PricedSubtotalUSD)
	if err != nil {
		return err
	}
	if stored.Cmp(subtotal) != 0 {
		return errors.New("store: frozen subtotal mismatch")
	}
	expectedStatus := "priced"
	if missing > 0 {
		expectedStatus = "partial"
		if priced == 0 {
			expectedStatus = "unpriced"
		}
		if known == 0 {
			expectedStatus = "unknown"
		}
	}
	if v.Status != expectedStatus {
		return errors.New("store: incorrect pricing completeness")
	}
	if missing == 0 {
		if v.EstimatedUSD == nil || *v.EstimatedUSD != v.PricedSubtotalUSD {
			return errors.New("store: invalid complete valuation")
		}
	} else if v.EstimatedUSD != nil {
		return errors.New("store: incomplete valuation marked complete")
	}
	return nil
}

// This checks the persisted dimensional contract, not a provider interpretation:
// either three disjoint write buckets or one explicitly marked total fallback.
func meterFrozenQuantities(t MeterTokens, prices map[string]string) (map[string]*int64, bool) {
	expected := map[string]*int64{"input_uncached": t.InputUncached, "cache_read": t.CacheRead, "output": t.Output}
	_, generic := prices["cache_write"]
	fallback := t.CacheWrite != nil && (t.CacheWrite5m == nil || t.CacheWrite1h == nil) && generic
	if fallback {
		expected["cache_write_other"] = t.CacheWrite
		return expected, true
	}
	expected["cache_write_5m"] = t.CacheWrite5m
	expected["cache_write_1h"] = t.CacheWrite1h
	var remainder *int64
	if t.CacheWrite != nil {
		n := *t.CacheWrite
		for _, bucket := range []*int64{t.CacheWrite5m, t.CacheWrite1h} {
			if bucket != nil {
				n -= *bucket
			}
		}
		remainder = &n
	}
	expected["cache_write_other"] = remainder
	return expected, false
}

// Validate the rounding interval and tie parity without performing a second
// valuation. All monetary strings were bounded before arbitrary-precision work.
func meterRoundedAmount(quantity int64, rate string, amount *big.Rat) bool {
	exact, ok := new(big.Rat).SetString(rate)
	if !ok {
		return false
	}
	exact.Mul(exact, new(big.Rat).SetInt64(quantity))
	exact.Quo(exact, new(big.Rat).SetInt64(1_000_000))
	delta := new(big.Rat).Sub(exact, amount)
	delta.Abs(delta)
	half := new(big.Rat).SetFrac(big.NewInt(1), big.NewInt(2_000_000_000_000))
	comparison := delta.Cmp(half)
	if comparison < 0 {
		return true
	}
	if comparison > 0 {
		return false
	}
	scaled := new(big.Rat).Mul(amount, new(big.Rat).SetInt64(1_000_000_000_000))
	return scaled.IsInt() && scaled.Num().Bit(0) == 0
}
