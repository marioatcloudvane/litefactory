package store

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
)

// MeterPriceRate is an immutable exact-decimal catalog row.
type MeterPriceRate struct {
	CodingAgent   string `json:"coding_agent"`
	Model         string `json:"model"`
	Dimension     string `json:"dimension"`
	USDPerMillion string `json:"usd_per_million"`
}

// MeterCatalog is a complete revision; absent rows mean unknown rates.
type MeterCatalog struct {
	RevisionID int64            `json:"revision_id"`
	Rates      []MeterPriceRate `json:"rates"`
}

// MeterValuationDimension retains the quantity and applied rate together.
type MeterValuationDimension struct {
	Dimension                string
	Quantity                 *int64
	USDPerMillion, AmountUSD *string
	Status                   string
}

// MeterValuation is append-only and belongs to exactly one measurement revision.
type MeterValuation struct {
	EstimatedUSD              *string
	PricedSubtotalUSD, Status string
	Dimensions                []MeterValuationDimension
}

var meterRateSyntax = regexp.MustCompile(`^(0|[1-9][0-9]{0,17})(\.[0-9]{1,9})?$`)

// ListMeterPrices returns the current complete catalog without changing prices.
func (db *DB) ListMeterPrices(ctx context.Context) (MeterCatalog, error) {
	var catalog MeterCatalog
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT MAX(id) FROM meter_price_revisions`).Scan(&catalog.RevisionID); err != nil {
			return err
		}
		var err error
		catalog.Rates, err = readMeterRates(ctx, tx, catalog.RevisionID)
		return err
	})
	return catalog, err
}
func readMeterRates(ctx context.Context, tx *sql.Tx, revision int64) ([]MeterPriceRate, error) {
	rows, err := tx.QueryContext(ctx, `SELECT coding_agent,model,dimension,usd_per_million FROM meter_price_rates WHERE revision_id=? ORDER BY coding_agent,model,dimension`, revision)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MeterPriceRate{}
	for rows.Next() {
		var r MeterPriceRate
		if err := rows.Scan(&r.CodingAgent, &r.Model, &r.Dimension, &r.USDPerMillion); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SetMeterPrices copies the latest complete catalog and replaces one entry
// atomically. Omitted dimensions disappear only from the replacement entry.
func (db *DB) SetMeterPrices(ctx context.Context, codingAgent, model string, rates []MeterPriceRate) (int64, error) {
	if err := meterIdentity(codingAgent, model); err != nil {
		return 0, err
	}
	if codingAgent != "claude" && codingAgent != "codex" {
		return 0, errors.New("store: unknown coding agent")
	}
	if len(rates) < 2 || len(rates) > 6 {
		return 0, errors.New("store: price entry requires input and output rates")
	}
	seen := map[string]bool{}
	for _, r := range rates {
		if r.CodingAgent != codingAgent || r.Model != model {
			return 0, errors.New("store: rate belongs to another entry")
		}
		switch r.Dimension {
		case "input_uncached", "cache_read", "cache_write", "cache_write_5m", "cache_write_1h", "output":
		default:
			return 0, errors.New("store: unknown price dimension")
		}
		if seen[r.Dimension] {
			return 0, errors.New("store: duplicate price dimension")
		}
		seen[r.Dimension] = true
		if !meterRateSyntax.MatchString(r.USDPerMillion) {
			return 0, errors.New("store: invalid exact price rate")
		}
	}
	if !seen["input_uncached"] || !seen["output"] {
		return 0, errors.New("store: input and output rates required")
	}
	var revision int64
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var previous int64
		if err := tx.QueryRowContext(ctx, `SELECT MAX(id) FROM meter_price_revisions`).Scan(&previous); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `INSERT INTO meter_price_revisions(created_at,currency,rate_scale,valuation_scale,rounding_policy) VALUES(?,'USD',9,12,'half_even_per_measurement_dimension_v1') RETURNING id`, FormatTime(nowUTC())).Scan(&revision); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO meter_price_rates SELECT ?,coding_agent,model,dimension,usd_per_million FROM meter_price_rates WHERE revision_id=? AND NOT(coding_agent=? AND model=?)`, revision, previous, codingAgent, model); err != nil {
			return err
		}
		for _, rate := range rates {
			value := rate.USDPerMillion
			if strings.Contains(value, ".") {
				value = strings.TrimRight(strings.TrimRight(value, "0"), ".")
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO meter_price_rates VALUES(?,?,?,?,?)`, revision, codingAgent, model, rate.Dimension, value); err != nil {
				return err
			}
		}
		return nil
	})
	return revision, err
}

func writeMeterValuation(ctx context.Context, tx *sql.Tx, measurementID, measurementRevision, priceRevision int64, v MeterValuation) error {
	var id int64
	if err := tx.QueryRowContext(ctx, `INSERT INTO meter_valuations(measurement_id,measurement_revision,price_revision_id,pricing_status,estimated_usd,priced_subtotal_usd,created_at) VALUES(?,?,?,?,?,?,?) RETURNING id`, measurementID, measurementRevision, priceRevision, v.Status, v.EstimatedUSD, v.PricedSubtotalUSD, FormatTime(nowUTC())).Scan(&id); err != nil {
		return err
	}
	for _, d := range v.Dimensions {
		if _, err := tx.ExecContext(ctx, `INSERT INTO meter_valuation_dimensions(valuation_id,dimension,quantity,usd_per_million,amount_usd,pricing_status) VALUES(?,?,?,?,?,?)`, id, d.Dimension, d.Quantity, d.USDPerMillion, d.AmountUSD, d.Status); err != nil {
			return err
		}
	}
	return nil
}
