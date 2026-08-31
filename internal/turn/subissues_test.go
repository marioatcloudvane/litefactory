package turn

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

// Tests for the sub-issue phase (subissues.go), against fakes throughout —
// nothing here reaches a network. AC-W1..AC-W10 refer to
// ARCHITECT_SUBISSUE_WIRING.md's acceptance criteria for this phase.
//
// The renderer under test is a real *report.Reporter wrapped in a spy, not a
// hand-rolled fake: redact.Body's fields are unexported and only constructible
// through a real [redact.Redactor.Apply], so building one any other way would
// mean reaching into internal/redact's internals from this package. The real
// Reporter does no I/O of its own — it is a pure renderer — so using it here
// is not a network dependency, only a correctness one, and it is already
// covered by internal/report's own tests.
//
// The forge half, [fakeSubIssueCreator], is a genuine in-memory fake: it is
// the boundary that must never touch a real GitHub, and per the C1 pre-flight's
// own warning it deliberately hands out an issue ID that differs from its
// Number (9001, 9002, ... versus 41, 42, ...), so a caller that swapped the
// two — the exact AC-C1.1a trap — would produce a visibly wrong URL rather
// than passing by coincidence.

// fakeSubIssueCreator is an in-memory model of [github.Client.CreateSubIssues]:
// idempotent by marker, stops at the first configured failure, and records
// every batch it was handed.
type fakeSubIssueCreator struct {
	mu sync.Mutex

	// byMarker is the parent's existing sub-issues, exactly what
	// ListSubIssues would answer — carried across calls, which is what makes
	// a second call idempotent the same way the real client's is.
	byMarker map[string]github.SubIssueResult

	// failStory, when non-empty, makes the spec with this story id come back
	// SubIssueFailed and stops the batch there, as CreateSubIssues documents
	// it does.
	failStory string

	// batchErr, when set, is returned as CreateSubIssues's error — the
	// "ListSubIssues itself failed" path, which never returns a result slice
	// at all.
	batchErr error

	nextID  int64
	nextNum int

	calls     int
	writes    int
	repoSeen  github.Repo
	parent    int
	specsSeen [][]github.SubIssueSpec
}

func newFakeSubIssueCreator() *fakeSubIssueCreator {
	return &fakeSubIssueCreator{
		byMarker: make(map[string]github.SubIssueResult),
		nextID:   9001,
		nextNum:  41,
	}
}

func (f *fakeSubIssueCreator) CreateSubIssues(
	_ context.Context, repo github.Repo, parent int, specs []github.SubIssueSpec,
) ([]github.SubIssueResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++
	f.repoSeen = repo
	f.parent = parent
	f.specsSeen = append(f.specsSeen, specs)

	if f.batchErr != nil {
		return nil, f.batchErr
	}

	results := make([]github.SubIssueResult, len(specs))
	stopped := false
	for i, spec := range specs {
		if stopped {
			results[i] = github.SubIssueResult{StoryID: spec.StoryID, Outcome: github.SubIssueNotAttempted}
			continue
		}
		if existing, ok := f.byMarker[spec.Marker]; ok {
			results[i] = github.SubIssueResult{
				StoryID: spec.StoryID,
				Outcome: github.SubIssueExisted,
				Number:  existing.Number,
				ID:      existing.ID,
				HTMLURL: existing.HTMLURL,
			}
			continue
		}
		if spec.StoryID == f.failStory {
			results[i] = github.SubIssueResult{
				StoryID: spec.StoryID,
				Outcome: github.SubIssueFailed,
				Err:     errors.New("fakeSubIssueCreator: simulated failure"),
			}
			stopped = true
			continue
		}

		f.writes++
		id, number := f.nextID, f.nextNum
		f.nextID++
		f.nextNum++
		res := github.SubIssueResult{
			StoryID: spec.StoryID,
			Outcome: github.SubIssueCreated,
			Number:  number,
			ID:      id,
			HTMLURL: fmt.Sprintf("https://github.com/%s/issues/%d", repo, number),
		}
		f.byMarker[spec.Marker] = res
		results[i] = res
	}
	return results, nil
}

func (f *fakeSubIssueCreator) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeSubIssueCreator) writeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.writes
}

func (f *fakeSubIssueCreator) lastSpecs() []github.SubIssueSpec {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.specsSeen) == 0 {
		return nil
	}
	return f.specsSeen[len(f.specsSeen)-1]
}

// spyRenderer wraps a real *report.Reporter and records every [report.Story]
// it was asked to render, so a test can inspect what this phase built without
// reimplementing redact.Body construction.
type spyRenderer struct {
	rep *report.Reporter

	mu      sync.Mutex
	stories []report.Story
}

func newSpyRenderer(t *testing.T) *spyRenderer {
	t.Helper()
	rep, err := report.New(report.Options{
		Redactor: redact.New(redact.Options{Environ: []string{}}),
		Limit:    github.CommentSizeLimit,
	})
	if err != nil {
		t.Fatalf("report.New: %v", err)
	}
	return &spyRenderer{rep: rep}
}

func (s *spyRenderer) RenderSubIssue(ctx context.Context, story report.Story) (report.SubIssue, error) {
	s.mu.Lock()
	s.stories = append(s.stories, story)
	s.mu.Unlock()
	return s.rep.RenderSubIssue(ctx, story)
}

func (s *spyRenderer) storiesSeen() []report.Story {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]report.Story(nil), s.stories...)
}

// testSessionDir is the aiteam session directory every test in this file
// writes plan_brief.md and its per-story files under: the direct guess
// [locateSessionDir] tries first, for the fixture's own branch and slug, on
// the calendar day the fixture's runs are actually allocated on (UTC, same
// clock store.AllocateRun stamps a run's StartedAt with).
func testSessionDir(t *testing.T) string {
	t.Helper()
	dir, err := naming.AITeamSessionDir(testBranch, time.Now().UTC(), testSlug)
	if err != nil {
		t.Fatalf("derive the test session directory: %v", err)
	}
	return dir
}

// planBrief renders a minimal but contract-shaped plan_brief.md carrying one
// Story Index row per id given.
func planBrief(ids ...string) string {
	var b strings.Builder
	b.WriteString("# Plan Brief — test\n\n## Story Index\n")
	b.WriteString("| ID | Title | Size | Depends On | Sub-issue |\n")
	b.WriteString("|----|-------|------|-----------|-----------|\n")
	for _, id := range ids {
		fmt.Fprintf(&b, "| %s | Story %s | M | — |  |\n", id, id)
	}
	b.WriteString("\n## Out of Scope\n- none\n")
	return b.String()
}

// storyFile renders a minimal but contract-shaped per-story sub-issue body.
func storyFile(id string) string {
	return "# " + id + " — Story " + id + "\n\n" +
		"> As a user, I want this story so that it ships.\n\n" +
		"One sentence describing what this story delivers.\n\n" +
		"## Why This Matters\nThis story matters because the tests say so.\n\n" +
		"## Architecture Notes\n- A key decision.\n\n" +
		"## Tasks\n| ID | Title | Type | Depends On | Est |\n|----|-------|------|-----------|-----|\n" +
		"| T-" + id + ".1 | Do the thing | Go | — | S |\n\n" +
		"## Acceptance Criteria\n| ID | Criterion |\n|----|-----------|\n" +
		"| AC-" + id + ".1 | It works |\n"
}

// writeSessionFile writes content at dir/relPath inside the run's worktree,
// creating whatever parent directories it needs — unlike
// [writeInWorktree], which assumes its parent already exists.
func writeSessionFile(t *testing.T, req harness.Request, dir, name, content string) {
	t.Helper()
	full := filepath.Join(req.WorktreePath, filepath.FromSlash(dir), name)
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}

// commitCount returns how many commits branch carries on the fixture's
// origin — the plain way to tell "one commit landed" from "two did" without
// caring about either one's sha.
func commitCount(t *testing.T, f *fixture, branch string) int {
	t.Helper()
	out := f.repo.GitIn(f.repo.Origin, "rev-list", "--count", "refs/heads/"+branch)
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		t.Fatalf("parse commit count %q: %v", out, err)
	}
	return n
}

// originFile reads path as it stands on branch, on the fixture's origin —
// what a reader who only has the pushed remote would see.
func originFile(t *testing.T, f *fixture, branch, path string) string {
	t.Helper()
	return f.repo.GitIn(f.repo.Origin, "show", branch+":"+path)
}

// req2 builds a second turn's request on the same session: a different
// trigger comment (so it is not a replay of the first) that asks for
// something unrelated to the plan, which is the shape AC-W2 needs — a later
// turn that does not itself touch plan_brief.md at all.
func req2(f *fixture, body string) Request {
	req := f.request()
	req.Trigger.CommentID = testComment + 1
	req.Trigger.Body = body
	req.Command = ParseCommand(body)
	return req
}

// TestFileStoryIssuesFilesTheMissingSubIssue is AC-W1: a pushed commit whose
// worktree carries an unresolved Story Index row and a matching per-story
// file gets its sub-issue created and its row rewritten with a working link,
// with no comment, flag or pointer file telling this phase to look.
func TestFileStoryIssuesFilesTheMissingSubIssue(t *testing.T) {
	t.Parallel()

	creator := newFakeSubIssueCreator()
	renderer := newSpyRenderer(t)
	dir := testSessionDir(t)

	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		writeSessionFile(t, req, dir, "plan_brief.md", planBrief("C1"))
		writeSessionFile(t, req, dir, "C1.md", storyFile("C1"))
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)
	f.opts.SubIssueCreator = creator
	f.opts.SubIssueRenderer = renderer

	result := f.execute(f.request())
	if !result.Succeeded() {
		t.Fatalf("turn did not succeed: %+v\nlogs:\n%s", result, f.logs)
	}

	if got := creator.callCount(); got != 1 {
		t.Errorf("CreateSubIssues called %d times, want 1", got)
	}
	if got := creator.writeCount(); got != 1 {
		t.Errorf("filed %d issues, want 1", got)
	}
	if got := commitCount(t, f, testBranch); got != 3 {
		t.Errorf("branch carries %d commits, want 3 (the base commit, claude's, then the write-back)", got)
	}

	content := originFile(t, f, testBranch, dir+"/plan_brief.md")
	wantURL := "https://github.com/acme/acme-api/issues/41"
	if !strings.Contains(content, wantURL) {
		t.Errorf("plan_brief.md on the remote does not carry %s:\n%s", wantURL, content)
	}
	if strings.Count(content, wantURL) != 1 {
		t.Errorf("plan_brief.md carries %s more than once:\n%s", wantURL, content)
	}
}

// TestFileStoryIssuesResumesOnALaterUnrelatedTurn is AC-W2: when the phase
// fails on the run that wrote plan_brief.md, a later turn on the same task —
// of any kind, touching nothing in plan_brief.md itself, even a turn that
// changes nothing at all — still finishes the outstanding sub-issue creation.
// This is what proves the gate is artifact presence, not this run's own diff.
func TestFileStoryIssuesResumesOnALaterUnrelatedTurn(t *testing.T) {
	t.Parallel()

	creator := newFakeSubIssueCreator()
	creator.batchErr = errors.New("simulated: the forge is unreachable")
	renderer := newSpyRenderer(t)
	dir := testSessionDir(t)

	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		if req.RunOrdinal == 1 {
			writeSessionFile(t, req, dir, "plan_brief.md", planBrief("C1"))
			writeSessionFile(t, req, dir, "C1.md", storyFile("C1"))
		}
		// Run 2 changes nothing in the worktree at all — proving this is not
		// gated on the second run's own diff.
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)
	f.opts.SubIssueCreator = creator
	f.opts.SubIssueRenderer = renderer

	first := f.execute(f.request())
	if !first.Succeeded() {
		t.Fatalf("first turn did not succeed: %+v\nlogs:\n%s", first, f.logs)
	}
	if got := commitCount(t, f, testBranch); got != 2 {
		t.Fatalf("branch carries %d commits after the failed batch, want 2 (the base commit and claude's, no write-back)", got)
	}

	// Fix the forge and run an unrelated second turn.
	creator.batchErr = nil
	second := f.execute(req2(f, "/spike say hello"))
	if !second.Succeeded() {
		t.Fatalf("second turn did not succeed: %+v\nlogs:\n%s", second, f.logs)
	}
	if second.Changed {
		t.Fatalf("the second turn's own stub wrote nothing; Changed should be false, got true")
	}

	if got := commitCount(t, f, testBranch); got != 3 {
		t.Errorf("branch carries %d commits after the second turn, want 3", got)
	}
	content := originFile(t, f, testBranch, dir+"/plan_brief.md")
	if !strings.Contains(content, "https://github.com/acme/acme-api/issues/41") {
		t.Errorf("plan_brief.md was not resolved by the second turn:\n%s", content)
	}
}

// TestFileStoryIssuesRerunIsANoOp is AC-W3: once every row is already
// resolved, a later turn's phase makes no create/attach call and produces no
// second commit — [github.SubIssueExisted] is the only outcome, and
// Worktree.Commit's "no diff, no commit" behaviour does the rest for free.
func TestFileStoryIssuesRerunIsANoOp(t *testing.T) {
	t.Parallel()

	creator := newFakeSubIssueCreator()
	renderer := newSpyRenderer(t)
	dir := testSessionDir(t)

	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		if req.RunOrdinal == 1 {
			writeSessionFile(t, req, dir, "plan_brief.md", planBrief("C1"))
			writeSessionFile(t, req, dir, "C1.md", storyFile("C1"))
		}
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)
	f.opts.SubIssueCreator = creator
	f.opts.SubIssueRenderer = renderer

	f.execute(f.request())
	resolvedCommits := commitCount(t, f, testBranch)
	if resolvedCommits != 3 {
		t.Fatalf("branch carries %d commits after the first turn, want 3", resolvedCommits)
	}
	if got := creator.writeCount(); got != 1 {
		t.Fatalf("wrote %d issues after the first turn, want 1", got)
	}

	f.execute(req2(f, "/spike say hello again"))

	if got := creator.callCount(); got != 2 {
		t.Errorf("CreateSubIssues called %d times across two turns, want 2", got)
	}
	if got := creator.writeCount(); got != 1 {
		t.Errorf("wrote %d issues across two turns, want 1 (the second is a re-run)", got)
	}
	if got := commitCount(t, f, testBranch); got != resolvedCommits {
		t.Errorf("branch carries %d commits after the re-run, want %d (no second write-back)", got, resolvedCommits)
	}
}

// TestFileStoryIssuesOneMissingStoryFileDoesNotBlockSiblings is AC-W4: one
// story's per-story file being missing does not prevent its siblings, in the
// same plan_brief.md, from being filed in the same turn.
func TestFileStoryIssuesOneMissingStoryFileDoesNotBlockSiblings(t *testing.T) {
	t.Parallel()

	creator := newFakeSubIssueCreator()
	renderer := newSpyRenderer(t)
	dir := testSessionDir(t)

	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		writeSessionFile(t, req, dir, "plan_brief.md", planBrief("C1", "C2"))
		writeSessionFile(t, req, dir, "C1.md", storyFile("C1"))
		// C2.md is deliberately never written.
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)
	f.opts.SubIssueCreator = creator
	f.opts.SubIssueRenderer = renderer

	result := f.execute(f.request())
	if !result.Succeeded() {
		t.Fatalf("turn did not succeed: %+v\nlogs:\n%s", result, f.logs)
	}

	specs := creator.lastSpecs()
	if len(specs) != 1 || specs[0].StoryID != "C1" {
		t.Fatalf("batch = %+v, want exactly one spec for C1", specs)
	}

	content := originFile(t, f, testBranch, dir+"/plan_brief.md")
	if !strings.Contains(content, "https://github.com/acme/acme-api/issues/41") {
		t.Errorf("C1's row was not resolved:\n%s", content)
	}
	if strings.Contains(content, "github.com/acme/acme-api/issues") &&
		strings.Count(content, "github.com/acme/acme-api/issues") != 1 {
		t.Errorf("C2's row should carry no link, only C1's should:\n%s", content)
	}
}

// TestFileStoryIssuesDetailLinkIsPinnedToAPushedSHA is AC-W5: the sha every
// rendered sub-issue's Detail link is pinned to is the commit this run's own
// push already confirmed reached the remote — never an unpushed or guessed
// one.
func TestFileStoryIssuesDetailLinkIsPinnedToAPushedSHA(t *testing.T) {
	t.Parallel()

	creator := newFakeSubIssueCreator()
	renderer := newSpyRenderer(t)
	dir := testSessionDir(t)

	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		writeSessionFile(t, req, dir, "plan_brief.md", planBrief("C1"))
		writeSessionFile(t, req, dir, "C1.md", storyFile("C1"))
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)
	f.opts.SubIssueCreator = creator
	f.opts.SubIssueRenderer = renderer

	result := f.execute(f.request())
	if !result.Succeeded() || result.SHAAfter == "" {
		t.Fatalf("turn did not succeed with a sha_after: %+v\nlogs:\n%s", result, f.logs)
	}

	stories := renderer.storiesSeen()
	if len(stories) != 1 {
		t.Fatalf("rendered %d stories, want 1", len(stories))
	}
	story := stories[0]
	if story.DetailSHA != result.SHAAfter {
		t.Errorf("story.DetailSHA = %q, want the run's own confirmed sha_after %q", story.DetailSHA, result.SHAAfter)
	}
	// It must actually be on the remote — cat-file -e fails the test if the
	// object cannot be found there.
	f.repo.GitIn(f.repo.Origin, "cat-file", "-e", story.DetailSHA)

	if story.Detail != dir+"/C1.md" {
		t.Errorf("story.Detail = %q, want %q", story.Detail, dir+"/C1.md")
	}
}

// TestFileStoryIssuesLeavesTheRunsOwnReportUntouched is AC-W6: the phase's
// own, second commit is never reflected in the fields the run's own report
// reads — the worktree ends up ahead of what result says, on purpose.
func TestFileStoryIssuesLeavesTheRunsOwnReportUntouched(t *testing.T) {
	t.Parallel()

	creator := newFakeSubIssueCreator()
	renderer := newSpyRenderer(t)
	dir := testSessionDir(t)

	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		writeSessionFile(t, req, dir, "plan_brief.md", planBrief("C1"))
		writeSessionFile(t, req, dir, "C1.md", storyFile("C1"))
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)
	f.opts.SubIssueCreator = creator
	f.opts.SubIssueRenderer = renderer

	result := f.execute(f.request())
	if !result.Succeeded() {
		t.Fatalf("turn did not succeed: %+v\nlogs:\n%s", result, f.logs)
	}
	wantFiles := []string{dir + "/C1.md", dir + "/plan_brief.md"}
	gotFiles := append([]string(nil), result.Files()...)
	sortStrings(gotFiles)
	if !equalStrings(gotFiles, wantFiles) {
		t.Errorf("result.Files() = %v, want %v (the phase's own commit must not appear)", gotFiles, wantFiles)
	}

	head := f.repo.GitIn(f.worktreeDir(), "rev-parse", "HEAD")
	if head == result.SHAAfter {
		t.Errorf("worktree HEAD (%s) still equals result.SHAAfter; the phase's commit never landed", head)
	}
	if got := commitCount(t, f, testBranch); got != 3 {
		t.Errorf("branch carries %d commits, want 3 (the base commit, commit1 reported, commit2 not)", got)
	}
}

// TestFileStoryIssuesFailureLeavesTheRunResultUnchanged is AC-W7: whatever
// goes wrong inside the phase, result.Outcome, result.Disposition and the
// runs row in the store are exactly what they would have been without the
// phase running at all.
func TestFileStoryIssuesFailureLeavesTheRunResultUnchanged(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		creator func() *fakeSubIssueCreator
	}{
		{
			name: "ListSubIssues fails outright",
			creator: func() *fakeSubIssueCreator {
				c := newFakeSubIssueCreator()
				c.batchErr = errors.New("simulated: unauthorized")
				return c
			},
		},
		{
			name: "CreateIssue fails mid-batch",
			creator: func() *fakeSubIssueCreator {
				c := newFakeSubIssueCreator()
				c.failStory = "C1"
				return c
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			creator := tt.creator()
			renderer := newSpyRenderer(t)
			dir := testSessionDir(t)

			var f *fixture
			runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
				writeSessionFile(t, req, dir, "plan_brief.md", planBrief("C1"))
				writeSessionFile(t, req, dir, "C1.md", storyFile("C1"))
				return succeeded(req, f.paths.Home()), nil
			})
			f = newFixture(t, runner)
			f.opts.SubIssueCreator = creator
			f.opts.SubIssueRenderer = renderer

			result := f.execute(f.request())
			if !result.Succeeded() {
				t.Fatalf("turn did not succeed: %+v\nlogs:\n%s", result, f.logs)
			}
			if result.Disposition != DispositionRan {
				t.Errorf("disposition = %s, want %s", result.Disposition, DispositionRan)
			}
			if got := commitCount(t, f, testBranch); got != 2 {
				t.Errorf("branch carries %d commits, want 2 (the base commit and claude's, no write-back on failure)", got)
			}

			runs := f.runs()
			if len(runs) != 1 || runs[0].Status != store.RunSucceeded {
				t.Errorf("runs = %+v, want exactly one succeeded row", runs)
			}
		})
	}
}

// TestFileStoryIssuesCallsCreateSubIssuesExactlyOnce is AC-W9's call-count
// half: one turn that attempts the phase calls CreateSubIssues exactly once,
// never in a loop.
func TestFileStoryIssuesCallsCreateSubIssuesExactlyOnce(t *testing.T) {
	t.Parallel()

	creator := newFakeSubIssueCreator()
	renderer := newSpyRenderer(t)
	dir := testSessionDir(t)

	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		writeSessionFile(t, req, dir, "plan_brief.md", planBrief("C1"))
		writeSessionFile(t, req, dir, "C1.md", storyFile("C1"))
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)
	f.opts.SubIssueCreator = creator
	f.opts.SubIssueRenderer = renderer

	f.execute(f.request())

	if got := creator.callCount(); got != 1 {
		t.Errorf("CreateSubIssues called %d times, want exactly 1", got)
	}
}

// TestFileStoryIssuesSkipsWithNoDependenciesConfigured proves the phase is
// opt-in at the Options level: a fixture that never sets SubIssueCreator or
// SubIssueRenderer — every other test in this package — runs exactly as it
// did before this phase existed.
func TestFileStoryIssuesSkipsWithNoDependenciesConfigured(t *testing.T) {
	t.Parallel()

	dir := testSessionDir(t)
	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		writeSessionFile(t, req, dir, "plan_brief.md", planBrief("C1"))
		writeSessionFile(t, req, dir, "C1.md", storyFile("C1"))
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)
	// f.opts.SubIssueCreator and SubIssueRenderer are left nil.

	result := f.execute(f.request())
	if !result.Succeeded() {
		t.Fatalf("turn did not succeed: %+v\nlogs:\n%s", result, f.logs)
	}
	if got := commitCount(t, f, testBranch); got != 2 {
		t.Errorf("branch carries %d commits, want 2 (the base commit and claude's; the phase must not run at all)", got)
	}
}

// TestFileStoryIssuesFallsBackToABranchDirectoryScan exercises the bounded
// directory scan [locateSessionDir] falls back to when the run's own start
// day is not where the session was named — a retry days after the session
// directory was written, which is exactly what the fallback exists for.
func TestFileStoryIssuesFallsBackToABranchDirectoryScan(t *testing.T) {
	t.Parallel()

	creator := newFakeSubIssueCreator()
	renderer := newSpyRenderer(t)

	yesterday := time.Now().UTC().AddDate(0, 0, -1)
	oldDir, err := naming.AITeamSessionDir(testBranch, yesterday, testSlug)
	if err != nil {
		t.Fatalf("derive yesterday's session directory: %v", err)
	}

	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		writeSessionFile(t, req, oldDir, "plan_brief.md", planBrief("C1"))
		writeSessionFile(t, req, oldDir, "C1.md", storyFile("C1"))
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)
	f.opts.SubIssueCreator = creator
	f.opts.SubIssueRenderer = renderer

	result := f.execute(f.request())
	if !result.Succeeded() {
		t.Fatalf("turn did not succeed: %+v\nlogs:\n%s", result, f.logs)
	}
	if got := creator.writeCount(); got != 1 {
		t.Errorf("filed %d issues via the fallback scan, want 1", got)
	}
	content := originFile(t, f, testBranch, oldDir+"/plan_brief.md")
	if !strings.Contains(content, "https://github.com/acme/acme-api/issues/41") {
		t.Errorf("plan_brief.md under the fallback-found directory was not resolved:\n%s", content)
	}
}

// sortStrings and equalStrings are tiny local helpers so this file adds no
// import of slices or sort for two call sites.
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestFileStoryIssuesSkipsWhenTheRunsOwnCommitFailed closes CR-B-002.
//
// The guard here used to be `result.Changed && !result.Pushed`, which reads as
// "the push failed" and covers exactly that. It does not cover [save]'s *other*
// failure: when Worktree.Commit itself errors, save returns with Err set but
// Changed, Pushed and SHAAfter all still at their zero values, so that
// condition was false and the phase ran anyway.
//
// That mattered for two reasons, and the second is the worse one. The
// per-story files are not in git at all, so the phase would file real GitHub
// sub-issues from content no commit holds. And its own write-back stages with
// git add --all, so it would sweep claude's still-uncommitted work into a
// second commit that no runs row names — while the run itself is reported as a
// commit failure.
//
// The commit is broken from inside the run, after claude has written the
// session files, by holding the index lock: git add cannot take it, so the
// commit fails while everything before it has already succeeded. That is the
// only ordering that isolates the commit the way
// TestExecutePushFailurePreservesTheCommit isolates the push.
func TestFileStoryIssuesSkipsWhenTheRunsOwnCommitFailed(t *testing.T) {
	t.Parallel()

	creator := newFakeSubIssueCreator()
	renderer := newSpyRenderer(t)

	dir := testSessionDir(t)
	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		writeSessionFile(t, req, dir, "plan_brief.md", planBrief("C1"))
		writeSessionFile(t, req, dir, "C1.md", storyFile("C1"))
		lockIndex(t, req.WorktreePath)
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)
	f.opts.SubIssueCreator = creator
	f.opts.SubIssueRenderer = renderer

	result := f.execute(f.request())

	switch {
	case result.Err == nil:
		t.Fatalf("the run's commit was sabotaged but Err is nil: %+v\nlogs:\n%s", result, f.logs)
	case result.Committed:
		t.Errorf("Committed = true after the index lock was held")
	}
	if creator.calls != 0 {
		t.Errorf("CreateSubIssues was called %d times after the run's own commit failed; "+
			"the phase must file nothing from content no commit holds", creator.calls)
	}
	if seen := renderer.storiesSeen(); len(seen) != 0 {
		t.Errorf("RenderSubIssue was called for %d stories after the run's own commit failed", len(seen))
	}
	// No commitCount assertion here, deliberately: it counts commits on the
	// origin, and a run whose commit failed never pushed, so the branch does
	// not exist there at all. That absence is the same proof — there is no
	// write-back commit because there is no branch to carry one.
	if result.Pushed {
		t.Error("Pushed = true after the commit failed")
	}
}

// lockIndex takes git's index lock in worktree and never releases it, which
// makes the next `git add` in that worktree fail. It resolves the real git
// directory first: a linked worktree's .git is a file naming it, not the
// directory itself, and the index lives beside the index the file points at.
func lockIndex(t *testing.T, worktree string) {
	t.Helper()

	gitPath := filepath.Join(worktree, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		t.Fatalf("stat %s: %v", gitPath, err)
	}

	gitDir := gitPath
	if !info.IsDir() {
		contents, err := os.ReadFile(gitPath) //nolint:gosec // a test-owned path
		if err != nil {
			t.Fatalf("read %s: %v", gitPath, err)
		}
		pointer, ok := strings.CutPrefix(strings.TrimSpace(string(contents)), "gitdir:")
		if !ok {
			t.Fatalf("%s is a file but does not name a gitdir: %q", gitPath, contents)
		}
		gitDir = strings.TrimSpace(pointer)
	}

	lock := filepath.Join(gitDir, "index.lock")
	if err := os.WriteFile(lock, nil, 0o600); err != nil {
		t.Fatalf("take the index lock at %s: %v", lock, err)
	}
	t.Cleanup(func() { _ = os.Remove(lock) })
}
