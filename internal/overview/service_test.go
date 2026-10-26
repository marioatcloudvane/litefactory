package overview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

type fakeReader struct {
	repos                                                 []store.OverviewRepository
	health                                                store.HealthSnapshot
	activity                                              []store.OverviewActivity
	waits                                                 store.OverviewWaits
	pubs                                                  []store.OverviewPublication
	failures                                              map[string]error
	activityMore, waitsMore, pubsMore, reposMore, undated bool
	from, to                                              time.Time
	cancel                                                context.CancelFunc
}

func (f *fakeReader) OverviewRepositories(ctx context.Context) ([]store.OverviewRepository, bool, error) {
	return f.repos, f.reposMore, f.failures["registry"]
}
func (f *fakeReader) OverviewHealth(ctx context.Context) (store.HealthSnapshot, error) {
	return f.health, f.failures["health"]
}
func (f *fakeReader) ListOverviewActivity(ctx context.Context) ([]store.OverviewActivity, bool, error) {
	return f.activity, f.activityMore, f.failures["activity"]
}
func (f *fakeReader) ListOverviewWaits(ctx context.Context) (store.OverviewWaits, error) {
	f.waits.HasMore = f.waitsMore
	return f.waits, f.failures["waits"]
}
func (f *fakeReader) ListOverviewPublications(ctx context.Context, from, to time.Time) (store.OverviewPublications, error) {
	f.from, f.to = from, to
	if f.cancel != nil {
		f.cancel()
	}
	return store.OverviewPublications{Entries: f.pubs, HasMore: f.pubsMore, Undated: f.undated}, f.failures["publications"]
}

func fixedService(f *fakeReader, now time.Time) *Service {
	return New(f, redact.New(redact.Options{Environ: []string{"OVERVIEW_API_TOKEN=hidden-overview-secret"}}), func() time.Time { return now })
}
func testNow() time.Time { return time.Date(2026, 10, 26, 12, 0, 0, 0, time.FixedZone("local", 3600)) }
func has(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func TestBuildHealthFreshnessAndUnknowns(t *testing.T) {
	now := testNow()
	for _, tc := range []struct {
		name         string
		found        bool
		written      time.Time
		want, reason string
	}{
		{"missing", false, time.Time{}, "unknown", "snapshot_missing"},
		{"unknown time", true, time.Time{}, "unknown", "unknown_snapshot_time"},
		{"future", true, now.Add(time.Second), "unknown", "unknown_snapshot_time"},
		{"five minutes", true, now.Add(-5 * time.Minute), "complete", ""},
		{"older", true, now.Add(-5*time.Minute - time.Nanosecond), "stale", "snapshot_older_than_five_minutes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeReader{health: store.HealthSnapshot{Found: tc.found, Listener: store.ListenerStatus{WrittenAt: tc.written}}}
			r, err := fixedService(f, now).Build(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			source := r.Coverage.Sources["health"]
			if source.Status != tc.want || r.Health["status"] != tc.want {
				t.Fatalf("health=%v source=%+v", r.Health, source)
			}
			if tc.reason != "" && !has(source.Reasons, tc.reason) {
				t.Fatalf("reason missing: %+v", source)
			}
			for _, key := range []string{"slots_used", "slots_total", "heartbeat_age_seconds"} {
				if r.Health[key] != nil {
					t.Fatalf("unknown %s=%v", key, r.Health[key])
				}
			}
		})
	}
}

func TestBuildWindowAndWaitAgeProvenance(t *testing.T) {
	now := testNow()
	since := now.Add(-30 * 24 * time.Hour)
	f := &fakeReader{undated: true, waits: store.OverviewWaits{Entries: []store.OverviewWait{{WaitID: "old", Since: &since, SinceProvenance: "confirmation", Summary: "An old unanswered question"}, {WaitID: "unknown"}}}}
	r, err := fixedService(f, now).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !f.to.Equal(now) || !f.from.Equal(now.Add(-168*time.Hour)) || r.GeneratedAt.Location() != time.UTC || r.Window.To != r.GeneratedAt {
		t.Fatalf("window=%+v query=%v..%v", r.Window, f.from, f.to)
	}
	if len(r.OwnerWaits.Items) != 2 || r.OwnerWaits.Items[0]["waiting_age_seconds"] != json.Number("2592000") || r.OwnerWaits.Items[0]["time_provenance"] != "confirmation" || r.OwnerWaits.Items[1]["waiting_age_seconds"] != nil {
		t.Fatalf("wait ages=%+v", r.OwnerWaits)
	}
	if !has(r.RecentPublications.Warnings, "undated_historical_confirmations_excluded") {
		t.Fatal("historical coverage warning missing")
	}
}

func TestBuildFailuresStayVisible(t *testing.T) {
	now := testNow()
	failure := errors.New("private diagnostic hidden-overview-secret")
	for _, failed := range []string{"registry", "health", "activity", "waits", "publications", "all"} {
		t.Run(failed, func(t *testing.T) {
			f := &fakeReader{failures: map[string]error{failed: failure}, activity: []store.OverviewActivity{{ID: "retained"}}}
			if failed == "all" {
				for _, name := range []string{"registry", "health", "activity", "waits", "publications"} {
					f.failures[name] = failure
				}
			}
			r, err := fixedService(f, now).Build(t.Context())
			if failed == "all" {
				if err == nil || strings.Contains(err.Error(), "private diagnostic") {
					t.Fatalf("all-source failure=%v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			source := r.Coverage.Sources[failed]
			if source.Status != "unavailable" || !has(source.Reasons, "source_read_failed") {
				t.Fatalf("failure looked complete: %+v", source)
			}
			if failed != "activity" && len(r.Activity.Items) != 1 {
				t.Fatal("valid activity lost")
			}
			raw, marshalErr := json.Marshal(r)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			if strings.Contains(string(raw), "private diagnostic") {
				t.Fatal("source diagnostic exposed")
			}
		})
	}
}

func TestOverviewIndependentEntryAndByteBounds(t *testing.T) {
	now := testNow()
	f := &fakeReader{}
	for i := 0; i < 60; i++ {
		f.activity = append(f.activity, store.OverviewActivity{ID: fmt.Sprint(i), Repository: strings.Repeat("a", 2048), TaskRef: strings.Repeat("b", 2048), Workflow: strings.Repeat("c", 2048), Stage: strings.Repeat("d", 2048)})
		f.waits.Entries = append(f.waits.Entries, store.OverviewWait{WaitID: fmt.Sprint(i), Summary: "Please answer", URL: "https://example.com/comment"})
		f.pubs = append(f.pubs, store.OverviewPublication{Key: fmt.Sprint(i), Summary: "Published", URL: "https://example.com/comment", ConfirmedAt: now})
	}
	result, err := fixedService(f, now).Overview(t.Context(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var r Result
	if err = json.Unmarshal(result.StructuredContent, &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Activity.Items) >= 50 || !r.Activity.HasMore || !r.Activity.Truncated || !has(r.Activity.Warnings, "byte_limit") {
		t.Fatalf("activity byte bounds=%+v", r.Activity)
	}
	for _, list := range []List{r.OwnerWaits, r.RecentPublications} {
		if len(list.Items) != 50 || !list.HasMore || !list.Truncated || !has(list.Warnings, "entry_limit") {
			t.Fatalf("independent entry bound=%+v", list)
		}
	}
	if len(result.Content) != 1 || result.Content[0].Text != string(result.StructuredContent) {
		t.Fatal("text and structured facts differ")
	}
	envelope, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if len(envelope) > 256*1024 {
		t.Fatalf("response bytes=%d", len(envelope))
	}
}

func TestBuildSummaryUTF8AndRedaction(t *testing.T) {
	f := &fakeReader{waits: store.OverviewWaits{Entries: []store.OverviewWait{{Summary: "hidden-overview-secret " + strings.Repeat("界", 300)}}}, pubs: []store.OverviewPublication{{Summary: "hidden-overview-secret " + strings.Repeat("界", 300), ConfirmedAt: testNow()}}}
	r, err := fixedService(f, testNow()).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []map[string]any{r.OwnerWaits.Items[0], r.RecentPublications.Items[0]} {
		summary, ok := item["summary"].(string)
		if !ok {
			t.Fatalf("summary type %T", item["summary"])
		}
		if len(summary) > 512 || !utf8.ValidString(summary) || strings.Contains(summary, "hidden-overview-secret") {
			t.Fatalf("unsafe summary %q", summary)
		}
	}
}

func TestOverviewCancellationAndExactInput(t *testing.T) {
	for _, args := range []string{"", `null`, `[]`, `{"repository":"private"}`, `1`, `{} {}`} {
		t.Run(args, func(t *testing.T) {
			if _, err := fixedService(&fakeReader{}, testNow()).Overview(t.Context(), json.RawMessage(args)); err == nil {
				t.Fatalf("accepted %q", args)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	f := &fakeReader{cancel: cancel}
	if _, err := fixedService(f, testNow()).Build(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}
}

func TestBuildRetainsHealthBlockersAndSourceTimes(t *testing.T) {
	now := testNow().UTC()
	resume := now.Add(2 * time.Minute)
	f := &fakeReader{reposMore: true, health: store.HealthSnapshot{Found: true, Listener: store.ListenerStatus{WrittenAt: now.Add(-time.Minute), LastHeartbeatStartedAt: now.Add(-30 * time.Second), RunsInFlight: 2, SlotsUsed: 2, SlotsTotal: 4, DiscoveryPaused: true, PauseReason: "rate limit", ExpectedResumeAt: resume, RunsHeld: "disk capacity"}}}
	reads := 0
	service := New(f, redact.New(redact.Options{Environ: []string{}}), func() time.Time { at := now.Add(time.Duration(reads) * time.Millisecond); reads++; return at })
	r, err := service.Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{"runs_in_flight": json.Number("2"), "slots_used": json.Number("2"), "slots_total": json.Number("4"), "discovery_paused": true, "pause_reason": "rate limit", "runs_held": "disk capacity", "heartbeat_age_seconds": json.Number("30")} {
		if r.Health[key] != want {
			t.Fatalf("%s=%v want=%v", key, r.Health[key], want)
		}
	}
	if r.Health["expected_resume_at"] != resume.Format(time.RFC3339) {
		t.Fatalf("resume=%v", r.Health["expected_resume_at"])
	}
	h := r.Coverage.Sources["health"]
	if h.ObservedAt == nil || !h.ObservedAt.Equal(f.health.Listener.WrittenAt) || !h.ReadAt.After(r.GeneratedAt) {
		t.Fatalf("source times=%+v generated=%v", h, r.GeneratedAt)
	}
	if !r.Coverage.RepositoriesHasMore || r.Coverage.Sources["registry"].Status != "truncated" || !has(r.Coverage.Warnings, "repository_coverage_truncated") {
		t.Fatalf("registry omissions hidden: %+v", r.Coverage)
	}
}

func TestBuildProtectsHostPathsLinksAndLargeIdentities(t *testing.T) {
	const largeID int64 = 9007199254740993
	f := &fakeReader{activity: []store.OverviewActivity{{ID: "activity", RepoID: largeID, SessionID: largeID, TaskRef: "Task"}}, waits: store.OverviewWaits{Entries: []store.OverviewWait{{WaitID: "safe", Summary: "Read /private/factory/worktrees/task/report.md", URL: "javascript:alert(1)", TaskURL: "https://example.com/item?token=hidden"}}}, pubs: []store.OverviewPublication{{Key: "published", Summary: "Published result", URL: "https://example.com/comment#123", ConfirmedAt: testNow()}}}
	service := New(f, redact.New(redact.Options{Environ: []string{}}), testNow, "/private/factory/worktrees")
	r, err := service.Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"repository_id", "session_id"} {
		if r.Activity.Items[0][key] != json.Number("9007199254740993") {
			t.Fatalf("identity precision lost: %s=%v", key, r.Activity.Items[0][key])
		}
	}
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "/private/factory/worktrees") {
		t.Fatal("host root exposed")
	}
	wait := r.OwnerWaits.Items[0]
	if wait["url"] != nil || wait["task_url"] != nil || !has(r.OwnerWaits.Warnings, "publication_link_missing") || !has(r.OwnerWaits.Warnings, "task_link_missing") {
		t.Fatalf("unsafe link retained or omission hidden: %+v %+v", wait, r.OwnerWaits.Warnings)
	}
	if len(r.RecentPublications.Items) != 1 || r.RecentPublications.Items[0]["url"] != "https://example.com/comment#123" {
		t.Fatalf("valid publication lost: %+v", r.RecentPublications)
	}
}

func TestBuildMalformedDisplayKeepsOtherFacts(t *testing.T) {
	f := &fakeReader{activity: []store.OverviewActivity{{ID: "working"}}, waits: store.OverviewWaits{Entries: []store.OverviewWait{{WaitID: "malformed", Summary: "<pre>unfinished"}}}, pubs: []store.OverviewPublication{{Key: "confirmed", Summary: "Confirmed result", ConfirmedAt: testNow()}}}
	r, err := fixedService(f, testNow()).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if r.OwnerWaits.Items[0]["summary"] != "[display unavailable]" || !has(r.Coverage.Warnings, "display_protection_omitted_fields") {
		t.Fatalf("unsafe display omission hidden: %+v", r)
	}
	if len(r.Activity.Items) != 1 || r.Activity.Items[0]["activity_id"] != "working" || len(r.RecentPublications.Items) != 1 || r.RecentPublications.Items[0]["delivery_key"] != "confirmed" {
		t.Fatal("valid sections lost after display failure")
	}
}
