package github

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// This file is the whole of litefactory's pagination support: an RFC 8288
// Link-header reader and a loop over it. That is the entire reason there is no
// SDK dependency in this package. If a *third* endpoint needs a special case
// beyond this, the trade has changed and a library is worth revisiting — two do
// not justify one.

// fetchPages walks every page of a list endpoint, decoding each into []T and
// concatenating the results. It returns the number of requests made, which is
// what the per-repo cost line in the tick log reports (AC-2.8).
//
// A missing or malformed Link header simply ends the walk: one page is the
// normal shape of a small repository's issue list, and GitHub omits the header
// entirely in that case.
func fetchPages[T any](ctx context.Context, c *Client, endpoint string) ([]T, int, error) {
	// An empty endpoint means [Client.endpoint] had no base to build one from.
	// Falling through the loop would report "no pages" — an empty issue list
	// from a client that never made a request, which is indistinguishable from
	// a quiet repository and would advance a watermark over unseen comments.
	if c == nil || endpoint == "" {
		return nil, 0, ErrUninitialized
	}

	var (
		all      []T
		requests int
	)
	for next := endpoint; next != ""; {
		if requests >= maxPages {
			return nil, requests, fmt.Errorf("%w: stopped after %d requests to %s",
				ErrTooManyPages, requests, endpoint)
		}
		var page []T
		res, err := c.do(ctx, http.MethodGet, next, nil, &page)
		requests++
		if err != nil {
			return nil, requests, err
		}
		all = append(all, page...)
		next = res.NextPage
	}
	return all, requests, nil
}

// nextPage resolves the rel="next" target of a Link header and checks that it
// stays on this client's own host.
//
// The check is not paranoia about GitHub: it is what stops a compromised or
// misconfigured intermediary from walking the client onto another host, where
// the next request would be built — Authorization header and all — by this
// package rather than by net/http's redirect logic, which is the thing that
// normally strips credentials on a cross-host hop.
func (c *Client) nextPage(header string) (string, error) {
	raw := nextLink(header)
	if raw == "" {
		return "", nil
	}
	next, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("malformed next-page link %q: %w", raw, err)
	}
	if !strings.EqualFold(next.Scheme, c.base.Scheme) || !strings.EqualFold(next.Host, c.base.Host) {
		return "", fmt.Errorf("next-page link %q: %w: leaves %s://%s",
			raw, ErrInvalidArgument, c.base.Scheme, c.base.Host)
	}
	return next.String(), nil
}

// nextLink returns the target of the rel="next" link-value in a Link header, or
// "" when there is none.
func nextLink(header string) string {
	for rest := header; ; {
		target, params, remainder, ok := cutLinkValue(rest)
		if !ok {
			return ""
		}
		if hasRel(params, "next") {
			return target
		}
		rest = remainder
	}
}

// cutLinkValue reads one <target>; params link-value off the front of s,
// returning the target, its parameter text, and what is left.
//
// Link-values are separated by commas, but a URL may legitimately contain one
// (GitHub's own cursor pagination puts commas in the "after" parameter), so the
// split is on the angle brackets that delimit the target and on the '<' that
// opens the next value — never on a comma.
func cutLinkValue(s string) (target, params, rest string, ok bool) {
	open := strings.IndexByte(s, '<')
	if open < 0 {
		return "", "", "", false
	}
	s = s[open+1:]

	end := strings.IndexByte(s, '>')
	if end < 0 {
		return "", "", "", false
	}
	target = strings.TrimSpace(s[:end])
	s = s[end+1:]

	if nextOpen := strings.IndexByte(s, '<'); nextOpen >= 0 {
		return target, s[:nextOpen], s[nextOpen:], true
	}
	return target, s, "", true
}

// hasRel reports whether a link-value's parameter text carries rel=want. The
// relation may be quoted or bare, and a quoted one may hold several
// space-separated relations.
func hasRel(params, want string) bool {
	for _, param := range strings.Split(params, ";") {
		name, value, found := strings.Cut(strings.TrimSpace(param), "=")
		if !found || !strings.EqualFold(strings.TrimSpace(name), "rel") {
			continue
		}
		value = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(value), ","))
		value = strings.Trim(value, `"'`)
		for _, rel := range strings.Fields(value) {
			if strings.EqualFold(rel, want) {
				return true
			}
		}
	}
	return false
}
