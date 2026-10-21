package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ErrAnalysisLimit reports a source collection ceiling, including failed calls.
var ErrAnalysisLimit = errors.New("analysis source limit reached")

type analysisBudgetKey struct{}

// AnalysisBudget bounds actual HTTP requests and decoded response bytes. A
// budget belongs to one invocation and is never reset by a reader.
type AnalysisBudget struct {
	Repository       string
	APIBase          string
	RateLimited      bool
	RetryAfter       time.Duration
	RetryReset       time.Time
	origin           string
	mu               sync.Mutex
	MaxRequests      int
	MaxResponseBytes int64
	MaxSourceBytes   int64
	Requests         int
	SourceBytes      int64
}

// WithAnalysisBudget disables conditional cache reuse for source observations
// and applies a shared source budget before SDK decoding.
func WithAnalysisBudget(ctx context.Context, budget *AnalysisBudget) context.Context {
	return context.WithValue(context.WithValue(ctx, unconditionalCacheKey{}, true), analysisBudgetKey{}, budget)
}

func (b *AnalysisBudget) read(next http.RoundTripper, req *http.Request) (*http.Response, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	prefix := ""
	if b.APIBase != "" {
		base, err := url.Parse(b.APIBase)
		if err != nil || base.Host == "" {
			return nil, ErrAnalysisLimit
		}
		b.origin = base.Scheme + "://" + base.Host
		prefix = strings.TrimRight(base.Path, "/")
	}
	if b.origin == "" {
		b.origin = req.URL.Scheme + "://" + req.URL.Host
	}
	repoPath := prefix + "/repos/" + strings.ToLower(b.Repository) + "/"
	if b.origin != req.URL.Scheme+"://"+req.URL.Host || req.Method != http.MethodGet || (b.Repository != "" && req.URL.Path != prefix+"/user" && !strings.HasPrefix(strings.ToLower(req.URL.Path), repoPath)) {
		return nil, ErrAnalysisLimit
	}
	if b.RateLimited {
		return nil, ErrRateLimited
	}
	if b.MaxRequests < 1 || b.MaxResponseBytes < 1 || b.MaxSourceBytes < 1 || b.Requests >= b.MaxRequests || b.SourceBytes >= b.MaxSourceBytes {
		return nil, ErrAnalysisLimit
	}
	b.Requests++
	resp, err := next.RoundTrip(req)
	if err != nil || resp == nil || resp.Body == nil {
		return resp, err
	}
	if isRateLimit(resp, apiFailure{}) {
		b.RateLimited = true
		b.RetryAfter, b.RetryReset = retryDelay(resp.Header, time.Now())
	}
	remaining := min(b.MaxResponseBytes, b.MaxSourceBytes-b.SourceBytes)
	// net/http has already decompressed gzip before this transport reads it.
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, remaining+1))
	closeErr := resp.Body.Close()
	b.SourceBytes += int64(len(body))
	if int64(len(body)) > remaining {
		return nil, ErrAnalysisLimit
	}
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	var failure struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &failure) == nil && isRateLimit(resp, apiFailure{message: failure.Message}) {
		b.RateLimited = true
		b.RetryAfter, b.RetryReset = retryDelay(resp.Header, time.Now())
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	return resp, nil
}

// RemainingSourceBytes returns the unspent shared remote and local allowance.
func (b *AnalysisBudget) RemainingSourceBytes() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return max(0, b.MaxSourceBytes-b.SourceBytes)
}

// ChargeSource accounts decoded local evidence against the same source ceiling.
func (b *AnalysisBudget) ChargeSource(bytes int64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if bytes < 0 || bytes > b.MaxSourceBytes-b.SourceBytes {
		return ErrAnalysisLimit
	}
	b.SourceBytes += bytes
	return nil
}
