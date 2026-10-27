package usageview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/connector"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestCleanUsageJSONRedactsSecretsAndHostPaths(t *testing.T) {
	t.Parallel()
	secret := "private-usage-secret-value-12345"
	service := New(nil, redact.New(redact.Options{Environ: []string{"USAGE_SECRET=" + secret}}))
	for _, value := range []string{
		secret, url.QueryEscape(secret), html.EscapeString(secret), `{"nested":"` + secret + `"}`, `{"` + secret + `":"value"}`, `{"\u002fUsers\u002fprivate-marker\u002fsource.jsonl":"value"}`, "/Users/private-marker/source.jsonl", url.QueryEscape("/Users/private-marker/source.jsonl"), url.QueryEscape(url.QueryEscape("/Users/private-marker/source.jsonl")), `\u002fUsers\u002fprivate-marker\u002fsource.jsonl`, "<pre>&#47;Users&#47;private-marker&#47;source.jsonl</pre>", "&#47;Users&#47;private-marker&#47;source.jsonl",
	} {
		safe, err := service.cleanJSON(t.Context(), map[string]any{"label": value, "nested": []any{map[string]any{"amount": "0.000000000000", "unknown": nil, "zero": int64(0), "known": false}}})
		if err != nil {
			t.Fatalf("clean %q: %v", value, err)
		}
		encoded, err := json.Marshal(safe)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), secret) || strings.Contains(string(encoded), "private-marker") {
			t.Fatalf("private field survived: %s", encoded)
		}
		nested := usageAssert[map[string]any](t, usageAssert[[]any](t, safe["nested"])[0])
		if nested["amount"] != "0.000000000000" || nested["unknown"] != nil || nested["zero"] != json.Number("0") || nested["known"] != false {
			t.Fatalf("redaction changed measured zero or uncertainty: %v", nested)
		}
	}
}

type boundedUsageReader struct {
	db      *store.DB
	options store.MeterSnapshotOptions
	err     error
	calls   int
}

func (r *boundedUsageReader) NewMeterSnapshot(ctx context.Context, f store.MeterFilter, o store.MeterSnapshotOptions) (*store.MeterSnapshot, error) {
	r.calls++
	r.options = o
	if r.err != nil {
		return nil, r.err
	}
	return r.db.NewMeterSnapshot(ctx, f, o)
}

func TestRecordedUsageSanitizedUnknownAndBoundedAcquisition(t *testing.T) {
	t.Parallel()
	db := usageTestDB(t)
	ctx := t.Context()
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: filepath.Join(t.TempDir(), "repo"), Forge: "github", Owner: "owner", Name: "usage", BaseBranch: "main", FeaturePrefix: "lf"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(ctx, repo, 31)
	if err != nil {
		t.Fatal(err)
	}
	segment, err := db.GetOrCreateSegment(ctx, session.ID, store.FirstSegmentOrdinal, "segment", store.AgentCodex)
	if err != nil {
		t.Fatal(err)
	}
	run, err := db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: store.ModeSpike, Agent: store.AgentCodex, TriggerCommentID: 1, LogPath: func(int) string { return "/Users/private-marker/raw-transcript.jsonl" }})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := db.StartMeterAttempt(ctx, run.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	const source = "private-source-invocation-marker"
	if err = db.PutMeterInvocation(ctx, store.MeterInvocation{AttemptID: attempt.ID, ID: source, SourceID: source, SourceInvocationID: source, Status: "partial"}); err != nil {
		t.Fatal(err)
	}
	zero := int64(0)
	observation := agent.UsageObservation{AttemptID: attempt.ID, SourceID: source, EventKey: "one", InvocationID: source, Mode: agent.CounterIncremental, Scope: agent.UsageExclusive, Tokens: agent.TokenUsage{Output: &zero}, Coverage: agent.UsageCoverage{Status: agent.CollectionPartial}}
	if err = db.RecordMeterEvent(ctx, attempt.ID, "codex", agent.UsageEvent{Observation: &observation}); err != nil {
		t.Fatal(err)
	}
	reader := &boundedUsageReader{db: db}
	service := New(reader, redact.New(redact.Options{Environ: []string{}}))
	projection, err := service.Usage(ctx, connector.UsageRequest{Filter: store.MeterFilter{}, View: "details"})
	if err != nil {
		t.Fatal(err)
	}
	if reader.options.MaxRows != 10000 || reader.options.MaxBytes != 16<<20 || !reader.options.IncludeHistoricalRates {
		t.Fatalf("unbounded acquisition: %+v", reader.options)
	}
	encoded, err := json.Marshal(projection)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{source, "private-marker", "source_invocation_id", "invocation_id", "transcript", "log_path"} {
		if strings.Contains(string(encoded), private) {
			t.Fatalf("raw source field escaped: %s", encoded)
		}
	}
	totals := usageAssert[map[string]any](t, projection.Facts["totals"])
	if totals["estimated_usd"] != nil || totals["priced_subtotal_usd"] != "0.000000000000" {
		t.Fatalf("unknown total or known zero subtotal lost: %v", totals)
	}
	known := usageAssert[map[string]any](t, totals["known_token_subtotals"])
	full := usageAssert[map[string]any](t, totals["tokens"])
	if known["output"] != json.Number("0") || full["output"] != nil {
		t.Fatalf("measured zero was confused with a complete total: %v", totals)
	}
	gaps := usageAssert[[]any](t, projection.Facts["gaps"])
	for _, want := range []string{"model_unknown", "collection_incomplete_total_unknown", "historical_prices_incomplete_total_unknown"} {
		found := false
		for _, gap := range gaps {
			if gap == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing visible gap %q: %v", want, gaps)
		}
	}
	// A subsequent snapshot proves the successful read released its temporary data.
	snapshot, err := db.NewMeterSnapshot(ctx, store.MeterFilter{}, store.MeterSnapshotOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err = snapshot.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestRecordedUsageAcquisitionFailureAndCancellationWithholdFacts(t *testing.T) {
	t.Parallel()
	boom := errors.New("source unavailable")
	reader := &boundedUsageReader{err: boom}
	service := New(reader, redact.New(redact.Options{Environ: []string{}}))
	out, err := service.Usage(t.Context(), connector.UsageRequest{View: "details"})
	if !errors.Is(err, boom) || out.Facts != nil || out.Items != nil || out.Revision != "" {
		t.Fatalf("acquisition failure returned evidence: %+v %v", out, err)
	}
	service = New(usageTestDB(t), redact.New(redact.Options{Environ: []string{}}))
	empty, err := service.Usage(t.Context(), connector.UsageRequest{View: "details"})
	if err != nil {
		t.Fatal(err)
	}
	totals := usageAssert[map[string]any](t, empty.Facts["totals"])
	if totals["estimated_usd"] != nil || totals["priced_subtotal_usd"] != nil || len(empty.Items) != 0 {
		t.Fatalf("absence fabricated measured zero: %+v", empty)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	out, err = service.Usage(ctx, connector.UsageRequest{View: "details", Empty: true})
	if !errors.Is(err, context.Canceled) || out.Facts != nil || out.Items != nil || out.Revision != "" {
		t.Fatalf("cancelled read returned evidence: %+v %v", out, err)
	}
}

func usageTestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestRecordedUsageRatePreviewKeepsCompleteDetailBasis(t *testing.T) {
	t.Parallel()
	db := usageTestDB(t)
	ctx := t.Context()
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: filepath.Join(t.TempDir(), "repo"), Forge: "github", Owner: "owner", Name: "basis", BaseBranch: "main", FeaturePrefix: "lf"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(ctx, repo, 2)
	if err != nil {
		t.Fatal(err)
	}
	segment, err := db.GetOrCreateSegment(ctx, session.ID, store.FirstSegmentOrdinal, "segment", store.AgentCodex)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 14; i++ {
		model := fmt.Sprintf("model-%02d", i)
		_, err = db.SetMeterPrices(ctx, "codex", model, []store.MeterPriceRate{{CodingAgent: "codex", Model: model, Dimension: "input_uncached", USDPerMillion: "1"}, {CodingAgent: "codex", Model: model, Dimension: "output", USDPerMillion: "2"}, {CodingAgent: "codex", Model: model, Dimension: "cache_read", USDPerMillion: "0.1"}, {CodingAgent: "codex", Model: model, Dimension: "cache_write", USDPerMillion: "1"}})
		if err != nil {
			t.Fatal(err)
		}
	}
	run, err := db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: store.ModeSpike, Agent: store.AgentCodex, TriggerCommentID: 1, LogPath: func(int) string { return "fixture" }})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := db.StartMeterAttempt(ctx, run.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.PutMeterInvocation(ctx, store.MeterInvocation{AttemptID: attempt.ID, ID: "root", SourceID: "root", SourceInvocationID: "root", Status: "complete"}); err != nil {
		t.Fatal(err)
	}
	zero, one := int64(0), int64(1)
	for i := 0; i < 14; i++ {
		model := fmt.Sprintf("model-%02d", i)
		o := agent.UsageObservation{AttemptID: attempt.ID, SourceID: "fixture", EventKey: model, InvocationID: "root", ActualModel: &model, ModelProvenance: "source", Mode: agent.CounterIncremental, Scope: agent.UsageExclusive, Tokens: agent.TokenUsage{InputUncached: &zero, Output: &one, CacheRead: &zero, CacheWrite: &zero}, Coverage: agent.UsageCoverage{Status: agent.CollectionComplete}}
		if err = db.RecordMeterEvent(ctx, attempt.ID, "codex", agent.UsageEvent{Observation: &o}); err != nil {
			t.Fatal(err)
		}
	}
	service := New(db, redact.New(redact.Options{Environ: []string{}}))
	p, err := service.Usage(ctx, connector.UsageRequest{View: "details"})
	if err != nil {
		t.Fatal(err)
	}
	if len(usageAssert[[]any](t, p.Facts["price_basis"])) != 50 || p.Facts["price_basis_count"] != json.Number("56") || p.Facts["price_basis_has_more"] != true {
		t.Fatalf("unbounded or incomplete rate preview: %+v", p.Facts)
	}
	if len(p.Items) != 14 {
		t.Fatalf("lost measurement details: %d", len(p.Items))
	}
	models := map[string]bool{}
	for _, item := range p.Items {
		basis := usageAssert[[]any](t, item["price_basis"])
		if len(basis) != 4 {
			t.Fatalf("missing detail price basis: %v", item)
		}
		model := usageAssert[string](t, item["model"])
		models[model] = true
		for _, rate := range basis {
			if usageAssert[map[string]any](t, rate)["model"] != model {
				t.Fatal("detail exposed unrelated rate")
			}
		}
	}
	if len(models) != 14 {
		t.Fatal("lost historical model basis")
	}
}

func usageAssert[T any](t *testing.T, value any) T {
	t.Helper()
	typed, ok := value.(T)
	if !ok {
		t.Fatalf("unexpected projection field type %T", value)
	}
	return typed
}
