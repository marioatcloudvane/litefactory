package github

import (
	"context"
	"fmt"

	gogithub "github.com/google/go-github/v90/github"
)

// Pagination is the SDK's now (D1). What used to live here — an RFC 8288
// Link-header reader, `nextLink`/`cutLinkValue`/`hasRel`, and the same-host
// check that guarded the URL it produced — is gone: go-github parses the Link
// header, and what it hands back is a page *number*, not a URL.
//
// That is a stronger guarantee than the check it replaced, and worth stating
// because the check was a security control, not a convenience. This package
// never again builds a request from a target a response supplied. A Link header
// pointing at another host, or downgrading https to http, cannot move the walk:
// the page number is re-applied to the client's own base URL, so the
// Authorization header can only ever be sent where it was already being sent.
//
// What does *not* come from the SDK is the bound. go-github will follow
// whatever a server offers, and a proxy bug or a cursor that never advances
// would be an unbounded loop inside one tick that burns the hourly quota. That
// is [maxPages] and [ErrTooManyPages], below.
//
// One thing the hand-rolled reader did better, recorded so it is a known trade
// rather than a surprise: it split link-values on the angle brackets, so a
// comma inside a cursor URL could not lose the target. go-github splits on
// commas. Every endpoint this package calls uses page-number pagination, whose
// links carry no commas, so the trade costs nothing here — but a future
// cursor-paginated endpoint is where it would show up.

// pageFunc fetches one page of a list endpoint. page is 0 for the first
// request, which is what the SDK's ListOptions omits from the query.
type pageFunc[T any] func(ctx context.Context, page int) ([]T, *gogithub.Response, error)

// fetchPages walks every page of a list endpoint and concatenates the results.
// It returns the number of requests made, which is what the per-repo cost line
// in the tick log reports (AC-2.8).
//
// A response with no next page ends the walk: one page is the normal shape of a
// small repository's issue list, and GitHub omits the Link header entirely in
// that case.
func fetchPages[T any](ctx context.Context, c *Client, fetch pageFunc[T]) ([]T, int, error) {
	// A client [New] did not build has nothing to call. Falling through the
	// loop would report "no pages" — an empty issue list from a client that
	// never made a request, which is indistinguishable from a quiet repository
	// and would advance a watermark over unseen comments.
	if c == nil || c.api == nil {
		return nil, 0, ErrUninitialized
	}

	var (
		all      []T
		requests int
		lastURL  string
	)
	for page := 0; ; {
		if requests >= maxPages {
			return nil, requests, fmt.Errorf("%w: stopped after %d requests to %s",
				ErrTooManyPages, requests, lastURL)
		}
		items, resp, err := do(ctx, c, func(ctx context.Context) ([]T, *gogithub.Response, error) {
			return fetch(ctx, page)
		})
		requests++
		if resp != nil && resp.Response != nil {
			lastURL = requestURL(resp.Response)
		}
		if err != nil {
			return nil, requests, err
		}
		all = append(all, items...)
		if resp == nil || resp.NextPage == 0 {
			return all, requests, nil
		}
		page = resp.NextPage
	}
}
