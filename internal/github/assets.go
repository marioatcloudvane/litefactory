package github

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

// assetAllowedHosts is the SSRF allow-list for [Client.FetchAsset]: the
// hosts GitHub actually serves issue/comment attachment bytes from. Nothing
// else is ever dialed, no matter what an issue body or a redirect claims.
//
// github.com itself is included, path-restricted below, because that is
// where the modern (2023+) drag-and-drop attachment upload flow places its
// asset URLs (https://github.com/user-attachments/assets/<uuid>). The other
// three are the older githubusercontent.com attachment hosts, kept for
// attachments minted before that migration and for private-repo images.
var assetAllowedHosts = map[string]bool{
	"github.com":                                true,
	"objects.githubusercontent.com":             true,
	"user-images.githubusercontent.com":         true,
	"private-user-images.githubusercontent.com": true,
}

// assetGitHubPathPrefix is the only github.com path FetchAsset will dial.
// github.com serves far more than attachments; without this the host check
// alone would allow-list the entire site.
const assetGitHubPathPrefix = "/user-attachments/assets/"

// assetURLAllowed reports whether u is a GitHub attachment asset URL
// FetchAsset may dial: HTTPS, on [assetAllowedHosts], and — for github.com
// itself — under [assetGitHubPathPrefix]. It is checked before the initial
// request and again from CheckRedirect on every redirect hop, so a URL that
// fails this check is one FetchAsset never sends the installation token to.
func assetURLAllowed(u *url.URL) bool {
	if u == nil || u.Scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if !assetAllowedHosts[host] {
		return false
	}
	if host == "github.com" && !strings.HasPrefix(u.Path, assetGitHubPathPrefix) {
		return false
	}
	return true
}

// FetchAsset fetches the bytes at rawURL — a GitHub-hosted issue or comment
// attachment — and returns them as an unread stream, alongside the
// response's declared, unsniffed Content-Type header. The caller is
// internal/attach, through an interface it declares at its own point of
// use: this package exports no Forge-shaped abstraction (see doc.go and
// surface_test.go's TestNoForgeAbstraction).
//
// This is the only place litefactory dials an attachment host, and it does
// so with the same authenticated *http.Client every other method in this
// package uses (via [Client.api].Client()), never [secrets.Token.Reveal]
// directly — see surface_test.go's TestRevealIsCalledExactlyOnce. Reusing
// that client means every request FetchAsset sends, including a followed
// redirect, carries the installation token's Authorization header. That is
// exactly why rawURL and every redirect target must pass [assetURLAllowed]
// before this package dials it: the check is what stops the token from
// ever reaching a host GitHub did not put it in front of.
//
// FetchAsset is single-shot. It retries nothing and sleeps nowhere — same
// as the rest of this package (see doc.go) — and it enforces no size
// ceiling of its own: streaming, bounding and aborting an oversized body is
// internal/attach's job (T-3.5), not this transport call's.
//
// The caller is responsible for closing the returned ReadCloser.
func (c *Client) FetchAsset(ctx context.Context, rawURL string) (io.ReadCloser, string, error) {
	if c == nil || c.api == nil {
		return nil, "", ErrUninitialized
	}
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return nil, "", fmt.Errorf("github: fetch asset: %w: empty url", ErrInvalidArgument)
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return nil, "", fmt.Errorf("github: fetch asset: %w: url did not parse", ErrInvalidArgument)
	}
	if !assetURLAllowed(parsed) {
		return nil, "", fmt.Errorf("github: fetch asset at host %q: %w", parsed.Hostname(), ErrAssetNotAllowed)
	}

	httpClient := c.api.Client()
	httpClient.CheckRedirect = func(req *http.Request, _ []*http.Request) error {
		if !assetURLAllowed(req.URL) {
			return fmt.Errorf("github: fetch asset redirected to host %q: %w", req.URL.Hostname(), ErrAssetNotAllowed)
		}
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, "", fmt.Errorf("github: fetch asset: %w", err)
	}

	start := c.clock()
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("github: fetch asset at host %q: %w", parsed.Hostname(), err)
	}
	c.logger().DebugContext(ctx, "github: fetch asset",
		slog.String("host", parsed.Hostname()),
		slog.Int("status", resp.StatusCode),
		slog.Duration("duration", c.clock().Sub(start)),
	)

	switch resp.StatusCode {
	case http.StatusOK:
		return resp.Body, resp.Header.Get("Content-Type"), nil
	case http.StatusUnauthorized, http.StatusNotFound:
		defer resp.Body.Close()
		return nil, "", fmt.Errorf("github: fetch asset at host %q: %w: %s",
			parsed.Hostname(), ErrAssetUnauthorized, http.StatusText(resp.StatusCode))
	default:
		defer resp.Body.Close()
		return nil, "", fmt.Errorf("github: fetch asset at host %q: %w: %s",
			parsed.Hostname(), ErrUnexpectedStatus, http.StatusText(resp.StatusCode))
	}
}
