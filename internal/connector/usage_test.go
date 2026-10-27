package connector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/store"
)

type usageTestProvider struct {
	policy string
	call   func(context.Context, UsageRequest) (UsageProjection, error)
}

func (p *usageTestProvider) Usage(ctx context.Context, r UsageRequest) (UsageProjection, error) {
	return p.call(ctx, r)
}
func (p *usageTestProvider) PolicyID() string {
	if p.policy != "" {
		return p.policy
	}
	return "usage-test-policy"
}

func usageTestProjection(n int) UsageProjection {
	items := make([]map[string]any, n)
	for i := range items {
		items[i] = map[string]any{"ordinal": i, "model": "recorded-model"}
	}
	return UsageProjection{Revision: "recorded-revision", Policy: "usage-test-policy", Facts: map[string]any{"records": n, "totals": map[string]any{"estimated_usd": "1.23456789", "priced_subtotal_usd": "1.23456789", "collection_status": "complete", "pricing_status": "priced"}, "price_basis": []any{}, "gaps": []any{}}, Items: items}
}

func usageTestServer(t *testing.T, p UsageProvider) (*connectorFixture, *Server, string, string) {
	t.Helper()
	f := newConnectorFixture(t, nil)
	f.options.Usage = p
	s, err := New(t.Context(), f.options)
	if err != nil {
		t.Fatal(err)
	}
	client := registerConnectorClient(t, s.Handler(), "usage boundary", "none")
	token, id := issueDiscussionTokens(t, f, client, store.ConnectorGrantTarget{}, FactoryUsageScope)
	return f, s, token, id
}
func usageTestArgs(t *testing.T, raw string) usageArguments {
	t.Helper()
	a, err := parseUsageArguments(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func usageTestEvidence(t *testing.T, r ToolResult) map[string]any {
	t.Helper()
	var v struct {
		Evidence map[string]any `json:"evidence"`
	}
	if err := json.Unmarshal(r.StructuredContent, &v); err != nil {
		t.Fatal(err)
	}
	return v.Evidence
}
func usageTestRead(t *testing.T, s *Server, token string, a usageArguments) map[string]any {
	t.Helper()
	r, err := s.readUsage(t.Context(), token, a)
	if err != nil {
		t.Fatal(err)
	}
	return usageTestEvidence(t, r)
}

func TestUsagePeriodsResolveExactUTCBounds(t *testing.T) {
	observed := time.Date(2026, 3, 29, 2, 30, 0, 123456789, time.FixedZone("offset", 2*60*60))
	utc := observed.UTC()
	cases := []struct {
		name, period, from, to string
		wantFrom, wantTo       *time.Time
	}{
		{name: "default", wantFrom: usageTimePointer(utc.Add(-720 * time.Hour)), wantTo: &utc},
		{name: "rolling thirty", period: "rolling_30_days", wantFrom: usageTimePointer(utc.Add(-720 * time.Hour)), wantTo: &utc},
		{name: "rolling seven across daylight saving", period: "rolling_7_days", wantFrom: usageTimePointer(utc.Add(-168 * time.Hour)), wantTo: &utc},
		{name: "today", period: "today", wantFrom: usageTimePointer(time.Date(2026, 3, 29, 0, 0, 0, 0, time.UTC)), wantTo: &utc},
		{name: "all history", period: "all_history"},
		{name: "explicit timezone", from: "2026-03-01T00:00:00+02:00", to: "2026-03-02T00:00:00-03:00", wantFrom: usageTimePointer(time.Date(2026, 2, 28, 22, 0, 0, 0, time.UTC)), wantTo: usageTimePointer(time.Date(2026, 3, 2, 3, 0, 0, 0, time.UTC))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := usageArguments{Scope: "factory", Period: tc.period, From: tc.from, To: tc.to}
			f, b, err := resolveUsagePeriod(a, observed)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(f.TimeFrom, tc.wantFrom) || !reflect.DeepEqual(f.TimeTo, tc.wantTo) || b.Zone != "UTC" || !b.FromInclusive || !b.ToExclusive {
				t.Fatalf("bounds %+v, filter %+v", b, f)
			}
		})
	}
	midnight := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	f, b, err := resolveUsagePeriod(usageArguments{Scope: "factory", Period: "today"}, midnight)
	if err != nil || f.TimeFrom == nil || f.TimeTo == nil || !f.TimeFrom.Equal(*f.TimeTo) || !b.From.Equal(midnight) {
		t.Fatalf("valid named empty window %+v %v", b, err)
	}
}
func usageTimePointer(t time.Time) *time.Time { return &t }

func TestUsageArgumentsRejectAmbiguousOrUntrustedSelectors(t *testing.T) {
	for _, raw := range []string{
		`{"scope":"factory","scope":"task"}`, `{"scope":"factory","period":null}`, `{"scope":"factory","period":"today","from":"2026-01-01T00:00:00Z"}`,
		`{"scope":"factory","period":"yesterday"}`, `{"scope":"factory","repository":"github:acme/api"}`, `{"scope":"repository","repository":"/host/private"}`,
		`{"scope":"task","repository":"github:acme/api"}`, `{"scope":"factory","limit":0}`, `{"scope":"factory","limit":51}`, `{"scope":"factory","unknown":true}`,
		`{"scope":"factory","from":"2026-01-01T00:00:00","to":"2026-01-02T00:00:00Z"}`, `{"scope":"factory","from":"2026-01-01T00:00:00Z","to":"2026-01-01T00:00:00Z"}`,
		`{"scope":"factory","from":"2026-01-02T00:00:00Z","to":"2026-01-01T00:00:00Z"}`, `{"scope":"factory","view":"details","by":["run"]}`,
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := parseUsageArguments(json.RawMessage(raw)); err == nil {
				t.Fatal("invalid argument accepted")
			}
		})
	}
}

func TestUsageBoundaryDeniesBeforeProviderAndWithholdsRevokedOutput(t *testing.T) {
	calls := 0
	p := &usageTestProvider{call: func(context.Context, UsageRequest) (UsageProjection, error) {
		calls++
		return usageTestProjection(1), nil
	}}
	f, s, token, id := usageTestServer(t, p)
	a := usageTestArgs(t, `{"scope":"repository","repository":"github:acme/api"}`)
	if _, err := s.readUsage(t.Context(), token, a); !errors.Is(err, store.ErrConnectorScope) || calls != 0 {
		t.Fatalf("pre-query scope denial calls=%d err=%v", calls, err)
	}
	a = usageTestArgs(t, `{"scope":"factory","view":"details","limit":1}`)
	p.call = func(ctx context.Context, r UsageRequest) (UsageProjection, error) {
		calls++
		if err := f.db.RevokeConnectorGrant(ctx, 42, id, f.now); err != nil {
			return UsageProjection{}, err
		}
		return usageTestProjection(4), nil
	}
	result, err := s.readUsage(t.Context(), token, a)
	if err == nil || len(result.StructuredContent) != 0 || calls != 1 {
		t.Fatalf("revoked evidence escaped %+v %v calls=%d", result, err, calls)
	}
	if len(s.usageViews.views) != 0 {
		t.Fatal("revoked construction retained a view")
	}
}

func TestUsagePagingRetainsSnapshotBoundsAndLiveReplay(t *testing.T) {
	calls := 0
	projection := usageTestProjection(4)
	p := &usageTestProvider{call: func(context.Context, UsageRequest) (UsageProjection, error) { calls++; return projection, nil }}
	f, s, token, _ := usageTestServer(t, p)
	a := usageTestArgs(t, `{"scope":"factory","view":"details","limit":1,"period":"today"}`)
	first := usageTestRead(t, s, token, a)
	a.Cursor = usageTestValue[string](t, first["next_cursor"])
	projection.Items[1]["model"] = "changed after capture"
	usageTestValue[map[string]any](t, projection.Facts["totals"])["estimated_usd"] = "999"
	f.now = f.now.Add(time.Minute)
	second := usageTestRead(t, s, token, a)
	replayed := usageTestRead(t, s, token, a)
	for _, key := range []string{"bounds", "totals", "revision", "captured_at"} {
		if !reflect.DeepEqual(first[key], second[key]) {
			t.Fatalf("snapshot %s changed: first=%#v second=%#v", key, first[key], second[key])
		}
	}
	for _, key := range []string{"bounds", "totals", "revision", "items", "has_more"} {
		if !reflect.DeepEqual(second[key], replayed[key]) {
			t.Fatalf("replay %s changed", key)
		}
	}
	if usageTestValue[map[string]any](t, usageTestValue[[]any](t, second["items"])[0])["model"] != "recorded-model" || calls != 1 {
		t.Fatalf("reloaded mutable projection calls=%d", calls)
	}
	altered := a
	altered.Period = "all_history"
	if _, err := s.readUsage(t.Context(), token, altered); err == nil {
		t.Fatal("cursor accepted changed filters")
	}
	client := registerConnectorClient(t, s.Handler(), "second owner grant", "none")
	other, _ := issueDiscussionTokens(t, f, client, store.ConnectorGrantTarget{}, FactoryUsageScope)
	if _, err := s.readUsage(t.Context(), other, a); err == nil {
		t.Fatal("cursor accepted another grant")
	}
	tampered := a
	tampered.Cursor = strings.Repeat("x", len(a.Cursor))
	if _, err := s.readUsage(t.Context(), token, tampered); err == nil {
		t.Fatal("tampered cursor accepted")
	}
	a.Cursor = usageTestValue[string](t, second["next_cursor"])
	third := usageTestRead(t, s, token, a)
	a.Cursor = usageTestValue[string](t, third["next_cursor"])
	last := usageTestRead(t, s, token, a)
	if last["has_more"] != false || len(s.usageViews.views) != 0 {
		t.Fatal("final page did not retire view")
	}
	if _, err := s.readUsage(t.Context(), token, a); err == nil {
		t.Fatal("retired cursor replay accepted")
	}
}

func TestUsageQuestionsReleaseCompleteViewsAndReplaceAbandonedOnes(t *testing.T) {
	p := &usageTestProvider{call: func(context.Context, UsageRequest) (UsageProjection, error) { return usageTestProjection(4), nil }}
	_, s, token, _ := usageTestServer(t, p)
	for _, period := range []string{"today", "rolling_7_days", "rolling_30_days"} {
		a := usageTestArgs(t, fmt.Sprintf(`{"scope":"factory","period":%q}`, period))
		usageTestRead(t, s, token, a)
		if len(s.usageViews.views) != 0 {
			t.Fatal("complete summary retained capacity")
		}
	}
	a := usageTestArgs(t, `{"scope":"factory","view":"details","limit":1}`)
	cursors := []string{}
	for i := 0; i < 3; i++ {
		first := usageTestRead(t, s, token, a)
		cursors = append(cursors, usageTestValue[string](t, first["next_cursor"]))
	}
	if len(s.usageViews.views) != 2 {
		t.Fatalf("view count %d", len(s.usageViews.views))
	}
	a.Cursor = cursors[0]
	if _, err := s.readUsage(t.Context(), token, a); err == nil {
		t.Fatal("oldest abandoned view survived replacement")
	}
	a.Cursor = cursors[1]
	usageTestRead(t, s, token, a)
}

func TestUsageFailedAndCancelledConstructionReleasesCapacity(t *testing.T) {
	p := &usageTestProvider{call: func(context.Context, UsageRequest) (UsageProjection, error) {
		return UsageProjection{}, errors.New("snapshot unavailable")
	}}
	_, s, token, _ := usageTestServer(t, p)
	a := usageTestArgs(t, `{"scope":"factory","view":"details","limit":1}`)
	for i := 0; i < 5; i++ {
		if _, err := s.readUsage(t.Context(), token, a); err == nil {
			t.Fatal("failed provider accepted")
		}
	}
	if len(s.usageViews.views) != 0 {
		t.Fatal("failed construction retained evidence")
	}
	ctx, cancel := context.WithCancel(t.Context())
	p.call = func(context.Context, UsageRequest) (UsageProjection, error) {
		cancel()
		return usageTestProjection(4), nil
	}
	if result, err := s.readUsage(ctx, token, a); err == nil || len(result.StructuredContent) != 0 {
		t.Fatal("cancelled evidence released")
	}
	p.call = func(context.Context, UsageRequest) (UsageProjection, error) { return usageTestProjection(4), nil }
	usageTestRead(t, s, token, a)
}

func TestUsageCacheBoundsConstructionReservations(t *testing.T) {
	var cache usageCache
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := cache.reserve("one", now); err != nil {
		t.Fatal(err)
	}
	if err := cache.reserve("one", now); err != nil {
		t.Fatal(err)
	}
	if err := cache.reserve("one", now); err == nil {
		cache.release("one")
		t.Fatal("third concurrent construction for one grant accepted")
	}
	if err := cache.reserve("two", now); err != nil {
		t.Fatal(err)
	}
	if err := cache.reserve("two", now); err != nil {
		t.Fatal(err)
	}
	if err := cache.reserve("three", now); err == nil {
		cache.release("three")
		t.Fatal("fifth global construction accepted")
	}
	for _, grant := range []string{"one", "one", "two", "two"} {
		cache.release(grant)
	}
	if err := cache.reserve("one", now); err != nil {
		t.Fatalf("released reservation unavailable %v", err)
	}
	cache.release("one")
}

func TestUsageNamedMidnightPassesEmptyWindowWithoutWidening(t *testing.T) {
	f := newConnectorFixture(t, nil)
	f.now = time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)
	calls := 0
	f.options.Usage = &usageTestProvider{call: func(_ context.Context, r UsageRequest) (UsageProjection, error) {
		calls++
		if !r.Empty || r.Filter.TimeFrom == nil || r.Filter.TimeTo == nil || !r.Filter.TimeFrom.Equal(f.now) || !r.Filter.TimeTo.Equal(f.now) {
			t.Fatalf("midnight window widened %+v", r)
		}
		return usageTestProjection(0), nil
	}}
	s, err := New(t.Context(), f.options)
	if err != nil {
		t.Fatal(err)
	}
	client := registerConnectorClient(t, s.Handler(), "midnight question", "none")
	token, _ := issueDiscussionTokens(t, f, client, store.ConnectorGrantTarget{}, FactoryUsageScope)
	a := usageTestArgs(t, `{"scope":"factory","period":"today"}`)
	evidence := usageTestRead(t, s, token, a)
	if evidence["records"] != float64(0) || calls != 1 {
		t.Fatalf("empty period evidence %+v calls=%d", evidence, calls)
	}
}

func TestUsageContinuationExpiryAndRevocationWithholdEvidence(t *testing.T) {
	for _, scenario := range []string{"expiry", "revocation"} {
		t.Run(scenario, func(t *testing.T) {
			p := &usageTestProvider{call: func(context.Context, UsageRequest) (UsageProjection, error) { return usageTestProjection(4), nil }}
			f, s, token, id := usageTestServer(t, p)
			a := usageTestArgs(t, `{"scope":"factory","view":"details","limit":1}`)
			first := usageTestRead(t, s, token, a)
			a.Cursor = usageTestValue[string](t, first["next_cursor"])
			if scenario == "expiry" {
				f.now = f.now.Add(6 * time.Minute)
			} else if err := f.db.RevokeConnectorGrant(t.Context(), 42, id, f.now); err != nil {
				t.Fatal(err)
			}
			if result, err := s.readUsage(t.Context(), token, a); err == nil || len(result.StructuredContent) != 0 {
				t.Fatalf("%s evidence released %+v %v", scenario, result, err)
			}
		})
	}
}

func TestUsageContinuationInvalidatesChangedRedactionPolicy(t *testing.T) {
	p := &usageTestProvider{call: func(context.Context, UsageRequest) (UsageProjection, error) { return usageTestProjection(4), nil }}
	_, s, token, _ := usageTestServer(t, p)
	a := usageTestArgs(t, `{"scope":"factory","view":"details","limit":1}`)
	first := usageTestRead(t, s, token, a)
	a.Cursor = usageTestValue[string](t, first["next_cursor"])
	p.policy = "updated-redaction-policy"
	if result, err := s.readUsage(t.Context(), token, a); err == nil || len(result.StructuredContent) != 0 {
		t.Fatalf("prior-policy evidence released %+v %v", result, err)
	}
	if len(s.usageViews.views) != 0 {
		t.Fatal("prior-policy view not retired")
	}
}

func usageTestValue[T any](t *testing.T, raw any) T {
	t.Helper()
	value, ok := raw.(T)
	if !ok {
		t.Fatalf("unexpected value type %T", raw)
	}
	return value
}

func TestUsageSerializedBudgetsAndNonadvancingPageRefusal(t *testing.T) {
	oversized := usageTestProjection(1)
	oversized.Items[0]["model"] = strings.Repeat("\"<漢", 30000)
	p := &usageTestProvider{call: func(context.Context, UsageRequest) (UsageProjection, error) { return oversized, nil }}
	_, s, token, _ := usageTestServer(t, p)
	args := usageTestArgs(t, `{"scope":"factory","view":"details"}`)
	if result, err := s.readUsage(t.Context(), token, args); err == nil || len(result.StructuredContent) > 0 || len(s.usageViews.views) != 0 {
		t.Fatal("oversized first unit returned an empty nonadvancing page")
	}
	// Count code points after clipping, and bound both serialized representations
	// plus an adversarial RPC identifier whose escapes expand during marshaling.
	result, err := DiscussionResult(strings.Repeat("漢", 1200), map[string]any{"items": []any{strings.Repeat("\"<", 500)}})
	if err != nil {
		t.Fatal(err)
	}
	var structured struct {
		Summary string `json:"summary"`
	}
	if err = json.Unmarshal(result.StructuredContent, &structured); err != nil {
		t.Fatal(err)
	}
	if utf8.RuneCountInString(structured.Summary) != 1000 {
		t.Fatal("summary code-point bound changed")
	}
	id, _ := json.Marshal(strings.Repeat("\u2028", 32000))
	for _, value := range []map[string]any{
		{"jsonrpc": "2.0", "id": json.RawMessage(id), "result": result},
		{"jsonrpc": "2.0", "id": json.RawMessage(id), "error": map[string]any{"code": -32602, "message": "Invalid params"}},
	} {
		w := httptest.NewRecorder()
		s.writeDiscussionValue(w, value)
		if w.Body.Len() > DiscussionMaxResponseBytes || !json.Valid(w.Body.Bytes()) {
			t.Fatal("final serialized usage envelope exceeded budget")
		}
	}
}
