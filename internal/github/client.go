package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/secrets"
)

// Tunables. They are constants rather than configuration because none of them
// is a decision an operator should have to make.
const (
	// DefaultTimeout bounds one HTTP request, start to finish. A tick that
	// hangs on a black-holed connection is worse than a tick that fails: the
	// daemon looks alive and does nothing.
	DefaultTimeout = 30 * time.Second

	// DefaultUserAgent identifies litefactory to GitHub. The API requires a
	// User-Agent and rejects requests without one.
	DefaultUserAgent = "litefactory"

	// apiVersion pins the REST API version, so a future default change at
	// GitHub is an explicit upgrade here rather than a surprise on a Tuesday.
	apiVersion = "2022-11-28"

	acceptJSON      = "application/vnd.github+json"
	contentTypeJSON = "application/json"

	// perPage is the maximum GitHub allows. Fewer per page means more
	// requests per tick, which is exactly what AC-2.8 bounds.
	perPage = 100

	// maxPages bounds one paginated call. 100 pages of 100 is 10,000 items
	// from a single endpoint; anything beyond that is a bug or a repository
	// litefactory has no business polling, and continuing would burn the
	// hourly quota in one tick.
	maxPages = 100

	// maxResponseBytes caps a decoded response body.
	maxResponseBytes = 32 << 20
	// maxErrorBytes caps the body read from a failed response.
	maxErrorBytes = 64 << 10
	// maxDrainBytes caps what is drained from an unread body to let the
	// connection be reused.
	maxDrainBytes = 64 << 10
)

// Options configures [New].
type Options struct {
	// APIBase is the REST API root; empty means https://api.github.com, which
	// is also config.DefaultAPIBase.
	APIBase string
	// Token is the bot credential. Required: this client is the bot's voice
	// and has no anonymous mode.
	Token secrets.Token
	// HTTPClient overrides the transport. Nil means a client with
	// [DefaultTimeout]. Tests point this at an httptest server, and so does
	// anything that needs a proxy.
	HTTPClient *http.Client
	// Logger receives request-level debug records and the per-repo listing
	// summary. Nil discards.
	Logger *slog.Logger
	// UserAgent overrides [DefaultUserAgent] — main appends the build version.
	UserAgent string
	// Now is the clock, and exists so a test can assert on a retry delay
	// computed from X-RateLimit-Reset. Nil means time.Now. This mirrors the
	// function-field seam in internal/secrets: one thing to substitute, no
	// interface worth naming.
	Now func() time.Time
}

// Client talks to one GitHub API base as one identity.
//
// It is safe for concurrent use — it holds no mutable state — and it retries
// nothing: a [RateLimitError] comes straight back to the caller with the delay
// GitHub asked for, and the daemon decides what to do about it.
type Client struct {
	base       *url.URL
	httpClient *http.Client
	token      secrets.Token
	log        *slog.Logger
	userAgent  string
	now        func() time.Time
}

// New builds a client.
//
// A malformed API base or a missing token is an error here rather than at the
// first request, so a misconfigured daemon fails at startup where someone is
// watching.
func New(opts Options) (*Client, error) {
	rawBase := strings.TrimSpace(opts.APIBase)
	if rawBase == "" {
		rawBase = defaultAPIBase
	}
	base, err := url.Parse(strings.TrimRight(rawBase, "/"))
	if err != nil {
		return nil, fmt.Errorf("github: api base %q: %w", rawBase, err)
	}
	if base.Scheme != "http" && base.Scheme != "https" {
		return nil, fmt.Errorf("github: api base %q: %w: needs an http or https scheme", rawBase, ErrInvalidArgument)
	}
	if base.Host == "" {
		return nil, fmt.Errorf("github: api base %q: %w: no host", rawBase, ErrInvalidArgument)
	}
	if opts.Token.Empty() {
		return nil, fmt.Errorf("github: %w: no bot token", ErrInvalidArgument)
	}

	c := &Client{
		base:       base,
		httpClient: opts.HTTPClient,
		token:      opts.Token,
		log:        opts.Logger,
		userAgent:  opts.UserAgent,
		now:        opts.Now,
	}
	if c.httpClient == nil {
		c.httpClient = &http.Client{Timeout: DefaultTimeout}
	}
	if c.log == nil {
		c.log = slog.New(slog.DiscardHandler)
	}
	if c.userAgent == "" {
		c.userAgent = DefaultUserAgent
	}
	if c.now == nil {
		c.now = time.Now
	}
	return c, nil
}

// defaultAPIBase repeats config.DefaultAPIBase rather than importing it: this
// package is below config in the stack, and the value is a protocol constant.
const defaultAPIBase = "https://api.github.com"

// BaseURL returns the API root this client talks to. Useful in a startup log
// line, where "which host am I actually calling" is otherwise invisible.
func (c *Client) BaseURL() string {
	if c == nil || c.base == nil {
		return ""
	}
	return c.base.String()
}

// logger returns the client's logger, or a discarding one for a [Client] that
// [New] did not build. Every log site in this package goes through it, so a
// zero value refuses its calls rather than panicking inside one.
func (c *Client) logger() *slog.Logger {
	if c == nil || c.log == nil {
		return slog.New(slog.DiscardHandler)
	}
	return c.log
}

// response is what [Client.do] reports about a successful call beyond the
// decoded body.
type response struct {
	// NextPage is the rel="next" URL from the Link header, empty on the last
	// page. It has already been checked to point at this client's own host.
	NextPage string
}

// endpoint builds an absolute URL for an API path.
//
// path is interpolated, not escaped: every component that reaches it has been
// through [Repo.Validate] or is a number, and those checks reject anything that
// would need escaping. That is deliberate — url.URL re-escapes a pre-escaped
// Path, so pre-escaping here would produce %252F in the wire request.
//
// A client [New] did not build has no base to interpolate into, and returns the
// empty string rather than panicking — [Client.do] turns that into
// [ErrUninitialized].
func (c *Client) endpoint(path string, query url.Values) string {
	if c == nil || c.base == nil {
		return ""
	}
	u := *c.base
	u.Path = strings.TrimRight(c.base.Path, "/") + path
	if len(query) > 0 {
		u.RawQuery = query.Encode()
	}
	return u.String()
}

// do performs one request and decodes its body.
//
// rawURL is absolute: either from [Client.endpoint] or a Link header that
// [Client.nextPage] has already confirmed points at this client's host. in is
// JSON-encoded as the request body when non-nil; out receives the decoded
// response when non-nil.
func (c *Client) do(ctx context.Context, method, rawURL string, in, out any) (response, error) {
	if c == nil || c.httpClient == nil || c.base == nil || rawURL == "" {
		return response{}, ErrUninitialized
	}

	var body io.Reader
	if in != nil {
		payload, err := json.Marshal(in)
		if err != nil {
			return response{}, fmt.Errorf("encode %s %s body: %w", method, rawURL, err)
		}
		body = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return response{}, fmt.Errorf("build %s %s: %w", method, rawURL, err)
	}
	req.Header.Set("Accept", acceptJSON)
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	req.Header.Set("User-Agent", c.userAgent)
	if in != nil {
		req.Header.Set("Content-Type", contentTypeJSON)
	}
	// The one and only place the credential is revealed. It goes into a
	// header, never a URL and never a log attribute; Go's http.Client strips
	// this header on a cross-host redirect, so a redirected request cannot
	// carry it off github.com.
	req.Header.Set("Authorization", "Bearer "+c.token.Reveal())

	start := c.now()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Returned as-is: the *url.Error already names the method and the URL,
		// and repeating them here would print both twice. Neither contains
		// credential material — the token travels in a header.
		return response{}, err
	}
	defer func() {
		// Draining before the close lets the connection go back in the pool
		// instead of being torn down. The Close call is written out here rather
		// than hidden in a helper so that it is visible to a reader and to the
		// linter that checks for it.
		drain(resp.Body)
		_ = resp.Body.Close()
	}()

	c.logger().DebugContext(ctx, "github: api request",
		slog.String("method", method),
		slog.String("url", rawURL),
		slog.Int("status", resp.StatusCode),
		slog.Duration("duration", c.now().Sub(start)),
		slog.Int("rate_limit_remaining", headerInt(resp.Header, "X-RateLimit-Remaining")),
	)

	var res response
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		// A truncated or unreadable error body still leaves the status, which
		// is what classification actually turns on.
		errBody, readErr := io.ReadAll(io.LimitReader(resp.Body, maxErrorBytes))
		if readErr != nil {
			errBody = nil
		}
		return res, classifyResponse(method, rawURL, resp, errBody, c.now())
	}

	next, err := c.nextPage(resp.Header.Get("Link"))
	if err != nil {
		return res, err
	}
	res.NextPage = next

	if out == nil || resp.StatusCode == http.StatusNoContent {
		return res, nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(out); err != nil {
		if errors.Is(err, io.EOF) {
			// 2xx with no body at all: the caller asked for a decode, GitHub
			// had nothing to say. Leave out at its zero value.
			return res, nil
		}
		return res, fmt.Errorf("decode %s %s response: %w", method, rawURL, err)
	}
	return res, nil
}

// drain reads a bounded amount of an unfinished response body. Whatever is left
// unread — the tail of an error body past [maxErrorBytes], or a body nobody
// asked to decode — would otherwise cost the connection.
func drain(body io.Reader) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, maxDrainBytes))
}
