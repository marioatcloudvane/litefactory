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
	original  time.Time
}

func newLostWebhookFixture(t *testing.T) *lostWebhookFixture {
	t.Helper()
	w := newWebhookFixture(t)
	w.clock.mu.Lock()
	w.clock.now = time.Now().UTC().Add(time.Minute)
	w.clock.mu.Unlock()
	f := &lostWebhookFixture{webhookFixture: w}
	start := w.clock.peek()
	f.original = start.Add(10 * time.Second)
	f.origin = recoveryProvider(t, w.fixture, func(out http.ResponseWriter, r *http.Request) bool {
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
	op, err := w.db.ClaimSubscriptionOperation(t.Context(), sub.ID, sub.Revision, "create")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.db.FinishSubscriptionOperation(t.Context(), op, "confirmed", "9", "active", "", start); err != nil {
		t.Fatal(err)
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
