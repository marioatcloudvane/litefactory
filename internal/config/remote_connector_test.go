package config

import (
	"strings"
	"testing"
)

func configuredRemote() RemoteConnector {
	c := DefaultRemoteConnector()
	c.Enabled = true
	c.OwnerID = 42
	c.GitHubClientID = "dedicated-app"
	c.GitHubSecretRef.Env = "CONNECTOR_GITHUB_SECRET"
	c.CookieSecretRef.Env = "CONNECTOR_COOKIE_KEY"
	return c
}
func TestRemoteConnectorValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change func(*RemoteConnector)
	}{
		{"public listen", func(c *RemoteConnector) { c.ListenAddr = "0.0.0.0:8788" }},
		{"DNS loopback", func(c *RemoteConnector) { c.ListenAddr = "localhost:8788" }},
		{"bad port", func(c *RemoteConnector) { c.ListenAddr = "127.0.0.1:0" }},
		{"HTTP issuer", func(c *RemoteConnector) { c.IssuerURL = "http://cloudvane.at/litefactory" }},
		{"empty HTTPS port", func(c *RemoteConnector) { c.IssuerURL = "https://cloudvane.at:/litefactory" }},
		{"empty fragment", func(c *RemoteConnector) { c.ResourceURL += "#" }},
		{"URL credentials", func(c *RemoteConnector) { c.ResourceURL = "https://user@cloudvane.at/litefactory/mcp" }},
		{"URL query", func(c *RemoteConnector) { c.ResourceURL += "?credential=hidden" }},
		{"encoded path", func(c *RemoteConnector) { c.ResourceURL = "https://cloudvane.at/litefactory/%6dcp" }},
		{"origin mismatch", func(c *RemoteConnector) { c.ResourceURL = "https://other.example/litefactory/mcp" }},
		{"origin path", func(c *RemoteConnector) { c.AllowedOrigins = []string{"https://client.example/path"} }},
		{"origin wildcard", func(c *RemoteConnector) { c.AllowedOrigins = []string{"https://*.example"} }},
		{"missing owner", func(c *RemoteConnector) { c.OwnerID = 0 }},
		{"missing app", func(c *RemoteConnector) { c.GitHubClientID = "" }},
		{"missing secret", func(c *RemoteConnector) { c.GitHubSecretRef = ConnectorSecretRef{} }},
		{"two secret sources", func(c *RemoteConnector) {
			c.CookieSecretRef.Keychain = Keychain{Service: "connector", Account: "cookie"}
		}},
		{"partial keychain", func(c *RemoteConnector) {
			c.CookieSecretRef = ConnectorSecretRef{Keychain: Keychain{Service: "connector"}}
		}},
		{"invalid env", func(c *RemoteConnector) { c.CookieSecretRef.Env = "9BAD" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := configuredRemote()
			test.change(&c)
			if c.Validate() == nil {
				t.Fatal("invalid connector security configuration accepted")
			}
		})
	}
	c := configuredRemote()
	c.AllowedOrigins = []string{"https://client.example"}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c := DefaultRemoteConnector(); c.Enabled || c.Validate() != nil {
		t.Fatal("default must be disabled and valid")
	}
	c = configuredRemote()
	c.OwnerID = 0
	c.GitHubClientID = ""
	c.CookieSecretRef = ConnectorSecretRef{}
	err := c.Validate()
	for _, field := range []string{"owner_id", "github_client_id", "cookie_secret"} {
		if err == nil || !strings.Contains(err.Error(), field) {
			t.Errorf("missing independent error for %s", field)
		}
	}
}
