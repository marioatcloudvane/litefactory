package turn

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
)

// workspaceJudgeRunner observes what a provider could read while it runs, then
// completes through the real result broker.
type workspaceJudgeRunner struct {
	delegate                  recoveryJudgeRunner
	failure                   error
	cited                     string
	cwd, contextDir, prompt   string
	head, result, discussion  string
	citedBytes, readableError string
	readable                  []string
}

func (r *workspaceJudgeRunner) RunJudge(ctx context.Context, q agent.JudgeRequest) (agent.JudgeRunResult, error) {
	r.cwd, r.prompt, r.readable = q.Invocation.WorkingDirectory, q.Invocation.Prompt, q.Invocation.ReadableDirectories
	if len(r.readable) == 1 {
		r.contextDir = r.readable[0]
		result, err := os.ReadFile(filepath.Join(r.contextDir, judgeResultFile))
		discussion, discussionErr := os.ReadFile(filepath.Join(r.contextDir, judgeDiscussionFile))
		r.result, r.discussion = string(result), string(discussion)
		if err = errors.Join(err, discussionErr); err != nil {
			r.readableError = err.Error()
		}
	}
	if r.cited != "" {
		raw, err := os.ReadFile(filepath.Join(r.cwd, filepath.FromSlash(r.cited)))
		if err != nil {
			r.readableError = err.Error()
		}
		r.citedBytes = string(raw)
		out, err := exec.CommandContext(ctx, "git", "-C", r.cwd, "rev-parse", "HEAD").Output()
		if err != nil {
			r.readableError = err.Error()
		}
		r.head = strings.TrimSpace(string(out))
	}
	if r.failure != nil {
		return agent.JudgeRunResult{}, r.failure
	}
	return r.delegate.RunJudge(ctx, q)
}

func assertJudgeWorkspaceRemoved(t *testing.T, r *workspaceJudgeRunner) {
	t.Helper()
	for _, dir := range []string{r.cwd, r.contextDir, filepath.Dir(r.cwd)} {
		if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("judge workspace %s was retained: %v", dir, err)
		}
	}
}

func TestJudgeReadsPinnedCommitCheckoutAndContextFiles(t *testing.T) {
	service, request, _ := judgeRecoveryFixture(t)
	repo := gittest.New(t)
	const cited = "internal/deep/nested/pkg/cited.go"
	repo.WriteFile(cited, "package pkg\n\n// Committed bytes the review must see.\n")
	// Factory reports that sort before internal/ and exceed the old 64 MiB
	// snapshot budget once cost the judge every cited source file (#1006).
	block := strings.Repeat("report line that fills the factory report directory\n", (1<<20)/52)
	for i := 0; i < 66; i++ {
		repo.WriteFile(fmt.Sprintf(".aiteam/main/report-%02d.md", i), block)
	}
	commit := repo.Commit("pinned review commit")
	// Uncommitted edits after the pin are not what the review reads.
	repo.WriteFile(cited, "package pkg\n\n// Later uncommitted edit.\n")
	binary, err := git.Resolve(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	service.Git = binary
	request.Root = repo.Dir
	request.Options.Focus = "check the cited code"
	request.Comments = []JudgeComment{{ID: "41", URL: "https://example.test/issue#41", Body: "Owner decision: keep the factory reports."}}
	runner := &workspaceJudgeRunner{cited: cited}
	service.Providers[agent.Kind("codex")] = runner
	service.Providers[agent.Kind("claude")] = runner

	if _, err := service.Execute(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if runner.readableError != "" {
		t.Fatalf("judge could not read its files: %s", runner.readableError)
	}
	if runner.head != commit || !strings.Contains(runner.citedBytes, "Committed bytes the review must see.") {
		t.Fatalf("judge did not read the pinned commit: head %s want %s, cited %q", runner.head, commit, runner.citedBytes)
	}
	if !strings.Contains(runner.result, "Saved conclusion") {
		t.Fatalf("pinned result file lacks the saved result: %s", runner.result)
	}
	if !strings.Contains(runner.discussion, "Owner decision: keep the factory reports.") || !strings.Contains(runner.discussion, "https://example.test/issue#41") {
		t.Fatalf("discussion file lacks the captured comment: %s", runner.discussion)
	}
	if strings.HasPrefix(runner.contextDir, runner.cwd+string(filepath.Separator)) || runner.contextDir == runner.cwd {
		t.Fatalf("context files sit inside the repository checkout: %s in %s", runner.contextDir, runner.cwd)
	}
	for _, want := range []string{runner.cwd, commit, filepath.Join(runner.contextDir, judgeResultFile), filepath.Join(runner.contextDir, judgeDiscussionFile), request.Options.Focus} {
		if !strings.Contains(runner.prompt, want) {
			t.Fatalf("prompt does not tell the judge %q:\n%s", want, runner.prompt)
		}
	}
	assertJudgeWorkspaceRemoved(t, runner)
	if worktrees := repo.Git("worktree", "list", "--porcelain"); strings.Count(worktrees, "worktree ") != 1 {
		t.Fatalf("judge checkout stayed registered:\n%s", worktrees)
	}
	q, err := service.DB.FindJudgeRequest(t.Context(), request.TicketKey, request.RemoteCommentID)
	if err != nil {
		t.Fatal(err)
	}
	if report := decodeJudgeSavedReport(t, q.Report); report.Presentation.Commit != commit || report.Presentation.Cutoff == "" {
		t.Fatalf("saved report lacks the commit provenance: %s", q.Report)
	}
}

func TestJudgeWorkspaceRemovedAfterProviderFailure(t *testing.T) {
	service, request, _ := judgeRecoveryFixture(t)
	repo := gittest.New(t)
	repo.WriteFile("main.go", "package main\n")
	repo.Commit("fixture")
	binary, err := git.Resolve(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	service.Git, request.Root = binary, repo.Dir
	runner := &workspaceJudgeRunner{cited: "main.go", failure: errors.New("authorization refused")}
	service.Providers[agent.Kind("codex")] = runner
	service.Providers[agent.Kind("claude")] = runner
	body, err := service.Execute(t.Context(), request)
	if err != nil || !strings.Contains(body, "unable to assess") {
		t.Fatalf("failure report: %s %v", body, err)
	}
	if runner.citedBytes == "" {
		t.Fatalf("checkout was not readable during the run: %s", runner.readableError)
	}
	assertJudgeWorkspaceRemoved(t, runner)
	if worktrees := repo.Git("worktree", "list", "--porcelain"); strings.Count(worktrees, "worktree ") != 1 {
		t.Fatalf("judge checkout stayed registered:\n%s", worktrees)
	}
}

func TestJudgeWithoutRepositoryStillReadsContext(t *testing.T) {
	service, request, _ := judgeRecoveryFixture(t)
	runner := &workspaceJudgeRunner{}
	service.Providers[agent.Kind("codex")] = runner
	service.Providers[agent.Kind("claude")] = runner
	if _, err := service.Execute(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if runner.readableError != "" || !strings.Contains(runner.result, "Saved conclusion") {
		t.Fatalf("context unreadable without a repository: %s %q", runner.readableError, runner.result)
	}
	q, err := service.DB.FindJudgeRequest(t.Context(), request.TicketKey, request.RemoteCommentID)
	if err != nil {
		t.Fatal(err)
	}
	if report := decodeJudgeSavedReport(t, q.Report); report.Presentation.Commit != "" || report.Presentation.RepositoryGap == "" {
		t.Fatalf("missing repository was not recorded: %s", q.Report)
	}
	assertJudgeWorkspaceRemoved(t, runner)
}
