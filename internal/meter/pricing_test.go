package meter

import (
	"math"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func pricedTokens(input, output, writes int64) agent.TokenUsage {
	zero := int64(0)
	return agent.TokenUsage{InputUncached: &input, Output: &output, CacheRead: &zero, CacheWrite: &writes, CacheWrite5m: &zero, CacheWrite1h: &zero, Reasoning: &zero}
}
func TestPriceExactRounding(t *testing.T) {
	for _, tc := range []struct{ rate, want string }{{"0.0000005", "0"}, {"0.0000015", "0.000000000002"}, {"1.25", "0.00000125"}, {"0", "0"}} {
		if got := dimensionAmount(1, tc.rate); got != tc.want {
			t.Errorf("%s -> %s want %s", tc.rate, got, tc.want)
		}
	}
	if got := dimensionAmount(math.MaxInt64, "999999999999999999.999999999"); got == "" {
		t.Fatal("overflow lost amount")
	}
	for _, rate := range []string{"-1", "1e3", "NaN", "0.0000000001", "1000000000000000000", "01", "1."} {
		if _, err := CanonicalRate(rate); err == nil {
			t.Errorf("accepted %s", rate)
		}
	}
	if got, err := CanonicalRate("1.25000"); err != nil || got != "1.25" {
		t.Fatal(got, err)
	}
}
func TestPriceUnknownAndFrozenDimensions(t *testing.T) {
	v, err := Value(agent.TokenUsage{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != "unknown" || v.EstimatedUSD != nil {
		t.Fatal(v)
	}
	v, err = Value(pricedTokens(100, 10, 0), nil)
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != "unpriced" || v.EstimatedUSD != nil {
		t.Fatal(v)
	}
	rates := []PriceRate{{"input_uncached", "2"}, {"output", "10"}, {"cache_read", "0"}, {"cache_write", "0"}}
	v, err = Value(pricedTokens(100, 10, 0), rates)
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != "priced" || v.EstimatedUSD == nil || *v.EstimatedUSD != "0.0003" {
		t.Fatalf("%+v", v)
	}
}
func TestPriceCacheSubsets(t *testing.T) {
	tokens := pricedTokens(0, 0, 100)
	five, hour := int64(30), int64(20)
	tokens.CacheWrite5m = &five
	tokens.CacheWrite1h = &hour
	rates := []PriceRate{{"input_uncached", "0"}, {"output", "0"}, {"cache_read", "0"}, {"cache_write", "1"}, {"cache_write_5m", "2"}, {"cache_write_1h", "3"}}
	v, err := Value(tokens, rates)
	if err != nil {
		t.Fatal(err)
	}
	if v.PricedSubtotalUSD != "0.00017" {
		t.Fatal(v)
	}
	tokens.CacheWrite1h = nil
	v, err = Value(tokens, rates)
	if err != nil {
		t.Fatal(err)
	}
	if v.PricedSubtotalUSD != "0.0001" {
		t.Fatal("total fallback double counted", v)
	}
	rates = append(rates[:3], rates[4:]...)
	v, err = Value(tokens, rates)
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != "partial" || v.EstimatedUSD != nil {
		t.Fatal(v)
	}
	for _, d := range v.Dimensions {
		if d.Dimension == "cache_write_other" && (d.Quantity == nil || *d.Quantity != 70) {
			t.Fatal("known unpriced remainder discarded", d)
		}
	}
	tokens.Output = new(int64)
	tokens.Reasoning = &five
	if _, err := Value(tokens, rates); err == nil {
		t.Fatal("invalid subset accepted")
	}
}
