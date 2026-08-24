package github

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Errors from this package read "github: <operation>: <detail>". The prefix is
// applied once, by whichever layer returns to the caller: the exported method
// for anything that came out of [Client.do], and the value check itself for a
// refused argument. Nothing below adds it a second time.

// Sentinel conditions, for errors.Is. Every one of them is also reachable
// through a typed error below; they exist so a caller that only needs to know
// *which* failure happened does not have to name a type.
var (
	// ErrUninitialized reports a [Client] that [New] did not build. The zero
	// value is inert rather than silently unauthenticated.
	ErrUninitialized = errors.New("client was not produced by New")

	// ErrInvalidArgument reports a value this package refused to send: an empty
	// repository owner, a non-positive issue number, an empty comment body.
	ErrInvalidArgument = errors.New("invalid argument")

	// ErrUnauthorized reports HTTP 401 — the credential was rejected. The bot
	// token is missing, revoked or malformed; retrying will not help.
	ErrUnauthorized = errors.New("credential rejected")

	// ErrForbidden reports HTTP 403 that is not a rate limit: the credential is
	// valid but not permitted to do this, usually a missing repository scope on
	// a fine-grained PAT.
	ErrForbidden = errors.New("forbidden")

	// ErrNotFound reports HTTP 404. On GitHub this is also what a private
	// resource returns to a token that cannot see it, so it does not always
	// mean the resource is absent.
	ErrNotFound = errors.New("not found")

	// ErrUnprocessable reports HTTP 422 — the request was understood and
	// rejected. An over-length comment body arrives here.
	ErrUnprocessable = errors.New("unprocessable")

	// ErrRateLimited reports a primary or secondary rate limit: HTTP 429, or
	// HTTP 403 carrying rate-limit headers. See [RateLimitError].
	ErrRateLimited = errors.New("rate limited")

	// ErrUnexpectedStatus reports any other non-2xx response, including 5xx.
	ErrUnexpectedStatus = errors.New("unexpected status")

	// ErrTooManyPages reports a list endpoint that kept offering a next page
	// past [maxPages]. Truncating silently would drop comments, so this is an
	// error rather than a short result.
	ErrTooManyPages = errors.New("too many pages")
)

// APIError is the detail shared by every non-2xx response.
//
// The method, URL, status and GitHub's own message are fields rather than an
// already-formatted string because callers print them and tests assert on them.
// None of them can contain the credential: the token travels in a header, never
// in a URL, and GitHub does not echo it.
//
// The typed errors below embed this value and unwrap to it, so
//
//	var apiErr *github.APIError
//	errors.As(err, &apiErr)
//
// works for any failure this package produces, while errors.Is against the
// sentinels above tells the cases apart.
type APIError struct {
	// Method is the HTTP method that failed.
	Method string
	// URL is the absolute request URL.
	URL string
	// StatusCode is the HTTP status.
	StatusCode int
	// Message is GitHub's "message" field, empty if the body was not JSON.
	Message string
	// DocumentationURL is GitHub's "documentation_url" field, if any.
	DocumentationURL string
	// RequestID is the x-github-request-id header, which is what GitHub
	// support asks for.
	RequestID string
	// Err is the sentinel this status maps to.
	Err error
}

func (e *APIError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s: %d %s", e.Method, e.URL, e.StatusCode, http.StatusText(e.StatusCode))
	if e.Message != "" {
		b.WriteString(": ")
		b.WriteString(e.Message)
	}
	if e.RequestID != "" {
		fmt.Fprintf(&b, " (request id %s)", e.RequestID)
	}
	return b.String()
}

func (e *APIError) Unwrap() error { return e.Err }

// UnauthorizedError reports HTTP 401: the bot credential was rejected.
type UnauthorizedError struct{ APIError }

func (e *UnauthorizedError) Unwrap() error { return &e.APIError }

// ForbiddenError reports HTTP 403 without rate-limit headers: the credential
// is valid but lacks permission for this resource.
type ForbiddenError struct{ APIError }

func (e *ForbiddenError) Unwrap() error { return &e.APIError }

// NotFoundError reports HTTP 404, which on GitHub also covers "exists but this
// token cannot see it".
type NotFoundError struct{ APIError }

func (e *NotFoundError) Unwrap() error { return &e.APIError }

// ValidationError reports HTTP 422. GitHub returns a per-field breakdown for
// these, which is worth surfacing: "body is too long" and "issue is locked"
// need different handling by the reporter.
type ValidationError struct {
	APIError
	// Errors is GitHub's per-field breakdown, empty when it sent none.
	Errors []ValidationDetail
}

func (e *ValidationError) Unwrap() error { return &e.APIError }

// ValidationDetail is one entry of a 422 response's "errors" array.
type ValidationDetail struct {
	Resource string `json:"resource"`
	Field    string `json:"field"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

// RateLimitError reports that GitHub refused the call because of a rate limit
// (AC-2.12).
//
// RetryAfter is the point of this type. The daemon backs off by that duration
// and resumes; it must not have to parse a duration back out of a message
// string, and a bare sentinel would not carry the number at all. The value is
// always positive: it comes from Retry-After when present, otherwise from
// X-RateLimit-Reset, otherwise from [DefaultRetryAfter].
type RateLimitError struct {
	APIError
	// RetryAfter is how long to wait before trying again. Always > 0.
	RetryAfter time.Duration
	// Reset is when the limit window rolls over, zero if GitHub did not say.
	Reset time.Time
	// Limit and Remaining are the quota headers, -1 when absent.
	Limit     int
	Remaining int
	// Resource names the quota that was exhausted ("core", "search", …),
	// empty when absent.
	Resource string
	// Secondary reports a secondary (abuse-detection) rate limit rather than
	// the hourly quota. Secondary limits clear in seconds, primary ones can
	// take the best part of an hour.
	Secondary bool
}

func (e *RateLimitError) Error() string {
	kind := "rate limited"
	if e.Secondary {
		kind = "secondary rate limited"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s: %d %s: %s, retry after %s",
		e.Method, e.URL, e.StatusCode, http.StatusText(e.StatusCode), kind, e.RetryAfter)
	if e.Message != "" {
		b.WriteString(": ")
		b.WriteString(e.Message)
	}
	return b.String()
}

func (e *RateLimitError) Unwrap() error { return &e.APIError }

// Bounds on a computed retry delay. A missing header must not mean "retry
// immediately", and a nonsense one must not park the daemon for a day.
const (
	// DefaultRetryAfter is used when a rate-limit response carries no usable
	// timing information at all.
	DefaultRetryAfter = 60 * time.Second
	// MinRetryAfter is the floor for a computed delay.
	MinRetryAfter = time.Second
	// MaxRetryAfter is the ceiling for a computed delay. GitHub's primary
	// window is an hour, so nothing legitimate exceeds it.
	MaxRetryAfter = time.Hour
)

// errorPayload is GitHub's JSON error shape.
type errorPayload struct {
	Message          string          `json:"message"`
	DocumentationURL string          `json:"documentation_url"`
	Errors           json.RawMessage `json:"errors"`
}

// classifyResponse turns a non-2xx response into the typed error for its
// status. body is whatever could be read of the response body, possibly empty
// or truncated; classification never depends on it succeeding.
func classifyResponse(method, url string, resp *http.Response, body []byte, now time.Time) error {
	var payload errorPayload
	if len(body) > 0 {
		// A non-JSON body (an HTML error page from a proxy, say) leaves the
		// message empty; the status is still authoritative.
		if err := json.Unmarshal(body, &payload); err != nil {
			payload = errorPayload{}
		}
	}

	base := APIError{
		Method:           method,
		URL:              url,
		StatusCode:       resp.StatusCode,
		Message:          payload.Message,
		DocumentationURL: payload.DocumentationURL,
		RequestID:        resp.Header.Get("X-GitHub-Request-Id"),
	}

	if isRateLimit(resp, payload.Message) {
		base.Err = ErrRateLimited
		delay, reset := retryDelay(resp.Header, now)
		return &RateLimitError{
			APIError:   base,
			RetryAfter: delay,
			Reset:      reset,
			Limit:      headerInt(resp.Header, "X-RateLimit-Limit"),
			Remaining:  headerInt(resp.Header, "X-RateLimit-Remaining"),
			Resource:   resp.Header.Get("X-RateLimit-Resource"),
			Secondary:  strings.Contains(strings.ToLower(payload.Message), "secondary rate limit"),
		}
	}

	switch resp.StatusCode {
	case http.StatusUnauthorized:
		base.Err = ErrUnauthorized
		return &UnauthorizedError{APIError: base}
	case http.StatusForbidden:
		base.Err = ErrForbidden
		return &ForbiddenError{APIError: base}
	case http.StatusNotFound:
		base.Err = ErrNotFound
		return &NotFoundError{APIError: base}
	case http.StatusUnprocessableEntity:
		base.Err = ErrUnprocessable
		return &ValidationError{APIError: base, Errors: validationDetails(payload.Errors)}
	default:
		base.Err = ErrUnexpectedStatus
		return &base
	}
}

// isRateLimit decides whether a refusal is a rate limit. GitHub answers 429 for
// secondary limits and 403 for the hourly quota, and the 403 is only
// distinguishable from a permission failure by its headers — or, when even
// those are absent, by its message.
func isRateLimit(resp *http.Response, message string) bool {
	if resp.StatusCode == http.StatusTooManyRequests {
		return true
	}
	if resp.StatusCode != http.StatusForbidden {
		return false
	}
	switch {
	case resp.Header.Get("Retry-After") != "":
		return true
	case resp.Header.Get("X-RateLimit-Remaining") == "0":
		return true
	default:
		return strings.Contains(strings.ToLower(message), "rate limit")
	}
}

// retryDelay computes how long to wait, and the reset instant when GitHub
// supplied one. Retry-After wins because it is what GitHub explicitly asks for
// on secondary limits; X-RateLimit-Reset is the fallback for primary ones.
func retryDelay(h http.Header, now time.Time) (time.Duration, time.Time) {
	var (
		delay time.Duration
		reset time.Time
	)

	if raw := strings.TrimSpace(h.Get("X-RateLimit-Reset")); raw != "" {
		if secs, err := strconv.ParseInt(raw, 10, 64); err == nil && secs > 0 {
			reset = time.Unix(secs, 0).UTC()
			delay = reset.Sub(now)
		}
	}

	if raw := strings.TrimSpace(h.Get("Retry-After")); raw != "" {
		// The header is defined as either a delta in seconds or an HTTP-date.
		// GitHub sends the delta; a proxy in the way may rewrite it.
		if secs, err := strconv.Atoi(raw); err == nil {
			delay = time.Duration(secs) * time.Second
		} else if at, err := http.ParseTime(raw); err == nil {
			delay = at.Sub(now)
		}
	}

	return clampRetry(delay), reset
}

// clampRetry keeps a computed delay inside sane bounds. A reset timestamp in
// the past — clock skew, or a response that sat in a proxy — must still produce
// a wait, or the daemon spins.
func clampRetry(d time.Duration) time.Duration {
	switch {
	case d <= 0:
		return DefaultRetryAfter
	case d < MinRetryAfter:
		return MinRetryAfter
	case d > MaxRetryAfter:
		return MaxRetryAfter
	default:
		return d
	}
}

// headerInt reads an integer header, returning -1 when it is absent or
// unparseable, so "zero remaining" stays distinguishable from "not reported".
func headerInt(h http.Header, name string) int {
	raw := strings.TrimSpace(h.Get(name))
	if raw == "" {
		return -1
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return -1
	}
	return n
}

// validationDetails decodes a 422 body's "errors" array. GitHub occasionally
// sends strings there instead of objects, so a decode failure yields no
// details rather than losing the whole error.
func validationDetails(raw json.RawMessage) []ValidationDetail {
	if len(raw) == 0 {
		return nil
	}
	var details []ValidationDetail
	if err := json.Unmarshal(raw, &details); err != nil {
		return nil
	}
	return details
}
