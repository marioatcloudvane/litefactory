package github_test

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/secrets"
)

type ticketPageTransport func(*http.Request) (*http.Response, error)

func (f ticketPageTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTicketBoundedCatalogReads(t *testing.T) {
	t.Parallel()
	const origin = "https://tickets.example.test"
	for _, tt := range []struct {
		name, query, payload string
		context              bool
		wantTickets          int
		wantMore             bool
	}{
		{"filtered page retains continuation", "direction=asc&page=3&per_page=2&sort=created&state=open", `[{"id":900,"number":9,"pull_request":{}}]`, false, 0, true},
		{"context trims extra ticket", "direction=desc&page=1&per_page=3&sort=updated&state=open", "[" + ticketJSON(origin, 700, "one", "body") + "," + ticketJSON(origin, 701, "two", "body") + "," + ticketJSON(origin, 702, "three", "body") + "]", true, 2, true},
		{"context retains filtered page continuation", "direction=desc&page=1&per_page=3&sort=updated&state=open", `[{"id":900,"number":9,"pull_request":{}}]`, true, 0, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			requests := 0
			client, err := github.New(github.Options{APIBase: origin, Token: secrets.NewToken("test-token"), HTTPClient: &http.Client{Transport: ticketPageTransport(func(r *http.Request) (*http.Response, error) {
				requests++
				if r.URL.Path != "/repos/"+testRepo.String()+"/issues" || r.URL.RawQuery != tt.query {
					t.Errorf("unexpected catalog request: %s", r.URL)
				}
				header := http.Header{"Content-Type": []string{"application/json"}, "Link": []string{fmt.Sprintf(`<%s/repos/%s/issues?page=4>; rel="next"`, origin, testRepo.String())}}
				return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader(tt.payload)), Request: r}, nil
			})}})
			if err != nil {
				t.Fatal(err)
			}
			adapter, err := github.NewTickets(client)
			if err != nil {
				t.Fatal(err)
			}
			var count int
			var more bool
			if tt.context {
				result, err := adapter.OpenContext(t.Context(), testRepo.String(), 2)
				if err != nil {
					t.Fatal(err)
				}
				count, more = len(result.Tickets), result.More
			} else {
				result, err := adapter.IssuesPage(t.Context(), testRepo.String(), "open", 3, 2)
				if err != nil {
					t.Fatal(err)
				}
				count, more = len(result.Tickets), result.More
				if result.NextPage != 4 {
					t.Fatalf("next page=%d", result.NextPage)
				}
			}
			if count != tt.wantTickets || more != tt.wantMore {
				t.Fatalf("catalog got count=%d more=%v", count, more)
			}
			if requests != 1 {
				t.Fatalf("bounded read followed pagination: %d requests", requests)
			}
		})
	}
}
