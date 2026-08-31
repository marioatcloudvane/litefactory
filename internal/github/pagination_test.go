package github_test

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
)

// pagedHandler serves one page per element of pages, selected by the ?page=
// parameter, advertising the next one in a Link header exactly as GitHub does.
//
// The link is built from the request's own host because that is what GitHub
// sends, not because the client needs it to be: since D1 the only thing read
// out of the header is the page number, which is then re-applied to the base
// URL the client was configured with. The full query is rebuilt from the
// request options, which is why the filters survive the hop.
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
//
// The bound is the one piece of pagination this package still owns after the
// SDK migration (D1): go-github will follow whatever a server advertises.
func TestPaginationIsBounded(t *testing.T) {
	t.Parallel()

	// Always page two, forever. The link has to carry a page number the SDK
	// can parse — that number is now the whole of what a Link header can say
	// to this client — and it must not be the page just served, or the walk
	// would end honestly rather than being stopped.
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", fmt.Sprintf(`<http://%s%s?page=2>; rel="next"`, r.Host, r.URL.Path))
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

// TestNextPageLinkToAnotherHostCannotMoveTheWalk is a security assertion, not a
// robustness one, and it survives D1 with its mechanism replaced.
//
// Before the SDK, this package built the paged request from the Link header's
// target, so net/http's redirect logic — which strips the Authorization header
// on a cross-host hop — never got a say, and an off-host target had to be
// detected and refused. go-github does not carry the target at all: it parses
// the page *number* out of the link and this package re-applies it to the
// client's own base URL. The off-host address is therefore not refused, it is
// never in a position to be used, which is why the assertion is now "the
// harvester received nothing" rather than "an error was returned".
func TestNextPageLinkToAnotherHostCannotMoveTheWalk(t *testing.T) {
	t.Parallel()

	// A real second server, so "did the credential go there" is a fact this
	// test can observe rather than infer.
	var harvested atomic.Int64
	harvester := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		harvested.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"number": 99}]`)
	}))
	t.Cleanup(harvester.Close)

	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "" {
			w.Header().Set("Link",
				fmt.Sprintf(`<%s/issues?page=2>; rel="next"`, harvester.URL))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"number": 1}]`)
	})

	issues, err := f.Client.ListIssues(t.Context(), testRepo, time.Time{})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if got := harvested.Load(); got != 0 {
		t.Fatalf("the off-host target received %d requests; the bot credential must never "+
			"be sent anywhere the client was not configured to talk to", got)
	}

	requests := f.Requests()
	if len(requests) != 2 {
		t.Fatalf("made %d requests to the configured host, want 2 — the walk continued "+
			"there rather than following the link", len(requests))
	}
	if got := requests[1].Query.Get("page"); got != "2" {
		t.Errorf("the second request asked for page %q, want 2", got)
	}
	for _, issue := range issues {
		if issue.Number == 99 {
			t.Error("a result served by the off-host target reached the caller")
		}
	}
}

// TestNextPageLinkOnADifferentSchemeCannotMoveTheWalk covers the downgrade
// case: an https:// link from an http:// API, or the reverse, would put the
// credential on the wire in clear text if the target were followed. For the
// reason above it cannot be — only the page number survives the header — so the
// assertion is that the walk stayed on the configured scheme and host.
func TestNextPageLinkOnADifferentSchemeCannotMoveTheWalk(t *testing.T) {
	t.Parallel()

	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "" {
			w.Header().Set("Link", fmt.Sprintf(`<https://%s/x?page=2>; rel="next"`, r.Host))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[]`)
	})

	if _, err := f.Client.ListIssues(t.Context(), testRepo, time.Time{}); err != nil {
		t.Fatalf("ListIssues: %v", err)
	}

	requests := f.Requests()
	if len(requests) != 2 {
		t.Fatalf("made %d requests, want 2 — both to the configured http:// base", len(requests))
	}
	// The path is the one this package built, not the /x the header named:
	// nothing from the link's target reached the request.
	for i, req := range requests {
		if got, want := req.Path, "/repos/acme/acme-api/issues"; got != want {
			t.Errorf("request %d path = %q, want %q", i, got, want)
		}
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
