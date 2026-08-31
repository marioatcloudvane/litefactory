package github_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
)

// Every test in this package drives a real net/http round trip against an
// httptest server. That is a deliberate choice over a fake transport: the
// things most likely to be wrong here — a header that is set on the wrong
// object, a Link header the parser does not recognise, a body that is encoded
// but never sent — are exactly the things a hand-rolled RoundTripper stub would
// paper over.
//
// It is a better choice since D1, not a worse one. The request shapes and the
// header parsing are go-github's now, and a RoundTripper fake would let this
// suite assert what this package *asked* for rather than what actually went on
// the wire. github.Options.HTTPClient is the seam, and it is the same one the
// daemon's fixture uses.

const (
	// testToken stands in for the bot credential. It is distinctive so that
	// "does this string appear anywhere it should not" is a decidable question
	// (see TestTokenNeverAppearsInLogsOrErrors).
	testToken = "ghp_botcredential_must_never_leak_0123456789"

	testUserAgent = "litefactory-test"
)

// testRepo is the repository every test polls unless it needs another.
var testRepo = github.Repo{Owner: "acme", Name: "acme-api"}

// capturedRequest is one request as the server received it.
type capturedRequest struct {
	Method string
	Path   string
	Query  url.Values
	Header http.Header
	Body   string
}

// JSON decodes the captured request body.
func (r capturedRequest) JSON(t *testing.T, out any) {
	t.Helper()
	if err := json.Unmarshal([]byte(r.Body), out); err != nil {
		t.Fatalf("decode request body %q: %v", r.Body, err)
	}
}

// fixture is an httptest server, a client pointed at it, a record of every
// request the server saw and everything the client logged.
type fixture struct {
	Client *github.Client
	Server *httptest.Server

	mu       sync.Mutex
	requests []capturedRequest
	logs     bytes.Buffer
}

// newFixture starts a server running handler and builds a client for it.
func newFixture(t *testing.T, handler http.HandlerFunc) *fixture {
	t.Helper()
	return newCustomFixture(t, handler, nil)
}

// newCustomFixture is newFixture with a hook that adjusts the client options
// after the defaults are filled in — an API base path prefix for the GitHub
// Enterprise shape, or a fixed clock for a retry-delay assertion.
func newCustomFixture(t *testing.T, handler http.HandlerFunc, customise func(*github.Options)) *fixture {
	t.Helper()

	f := &fixture{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			// Not Fatalf: this runs on the server's goroutine.
			t.Errorf("read request body: %v", err)
		}
		f.record(capturedRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  r.URL.Query(),
			Header: r.Header.Clone(),
			Body:   string(body),
		})
		handler(w, r)
	}))
	t.Cleanup(f.Server.Close)

	opts := github.Options{
		APIBase:    f.Server.URL,
		Token:      secrets.NewToken(testToken),
		HTTPClient: f.Server.Client(),
		// Debug level, so the request line the client emits on every call is
		// captured and can be searched for credential material.
		Logger:    slog.New(slog.NewJSONHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug})),
		UserAgent: testUserAgent,
	}
	if customise != nil {
		customise(&opts)
	}

	client, err := github.New(opts)
	if err != nil {
		t.Fatalf("github.New: %v", err)
	}
	f.Client = client
	return f
}

// Write makes the fixture the client's log sink.
func (f *fixture) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.logs.Write(p)
}

func (f *fixture) record(req capturedRequest) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
}

// Requests returns every request the server received, in order.
func (f *fixture) Requests() []capturedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]capturedRequest(nil), f.requests...)
}

// Only asserts that exactly one request was made and returns it.
func (f *fixture) Only(t *testing.T) capturedRequest {
	t.Helper()
	reqs := f.Requests()
	if len(reqs) != 1 {
		t.Fatalf("server received %d requests, want exactly 1", len(reqs))
	}
	return reqs[0]
}

// Logs returns everything the client has logged so far.
func (f *fixture) Logs() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.logs.String()
}

// respondJSON answers every request with the same status and body.
func respondJSON(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

// body builds a redact.Body the only way there is to build one. Tests cannot
// fabricate one, which is the entire point of the type — so PostComment's
// parameter is exercised through the real redaction pass here.
func body(t *testing.T, text string) redact.Body {
	t.Helper()
	return bodyWithLimit(t, text, github.CommentSizeLimit)
}

func bodyWithLimit(t *testing.T, text string, limit int) redact.Body {
	t.Helper()
	redacted, err := redact.New(redact.Options{Environ: []string{}}).Apply(t.Context(), text, limit)
	if err != nil {
		t.Fatalf("redact.Apply: %v", err)
	}
	return redacted
}

// assertNoToken fails if the credential appears in text. It is the assertion
// behind "the token is revealed at exactly one place and never recorded".
func assertNoToken(t *testing.T, where, text string) {
	t.Helper()
	if strings.Contains(text, testToken) {
		t.Errorf("the bot token appears in %s:\n%s", where, text)
	}
}
