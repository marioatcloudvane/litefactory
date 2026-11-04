package linear

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/cloudvane/litefactory/internal/ticket"
)

// assetHost is the only host FetchAsset sends the connection credential to:
// where Linear serves files pasted into issues and comments. internal/attach
// keeps its own recognition list; this check is the credential boundary and
// is deliberately not shared with it.
const assetHost = "uploads.linear.app"

// assetURLAllowed reports whether u is an https URL on [assetHost].
func assetURLAllowed(u *url.URL) bool {
	return u != nil && u.Scheme == "https" && u.User == nil && strings.EqualFold(u.Hostname(), assetHost) && (u.Port() == "" || u.Port() == "443")
}

// FetchAsset fetches one Linear upload and returns its unread body with the
// declared Content-Type. Anything not https on uploads.linear.app is refused
// before dialing. The request carries the same Authorization form as GraphQL
// requests, goes through the connection's request accounting, and is sent by
// the client's transport, which never follows redirects, so the credential
// cannot be forwarded to another host. It is single-shot; retrying, sniffing
// and size limits belong to internal/attach. Errors carry neither the URL nor
// the credential. The caller closes the returned body.
func (c *Client) FetchAsset(ctx context.Context, rawURL string) (_ io.ReadCloser, _ string, resultErr error) {
	const operation = "fetch asset"
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || !assetURLAllowed(parsed) {
		return nil, "", failure(operation, ticket.Refused, "asset url is not a Linear upload")
	}
	if c.config.RequestContext != nil {
		var finish func(error)
		ctx, finish, err = c.config.RequestContext(ctx)
		if err != nil {
			return nil, "", err
		}
		if finish != nil {
			defer func() { finish(resultErr) }()
		}
	}
	if l := requestLogger.Load(); l != nil {
		l.DebugContext(ctx, "linear request", "operation", operation, "mutation", false)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, "", failure(operation, ticket.Refused, "invalid asset request")
	}
	authorization := c.token.Reveal()
	if c.config.AuthKind == "oauth" {
		authorization = "Bearer " + authorization
	}
	req.Header.Set("Authorization", authorization)
	response, err := c.http.Do(req)
	if err != nil {
		return nil, "", failure(operation, ticket.Unavailable, "asset transport failed")
	}
	if response.StatusCode != http.StatusOK {
		_ = response.Body.Close()
		kind := ticket.Unavailable
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusNotFound {
			kind = ticket.Refused
		}
		return nil, "", failure(operation, kind, "asset request returned "+strings.ToLower(http.StatusText(response.StatusCode)))
	}
	return response.Body, response.Header.Get("Content-Type"), nil
}
