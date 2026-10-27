package store

import (
	"cmp"
	"context"
	"database/sql"
	"slices"
)

// MeterHistoricalRate is a referenced rate at the attempt's recorded revision.
// Missing entries remain missing; the current catalogue is never substituted.
type MeterHistoricalRate struct {
	RevisionID int64 `json:"revision_id"`
	MeterPriceRate
	Currency       string `json:"currency"`
	RateScale      int    `json:"rate_scale"`
	ValuationScale int    `json:"valuation_scale"`
	RoundingPolicy string `json:"rounding_policy"`
}

type meterRateReference struct {
	revision     int64
	agent, model string
}

func readMeterHistoricalRates(ctx context.Context, tx *sql.Tx, ref meterRateReference) ([]MeterHistoricalRate, error) {
	rows, err := tx.QueryContext(ctx, `SELECT r.revision_id,r.coding_agent,r.model,r.dimension,r.usd_per_million,p.currency,p.rate_scale,p.valuation_scale,p.rounding_policy FROM meter_price_rates r JOIN meter_price_revisions p ON p.id=r.revision_id WHERE r.revision_id=? AND r.coding_agent=? AND r.model=? ORDER BY r.dimension`, ref.revision, ref.agent, ref.model)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []MeterHistoricalRate
	for rows.Next() {
		var r MeterHistoricalRate
		if err := rows.Scan(&r.RevisionID, &r.CodingAgent, &r.Model, &r.Dimension, &r.USDPerMillion, &r.Currency, &r.RateScale, &r.ValuationScale, &r.RoundingPolicy); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

func compareMeterHistoricalRates(a, b MeterHistoricalRate) int {
	return cmp.Or(cmp.Compare(a.RevisionID, b.RevisionID), cmp.Compare(a.CodingAgent, b.CodingAgent), cmp.Compare(a.Model, b.Model), cmp.Compare(a.Dimension, b.Dimension))
}

// HistoricalRates returns a private copy of the snapshot's referenced rates.
// Rates are captured only when IncludeHistoricalRates was requested.
func (s *MeterSnapshot) HistoricalRates() []MeterHistoricalRate {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.historicalRates)
}

func meterSnapshotFamily(ctx context.Context, tx *sql.Tx, repoID, issue int64, limit int) ([]int64, error) {
	// A task snapshot always has a repository-qualified selector. Keep the same
	// persisted membership policy as meterPredicates, including retired links.
	rows, err := tx.QueryContext(ctx, `SELECT issue_number FROM plan_story_drafts WHERE parent=(SELECT forge||':'||owner||'/'||name||'#'||? FROM repos WHERE id=?) AND issue_number>0 UNION SELECT issue_number FROM managed_stories WHERE parent_task=(SELECT forge||':'||owner||'/'||name||'#'||? FROM repos WHERE id=?) LIMIT ?`, issue, repoID, issue, repoID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var members []int64
	for rows.Next() {
		var member int64
		if err := rows.Scan(&member); err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	return members, rows.Err()
}

// SelectedIssues records the task's repository-qualified report membership at
// capture time, including members with no usage. Factory/repository reads have
// no selected task family and return nil.
func (s *MeterSnapshot) SelectedIssues() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.selectedIssues)
}
