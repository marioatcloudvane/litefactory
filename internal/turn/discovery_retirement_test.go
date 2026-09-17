package turn

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/secrets"
)

// A Markdown story saved by a working run is an artifact, not publication
// authority. Use the same concrete forge/renderer types as daemon setup and
// observe their HTTP boundary while executing the real git/SQLite save path.
func TestWorkingRunSavesMarkdownWithoutDiscoveringStories(t *testing.T) {
	t.Parallel()
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, "unexpected publication", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	forge, err := github.New(github.Options{APIBase: server.URL, HTTPClient: server.Client(), Token: secrets.NewToken("fixture")})
	mustProgress(t, err)
	var _ PlanForge = forge
	renderer, err := report.New(report.Options{Redactor: redact.New(redact.Options{Environ: []string{}}), Limit: github.CommentSizeLimit})
	mustProgress(t, err)
	dir, err := naming.AITeamSessionDir(testBranch, time.Now().UTC(), testSlug)
	mustProgress(t, err)
	const brief = "# Plan\n\n## Story Index\n- [C1: retained artifact](C1.md)\n"
	const story = "# C1: retained artifact\n\n## User Story\nKeep this document.\n\n## Acceptance Criteria\n- File survives save.\n"
	var f *fixture
	runner := newStubRunner(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		mustProgress(t, os.MkdirAll(filepath.Join(req.WorktreePath, dir), 0o700))
		writeInWorktree(t, req, dir+"/plan_brief.md", brief)
		writeInWorktree(t, req, dir+"/C1.md", story)
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)
	f.opts.SubIssueCreator, f.opts.SubIssueRenderer = forge, renderer
	result := f.execute(f.request())
	if !result.Succeeded() || !result.Committed || !result.Pushed {
		t.Fatalf("save failed: %+v\n%s", result, f.logs)
	}
	if calls.Load() != 0 {
		t.Fatalf("Markdown triggered %d forge calls", calls.Load())
	}
	if got := originFile(t, f, testBranch, dir+"/plan_brief.md"); got != brief[:len(brief)-1] {
		t.Fatalf("saved brief changed: %q", got)
	}
	if got := originFile(t, f, testBranch, dir+"/C1.md"); got != story[:len(story)-1] {
		t.Fatalf("saved story changed: %q", got)
	}
	if got := commitCount(t, f, testBranch); got != 2 {
		t.Fatalf("got %d commits, want base plus one save", got)
	}
}
