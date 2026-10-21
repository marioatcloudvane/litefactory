package prevention

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
)

func TestCollectorStopsAtRateLimitAndRetainsSanitizedReport(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/comments") {
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"message":"limited"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 1, "number": 1, "title": "bug", "body": "credential secret-value-123456", "html_url": "https://github.com/o/r/issues/1"})
	}))
	defer server.Close()
	client, err := github.New(github.Options{APIBase: server.URL, Token: secrets.NewToken("fixture-token")})
	if err != nil {
		t.Fatal(err)
	}
	collector := Collector{GitHub: client, Redactor: redact.New(redact.Options{Environ: []string{"API_TOKEN=secret-value-123456"}})}
	manifest, err := collector.Collect(t.Context(), Scope{Repository: "o/r", Issues: []int{1, 2}, Limits: DefaultLimits()})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 || manifest.Requests != 2 || !manifest.Frozen {
		t.Fatalf("requests=%d manifest=%+v", requests, manifest)
	}
	if len(manifest.Evidence) != 1 || strings.Contains(manifest.Evidence[0].Excerpt, "secret-value-123456") {
		t.Fatalf("unsafe or absent evidence: %+v", manifest.Evidence)
	}
	found := false
	for _, v := range manifest.Coverage {
		if v.Status == "limited" && strings.Contains(v.Reason, "no retry") {
			found = true
		}
	}
	if !found {
		t.Fatal("rate limit gap missing")
	}
}

func TestCollectorLimitsFixesToDirectSameRepositoryLinks(t *testing.T) {
	var paths []string
	sha := strings.Repeat("a", 40)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/o/r/issues/1":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 1, "number": 1, "body": "https://github.com/other/r/pull/99 https://github.com/o/r/pull/2 https://example.com/leak", "html_url": "https://github.com/o/r/issues/1"})
		case "/repos/o/r/issues/1/comments":
			_, _ = w.Write([]byte(`[]`))
		case "/repos/o/r/pulls/2":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 2, "number": 2, "body": "https://github.com/o/r/pull/3", "head": map[string]any{"sha": sha}})
		case "/repos/o/r/pulls/2/files":
			_, _ = w.Write([]byte(`[{"filename":"file.go","patch":"+ guarded"}]`))
		case "/repos/o/r/pulls/2/comments":
			_, _ = w.Write([]byte(`[]`))
		default:
			t.Errorf("unexpected source expansion: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client, err := github.New(github.Options{APIBase: server.URL, Token: secrets.NewToken("fixture-token")})
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := (Collector{GitHub: client, Redactor: redact.New(redact.Options{Environ: []string{}})}).Collect(t.Context(), Scope{Repository: "o/r", Issues: []int{1}, Limits: DefaultLimits()})
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 6 {
		t.Fatalf("requests = %v", paths)
	}
	found := false
	for _, gap := range manifest.Coverage {
		if gap.Status == "excluded" {
			found = true
		}
	}
	if !found {
		t.Fatal("excluded source coverage missing")
	}
}

func TestCollectorRetainsFixObservationGaps(t *testing.T) {
	headReads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/o/r/issues/1":
			_, _ = w.Write([]byte(`{"id":1,"number":1,"body":"https://github.com/o/r/pull/2","html_url":"https://github.com/o/r/issues/1"}`))
		case "/repos/o/r/issues/1/comments":
			_, _ = w.Write([]byte(`[]`))
		case "/repos/o/r/pulls/2":
			headReads++
			head := strings.Repeat("a", 40)
			if headReads > 1 {
				head = strings.Repeat("b", 40)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 2, "number": 2, "head": map[string]any{"sha": head}})
		case "/repos/o/r/pulls/2/files":
			w.Header().Set("Link", `<https://api.github.com/repos/o/r/pulls/2/files?page=2>; rel="next"`)
			_, _ = w.Write([]byte(`[{"filename":"binary.dat","status":"modified"}]`))
		case "/repos/o/r/pulls/2/comments":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"forbidden"}`))
		default:
			t.Errorf("unexpected source %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client, err := github.New(github.Options{APIBase: server.URL, Token: secrets.NewToken("fixture-token")})
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := (Collector{GitHub: client, Redactor: redact.New(redact.Options{Environ: []string{}})}).Collect(t.Context(), Scope{Repository: "o/r", Issues: []int{1}, Limits: DefaultLimits()})
	if err != nil {
		t.Fatal(err)
	}
	for _, reason := range []string{"PR patch unavailable", "additional PR file pages omitted", "PR head changed", "source unavailable"} {
		found := false
		for _, gap := range manifest.Coverage {
			if strings.Contains(gap.Reason, reason) {
				found = true
			}
		}
		if !found {
			t.Errorf("missing %q gap in %+v", reason, manifest.Coverage)
		}
	}
	foundPatch := false
	for _, evidence := range manifest.Evidence {
		if evidence.Kind == "patch" {
			foundPatch = true
			if evidence.Revision != strings.Repeat("a", 40) {
				t.Errorf("patch revision was silently updated: %+v", evidence)
			}
		}
	}
	if !foundPatch {
		t.Fatal("initial patch observation missing")
	}
}

func TestCollectorRetainsChangedCommentIdentityAndPaginationLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/repos/o/r/issues/1" {
			_, _ = w.Write([]byte(`{"id":1,"number":1,"body":"bug","html_url":"https://github.com/o/r/issues/1"}`))
			return
		}
		if r.URL.Path != "/repos/o/r/issues/1/comments" {
			t.Errorf("unexpected source %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Link", `<https://api.github.com/repos/o/r/issues/1/comments?page=3>; rel="next"`)
		if r.URL.Query().Get("page") == "1" {
			w.Header().Set("Link", `<https://api.github.com/repos/o/r/issues/1/comments?page=2>; rel="next"`)
			_, _ = w.Write([]byte(`[{"id":7,"body":"initial cause"}]`))
		} else {
			_, _ = w.Write([]byte(`[{"id":7,"body":"initial cause"},{"id":7,"body":"corrected cause"}]`))
		}
	}))
	defer server.Close()
	client, err := github.New(github.Options{APIBase: server.URL, Token: secrets.NewToken("fixture-token")})
	if err != nil {
		t.Fatal(err)
	}
	limits := DefaultLimits()
	limits.Requests = 3
	manifest, err := (Collector{GitHub: client, Redactor: redact.New(redact.Options{Environ: []string{}})}).Collect(t.Context(), Scope{Repository: "o/r", Issues: []int{1}, Limits: limits})
	if err != nil {
		t.Fatal(err)
	}
	var comments []Evidence
	for _, e := range manifest.Evidence {
		if e.Kind == "comment" {
			comments = append(comments, e)
		}
	}
	if len(comments) != 2 || comments[0].Locator != comments[1].Locator || comments[0].Digest == comments[1].Digest {
		t.Fatalf("changed observations were lost or duplicate retained: %+v", comments)
	}
	limited := false
	for _, gap := range manifest.Coverage {
		if gap.Source == "discussion" && gap.Status == "limited" {
			limited = true
		}
	}
	if !limited || manifest.Requests != 3 {
		t.Fatalf("missing pagination ceiling: %+v", manifest)
	}
}

type localEvidenceFunc func(context.Context, string, int, int) ([]LocalObservation, error)

func (f localEvidenceFunc) ReadBugEvidence(ctx context.Context, repo string, issue, limit int) ([]LocalObservation, error) {
	return f(ctx, repo, issue, limit)
}

func TestCollectorRetainsLocalPrefixAndChargesSharedSources(t *testing.T) {
	budget := &github.AnalysisBudget{Repository: "o/r", MaxRequests: 30, MaxResponseBytes: 1024, MaxSourceBytes: 32}
	if err := budget.ChargeSource(24); err != nil {
		t.Fatal(err)
	}
	local := localEvidenceFunc(func(_ context.Context, _ string, _ int, limit int) ([]LocalObservation, error) {
		if limit != 8 {
			t.Errorf("local allowance %d, want shared remaining 8", limit)
		}
		return []LocalObservation{{Kind: "repair", Locator: "repair:1", Body: "observed"}}, errors.New("additional local observation omitted")
	})
	manifest, err := (Collector{Budget: budget, Local: local, Redactor: redact.New(redact.Options{Environ: []string{}})}).Collect(t.Context(), Scope{Repository: "o/r", Issues: []int{1}, Limits: DefaultLimits()})
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Evidence) != 1 || manifest.Evidence[0].Kind != "repair" || manifest.SourceBytes != 32 {
		t.Fatalf("local prefix or accounting lost: %+v", manifest)
	}
	found := false
	for _, gap := range manifest.Coverage {
		if gap.Source == "local_bug" && strings.Contains(gap.Reason, "retained available prefix") {
			found = true
		}
	}
	if !found {
		t.Fatal("partial local error gap missing")
	}
}

func TestCollectorSanitizesExcludedFixMetadata(t *testing.T) {
	const credential = "registered-secret-123456"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/comments") {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 1, "number": 1, "body": "https://github.com/o/r/commit/" + credential, "html_url": "https://github.com/o/r/issues/1"})
	}))
	defer server.Close()
	client, err := github.New(github.Options{APIBase: server.URL, Token: secrets.NewToken("fixture-token")})
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := (Collector{GitHub: client, Redactor: redact.New(redact.Options{Environ: []string{"API_TOKEN=" + credential}})}).Collect(t.Context(), Scope{Repository: "o/r", Issues: []int{1}, Limits: DefaultLimits()})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), credential) {
		t.Fatal("credential leaked through fix-reference coverage metadata")
	}
}

func TestCollectorBoundsAmplifiedCoverageMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/comments") {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"number": 1, "title": "Bug", "body": strings.Repeat("https://github.com/elsewhere/repo/pull/1 ", 2000)})
	}))
	defer server.Close()
	client, err := github.New(github.Options{APIBase: server.URL, Token: secrets.NewToken("fixture-token")})
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := (Collector{GitHub: client, Redactor: redact.New(redact.Options{Environ: []string{}})}).Collect(t.Context(), Scope{Repository: "o/r", Issues: []int{1}, Limits: DefaultLimits()})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !manifest.Frozen || len(manifest.Evidence) == 0 || manifest.CoverageOmitted == 0 || len(manifest.Coverage) > manifest.Policy.CoverageEntries || len(raw) > 512<<10 {
		t.Fatalf("amplified metadata lost bounded evidence: bytes=%d omitted=%d coverage=%d", len(raw), manifest.CoverageOmitted, len(manifest.Coverage))
	}
}
