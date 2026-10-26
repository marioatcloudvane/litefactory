package connector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestGitHubIdentityProviderRequiresFreshBoundIdentity(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, tokenBody, userBody string
		tokenStatus, userStatus   int
		valid                     bool
	}{
		{name: "identity", tokenBody: `{"access_token":"upstream-secret"}`, userBody: `{"id":42}`, tokenStatus: 200, userStatus: 200, valid: true},
		{name: "provider denial", tokenBody: `{"error":"access_denied"}`, tokenStatus: 200},
		{name: "provider throttling", tokenBody: `{"access_token":"upstream-secret"}`, tokenStatus: 429},
		{name: "malformed token", tokenBody: `not-json`, tokenStatus: 200},
		{name: "identity unavailable", tokenBody: `{"access_token":"upstream-secret"}`, userStatus: 503, tokenStatus: 200},
		{name: "missing numeric identity", tokenBody: `{"access_token":"upstream-secret"}`, userBody: `{"login":"owner"}`, tokenStatus: 200, userStatus: 200},
		{name: "wrong identity type", tokenBody: `{"access_token":"upstream-secret"}`, userBody: `{"id":"42"}`, tokenStatus: 200, userStatus: 200},
		{name: "oversized token", tokenBody: strings.Repeat("x", 64*1024+1), tokenStatus: 200},
		{name: "oversized identity", tokenBody: `{"access_token":"upstream-secret"}`, userBody: strings.Repeat("x", 64*1024+1), tokenStatus: 200, userStatus: 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/token":
					if r.Method != http.MethodPost {
						t.Error("token exchange was not POST")
					}
					if err := r.ParseForm(); err != nil {
						t.Error(err)
					}
					if r.Form.Get("code") != "code" || r.Form.Get("code_verifier") != "verifier" || r.Form.Get("redirect_uri") != "https://cloudvane.at/litefactory/oauth/github/callback" || r.Form.Get("client_secret") != "app-secret" {
						t.Error("upstream exchange lost binding")
					}
					if r.URL.RawQuery != "" || r.Form.Get("scope") != "" {
						t.Error("upstream token exchange leaked query or requested scope")
					}
					w.WriteHeader(test.tokenStatus)
					_, _ = w.Write([]byte(test.tokenBody))
				case "/user":
					if r.Header.Get("Authorization") != "Bearer upstream-secret" {
						t.Error("fresh upstream credential absent")
					}
					w.WriteHeader(test.userStatus)
					_, _ = w.Write([]byte(test.userBody))
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
				}
			}))
			defer server.Close()
			provider := GitHubProvider{ClientID: "app", ClientSecret: "app-secret", AuthorizeURL: server.URL + "/authorize", TokenURL: server.URL + "/token", UserURL: server.URL + "/user", Client: server.Client()}
			location, err := url.Parse(provider.AuthorizationURL("state", "challenge", "https://cloudvane.at/litefactory/oauth/github/callback"))
			if err != nil {
				t.Fatal(err)
			}
			q := location.Query()
			if q.Get("state") != "state" || q.Get("code_challenge") != "challenge" || q.Get("code_challenge_method") != "S256" || q.Get("scope") != "" || q.Get("client_secret") != "" {
				t.Fatalf("unsafe authorization URL %v", q)
			}
			owner, err := provider.Verify(t.Context(), "code", "verifier", "https://cloudvane.at/litefactory/oauth/github/callback")
			if test.valid {
				if err != nil || owner != 42 {
					t.Fatalf("identity: %d %v", owner, err)
				}
			} else if err == nil || owner != 0 {
				t.Fatalf("invalid provider response accepted: %d %v", owner, err)
			}
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatalf("credential in provider error: %v", err)
			}
		})
	}
}

func TestGitHubProviderRefusesRedirectsAndCancellation(t *testing.T) {
	t.Parallel()
	var followed atomic.Int64
	destination := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { followed.Add(1); w.WriteHeader(http.StatusOK) }))
	defer destination.Close()
	redirect := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	provider := GitHubProvider{ClientID: "app", ClientSecret: "secret", TokenURL: redirect.URL, UserURL: destination.URL, Client: redirect.Client()}
	if owner, err := provider.Verify(t.Context(), "code", "verifier", "https://client.example/callback"); err == nil || owner != 0 {
		t.Fatal("accepted a credential-bearing provider redirect")
	}
	if followed.Load() != 0 {
		t.Fatal("followed credential-bearing redirect")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := provider.Verify(ctx, "code", "verifier", "https://client.example/callback"); err == nil {
		t.Fatal("ignored canceled request")
	}
}
