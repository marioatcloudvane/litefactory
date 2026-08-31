package github

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	gogithub "github.com/google/go-github/v90/github"

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

	// perPage is the maximum GitHub allows. Fewer per page means more
	// requests per tick, which is exactly what AC-2.8 bounds.
	perPage = 100

	// maxPages bounds one paginated call. 100 pages of 100 is 10,000 items
	// from a single endpoint; anything beyond that is a bug or a repository
	// litefactory has no business polling, and continuing would burn the
	// hourly quota in one tick.
	maxPages = 100
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
// It is a concrete struct wrapping the SDK client, deliberately not an
// interface: the Forge protocol is still LF-18's to extract from a second
// working implementation, and adopting a dependency did not change that
// (see the package documentation, and TestNoForgeAbstraction).
//
// It is safe for concurrent use and it retries nothing: a [RateLimitError]
// comes straight back to the caller with the delay GitHub asked for, and the
// daemon decides what to do about it.
type Client struct {
	// api is the SDK client. It is a field rather than an embedded type on
	// purpose: embedding would promote the SDK's entire method set onto
	// [Client], which is the surface the package guards.
	api *gogithub.Client
	// base is the API root as this package was configured with it, without a
	// trailing slash. The SDK keeps its own normalised copy (which must end in
	// "/"); this one exists for [Client.BaseURL], whose output goes in a
	// startup log line.
	base      *url.URL
	log       *slog.Logger
	userAgent string
	now       func() time.Time
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
		base:      base,
		log:       opts.Logger,
		userAgent: opts.UserAgent,
		now:       opts.Now,
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

	// The SDK normalises the base itself and requires a trailing slash, which
	// parseURL adds; passing the trimmed form keeps the two copies agreeing on
	// everything but that slash.
	sdkBase := base.String() + "/"
	options := []gogithub.ClientOptionsFunc{
		gogithub.WithURLs(&sdkBase, nil),
		gogithub.WithUserAgent(c.userAgent),
		// Client-side rate-limit bookkeeping is off. The SDK will otherwise
		// remember a 403/429 and refuse the *next* call without asking GitHub,
		// which is a form of backoff — and backoff below the daemon is exactly
		// what this package does not do (see the package documentation). The
		// daemon owns the wait because only it knows what else is queued.
		gogithub.WithDisableRateLimitCheck(),
	}
	if opts.HTTPClient != nil {
		options = append(options, gogithub.WithHTTPClient(opts.HTTPClient))
	} else {
		options = append(options, gogithub.WithTimeout(DefaultTimeout))
	}
	// The one and only place the credential is revealed. WithAuthToken is the
	// SDK's single credential path: it installs a RoundTripper that sets the
	// Authorization header on a *clone* of each request, so the token reaches a
	// header and never a URL, a log attribute or an error. Go's http.Client
	// strips that header on a cross-host redirect, so a redirected request
	// cannot carry it off github.com. TestRevealIsCalledExactlyOnce fails the
	// build if this line moves or gains a sibling.
	options = append(options, gogithub.WithAuthToken(opts.Token.Reveal()))

	api, err := gogithub.NewClient(options...)
	if err != nil {
		// The SDK's construction errors name the option that failed and never
		// echo the token: WithAuthToken's only failure is "token must not be
		// empty", which cannot be reached past the Empty() check above.
		return nil, fmt.Errorf("github: build api client for %q: %w", base.String(), err)
	}
	c.api = api
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

// clock reads the client's clock, tolerating a [Client] that [New] did not
// build for the same reason [Client.logger] does.
func (c *Client) clock() time.Time {
	if c == nil || c.now == nil {
		return time.Now()
	}
	return c.now()
}

// do performs one SDK call: it refuses an uninitialised client, logs the
// request, and translates whatever the SDK returned into this package's error
// taxonomy.
//
// Every endpoint in this package goes through here, which is what makes "one
// HTTP stack, one error taxonomy, one credential path" checkable by reading the
// call sites rather than by trusting them.
func do[T any](ctx context.Context, c *Client, call func(context.Context) (T, *gogithub.Response, error)) (T, *gogithub.Response, error) {
	var zero T
	// A zero Client has no SDK client to call. Falling through would panic,
	// and a nil-safe fall-through would report "no results" from a client that
	// never made a request — indistinguishable from a quiet repository, and
	// enough to advance a watermark over unseen comments.
	if c == nil || c.api == nil {
		return zero, nil, ErrUninitialized
	}

	start := c.clock()
	value, resp, err := call(ctx)
	c.observe(ctx, resp, start)
	if err != nil {
		return zero, resp, classify(resp, err, c.clock())
	}
	return value, resp, nil
}

// observe writes the per-request debug record. It is the line that makes "what
// did this tick actually cost" answerable, and it carries no credential: the
// method, URL and status are all safe to print, and the token travels in a
// header this package never reads back.
func (c *Client) observe(ctx context.Context, resp *gogithub.Response, start time.Time) {
	if resp == nil || resp.Response == nil {
		return
	}
	c.logger().DebugContext(ctx, "github: api request",
		slog.String("method", requestMethod(resp.Response)),
		slog.String("url", requestURL(resp.Response)),
		slog.Int("status", resp.StatusCode),
		slog.Duration("duration", c.clock().Sub(start)),
		slog.Int("rate_limit_remaining", headerInt(resp.Header, "X-RateLimit-Remaining")),
	)
}

// requestMethod and requestURL read back what was sent. net/http fills in
// Response.Request for every real round trip; both tolerate its absence rather
// than making a diagnostic the reason a tick panics.
func requestMethod(resp *http.Response) string {
	if resp == nil || resp.Request == nil {
		return ""
	}
	return resp.Request.Method
}

func requestURL(resp *http.Response) string {
	if resp == nil || resp.Request == nil || resp.Request.URL == nil {
		return ""
	}
	return resp.Request.URL.String()
}
