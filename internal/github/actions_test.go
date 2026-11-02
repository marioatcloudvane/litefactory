package github_test

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
)

func TestActionsReadIdentityPagesAndRerun(t *testing.T) {
	t.Parallel()
	const run = `{"id":91,"workflow_id":42,"run_number":18,"run_attempt":3,"head_sha":"abc","head_branch":"main","status":"completed","conclusion":"success","html_url":"https://github.com/acme/acme-api/actions/runs/91","created_at":"2026-11-01T12:00:00Z","updated_at":"2026-11-01T13:00:00Z"}`
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/acme-api":
			fmt.Fprint(w, `{"id":7,"full_name":"acme/acme-api"}`)
		case "/repos/acme/acme-api/actions/workflows/check.yml":
			fmt.Fprint(w, `{"id":42,"path":".github/workflows/check.yml","name":"Check","state":"active"}`)
		case "/repos/acme/acme-api/actions/workflows/42/runs":
			if r.URL.Query().Get("branch") != "main" || r.URL.Query().Get("per_page") != "100" || r.URL.Query().Get("created") == "" || r.URL.Query().Get("status") != "" {
				t.Errorf("query=%s", r.URL.RawQuery)
			}
			w.Header().Set("Link", `<https://untrusted.example/runs?page=2>; rel="next"`)
			fmt.Fprintf(w, `{"total_count":101,"workflow_runs":[%s]}`, run)
		case "/repos/acme/acme-api/actions/runs/91":
			fmt.Fprint(w, run)
		default:
			t.Errorf("unexpected path=%s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	repo, _, err := f.Client.InspectActionsRepository(t.Context(), testRepo)
	if err != nil || repo.ID != 7 {
		t.Fatalf("repo=%+v err=%v", repo, err)
	}
	workflow, _, err := f.Client.InspectActionsWorkflow(t.Context(), testRepo, ".github/workflows/check.yml")
	if err != nil || workflow.ID != 42 || workflow.State != "active" {
		t.Fatalf("workflow=%+v err=%v", workflow, err)
	}
	page, _, err := f.Client.ListActionsRuns(t.Context(), testRepo, 42, "main", "2026-11-01..2026-11-02", 1)
	if err != nil || page.Total != 101 || page.NextPage != 2 || len(page.Runs) != 1 {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	got, _, err := f.Client.GetActionsRun(t.Context(), testRepo, 91)
	if err != nil || got.Attempt != 3 || got.RunNumber != 18 {
		t.Fatalf("run=%+v err=%v", got, err)
	}
	for _, req := range f.Requests() {
		if req.Method != http.MethodGet {
			t.Errorf("method=%s", req.Method)
		}
	}
}

func TestActionsTimingPreservesFullServerBoundsAndClassifiedError(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	f := newCustomFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7200")
		w.Header().Set("X-Poll-Interval", "10800")
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", fmt.Sprint(now.Add(4*time.Hour).Unix()))
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"message":"rate limited"}`)
	}, func(o *github.Options) { o.Now = func() time.Time { return now } })
	_, timing, err := f.Client.InspectActionsRepository(t.Context(), testRepo)
	if !errors.Is(err, github.ErrRateLimited) || !timing.RateLimited || !timing.RetryAfter.Equal(now.Add(2*time.Hour)) || !timing.RateLimitReset.Equal(now.Add(4*time.Hour)) || timing.PollInterval != 3*time.Hour {
		t.Fatalf("timing=%+v err=%v", timing, err)
	}
}

func TestActionsMalformedEvidenceCannotBecomeEmptyHistory(t *testing.T) {
	t.Parallel()
	for _, body := range []string{`{}`, `{"total_count":0}`, `{"total_count":1,"workflow_runs":[null]}`, `{"total_count":0,"workflow_runs":[]}`} {
		t.Run(body, func(t *testing.T) {
			f := newFixture(t, respondJSON(200, body))
			page, _, err := f.Client.ListActionsRuns(t.Context(), testRepo, 42, "main", "", 1)
			if body == `{"total_count":0,"workflow_runs":[]}` {
				if err != nil || page.Total != 0 {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("malformed result accepted")
			}
		})
	}
}

func TestActionsRetryAfterDateAndDisabledMetadata(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	f := newCustomFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", now.Add(5*time.Hour).Format(http.TimeFormat))
		fmt.Fprint(w, `{"id":42,"path":".github/workflows/check.yml","state":"disabled_manually"}`)
	}, func(o *github.Options) { o.Now = func() time.Time { return now } })
	workflow, timing, err := f.Client.InspectActionsWorkflow(t.Context(), testRepo, ".github/workflows/check.yml")
	if err != nil || workflow.State != "disabled_manually" || !timing.RetryAfter.Equal(now.Add(5*time.Hour)) {
		t.Fatalf("workflow=%+v timing=%+v err=%v", workflow, timing, err)
	}
}
