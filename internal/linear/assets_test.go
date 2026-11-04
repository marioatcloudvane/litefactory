package linear

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/cloudvane/litefactory/internal/secrets"
)

const assetTestToken = "lin_api_sensitive-asset-token"

// assetRequest is what the recording transport saw for one dial.
type assetRequest struct {
	method, host, scheme, authorization string
}

// assetTransport answers per host and records every request it was asked to
// send, so a test can prove a refused URL was never dialed at all.
type assetTransport struct {
	mu       sync.Mutex
	requests []assetRequest
	respond  func(*http.Request) *http.Response
}

func (a *assetTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	a.mu.Lock()
	a.requests = append(a.requests, assetRequest{req.Method, req.URL.Host, req.URL.Scheme, req.Header.Get("Authorization")})
	a.mu.Unlock()
	return a.respond(req), nil
}

func (a *assetTransport) seen() []assetRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]assetRequest(nil), a.requests...)
}

func assetResponse(status int, contentType string, body []byte, header http.Header) *http.Response {
	if header == nil {
		header = http.Header{}
	}
	if contentType != "" {
		header.Set("Content-Type", contentType)
	}
	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(bytes.NewReader(body))}
}

func assetClient(t *testing.T, authKind string, transport *assetTransport) *Client {
	t.Helper()
	c, err := New(Config{Origin: "https://api.linear.app", WorkspaceID: "workspace", TeamID: "team", ProjectID: "project", AuthKind: authKind},
		secrets.NewToken(assetTestToken), &http.Client{Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestFetchAssetReturnsUploadBytesWithTheConnectionCredential(t *testing.T) {
	t.Parallel()
	png := []byte("\x89PNG\r\n\x1a\nrest")
	for _, tc := range []struct{ kind, want string }{
		{"api_key", assetTestToken},
		{"", assetTestToken},
		{"oauth", "Bearer " + assetTestToken},
	} {
		transport := &assetTransport{respond: func(*http.Request) *http.Response {
			return assetResponse(http.StatusOK, "image/png", png, nil)
		}}
		body, contentType, err := assetClient(t, tc.kind, transport).FetchAsset(context.Background(), "https://uploads.linear.app/org/upload-id/shot.png?signature=abc")
		if err != nil {
			t.Fatalf("%s: FetchAsset: %v", tc.kind, err)
		}
		got, _ := io.ReadAll(body)
		_ = body.Close()
		if !bytes.Equal(got, png) || contentType != "image/png" {
			t.Errorf("%s: body %q type %q", tc.kind, got, contentType)
		}
		seen := transport.seen()
		if len(seen) != 1 || seen[0].method != http.MethodGet || seen[0].host != "uploads.linear.app" || seen[0].authorization != tc.want {
			t.Errorf("%s: requests = %+v", tc.kind, seen)
		}
	}
}

func TestFetchAssetRefusesEveryOtherHostBeforeDialing(t *testing.T) {
	t.Parallel()
	transport := &assetTransport{respond: func(*http.Request) *http.Response {
		t.Error("a refused URL was dialed")
		return assetResponse(http.StatusOK, "", nil, nil)
	}}
	c := assetClient(t, "api_key", transport)
	for _, raw := range []string{
		"http://uploads.linear.app/org/a.png",
		"https://evil.example/a.png",
		"https://api.linear.app/x",
		"https://uploads.linear.app.evil.example/a.png",
		"https://user@uploads.linear.app.evil.example/a.png",
		"",
		"://bad",
	} {
		if body, _, err := c.FetchAsset(context.Background(), raw); err == nil {
			_ = body.Close()
			t.Errorf("FetchAsset(%q) succeeded, want refusal", raw)
		}
	}
	if seen := transport.seen(); len(seen) != 0 {
		t.Errorf("refused URLs were dialed: %+v", seen)
	}
}

func TestFetchAssetDoesNotFollowARedirect(t *testing.T) {
	t.Parallel()
	transport := &assetTransport{respond: func(req *http.Request) *http.Response {
		if req.URL.Host != "uploads.linear.app" {
			t.Errorf("redirect target %s was dialed", req.URL.Host)
		}
		return assetResponse(http.StatusFound, "", nil, http.Header{"Location": {"https://storage.example/signed?token=x"}})
	}}
	body, _, err := assetClient(t, "api_key", transport).FetchAsset(context.Background(), "https://uploads.linear.app/org/a.png")
	if err == nil {
		_ = body.Close()
		t.Fatal("a redirect was treated as success")
	}
	seen := transport.seen()
	if len(seen) != 1 || seen[0].host != "uploads.linear.app" {
		t.Errorf("requests = %+v, want only the upload host", seen)
	}
}

func TestFetchAssetFailuresCarryNoURLOrToken(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusInternalServerError} {
		transport := &assetTransport{respond: func(*http.Request) *http.Response {
			return assetResponse(status, "text/plain", []byte("denied "+assetTestToken), nil)
		}}
		body, _, err := assetClient(t, "api_key", transport).FetchAsset(context.Background(), "https://uploads.linear.app/org/secret-id/a.png?signature=querysecret")
		if err == nil {
			_ = body.Close()
			t.Fatalf("status %d succeeded", status)
		}
		for _, leaked := range []string{assetTestToken, "querysecret", "secret-id", "uploads.linear.app"} {
			if strings.Contains(err.Error(), leaked) {
				t.Errorf("status %d error %q carries %q", status, err, leaked)
			}
		}
		if n := len(transport.seen()); n != 1 {
			t.Errorf("status %d: %d requests, want 1 (no retry in transport)", status, n)
		}
	}
}

func TestFetchAssetUsesRequestAccounting(t *testing.T) {
	t.Parallel()
	transport := &assetTransport{respond: func(*http.Request) *http.Response {
		return assetResponse(http.StatusOK, "image/png", []byte("x"), nil)
	}}
	held := errors.New("held")
	c := assetClient(t, "api_key", transport)
	c.config.RequestContext = func(ctx context.Context) (context.Context, func(error), error) { return ctx, nil, held }
	if _, _, err := c.FetchAsset(context.Background(), "https://uploads.linear.app/org/a.png"); !errors.Is(err, held) {
		t.Errorf("err = %v, want the request hold", err)
	}
	if n := len(transport.seen()); n != 0 {
		t.Errorf("a held request was dialed %d times", n)
	}

	var finished []error
	c.config.RequestContext = func(ctx context.Context) (context.Context, func(error), error) {
		return ctx, func(err error) { finished = append(finished, err) }, nil
	}
	body, _, err := c.FetchAsset(context.Background(), "https://uploads.linear.app/org/a.png")
	if err != nil {
		t.Fatal(err)
	}
	_ = body.Close()
	if len(finished) != 1 || finished[0] != nil {
		t.Errorf("finish calls = %v, want one nil", finished)
	}
}
