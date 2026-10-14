package report

import (
	"github.com/cloudvane/litefactory/internal/agent"
	"math"
	"testing"
)

func TestMeterSummaryKnownAndUnknown(t *testing.T) {
	t.Parallel()
	zero := int64(0)
	n := int64(5)
	amount := "0.000000000001"
	var s MeterSummary
	if err := s.Add(MeterValues{Tokens: agent.TokenUsage{InputUncached: &zero, Output: &n}, EstimatedUSD: &amount, PricedSubtotalUSD: &amount, PriceRevisionID: 1, CollectionStatus: "complete", PricingStatus: "priced"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Add(MeterValues{Tokens: agent.TokenUsage{}, CollectionStatus: "unknown", PricingStatus: "unpriced", PriceRevisionID: 2}); err != nil {
		t.Fatal(err)
	}
	if s.EstimatedUSD != nil || s.Tokens.Output != nil || *s.KnownTokens.Output != 5 || s.PricedSubtotalUSD == nil || *s.PricedSubtotalUSD != "0.000000000001" {
		t.Fatalf("summary %+v", s)
	}
}
func TestMeterSummaryExactAmountsAndOverflow(t *testing.T) {
	t.Parallel()
	amount := "0.000000000001"
	var s MeterSummary
	for range 3 {
		if err := s.Add(MeterValues{EstimatedUSD: &amount, PricedSubtotalUSD: &amount, CollectionStatus: "complete", PricingStatus: "priced"}); err != nil {
			t.Fatal(err)
		}
	}
	if *s.EstimatedUSD != "0.000000000003" {
		t.Fatal(*s.EstimatedUSD)
	}
	maximumTokens := int64(math.MaxInt64)
	one := int64(1)
	s = MeterSummary{}
	if err := s.Add(MeterValues{Tokens: agent.TokenUsage{Output: &maximumTokens}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Add(MeterValues{Tokens: agent.TokenUsage{Output: &one}}); err == nil {
		t.Fatal("overflow accepted")
	}
}

func TestMeterHomogeneousUnknownAndUnpriced(t *testing.T) {
	t.Parallel()
	for _, pricing := range []string{"unknown", "unpriced"} {
		var s MeterSummary
		for range 2 {
			if err := s.Add(MeterValues{CollectionStatus: "unknown", PricingStatus: pricing}); err != nil {
				t.Fatal(err)
			}
		}
		if s.CollectionStatus != "unknown" || s.PricingStatus != pricing {
			t.Fatalf("lost status: %+v", s)
		}
	}
}

func TestMeterPartialQuantityAndUnpricedEstimate(t *testing.T) {
	t.Parallel()
	n := int64(7)
	amount := "1"
	var s MeterSummary
	if err := s.Add(MeterValues{Tokens: agent.TokenUsage{Output: &n}, EstimatedUSD: &amount, PricedSubtotalUSD: &amount, CollectionStatus: "partial", PricingStatus: "priced"}); err != nil {
		t.Fatal(err)
	}
	if s.Tokens.Output != nil || s.KnownTokens.Output == nil || *s.KnownTokens.Output != 7 || s.EstimatedUSD != nil {
		t.Fatalf("partial masquerades as total: %+v", s)
	}
	s = MeterSummary{}
	if err := s.Add(MeterValues{EstimatedUSD: &amount, CollectionStatus: "complete", PricingStatus: "unpriced"}); err != nil {
		t.Fatal(err)
	}
	if s.EstimatedUSD != nil {
		t.Fatal("unpriced amount displayed")
	}
}

func TestMeterDimensionGapPreservesOtherTotals(t *testing.T) {
	t.Parallel()
	n, zero := int64(7), int64(0)
	amount := "1"
	var s MeterSummary
	if err := s.Add(MeterValues{Tokens: agent.TokenUsage{InputUncached: &n, CacheRead: &zero, Output: &n}, EstimatedUSD: &amount, PricedSubtotalUSD: &amount, CollectionStatus: "partial", PricingStatus: "priced", Diagnostics: []string{"inclusive_summary_mismatch", "inclusive_summary_mismatch_output"}}); err != nil {
		t.Fatal(err)
	}
	if s.Tokens.InputUncached == nil || *s.Tokens.InputUncached != 7 || s.Tokens.CacheRead == nil || *s.Tokens.CacheRead != 0 || s.Tokens.Output != nil || s.Tokens.CacheWrite != nil || *s.KnownTokens.Output != 7 || s.EstimatedUSD != nil || s.PricedSubtotalUSD == nil {
		t.Fatalf("scoped gap: %+v", s)
	}
	if err := s.Add(MeterValues{Tokens: agent.TokenUsage{InputUncached: &n}, CollectionStatus: "partial", Diagnostics: []string{"descendant_usage_unavailable"}}); err != nil {
		t.Fatal(err)
	}
	if s.Tokens.InputUncached != nil || *s.KnownTokens.InputUncached != 14 {
		t.Fatalf("descendant gap: %+v", s)
	}
}
