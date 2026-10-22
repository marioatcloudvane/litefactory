package github_test

import (
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
)

func TestAnalysisBudgetCountsFailuresAndStopsBeforeNextRequest(t *testing.T) {
	f := newFixture(t, respondJSON(http.StatusForbidden, `{"message":"denied"}`))
	budget := &github.AnalysisBudget{MaxRequests: 1, MaxResponseBytes: 1024, MaxSourceBytes: 2048}
	ctx := github.WithAnalysisBudget(t.Context(), budget)
	if _, err := f.Client.GetIssue(ctx, testRepo, 1); !errors.Is(err, github.ErrForbidden) {
		t.Fatalf("first request: %v", err)
	}
	if _, err := f.Client.GetIssue(ctx, testRepo, 2); !errors.Is(err, github.ErrAnalysisLimit) {
		t.Fatalf("ceiling: %v", err)
	}
	if budget.Requests != 1 || budget.SourceBytes == 0 {
		t.Fatalf("budget: %+v", budget)
	}
	f.Only(t)
}

func TestAnalysisBudgetBoundsBytesBeforeJSONDecoding(t *testing.T) {
	f := newFixture(t, respondJSON(http.StatusOK, `{"id":1,"number":1,"body":"`+strings.Repeat("x", 2000)+`"}`))
	budget := &github.AnalysisBudget{MaxRequests: 2, MaxResponseBytes: 256, MaxSourceBytes: 512}
	if _, err := f.Client.GetIssue(github.WithAnalysisBudget(t.Context(), budget), testRepo, 1); !errors.Is(err, github.ErrAnalysisLimit) {
		t.Fatalf("oversized response accepted: %v", err)
	}
	if budget.SourceBytes != 257 {
		t.Fatalf("read %d bytes, want bounded probe", budget.SourceBytes)
	}
}

func TestAnalysisBudgetBoundsDecompressedGzip(t *testing.T) {
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		zip := gzip.NewWriter(w)
		if _, err := io.WriteString(zip, `{"id":1,"number":1,"body":"`+strings.Repeat("x", 8192)+`"}`); err != nil {
			t.Error(err)
		}
		if err := zip.Close(); err != nil {
			t.Error(err)
		}
	}))
	budget := &github.AnalysisBudget{MaxRequests: 1, MaxResponseBytes: 256, MaxSourceBytes: 512}
	if _, err := f.Client.GetIssue(github.WithAnalysisBudget(t.Context(), budget), testRepo, 1); !errors.Is(err, github.ErrAnalysisLimit) {
		t.Fatalf("decompressed limit: %v", err)
	}
	if budget.SourceBytes != 257 {
		t.Fatalf("decoded bytes = %d", budget.SourceBytes)
	}
}

func TestAnalysisBudgetPreservesEnterpriseAuthenticationRateLimit(t *testing.T) {
	f := newCustomFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/user" {
			respondJSON(http.StatusOK, userJSON)(w, r)
			return
		}
		w.Header().Set("Retry-After", "45")
		respondJSON(http.StatusTooManyRequests, `{"message":"secondary rate limit"}`)(w, r)
	}), func(options *github.Options) { options.APIBase += "/api/v3" })
	budget := &github.AnalysisBudget{APIBase: f.Server.URL + "/api/v3", Repository: testRepo.String(), MaxRequests: 3, MaxResponseBytes: 1024, MaxSourceBytes: 4096}
	ctx := github.WithAnalysisBudget(t.Context(), budget)
	if _, err := f.Client.AuthenticatedUser(ctx); err != nil {
		t.Fatalf("enterprise authentication: %v", err)
	}
	if _, err := f.Client.GetIssue(ctx, testRepo, 1); !errors.Is(err, github.ErrRateLimited) {
		t.Fatalf("source rate limit: %v", err)
	}
	if !budget.RateLimited || budget.RetryAfter != 45*time.Second || budget.Requests != 2 {
		t.Fatalf("lost auth/source rate metadata: %+v", budget)
	}
	if _, err := f.Client.GetIssue(ctx, testRepo, 1); !errors.Is(err, github.ErrRateLimited) {
		t.Fatalf("rate limit should remain terminal: %v", err)
	}
	if budget.Requests != 2 {
		t.Fatal("rate-limited invocation sent another request")
	}
}
