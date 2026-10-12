package linear

import (
	"context"
	"net/http"
	"strconv"
	"time"
)

// RequestObservation describes one HTTP attempt without retaining response
// bodies, headers, URLs or credentials. Request quota and refusal advice have
// different units and must be consumed independently.
type RequestObservation struct {
	StatusCode       int
	RequestRateValid bool
	Limit, Remaining int
	Reset            time.Time
	RetryAfter       time.Duration
}

type requestObserverKey struct{}

// WithRequestObserver adds a synchronous, operation-local observer. Callbacks
// must be safe for concurrent callers; existing observers are preserved.
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

func requestObservation(response *http.Response) RequestObservation {
	if response == nil {
		return RequestObservation{}
	}
	o := RequestObservation{StatusCode: response.StatusCode}
	h := response.Header
	limit, le := strconv.Atoi(h.Get("X-RateLimit-Requests-Limit"))
	remaining, re := strconv.Atoi(h.Get("X-RateLimit-Requests-Remaining"))
	reset, se := strconv.ParseInt(h.Get("X-RateLimit-Requests-Reset"), 10, 64)
	if le == nil && re == nil && se == nil && limit > 0 && remaining >= 0 && remaining <= limit && reset > 0 {
		o.RequestRateValid = true
		o.Limit, o.Remaining, o.Reset = limit, remaining, time.UnixMilli(reset).UTC()
	}
	return o
}
