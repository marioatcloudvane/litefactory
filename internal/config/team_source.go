package config

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// DefaultTeamSource is the anonymous standard catalogue. Publication is separate
// from configuration: this constant does not promise that a catalogue exists.
const DefaultTeamSource = "https://raw.githubusercontent.com/marioatcloudvane/litefactory/main/teams/teams.json"

// NormalizeTeamSource returns the canonical source identity used for local
// namespacing. Errors deliberately exclude the input URL, which may contain
// credentials. Empty selects the standard source.
func NormalizeTeamSource(source string) (string, error) {
	if source == "" {
		return DefaultTeamSource, nil
	}
	invalid := func() (string, error) {
		return "", fmt.Errorf("settings.team_source: require an anonymous absolute HTTPS catalogue URL without credentials, query or fragment and with a confined path")
	}
	if strings.TrimSpace(source) != source || strings.ContainsAny(source, "\x00\r\n\\") {
		return invalid()
	}
	u, err := url.Parse(source)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || u.Hostname() == "" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(source, "#") {
		return invalid()
	}
	if u.Path == "" || !strings.HasPrefix(u.Path, "/") || strings.HasSuffix(u.Path, "/") {
		return invalid()
	}
	// Validate decoded segments too; escaped separators can otherwise conceal
	// traversal or give different servers different interpretations of identity.
	for _, part := range strings.Split(strings.TrimPrefix(u.EscapedPath(), "/"), "/") {
		decoded, decodeErr := url.PathUnescape(part)
		if decodeErr != nil || decoded == "" || decoded == "." || decoded == ".." || strings.ContainsAny(decoded, "/\\\x00\r\n%") {
			return invalid()
		}
	}
	u.Scheme = "https"
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return invalid()
		}
		port = strconv.Itoa(n)
	}
	if port == "443" {
		port = ""
	}
	switch {
	case port != "":
		u.Host = net.JoinHostPort(host, port)
	case strings.Contains(host, ":"):
		u.Host = "[" + host + "]"
	default:
		u.Host = host
	}
	u.RawPath = ""
	return u.String(), nil
}
