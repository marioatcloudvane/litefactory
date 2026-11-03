package localusage

import (
	"context"

	"github.com/cloudvane/litefactory/internal/meterview"
	"github.com/cloudvane/litefactory/internal/store"
)

// Rate is an exact immutable USD-per-million-token price value.
type Rate struct {
	CodingAgent   string `json:"coding_agent"`
	Model         string `json:"model"`
	Dimension     string `json:"dimension"`
	USDPerMillion string `json:"usd_per_million"`
}

// Catalog is a complete current price revision; absent rates remain unknown.
type Catalog struct {
	RevisionID int64  `json:"revision_id"`
	Rates      []Rate `json:"rates"`
}

// Prices owns existing atomic price revision reads and replacement.
type Prices struct{ db *store.DB }

// NewPrices composes local price operations over existing state.
func NewPrices(db *store.DB) *Prices { return &Prices{db: db} }

// List returns the current catalog without exposing persistence rows.
func (s *Prices) List(ctx context.Context) (Catalog, error) {
	source, err := meterview.Prices(ctx, s.db, "", "")
	if err != nil {
		return Catalog{}, err
	}
	result := Catalog{RevisionID: source.RevisionID}
	if source.Rates != nil {
		result.Rates = make([]Rate, 0, len(source.Rates))
	}
	for _, rate := range source.Rates {
		result.Rates = append(result.Rates, Rate(rate))
	}
	return result, nil
}

// Set replaces one entry in a new revision, preserving all attempt price pins.
func (s *Prices) Set(ctx context.Context, codingAgent, model string, rates []Rate) (int64, error) {
	values := make([]store.MeterPriceRate, 0, len(rates))
	for _, rate := range rates {
		values = append(values, store.MeterPriceRate(rate))
	}
	return s.db.SetMeterPrices(ctx, codingAgent, model, values)
}
