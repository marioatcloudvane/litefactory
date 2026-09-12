package github

import (
	"context"
	"net/http"
	"strconv"
	"time"
)

// RequestObservation describes one actual transport request, before cached
// representations are reconstructed. It contains neither URLs nor credentials.
// Callbacks run synchronously and must be safe for concurrent callers.
type RequestObservation struct {
	Endpoint      string
	StatusCode    int // Zero means a transport failure without a response.
	Page          bool
	Authenticated bool
	CoreRateValid bool
	Limit         int
	Remaining     int
	Reset         time.Time
}

type requestObserverKey struct{}

// WithRequestObserver adds an operation-local observer. Existing observers are
// retained, including Options.Observe, so foreground and background accounting
// can share observations without sharing mutable HTTP response objects.
func WithRequestObserver(ctx context.Context, observe func(RequestObservation)) context.Context {
	if observe == nil {
		return ctx
	}
	previous, _ := ctx.Value(requestObserverKey{}).(func(RequestObservation))
	return context.WithValue(ctx, requestObserverKey{}, func(o RequestObservation) {
		if previous != nil {
			previous(o)
		}
		observe(o)
	})
}

func networkObservation(req *http.Request, resp *http.Response, endpoint string, page bool) RequestObservation {
	o := RequestObservation{Endpoint: endpoint, Page: page, Authenticated: req.Header.Get("Authorization") != ""}
	if resp == nil {
		return o
	}
	o.StatusCode = resp.StatusCode
	h := resp.Header
	limit, le := strconv.Atoi(h.Get("X-RateLimit-Limit"))
	remaining, re := strconv.Atoi(h.Get("X-RateLimit-Remaining"))
	reset, se := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64)
	if h.Get("X-RateLimit-Resource") == "core" && le == nil && re == nil && se == nil && limit > 0 && remaining >= 0 && remaining <= limit && reset > 0 {
		o.CoreRateValid = true
		o.Limit, o.Remaining, o.Reset = limit, remaining, time.Unix(reset, 0).UTC()
	}
	return o
}

// WithoutConditionalCache forces ordinary retrieval for a bounded discovery
// observation when the daemon has no fresh quota evidence.
func WithoutConditionalCache(ctx context.Context) context.Context {
	return context.WithValue(ctx, unconditionalCacheKey{}, true)
}

type unconditionalCacheKey struct{}
