package github_test

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gogithub "github.com/google/go-github/v90/github"

	"github.com/cloudvane/litefactory/internal/github"
)

// This file is the boundary between litefactory and go-github, asserted rather
// than assumed (D1). Everything here would have passed trivially before the
// migration; each test exists because the SDK could plausibly change the
// answer, and every one of these answers is something a caller outside this
// package depends on.

// TestNoSDKErrorTypeReachesACaller is AC-D1.7 in executable form. internal/daemon
// branches on *github.RateLimitError and internal/turn on
// github.ErrInvalidArgument; neither imports go-github, and neither should have
// to. A go-github error leaking through here would be a silent contract change
// — the caller's errors.As stops matching and the daemon stops backing off.
func TestNoSDKErrorTypeReachesACaller(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		status  int
		headers map[string]string
		body    string
	}{
		{name: "401", status: http.StatusUnauthorized, body: `{"message": "Bad credentials"}`},
		{name: "403", status: http.StatusForbidden, body: `{"message": "Resource not accessible"}`},
		{name: "404", status: http.StatusNotFound, body: `{"message": "Not Found"}`},
		{name: "422", status: http.StatusUnprocessableEntity, body: `{"message": "Validation Failed"}`},
		{
			name:    "429",
			status:  http.StatusTooManyRequests,
			headers: map[string]string{"Retry-After": "30"},
			body:    `{"message": "slow down"}`,
		},
		{
			// The shape go-github itself recognises as a primary rate limit,
			// so this is the case where its own *RateLimitError is what has to
			// be translated rather than passed on.
			name:    "403 over quota",
			status:  http.StatusForbidden,
			headers: map[string]string{"X-RateLimit-Remaining": "0"},
			body:    `{"message": "API rate limit exceeded"}`,
		},
		{
			// The shape go-github recognises as a *secondary* limit, by the
			// documentation_url rather than by any header.
			name:   "403 secondary",
			status: http.StatusForbidden,
			body: `{"message": "You have exceeded a secondary rate limit",
			        "documentation_url": "https://docs.github.com/rest/using-the-rest-api/rate-limits-for-the-rest-api#about-secondary-rate-limits"}`,
		},
		{name: "500", status: http.StatusInternalServerError, body: `{"message": "oops"}`},
		{
			// go-github reports 202 as an error because the work has not
			// happened yet. It carries no response of its own, which is the
			// case most likely to fall through a translation layer.
			name:   "202",
			status: http.StatusAccepted,
		},
		{
			// A 401 carrying an OTP challenge, which go-github gives its own
			// type. A token-authenticated client can never satisfy one.
			name:    "401 with a two-factor challenge",
			status:  http.StatusUnauthorized,
			headers: map[string]string{"X-GitHub-OTP": "required; app"},
			body:    `{"message": "Must specify two-factor authentication OTP code."}`,
		},
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

			_, err := f.Client.GetIssue(t.Context(), testRepo, 7)
			if err == nil {
				t.Fatalf("GetIssue against a %d returned no error", tc.status)
			}

			leaks := map[string]func(error) bool{
				"*gogithub.ErrorResponse":       func(e error) bool { return errors.As(e, new(*gogithub.ErrorResponse)) },
				"*gogithub.RateLimitError":      func(e error) bool { return errors.As(e, new(*gogithub.RateLimitError)) },
				"*gogithub.AbuseRateLimitError": func(e error) bool { return errors.As(e, new(*gogithub.AbuseRateLimitError)) },
				"*gogithub.TwoFactorAuthError":  func(e error) bool { return errors.As(e, new(*gogithub.TwoFactorAuthError)) },
				"*gogithub.AcceptedError":       func(e error) bool { return errors.As(e, new(*gogithub.AcceptedError)) },
			}
			for name, matches := range leaks {
				if matches(err) {
					t.Errorf("the error unwraps to %s; go-github's types must not cross "+
						"this package's boundary: %v", name, err)
				}
			}

			// The positive half: it is one of ours, and it carries the detail a
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
			assertNoToken(t, "the returned error", err.Error())
		})
	}
}

// TestASecondaryRateLimitTheSDKRecognisesIsStillOurRateLimitError covers the
// one classification go-github makes from the documentation_url rather than
// from a header. It has to arrive as a rate limit with a positive delay, or the
// daemon treats a two-minute pause as a permanent permission failure.
func TestASecondaryRateLimitTheSDKRecognisesIsStillOurRateLimitError(t *testing.T) {
	t.Parallel()

	f := newFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "42")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message": "You have exceeded a secondary rate limit",
			"documentation_url": "https://docs.github.com/rest/using-the-rest-api/rate-limits-for-the-rest-api#about-secondary-rate-limits"}`))
	})

	_, err := f.Client.ListIssues(t.Context(), testRepo, time.Time{})

	var limited *github.RateLimitError
	if !errors.As(err, &limited) {
		t.Fatalf("ListIssues error = %v, want a *github.RateLimitError", err)
	}
	if !limited.Secondary {
		t.Error("Secondary = false; a secondary limit clears in seconds and a primary one " +
			"can take the best part of an hour")
	}
	if limited.RetryAfter != 42*time.Second {
		t.Errorf("RetryAfter = %s, want 42s", limited.RetryAfter)
	}
	if !errors.Is(err, github.ErrRateLimited) {
		t.Errorf("errors.Is(%v, ErrRateLimited) = false", err)
	}
}

// TestARateLimitDoesNotStopTheNextCall pins the reason [New] switches
// go-github's client-side rate-limit bookkeeping off.
//
// Left on, the SDK remembers a 403/429 and refuses the *next* call without
// asking GitHub until the reset it recorded. That is backoff, and backoff below
// the daemon is what this package does not do: the daemon owns the wait because
// only it knows what else is queued. It also decides *when* to try again, so a
// second client-side clock would silently override its schedule — and every
// subsequent tick would fail without a request, which reads in the log exactly
// like GitHub still refusing.
func TestARateLimitDoesNotStopTheNextCall(t *testing.T) {
	t.Parallel()

	// atomic: net/http may hand the two requests to different goroutines.
	var refused atomic.Bool
	f := newFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		if refused.CompareAndSwap(false, true) {
			w.Header().Set("X-RateLimit-Remaining", "0")
			// An hour out, so a client-side memory of it would swallow every
			// call this test could make.
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10))
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message": "API rate limit exceeded"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"number": 3, "title": "after the wait"}]`))
	})

	if _, err := f.Client.ListIssues(t.Context(), testRepo, time.Time{}); !errors.Is(err, github.ErrRateLimited) {
		t.Fatalf("the first call's error = %v, want ErrRateLimited", err)
	}

	issues, err := f.Client.ListIssues(t.Context(), testRepo, time.Time{})
	if err != nil {
		t.Fatalf("the second call was refused without reaching GitHub: %v", err)
	}
	if len(issues) != 1 || issues[0].Number != 3 {
		t.Fatalf("issues = %+v, want the single issue the server served", issues)
	}
	if got := len(f.Requests()); got != 2 {
		t.Errorf("the server saw %d requests, want 2 — the second call must be made, "+
			"not answered from a client-side rate-limit memory", got)
	}
}

// TestEveryEndpointGoesThroughTheSDK is AC-D1.8's greppable half: no
// hand-rolled net/http request survives in this package, and none of the
// Link-header helpers the SDK replaced is still sitting there dead.
//
// It reads the sources rather than the behaviour because that is where the
// mistake would be made — an "it's only one call" helper added later, which no
// endpoint test would notice, and which would be a second HTTP stack and
// therefore a second credential path.
func TestEveryEndpointGoesThroughTheSDK(t *testing.T) {
	t.Parallel()

	// net/http itself is still imported, for status constants and for the
	// *http.Response the SDK hands back. What must not reappear is anything
	// that builds or sends a request.
	handRolled := []string{
		"http.NewRequest",
		"http.NewRequestWithContext",
		"http.DefaultClient",
		"http.Get(",
		"http.Post(",
		".RoundTrip(",
	}
	// The Link-header reader, removed by D1. A dead helper left behind is how
	// the next reader concludes the package still hand-rolls pagination.
	removed := []string{
		"func nextLink(",
		"func cutLinkValue(",
		"func hasRel(",
		"func (c *Client) nextPage(",
		"func (c *Client) endpoint(",
		"func (c *Client) do(",
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		contents, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for i, line := range strings.Split(string(contents), "\n") {
			for _, needle := range handRolled {
				if strings.Contains(line, needle) {
					t.Errorf("%s:%d builds or sends an HTTP request by hand (%s): %s\n\n"+
						"One HTTP stack means one credential path, which is the whole "+
						"reason D1 was a full migration rather than a hybrid.",
						name, i+1, needle, strings.TrimSpace(line))
				}
			}
			for _, needle := range removed {
				if strings.Contains(line, needle) {
					t.Errorf("%s:%d declares %s, which D1 removed", name, i+1, needle)
				}
			}
		}
	}
}
