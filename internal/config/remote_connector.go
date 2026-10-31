package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// ConnectorSecretRef holds an indirect credential reference.
type ConnectorSecretRef struct {
	Keychain Keychain `yaml:"keychain"`
	Env      string   `yaml:"env"`
}

// RemoteConnector configures the separate owner-only HTTPS proxy backend.
// Enablement does not supply the trusted local revocation capability.
type RemoteConnector struct {
	Workflows       []WorkflowRegistration      `yaml:"workflows"`
	WorkflowNotices []WorkflowNoticeDestination `yaml:"workflow_notices"`
	Enabled         bool                        `yaml:"enabled"`
	ListenAddr      string                      `yaml:"listen"`
	ResourceURL     string                      `yaml:"resource_url"`
	IssuerURL       string                      `yaml:"issuer_url"`
	OwnerID         int64                       `yaml:"owner_id"`
	GitHubClientID  string                      `yaml:"github_client_id"`
	GitHubSecretRef ConnectorSecretRef          `yaml:"github_secret"`
	CookieSecretRef ConnectorSecretRef          `yaml:"cookie_secret"`
	AllowedOrigins  []string                    `yaml:"allowed_origins"`
}

// DefaultRemoteConnector is disabled until explicitly configured.
func DefaultRemoteConnector() RemoteConnector {
	return RemoteConnector{ListenAddr: "127.0.0.1:8788", ResourceURL: "https://cloudvane.at/litefactory/mcp", IssuerURL: "https://cloudvane.at/litefactory", AllowedOrigins: []string{}}
}

// Validate rejects ambiguous public URLs and non-loopback listeners.
func (c RemoteConnector) Validate() error {
	var errs []error
	errs = append(errs, validateWorkflowRegistrations(c.Workflows, c.WorkflowNotices))
	host, port, err := net.SplitHostPort(c.ListenAddr)
	ip := net.ParseIP(host)
	p, portErr := strconv.Atoi(port)
	if err != nil || ip == nil || !ip.IsLoopback() || portErr != nil || p < 1 || p > 65535 {
		errs = append(errs, errors.New("remote_connector.listen must be a numeric loopback address with a valid port"))
	}
	issuer, issuerErr := connectorURL(c.IssuerURL, "/litefactory")
	resource, resourceErr := connectorURL(c.ResourceURL, "/litefactory/mcp")
	if issuerErr != nil {
		errs = append(errs, fmt.Errorf("remote_connector.issuer_url: %w", issuerErr))
	}
	if resourceErr != nil {
		errs = append(errs, fmt.Errorf("remote_connector.resource_url: %w", resourceErr))
	}
	if issuerErr == nil && resourceErr == nil && issuer.Host != resource.Host {
		errs = append(errs, errors.New("remote_connector resource and issuer must share an HTTPS origin"))
	}
	for _, origin := range c.AllowedOrigins {
		if _, err := connectorURL(origin, ""); err != nil {
			errs = append(errs, errors.New("remote_connector.allowed_origins must contain exact HTTPS origins without paths"))
		}
	}
	if c.Enabled {
		if c.OwnerID <= 0 {
			errs = append(errs, errors.New("remote_connector.owner_id must be positive"))
		}
		if c.GitHubClientID == "" || len(c.GitHubClientID) > 128 || strings.ContainsAny(c.GitHubClientID, " \t\r\n") {
			errs = append(errs, errors.New("remote_connector.github_client_id is required and must be bounded"))
		}
		errs = append(errs, c.GitHubSecretRef.validate("github_secret"), c.CookieSecretRef.validate("cookie_secret"))
	}
	return errors.Join(errs...)
}

func connectorURL(raw, path string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" || u.Path != path || strings.ContainsAny(raw, "#\\\r\n\t ") || strings.Contains(u.Host, "%") || strings.Contains(u.Host, "*") || strings.HasSuffix(u.Host, ":") {
		return nil, errors.New("must be an unambiguous fixed HTTPS URL with the required path")
	}
	if u.Port() != "" {
		p, e := strconv.Atoi(u.Port())
		if e != nil || p < 1 || p > 65535 {
			return nil, errors.New("invalid HTTPS port")
		}
	}
	return u, nil
}

func (r ConnectorSecretRef) validate(name string) error {
	keychain := r.Keychain.Service != "" || r.Keychain.Account != ""
	if keychain && r.Env != "" || !keychain && r.Env == "" || keychain && (r.Keychain.Service == "" || r.Keychain.Account == "") {
		return fmt.Errorf("remote_connector.%s requires exactly one complete keychain or environment reference", name)
	}
	if r.Env != "" {
		for i, ch := range r.Env {
			valid := ch == '_' || ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z' || i > 0 && ch >= '0' && ch <= '9'
			if !valid {
				return fmt.Errorf("remote_connector.%s has an invalid environment variable name", name)
			}
		}
	}
	return nil
}
