package daemon_test

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/ticket"
)

// lostWebhookFixture is a connection-mode daemon with a recorded GitHub hook
// whose delivery log holds one failed `issues opened` delivery for testIssue
// once failed is set. It counts delivery-log reads and redelivery POSTs.
type lostWebhookFixture struct {
	*webhookFixture
	origin    ticket.Origin
	failed    atomic.Bool
	forbidden atomic.Bool
	reads     atomic.Int64
	posts     atomic.Int64
	listings  atomic.Int64
	// hookURL is the URL the repository's hook 9 reports in a hook listing.
	hookURL  atomic.Value
	original time.Time
}

func newLostWebhookFixture(t *testing.T) *lostWebhookFixture {
	t.Helper()
	return newLostWebhookFixtureWith(t, true, "")
}

// newLostWebhookFixtureWith builds the fixture; recorded=false leaves the
// subscription without a recorded hook ID, as an existing subscription whose
// hook was made before the ID was kept (#1516). hookURL is the URL hook 9
// reports in a hook listing; empty means the subscription's callback URL.
func newLostWebhookFixtureWith(t *testing.T, recorded bool, hookURL string) *lostWebhookFixture {
	t.Helper()
	w := newWebhookFixture(t)
	w.clock.mu.Lock()
	w.clock.now = time.Now().UTC().Add(time.Minute)
	w.clock.mu.Unlock()
	f := &lostWebhookFixture{webhookFixture: w}
	start := w.clock.peek()
	f.original = start.Add(10 * time.Second)
	f.origin = recoveryProvider(t, w.fixture, func(out http.ResponseWriter, r *http.Request) bool {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/repos/"+w.repo.Slug()+"/hooks") {
			f.listings.Add(1)
			out.Header().Set("Content-Type", "application/json")
			url, _ := f.hookURL.Load().(string)
			_, _ = fmt.Fprintf(out, `[{"id":7,"active":true,"events":["push"],"config":{"url":"https://elsewhere.example/hook","content_type":"json"}},{"id":9,"active":true,"events":["issues","issue_comment"],"config":{"url":%q,"content_type":"json"}}]`, url)
			return true
		}
		if !strings.Contains(r.URL.Path, "/hooks/9/deliveries") {
			return false
		}
		out.Header().Set("Content-Type", "application/json")
		out.Header().Set("Date", w.clock.peek().UTC().Format(http.TimeFormat))
		if f.forbidden.Load() {
			out.WriteHeader(http.StatusForbidden)
			_, _ = out.Write([]byte(`{"message":"Resource not accessible by personal access token"}`))
			return true
		}
		if r.Method == http.MethodPost {
			f.posts.Add(1)
			out.WriteHeader(http.StatusAccepted)
			return true
		}
		if strings.HasSuffix(r.URL.Path, "/deliveries/1") {
			_, _ = fmt.Fprintf(out, `{"id":1,"guid":"lost-open","repository_id":1,"event":"issues","action":"opened","request":{"payload":{"repository":{"full_name":%q},"issue":{"number":%d,"url":%q}}}}`,
				w.repo.Slug(), testIssue, string(f.origin)+"/repos/"+w.repo.Slug()+"/issues/"+strconv.Itoa(testIssue))
			return true
		}
		f.reads.Add(1)
		if !f.failed.Load() {
			_, _ = out.Write([]byte(`[]`))
			return true
		}
		_, _ = fmt.Fprintf(out, `[{"id":1,"guid":"lost-open","repository_id":1,"event":"issues","action":"opened","redelivery":false,"status_code":502,"delivered_at":%q}]`, f.original.Format(time.RFC3339))
		return true
	})
	configureRecoverySource(t, w, f.origin)
	sub, err := w.db.GetTicketSubscription(t.Context(), recoverySelector)
	if err != nil {
		t.Fatal(err)
	}
	if hookURL == "" {
		hookURL = sub.CallbackURL
	}
	f.hookURL.Store(hookURL)
	if recorded {
		op, err := w.db.ClaimSubscriptionOperation(t.Context(), sub.ID, sub.Revision, "create")
		if err != nil {
			t.Fatal(err)
		}
		if err := w.db.FinishSubscriptionOperation(t.Context(), op, "confirmed", "9", "active", "", start); err != nil {
			t.Fatal(err)
		}
	}
	w.daemon = newRecoveryDaemon(t, w, f.origin, "lost-webhook", false)
	w.plantCursor()
	return f
}

// A new issue whose `issues opened` webhook failed on GitHub's side is
// redelivered by the five-minute delivery-log check, without waiting for the
// hourly listing (#1508, lisisite#19).
func TestLostIssueWebhookRedeliveredWithinFiveMinutes(t *testing.T) {
	f := newLostWebhookFixture(t)
	f.clock.advance(10 * time.Second)
	f.assignedIssue("Lost webhook", "/spike investigate the lost webhook")
	f.failed.Store(true)

	// Heartbeats inside the five minutes do not read the delivery log.
	f.clock.advance(time.Minute)
	f.settle()
	if got := f.posts.Load(); got != 0 {
		t.Fatalf("redelivery POSTs one minute after the last check = %d, want 0", got)
	}

	f.clock.advance(5 * time.Minute)
	f.settle()
	if got := f.posts.Load(); got != 1 {
		t.Fatalf("redelivery POSTs within five minutes = %d, want 1\nlogs:\n%s", got, f.fixture.logs)
	}
	if got := f.turns.triggers(); !slices.Equal(got, []int64{0}) {
		t.Fatalf("executions = %v, want the issue body\nlogs:\n%s", got, f.fixture.logs)
	}

	// The next check looks only at deliveries since the last one: the same
	// failed delivery is neither re-requested nor polled again.
	reads := f.reads.Load()
	f.clock.advance(5 * time.Minute)
	f.settle()
	if got := f.reads.Load(); got != reads+1 {
		t.Fatalf("delivery-log reads on the next check = %d, want one more than %d", got, reads)
	}
	if got := f.posts.Load(); got != 1 {
		t.Fatalf("the next check re-requested an old delivery: POSTs = %d", got)
	}
	if got := f.turns.triggers(); !slices.Equal(got, []int64{0}) {
		t.Fatalf("duplicate executions = %v", got)
	}
}

// An existing GitHub subscription with no recorded hook ID finds its hook by
// the callback URL, records the ID and redelivers a failed delivery, without an
// operator step (#1516).
func TestLostWebhookRedeliveredWhenHookIDWasNotRecorded(t *testing.T) {
	f := newLostWebhookFixtureWith(t, false, "")
	f.clock.advance(10 * time.Second)
	f.assignedIssue("Lost webhook", "/spike investigate the lost webhook")
	f.failed.Store(true)
	f.clock.advance(6 * time.Minute)
	f.settle()
	if got := f.posts.Load(); got != 1 {
		t.Fatalf("redelivery POSTs with an unrecorded hook ID = %d, want 1\nlogs:\n%s", got, f.fixture.logs)
	}
	sub, err := f.db.GetTicketSubscription(t.Context(), recoverySelector)
	if err != nil {
		t.Fatal(err)
	}
	if sub.RemoteHookID != "9" {
		t.Fatalf("recorded hook ID = %q, want 9", sub.RemoteHookID)
	}
	if strings.Contains(f.fixture.logs.String(), "webhook recovery off") {
		t.Fatalf("recovery reported off although the hook was found\nlogs:\n%s", f.fixture.logs)
	}
}

// When no listed hook points at the subscription's callback, recovery says so
// once as a warning with the reason, not as an INFO that polling covers intake.
func TestUnfoundHookIsWarnedOnce(t *testing.T) {
	f := newLostWebhookFixtureWith(t, false, "https://other.example/webhook")
	f.clock.advance(10 * time.Second)
	f.assignedIssue("Lost webhook", "/spike investigate the lost webhook")
	f.failed.Store(true)
	for range 3 {
		f.clock.advance(5*time.Minute + time.Second)
		f.settle()
	}
	logs := f.fixture.logs.String()
	const line = "webhook recovery off: the repository's webhook could not be found"
	var warned []string
	for _, entry := range strings.Split(logs, "\n") {
		if strings.Contains(entry, line) {
			warned = append(warned, entry)
		}
	}
	if len(warned) != 1 || !strings.Contains(warned[0], "level=WARN") || !strings.Contains(warned[0], "reason=") {
		t.Fatalf("want one WARN %q with a reason\nlogs:\n%s", line, logs)
	}
	if strings.Contains(logs, "webhooks and ordinary polling cover intake") {
		t.Fatalf("unfound hook still logged as covered by polling\nlogs:\n%s", logs)
	}
	if got := f.posts.Load(); got != 0 {
		t.Fatalf("redelivery POSTs without a hook = %d", got)
	}
}

// A token that cannot read the delivery log says so plainly and does not
// bring the listing forward.
func TestUnreadableDeliveryLogIsLoggedWithoutFasterListing(t *testing.T) {
	f := newLostWebhookFixture(t)
	f.forbidden.Store(true)
	f.clock.advance(10 * time.Second)
	f.assignedIssue("Lost webhook", "/spike investigate the lost webhook")
	for range 3 {
		f.clock.advance(5*time.Minute + time.Second)
		f.settle()
	}
	logs := f.fixture.logs.String()
	if !strings.Contains(logs, "cannot read the webhook delivery log") {
		t.Fatalf("no plain unreadable-delivery-log line\nlogs:\n%s", logs)
	}
	if strings.Count(logs, "cannot read the webhook delivery log") != 1 {
		t.Fatalf("unreadable delivery log repeated on every check\nlogs:\n%s", logs)
	}
	if got := f.turns.triggers(); len(got) != 0 {
		t.Fatalf("a faster listing picked the issue up: executions = %v", got)
	}
}

// forge_issues counts what the connection-mode listing returned (#1508): it
// read 0 on every tick, even a listing that found a new issue.
func TestConnectionListingCountsForgeIssues(t *testing.T) {
	w := newWebhookFixture(t)
	w.clock.mu.Lock()
	w.clock.now = time.Now().UTC().Add(time.Minute)
	w.clock.mu.Unlock()
	origin := recoveryProvider(t, w.fixture)
	configureRecoverySource(t, w, origin)
	w.daemon = newRecoveryDaemon(t, w, origin, "forge-issues-count", false)
	w.plantCursor()
	w.clock.advance(10 * time.Second)
	w.assignedIssue("Counted", "/spike count me")
	w.clock.advance(scheduleInterval)
	if got := w.settle().Issues; got != 1 {
		t.Fatalf("forge_issues on the listing that found the issue = %d, want 1", got)
	}
}
