package github_test

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/ticket"
)

func recoveryAdapter(t *testing.T, transport ticketPageTransport) *github.Tickets {
	t.Helper()
	client, err := github.New(github.Options{APIBase: "https://tickets.example.test", Token: secrets.NewToken("test-token"), HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := github.NewTickets(client)
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func TestRecoveryPagesBindContinuationAndRetainFilteredPages(t *testing.T) {
	t.Parallel()
	requests := 0
	adapter := recoveryAdapter(t, func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.Query().Get("state") != "all" || r.URL.Query().Get("sort") != "updated" {
			t.Errorf("wrong recovery query: %s", r.URL)
		}
		payload := `[{"id":900,"number":9,"pull_request":{}}]`
		header := http.Header{"Content-Type": []string{"application/json"}}
		if requests == 1 {
			header.Set("Link", fmt.Sprintf(`<https://tickets.example.test/repos/%s/issues?page=2>; rel="next"`, testRepo))
		}
		return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader(payload)), Request: r}, nil
	})
	since := time.Date(2026, 10, 11, 1, 2, 3, 0, time.UTC)
	page, err := adapter.ChangesPage(t.Context(), testRepo.String(), since, ticket.DiscoveryCursor{}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if page.Complete || len(page.Tickets) != 0 || page.Next.Value == "" || requests != 1 {
		t.Fatalf("filtered page lost continuation: %+v requests=%d", page, requests)
	}
	for _, tc := range []struct {
		name, container string
		since           time.Time
		cursor          ticket.DiscoveryCursor
		limit           int
	}{
		{"container", "other/repo", since, page.Next, 2},
		{"window", testRepo.String(), since.Add(time.Second), page.Next, 2},
		{"bound", testRepo.String(), since, page.Next, 3},
		{"version", testRepo.String(), since, ticket.DiscoveryCursor{Provider: ticket.GitHub, Origin: page.Next.Origin, Container: testRepo.String(), Kind: "future", Value: page.Next.Value}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := adapter.ChangesPage(t.Context(), tc.container, tc.since, tc.cursor, tc.limit); err == nil {
				t.Fatal("accepted foreign continuation")
			}
		})
	}
	if _, err := adapter.ReconcilePage(t.Context(), testRepo.String(), page.Next, 2); err == nil {
		t.Fatal("accepted another stream continuation")
	}
	if requests != 1 {
		t.Fatalf("invalid continuation performed I/O: %d", requests)
	}
	final, err := adapter.ChangesPage(t.Context(), testRepo.String(), since, page.Next, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !final.Complete || requests != 2 {
		t.Fatalf("completion=%v requests=%d", final.Complete, requests)
	}
}

func TestRecoveryReplyPageRejectsForeignOrigin(t *testing.T) {
	t.Parallel()
	for _, origin := range []string{"https://tickets.example.test", "https://foreign.example.test"} {
		t.Run(origin, func(t *testing.T) {
			t.Parallel()
			adapter := recoveryAdapter(t, func(r *http.Request) (*http.Response, error) {
				payload := fmt.Sprintf(`[{"id":801,"url":"%s/repos/%s/issues/comments/801","issue_url":"%s/repos/%s/issues/7"}]`, origin, testRepo, origin, testRepo)
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(payload)), Request: r}, nil
			})
			page, err := adapter.ReplyChangesPage(t.Context(), testRepo.String(), time.Time{}, ticket.DiscoveryCursor{}, 2)
			if origin == "https://foreign.example.test" {
				if err == nil {
					t.Fatal("accepted foreign source")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !page.Complete || len(page.Locators) != 1 || page.Locators[0].Key != "7" {
				t.Fatalf("page=%+v", page)
			}
		})
	}
}

func TestRecoveryReconciliationContinuesBeyondEagerListingLimit(t *testing.T) {
	t.Parallel()
	requests := 0
	adapter := recoveryAdapter(t, func(r *http.Request) (*http.Response, error) {
		requests++
		header := http.Header{"Content-Type": []string{"application/json"}}
		if requests == 1 {
			header.Set("Link", fmt.Sprintf(`<https://tickets.example.test/repos/%s/issues?page=100001>; rel="next"`, testRepo))
		} else if r.URL.Query().Get("page") != "100001" {
			t.Errorf("continuation page = %s", r.URL.Query().Get("page"))
		}
		return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader(`[]`)), Request: r}, nil
	})
	first, err := adapter.ReconcilePage(t.Context(), testRepo.String(), ticket.DiscoveryCursor{}, 50)
	if err != nil {
		t.Fatal(err)
	}
	if first.Complete {
		t.Fatal("lost continuation")
	}
	last, err := adapter.ReconcilePage(t.Context(), testRepo.String(), first.Next, 50)
	if err != nil {
		t.Fatal(err)
	}
	if !last.Complete || requests != 2 {
		t.Fatalf("completion=%v requests=%d", last.Complete, requests)
	}
}
