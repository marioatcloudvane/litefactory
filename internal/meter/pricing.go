package meter

import (
	"errors"
	"math/big"
	"regexp"
	"sort"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
)

// PriceRate is one exact USD-per-million billable dimension rate.
type PriceRate struct{ Dimension, USDPerMillion string }

// PricedDimension freezes a disjoint billable quantity, its applied rate and
// its amount rounded half-even to twelve USD decimal places.
type PricedDimension struct {
	Dimension                string
	Quantity                 *int64
	USDPerMillion, AmountUSD *string
	Status                   string
}

// Valuation contains observed consumption only. Collection completeness remains
// a separate concern and must qualify any public all-in estimate.
type Valuation struct {
	Dimensions                []PricedDimension
	EstimatedUSD              *string
	PricedSubtotalUSD, Status string
}

var rateSyntax = regexp.MustCompile(`^(0|[1-9][0-9]{0,17})(\.[0-9]{1,9})?$`)

// CanonicalRate validates precision without silently rounding a user's rate.
func CanonicalRate(value string) (string, error) {
	if !rateSyntax.MatchString(value) {
		return "", errors.New("price must be nonnegative decimal USD/million with at most 18 integral and 9 fractional digits")
	}
	if strings.Contains(value, ".") {
		value = strings.TrimRight(strings.TrimRight(value, "0"), ".")
	}
	return value, nil
}

// PriceDimensions enumerates accepted editable rates. Reasoning is an output
// subset, so it is deliberately absent from this billable list.
func PriceDimensions() []string {
	return []string{"input_uncached", "cache_read", "cache_write", "cache_write_5m", "cache_write_1h", "output"}
}

// Value applies a pinned catalog entry, never a current-price lookup. An empty
// list is a valid missing entry and remains explicitly unpriced.
func Value(tokens agent.TokenUsage, rates []PriceRate) (Valuation, error) {
	if err := Validate(tokens); err != nil {
		return Valuation{}, err
	}
	prices := make(map[string]string, len(rates))
	for _, rate := range rates {
		valid := false
		for _, dimension := range PriceDimensions() {
			if dimension == rate.Dimension {
				valid = true
			}
		}
		if !valid {
			return Valuation{}, errors.New("unknown price dimension")
		}
		if _, duplicate := prices[rate.Dimension]; duplicate {
			return Valuation{}, errors.New("duplicate price dimension")
		}
		value, err := CanonicalRate(rate.USDPerMillion)
		if err != nil {
			return Valuation{}, err
		}
		prices[rate.Dimension] = value
	}
	var result Valuation
	appendDimension := func(name string, quantity *int64, rateKey, status string) {
		d := PricedDimension{Dimension: name, Status: "unknown"}
		if quantity != nil {
			v := *quantity
			d.Quantity = &v
			d.Status = "unpriced"
			if rate, ok := prices[rateKey]; ok {
				d.USDPerMillion = &rate
				amount := dimensionAmount(v, rate)
				d.AmountUSD = &amount
				d.Status = status
			}
		}
		result.Dimensions = append(result.Dimensions, d)
	}
	appendDimension("input_uncached", tokens.InputUncached, "input_uncached", "priced")
	appendDimension("cache_read", tokens.CacheRead, "cache_read", "priced")
	if tokens.CacheWrite != nil && (tokens.CacheWrite5m == nil || tokens.CacheWrite1h == nil) {
		if _, generic := prices["cache_write"]; generic {
			appendDimension("cache_write_other", tokens.CacheWrite, "cache_write", "priced_total_fallback")
		} else {
			appendDimension("cache_write_5m", tokens.CacheWrite5m, "cache_write_5m", "priced")
			appendDimension("cache_write_1h", tokens.CacheWrite1h, "cache_write_1h", "priced")
			remaining := *tokens.CacheWrite
			for _, bucket := range []*int64{tokens.CacheWrite5m, tokens.CacheWrite1h} {
				if bucket != nil {
					remaining -= *bucket
				}
			}
			appendDimension("cache_write_other", &remaining, "cache_write", "priced")
		}
	} else {
		var remainder *int64
		if tokens.CacheWrite != nil {
			n := *tokens.CacheWrite - *tokens.CacheWrite5m - *tokens.CacheWrite1h
			remainder = &n
		}
		for _, bucket := range []struct {
			name     string
			quantity *int64
		}{{"cache_write_5m", tokens.CacheWrite5m}, {"cache_write_1h", tokens.CacheWrite1h}} {
			key := bucket.name
			if _, ok := prices[key]; !ok {
				key = "cache_write"
			}
			appendDimension(bucket.name, bucket.quantity, key, "priced")
		}
		appendDimension("cache_write_other", remainder, "cache_write", "priced")
	}
	appendDimension("output", tokens.Output, "output", "priced")
	subtotal := new(big.Rat)
	priced, unknown := 0, 0
	for _, d := range result.Dimensions {
		if d.AmountUSD == nil {
			unknown++
			continue
		}
		value, ok := new(big.Rat).SetString(*d.AmountUSD)
		if !ok {
			return Valuation{}, errors.New("invalid internally calculated amount")
		}
		subtotal.Add(subtotal, value)
		priced++
	}
	result.PricedSubtotalUSD = decimalUSD(subtotal)
	result.Status = "priced"
	if unknown > 0 {
		result.Status = "partial"
		if priced == 0 {
			result.Status = "unpriced"
		}
	} else {
		total := result.PricedSubtotalUSD
		result.EstimatedUSD = &total
	}
	allUnknown := true
	for _, d := range result.Dimensions {
		if d.Quantity != nil {
			allUnknown = false
		}
	}
	if allUnknown {
		result.Status = "unknown"
	}
	return result, nil
}
func dimensionAmount(quantity int64, rate string) string {
	value, _ := new(big.Rat).SetString(rate)
	value.Mul(value, new(big.Rat).SetInt64(quantity))
	value.Quo(value, new(big.Rat).SetInt64(1_000_000))
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(12), nil)
	numerator := new(big.Int).Mul(value.Num(), scale)
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(numerator, value.Denom(), remainder)
	compare := new(big.Int).Lsh(remainder, 1).Cmp(value.Denom())
	if compare > 0 || compare == 0 && quotient.Bit(0) == 1 {
		quotient.Add(quotient, big.NewInt(1))
	}
	return decimalUSD(new(big.Rat).SetFrac(quotient, scale))
}
func decimalUSD(value *big.Rat) string {
	result := value.FloatString(12)
	return strings.TrimRight(strings.TrimRight(result, "0"), ".")
}

// SortedRates converts an exact entry to a stable rate list for persistence.
func SortedRates(rates map[string]string) ([]PriceRate, error) {
	keys := make([]string, 0, len(rates))
	for key := range rates {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]PriceRate, 0, len(keys))
	for _, key := range keys {
		value, err := CanonicalRate(rates[key])
		if err != nil {
			return nil, err
		}
		out = append(out, PriceRate{Dimension: key, USDPerMillion: value})
	}
	return out, nil
}
