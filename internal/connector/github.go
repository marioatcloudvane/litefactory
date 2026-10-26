package connector

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// GitHubIdentityProvider verifies a fresh identity; no upstream token escapes it.
type GitHubIdentityProvider interface {
	AuthorizationURL(state, challenge, redirectURI string) string
	Verify(ctx context.Context, code, verifier, redirectURI string) (int64, error)
}

// GitHubProvider calls a dedicated OAuth application's identity-only endpoints.
// Custom endpoints and a client allow controlled external dependencies in tests.
type GitHubProvider struct {
	ClientID, ClientSecret          string
	AuthorizeURL, TokenURL, UserURL string
	Client                          *http.Client
}

func (g *GitHubProvider) endpoints() (string, string, string) {
	a, t, u := g.AuthorizeURL, g.TokenURL, g.UserURL
	if a == "" {
		a = "https://github.com/login/oauth/authorize"
	}
	if t == "" {
		t = "https://github.com/login/oauth/access_token"
	}
	if u == "" {
		u = "https://api.github.com/user"
	}
	return a, t, u
}

// AuthorizationURL requests no repository permission and binds upstream PKCE.
func (g *GitHubProvider) AuthorizationURL(state, challenge, redirectURI string) string {
	a, _, _ := g.endpoints()
	v := url.Values{"client_id": {g.ClientID}, "redirect_uri": {redirectURI}, "state": {state}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}}
	return a + "?" + v.Encode()
}

// Verify exchanges a fresh code and checks the immutable numeric /user identity.
func (g *GitHubProvider) Verify(ctx context.Context, code, verifier, redirectURI string) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, tokenURL, userURL := g.endpoints()
	v := url.Values{"client_id": {g.ClientID}, "client_secret": {g.ClientSecret}, "code": {code}, "code_verifier": {verifier}, "redirect_uri": {redirectURI}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(v.Encode()))
	if err != nil {
		return 0, errors.New("connector: identity provider unavailable")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	client := g.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	// Never follow provider redirects with application credentials or a bearer.
	boundedClient := *client
	boundedClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	response, err := boundedClient.Do(req)
	if err != nil {
		return 0, errors.New("connector: identity provider unavailable")
	}
	defer func() { _ = response.Body.Close() }()
	var token struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if response.StatusCode != http.StatusOK || decodeProvider(response.Body, &token) != nil || token.AccessToken == "" || token.Error != "" {
		return 0, errors.New("connector: identity verification failed")
	}
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, userURL, nil)
	if err != nil {
		return 0, errors.New("connector: identity provider unavailable")
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	identity, err := boundedClient.Do(req)
	if err != nil {
		return 0, errors.New("connector: identity provider unavailable")
	}
	defer func() { _ = identity.Body.Close() }()
	var user struct {
		ID int64 `json:"id"`
	}
	if identity.StatusCode != http.StatusOK || decodeProvider(identity.Body, &user) != nil || user.ID <= 0 {
		return 0, errors.New("connector: identity verification failed")
	}
	return user.ID, nil
}

func decodeProvider(r io.Reader, target any) error {
	b, err := io.ReadAll(io.LimitReader(r, 64*1024+1))
	if err != nil || len(b) > 64*1024 {
		return errors.New("connector: invalid provider response")
	}
	return json.Unmarshal(b, target)
}
