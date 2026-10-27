package meterview

import (
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestCapturedRowsMatchMeterAndRetainHistoricalRates(t *testing.T) {
	f := newMeterViewFixture(t)
	ctx := t.Context()
	// Retries are separate attempts. Exclusive child usage adds once; an
	// unknown model remains visible, even while selecting a known model.
	for _, model := range []string{"root-model", "root-model", "unpriced-model"} {
		a, err := f.db.StartMeterAttempt(ctx, f.run.ID, "claude")
		if err != nil {
			t.Fatal(err)
		}
		f.invocation(a.ID, "root", "")
		f.usage(a.ID, "root", "root-usage", model, 10, 2)
		f.invocation(a.ID, "child", "root")
		f.usage(a.ID, "child", "child-usage", "child-model", 3, 1)
		f.finish(a.ID, "success", agent.UsageCoverage{Status: agent.CollectionComplete})
	}
	expected, err := Build(ctx, f.db, store.MeterFilter{}, true)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := f.db.NewMeterSnapshot(ctx, store.MeterFilter{}, store.MeterSnapshotOptions{MaxRows: 10000, MaxBytes: 16 << 20, IncludeHistoricalRates: true})
	if err != nil {
		t.Fatal(err)
	}
	var rows []store.MeterRow
	for {
		page, err := snapshot.Next(ctx, 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		rows = append(rows, page...)
	}
	rates := snapshot.HistoricalRates()
	if err := snapshot.Close(ctx); err != nil {
		t.Fatal(err)
	}
	actual, err := BuildFromRows(rows, true)
	if err != nil {
		t.Fatal(err)
	}
	actual.AsOf, expected.AsOf = actual.AsOf.UTC(), actual.AsOf.UTC()
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("captured meter differs: got %+v want %+v", actual, expected)
	}
	if len(rows) != 6 || len(rates) != 8 || actual.Summary.EstimatedUSD != nil || actual.Summary.PricedSubtotalUSD == nil {
		t.Fatalf("unexpected capture rows=%d rates=%+v summary=%+v", len(rows), rates, actual.Summary)
	}
	for _, rate := range rates {
		if rate.USDPerMillion != "1" || rate.Currency != "USD" || rate.RoundingPolicy == "" {
			t.Fatalf("bad historical rate: %+v", rate)
		}
	}
	changed := []store.MeterPriceRate{{CodingAgent: "claude", Model: "root-model", Dimension: "input_uncached", USDPerMillion: "999"}, {CodingAgent: "claude", Model: "root-model", Dimension: "output", USDPerMillion: "999"}}
	if _, err := f.db.SetMeterPrices(ctx, "claude", "root-model", changed); err != nil {
		t.Fatal(err)
	}
	again, err := Build(ctx, f.db, store.MeterFilter{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual.Summary, again.Summary) {
		t.Fatalf("catalogue repriced historical work: %+v / %+v", actual.Summary, again.Summary)
	}
	second, err := f.db.NewMeterSnapshot(ctx, store.MeterFilter{}, store.MeterSnapshotOptions{IncludeHistoricalRates: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := second.Close(ctx); err != nil {
			t.Error(err)
		}
	}()
	if !reflect.DeepEqual(rates, second.HistoricalRates()) {
		t.Fatal("historical rates changed with current catalogue")
	}
	// A caller cannot mutate the snapshot's captured historical facts.
	rates[0].USDPerMillion = "tampered"
	if reflect.DeepEqual(rates, second.HistoricalRates()) {
		t.Fatal("historical rates alias caller memory")
	}
}

func TestBreakdownRunIdentityOrderingAndModelPresence(t *testing.T) {
	zero := int64(0)
	empty := ""
	rows := []store.MeterRow{
		{Project: "github:z/repo", Issue: 7, Run: 1, ActualModel: &empty},
		{Project: "github:a/repo", Issue: 7, Run: 1, ActualModel: &empty},
		{Project: "github:a/repo", Issue: 7, Run: 1, ActualModel: nil},
	}
	for i := range rows {
		rows[i].Tokens.InputUncached = &zero
		rows[i].CollectionStatus = "complete"
	}
	var previous Breakdown
	for n := 0; n < 10; n++ {
		b, err := BreakdownFromRows(rows, []string{"run", "model"})
		if err != nil {
			t.Fatal(err)
		}
		if len(b.Rows) != 3 || b.Rows[0].Project != "github:a/repo" || b.Rows[0].Model != nil || b.Rows[1].Model == nil || b.Rows[2].Project != "github:z/repo" {
			t.Fatalf("unstable identities: %+v", b.Rows)
		}
		if !strings.Contains(b.Rows[2].Label(b.By), "github:z/repo#7") {
			t.Fatal("run label drops repository")
		}
		if n > 0 && !reflect.DeepEqual(previous, b) {
			t.Fatal("breakdown order changed")
		}
		previous = b
		rows[0], rows[2] = rows[2], rows[0]
	}
}
