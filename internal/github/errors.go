package github

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	gogithub "github.com/google/go-github/v90/github"
)

// Errors from this package read "github: <operation>: <detail>". The prefix is
// applied once, by whichever layer returns to the caller: the exported method
// for anything that came back from an SDK call, and the value check itself for
// a refused argument. Nothing below adds it a second time — and the SDK's own
// error strings, which name the method and the URL, never carry it either.

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

// The SDK is the transport; this file is the boundary.
//
// go-github reports failures with its own error types — *gogithub.ErrorResponse,
// *gogithub.RateLimitError, *gogithub.AbuseRateLimitError,
// *gogithub.TwoFactorAuthError, *gogithub.AcceptedError. None of them reaches a
// caller of this package. [classify] converts each one into the taxonomy above
// before it leaves, so adopting the SDK cost this package's callers nothing
// (AC-D1.7): internal/daemon still branches on *github.RateLimitError and
// internal/turn still reads github.ErrInvalidArgument, unchanged.
//
// Classification itself is deliberately *not* delegated to the SDK. It is
// derived here from the status code and the response headers, exactly as it was
// before the migration, because GitHub answers 403 for the hourly quota and 429
// for a secondary limit and the SDK only recognises the subset of those that
// carry the headers it looks for. Reading the headers ourselves keeps
// "come back later" from being reported as "the token cannot do this" — a
// difference worth a crash loop.

// apiFailure is what this package needs out of whichever error type the SDK
// produced: the response it came from, GitHub's own message, and the 422
// breakdown. Reaching for the response rather than the SDK error's own string
// is what keeps the message text this package emits under its own control.
type apiFailure struct {
	response         *http.Response
	message          string
	documentationURL string
	details          []ValidationDetail
	// secondary reports that the SDK recognised a secondary (abuse-detection)
	// rate limit from GitHub's documentation_url, which is more reliable than
	// matching on the message text.
	secondary bool
}

// failureOf reads an SDK error. The second result is false for anything that is
// not a GitHub API failure — a transport error, a cancelled context, a body
// that would not decode — none of which this package reshapes.
func failureOf(err error) (apiFailure, bool) {
	var errResp *gogithub.ErrorResponse
	if errors.As(err, &errResp) {
		return apiFailure{
			response:         errResp.Response,
			message:          errResp.Message,
			documentationURL: errResp.DocumentationURL,
			details:          validationDetails(errResp.Errors),
		}, true
	}

	// A 401 carrying an OTP challenge. litefactory authenticates with a token
	// and can never satisfy one, so it is a rejected credential like any other
	// 401; the distinct type exists only so the message survives.
	var twoFactor *gogithub.TwoFactorAuthError
	if errors.As(err, &twoFactor) {
		return apiFailure{
			response:         twoFactor.Response,
			message:          twoFactor.Message,
			documentationURL: twoFactor.DocumentationURL,
		}, true
	}

	var rateLimit *gogithub.RateLimitError
	if errors.As(err, &rateLimit) {
		return apiFailure{response: rateLimit.Response, message: rateLimit.Message}, true
	}

	var abuse *gogithub.AbuseRateLimitError
	if errors.As(err, &abuse) {
		return apiFailure{response: abuse.Response, message: abuse.Message, secondary: true}, true
	}

	// A 202 with an empty body, which the SDK reports as an error because the
	// work it describes has not happened yet. No endpoint this package calls
	// answers 202 — before the migration one would have decoded as an ordinary
	// success — but if one starts to, the caller must see this package's own
	// [APIError] rather than an SDK type. It carries no response of its own, so
	// the status and the URL come from the response the call returned.
	var accepted *gogithub.AcceptedError
	if errors.As(err, &accepted) {
		return apiFailure{}, true
	}

	return apiFailure{}, false
}

// classify turns an SDK error into the typed error for its status.
//
// resp is the SDK response the call returned, which is non-nil whenever there
// was an HTTP exchange at all; err is what the SDK reported. now is the clock,
// and is only read to turn X-RateLimit-Reset into a delay.
func classify(resp *gogithub.Response, err error, now time.Time) error {
	if err == nil {
		return nil
	}

	failure, isAPI := failureOf(err)
	if !isAPI {
		// Not a status this package classifies: a transport failure, a
		// cancelled context, an undecodable body. Returned as-is — the
		// *url.Error already names the method and the URL, repeating them here
		// would print both twice, and neither contains credential material
		// because the token travels in a header.
		return err
	}
	httpResp := failure.response
	if httpResp == nil && resp != nil {
		httpResp = resp.Response
	}
	if httpResp == nil {
		return err
	}

	base := APIError{
		Method:           requestMethod(httpResp),
		URL:              requestURL(httpResp),
		StatusCode:       httpResp.StatusCode,
		Message:          failure.message,
		DocumentationURL: failure.documentationURL,
		RequestID:        httpResp.Header.Get("X-GitHub-Request-Id"),
	}

	if isRateLimit(httpResp, failure) {
		base.Err = ErrRateLimited
		delay, reset := retryDelay(httpResp.Header, now)
		return &RateLimitError{
			APIError:   base,
			RetryAfter: delay,
			Reset:      reset,
			Limit:      headerInt(httpResp.Header, "X-RateLimit-Limit"),
			Remaining:  headerInt(httpResp.Header, "X-RateLimit-Remaining"),
			Resource:   httpResp.Header.Get("X-RateLimit-Resource"),
			Secondary: failure.secondary ||
				strings.Contains(strings.ToLower(failure.message), "secondary rate limit"),
		}
	}

	switch httpResp.StatusCode {
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
		return &ValidationError{APIError: base, Errors: failure.details}
	default:
		base.Err = ErrUnexpectedStatus
		return &base
	}
}

// isRateLimit decides whether a refusal is a rate limit. GitHub answers 429 for
// secondary limits and 403 for the hourly quota, and the 403 is only
// distinguishable from a permission failure by its headers — or, when even
// those are absent, by its message.
func isRateLimit(resp *http.Response, failure apiFailure) bool {
	if failure.secondary {
		return true
	}
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
		return strings.Contains(strings.ToLower(failure.message), "rate limit")
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

// validationDetails converts a 422 response's per-field breakdown.
//
// GitHub occasionally sends strings in that array instead of objects. The SDK
// absorbs the shape rather than failing on it, by unmarshalling such an entry
// into Error.Message alone — but an entry with no resource, no field and no
// code is not a breakdown, and it is the *code* the reporter branches on to
// tell "body is too long" from "issue is locked". Those entries are dropped,
// which is also what the hand-rolled decoder did before the SDK migration: the
// text is not lost, it is already on [APIError.Message].
func validationDetails(errs []gogithub.Error) []ValidationDetail {
	if len(errs) == 0 {
		return nil
	}
	details := make([]ValidationDetail, 0, len(errs))
	for _, e := range errs {
		if e.Resource == "" && e.Field == "" && e.Code == "" {
			continue
		}
		details = append(details, ValidationDetail{
			Resource: e.Resource,
			Field:    e.Field,
			Code:     e.Code,
			Message:  e.Message,
		})
	}
	if len(details) == 0 {
		return nil
	}
	return details
}
