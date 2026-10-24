package discord_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/messaging/discord"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestOrdinaryIntakeFiltersAndCapability(t *testing.T) {
	flags := uint64(1 << 18)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/applications/@me" {
			fmt.Fprintf(w, `{"flags":%d}`, flags)
			return
		}
		fmt.Fprint(w, `[
   {"id":"9","channel_id":"2","content":"owner","timestamp":"2026-10-11T12:00:00Z","author":{"id":"3"},"message_reference":{"channel_id":"2","message_id":"7"}},
   {"id":"8","channel_id":"2","content":"private outsider","author":{"id":"4"}},
   {"id":"7","channel_id":"2","content":"bot","author":{"id":"3","bot":true}},
   {"id":"6","channel_id":"2","content":"webhook","author":{"id":"3"},"webhook_id":"1"},
   {"id":"5","channel_id":"2","content":"system","author":{"id":"3"},"type":6},
   {"id":"4","channel_id":"99","content":"thread","author":{"id":"3"}},
   {"id":"3","channel_id":"2","content":"forwarded","author":{"id":"3"},"message_snapshots":[{}]}
  ]`)
	}))
	defer api.Close()
	now := time.Now()
	a := discord.New(discord.Options{APIURL: api.URL, Client: api.Client(), Now: func() time.Time { return now }})
	if err := a.CheckIntakeCapability(t.Context(), "2"); err != nil {
		t.Fatal(err)
	}
	page, err := a.ReadIntakePage(t.Context(), "2", "3", "")
	if err != nil || len(page.Messages) != 1 || page.Count != 7 || page.HighID != "9" || page.LowID != "3" || page.Messages[0].ReplyTo != "7" {
		t.Fatalf("page %+v error %v", page, err)
	}
	flags = 0
	now = now.Add(6 * time.Minute)
	if a.CheckIntakeCapability(t.Context(), "2") == nil {
		t.Error("missing content capability accepted")
	}
}
func TestOrdinaryIntakeRateLimit(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"retry_after":12}`)
	}))
	defer api.Close()
	a := discord.New(discord.Options{APIURL: api.URL, Client: api.Client()})
	_, err := a.ReadIntakePage(t.Context(), "2", "3", "")
	var failure *discord.IntakeFailure
	if !errors.As(err, &failure) || failure.RetryAfter != 12*time.Second {
		t.Fatalf("retry error %v", err)
	}
}

// fakeReads is a Discord API that counts reads per route and answers with
// the status set for each route.
type fakeReads struct {
	mu     sync.Mutex
	calls  map[string]int
	status map[string]int
	header http.Header
}

func (f *fakeReads) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	route := r.URL.Path
	if r.URL.Query().Get("limit") == "1" {
		route += "?limit=1"
	}
	f.calls[route]++
	status := f.status[r.URL.Path]
	for name, values := range f.header {
		w.Header()[name] = values
	}
	f.mu.Unlock()
	if status != 0 {
		w.WriteHeader(status)
		if status == http.StatusTooManyRequests {
			fmt.Fprint(w, `{"retry_after":30,"global":false}`)
		}
		return
	}
	switch r.URL.Path {
	case "/applications/@me":
		fmt.Fprint(w, `{"flags":262144}`)
	case "/users/@me":
		fmt.Fprint(w, `{"id":"5","bot":true}`)
	default:
		fmt.Fprint(w, `[]`)
	}
}

func (f *fakeReads) count(route string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[route]
}

func (f *fakeReads) set(route string, status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status[route] = status
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *fakeClock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func readsFixture(t *testing.T) (*discord.Adapter, *fakeReads, *fakeClock, *messaging.Intake) {
	t.Helper()
	fake := &fakeReads{calls: map[string]int{}, status: map[string]int{}, header: http.Header{}}
	api := httptest.NewServer(fake)
	t.Cleanup(api.Close)
	clock := &fakeClock{now: time.Now()}
	a := discord.New(discord.Options{APIURL: api.URL, Client: api.Client(), Now: clock.Now})
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "factory.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	intake := &messaging.Intake{DB: db, Reader: a, Owner: "3", Transport: "discord", Channels: []string{"2"}, Protect: func(_ context.Context, text string) (string, error) { return text, nil }}
	return a, fake, clock, intake
}

func TestIntakeReadsOnceAPollAndChecksAccessEveryFewMinutes(t *testing.T) {
	_, fake, clock, intake := readsFixture(t)
	// Ten minutes of polls every five seconds.
	for range 120 {
		if err := intake.Poll(t.Context(), clock.Now()); err != nil {
			t.Fatal(err)
		}
		clock.Add(5 * time.Second)
	}
	reads, checks := fake.count("/channels/2/messages"), fake.count("/applications/@me")
	if reads != 120 || checks != 2 || fake.count("/channels/2/messages?limit=1") != 2 {
		t.Fatalf("reads=%d capability checks=%d history checks=%d", reads, checks, fake.count("/channels/2/messages?limit=1"))
	}
	// Before: three calls a poll, 36 a minute. Now about 12.4 a minute.
	if perMinute := float64(reads+checks+fake.count("/channels/2/messages?limit=1")) / 10; perMinute > 13 {
		t.Fatalf("%.1f Discord calls a minute", perMinute)
	}
}

func TestIntakeWaitsOutRetryAfterForEveryRead(t *testing.T) {
	a, fake, clock, intake := readsFixture(t)
	if err := intake.Poll(t.Context(), clock.Now()); err != nil {
		t.Fatal(err)
	}
	fake.set("/channels/2/messages", http.StatusTooManyRequests)
	err := intake.Poll(t.Context(), clock.Now())
	var failure *discord.IntakeFailure
	if !errors.As(err, &failure) || failure.RetryAfter != 30*time.Second || failure.Kind() != "rate limited" {
		t.Fatalf("rate limit not reported: %v", err)
	}
	limited := fake.count("/channels/2/messages")
	fake.set("/channels/2/messages", 0)
	// Within Retry-After no read of that route reaches Discord.
	for range 5 {
		clock.Add(5 * time.Second)
		err = intake.Poll(t.Context(), clock.Now())
		if !errors.As(err, &failure) || failure.RetryAfter <= 0 || failure.RetryAfter > 25*time.Second {
			t.Fatalf("read during Retry-After: %v", err)
		}
	}
	if _, err = a.ReadAskHistory(t.Context(), "2"); err == nil {
		t.Fatal("history read ran during Retry-After")
	}
	if fake.count("/channels/2/messages") != limited {
		t.Fatalf("Discord called during Retry-After: %d > %d", fake.count("/channels/2/messages"), limited)
	}
	clock.Add(6 * time.Second)
	if err = intake.Poll(t.Context(), clock.Now()); err != nil {
		t.Fatalf("reads did not resume after Retry-After: %v", err)
	}
}

func TestGlobalRateLimitStopsAllReads(t *testing.T) {
	a, fake, clock, _ := readsFixture(t)
	fake.header.Set("X-RateLimit-Global", "true")
	fake.set("/applications/@me", http.StatusTooManyRequests)
	if err := a.CheckIntakeCapability(t.Context(), "2"); err == nil {
		t.Fatal("rate limit accepted")
	}
	fake.set("/applications/@me", 0)
	_, err := a.ReadIntakePage(t.Context(), "2", "3", "")
	var failure *discord.IntakeFailure
	if !errors.As(err, &failure) || failure.RetryAfter <= 0 || fake.count("/channels/2/messages") != 0 {
		t.Fatalf("global rate limit not honoured: %v calls=%d", err, fake.count("/channels/2/messages"))
	}
	clock.Add(31 * time.Second)
	if _, err = a.ReadIntakePage(t.Context(), "2", "3", ""); err != nil {
		t.Fatal(err)
	}
}

func TestRefusedReadSurfacesAtOnce(t *testing.T) {
	a, fake, clock, intake := readsFixture(t)
	if err := intake.Poll(t.Context(), clock.Now()); err != nil {
		t.Fatal(err)
	}
	clock.Add(5 * time.Second)
	// The capability check still stands, but the read itself is refused.
	fake.set("/channels/2/messages", http.StatusUnauthorized)
	err := intake.Poll(t.Context(), clock.Now())
	var failure *discord.IntakeFailure
	if !errors.As(err, &failure) || !failure.Capability || failure.Kind() != "unauthorized" {
		t.Fatalf("refused read not surfaced: %v", err)
	}
	// The next access check asks Discord again instead of the cached pass.
	checks := fake.count("/applications/@me")
	if err = a.CheckIntakeCapability(t.Context(), "2"); err == nil || fake.count("/applications/@me") != checks+1 {
		t.Fatalf("refused access still cached: %v", err)
	}
}
