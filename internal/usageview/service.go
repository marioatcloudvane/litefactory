// Package usageview projects recorded usage without workflow or price mutation.
package usageview

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/url"
	"strings"

	"github.com/cloudvane/litefactory/internal/connector"
	"github.com/cloudvane/litefactory/internal/meterview"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

// Reader exposes only bounded consistent recorded-meter acquisition.
type Reader interface {
	NewMeterSnapshot(context.Context, store.MeterFilter, store.MeterSnapshotOptions) (*store.MeterSnapshot, error)
}

// Service owns the read-only projection and its shared redaction boundary.
type Service struct {
	reader   Reader
	redactor *redact.Redactor
	roots    []string
}

// New composes recorded evidence with the installation's shared secret redactor.
func New(reader Reader, r *redact.Redactor, roots ...string) *Service {
	return &Service{reader, r, append([]string{"/Users", "/home", "/private", "/var", "/tmp"}, roots...)}
}

// PolicyID invalidates retained projections when registered secrets change.
func (s *Service) PolicyID() string {
	if s.redactor == nil {
		return ""
	}
	return s.redactor.PolicyID()
}

// Usage acquires everything before returning any facts and closes SQLite state
// even after cancellation. No live snapshot escapes to a remote continuation.
func (s *Service) Usage(ctx context.Context, r connector.UsageRequest) (out connector.UsageProjection, err error) {
	if s.reader == nil || s.redactor == nil {
		return out, errors.New("usage unavailable")
	}
	policy := s.PolicyID()
	rows := []store.MeterRow{}
	snapshot, e := s.reader.NewMeterSnapshot(ctx, r.Filter, store.MeterSnapshotOptions{MaxRows: 10000, MaxBytes: 16 << 20, IncludeHistoricalRates: true, AllowEmptyInterval: r.Empty})
	if e != nil {
		return out, e
	}
	defer func() { err = errors.Join(err, snapshot.Close(context.WithoutCancel(ctx))) }()
	for {
		page, e := snapshot.Next(ctx, 256)
		if e != nil {
			return out, e
		}
		if len(page) == 0 {
			break
		}
		rows = append(rows, page...)
	}
	rates := snapshot.HistoricalRates()
	members := snapshot.SelectedIssues()
	view, e := meterview.BuildFromRows(rows, false)
	if e != nil {
		return out, e
	}
	facts := map[string]any{"repository": r.Repository, "selected_issue_count": len(members), "totals": view.Summary, "records": len(rows), "price_basis": rates[:min(50, len(rates))], "price_basis_count": len(rates), "price_basis_has_more": len(rates) > 50, "gaps": []string{}, "helper_overhead": "excluded", "filters": map[string]any{"mode": r.Filter.Mode, "agent": r.Filter.CodingAgent, "model": r.Filter.Model, "view": r.View, "by": r.By}}
	gaps := append([]string{}, view.Summary.Diagnostics...)
	if len(rows) == 0 {
		gaps = append(gaps, "no_records_not_measured_zero")
	}
	if view.Summary.CollectionStatus != "complete" {
		gaps = append(gaps, "collection_incomplete_total_unknown")
	}
	if view.Summary.PricingStatus != "priced" {
		gaps = append(gaps, "historical_prices_incomplete_total_unknown")
	}
	type rateKey struct {
		revision     int64
		agent, model string
	}
	referenced := map[rateKey][]store.MeterHistoricalRate{}
	for _, rate := range rates {
		k := rateKey{rate.RevisionID, rate.CodingAgent, rate.Model}
		referenced[k] = append(referenced[k], rate)
	}
	items := []map[string]any{}
	for _, row := range rows {
		if row.ActualModel == nil {
			gaps = appendUnique(gaps, "model_unknown")
		}
	}
	switch r.View {
	case "details":
		for _, row := range rows {
			// Deliberately omit invocation IDs, raw source identifiers and paths.
			items = append(items, map[string]any{"repository": row.Project, "issue": row.Issue, "run": row.Run, "execution_path": row.ExecutionPath, "attempt_id": row.AttemptID, "attempt_started_at": row.AttemptStartedAt, "attempt_finished_at": row.AttemptFinishedAt, "row_kind": row.RowKind, "mode": row.Mode, "agent": row.CodingAgent, "model": row.ActualModel, "model_provenance": row.ModelProvenance, "tokens": row.Tokens, "estimated_usd": row.EstimatedUSD, "priced_subtotal_usd": row.PricedSubtotalUSD, "collection_status": row.CollectionStatus, "pricing_status": row.PricingStatus, "price_revision_id": row.PriceRevisionID, "measurement_id": row.MeasurementID, "measurement_revision": row.MeasurementRevision, "diagnostic_codes": row.Diagnostics, "provider_reported_zero_cost_refusal": row.ProviderReportedZeroCostRefusal, "price_basis": referenced[rateKey{row.PriceRevisionID, row.CodingAgent, meterview.Amount(row.ActualModel)}]})
		}
	case "breakdown":
		b, e := meterview.BreakdownFromRows(rows, r.By)
		if e != nil {
			return out, e
		}
		for _, row := range b.Rows {
			items = append(items, map[string]any{"repository": row.Project, "issue": row.Issue, "run": row.Run, "mode": row.Mode, "agent": row.Agent, "model": row.Model, "label": row.Label(r.By), "totals": row.Summary, "records": row.Records})
		}
	}
	if len(rates) > 50 {
		gaps = append(gaps, "historical_rate_preview_truncated_use_details")
	}
	facts["gaps"] = gaps
	// Revision covers the full source basis and selection before any redaction.
	revisionBytes, e := json.Marshal(struct {
		Members []int64
		Rows    []store.MeterRow
		Rates   []store.MeterHistoricalRate
		Filter  store.MeterFilter
		View    string
		By      []string
	}{members, rows, rates, r.Filter, r.View, r.By})
	if e != nil {
		return out, e
	}
	hash := sha256.Sum256(revisionBytes)
	safe, e := s.cleanJSON(ctx, map[string]any{"facts": facts, "items": items})
	if e != nil {
		return out, e
	}
	var ok bool
	out.Facts, ok = safe["facts"].(map[string]any)
	if !ok {
		return connector.UsageProjection{}, errors.New("usage facts unavailable")
	}
	safeItems, ok := safe["items"].([]any)
	if !ok {
		return connector.UsageProjection{}, errors.New("usage items unavailable")
	}
	for _, item := range safeItems {
		value, ok := item.(map[string]any)
		if !ok {
			return connector.UsageProjection{}, errors.New("usage item unavailable")
		}
		out.Items = append(out.Items, value)
	}
	if out.Items == nil {
		out.Items = []map[string]any{}
	}
	if policy != s.PolicyID() {
		return connector.UsageProjection{}, errors.New("usage redaction policy changed")
	}
	out.Policy = policy
	out.Revision = hex.EncodeToString(hash[:]) + ":" + out.Policy
	if ctx.Err() != nil {
		return connector.UsageProjection{}, ctx.Err()
	}
	return out, nil
}
func appendUnique(v []string, s string) []string {
	for _, x := range v {
		if x == s {
			return v
		}
	}
	return append(v, s)
}
func (s *Service) cleanJSON(ctx context.Context, v any) (map[string]any, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return nil, e
	}
	var x any
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if e = d.Decode(&x); e != nil {
		return nil, e
	}
	x, e = s.clean(ctx, x)
	if e != nil {
		return nil, e
	}
	out, ok := x.(map[string]any)
	if !ok {
		return nil, errors.New("usage projection unavailable")
	}
	return out, nil
}
func (s *Service) clean(ctx context.Context, v any) (any, error) { return s.cleanValue(ctx, v, 0) }
func (s *Service) cleanValue(ctx context.Context, v any, depth int) (any, error) {
	if depth > 32 {
		return nil, errors.New("usage field nesting exceeds budget")
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	switch x := v.(type) {
	case string:
		// Reject encoded host paths as a whole field, then redact the original
		// display. Decoding cannot turn an untrusted field into a trusted link.
		decoded := x
		for i := 0; i < 8; i++ {
			next, e := url.QueryUnescape(html.UnescapeString(decoded))
			if e != nil || next == decoded {
				break
			}
			decoded = next
		}
		if strings.Contains(decoded, `\u`) {
			var inner string
			if json.Unmarshal([]byte(`"`+decoded+`"`), &inner) == nil {
				decoded = inner
			}
		}
		if report.StripHostPaths(decoded, s.roots) != decoded {
			return "[host path]", nil
		}

		if decoded != x {
			safe, e := s.redactor.ApplyReviewJSONDisplay(ctx, decoded, 512<<10)
			if e != nil || !safe.Valid() || safe.Truncated() {
				return nil, errors.New("usage redaction failed")
			}
			if safe.String() != decoded {
				return "[redacted]", nil
			}
		}
		if strings.HasPrefix(strings.TrimSpace(x), "{") || strings.HasPrefix(strings.TrimSpace(x), "[") || strings.HasPrefix(strings.TrimSpace(x), "\"") {
			var inner any
			decoder := json.NewDecoder(strings.NewReader(x))
			decoder.UseNumber()
			if json.Valid([]byte(x)) && decoder.Decode(&inner) == nil {
				safe, e := s.cleanValue(ctx, inner, depth+1)
				if e != nil {
					return nil, e
				}
				encoded, e := json.Marshal(safe)
				if e != nil {
					return nil, e
				}
				x = string(encoded)
			}
		}
		b, e := s.redactor.ApplyReviewJSONDisplay(ctx, report.StripHostPaths(x, s.roots), 512<<10)
		if e != nil || !b.Valid() || b.Truncated() {
			return nil, errors.New("usage redaction failed")
		}
		return b.String(), nil
	case map[string]any:
		out := map[string]any{}
		for k, v := range x {
			clean, e := s.cleanValue(ctx, v, depth+1)
			if e != nil {
				return nil, e
			}
			key, e := s.cleanValue(ctx, k, depth+1)
			if e != nil {
				return nil, e
			}
			name, ok := key.(string)
			if !ok {
				return nil, errors.New("invalid usage field")
			}
			if _, exists := out[name]; exists {
				return nil, errors.New("redacted usage key collision")
			}
			out[name] = clean
		}
		return out, nil
	case []any:
		out := make([]any, len(x))
		for i, v := range x {
			clean, e := s.cleanValue(ctx, v, depth+1)
			if e != nil {
				return nil, e
			}
			out[i] = clean
		}
		return out, nil
	case nil, bool, json.Number:
		return x, nil
	default:
		return nil, fmt.Errorf("unsupported usage field")
	}
}
