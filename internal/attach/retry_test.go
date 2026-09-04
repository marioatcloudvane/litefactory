package attach

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
)

// flakyFetcher fails a URL a fixed number of times, then serves it.
type flakyFetcher struct {
	mu       sync.Mutex
	failures map[string]int
	payload  []byte
	calls    map[string]int
}

func newFlaky(payload []byte) *flakyFetcher {
	return &flakyFetcher{failures: map[string]int{}, calls: map[string]int{}, payload: payload}
}

func (f *flakyFetcher) failTimes(url string, n int) *flakyFetcher {
	f.failures[url] = n
	return f
}

func (f *flakyFetcher) FetchAsset(_ context.Context, rawURL string) (io.ReadCloser, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[rawURL]++
	if f.failures[rawURL] > 0 {
		f.failures[rawURL]--
		return nil, "", errors.New("404 not found")
	}
	return io.NopCloser(strings.NewReader(string(f.payload))), "", nil
}

func (f *flakyFetcher) callCount(url string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[url]
}

// TestOneFailedFetchIsRetriedAndSucceeds is AC-8.1's happy half: a single
// transient failure costs one extra round trip and the attachment still
// lands.
func TestOneFailedFetchIsRetriedAndSucceeds(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	url := assetURL("flaky")
	f := newFlaky(body(pngMagic, 512)).failTimes(url, 1)

	got, err := Ingest(context.Background(), f, dir, []Ref{{Name: "flaky", URL: url}}, generous)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if len(got.Accepted) != 1 {
		t.Fatalf("accepted %d, want 1: %+v", len(got.Accepted), got.Rejected)
	}
	if n := f.callCount(url); n != 2 {
		t.Errorf("fetcher was called %d times, want 2 (one attempt, one retry)", n)
	}
	if got.Accepted[0].Route != RouteRetry {
		t.Errorf("Route = %v, want RouteRetry", got.Accepted[0].Route)
	}
}

// TestRetryHappensExactlyOnce is AC-8.1's "never a loop". A URL that always
// fails must cost two calls and no more, however many times it is offered.
func TestRetryHappensExactlyOnce(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	url := assetURL("always-gone")
	f := newFlaky(body(pngMagic, 512)).failTimes(url, 99)

	got, err := Ingest(context.Background(), f, dir, []Ref{{Name: "always gone", URL: url}}, generous)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if n := f.callCount(url); n != 2 {
		t.Fatalf("fetcher was called %d times, want exactly 2", n)
	}
	if len(got.Rejected) != 1 || got.Rejected[0].Reason != ReasonFetchFailed {
		t.Fatalf("rejections = %+v, want one ReasonFetchFailed", got.Rejected)
	}
	if len(got.Accepted) != 0 {
		t.Errorf("accepted %+v, want none", got.Accepted)
	}
}

// TestASucceedingFetchIsNotRetried keeps the retry from doubling the cost of
// the ordinary case.
func TestASucceedingFetchIsNotRetried(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	url := assetURL("fine")
	f := newFlaky(body(pngMagic, 512))

	got, err := Ingest(context.Background(), f, dir, []Ref{{Name: "fine", URL: url}}, generous)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if n := f.callCount(url); n != 1 {
		t.Errorf("fetcher was called %d times, want 1", n)
	}
	if len(got.Accepted) != 1 || got.Accepted[0].Route != RouteFirstAttempt {
		t.Fatalf("accepted %+v, want one on RouteFirstAttempt", got.Accepted)
	}
}

// TestADecisionIsNotRetried is the other half of "only a failure is retried":
// an attachment refused for its type or its size was answered, not missed,
// and asking again would reach the same answer at the cost of a round trip.
func TestADecisionIsNotRetried(t *testing.T) {
	t.Parallel()

	t.Run("unsupported type", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		url := assetURL("notes")
		f := newFlaky([]byte("plain text, not an image or a document"))

		got, err := Ingest(context.Background(), f, dir, []Ref{{Name: "notes", URL: url}}, generous)
		if err != nil {
			t.Fatalf("Ingest: %v", err)
		}
		if n := f.callCount(url); n != 1 {
			t.Errorf("fetcher was called %d times for an unsupported type, want 1", n)
		}
		if len(got.Rejected) != 1 || got.Rejected[0].Reason != ReasonUnsupportedType {
			t.Fatalf("rejections = %+v, want one ReasonUnsupportedType", got.Rejected)
		}
	})

	t.Run("oversized", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		url := assetURL("huge")
		f := newFlaky(body(pngMagic, 50_000))
		lim := Limits{MaxImageBytes: 1000, MaxDocumentBytes: 1000, MaxTotalBytes: 1 << 20, MaxCount: 10}

		got, err := Ingest(context.Background(), f, dir, []Ref{{Name: "huge", URL: url}}, lim)
		if err != nil {
			t.Fatalf("Ingest: %v", err)
		}
		if n := f.callCount(url); n != 1 {
			t.Errorf("fetcher was called %d times for an oversized file, want 1", n)
		}
		if len(got.Rejected) != 1 || got.Rejected[0].Reason != ReasonOversized {
			t.Fatalf("rejections = %+v, want one ReasonOversized", got.Rejected)
		}
	})
}

// TestAnExhaustedRetryIsARejectionNotAFailure is the story's own framing: a
// fetch that runs out of attempts or budget is an attachment that did not
// make it, never a turn that failed.
func TestAnExhaustedRetryIsARejectionNotAFailure(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	f := newFlaky(body(pngMagic, 512)).
		failTimes(assetURL("gone"), 99)
	// A second, healthy attachment proves the turn carries on around the
	// failure rather than stopping at it.
	f.failures[assetURL("fine")] = 0

	got, err := Ingest(context.Background(), f, dir, refsFor("gone", "fine"), generous)
	if err != nil {
		t.Fatalf("Ingest returned an error rather than a rejection: %v", err)
	}
	if len(got.Accepted) != 1 || got.Accepted[0].Name != "fine" {
		t.Fatalf("accepted %+v, want the healthy one", got.Accepted)
	}
	if len(got.Rejected) != 1 || got.Rejected[0].Reason != ReasonFetchFailed {
		t.Fatalf("rejections = %+v, want one ReasonFetchFailed", got.Rejected)
	}
}

// TestTimingBudgetsAreTheOnesTheStoryNames pins the two numbers, because
// they are the kind of constant that drifts silently and the story states
// them explicitly.
func TestTimingBudgetsAreTheOnesTheStoryNames(t *testing.T) {
	t.Parallel()

	if perFetchTimeout.Seconds() != 30 {
		t.Errorf("perFetchTimeout = %v, want 30s", perFetchTimeout)
	}
	if perTurnTimeout.Seconds() != 120 {
		t.Errorf("perTurnTimeout = %v, want 120s", perTurnTimeout)
	}
}

func TestRouteStringsAreStable(t *testing.T) {
	t.Parallel()

	for route, want := range map[Route]string{
		RouteFirstAttempt: "first attempt",
		RouteRetry:        "retry",
		RouteNone:         "not fetched",
	} {
		if got := route.String(); got != want {
			t.Errorf("Route(%d).String() = %q, want %q", route, got, want)
		}
	}
}
