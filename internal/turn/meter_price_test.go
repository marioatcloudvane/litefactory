package turn

import (
	"context"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestMeterPinnedPricesSurviveInFlightEdits(t *testing.T) {
	var f *fixture
	var ids []string
	var pinned int64
	rates := func(price string) []store.MeterPriceRate {
		var out []store.MeterPriceRate
		for _, dimension := range []string{"input_uncached", "output", "cache_read", "cache_write"} {
			out = append(out, store.MeterPriceRate{CodingAgent: "claude", Model: "model", Dimension: dimension, USDPerMillion: price})
		}
		return out
	}
	runner := newStubRunner(func(ctx context.Context, req harness.Request) (*harness.RunResult, error) {
		ids = append(ids, req.Usage.AttemptID)
		if len(ids) == 1 {
			if _, err := f.db.SetMeterPrices(ctx, "claude", "model", rates("2")); err != nil {
				t.Fatal(err)
			}
		}
		complete := agent.UsageCoverage{Status: agent.CollectionComplete}
		zero, n := int64(0), int64(100)
		model := "model"
		for _, v := range []agent.UsageInvocation{{AttemptID: req.Usage.AttemptID, ID: "root", SourceID: "s", SourceInvocationID: "root", Coverage: complete}, {AttemptID: req.Usage.AttemptID, ID: "child", ParentID: "root", SourceID: "s", SourceInvocationID: "child", Coverage: complete}} {
			if err := req.Usage.Emit(ctx, agent.UsageEvent{Invocation: &v}); err != nil {
				t.Fatal(err)
			}
			o := agent.UsageObservation{AttemptID: req.Usage.AttemptID, SourceID: "s", EventKey: v.ID, InvocationID: v.ID, ActualModel: &model, ModelProvenance: "source", Mode: agent.CounterIncremental, Scope: agent.UsageExclusive, Tokens: agent.TokenUsage{InputUncached: &n, Output: &zero, CacheRead: &zero, CacheWrite: &zero, CacheWrite5m: &zero, CacheWrite1h: &zero, Reasoning: &zero}, Coverage: complete}
			if err := req.Usage.Emit(ctx, agent.UsageEvent{Observation: &o}); err != nil {
				t.Fatal(err)
			}
		}
		result := succeeded(req, f.paths.Home())
		result.Usage = &complete
		return result, nil
	})
	f = newFixture(t, runner)
	var err error
	pinned, err = f.db.SetMeterPrices(t.Context(), "claude", "model", rates("1"))
	if err != nil {
		t.Fatal(err)
	}
	first := f.execute(f.request())
	if !first.Succeeded() {
		t.Fatal(first)
	}
	next := f.request()
	next.Trigger.CommentID++
	second := f.execute(next)
	if !second.Succeeded() {
		t.Fatal(second)
	}
	snapshot, err := f.db.NewMeterSnapshot(t.Context(), store.MeterFilter{}, store.MeterSnapshotOptions{})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := snapshot.Next(t.Context(), 256)
	if err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatal(rows)
	}
	for _, row := range rows {
		want := "0.0002"
		if row.AttemptID == ids[0] {
			want = "0.0001"
			if row.PriceRevisionID != pinned {
				t.Fatal("child repinned")
			}
		}
		if row.EstimatedUSD == nil || *row.EstimatedUSD != want {
			t.Fatalf("valuation %+v want %s", row, want)
		}
	}
}
