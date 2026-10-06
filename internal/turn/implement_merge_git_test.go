package turn

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/bugchecks"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/store"
)

const implementFeature = "lf/1-feature"

// implementGit is a real temporary repository with a feature (integration)
// branch, a local bare origin and the merge activity wired to both.
type implementGit struct {
	*implementFixture
	repo        *gittest.Repo
	clone       *git.Clone
	scratchRoot string
	sawFiles    [][]string
}

func newImplementGit(t *testing.T, withRecipe bool, stories ...ImplementStory) *implementGit {
	t.Helper()
	g := &implementGit{implementFixture: newImplementFixture(t, stories...), repo: gittest.New(t)}
	if withRecipe {
		g.repo.WriteFile(".litefactory/bug-checks.yaml", implementRecipe)
		g.repo.Commit("add build checks")
		g.repo.Git("push", "--quiet", "origin", gittest.DefaultBranch)
	}
	g.repo.Git("branch", implementFeature)
	g.repo.Git("push", "--quiet", "origin", implementFeature)
	binary, err := git.Resolve(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if g.clone, err = binary.Open(t.Context(), g.repo.Dir); err != nil {
		t.Fatal(err)
	}
	g.scratchRoot = filepath.Join(t.TempDir(), "scratch")
	g.bindings.Repo = GitImplementRepo{Clone: g.clone, Remote: "origin", ScratchRoot: g.scratchRoot, Author: git.Author{Name: "Test", Email: "test@example.invalid"}}
	g.bindings.RunCheck = func(_ context.Context, worktree string, _ config.BugCheckCommand) bugchecks.CommandResult {
		entries, _ := os.ReadDir(worktree)
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		g.sawFiles = append(g.sawFiles, names)
		if _, err := os.Stat(filepath.Join(worktree, "bad.txt")); err == nil {
			exit := 1
			return bugchecks.CommandResult{Outcome: "failed", Exit: &exit}
		}
		return bugchecks.CommandResult{Outcome: "passed"}
	}
	g.auth.IntegrationBranch = implementFeature
	return g
}

// pushStory publishes a story branch cut from main that writes one file.
func (g *implementGit) pushStory(key, file, content string) {
	g.t.Helper()
	g.repo.Git("checkout", "--quiet", "-b", "lf/"+key, gittest.DefaultBranch)
	g.repo.WriteFile(file, content)
	g.repo.Commit("work for " + key)
	g.repo.Git("push", "--quiet", "origin", "lf/"+key)
	g.repo.Git("checkout", "--quiet", gittest.DefaultBranch)
}

func (g *implementGit) remoteFeature() string {
	g.t.Helper()
	fields := strings.Fields(g.repo.Git("ls-remote", "--refs", "origin", "refs/heads/"+implementFeature))
	if len(fields) != 2 {
		g.t.Fatalf("integration branch missing on origin: %v", fields)
	}
	return fields[0]
}

// remoteFile reads a file from the integration branch on the origin.
func (g *implementGit) remoteFile(file string) string {
	g.t.Helper()
	g.repo.Git("fetch", "--quiet", "origin")
	return g.repo.Git("show", "origin/"+implementFeature+":"+file)
}

func (g *implementGit) scratchLeft() int {
	entries, err := os.ReadDir(g.scratchRoot)
	if err != nil {
		return 0
	}
	return len(entries)
}

func (g *implementGit) start(keys ...string) {
	g.t.Helper()
	g.ready()
	for _, k := range keys {
		g.drive(k, store.ImplementDone, implementClock)
	}
}

func TestImplementMergeGitGreenMergeLandsOnTheIntegrationBranchOnly(t *testing.T) {
	g := newImplementGit(t, true, implementStories(1, nil)...)
	g.pushStory("story-001", "feature.txt", "hello\n")
	g.start("story-001")
	mainBefore := g.repo.Git("rev-parse", "origin/"+gittest.DefaultBranch)
	featureBefore := g.remoteFeature()

	res, out, err := g.merge()
	if err != nil || res.Outcome != "merged" {
		t.Fatalf("%s %+v %v", res.Outcome, out, err)
	}
	head := g.remoteFeature()
	if head == featureBefore || head != out.Commit {
		t.Fatalf("feature head %s, merge commit %s, before %s", head, out.Commit, featureBefore)
	}
	if parents := g.repo.Git("rev-list", "--parents", "-n", "1", head); len(strings.Fields(parents)) != 3 {
		t.Fatalf("expected a merge commit, got %q", parents)
	}
	if got := g.remoteFile("feature.txt"); got != "hello" {
		t.Fatalf("feature.txt on the integration branch = %q", got)
	}
	if got := g.repo.Git("rev-parse", "origin/"+gittest.DefaultBranch); got != mainBefore {
		t.Fatal("the default branch must never move")
	}
	if len(g.sawFiles) != 3 {
		t.Fatalf("every phase runs once, ran %d", len(g.sawFiles))
	}
	for _, names := range g.sawFiles {
		if !strings.Contains(strings.Join(names, ","), "feature.txt") {
			t.Fatalf("checks must run on the merged tree, saw %v", names)
		}
	}
	if row := g.row("story-001"); row.State != store.ImplementMerged || row.MergeCommit != head {
		t.Fatalf("row = %+v", row)
	}
	notes := g.forge.bodies(11)
	if len(notes) != 1 || !strings.Contains(notes[0], head) {
		t.Fatalf("notes = %q", notes)
	}
	if g.scratchLeft() != 0 {
		t.Fatal("scratch worktree was not discarded")
	}
	if registered, _ := g.clone.WorktreeList(t.Context()); len(registered) != 1 {
		t.Fatalf("worktree registrations left behind: %v", registered)
	}
}

func TestImplementMergeGitConflictParksAndTheNextDoneStoryStillMerges(t *testing.T) {
	g := newImplementGit(t, true, implementStories(3, nil)...)
	g.pushStory("story-001", "shared.txt", "one\n")
	g.pushStory("story-002", "shared.txt", "two\n")
	g.pushStory("story-003", "other.txt", "three\n")
	g.start("story-001", "story-002", "story-003")

	if res, _, err := g.merge(); err != nil || res.Outcome != "merged" {
		t.Fatalf("story-001: %s %v", res.Outcome, err)
	}
	afterFirst := g.remoteFeature()
	res, out, err := g.merge()
	if err != nil || res.Outcome != "parked" || out.Story != "story-002" || !strings.Contains(out.Reason, "conflicts") {
		t.Fatalf("story-002: %s %+v %v", res.Outcome, out, err)
	}
	if g.remoteFeature() != afterFirst {
		t.Fatal("a conflict must leave the integration branch unchanged")
	}
	if g.scratchLeft() != 0 {
		t.Fatal("scratch worktree was not discarded")
	}
	if res, out, err = g.merge(); err != nil || res.Outcome != "merged" || out.Story != "story-003" {
		t.Fatalf("story-003: %s %+v %v", res.Outcome, out, err)
	}
	if row := g.row("story-002"); row.State != store.ImplementParked || row.Reason == "" {
		t.Fatalf("story-002 row = %+v", row)
	}
	if len(g.forge.bodies(12)) != 0 {
		t.Fatal("a parked story gets no merged note")
	}
}

func TestImplementMergeGitFailingCheckParksWithoutPublishing(t *testing.T) {
	g := newImplementGit(t, true, implementStories(1, nil)...)
	g.pushStory("story-001", "bad.txt", "breaks the build\n")
	g.start("story-001")
	before := g.remoteFeature()
	res, out, err := g.merge()
	if err != nil || res.Outcome != "parked" || !strings.Contains(out.Reason, "build check failed") {
		t.Fatalf("%s %+v %v", res.Outcome, out, err)
	}
	if g.remoteFeature() != before || g.forge.posts != 0 || g.scratchLeft() != 0 {
		t.Fatal("a failing check must publish nothing and clean up")
	}
}

func TestImplementMergeGitMissingRecipeParksWithoutMerging(t *testing.T) {
	g := newImplementGit(t, false, implementStories(1, nil)...)
	g.pushStory("story-001", "feature.txt", "x\n")
	g.start("story-001")
	before := g.remoteFeature()
	res, out, err := g.merge()
	if err != nil || res.Outcome != "parked" || out.Reason != "no build check configured" {
		t.Fatalf("%s %+v %v", res.Outcome, out, err)
	}
	if g.remoteFeature() != before || len(g.sawFiles) != 0 {
		t.Fatal("no check configured means nothing may be merged or run")
	}
}

// movingRepo advances the remote integration branch once, between the merge and
// its push, as a competing writer would.
type movingRepo struct {
	ImplementRepo
	advance func()
}

func (m *movingRepo) Begin(ctx context.Context, branch string) (ImplementScratch, error) {
	s, err := m.ImplementRepo.Begin(ctx, branch)
	if err != nil {
		return nil, err
	}
	advance := m.advance
	m.advance = nil
	return &movingScratch{ImplementScratch: s, advance: advance}, nil
}

type movingScratch struct {
	ImplementScratch
	advance func()
}

func (s *movingScratch) Publish(ctx context.Context, branch, expected string) error {
	if s.advance != nil {
		s.advance()
		s.advance = nil
	}
	return s.ImplementScratch.Publish(ctx, branch, expected)
}

func TestImplementMergeGitPushRaceRetriesAndTheRebuiltMergeLands(t *testing.T) {
	g := newImplementGit(t, true, implementStories(1, nil)...)
	g.pushStory("story-001", "feature.txt", "mine\n")
	g.start("story-001")
	fired := false
	g.bindings.Repo = &movingRepo{ImplementRepo: g.bindings.Repo, advance: func() {
		fired = true
		g.repo.Git("checkout", "--quiet", implementFeature)
		g.repo.WriteFile("rival.txt", "rival\n")
		g.repo.Commit("a rival writer")
		g.repo.Git("push", "--quiet", "origin", implementFeature)
		g.repo.Git("checkout", "--quiet", gittest.DefaultBranch)
	}}
	res, out, err := g.merge()
	if err != nil || res.Outcome != "retry" || !fired {
		t.Fatalf("%s %+v %v", res.Outcome, out, err)
	}
	rival := g.remoteFeature()
	if row := g.row("story-001"); row.State != store.ImplementDone || g.forge.posts != 0 {
		t.Fatalf("a push race must neither park nor post: %+v", row)
	}
	if res, out, err = g.merge(); err != nil || res.Outcome != "merged" {
		t.Fatalf("retry: %s %+v %v", res.Outcome, out, err)
	}
	if g.remoteFile("feature.txt") != "mine" || g.remoteFile("rival.txt") != "rival" {
		t.Fatal("the rebuilt merge must keep the rival's work and add the story")
	}
	if ancestor := g.repo.Git("merge-base", rival, g.remoteFeature()); ancestor != rival {
		t.Fatal("the push must fast-forward from the rival's head")
	}
}

func TestImplementMergeGitStoryAlreadyOnTheIntegrationBranchIsRecordedWithoutChecks(t *testing.T) {
	g := newImplementGit(t, true, implementStories(1, nil)...)
	g.pushStory("story-001", "feature.txt", "x\n")
	g.start("story-001")
	// An earlier call published the merge and died before the ledger moved.
	g.repo.Git("push", "--quiet", "origin", "lf/story-001:refs/heads/"+implementFeature)
	head := g.remoteFeature()
	res, out, err := g.merge()
	if err != nil || res.Outcome != "merged" || out.Commit != head {
		t.Fatalf("%s %+v %v", res.Outcome, out, err)
	}
	if len(g.sawFiles) != 0 || g.remoteFeature() != head || len(g.forge.bodies(11)) != 1 {
		t.Fatal("nothing more to publish, one note")
	}
}
