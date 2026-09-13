package cli

import (
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/meter"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

// Synthetic stored facts exercise output semantics, not provider certification.
func TestMeterPresentationCoverage(t *testing.T) {
	for _, tc := range []struct {
		name, status, want string
		measured, refusal  bool
	}{
		{"complete", "complete", "Complete local estimate USD: 1.25 at recorded rates", true, false},
		{"partial", "partial", "Observed lower bound USD: 1.250000000000 at recorded rates; total unknown", true, false},
		{"unknown", "unknown", "Local estimated USD: unknown; observed priced subtotal USD: unknown", false, false},
		{"refusal", "unknown", "Provider-reported zero-cost refusal (USD 0)", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newLogsHome(t)
			if _, err := config.WriteDefault(config.NewPaths(f.home)); err != nil {
				t.Fatal(err)
			}
			repo := f.repo("acme", "api")
			session := f.session(repo, 73)
			run := f.allocateRun(session, 1, time.Time{})
			if tc.measured {
				code, _, diag := f.run("meter", "prices", "set", "fixture-model", "--coding_agent", "claude", "--input", "1.25", "--output", "0", "--cache_read", "0", "--cache_write", "0")
				if code != 0 {
					t.Fatal(diag)
				}
			}
			a, err := f.db.StartMeterAttempt(t.Context(), run.ID, "claude")
			if err != nil {
				t.Fatal(err)
			}
			if tc.measured {
				if err = f.db.PutMeterInvocation(t.Context(), store.MeterInvocation{AttemptID: a.ID, ID: "root", SourceID: "source", SourceInvocationID: "root", Status: tc.status}); err != nil {
					t.Fatal(err)
				}
				n, zero := int64(1000000), int64(0)
				model := "fixture-model"
				tokens := store.MeterTokens{InputUncached: &n, CacheRead: &zero, CacheWrite: &zero, Output: &zero}
				o := store.MeterObservation{AttemptID: a.ID, SourceID: "source", EventKey: "one", InvocationID: "root", Semantics: "incremental/exclusive", CounterEpoch: "fresh", CounterKey: "input", ModelKey: `["fixture-model"]`, ActualModel: &model, ModelProvenance: "synthetic", Digest: strings.Repeat("a", 64), Status: tc.status, Tokens: tokens}
				if err = f.db.ApplyMeterObservation(t.Context(), o, func(store.MeterLedgerState) (store.MeterMutation, error) {
					v, err := meter.Value(agent.TokenUsage(tokens), []meter.PriceRate{{Dimension: "input_uncached", USDPerMillion: "1.25"}, {Dimension: "cache_read", USDPerMillion: "0"}, {Dimension: "cache_write", USDPerMillion: "0"}, {Dimension: "output", USDPerMillion: "0"}})
					if err != nil {
						return store.MeterMutation{}, err
					}
					frozen := store.MeterValuation{EstimatedUSD: v.EstimatedUSD, PricedSubtotalUSD: v.PricedSubtotalUSD, Status: v.Status}
					for _, d := range v.Dimensions {
						frozen.Dimensions = append(frozen.Dimensions, store.MeterValuationDimension{Dimension: d.Dimension, Quantity: d.Quantity, USDPerMillion: d.USDPerMillion, AmountUSD: d.AmountUSD, Status: d.Status})
					}
					return store.MeterMutation{Measurement: &tokens, Valuation: &frozen}, nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			var codes []string
			if tc.status != "complete" {
				codes = []string{"independent_fallback_diagnostic"}
			}
			outcome := "success"
			if tc.refusal {
				outcome = "failure"
			}
			if err = f.db.FinishMeterAttemptWithRefusal(t.Context(), a.ID, outcome, tc.status, "", "synthetic", codes, tc.refusal); err != nil {
				t.Fatal(err)
			}
			code, out, diagnostics := f.run("meter", "acme/api", "73", "--details")
			if code != 0 || !strings.Contains(out, tc.want) {
				t.Fatalf("output %d %s %s", code, out, diagnostics)
			}
			if tc.refusal && !strings.Contains(out, "Provider-reported zero-cost refusals: 1 attempts") {
				t.Fatal(out)
			}
			if tc.status != "complete" && !strings.Contains(out, "independent_fallback_diagnostic") {
				t.Fatal(out)
			}
			code, out, diagnostics = f.run("meter", "acme/api", "73", "--json", "--details")
			if code != 0 {
				t.Fatal(diagnostics)
			}
			var payload struct {
				Summary report.MeterSummary `json:"summary"`
				Details []store.MeterRow    `json:"details"`
			}
			if err = json.Unmarshal([]byte(out), &payload); err != nil {
				t.Fatal(err)
			}
			if (payload.Summary.EstimatedUSD != nil) != (tc.status == "complete") {
				t.Fatalf("estimate %+v", payload.Summary)
			}
			if (payload.Summary.PricedSubtotalUSD != nil) != tc.measured {
				t.Fatalf("subtotal %+v", payload.Summary)
			}
			if len(payload.Details) != 1 || payload.Details[0].ProviderReportedZeroCostRefusal != tc.refusal {
				t.Fatalf("details %+v", payload.Details)
			}
			if tc.refusal && (payload.Details[0].ActualModel != nil || payload.Details[0].PricingStatus != "unknown") {
				t.Fatalf("invented refusal pricing %+v", payload.Details[0])
			}
			code, out, diagnostics = f.run("meter", "export", "github:acme/api")
			if code != 0 {
				t.Fatal(diagnostics)
			}
			records, err := csv.NewReader(strings.NewReader(out)).ReadAll()
			if err != nil || len(records) != 2 {
				t.Fatalf("CSV %v %v", records, err)
			}
			if len(records[0]) != 26 || (records[1][20] != "") != (tc.status == "complete") || (records[1][21] != "") != tc.measured {
				t.Fatalf("CSV values %v", records)
			}
			if strings.Contains(records[1][24], "provider_reported_zero_cost_refusal") != tc.refusal {
				t.Fatalf("CSV refusal %v", records[1])
			}
		})
	}
}
