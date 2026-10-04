package daemon_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/turn"
	"github.com/cloudvane/litefactory/internal/webhook"
)

const webhookTestSecret = "whsec-test-secret"

// keychainSpy is the keychain the webhook secrets are read from, recording
// every lookup.
type keychainSpy struct {
	mu      sync.Mutex
	calls   [][2]string
	secrets map[string]string
}

func (k *keychainSpy) lookup(_ context.Context, service, account string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.calls = append(k.calls, [2]string{service, account})
	if v, ok := k.secrets[account]; ok {
		return v, nil
	}
	return "", secrets.ErrNotFound
}

func (k *keychainSpy) count() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return len(k.calls)
}

func (k *keychainSpy) store(account, secret string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.secrets[account] = secret
}

// webhookFixture is a fixture whose daemon lists on a 60 minute schedule and
// serves webhooks on an ephemeral loopback port, with an injected tick source
// nobody feeds: only a wake can run a pass on the loop.
type webhookFixture struct {
	*fixture
	spy   *keychainSpy
	ln    net.Listener
	logs  *syncBuffer
	ticks chan time.Time
}

func newWebhookFixture(t *testing.T) *webhookFixture {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("bind an ephemeral port: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	w := &webhookFixture{
		fixture: newFixture(t),
		spy:     &keychainSpy{secrets: map[string]string{testOwnerName + "/" + testRepoName: webhookTestSecret}},
		ln:      ln,
		logs:    &syncBuffer{},
		ticks:   make(chan time.Time),
	}
	w.daemon = w.newDaemon(daemon.Options{
		DiscoveryInterval: scheduleInterval,
		Ticks:             w.ticks,
		Logger:            slog.New(slog.NewJSONHandler(w.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Webhook:           &daemon.WebhookOptions{Listener: ln, Secrets: secrets.Resolver{Keychain: w.spy.lookup}, Now: w.clock.peek},
	})
	return w
}

func issuePayload(number int) []byte {
	return []byte(fmt.Sprintf(`{"action":"opened","issue":{"number":%d},"repository":{"full_name":"%s/%s"}}`, number, testOwnerName, testRepoName))
}

// post sends one delivery and returns the status code.
func (w *webhookFixture) post(event, delivery, signature string, body []byte) int {
	w.t.Helper()
	req, err := http.NewRequestWithContext(w.t.Context(), http.MethodPost, "http://"+w.ln.Addr().String()+webhook.Path, bytes.NewReader(body))
	if err != nil {
		w.t.Fatal(err)
	}
	req.Header.Set("X-GitHub-Event", event)
	req.Header.Set("X-GitHub-Delivery", delivery)
	if signature != "" {
		req.Header.Set("X-Hub-Signature-256", signature)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		w.t.Fatalf("post a delivery: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// run starts the daemon loop and returns a stop function that cancels it and
// waits for Run to return.
func (w *webhookFixture) run() (stop func() error) {
	ctx, cancel := context.WithCancel(w.t.Context())
	done := make(chan error, 1)
	go func() { done <- w.daemon.Run(ctx) }()
	return func() error {
		cancel()
		select {
		case err := <-done:
			return err
		case <-time.After(30 * time.Second):
			return fmt.Errorf("Run did not return after cancellation")
		}
	}
}

// AC-009, AC-004, AC-007, AC-008: a signed delivery served by the running
// daemon wakes one pass on the loop, which reads the issue fresh without a
// listing, and the daemon stops gracefully with its listener.
func TestWebhookDeliveryWakesAScopedPassOnTheLoop(t *testing.T) {
	w := newWebhookFixture(t)
	w.plantCursor()
	w.assignedIssue("Add a /health endpoint", "Please add a health endpoint.")
	listings := w.listings()
	keychainCalls := w.spy.count()

	ran := make(chan struct{}, 4)
	w.turns.set(func(ctx context.Context, req turn.Request) (turn.Result, error) {
		ran <- struct{}{}
		return w.ranSuccessfully(ctx, req)
	})
	stop := w.run()

	body := issuePayload(testIssue)
	if got := w.post("issues", "d-bad", "sha256="+strings.Repeat("0", 64), body); got != http.StatusUnauthorized {
		t.Fatalf("a bad signature answered %d, want 401", got)
	}
	if got := w.daemon.PendingHintsForTest(); got != 0 {
		t.Fatalf("a rejected delivery left %d hints", got)
	}
	if got := w.post("issues", "d-1", webhook.Sign([]byte(webhookTestSecret), body), body); got != http.StatusAccepted {
		t.Fatalf("a signed delivery answered %d, want 202", got)
	}

	select {
	case <-ran:
	case <-time.After(30 * time.Second):
		t.Fatalf("the delivery did not wake a pass that ran the issue\nlogs:\n%s", w.logs)
	}
	if err := stop(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := w.listings(); got != listings {
		t.Errorf("the woken pass listed the forge: %d listings, want %d", got, listings)
	}
	if got := w.forge.issueReadsOf(testIssue); got < 1 {
		t.Errorf("the issue was never read fresh")
	}
	if got := w.spy.count(); got != keychainCalls {
		t.Errorf("the delivery caused %d keychain lookups", got-keychainCalls)
	}
	records := dispatchRecords(t, w.logs, "worker dispatched")
	if len(records) != 1 || fmt.Sprint(records[0]["dispatch_sources"]) != "[webhook]" {
		t.Errorf("dispatch records = %v, want one with sources [webhook]", records)
	}
	if got := w.daemon.PendingHintsForTest(); got != 0 {
		t.Errorf("%d hints still pending after the pass", got)
	}
	if conn, err := net.DialTimeout("tcp", w.ln.Addr().String(), time.Second); err == nil {
		_ = conn.Close()
		t.Error("the listener still accepts connections after Run returned")
	}
}

// AC-004, AC-007: a hint is read fresh through the budgeted read and handed to
// admission with the webhook source, without a listing.
func TestWebhookHintReadsTheIssueFreshAndDispatchesIt(t *testing.T) {
	w := newWebhookFixture(t)
	w.plantCursor()
	w.assignedIssue("Add a /health endpoint", "Please add a health endpoint.")
	listings := w.listings()

	w.daemon.WakeForTest(w.repo.ID, testIssue)
	result := w.settle()

	if result.Hinted != 1 || result.Dispatched != 1 {
		t.Fatalf("hinted=%d dispatched=%d, want 1 and 1", result.Hinted, result.Dispatched)
	}
	if got := w.listings(); got != listings {
		t.Errorf("a heartbeat with a hint listed the forge")
	}
	if got := w.forge.issueReadsOf(testIssue); got < 1 {
		t.Errorf("the hinted issue was never read")
	}
	if got := w.daemon.PendingHintsForTest(); got != 0 {
		t.Errorf("the hint stayed pending after its read")
	}
}

// AC-007: a closed issue named by a hint is skipped as it is today, and the
// hint is not retried forever.
func TestWebhookHintForAClosedIssueIsSkipped(t *testing.T) {
	w := newWebhookFixture(t)
	w.plantCursor()
	w.assignedIssue("Add a /health endpoint", "Please add a health endpoint.")
	w.forge.closeIssue(testIssue)

	w.daemon.WakeForTest(w.repo.ID, testIssue)
	result := w.settle()

	if result.Dispatched != 0 {
		t.Fatalf("a closed issue was dispatched")
	}
	if got := w.daemon.PendingHintsForTest(); got != 0 {
		t.Errorf("the closed issue's hint stayed pending")
	}
}

// AC-007: a failed read keeps the hint pending; the next heartbeat retries it.
func TestWebhookFailedReadKeepsTheHintPending(t *testing.T) {
	w := newWebhookFixture(t)
	w.plantCursor()
	w.assignedIssue("Add a /health endpoint", "Please add a health endpoint.")
	var mu sync.Mutex
	failing := true
	w.forge.onGetIssue = func(rw http.ResponseWriter, number, _ int) bool {
		mu.Lock()
		defer mu.Unlock()
		if number != testIssue || !failing {
			return false
		}
		http.Error(rw, `{"message":"boom"}`, http.StatusInternalServerError)
		return true
	}

	w.daemon.WakeForTest(w.repo.ID, testIssue)
	if result := w.settle(); result.Hinted != 0 || result.Dispatched != 0 {
		t.Fatalf("a failed read produced work: %+v", result)
	}
	if got := w.daemon.PendingHintsForTest(); got != 1 {
		t.Fatalf("pending hints = %d after a failed read, want 1", got)
	}

	mu.Lock()
	failing = false
	mu.Unlock()
	w.clock.advance(30 * time.Second)
	if result := w.settle(); result.Hinted != 1 || result.Dispatched != 1 {
		t.Fatalf("the retry did not run the issue: %+v", result)
	}
	if got := w.daemon.PendingHintsForTest(); got != 0 {
		t.Errorf("pending hints = %d after the retry", got)
	}
}

// AC-007: a rate-limited read backs the daemon off and keeps the hint.
func TestWebhookRateLimitedReadKeepsTheHintAndBacksOff(t *testing.T) {
	w := newWebhookFixture(t)
	w.plantCursor()
	w.assignedIssue("Add a /health endpoint", "Please add a health endpoint.")
	const retryAfter = 45 * time.Second
	w.forge.quotaReset = w.clock.Now().Add(retryAfter).Unix()
	var mu sync.Mutex
	limited := true
	w.forge.onGetIssue = func(rw http.ResponseWriter, number, _ int) bool {
		mu.Lock()
		defer mu.Unlock()
		if number != testIssue || !limited {
			return false
		}
		writeRateLimit(rw, retryAfter)
		return true
	}

	w.daemon.WakeForTest(w.repo.ID, testIssue)
	if _, err := w.tickErr(); err == nil {
		t.Fatal("a rate-limited hint read returned no error")
	}
	if got := w.daemon.PendingHintsForTest(); got != 1 {
		t.Fatalf("pending hints = %d, want 1", got)
	}

	mu.Lock()
	limited = false
	mu.Unlock()
	w.clock.advance(2 * retryAfter)
	if result := w.settle(); result.Dispatched != 1 {
		t.Fatalf("the hint did not run after the backoff: %+v", result)
	}
}

// AC-006: ten deliveries for one issue are one pending hint and one wake.
func TestWebhookTenDeliveriesCollapseToOneHintAndOneWake(t *testing.T) {
	w := newWebhookFixture(t)
	for range 10 {
		w.daemon.WakeForTest(w.repo.ID, testIssue)
	}
	if got := w.daemon.PendingHintsForTest(); got != 1 {
		t.Errorf("pending hints = %d, want 1", got)
	}
	if got := w.daemon.WakeSignalsForTest(); got != 1 {
		t.Errorf("wake signals = %d, want 1", got)
	}
}

// AC-006: overflowing the hint set drops hints and forces the next pass to be
// a full listing, even inside the discovery interval.
func TestWebhookOverflowForcesAFullListing(t *testing.T) {
	w := newWebhookFixture(t)
	w.plantCursor()
	listings := w.listings()

	w.clock.advance(30 * time.Second)
	w.settle()
	if got := w.listings(); got != listings {
		t.Fatalf("a heartbeat inside the interval listed")
	}

	for n := 1; n <= daemon.MaxPendingHintsForTest+1; n++ {
		w.daemon.WakeForTest(w.repo.ID, 1000+n)
	}
	w.clock.advance(30 * time.Second)
	w.settle()
	if got := w.listings(); got != listings+1 {
		t.Fatalf("listings = %d after an overflow, want %d", got, listings+1)
	}

	w.clock.advance(30 * time.Second)
	w.settle()
	if got := w.listings(); got != listings+1 {
		t.Errorf("the forced listing repeated: %d listings", got)
	}
}

// AC-008: the secret is read from the keychain under the documented names, a
// missing one is retried from the heartbeat at most every five minutes, and
// a loaded one is never read again.
func TestWebhookSecretsAreLoadedOnceAndMissingOnesRetriedEveryFiveMinutes(t *testing.T) {
	w := newWebhookFixture(t)
	account := testOwnerName + "/" + testRepoName
	w.spy.mu.Lock()
	delete(w.spy.secrets, account)
	w.spy.mu.Unlock()

	w.plantCursor()
	if got := w.spy.count(); got != 1 {
		t.Fatalf("keychain lookups after the first heartbeat = %d, want 1", got)
	}
	if got := w.spy.calls[0]; got != [2]string{"litefactory-webhook", account} {
		t.Fatalf("keychain lookup = %v", got)
	}

	w.clock.advance(4 * time.Minute)
	w.settle()
	if got := w.spy.count(); got != 1 {
		t.Fatalf("retried after 4 minutes: %d lookups", got)
	}
	w.clock.advance(2 * time.Minute)
	w.settle()
	if got := w.spy.count(); got != 2 {
		t.Fatalf("lookups after 6 minutes = %d, want 2", got)
	}

	w.spy.store(account, webhookTestSecret)
	w.clock.advance(6 * time.Minute)
	w.settle()
	if got := w.spy.count(); got != 3 {
		t.Fatalf("lookups after the secret was stored = %d, want 3", got)
	}
	w.clock.advance(6 * time.Minute)
	w.settle()
	if got := w.spy.count(); got != 3 {
		t.Fatalf("a loaded secret was read again: %d lookups", got)
	}
}

// AC-009: a taken port refuses start-up with a message naming the address and
// the config key, and gives the singleton lock back.
func TestOpenWithAPortInUseNamesTheAddressAndKeyAndReleasesTheLock(t *testing.T) {
	f := newFixture(t)
	dir := fakeClaude(t)
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()

	setup := f.setup(dir)
	setup.Config.Webhook.Listen = busy.Addr().String()
	loop, _, err := daemon.Open(t.Context(), setup)
	if err == nil {
		t.Fatalf("Open started with the port taken; closing: %v", loop.Close())
	}
	for _, want := range []string{busy.Addr().String(), "webhook.listen"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not name %q:\n%v", want, err)
		}
	}
	lock, err := daemon.AcquireLock(f.paths.LockPath())
	if err != nil {
		t.Fatalf("the singleton lock was not released: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
}

// AC-001, AC-008, AC-009: Open binds the configured address, loads each
// repository's secret once under the documented keychain names, and Close
// frees the port; an empty address leaves the listener off.
func TestOpenBindsTheListenerLoadsSecretsOnceAndCloseFreesThePort(t *testing.T) {
	f := newFixture(t)
	dir := fakeClaude(t)
	spy := &keychainSpy{secrets: map[string]string{testOwnerName + "/" + testRepoName: webhookTestSecret}}

	setup := f.setup(dir)
	setup.Config.Webhook.Listen = "127.0.0.1:0"
	setup.WebhookSecrets = secrets.Resolver{Keychain: spy.lookup}
	loop, startup, err := daemon.Open(t.Context(), setup)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if startup.WebhookAddr == "" {
		t.Fatal("Startup does not report the bound address")
	}
	if spy.count() != 1 || spy.calls[0] != [2]string{"litefactory-webhook", testOwnerName + "/" + testRepoName} {
		t.Errorf("keychain lookups at start = %v", spy.calls)
	}
	if err := loop.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	rebound, err := net.Listen("tcp", startup.WebhookAddr)
	if err != nil {
		t.Fatalf("the port was not freed by Close: %v", err)
	}
	_ = rebound.Close()

	off := f.setup(dir)
	off.Config.Webhook.Listen = ""
	loop, startup, err = daemon.Open(t.Context(), off)
	if err != nil {
		t.Fatalf("Open with the listener off: %v", err)
	}
	defer loop.Close()
	if startup.WebhookAddr != "" {
		t.Errorf("a listener is reported with webhook.listen empty: %q", startup.WebhookAddr)
	}
}

// An owner comment delivered by webhook starts its run while background
// discovery is exhausted: the hinted issue read and its comment listing take
// the operational lane. A backlog task in the same tick still waits.
func TestWebhookHintedCommentRunsWhileDiscoveryIsExhausted(t *testing.T) {
	w := newWebhookFixture(t)
	w.plantCursor()
	w.issue("Add a /health endpoint")
	w.forge.addIssue(fakeIssue{Number: testIssue + 1, Title: "Backlog", Body: "Nothing to do yet.", User: stranger()})
	w.daemon.WakeForTest(w.repo.ID, testIssue)
	w.daemon.WakeForTest(w.repo.ID, testIssue+1)
	w.settle() // first sight of both, with quota to spare
	if got := len(w.turns.calls()); got != 0 {
		t.Fatalf("first sight ran %d turns, want 0", got)
	}

	// Real quota stays above zero but under the floor, so every background
	// discovery read is held back.
	w.forge.mu.Lock()
	w.forge.quotaRemaining = 2000
	w.forge.mu.Unlock()
	backlog := w.sessionOf(testIssue + 1)
	if err := w.db.SetSessionNeedsPoll(t.Context(), backlog.ID); err != nil {
		t.Fatal(err)
	}
	w.forge.addComment(testIssue+1, 702, owner(), "Please add a readiness endpoint too.")
	w.forge.addComment(testIssue, 701, owner(), "Please add a health endpoint.")
	// Past the freshness window, so the tick's probe observes the low quota
	// before the hinted read.
	w.clock.advance(3 * time.Minute)
	w.daemon.WakeForTest(w.repo.ID, testIssue)
	w.settle()

	if got := w.turns.triggersFor(w.sessionOf(testIssue).ID); len(got) != 1 || got[0] != 701 {
		t.Fatalf("hinted comment triggers = %v, want [701]\n%s", got, w.logs)
	}
	if got := w.turns.triggersFor(backlog.ID); len(got) != 0 {
		t.Fatalf("backlog task ran %v under an exhausted budget", got)
	}
}
