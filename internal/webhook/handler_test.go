package webhook_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/webhook"
)

var testSecret = []byte("s3cret")

type fakeLookup map[string]struct {
	id     int64
	secret []byte
}

func (l fakeLookup) Lookup(fullName string) (int64, []byte, bool) {
	e, ok := l[fullName]
	return e.id, e.secret, ok
}

type hint struct {
	repo  int64
	issue int
}

type fakeSink struct {
	mu    sync.Mutex
	hints []hint
}

func (s *fakeSink) Wake(repo int64, issue int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hints = append(s.hints, hint{repo, issue})
}

func (s *fakeSink) got() []hint {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]hint(nil), s.hints...)
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type rig struct {
	h     *webhook.Handler
	sink  *fakeSink
	logs  *syncBuf
	clock *clock
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{sink: &fakeSink{}, logs: &syncBuf{}, clock: &clock{t: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)}}
	h, err := webhook.NewHandler(webhook.Options{
		Lookup: fakeLookup{"acme/api": {id: 7, secret: testSecret}, "acme/nosecret": {id: 8}},
		Sink:   r.sink,
		Logger: slog.New(slog.NewTextHandler(r.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Now:    r.clock.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	r.h = h
	return r
}

type delivery struct {
	method, path, event, id, signature string
	body                               []byte
	noSignature                        bool
}

func issueBody(repo string, number int) []byte {
	return []byte(fmt.Sprintf(`{"action":"opened","issue":{"number":%d},"repository":{"full_name":%q}}`, number, repo))
}

func signed(event, id, repo string, number int) delivery {
	body := issueBody(repo, number)
	return delivery{method: http.MethodPost, path: webhook.Path, event: event, id: id, signature: webhook.Sign(testSecret, body), body: body}
}

func (r *rig) do(d delivery) (int, string) {
	req := httptest.NewRequest(d.method, d.path, bytes.NewReader(d.body))
	if d.event != "" {
		req.Header.Set("X-GitHub-Event", d.event)
	}
	if d.id != "" {
		req.Header.Set("X-GitHub-Delivery", d.id)
	}
	if d.signature != "" && !d.noSignature {
		req.Header.Set("X-Hub-Signature-256", d.signature)
	}
	rec := httptest.NewRecorder()
	r.h.ServeHTTP(rec, req)
	body, _ := io.ReadAll(rec.Result().Body)
	return rec.Code, string(body)
}

// AC-001: path and method.
func TestPathAndMethod(t *testing.T) {
	r := newRig(t)
	d := signed("issues", "d1", "acme/api", 5)

	other := d
	other.path = "/other"
	if code, _ := r.do(other); code != http.StatusNotFound {
		t.Errorf("another path: %d, want 404", code)
	}
	for _, m := range []string{http.MethodGet, http.MethodPut, http.MethodDelete, http.MethodHead} {
		bad := d
		bad.method = m
		if code, _ := r.do(bad); code != http.StatusMethodNotAllowed {
			t.Errorf("%s: %d, want 405", m, code)
		}
	}
	if len(r.sink.got()) != 0 {
		t.Error("a refused request queued a hint")
	}
}

// AC-001: the body cap, with and without a declared length.
func TestBodyCap(t *testing.T) {
	r := newRig(t)
	big := bytes.Repeat([]byte("x"), webhook.MaxBodyBytes+1)
	d := delivery{method: http.MethodPost, path: webhook.Path, event: "issues", id: "d", body: big}
	if code, _ := r.do(d); code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversize body: %d, want 413", code)
	}

	// Chunked: no Content-Length for the early check to use.
	req := httptest.NewRequest(http.MethodPost, webhook.Path, io.NopCloser(bytes.NewReader(big)))
	req.ContentLength = -1
	req.Header.Set("X-GitHub-Event", "issues")
	req.Header.Set("X-GitHub-Delivery", "d")
	rec := httptest.NewRecorder()
	r.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversize chunked body: %d, want 413", rec.Code)
	}

	exact := signed("issues", "d2", "acme/api", 5)
	exact.body = append(exact.body[:len(exact.body)-1:len(exact.body)-1], bytes.Repeat([]byte(" "), webhook.MaxBodyBytes-len(exact.body)+1)...)
	exact.body = append(exact.body, '}')
	exact.body = exact.body[:webhook.MaxBodyBytes]
	exact.signature = webhook.Sign(testSecret, exact.body)
	if code, _ := r.do(exact); code == http.StatusRequestEntityTooLarge {
		t.Error("a body of exactly the cap was refused")
	}
}

// AC-001: required headers and bad JSON.
func TestHeadersAndJSON(t *testing.T) {
	r := newRig(t)
	good := signed("issues", "d1", "acme/api", 5)

	noEvent := good
	noEvent.event = ""
	noID := good
	noID.id = ""
	badJSON := good
	badJSON.body = []byte(`{"repository":`)
	badJSON.signature = webhook.Sign(testSecret, badJSON.body)
	for name, d := range map[string]delivery{"no event": noEvent, "no delivery id": noID, "bad json": badJSON} {
		if code, _ := r.do(d); code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", name, code)
		}
	}
}

// AC-002: every authentication failure answers the same bytes, queues nothing,
// records nothing and logs nothing at info or above.
func TestUnauthorizedIsIdenticalAndSilent(t *testing.T) {
	r := newRig(t)

	badSig := signed("issues", "d1", "acme/api", 5)
	badSig.signature = webhook.Sign([]byte("wrong"), badSig.body)
	missing := signed("issues", "d2", "acme/api", 5)
	missing.noSignature = true
	malformed := signed("issues", "d3", "acme/api", 5)
	malformed.signature = "sha256=zz"
	noPrefix := signed("issues", "d4", "acme/api", 5)
	noPrefix.signature = strings.TrimPrefix(noPrefix.signature, "sha256=")
	unknown := signed("issues", "d5", "evil/repo", 5)
	noSecret := signed("issues", "d6", "acme/nosecret", 5)
	noRepo := signed("issues", "d7", "acme/api", 5)
	noRepo.body = []byte(`{}`)
	noRepo.signature = webhook.Sign(testSecret, noRepo.body)

	var first string
	for name, d := range map[string]delivery{"bad": badSig, "missing": missing, "malformed": malformed, "no prefix": noPrefix, "unknown repo": unknown, "no secret": noSecret, "no repository": noRepo} {
		code, body := r.do(d)
		if code != http.StatusUnauthorized {
			t.Errorf("%s: %d, want 401", name, code)
		}
		if first == "" {
			first = body
		}
		if body != first {
			t.Errorf("%s: body %q differs from %q", name, body, first)
		}
	}
	if len(r.sink.got()) != 0 {
		t.Error("an unauthenticated request queued a hint")
	}
	for _, level := range []string{"level=INFO", "level=WARN", "level=ERROR"} {
		if strings.Contains(r.logs.String(), level) {
			t.Errorf("logged at %s before a valid signature:\n%s", level, r.logs)
		}
	}

	// A rejected delivery id was not recorded: the same id, now signed, queues.
	ok := signed("issues", "d1", "acme/api", 5)
	if code, _ := r.do(ok); code != http.StatusAccepted {
		t.Errorf("a signed delivery reusing a rejected id: %d, want 202", code)
	}
}

// AC-002: the repository match is case-insensitive.
func TestRepositoryMatchIgnoresCase(t *testing.T) {
	r := newRig(t)
	d := signed("issues", "d1", "ACME/Api", 5)
	if code, _ := r.do(d); code != http.StatusAccepted {
		t.Fatalf("mixed-case repository: %d, want 202", code)
	}
	if got := r.sink.got(); len(got) != 1 || got[0] != (hint{7, 5}) {
		t.Errorf("hints = %v", got)
	}
}

// AC-003: ping is 200, other events are 202, neither queues a hint.
func TestPingAndOtherEvents(t *testing.T) {
	r := newRig(t)
	if code, _ := r.do(signed("ping", "p1", "acme/api", 5)); code != http.StatusOK {
		t.Errorf("ping: %d, want 200", code)
	}
	for _, event := range []string{"push", "pull_request", "star", "workflow_run"} {
		if code, _ := r.do(signed(event, "e-"+event, "acme/api", 5)); code != http.StatusAccepted {
			t.Errorf("%s: %d, want 202", event, code)
		}
	}
	if len(r.sink.got()) != 0 {
		t.Errorf("ping or an ignored event queued hints: %v", r.sink.got())
	}
}

// AC-004: issue numbers by event.
func TestIssueNumbers(t *testing.T) {
	r := newRig(t)
	if code, _ := r.do(signed("issue_comment", "c1", "acme/api", 11)); code != http.StatusAccepted {
		t.Errorf("issue_comment: %d", code)
	}
	sub := signed("sub_issues", "s1", "acme/api", 0)
	sub.body = []byte(`{"action":"sub_issue_added","parent_issue":{"number":20},"sub_issue":{"number":21},"repository":{"full_name":"acme/api"},"sender":{"login":"x"}}`)
	sub.signature = webhook.Sign(testSecret, sub.body)
	if code, _ := r.do(sub); code != http.StatusAccepted {
		t.Errorf("sub_issues: %d", code)
	}
	want := []hint{{7, 11}, {7, 20}, {7, 21}}
	got := r.sink.got()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("hints = %v, want %v", got, want)
	}

	// A signed event that names no issue is 202 with no hint and no recorded id.
	none := signed("issues", "n1", "acme/api", 0)
	none.body = []byte(`{"action":"x","repository":{"full_name":"acme/api"}}`)
	none.signature = webhook.Sign(testSecret, none.body)
	if code, _ := r.do(none); code != http.StatusAccepted {
		t.Errorf("no number: %d, want 202", code)
	}
	zero := signed("issues", "n2", "acme/api", 0)
	if code, _ := r.do(zero); code != http.StatusAccepted {
		t.Errorf("number 0: %d, want 202", code)
	}
	if len(r.sink.got()) != len(want) {
		t.Errorf("a signed event without a number queued a hint: %v", r.sink.got())
	}
}

// AC-005: duplicates are 200 with no second hint, until the id expires.
func TestDuplicateDelivery(t *testing.T) {
	r := newRig(t)
	d := signed("issues", "dup", "acme/api", 5)
	if code, _ := r.do(d); code != http.StatusAccepted {
		t.Fatalf("first: %d", code)
	}
	if code, _ := r.do(d); code != http.StatusOK {
		t.Errorf("duplicate: %d, want 200", code)
	}
	if len(r.sink.got()) != 1 {
		t.Errorf("hints = %v, want one", r.sink.got())
	}
	r.clock.advance(webhook.DeliveryIDTTL + time.Second)
	if code, _ := r.do(d); code != http.StatusAccepted {
		t.Errorf("after the id expired: %d, want 202", code)
	}
	if len(r.sink.got()) != 2 {
		t.Errorf("hints = %v, want two", r.sink.got())
	}
}

// AC-005: the id memory is bounded and evicts the oldest first.
func TestDeliveryIDMemoryIsBounded(t *testing.T) {
	r := newRig(t)
	for i := 0; i < webhook.MaxDeliveryIDs+5; i++ {
		d := signed("issues", fmt.Sprintf("id-%d", i), "acme/api", 5)
		if code, _ := r.do(d); code != http.StatusAccepted {
			t.Fatalf("delivery %d: %d", i, code)
		}
		r.clock.advance(time.Millisecond)
	}
	// The newest is still remembered; the oldest has been evicted.
	if code, _ := r.do(signed("issues", fmt.Sprintf("id-%d", webhook.MaxDeliveryIDs+4), "acme/api", 5)); code != http.StatusOK {
		t.Errorf("newest id not remembered: %d", code)
	}
	if code, _ := r.do(signed("issues", "id-0", "acme/api", 5)); code != http.StatusAccepted {
		t.Errorf("oldest id not evicted: %d", code)
	}
}

// AC-005: the log carries only delivery, event, repo, issue and outcome, and
// never the payload or the signature.
func TestLogsCarryNoPayload(t *testing.T) {
	r := newRig(t)
	d := signed("issues", "log-1", "acme/api", 42)
	d.body = []byte(`{"action":"opened","issue":{"number":42,"title":"TOPSECRETTITLE","body":"TOPSECRETBODY"},"repository":{"full_name":"acme/api"}}`)
	d.signature = webhook.Sign(testSecret, d.body)
	if code, _ := r.do(d); code != http.StatusAccepted {
		t.Fatalf("%d", code)
	}
	logs := r.logs.String()
	for _, want := range []string{"log-1", "issues", "acme/api", "42", "queued"} {
		if !strings.Contains(logs, want) {
			t.Errorf("log lacks %q:\n%s", want, logs)
		}
	}
	for _, banned := range []string{"TOPSECRET", d.signature, string(testSecret)} {
		if strings.Contains(logs, banned) {
			t.Errorf("log contains %q:\n%s", banned, logs)
		}
	}
}

// AC-002: rejection logs are rate limited.
func TestRejectionLogsAreRateLimited(t *testing.T) {
	r := newRig(t)
	d := signed("issues", "x", "acme/api", 5)
	d.signature = "sha256=" + strings.Repeat("0", 64)
	for range 50 {
		r.do(d)
	}
	if n := strings.Count(r.logs.String(), "webhook request rejected"); n != 1 {
		t.Errorf("%d rejection lines for 50 rejections inside one window, want 1", n)
	}
	r.clock.advance(time.Minute)
	r.do(d)
	if n := strings.Count(r.logs.String(), "webhook request rejected"); n != 2 {
		t.Errorf("%d rejection lines after the window, want 2", n)
	}
	if !strings.Contains(r.logs.String(), "suppressed_since_last=49") {
		t.Errorf("the suppressed count was not reported:\n%s", r.logs)
	}
}

// Concurrent redeliveries of one id queue exactly one hint.
func TestConcurrentDuplicatesQueueOnce(t *testing.T) {
	r := newRig(t)
	d := signed("issues", "same", "acme/api", 5)
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.do(d)
		}()
	}
	wg.Wait()
	if got := r.sink.got(); len(got) != 1 {
		t.Errorf("hints = %v, want one", got)
	}
}

func TestNewHandlerValidatesOptions(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	for name, opts := range map[string]webhook.Options{
		"no lookup": {Sink: &fakeSink{}, Logger: log},
		"no sink":   {Lookup: fakeLookup{}, Logger: log},
		"no logger": {Lookup: fakeLookup{}, Sink: &fakeSink{}},
	} {
		if _, err := webhook.NewHandler(opts); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// AC-001, AC-009: a real listener serves the handler, rejects a taken port and
// shuts down gracefully.
func TestServerServesAndShutsDown(t *testing.T) {
	r := newRig(t)
	ln, err := webhook.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := webhook.Listen(ln.Addr().String()); err == nil || !strings.Contains(err.Error(), ln.Addr().String()) {
		t.Errorf("binding a taken port: %v, want an error naming the address", err)
	}
	srv := webhook.NewServer(ln, r.h)
	served := make(chan error, 1)
	go func() { served <- srv.Serve() }()

	d := signed("issues", "real", "acme/api", 9)
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://"+srv.Addr().String()+webhook.Path, bytes.NewReader(d.body))
	req.Header.Set("X-GitHub-Event", d.event)
	req.Header.Set("X-GitHub-Delivery", d.id)
	req.Header.Set("X-Hub-Signature-256", d.signature)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status %d", resp.StatusCode)
	}

	if err := srv.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if err := <-served; err != nil {
		t.Errorf("Serve returned %v after a graceful shutdown", err)
	}
	if err := srv.Close(); err != nil {
		t.Errorf("Close after Shutdown: %v", err)
	}
	if got := r.sink.got(); len(got) != 1 || got[0] != (hint{7, 9}) {
		t.Errorf("hints = %v", got)
	}
}
