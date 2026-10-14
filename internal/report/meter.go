package report

import (
	"errors"
	"math"
	"math/big"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
)

// MeterValues is a prepared exclusive row. No provider summaries or historical
// valuation revisions may be supplied by orchestration.
type MeterValues struct {
	Tokens                          agent.TokenUsage
	EstimatedUSD, PricedSubtotalUSD *string
	PriceRevisionID                 int64
	CollectionStatus, PricingStatus string
	Diagnostics                     []string
}

// MeterSummary separates complete totals from known subtotals and never treats
// absent measurement as zero. Call Add for every selected coverage row as well.
type MeterSummary struct {
	Tokens            agent.TokenUsage `json:"tokens"`
	KnownTokens       agent.TokenUsage `json:"known_token_subtotals"`
	EstimatedUSD      *string          `json:"estimated_usd"`
	PricedSubtotalUSD *string          `json:"priced_subtotal_usd"`
	CollectionStatus  string           `json:"collection_status"`
	PricingStatus     string           `json:"pricing_status"`
	PriceRevisions    []int64          `json:"price_revisions"`
	Diagnostics       []string         `json:"diagnostic_codes"`
	HelperOverhead    string           `json:"helper_overhead"`
	rows              int
}

func meterFields(u *agent.TokenUsage) []**int64 {
	return []**int64{&u.InputUncached, &u.CacheRead, &u.CacheWrite, &u.CacheWrite5m, &u.CacheWrite1h, &u.Output, &u.Reasoning}
}

// Add combines stored quantities and exact frozen amounts; it never prices them.
func (s *MeterSummary) Add(v MeterValues) error {
	if v.CollectionStatus == "" {
		v.CollectionStatus = "unknown"
	}
	if v.PricingStatus == "" {
		v.PricingStatus = "unknown"
	}
	totals, known, values := meterFields(&s.Tokens), meterFields(&s.KnownTokens), meterFields(&v.Tokens)
	for i, p := range values {
		if *p == nil {
			*totals[i] = nil
			continue
		}
		if **p < 0 {
			return errors.New("negative stored meter quantity")
		}
		n := **p
		if *known[i] != nil {
			if **known[i] > math.MaxInt64-n {
				return errors.New("meter quantity overflow")
			}
			n += **known[i]
		}
		*known[i] = &n
		if s.rows == 0 || *totals[i] != nil {
			*totals[i] = &n
		}
	}
	var err error
	s.PricedSubtotalUSD, err = addMeterAmount(s.PricedSubtotalUSD, v.PricedSubtotalUSD)
	if err != nil {
		return err
	}
	if s.rows == 0 {
		s.EstimatedUSD = v.EstimatedUSD
		s.CollectionStatus = v.CollectionStatus
		s.PricingStatus = v.PricingStatus
	} else {
		if s.EstimatedUSD != nil && v.EstimatedUSD != nil {
			s.EstimatedUSD, err = addMeterAmount(s.EstimatedUSD, v.EstimatedUSD)
		} else {
			s.EstimatedUSD = nil
		}
		if err != nil {
			return err
		}
		if v.CollectionStatus != s.CollectionStatus {
			s.CollectionStatus = "partial"
		}
		if v.PricingStatus != s.PricingStatus {
			s.PricingStatus = "partial"
		}
	}
	if v.CollectionStatus != "complete" {
		s.EstimatedUSD = nil
	}
	if s.CollectionStatus != "complete" {
		s.EstimatedUSD = nil
		// Only explicitly dimension-scoped gaps preserve other dimension totals.
		// Historical or discovery gaps remain unknown, with measured subtotals.
		if !scopedMeterGap(v.Diagnostics) && v.CollectionStatus != "complete" {
			s.Tokens = agent.TokenUsage{}
		} else {
			for i, name := range []string{"input_uncached", "cache_read", "cache_write", "cache_write_5m", "cache_write_1h", "output", "reasoning"} {
				if slices.Contains(v.Diagnostics, "inclusive_summary_mismatch_"+name) {
					*totals[i] = nil
				}
			}
		}
	}
	if s.PricingStatus != "priced" {
		s.EstimatedUSD = nil
	}
	if v.PriceRevisionID > 0 && !slices.Contains(s.PriceRevisions, v.PriceRevisionID) {
		s.PriceRevisions = append(s.PriceRevisions, v.PriceRevisionID)
		slices.Sort(s.PriceRevisions)
	}
	s.Diagnostics = append(s.Diagnostics, v.Diagnostics...)
	slices.Sort(s.Diagnostics)
	s.Diagnostics = slices.Compact(s.Diagnostics)
	s.HelperOverhead = "excluded"
	s.rows++
	return nil
}

func addMeterAmount(a, b *string) (*string, error) {
	if b == nil {
		return a, nil
	}
	parse := func(value string) (*big.Rat, error) {
		if value == "" || strings.Trim(value, "0123456789.") != "" || strings.Count(value, ".") > 1 {
			return nil, errors.New("invalid stored meter amount")
		}
		r, ok := new(big.Rat).SetString(value)
		if !ok || r.Sign() < 0 {
			return nil, errors.New("invalid stored meter amount")
		}
		return r, nil
	}
	sum, err := parse(*b)
	if err != nil {
		return nil, err
	}
	if a != nil {
		prior, e := parse(*a)
		if e != nil {
			return nil, e
		}
		sum.Add(sum, prior)
	}
	result := sum.FloatString(12)
	return &result, nil
}

func scopedMeterGap(codes []string) bool {
	found := false
	for _, code := range codes {
		switch code {
		case "inclusive_summary_mismatch":
		case "inclusive_summary_mismatch_input_uncached", "inclusive_summary_mismatch_cache_read", "inclusive_summary_mismatch_cache_write", "inclusive_summary_mismatch_output":
			found = true
		default:
			return false
		}
	}
	return found
}
