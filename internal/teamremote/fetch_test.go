package teamremote_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/teamremote"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFetchRejectsRedirectEscapes(t *testing.T) {
	for _, target := range []string{"http://example.org/catalog/index.json", "https://evil.example/catalog/index.json", "https://example.org/outside.json", "https://example.org:444/catalog/index.json", "https://user:password@example.org/catalog/index.json"} {
		t.Run(target, func(t *testing.T) {
			calls := 0
			client := teamremote.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {target}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
			})}
			p, err := client.Fetch(t.Context(), "https://example.org/catalog/teams.json", "go-team", "", "")
			if err == nil || p != nil {
				t.Fatal("untrusted redirect produced package")
			}
			if calls != 1 {
				t.Fatalf("redirect target contacted: %d requests", calls)
			}
		})
	}
}

func TestFetchBoundsRedirectsAndIndex(t *testing.T) {
	for _, scenario := range []string{"redirect loop", "oversized discovery"} {
		t.Run(scenario, func(t *testing.T) {
			calls := 0
			client := teamremote.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if scenario == "redirect loop" {
					return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {"https://example.org/catalog/again.json"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(strings.Repeat(" ", teamremote.MaxIndexBytes+1))), Request: r}, nil
			})}
			p, err := client.Fetch(t.Context(), "https://example.org/catalog/teams.json", "go-team", "", "")
			if err == nil || p != nil {
				t.Fatal("unbounded input accepted")
			}
			if calls > 4 {
				t.Fatalf("more than three redirects followed: %d requests", calls)
			}
		})
	}
}
