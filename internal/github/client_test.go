package github_test

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/secrets"
)

const userJSON = `{"id": 4242, "login": "litefactory-bot", "type": "Bot"}`

// TestRequestCarriesTheCredentialAndAPIHeaders is the positive half of the
// credential story: the Authorization header is built from the token, and it is
// built correctly. The negative half is TestTokenNeverAppearsInLogsOrErrors.
func TestRequestCarriesTheCredentialAndAPIHeaders(t *testing.T) {
	t.Parallel()

	f := newFixture(t, respondJSON(http.StatusOK, userJSON))
	if _, err := f.Client.AuthenticatedUser(t.Context()); err != nil {
		t.Fatalf("AuthenticatedUser: %v", err)
	}

	req := f.Only(t)
	if got, want := req.Header.Get("Authorization"), "Bearer "+testToken; got != want {
		t.Errorf("Authorization header = %q, want %q", got, want)
	}
	if got, want := req.Header.Get("Accept"), "application/vnd.github+json"; got != want {
		t.Errorf("Accept header = %q, want %q", got, want)
	}
	if got, want := req.Header.Get("X-GitHub-Api-Version"), "2022-11-28"; got != want {
		t.Errorf("X-GitHub-Api-Version header = %q, want %q", got, want)
	}
	if got, want := req.Header.Get("User-Agent"), testUserAgent; got != want {
		t.Errorf("User-Agent header = %q, want %q", got, want)
	}
	if got, want := req.Path, "/user"; got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
}

// TestCredentialIsNeverPlacedInTheURL guards the other half of "the token goes
// in a header": a query parameter would end up in the server's access log, in
// the client's own debug line, and in every error this package formats.
func TestCredentialIsNeverPlacedInTheURL(t *testing.T) {
	t.Parallel()

	f := newFixture(t, respondJSON(http.StatusOK, `[]`))
	if _, err := f.Client.ListIssues(t.Context(), testRepo, time.Time{}); err != nil {
		t.Fatalf("ListIssues: %v", err)
	}

	req := f.Only(t)
	assertNoToken(t, "the request path", req.Path)
	assertNoToken(t, "the request query", req.Query.Encode())
}

// TestTokenNeverAppearsInLogsOrErrors runs every failure shape this package
// produces and asserts that none of them records the credential — not in the
// error the daemon will log and post about, and not in the debug line the
// client writes for every request.
//
// daemon.log is unredacted by design (S-7), so this package must not put a
// credential into it in the first place.
func TestTokenNeverAppearsInLogsOrErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		status  int
		headers map[string]string
		body    string
	}{
		{name: "ok", status: http.StatusOK, body: userJSON},
		{name: "unauthorized", status: http.StatusUnauthorized, body: `{"message": "Bad credentials"}`},
		{name: "forbidden", status: http.StatusForbidden, body: `{"message": "Resource not accessible"}`},
		{name: "not found", status: http.StatusNotFound, body: `{"message": "Not Found"}`},
		{name: "unprocessable", status: http.StatusUnprocessableEntity, body: `{"message": "Validation Failed"}`},
		{
			name:    "rate limited",
			status:  http.StatusTooManyRequests,
			headers: map[string]string{"Retry-After": "30"},
			body:    `{"message": "You have exceeded a secondary rate limit"}`,
		},
		{name: "server error", status: http.StatusInternalServerError, body: `{"message": "oops"}`},
		{name: "undecodable body", status: http.StatusOK, body: `<html>not json</html>`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t, func(w http.ResponseWriter, _ *http.Request) {
				for name, value := range tc.headers {
					w.Header().Set(name, value)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})

			_, err := f.Client.AuthenticatedUser(t.Context())
			if err != nil {
				assertNoToken(t, "the returned error", err.Error())
			}
			logs := f.Logs()
			if logs == "" {
				t.Fatal("the client logged nothing, so this test proves nothing")
			}
			assertNoToken(t, "the client's log output", logs)
		})
	}
}

// TestTransportFailureDoesNotLeakTheCredential covers the path where there is
// no response at all: the error comes from net/http rather than from this
// package, and it carries the request URL.
func TestTransportFailureDoesNotLeakTheCredential(t *testing.T) {
	t.Parallel()

	f := newFixture(t, respondJSON(http.StatusOK, userJSON))
	f.Server.Close()

	_, err := f.Client.AuthenticatedUser(t.Context())
	if err == nil {
		t.Fatal("AuthenticatedUser against a closed server returned no error")
	}
	assertNoToken(t, "the transport error", err.Error())
	if !strings.HasPrefix(err.Error(), "github: ") {
		t.Errorf("error %q does not start with the package prefix", err)
	}
}

// TestStatusMapping pins the classification every caller branches on. The
// daemon treats 401 as fatal, 404 as "the token cannot see this repo", 422 as a
// bad body and a rate limit as "come back later"; conflating any two of them
// produces either a crash loop or a silent stall.
func TestStatusMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		status   int
		headers  map[string]string
		body     string
		sentinel error
		as       func(error) bool
	}{
		{
			name:     "401 is a rejected credential",
			status:   http.StatusUnauthorized,
			body:     `{"message": "Bad credentials"}`,
			sentinel: github.ErrUnauthorized,
			as: func(err error) bool {
				var typed *github.UnauthorizedError
				return errors.As(err, &typed)
			},
		},
		{
			name:     "403 without rate headers is a permission failure",
			status:   http.StatusForbidden,
			body:     `{"message": "Resource not accessible by personal access token"}`,
			sentinel: github.ErrForbidden,
			as: func(err error) bool {
				var typed *github.ForbiddenError
				return errors.As(err, &typed)
			},
		},
		{
			name:     "404 is not found",
			status:   http.StatusNotFound,
			body:     `{"message": "Not Found"}`,
			sentinel: github.ErrNotFound,
			as: func(err error) bool {
				var typed *github.NotFoundError
				return errors.As(err, &typed)
			},
		},
		{
			name:     "422 is unprocessable",
			status:   http.StatusUnprocessableEntity,
			body:     `{"message": "Validation Failed"}`,
			sentinel: github.ErrUnprocessable,
			as: func(err error) bool {
				var typed *github.ValidationError
				return errors.As(err, &typed)
			},
		},
		{
			name:     "429 is a rate limit",
			status:   http.StatusTooManyRequests,
			headers:  map[string]string{"Retry-After": "12"},
			body:     `{"message": "slow down"}`,
			sentinel: github.ErrRateLimited,
			as: func(err error) bool {
				var typed *github.RateLimitError
				return errors.As(err, &typed)
			},
		},
		{
			name:     "403 with an exhausted quota is a rate limit",
			status:   http.StatusForbidden,
			headers:  map[string]string{"X-RateLimit-Remaining": "0"},
			body:     `{"message": "API rate limit exceeded"}`,
			sentinel: github.ErrRateLimited,
			as: func(err error) bool {
				var typed *github.RateLimitError
				return errors.As(err, &typed)
			},
		},
		{
			name:     "500 is an unexpected status",
			status:   http.StatusInternalServerError,
			body:     `{"message": "server error"}`,
			sentinel: github.ErrUnexpectedStatus,
			as: func(err error) bool {
				var typed *github.APIError
				return errors.As(err, &typed)
			},
		},
		{
			name:     "a non-JSON error body still classifies",
			status:   http.StatusNotFound,
			body:     `<html><body>nginx</body></html>`,
			sentinel: github.ErrNotFound,
			as: func(err error) bool {
				var typed *github.NotFoundError
				return errors.As(err, &typed)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t, func(w http.ResponseWriter, _ *http.Request) {
				for name, value := range tc.headers {
					w.Header().Set(name, value)
				}
				w.Header().Set("X-GitHub-Request-Id", "REQ-1")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})

			_, err := f.Client.GetIssue(t.Context(), testRepo, 7)
			if err == nil {
				t.Fatalf("GetIssue against a %d returned no error", tc.status)
			}
			if !errors.Is(err, tc.sentinel) {
				t.Errorf("errors.Is(%v, %v) = false", err, tc.sentinel)
			}
			if !tc.as(err) {
				t.Errorf("error %v is not the expected concrete type", err)
			}

			// Every failure also unwraps to the shared detail, which is what a
			// report renders.
			var api *github.APIError
			if !errors.As(err, &api) {
				t.Fatalf("errors.As(%v, *github.APIError) = false", err)
			}
			if api.StatusCode != tc.status {
				t.Errorf("APIError.StatusCode = %d, want %d", api.StatusCode, tc.status)
			}
			if api.Method != http.MethodGet {
				t.Errorf("APIError.Method = %q, want GET", api.Method)
			}
			if !strings.Contains(api.URL, "/repos/acme/acme-api/issues/7") {
				t.Errorf("APIError.URL = %q, want the request URL", api.URL)
			}
			if api.RequestID != "REQ-1" {
				t.Errorf("APIError.RequestID = %q, want REQ-1", api.RequestID)
			}
		})
	}
}

// TestRateLimitRetryAfter is AC-2.12's contract with the daemon: the delay is a
// duration on a typed error, not a number the daemon has to scrape out of a
// message. Every source GitHub uses for that number is covered, plus the two
// nonsense cases that would otherwise spin the loop or park it for a day.
func TestRateLimitRetryAfter(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	// resetHeader renders X-RateLimit-Reset relative to the fixed clock, and
	// resetAt is the instant the client should report for it. Deriving both
	// from `now` keeps the epoch arithmetic out of the table.
	resetHeader := func(d time.Duration) string {
		return strconv.FormatInt(now.Add(d).Unix(), 10)
	}
	resetAt := func(d time.Duration) time.Time { return now.Add(d).UTC() }

	tests := []struct {
		name      string
		status    int
		headers   map[string]string
		body      string
		want      time.Duration
		wantReset time.Time
		secondary bool
	}{
		{
			name:    "Retry-After in seconds",
			status:  http.StatusTooManyRequests,
			headers: map[string]string{"Retry-After": "45"},
			want:    45 * time.Second,
		},
		{
			name:    "Retry-After as an HTTP date",
			status:  http.StatusTooManyRequests,
			headers: map[string]string{"Retry-After": now.Add(90 * time.Second).Format(http.TimeFormat)},
			want:    90 * time.Second,
		},
		{
			name:   "X-RateLimit-Reset when there is no Retry-After",
			status: http.StatusForbidden,
			headers: map[string]string{
				"X-RateLimit-Remaining": "0",
				"X-RateLimit-Reset":     resetHeader(600 * time.Second),
			},
			body:      `{"message": "API rate limit exceeded"}`,
			want:      600 * time.Second,
			wantReset: resetAt(600 * time.Second),
		},
		{
			name:   "Retry-After wins over X-RateLimit-Reset",
			status: http.StatusTooManyRequests,
			headers: map[string]string{
				"Retry-After":       "5",
				"X-RateLimit-Reset": resetHeader(600 * time.Second),
			},
			want: 5 * time.Second,
			// The reset instant is still reported: it is the end of the window,
			// which is a different fact from "when to try again".
			wantReset: resetAt(600 * time.Second),
		},
		{
			name:   "no timing information at all falls back to the default",
			status: http.StatusForbidden,
			headers: map[string]string{
				"X-RateLimit-Remaining": "0",
			},
			body: `{"message": "API rate limit exceeded"}`,
			want: github.DefaultRetryAfter,
		},
		{
			name:   "a reset in the past still produces a wait",
			status: http.StatusForbidden,
			headers: map[string]string{
				"X-RateLimit-Remaining": "0",
				"X-RateLimit-Reset":     resetHeader(-1000 * time.Second),
			},
			body:      `{"message": "API rate limit exceeded"}`,
			want:      github.DefaultRetryAfter,
			wantReset: resetAt(-1000 * time.Second),
		},
		{
			name:    "an absurd Retry-After is capped",
			status:  http.StatusTooManyRequests,
			headers: map[string]string{"Retry-After": "864000"},
			want:    github.MaxRetryAfter,
		},
		{
			name:      "a secondary limit is distinguished",
			status:    http.StatusForbidden,
			headers:   map[string]string{"Retry-After": "60"},
			body:      `{"message": "You have exceeded a secondary rate limit. Please wait."}`,
			want:      60 * time.Second,
			secondary: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newCustomFixture(t, func(w http.ResponseWriter, _ *http.Request) {
				for name, value := range tc.headers {
					w.Header().Set(name, value)
				}
				w.Header().Set("X-RateLimit-Limit", "5000")
				w.Header().Set("X-RateLimit-Resource", "core")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}, func(opts *github.Options) {
				opts.Now = func() time.Time { return now }
			})

			_, err := f.Client.ListIssues(t.Context(), testRepo, time.Time{})

			var limited *github.RateLimitError
			if !errors.As(err, &limited) {
				t.Fatalf("ListIssues error = %v, want a *github.RateLimitError", err)
			}
			if limited.RetryAfter != tc.want {
				t.Errorf("RetryAfter = %s, want %s", limited.RetryAfter, tc.want)
			}
			if limited.RetryAfter <= 0 {
				t.Error("RetryAfter must always be positive; a zero delay spins the daemon")
			}
			if !limited.Reset.Equal(tc.wantReset) {
				t.Errorf("Reset = %s, want %s", limited.Reset, tc.wantReset)
			}
			if limited.Secondary != tc.secondary {
				t.Errorf("Secondary = %t, want %t", limited.Secondary, tc.secondary)
			}
			if limited.Limit != 5000 {
				t.Errorf("Limit = %d, want 5000", limited.Limit)
			}
			if limited.Resource != "core" {
				t.Errorf("Resource = %q, want core", limited.Resource)
			}
			if !errors.Is(err, github.ErrRateLimited) {
				t.Errorf("errors.Is(%v, ErrRateLimited) = false", err)
			}
		})
	}
}

// TestRateLimitReportsAbsentQuotaHeadersAsUnknown keeps "the quota is exhausted"
// distinguishable from "GitHub did not say", which a zero value would not.
func TestRateLimitReportsAbsentQuotaHeadersAsUnknown(t *testing.T) {
	t.Parallel()

	f := newFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "10")
		w.WriteHeader(http.StatusTooManyRequests)
	})

	_, err := f.Client.ListIssues(t.Context(), testRepo, time.Time{})
	var limited *github.RateLimitError
	if !errors.As(err, &limited) {
		t.Fatalf("ListIssues error = %v, want a *github.RateLimitError", err)
	}
	if limited.Limit != -1 || limited.Remaining != -1 {
		t.Errorf("Limit/Remaining = %d/%d, want -1/-1 when the headers are absent",
			limited.Limit, limited.Remaining)
	}
}

// TestValidationErrorCarriesTheFieldBreakdown matters because 422 is what an
// over-length or otherwise refused comment body comes back as, and the report
// layer needs to tell "body too long" from "issue is locked".
func TestValidationErrorCarriesTheFieldBreakdown(t *testing.T) {
	t.Parallel()

	f := newFixture(t, respondJSON(http.StatusUnprocessableEntity, `{
		"message": "Validation Failed",
		"documentation_url": "https://docs.github.com/rest",
		"errors": [{"resource": "IssueComment", "field": "body", "code": "too_long"}]
	}`))

	_, err := f.Client.PostComment(t.Context(), testRepo, 7, body(t, "hello"))
	var invalid *github.ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("PostComment error = %v, want a *github.ValidationError", err)
	}
	if len(invalid.Errors) != 1 || invalid.Errors[0].Code != "too_long" {
		t.Fatalf("Errors = %+v, want one entry with code too_long", invalid.Errors)
	}
	if invalid.DocumentationURL == "" {
		t.Error("DocumentationURL is empty, want GitHub's own link")
	}
}

// TestValidationErrorSurvivesAnUnexpectedErrorsShape covers GitHub occasionally
// sending strings in the errors array. Losing the breakdown is acceptable;
// losing the whole error is not.
func TestValidationErrorSurvivesAnUnexpectedErrorsShape(t *testing.T) {
	t.Parallel()

	f := newFixture(t, respondJSON(http.StatusUnprocessableEntity,
		`{"message": "Validation Failed", "errors": ["body is too long"]}`))

	_, err := f.Client.PostComment(t.Context(), testRepo, 7, body(t, "hello"))
	var invalid *github.ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("PostComment error = %v, want a *github.ValidationError", err)
	}
	if invalid.Message != "Validation Failed" {
		t.Errorf("Message = %q, want GitHub's message", invalid.Message)
	}
	if len(invalid.Errors) != 0 {
		t.Errorf("Errors = %+v, want none from an unrecognised shape", invalid.Errors)
	}
}

// TestNewValidatesItsOptions moves a misconfiguration to startup, where someone
// is watching, instead of the first tick at three in the morning.
func TestNewValidatesItsOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts github.Options
		want error
	}{
		{
			name: "no token",
			opts: github.Options{APIBase: "https://api.github.com"},
			want: github.ErrInvalidArgument,
		},
		{
			name: "a base that is not a URL",
			opts: github.Options{APIBase: "://nope", Token: secrets.NewToken(testToken)},
		},
		{
			name: "a base with no scheme",
			opts: github.Options{APIBase: "api.github.com", Token: secrets.NewToken(testToken)},
			want: github.ErrInvalidArgument,
		},
		{
			name: "a base with a non-http scheme",
			opts: github.Options{APIBase: "ftp://api.github.com", Token: secrets.NewToken(testToken)},
			want: github.ErrInvalidArgument,
		},
		{
			name: "a base with no host",
			opts: github.Options{APIBase: "https://", Token: secrets.NewToken(testToken)},
			want: github.ErrInvalidArgument,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client, err := github.New(tc.opts)
			if err == nil {
				t.Fatalf("New(%+v) returned a client, want an error", tc.opts)
			}
			if client != nil {
				t.Error("New returned a non-nil client alongside an error")
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Errorf("errors.Is(%v, %v) = false", err, tc.want)
			}
			assertNoToken(t, "a construction error", err.Error())
		})
	}
}

// TestNewDefaultsToTheGitHubAPI keeps the zero-config path honest: an empty
// APIBase must reach github.com, not "".
func TestNewDefaultsToTheGitHubAPI(t *testing.T) {
	t.Parallel()

	client, err := github.New(github.Options{Token: secrets.NewToken(testToken)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got, want := client.BaseURL(), "https://api.github.com"; got != want {
		t.Errorf("BaseURL() = %q, want %q", got, want)
	}
}

// TestAPIBasePathPrefixIsPreserved covers the GitHub Enterprise shape. It is
// not supported at M0, but a base path that silently disappeared would produce
// a 404 nobody could explain.
func TestAPIBasePathPrefixIsPreserved(t *testing.T) {
	t.Parallel()

	f := newCustomFixture(t, respondJSON(http.StatusOK, userJSON), func(opts *github.Options) {
		opts.APIBase += "/api/v3/"
	})

	if _, err := f.Client.AuthenticatedUser(t.Context()); err != nil {
		t.Fatalf("AuthenticatedUser: %v", err)
	}
	if got, want := f.Only(t).Path, "/api/v3/user"; got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
}

// TestZeroClientIsInert covers what the type system cannot. github.Client is an
// exported struct, so &github.Client{} compiles; it must refuse rather than
// panic or, worse, issue an unauthenticated request.
func TestZeroClientIsInert(t *testing.T) {
	t.Parallel()

	clients := map[string]*github.Client{
		"nil":  nil,
		"zero": {},
	}

	for name, client := range clients {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := client.BaseURL(); got != "" {
				t.Errorf("BaseURL() = %q, want empty", got)
			}

			calls := map[string]func() error{
				"AuthenticatedUser": func() error {
					_, err := client.AuthenticatedUser(t.Context())
					return err
				},
				"ListIssues": func() error {
					_, err := client.ListIssues(t.Context(), testRepo, time.Time{})
					return err
				},
				"GetIssue": func() error {
					_, err := client.GetIssue(t.Context(), testRepo, 7)
					return err
				},
				"ListCommentsAfter": func() error {
					_, err := client.ListCommentsAfter(t.Context(), testRepo, 7, 0)
					return err
				},
				"PostComment": func() error {
					_, err := client.PostComment(t.Context(), testRepo, 7, body(t, "hi"))
					return err
				},
				"AddCommentReaction": func() error {
					return client.AddCommentReaction(t.Context(), testRepo, 99, github.ReactionEyes)
				},
			}
			for method, call := range calls {
				if err := call(); !errors.Is(err, github.ErrUninitialized) {
					t.Errorf("%s on a %s client: error = %v, want ErrUninitialized", method, name, err)
				}
			}
		})
	}
}

// TestContextCancellationStopsARequest is what makes SIGTERM-within-3-seconds
// possible (AC-2.1): a tick blocked on a hung API call has to unwind from the
// root context, not from the HTTP timeout.
func TestContextCancellationStopsARequest(t *testing.T) {
	t.Parallel()

	released := make(chan struct{})
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		close(released)
	})

	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err := f.Client.ListIssues(ctx, testRepo, time.Time{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ListIssues error = %v, want context.Canceled", err)
	}
	select {
	case <-released:
	case <-time.After(5 * time.Second):
		t.Error("the server never saw the request cancelled")
	}
}

// TestErrorsAreNamespacedExactlyOnce keeps the failure text readable. Every
// error this package returns identifies the package once, at the front —
// "github: list issues for acme/x: GET …: 403", never "github: …: github: …".
func TestErrorsAreNamespacedExactlyOnce(t *testing.T) {
	t.Parallel()

	f := newFixture(t, respondJSON(http.StatusNotFound, `{"message": "Not Found"}`))
	badRepo := github.Repo{Owner: "acme", Name: "a/b"}

	calls := map[string]func() error{
		"AuthenticatedUser": func() error {
			_, err := f.Client.AuthenticatedUser(t.Context())
			return err
		},
		"ListIssues": func() error {
			_, err := f.Client.ListIssues(t.Context(), testRepo, time.Time{})
			return err
		},
		"GetIssue": func() error {
			_, err := f.Client.GetIssue(t.Context(), testRepo, 7)
			return err
		},
		"ListCommentsAfter": func() error {
			_, err := f.Client.ListCommentsAfter(t.Context(), testRepo, 7, 0)
			return err
		},
		"PostComment": func() error {
			_, err := f.Client.PostComment(t.Context(), testRepo, 7, body(t, "hi"))
			return err
		},
		"AddCommentReaction": func() error {
			return f.Client.AddCommentReaction(t.Context(), testRepo, 99, github.ReactionEyes)
		},
		"a refused repository name": func() error {
			_, err := f.Client.GetIssue(t.Context(), badRepo, 7)
			return err
		},
		"a refused issue number": func() error {
			_, err := f.Client.GetIssue(t.Context(), testRepo, 0)
			return err
		},
	}

	for name, call := range calls {
		err := call()
		if err == nil {
			t.Errorf("%s returned no error", name)
			continue
		}
		message := err.Error()
		if !strings.HasPrefix(message, "github: ") {
			t.Errorf("%s: error %q does not start with %q", name, message, "github: ")
		}
		if strings.Count(message, "github: ") != 1 {
			t.Errorf("%s: error %q names the package more than once", name, message)
		}
	}
}
