package github

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
)

type cacheUnitTransport func(*http.Request) (*http.Response, error)

func (f cacheUnitTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func cacheUnitResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Etag": {`"version"`}, "Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), ContentLength: -1}
}
func cacheUnitClient(next cacheUnitTransport) *pollingTransport {
	base, _ := url.Parse("https://example.test/")
	return &pollingTransport{next: next, client: &Client{base: base}, entries: make(map[[32]byte]cachedRepresentation)}
}
func cacheUnitRequest(t *testing.T) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.test/repos/o/r/issues/1", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer fixture")
	return req
}

type cacheUnitMetadata struct {
	StatusCode int
	Header     http.Header
}

func cacheUnitRead(t *testing.T, tr *pollingTransport, req *http.Request) (cacheUnitMetadata, string) {
	t.Helper()
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return cacheUnitMetadata{StatusCode: resp.StatusCode, Header: resp.Header}, string(b)
}

func TestPollingCacheUnit_RepresentationIsolation(t *testing.T) {
	tests := []struct {
		name  string
		alter func(*http.Request)
	}{
		{"authorization", func(r *http.Request) { r.Header.Set("Authorization", "Bearer other") }},
		{"accept", func(r *http.Request) { r.Header.Set("Accept", "application/other+json") }},
		{"api version", func(r *http.Request) { r.Header.Set("X-GitHub-Api-Version", "2099-01-01") }},
		{"query", func(r *http.Request) { r.URL.RawQuery = "page=2" }},
		{"path", func(r *http.Request) { r.URL.Path = "/repos/o/r/issues/2" }},
		{"origin", func(r *http.Request) { r.URL.Host = "other.test" }},
		{"method", func(r *http.Request) { r.Method = http.MethodPost }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tr := cacheUnitClient(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("If-None-Match") != "" {
					return cacheUnitResponse(304, ""), nil
				}
				return cacheUnitResponse(200, r.URL.String()+r.Header.Get("Authorization")+r.Header.Get("Accept")+r.Header.Get("X-GitHub-Api-Version")+r.Method), nil
			})
			req := cacheUnitRequest(t)
			_, first := cacheUnitRead(t, tr, req)
			changed := req.Clone(t.Context())
			tt.alter(changed)
			_, got := cacheUnitRead(t, tr, changed)
			if got == first {
				t.Fatal("different representation reused original response")
			}
			if req.Header.Get("If-None-Match") != "" {
				t.Fatal("caller request mutated")
			}
		})
	}
}

func TestPollingCacheUnit_BypassIsComplete(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		headers http.Header
	}{
		{"oversized body", strings.Repeat("x", cacheMaxBody+333), nil},
		{"oversized metadata", "complete", http.Header{"Link": {strings.Repeat("x", cacheMaxHeaders+1)}}},
		{"no validator", "complete", http.Header{"Etag": {}}},
		{"no store", "complete", http.Header{"Cache-Control": {"private, no-store"}}},
		{"unsupported vary", "complete", http.Header{"Vary": {"X-Private"}}},
		{"cookie", "complete", http.Header{"Set-Cookie": {"private=value"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tr := cacheUnitClient(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("If-None-Match") != "" {
					return nil, errors.New("unsafe response retained")
				}
				resp := cacheUnitResponse(200, tt.body)
				for k, v := range tt.headers {
					resp.Header[k] = v
				}
				return resp, nil
			})
			for range 2 {
				_, got := cacheUnitRead(t, tr, cacheUnitRequest(t))
				if got != tt.body {
					t.Fatalf("body length %d, want %d", len(got), len(tt.body))
				}
			}
		})
	}
}

// githubVary is the exact Vary value GitHub's REST API sends.
const githubVary = "Accept, Authorization, Cookie, X-GitHub-OTP,Accept-Encoding, Accept, X-Requested-With"

func githubVaryResponse(status int, body string) *http.Response {
	resp := cacheUnitResponse(status, body)
	resp.Header.Set("Etag", `W/"x"`)
	resp.Header.Set("Cache-Control", "private, max-age=60, s-maxage=60")
	resp.Header.Set("Vary", githubVary)
	return resp
}

func TestPollingCacheUnit_GitHubVaryIsRetained(t *testing.T) {
	t.Parallel()
	var conditional []string
	tr := cacheUnitClient(func(r *http.Request) (*http.Response, error) {
		conditional = append(conditional, r.Header.Get("If-None-Match"))
		if r.Header.Get("If-None-Match") != "" {
			return githubVaryResponse(http.StatusNotModified, ""), nil
		}
		return githubVaryResponse(http.StatusOK, "cached body"), nil
	})
	for i := range 2 {
		meta, got := cacheUnitRead(t, tr, cacheUnitRequest(t))
		if meta.StatusCode != http.StatusOK || got != "cached body" {
			t.Fatalf("read %d: status %d body %q", i, meta.StatusCode, got)
		}
	}
	if len(conditional) != 2 || conditional[0] != "" || conditional[1] != `W/"x"` {
		t.Fatalf("If-None-Match sent = %q, want [\"\" W/\"x\"]", conditional)
	}
}

func TestPollingCacheUnit_GitHubVaryRequestHeadersBypass(t *testing.T) {
	for _, name := range []string{"Cookie", "X-GitHub-OTP", "X-Requested-With"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tr := cacheUnitClient(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("If-None-Match") != "" {
					return nil, errors.New("validator injected into ineligible request")
				}
				return githubVaryResponse(http.StatusOK, "body"), nil
			})
			for range 2 {
				req := cacheUnitRequest(t)
				req.Header.Set(name, "value")
				if _, got := cacheUnitRead(t, tr, req); got != "body" {
					t.Fatalf("body %q", got)
				}
			}
		})
	}
}

func TestPollingCacheUnit_EvictionAndRestart(t *testing.T) {
	for _, tt := range []struct {
		name  string
		count int
		body  string
	}{
		{"entry bound", cacheMaxEntries + 1, "value"},
		{"byte bound", cacheMaxBytes/cacheMaxBody + 1, strings.Repeat("v", cacheMaxBody)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			next := cacheUnitTransport(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("If-None-Match") != "" {
					return cacheUnitResponse(304, ""), nil
				}
				return cacheUnitResponse(200, tt.body), nil
			})
			tr := cacheUnitClient(next)
			for i := range tt.count {
				req := cacheUnitRequest(t)
				req.URL.RawQuery = fmt.Sprintf("page=%d", i)
				cacheUnitRead(t, tr, req)
			}
			// Changing the origin's content shows that eviction performs ordinary retrieval.
			tr.next = cacheUnitTransport(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("If-None-Match") != "" {
					return nil, errors.New("evicted response retained")
				}
				return cacheUnitResponse(200, "fresh"), nil
			})
			req := cacheUnitRequest(t)
			req.URL.RawQuery = "page=0"
			_, got := cacheUnitRead(t, tr, req)
			if got != "fresh" {
				t.Fatal(got)
			}
			restarted := cacheUnitClient(tr.next.RoundTrip)
			_, got = cacheUnitRead(t, restarted, req)
			if got != "fresh" {
				t.Fatal(got)
			}
		})
	}
}

func TestPollingCacheUnit_ConcurrentClonedRepresentations(t *testing.T) {
	t.Parallel()
	tr := cacheUnitClient(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("If-None-Match") != "" {
			return cacheUnitResponse(304, ""), nil
		}
		return cacheUnitResponse(200, "original"), nil
	})
	req := cacheUnitRequest(t)
	resp, _ := cacheUnitRead(t, tr, req)
	resp.Header.Set("Content-Type", "mutated")
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			resp, got := cacheUnitRead(t, tr, req)
			if got != "original" || resp.Header.Get("Content-Type") != "application/json" {
				t.Errorf("cached representation corrupted: %q %v", got, resp.Header)
			}
			resp.Header.Set("Content-Type", "caller mutation")
		})
	}
	wg.Wait()
	if req.Header.Get("If-None-Match") != "" {
		t.Fatal("shared request mutated")
	}
}

func TestPollingCacheUnit_FailureNeverReturnsStale(t *testing.T) {
	for _, status := range []int{401, 403, 404, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			t.Parallel()
			calls := 0
			tr := cacheUnitClient(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return cacheUnitResponse(200, "old"), nil
				}
				if calls == 2 {
					return cacheUnitResponse(status, "failure"), nil
				}
				if r.Header.Get("If-None-Match") != "" {
					return nil, errors.New("failed validation retained")
				}
				return cacheUnitResponse(200, "new"), nil
			})
			req := cacheUnitRequest(t)
			cacheUnitRead(t, tr, req)
			resp, got := cacheUnitRead(t, tr, req)
			if resp.StatusCode != status || got != "failure" {
				t.Fatalf("got %d %q", resp.StatusCode, got)
			}
			_, got = cacheUnitRead(t, tr, req)
			if got != "new" {
				t.Fatal(got)
			}
		})
	}
	t.Run("transport failure", func(t *testing.T) {
		sentinel := errors.New("network unavailable")
		tr := cacheUnitClient(func(*http.Request) (*http.Response, error) { return cacheUnitResponse(200, "old"), nil })
		req := cacheUnitRequest(t)
		cacheUnitRead(t, tr, req)
		tr.next = cacheUnitTransport(func(*http.Request) (*http.Response, error) { return nil, sentinel })
		resp, err := tr.RoundTrip(req)
		if resp != nil {
			defer resp.Body.Close()
		}
		if resp != nil || !errors.Is(err, sentinel) {
			t.Fatalf("got %v %v", resp, err)
		}
	})
}
