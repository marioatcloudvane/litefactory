package daemon

import (
	"errors"
	"net"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestRemoteOwnerPinAwayAndBackSurvivesPreparationFailure(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"missing control", "missing secret", "occupied listener"} {
		t.Run(failure, func(t *testing.T) {
			t.Parallel()
			s := remoteSetupFixture(t)
			now := time.Now()
			before, err := s.DB.ConnectorOwner(t.Context(), 42)
			if err != nil {
				t.Fatal(err)
			}
			client := store.ConnectorClient{ID: "client", Name: "client", AuthMethod: "none", Redirects: []string{"https://client.test/callback"}, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
			if err := s.DB.RegisterConnectorClient(t.Context(), client, now); err != nil {
				t.Fatal(err)
			}
			browser := store.ConnectorBrowser{ID: "browser", ClientID: client.ID, RedirectURI: client.Redirects[0], Resource: s.Config.RemoteConnector.ResourceURL, Scope: "factory:overview", PKCEChallenge: "challenge", NonceDigest: "nonce", UpstreamStateDigest: "upstream", OwnerGeneration: before.Generation, ExpiresAt: now.Add(10 * time.Minute)}
			if err := s.DB.CreateConnectorBrowser(t.Context(), browser, 42, now); err != nil {
				t.Fatal(err)
			}
			if err := s.DB.VerifyConnectorBrowser(t.Context(), browser.ID, "nonce", "upstream", 42, now); err != nil {
				t.Fatal(err)
			}
			grant := store.ConnectorGrant{ID: "grant", ClientID: client.ID, Resource: browser.Resource, Scope: browser.Scope, OwnerID: 42, OwnerGeneration: before.Generation, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
			code := store.ConnectorCode{Digest: "code", GrantID: grant.ID, ClientID: client.ID, RedirectURI: browser.RedirectURI, Resource: browser.Resource, Scope: browser.Scope, PKCEChallenge: browser.PKCEChallenge, ExpiresAt: now.Add(5 * time.Minute)}
			if err := s.DB.ConsentConnectorBrowser(t.Context(), browser.ID, "nonce", 42, now, grant, code, true); err != nil {
				t.Fatal(err)
			}
			exchange := store.ConnectorExchange{Digest: code.Digest, ClientID: client.ID, RedirectURI: code.RedirectURI, Resource: code.Resource, Scope: code.Scope, PKCEChallenge: code.PKCEChallenge, OwnerID: 42, Now: now, Pair: store.ConnectorTokenPair{AccessDigest: "access", RefreshDigest: "refresh", AccessExpiresAt: now.Add(15 * time.Minute)}}
			if _, err := s.DB.ExchangeConnectorCode(t.Context(), exchange); err != nil {
				t.Fatal(err)
			}
			s.Config.RemoteConnector.OwnerID = 43
			if runtime, err := prepareRemoteConnector(t.Context(), s); err == nil || runtime != nil {
				t.Fatal("mismatched owner accepted")
			}
			s.Config.RemoteConnector.OwnerID = 42
			switch failure {
			case "missing control":
				s.ConnectorControl = nil
			case "missing secret":
				s.ConnectionSecrets.LookupEnv = func(string) (string, bool) { return "", false }
			case "occupied listener":
				var lc net.ListenConfig
				occupied, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				defer occupied.Close()
				s.Config.RemoteConnector.ListenAddr = occupied.Addr().String()
			}
			// The restored pin must reconcile even if later prerequisites fail.
			runtime, preparationErr := prepareRemoteConnector(t.Context(), s)
			if preparationErr == nil {
				t.Fatal("expected preparation failure")
			}
			if runtime != nil {
				_ = runtime.lifecycle.Close()
			}
			after, err := s.DB.ConnectorOwner(t.Context(), 42)
			if err != nil {
				t.Fatal(err)
			}
			if after.Generation <= before.Generation {
				t.Fatal("owner generation did not advance")
			}
			if _, err := s.DB.CheckConnectorAccess(t.Context(), "access", 42, browser.Resource, browser.Scope, now); !errors.Is(err, store.ErrConnectorInvalid) {
				t.Fatalf("old access revived: %v", err)
			}
		})
	}
}
