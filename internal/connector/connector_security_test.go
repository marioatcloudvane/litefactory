package connector

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestRevocationClientAuthenticationAndIsolation(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"none", "client_secret_basic", "client_secret_post"} {
		t.Run(method, func(t *testing.T) {
			fixture := newConnectorFixture(t, nil)
			server := fixture.server(t)
			first := registerConnectorClient(t, server, "Client A", method)
			second := registerConnectorClient(t, server, "Client B", method)
			tokens, _ := fixture.issueTokens(t, server, first)
			for _, token := range []string{tokens.AccessToken, tokens.RefreshToken} {
				values := url.Values{"client_id": {second.ClientID}, "token": {token}, "token_type_hint": {"incorrect"}}
				response := connectorForm(t, server, fixture.issuer+"/oauth/revoke", values, clientCredentials(second, values))
				if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "invalid_grant") {
					t.Fatalf("cross-client revocation: HTTP %d %s", response.Code, response.Body.String())
				}
			}
			if response := fixture.ping(t, server, tokens.AccessToken); response.Code != http.StatusOK {
				t.Fatalf("cross-client attempt changed access: HTTP %d", response.Code)
			}
			if method != "none" {
				// Refresh transport admission independently; this fixture exercises client
				// authentication rather than rate limits.
				server = fixture.server(t)
				for _, credentials := range []url.Values{
					{"client_id": {first.ClientID}, "token": {tokens.RefreshToken}},
					{"client_id": {first.ClientID}, "client_secret": {"wrong-secret"}, "token": {tokens.RefreshToken}},
				} {
					response := connectorForm(t, server, fixture.issuer+"/oauth/revoke", credentials, nil)
					if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "invalid_client") {
						t.Fatalf("missing/wrong/downgraded credentials: HTTP %d %s", response.Code, response.Body.String())
					}
				}
				if response := fixture.ping(t, server, tokens.AccessToken); response.Code != http.StatusOK {
					t.Fatal("bad credentials changed token state")
				}
			}
			values := url.Values{"client_id": {first.ClientID}, "token": {tokens.RefreshToken}, "token_type_hint": {"access_token"}}
			response := connectorForm(t, server, fixture.issuer+"/oauth/revoke", values, clientCredentials(first, values))
			if response.Code != http.StatusOK || response.Body.Len() != 0 {
				t.Fatalf("family revoke with misleading hint: HTTP %d %s", response.Code, response.Body.String())
			}
			if response := fixture.ping(t, fixture.server(t), tokens.AccessToken); response.Code != http.StatusUnauthorized {
				t.Fatalf("family access remains usable: HTTP %d", response.Code)
			}
		})
	}
}

func TestRevocationOpaqueResponsesAndNarrowRoute(t *testing.T) {
	t.Parallel()
	fixture := newConnectorFixture(t, nil)
	server := fixture.server(t)
	client := registerConnectorClient(t, server, "Client", "none")
	tokens, _ := fixture.issueTokens(t, server, client)
	for _, token := range []string{tokens.AccessToken, tokens.AccessToken, "unknown-token"} {
		response := connectorForm(t, server, fixture.issuer+"/oauth/revoke", url.Values{"client_id": {client.ClientID}, "token": {token}}, nil)
		if response.Code != http.StatusOK || response.Body.Len() != 0 || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("revocation response reveals token status: HTTP %d %s", response.Code, response.Body.String())
		}
	}
	for _, parameters := range []url.Values{
		{"client_id": {client.ClientID}, "token": {tokens.RefreshToken}, "grant_id": {"any"}},
		{"client_id": {client.ClientID}, "token": {tokens.RefreshToken}, "revoke_all": {"true"}},
		{"client_id": {client.ClientID}, "token": {tokens.RefreshToken, tokens.AccessToken}},
	} {
		response := connectorForm(t, server, fixture.issuer+"/oauth/revoke", parameters, nil)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("administration/duplicate parameters accepted: HTTP %d", response.Code)
		}
	}
	response := connectorRequest(t, server, http.MethodGet, fixture.issuer+"/oauth/revoke", "", "", nil)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET revocation: HTTP %d", response.Code)
	}
	// Access-only revocation must preserve refresh authority.
	refreshed := connectorForm(t, fixture.server(t), fixture.issuer+"/oauth/token", url.Values{"client_id": {client.ClientID}, "grant_type": {"refresh_token"}, "resource": {fixture.options.Config.ResourceURL}, "refresh_token": {tokens.RefreshToken}}, nil)
	if refreshed.Code != http.StatusOK {
		t.Fatalf("access revocation or refused administration destroyed family: HTTP %d %s", refreshed.Code, refreshed.Body.String())
	}
}

func TestDurableRevocationAcrossRestart(t *testing.T) {
	t.Parallel()
	fixture := newConnectorFixture(t, nil)
	server := fixture.server(t)
	client := registerConnectorClient(t, server, "Client", "none")
	revoked, grant := fixture.issueTokens(t, server, client)
	unaffected, _ := fixture.issueTokens(t, server, client)
	if err := fixture.control.Revoke(t.Context(), grant); err != nil {
		t.Fatal(err)
	}
	restarted := fixture.restart(t)
	if response := fixture.ping(t, restarted, revoked.AccessToken); response.Code != http.StatusUnauthorized {
		t.Fatalf("restart restored revoked access: HTTP %d", response.Code)
	}
	refreshed := connectorForm(t, restarted, fixture.issuer+"/oauth/token", url.Values{"client_id": {client.ClientID}, "grant_type": {"refresh_token"}, "resource": {fixture.options.Config.ResourceURL}, "refresh_token": {revoked.RefreshToken}}, nil)
	if refreshed.Code != http.StatusBadRequest {
		t.Fatalf("restart restored refresh: HTTP %d", refreshed.Code)
	}
	if response := fixture.ping(t, restarted, unaffected.AccessToken); response.Code != http.StatusOK {
		t.Fatalf("revoke one affected another family: HTTP %d", response.Code)
	}
	if err := fixture.control.RevokeAll(t.Context()); err != nil {
		t.Fatal(err)
	}
	if response := fixture.ping(t, fixture.server(t), unaffected.AccessToken); response.Code != http.StatusUnauthorized {
		t.Fatalf("revoke all did not stop remaining family: HTTP %d", response.Code)
	}
}

func TestProtectedAccessExpiryOwnerAndReadiness(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"expiry", "owner", "local-control", "database"} {
		t.Run(change, func(t *testing.T) {
			fixture := newConnectorFixture(t, nil)
			server := fixture.server(t)
			client := registerConnectorClient(t, server, "Client", "none")
			tokens, _ := fixture.issueTokens(t, server, client)
			want := http.StatusUnauthorized
			switch change {
			case "expiry":
				fixture.now = fixture.now.Add(15 * time.Minute)
			case "owner":
				for _, id := range []int64{43, 42} {
					if _, err := fixture.db.UpsertIdentity(t.Context(), store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, UserID: id, Handle: "owner", Source: store.SourceGH}); err != nil {
						t.Fatal(err)
					}
				}
			case "local-control":
				fixture.control.unavailable.Store(true)
				want = http.StatusServiceUnavailable
			case "database":
				if err := fixture.db.Close(); err != nil {
					t.Fatal(err)
				}
				want = http.StatusServiceUnavailable
			}
			if response := fixture.ping(t, server, tokens.AccessToken); response.Code != want {
				t.Fatalf("%s failed closed with HTTP %d, want %d", change, response.Code, want)
			}
		})
	}
}

func TestConstructionRequiresTrustedControl(t *testing.T) {
	t.Parallel()
	fixture := newConnectorFixture(t, nil)
	fixture.options.LocalControl = nil
	if _, err := New(t.Context(), fixture.options); err == nil {
		t.Fatal("connector constructed without working local revocation")
	}
	fixture.options.LocalControl = &LocalControl{}
	if _, err := New(t.Context(), fixture.options); err == nil {
		t.Fatal("empty capability bypassed local revocation gate")
	}
	fixture.options.LocalControl = nil
	if _, err := NewLocalControl(t.Context(), fixture.db, &connectorUnavailableControl{}); err == nil {
		t.Fatal("unavailable command host registered")
	}
}

type connectorUnavailableControl struct{}

func (*connectorUnavailableControl) Ready(context.Context) error { return errors.New("not available") }
func (*connectorUnavailableControl) List(context.Context) ([]store.ConnectorGrantSummary, error) {
	return nil, nil
}
func (*connectorUnavailableControl) Revoke(context.Context, string) error { return nil }
func (*connectorUnavailableControl) RevokeAll(context.Context) error      { return nil }

func TestRefreshReplayAndSpentAncestorRemainRevokedAfterRestart(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"replay", "revoke spent ancestor"} {
		t.Run(action, func(t *testing.T) {
			fixture := newConnectorFixture(t, nil)
			server := fixture.server(t)
			client := registerConnectorClient(t, server, "Client", "none")
			original, _ := fixture.issueTokens(t, server, client)
			unrelated, _ := fixture.issueTokens(t, server, client)
			refreshed := connectorForm(t, server, fixture.issuer+"/oauth/token", url.Values{"client_id": {client.ClientID}, "grant_type": {"refresh_token"}, "resource": {fixture.options.Config.ResourceURL}, "refresh_token": {original.RefreshToken}}, nil)
			var successor connectorTestTokens
			connectorDecode(t, refreshed, &successor)
			if refreshed.Code != 200 || successor.RefreshToken == "" {
				t.Fatalf("initial rotation: HTTP %d %s", refreshed.Code, refreshed.Body.String())
			}
			if response := fixture.ping(t, server, original.AccessToken); response.Code != 200 {
				t.Fatal("rotation unexpectedly revoked sibling access")
			}
			var invalidated *httptest.ResponseRecorder
			if action == "replay" {
				invalidated = connectorForm(t, server, fixture.issuer+"/oauth/token", url.Values{"client_id": {client.ClientID}, "grant_type": {"refresh_token"}, "resource": {fixture.options.Config.ResourceURL}, "refresh_token": {original.RefreshToken}}, nil)
				if invalidated.Code != 400 {
					t.Fatalf("spent refresh replay: HTTP %d", invalidated.Code)
				}
			} else {
				invalidated = connectorForm(t, server, fixture.issuer+"/oauth/revoke", url.Values{"client_id": {client.ClientID}, "token": {original.RefreshToken}, "token_type_hint": {"access_token"}}, nil)
				if invalidated.Code != 200 || invalidated.Body.Len() != 0 {
					t.Fatalf("spent ancestor revocation: HTTP %d", invalidated.Code)
				}
			}
			restarted := fixture.restart(t)
			for _, token := range []string{original.AccessToken, successor.AccessToken} {
				if response := fixture.ping(t, restarted, token); response.Code != 401 {
					t.Fatalf("restart restored invalidated family access: HTTP %d", response.Code)
				}
			}
			response := connectorForm(t, restarted, fixture.issuer+"/oauth/token", url.Values{"client_id": {client.ClientID}, "grant_type": {"refresh_token"}, "resource": {fixture.options.Config.ResourceURL}, "refresh_token": {successor.RefreshToken}}, nil)
			if response.Code != 400 {
				t.Fatalf("restart restored successor refresh: HTTP %d", response.Code)
			}
			if response := fixture.ping(t, restarted, unrelated.AccessToken); response.Code != 200 {
				t.Fatalf("family invalidation affected unrelated grant: HTTP %d", response.Code)
			}
		})
	}
}

func TestConfidentialBasicCredentialFailuresPreserveGrant(t *testing.T) {
	t.Parallel()
	fixture := newConnectorFixture(t, nil)
	server := fixture.server(t)
	client := registerConnectorClient(t, server, "Client", "client_secret_basic")
	tokens, _ := fixture.issueTokens(t, server, client)
	headers := http.Header{}
	request := &http.Request{Header: headers}
	request.SetBasicAuth(client.ClientID, "wrong-secret-marker")
	response := connectorForm(t, server, fixture.issuer+"/oauth/revoke", url.Values{"token": {tokens.RefreshToken}}, headers)
	if response.Code != 401 || !strings.HasPrefix(response.Header().Get("WWW-Authenticate"), "Basic ") {
		t.Fatalf("failed Basic credentials did not challenge: HTTP %d", response.Code)
	}
	request.SetBasicAuth(client.ClientID, client.ClientSecret)
	response = connectorForm(t, server, fixture.issuer+"/oauth/revoke", url.Values{"token": {tokens.RefreshToken}, "client_id": {client.ClientID}}, headers)
	if response.Code != 401 {
		t.Fatalf("multiple authentication methods accepted: HTTP %d", response.Code)
	}
	if response := fixture.ping(t, server, tokens.AccessToken); response.Code != 200 {
		t.Fatalf("failed authentication changed grant: HTTP %d", response.Code)
	}
}
