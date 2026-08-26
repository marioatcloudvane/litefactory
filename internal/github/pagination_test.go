package github_test

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
)

// pagedHandler serves one page per element of pages, selected by the ?page=
// parameter, advertising the next one in a Link header exactly as GitHub does.
// The link is built from the request's own host, so the client walks back to
// the httptest server.
func pagedHandler(pages []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page := 1
		if raw := r.URL.Query().Get("page"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			page = n
		}
		if page > len(pages) {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if page < len(pages) {
			next := *r.URL
			query := next.Query()
			query.Set("page", strconv.Itoa(page+1))
			next.RawQuery = query.Encode()

			last := *r.URL
			lastQuery := last.Query()
			lastQuery.Set("page", strconv.Itoa(len(pages)))
			last.RawQuery = lastQuery.Encode()

			w.Header().Set("Link", fmt.Sprintf(`<http://%s%s>; rel="next", <http://%s%s>; rel="last"`,
				r.Host, next.RequestURI(), r.Host, last.RequestURI()))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, pages[page-1])
	}
}

// TestListIssuesFollowsLinkPagination walks a three-page result. The assertion
// that matters is that nothing is lost or duplicated in the middle: a
// pagination bug shows up as a comment litefactory never sees, which looks
// exactly like the daemon being asleep.
func TestListIssuesFollowsLinkPagination(t *testing.T) {
	t.Parallel()

	f := newFixture(t, pagedHandler([]string{
		`[{"number": 1, "title": "one"}, {"number": 2, "title": "two"}]`,
		`[{"number": 3, "title": "three"}]`,
		`[{"number": 4, "title": "four"}, {"number": 5, "title": "five"}]`,
	}))

	issues, err := f.Client.ListIssues(t.Context(), testRepo, time.Time{})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}

	var numbers []int
	for _, issue := range issues {
		numbers = append(numbers, issue.Number)
	}
	if fmt.Sprint(numbers) != "[1 2 3 4 5]" {
		t.Errorf("issue numbers = %v, want [1 2 3 4 5]", numbers)
	}

	requests := f.Requests()
	if len(requests) != 3 {
		t.Fatalf("made %d requests, want one per page (3)", len(requests))
	}
	for i, req := range requests {
		wantPage := ""
		if i > 0 {
			wantPage = strconv.Itoa(i + 1)
		}
		if got := req.Query.Get("page"); got != wantPage {
			t.Errorf("request %d asked for page %q, want %q", i, got, wantPage)
		}
		// The Link header carries the whole query, so the filters must survive
		// the hop. Losing state=open on page two would start pulling closed
		// issues back down the wire from there on.
		if got := req.Query.Get("state"); got != "open" {
			t.Errorf("request %d lost state=open (got %q)", i, got)
		}
	}
}

// TestPaginationCountsEveryRequest checks the number AC-2.8 asks to be logged.
// "Tick cost is O(registered repos), bounded and logged" is only auditable if
// the cost line is truthful.
func TestPaginationCountsEveryRequest(t *testing.T) {
	t.Parallel()

	f := newFixture(t, pagedHandler([]string{`[{"number": 1}]`, `[{"number": 2}]`}))
	if _, err := f.Client.ListIssues(t.Context(), testRepo, time.Time{}); err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if got, want := f.Logs(), `"requests":2`; !strings.Contains(got, want) {
		t.Errorf("the per-repo cost line does not report %s:\n%s", want, got)
	}
}

// TestPaginationIsBounded covers a server that offers a next page forever —
// a proxy bug, or a cursor that never advances. Without the bound this is an
// infinite loop inside one tick that burns the hourly quota; the daemon must
// get an error instead.
func TestPaginationIsBounded(t *testing.T) {
	t.Parallel()

	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", fmt.Sprintf(`<http://%s%s>; rel="next"`, r.Host, r.URL.RequestURI()))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"number": 1}]`)
	})

	_, err := f.Client.ListIssues(t.Context(), testRepo, time.Time{})
	if !errors.Is(err, github.ErrTooManyPages) {
		t.Fatalf("ListIssues error = %v, want ErrTooManyPages", err)
	}
	if got := len(f.Requests()); got == 0 || got > 100 {
		t.Errorf("made %d requests, want a bounded number no greater than 100", got)
	}
}

// TestNextPageLinkToAnotherHostIsRefused is a security assertion, not a
// robustness one. This package builds the paged request itself, so net/http's
// redirect logic — which strips the Authorization header on a cross-host hop —
// never gets a say. Following an off-host Link would hand the bot credential to
// whoever supplied the header.
func TestNextPageLinkToAnotherHostIsRefused(t *testing.T) {
	t.Parallel()

	f := newFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Link", `<https://credential-harvester.example.com/issues?page=2>; rel="next"`)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"number": 1}]`)
	})

	_, err := f.Client.ListIssues(t.Context(), testRepo, time.Time{})
	if !errors.Is(err, github.ErrInvalidArgument) {
		t.Fatalf("ListIssues error = %v, want ErrInvalidArgument for an off-host next link", err)
	}
	if got := len(f.Requests()); got != 1 {
		t.Errorf("made %d requests, want exactly 1 — the second was refused", got)
	}
	if !strings.Contains(err.Error(), "credential-harvester.example.com") {
		t.Errorf("error %q does not name the host it refused", err)
	}
}

// TestNextPageLinkOnADifferentSchemeIsRefused covers the downgrade case: an
// http:// link from an https:// API would put the credential on the wire in
// clear text.
func TestNextPageLinkOnADifferentSchemeIsRefused(t *testing.T) {
	t.Parallel()

	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", fmt.Sprintf(`<https://%s/x?page=2>; rel="next"`, r.Host))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[]`)
	})

	_, err := f.Client.ListIssues(t.Context(), testRepo, time.Time{})
	if !errors.Is(err, github.ErrInvalidArgument) {
		t.Fatalf("ListIssues error = %v, want ErrInvalidArgument for a scheme change", err)
	}
}

// TestMalformedLinkHeaderEndsTheWalk covers the benign case. A header this
// package cannot parse means "no more pages", which is also what GitHub means
// when it omits the header on a single-page result.
func TestMalformedLinkHeaderEndsTheWalk(t *testing.T) {
	t.Parallel()

	f := newFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Link", `this is not a link header`)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"number": 1}]`)
	})

	issues, err := f.Client.ListIssues(t.Context(), testRepo, time.Time{})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(issues) != 1 {
		t.Errorf("got %d issues, want 1", len(issues))
	}
	if got := len(f.Requests()); got != 1 {
		t.Errorf("made %d requests, want 1", got)
	}
}
