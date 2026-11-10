package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ErrTeamSourceFolder names the one install source form.
var ErrTeamSourceFolder = errors.New("teams are installed from a teams/source folder: pass --source /absolute/path/to/teams/source, or set settings.team_source to that folder")

// TeamSourceFolder checks a team install source: the absolute path, or file://
// URL, of an existing teams/source folder. It returns the source identity, a
// file:// URL that names the installed namespace, and the folder path. Remote
// catalogue URLs and teams.json files are refused, and so is an empty source:
// there is no standard catalogue.
func TeamSourceFolder(source string) (identity, dir string, err error) {
	lower := strings.ToLower(source)
	if source == "" || strings.HasPrefix(lower, "https:") || strings.HasPrefix(lower, "http:") {
		return "", "", ErrTeamSourceFolder
	}
	if strings.HasPrefix(source, "/") && len(source) > 1 {
		source = strings.TrimRight(source, "/")
	}
	identity, err = NormalizeTeamSource(source)
	if err != nil || !strings.HasPrefix(identity, "file://") {
		return "", "", ErrTeamSourceFolder
	}
	u, err := url.Parse(identity)
	if err != nil {
		return "", "", ErrTeamSourceFolder
	}
	dir = filepath.FromSlash(u.Path)
	info, err := os.Stat(dir)
	if err != nil {
		return "", "", fmt.Errorf("team source folder %s: %w", dir, err)
	}
	if !info.IsDir() {
		return "", "", ErrTeamSourceFolder
	}
	return identity, dir, nil
}

// NormalizeTeamSource returns the canonical source identity used for local
// namespacing. Errors deliberately exclude the input, which may contain
// credentials. A folder on this machine is a file:// URL or an absolute path.
// HTTPS catalogue URLs are still accepted as identities so teams that earlier
// versions installed from a remote catalogue can be listed, relinked and
// removed; new installs go through TeamSourceFolder.
func NormalizeTeamSource(source string) (string, error) {
	invalid := func() (string, error) {
		return "", errors.New("settings.team_source: require the absolute path of a teams/source folder, without credentials, query or fragment and with a confined path")
	}
	if source == "" || strings.TrimSpace(source) != source || strings.ContainsAny(source, "\x00\r\n\\") {
		return invalid()
	}
	if strings.HasPrefix(source, "/") && !strings.HasPrefix(source, "//") {
		source = (&url.URL{Scheme: "file", Path: source}).String()
	}
	u, err := url.Parse(source)
	if err == nil && strings.EqualFold(u.Scheme, "file") {
		if u.Host != "" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(source, "#") {
			return invalid()
		}
	} else if err != nil || !strings.EqualFold(u.Scheme, "https") || u.Hostname() == "" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(source, "#") {
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
	if strings.EqualFold(u.Scheme, "file") {
		u.Scheme, u.RawPath = "file", ""
		return u.String(), nil
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
