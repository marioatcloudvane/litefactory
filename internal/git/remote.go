package git

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"
)

// ForgeGitHub is the only forge litefactory speaks to. It is a value, not a
// capability negotiation: the forge protocol is deliberately deferred until
// there is a second implementation to extract it from.
const ForgeGitHub = "github"

// supportedHosts maps a remote host to its forge. ssh.github.com is GitHub's
// port-443 SSH endpoint, used by people behind firewalls that block port 22;
// rejecting it as "not GitHub" would be both wrong and baffling.
var supportedHosts = map[string]string{
	"github.com":     ForgeGitHub,
	"ssh.github.com": ForgeGitHub,
}

// RepoRef identifies a repository on a forge.
type RepoRef struct {
	// Forge is the hosting service, always [ForgeGitHub] at M0.
	Forge string
	// Owner is the user or organisation, in the case the remote spelled it.
	Owner string
	// Name is the repository name with any ".git" suffix removed.
	Name string
}

// String renders the reference as "forge:owner/name", the form stored in the
// database and printed by "repo list".
func (r RepoRef) String() string { return r.Forge + ":" + r.Owner + "/" + r.Name }

// Slug renders the reference as "owner/name", the form GitHub's API paths use.
func (r RepoRef) Slug() string { return r.Owner + "/" + r.Name }

// RemoteURLError reports a remote URL that could not be turned into a
// [RepoRef]. It keeps the URL and, when one was found, the host, so the message
// can tell the user which host was rejected instead of only that one was.
type RemoteURLError struct {
	// URL is the remote URL as configured, whitespace trimmed.
	URL string
	// Host is the host that was parsed out, empty when parsing failed first.
	Host string
	// Err is [ErrUnsupportedForge] or [ErrMalformedRemoteURL].
	Err error
}

func (e *RemoteURLError) Error() string {
	if e.Host != "" {
		return fmt.Sprintf("remote %q: %v (host %q)", e.URL, e.Err, e.Host)
	}
	return fmt.Sprintf("remote %q: %v", e.URL, e.Err)
}

func (e *RemoteURLError) Unwrap() error { return e.Err }

// ParseRemoteURL turns an origin URL into a [RepoRef]. It is pure: no network,
// no filesystem, no git.
//
// All four forms git uses are accepted, with or without the ".git" suffix and
// with or without trailing slashes:
//
//	git@github.com:owner/name.git
//	https://github.com/owner/name.git
//	ssh://git@github.com/owner/name
//	ssh://git@github.com:443/owner/name.git
//
// The host is compared case-insensitively; owner and name keep the case the
// remote spelled them with, because that is the case GitHub displays. A host
// other than GitHub is rejected with [ErrUnsupportedForge] rather than guessed
// at.
func ParseRemoteURL(raw string) (RepoRef, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return RepoRef{}, &RemoteURLError{URL: trimmed, Err: ErrMalformedRemoteURL}
	}

	host, path, err := splitRemoteURL(trimmed)
	if err != nil {
		return RepoRef{}, &RemoteURLError{URL: trimmed, Err: err}
	}
	forge, ok := supportedHosts[strings.ToLower(host)]
	if !ok {
		return RepoRef{}, &RemoteURLError{URL: trimmed, Host: host, Err: ErrUnsupportedForge}
	}
	owner, name, err := splitRepoPath(path)
	if err != nil {
		return RepoRef{}, &RemoteURLError{URL: trimmed, Host: host, Err: err}
	}
	return RepoRef{Forge: forge, Owner: owner, Name: name}, nil
}

// splitRemoteURL separates the host from the repository path.
func splitRemoteURL(raw string) (host, path string, err error) {
	if strings.Contains(raw, "://") {
		parsed, perr := url.Parse(raw)
		if perr != nil {
			return "", "", ErrMalformedRemoteURL
		}
		switch strings.ToLower(parsed.Scheme) {
		case "ssh", "git", "http", "https":
		default:
			return "", "", ErrMalformedRemoteURL
		}
		// Hostname drops any port and the brackets around an IPv6 literal.
		if parsed.Hostname() == "" {
			return "", "", ErrMalformedRemoteURL
		}
		return parsed.Hostname(), parsed.Path, nil
	}

	// SCP-like syntax: [user@]host:path. This is not a URL, and net/url does
	// not merely fail on it — it succeeds with the wrong answer, reading
	// "git@github.com:owner/name.git" as scheme "git@github.com". So it is
	// split by hand, and only after "://" has been ruled out above.
	colon := strings.Index(raw, ":")
	if colon < 0 {
		return "", "", ErrMalformedRemoteURL
	}
	// A slash before the colon means a local path such as ../mirror:copy,
	// which git also treats as a path rather than as SCP syntax.
	if slash := strings.Index(raw, "/"); slash >= 0 && slash < colon {
		return "", "", ErrMalformedRemoteURL
	}
	host, path = raw[:colon], raw[colon+1:]
	if at := strings.LastIndex(host, "@"); at >= 0 {
		host = host[at+1:]
	}
	host = strings.Trim(host, "[]")
	if host == "" {
		return "", "", ErrMalformedRemoteURL
	}
	return host, path, nil
}

// splitRepoPath reduces a repository path to its owner and name, stripping
// surrounding slashes and the ".git" suffix. GitHub paths are exactly two
// segments deep, so anything else is malformed rather than merely unusual.
func splitRepoPath(path string) (owner, name string, err error) {
	trimmed := strings.Trim(path, "/")
	trimmed = strings.TrimSuffix(trimmed, ".git")
	trimmed = strings.Trim(trimmed, "/")

	owner, name, ok := strings.Cut(trimmed, "/")
	if !ok || strings.Contains(name, "/") {
		return "", "", ErrMalformedRemoteURL
	}
	if !validPathSegment(owner) || !validPathSegment(name) {
		return "", "", ErrMalformedRemoteURL
	}
	return owner, name, nil
}

// validPathSegment rejects the segments that would make a nonsense reference:
// empty, a relative-path element, or anything carrying whitespace or a
// separator.
func validPathSegment(s string) bool {
	if s == "" || s == "." || s == ".." {
		return false
	}
	return !strings.ContainsAny(s, `\:`) &&
		strings.IndexFunc(s, unicode.IsSpace) < 0 &&
		strings.IndexFunc(s, unicode.IsControl) < 0
}
