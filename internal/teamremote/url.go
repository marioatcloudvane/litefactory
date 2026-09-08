package teamremote

import (
	"fmt"
	"net/url"
	"path"
	"strings"
)

// NormalizeIndexURL explicitly translates GitHub file locations before origin
// enforcement. It is not permission for HTTP redirects to change origin.
func NormalizeIndexURL(raw string) (string, error) {
	u, err := safeURL(raw)
	if err != nil {
		return "", err
	}
	if strings.EqualFold(u.Host, "github.com") {
		parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
		if len(parts) < 3 {
			return "", fmt.Errorf("GitHub source must name a file")
		}
		owner, repo, ref, rest := parts[0], parts[1], "HEAD", parts[2:]
		if parts[2] == "blob" || parts[2] == "raw" {
			if len(parts) < 5 {
				return "", fmt.Errorf("GitHub source must name a ref and file")
			}
			ref, rest = parts[3], parts[4:]
		}
		u.Host = "raw.githubusercontent.com"
		u.Path = "/" + strings.Join(append([]string{owner, repo, ref}, rest...), "/")
		u.RawPath = ""
	}
	return u.String(), nil
}

func safeURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || strings.ContainsAny(raw, "\\\r\n\t ") {
		return nil, fmt.Errorf("source must be HTTPS without credentials, query or fragment")
	}
	if err := ValidateRelativePath(strings.TrimPrefix(u.EscapedPath(), "/")); err != nil {
		return nil, fmt.Errorf("source path: %w", err)
	}
	return u, nil
}

// ValidateRelativePath rejects ambiguity and traversal before filesystem or URL use.
func ValidateRelativePath(p string) error {
	if p == "" || strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\\%:?#\x00\r\n\t") || path.Clean(p) != p || p == "." || p == ".." || strings.HasPrefix(p, "../") {
		return fmt.Errorf("unsafe relative file path")
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("unsafe relative file path")
		}
	}
	return nil
}

// ResolveReference resolves a safe child path from the containing document's
// directory. Absolute references and parent traversal are never allowed.
func ResolveReference(base, relative string) (string, error) {
	u, err := safeURL(base)
	if err != nil {
		return "", err
	}
	if err := ValidateRelativePath(relative); err != nil {
		return "", err
	}
	u.Path = path.Join(path.Dir(u.Path), relative)
	u.RawPath = ""
	return u.String(), nil
}
