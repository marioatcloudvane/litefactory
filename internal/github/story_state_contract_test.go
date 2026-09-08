package github_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
)

func TestSetStoryStateClosesCompletedAndReopens(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, state, reason string
		completed           bool
	}{{"complete", "closed", "completed", true}, {"invalidate", "open", "reopened", false}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/acme-api/issues/42":
					respondJSON(200, `{"id":9005,"number":42,"state":"open"}`)(w, r)
				case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/acme-api/issues/42/parent":
					respondJSON(200, `{"id":9001,"number":141}`)(w, r)
				case r.Method == http.MethodPatch && r.URL.Path == "/repos/acme/acme-api/issues/42":
					respondJSON(200, `{"id":9005,"number":42,"state":"`+tc.state+`"}`)(w, r)
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
				}
			})
			if err := f.Client.SetStoryState(t.Context(), testRepo, 141, 9001, 42, 9005, tc.completed); err != nil {
				t.Fatal(err)
			}
			requests := f.Requests()
			if len(requests) != 3 {
				t.Fatalf("requests=%d, want verified child, parent, patch", len(requests))
			}
			var patch struct {
				State  string `json:"state"`
				Reason string `json:"state_reason"`
			}
			requests[2].JSON(t, &patch)
			if patch.State != tc.state || patch.Reason != tc.reason {
				t.Fatalf("state patch=%+v", patch)
			}
			var fields map[string]json.RawMessage
			requests[2].JSON(t, &fields)
			if len(fields) != 2 {
				t.Fatalf("unexpected fields in state-only update: %s", requests[2].Body)
			}
		})
	}
}

func TestSetStoryStateRefusesChangedIdentityBeforeMutation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, child, parent string }{
		{"child replaced", `{"id":9999,"number":42}`, `{"id":9001,"number":141}`},
		{"parent replaced", `{"id":9005,"number":42}`, `{"id":9999,"number":141}`},
		{"parent moved", `{"id":9005,"number":42}`, `{"id":9001,"number":142}`},
		{"parent is pull request", `{"id":9005,"number":42}`, `{"id":9001,"number":141,"pull_request":{"url":"https://example.test/pr"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("mutation after changed identity: %s", r.Method)
					http.NotFound(w, r)
					return
				}
				if r.URL.Path == "/repos/acme/acme-api/issues/42/parent" {
					respondJSON(200, tc.parent)(w, r)
				} else {
					respondJSON(200, tc.child)(w, r)
				}
			})
			if err := f.Client.SetStoryState(t.Context(), testRepo, 141, 9001, 42, 9005, true); err == nil {
				t.Fatal("changed relationship accepted")
			}
		})
	}
}

func TestSetStoryStateRejectsUnconfirmedUpdate(t *testing.T) {
	t.Parallel()
	for _, body := range []string{`null`, `{"id":9999,"number":42,"state":"closed"}`, `{"id":9005,"number":43,"state":"closed"}`, `{"id":9005,"number":42,"state":"open"}`, `{`} {
		t.Run(body, func(t *testing.T) {
			f := newFixture(t, storyStateReplyHandler(t, 200, body, false))
			if err := f.Client.SetStoryState(t.Context(), testRepo, 141, 9001, 42, 9005, true); err == nil {
				t.Fatal("unconfirmed state update accepted")
			}
		})
	}
}

func storyStateReplyHandler(t *testing.T, status int, body string, rate bool) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			if rate {
				w.Header().Set("Retry-After", "30")
			}
			respondJSON(status, body)(w, r)
			return
		}
		switch r.URL.Path {
		case "/repos/acme/acme-api/issues/42":
			respondJSON(200, `{"id":9005,"number":42,"state":"open"}`)(w, r)
		case "/repos/acme/acme-api/issues/42/parent":
			respondJSON(200, `{"id":9001,"number":141}`)(w, r)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}
}

func TestSetStoryStatePreservesErrorClassification(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		status int
		want   error
		rate   bool
	}{
		{"unauthorized", 401, github.ErrUnauthorized, false},
		{"forbidden", 403, github.ErrForbidden, false},
		{"not found", 404, github.ErrNotFound, false},
		{"validation", 422, github.ErrUnprocessable, false},
		{"rate limit", 429, github.ErrRateLimited, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, storyStateReplyHandler(t, tc.status, `{"message":"request refused"}`, tc.rate))
			err := f.Client.SetStoryState(t.Context(), testRepo, 141, 9001, 42, 9005, true)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error=%v, want %v", err, tc.want)
			}
			if tc.rate {
				var limit *github.RateLimitError
				if !errors.As(err, &limit) || limit.RetryAfter != 30*time.Second {
					t.Fatalf("lost rate retry: %v", err)
				}
			}
		})
	}
}
