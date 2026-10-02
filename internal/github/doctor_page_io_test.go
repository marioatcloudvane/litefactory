package github_test

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
)

// Local HTTP fixtures exercise the SDK and transport; they are not live forge
// observations and do not establish native provider or daemon availability.
func TestDoctorHTTPPageRetainsCursorWithoutFollowingLink(t *testing.T) {
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Link", fmt.Sprintf("<http://%s/repos/acme/acme-api/issues/comments?page=3&per_page=12>; rel=\"next\"", r.Host))
		_, _ = fmt.Fprint(w, `[{"id":91,"body":"/doctor explain","issue_url":"https://api.github.com/repos/acme/acme-api/issues/406","user":{"id":7,"type":"User"},"created_at":"2026-09-29T10:01:00Z","updated_at":"2026-09-29T10:02:00Z"}]`)
	})
	since := time.Date(2026, 9, 29, 12, 0, 0, 0, time.FixedZone("fixture", 2*60*60))
	page, err := f.Client.ListRepositoryCommentsPage(t.Context(), testRepo, since, 2, 12)
	if err != nil {
		t.Fatal(err)
	}
	if page.NextPage != 3 || len(page.Comments) != 1 || page.Comments[0].Comment.ID != 91 {
		t.Fatalf("page identity: %+v", page)
	}
	request := f.Only(t)
	if request.Method != http.MethodGet || request.Path != "/repos/acme/acme-api/issues/comments" {
		t.Fatalf("unexpected route: %+v", request)
	}
	for key, want := range map[string]string{"page": "2", "per_page": "12", "sort": "updated", "direction": "asc", "since": "2026-09-29T10:00:00Z"} {
		if got := request.Query.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestDoctorHTTPPageRefusesOversizeAndPreservesRateLimit(t *testing.T) {
	t.Run("oversize", func(t *testing.T) {
		f := newFixture(t, respondJSON(http.StatusOK, `[{"id":1,"issue_url":"https://api.github.com/repos/acme/acme-api/issues/406"},{"id":2,"issue_url":"https://api.github.com/repos/acme/acme-api/issues/407"}]`))
		if _, err := f.Client.ListRepositoryCommentsPage(t.Context(), testRepo, time.Time{}, 1, 1); err == nil {
			t.Fatal("server page over configured bound accepted")
		}
		f.Only(t)
	})
	t.Run("quota", func(t *testing.T) {
		f := newFixture(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = fmt.Fprint(w, `{"message":"fixture quota"}`)
		})
		_, err := f.Client.ListRepositoryCommentsPage(t.Context(), testRepo, time.Time{}, 4, 12)
		var rate *github.RateLimitError
		if !errors.Is(err, github.ErrRateLimited) || !errors.As(err, &rate) {
			t.Fatalf("lost typed quota failure: %v", err)
		}
		f.Only(t) // Transport must not retry or consume another page itself.
	})
	t.Run("invalid_request", func(t *testing.T) {
		f := newFixture(t, respondJSON(http.StatusOK, `[]`))
		for _, test := range []struct{ page, limit int }{{0, 12}, {1, 0}, {1, 101}} {
			if _, err := f.Client.ListRepositoryCommentsPage(t.Context(), testRepo, time.Time{}, test.page, test.limit); err == nil {
				t.Fatalf("invalid page admitted: %+v", test)
			}
		}
		if len(f.Requests()) != 0 {
			t.Fatal("invalid bounds reached HTTP transport")
		}
	})
}
