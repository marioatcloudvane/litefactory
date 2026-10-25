package turn

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/naming"

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
}

func newImplementGit(t *testing.T, stories ...ImplementStory) *implementGit {
	t.Helper()
	g := &implementGit{implementFixture: newImplementFixture(t, stories...), repo: gittest.New(t)}
	g.repo.Git("branch", implementFeature)
	g.repo.Git("push", "--quiet", "origin", implementFeature)
	binary, err := git.Resolve(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if g.clone, err = binary.Open(t.Context(), g.repo.Dir); err != nil {
		t.Fatal(err)
	}
	g.scratchRoot = config.NewPaths(filepath.Join(t.TempDir(), "home")).ImplementScratchDir()
	g.bindings.Repo = GitImplementRepo{Clone: g.clone, Remote: "origin", ScratchRoot: g.scratchRoot, Author: git.Author{Name: "Test", Email: "test@example.invalid"}}
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
	g := newImplementGit(t, implementStories(1, nil)...)
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

func TestImplementMergeGitConflictRestartsTheStoryAndTheNextDoneStoryStillMerges(t *testing.T) {
	g := newImplementGit(t, implementStories(3, nil)...)
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
	conflictReason := out.Reason
	if !strings.Contains(out.Reason, "shared.txt") {
		t.Fatalf("the restart reason does not name the conflicting path: %q", out.Reason)
	}
	if w := g.logs.onePark(t, "story-002", implementFeature); !strings.Contains(w["reason"], "shared.txt") {
		t.Fatalf("the park log does not name the conflicting path: %v", w)
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
	if row := g.row("story-002"); row.State != store.ImplementWaiting || row.Attempts != 0 || row.ParkOrigin != "" || row.GuidanceQuestion != conflictReason {
		t.Fatalf("story-002 row = %+v, want waiting so its worker resolves the conflict", row)
	}
	if len(g.forge.bodies(12)) != 0 {
		t.Fatal("a parked story gets no merged note")
	}
	if _, ready := g.ready(); len(ready.Started) != 1 || ready.Started[0].Key != "story-002" {
		t.Fatalf("ready started %+v, want story-002 again", ready.Started)
	}

	// The worker's run: the factory starts the merge of the feature branch,
	// the worker resolves the conflict, and the save concludes the merge.
	featureTip := g.remoteFeature()
	g.repo.Git("fetch", "--quiet", "origin")
	worktree, err := g.clone.WorktreeAdd(t.Context(), filepath.Join(t.TempDir(), "story-002"), git.WorktreeAddOptions{Branch: "lf/story-002"})
	if err != nil {
		t.Fatal(err)
	}
	conflicts, err := worktree.StartMerge(t.Context(), "origin/"+implementFeature)
	if err != nil || len(conflicts) != 1 || conflicts[0] != "shared.txt" {
		t.Fatalf("StartMerge = %v, %v", conflicts, err)
	}
	if err := os.WriteFile(filepath.Join(worktree.Path(), "shared.txt"), []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Commit(t.Context(), git.CommitOptions{Summary: "Resolve the conflict.", Task: "github:acme/widgets#12", Mode: "implement", Run: 1, Author: git.Author{Name: "Test", Email: "test@example.invalid"}}); err != nil {
		t.Fatal(err)
	}
	if parents := strings.Fields(g.repo.GitIn(worktree.Path(), "rev-list", "--parents", "-n", "1", "HEAD")); len(parents) != 3 || parents[2] != featureTip {
		t.Fatalf("story-002 head parents = %v, want a merge of %s", parents, featureTip)
	}
	if err := worktree.Push(t.Context(), "origin"); err != nil {
		t.Fatal(err)
	}
	g.drive("story-002", store.ImplementDone, implementClock)
	if res, out, err = g.merge(); err != nil || res.Outcome != "merged" || out.Story != "story-002" {
		t.Fatalf("story-002 after resolution: %s %+v %v", res.Outcome, out, err)
	}
	if got := g.remoteFile("shared.txt"); got != "one\ntwo" {
		t.Fatalf("shared.txt on the integration branch = %q", got)
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
	return &movingScratch{ImplementScratch: s, advance: func() {
		if m.advance != nil {
			advance := m.advance
			m.advance = nil
			advance()
		}
	}}, nil
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
	g := newImplementGit(t, implementStories(1, nil)...)
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
	g := newImplementGit(t, implementStories(1, nil)...)
	g.pushStory("story-001", "feature.txt", "x\n")
	g.start("story-001")
	// An earlier call published the merge and died before the ledger moved.
	g.repo.Git("push", "--quiet", "origin", "lf/story-001:refs/heads/"+implementFeature)
	head := g.remoteFeature()
	res, out, err := g.merge()
	if err != nil || res.Outcome != "merged" || out.Commit != head {
		t.Fatalf("%s %+v %v", res.Outcome, out, err)
	}
	if g.remoteFeature() != head || len(g.forge.bodies(11)) != 1 {
		t.Fatal("nothing more to publish, one note")
	}
}

func TestImplementMergeGitIndependentTimestampMigrations(t *testing.T) {
	g := newImplementGit(t, implementStories(2, nil)...)
	const one = "internal/store/migrations/20261007130500123_one.sql"
	const two = "internal/store/migrations/20261007130500124_two.sql"
	g.pushStory("story-001", one, "CREATE TABLE one (id INTEGER);\n")
	g.pushStory("story-002", two, "CREATE TABLE two (id INTEGER);\n")
	g.start("story-001", "story-002")
	for range 2 {
		if res, out, err := g.merge(); err != nil || res.Outcome != "merged" {
			t.Fatalf("%s %+v %v", res.Outcome, out, err)
		}
	}
	if g.remoteFile(one) != "CREATE TABLE one (id INTEGER);" || g.remoteFile(two) != "CREATE TABLE two (id INTEGER);" {
		t.Fatal("combined migration contents changed")
	}
	if g.scratchLeft() != 0 || g.forge.posts != 2 {
		t.Fatal("expected cleanup and both merged notes")
	}
}

func TestImplementMergeGitDuplicateMigrationParksAndResumes(t *testing.T) {
	g := newImplementGit(t, implementStories(2, nil)...)
	const one = "internal/store/migrations/20261007130500123_one.sql"
	const two = "internal/store/migrations/20261007130500123_two.sql"
	const renamed = "internal/store/migrations/20261007130500124_two.sql"
	g.pushStory("story-001", one, "CREATE TABLE one (id INTEGER);\n")
	g.pushStory("story-002", two, "CREATE TABLE two (id INTEGER);\n")
	g.start("story-001", "story-002")
	if res, _, err := g.merge(); err != nil || res.Outcome != "merged" {
		t.Fatalf("first: %s %v", res.Outcome, err)
	}
	before := g.remoteFeature()
	res, out, err := g.merge()
	if err != nil || res.Outcome != "parked" {
		t.Fatalf("%s %+v %v", res.Outcome, out, err)
	}
	for _, want := range []string{filepath.Base(one), filepath.Base(two), "share version 20261007130500123"} {
		if !strings.Contains(out.Reason, want) {
			t.Fatalf("reason %q missing %q", out.Reason, want)
		}
	}
	if g.remoteFeature() != before || g.forge.posts != 1 || len(g.forge.bodies(12)) != 0 || g.scratchLeft() != 0 {
		t.Fatal("duplicate published, posted or leaked scratch")
	}
	if row := g.row("story-002"); row.State != store.ImplementParked || row.Reason != out.Reason {
		t.Fatalf("row: %+v", row)
	}
	if w := g.logs.onePark(t, "story-002", implementFeature); w["reason"] != out.Reason {
		t.Fatalf("park log = %v", w)
	}
	rt := &implementRuntime{opts: Options{DB: g.db}, parentRef: implementParent, generation: 1}
	if n, err := rt.resumeStories(t.Context(), "retry story-002 with a fresh migration timestamp"); err != nil || n != 1 {
		t.Fatalf("resume %d: %v", n, err)
	}
	g.repo.Git("checkout", "--quiet", "lf/story-002")
	g.repo.Git("mv", two, renamed)
	g.repo.Commit("rename only the unapplied migration")
	g.repo.Git("push", "--quiet", "origin", "lf/story-002")
	g.repo.Git("checkout", "--quiet", gittest.DefaultBranch)
	g.start("story-002")
	if res, out, err = g.merge(); err != nil || res.Outcome != "merged" {
		t.Fatalf("resumed: %s %+v %v", res.Outcome, out, err)
	}
	if g.remoteFile(renamed) != "CREATE TABLE two (id INTEGER);" || g.scratchLeft() != 0 {
		t.Fatal("rename did not land cleanly")
	}
}

func TestImplementMergeGitPushRaceRechecksMigrationVersions(t *testing.T) {
	g := newImplementGit(t, implementStories(1, nil)...)
	g.pushStory("story-001", "internal/store/migrations/20261007130500123_one.sql", "SELECT 1;\n")
	g.start("story-001")
	g.bindings.Repo = &movingRepo{ImplementRepo: g.bindings.Repo, advance: func() {
		g.repo.Git("checkout", "--quiet", implementFeature)
		g.repo.WriteFile("internal/store/migrations/20261007130500123_rival.sql", "SELECT 2;\n")
		g.repo.Commit("rival migration")
		g.repo.Git("push", "--quiet", "origin", implementFeature)
		g.repo.Git("checkout", "--quiet", gittest.DefaultBranch)
	}}
	if res, _, err := g.merge(); err != nil || res.Outcome != "retry" {
		t.Fatalf("first: %s %v", res.Outcome, err)
	}
	before := g.remoteFeature()
	if res, out, err := g.merge(); err != nil || res.Outcome != "parked" || !strings.Contains(out.Reason, "share version") {
		t.Fatalf("retry: %s %+v %v", res.Outcome, out, err)
	}
	if g.remoteFeature() != before || g.forge.posts != 0 || g.scratchLeft() != 0 {
		t.Fatal("revalidated conflict published or leaked")
	}
}

func TestImplementMergeGitMigrationInspectionFailuresPreventPublication(t *testing.T) {
	for _, tc := range []struct{ name, file string }{
		{"parse", "internal/store/migrations/bad.sql"},
		{"not a directory", "internal/store/migrations"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := newImplementGit(t, implementStories(1, nil)...)
			g.pushStory("story-001", tc.file, "SELECT 1;\n")
			g.start("story-001")
			before := g.remoteFeature()
			if _, _, err := g.merge(); err == nil || !strings.Contains(err.Error(), "inspect migrations") {
				t.Fatalf("expected inspection failure, got %v", err)
			}
			if g.remoteFeature() != before || g.forge.posts != 0 || g.scratchLeft() != 0 || g.row("story-001").State != store.ImplementDone {
				t.Fatal("inspection failure changed state or leaked")
			}
		})
	}
}

// A real merge, Validate handback and second worker commit retain the first
// round's note throughout. The completion report must describe the new merge.
func TestImplementMergeGitReopenedStoryMergesCurrentWork(t *testing.T) {
	f := newImplementBackFixture(t)
	g := newImplementGit(t, implementStories(3, nil)...)
	parent, err := naming.ParseRef(f.parent)
	if err != nil {
		t.Fatal(err)
	}
	g.auth.Parent = parent
	g.bindings.DB = f.db
	g.bindings.Now = func() time.Time { return f.clock.Add(time.Minute) }
	move := func(from, to string) {
		t.Helper()
		row := f.row("story-003")
		guidance := ""
		if from == store.ImplementMerged {
			guidance = "fixture finding for the first merge"
		}
		if _, err := f.db.TransitionImplementStory(t.Context(), store.ImplementTransition{
			Parent: f.parent, Generation: 1, Key: row.Key, From: from, To: to,
			ExpectedRevision: row.Revision, At: f.clock, Guidance: guidance,
		}); err != nil {
			t.Fatal(err)
		}
	}
	move(store.ImplementMerged, store.ImplementWaiting)
	move(store.ImplementWaiting, store.ImplementRunning)
	move(store.ImplementRunning, store.ImplementDone)
	g.pushStory("story-003", "feature.txt", "round one\n")
	res, first, err := g.merge()
	if err != nil || res.Outcome != "merged" {
		t.Fatalf("first merge: %+v %v", first, err)
	}
	// Only story-003 can advance in this fixture; the other rows stay merged.
	f.advance = func(branch, _ string) (bool, error) {
		if branch != "lf/story-003" {
			return false, nil
		}
		g.repo.Git("checkout", "--quiet", branch)
		g.repo.Git("merge", "--ff-only", first.Commit)
		g.repo.Git("push", "--quiet", "origin", branch)
		return true, nil
	}
	f.toOwner()
	f.say(f.command(ValidateImplementCommand))
	if row := f.row("story-003"); row.State != store.ImplementWaiting || row.MergeCommit != "" {
		t.Fatalf("Validate did not clear the first merge: %+v", row)
	}
	g.repo.WriteFile("feature.txt", "round two\n")
	tip := g.repo.Commit("repair the Validate finding")
	g.repo.Git("push", "--quiet", "origin", "lf/story-003")
	g.repo.Git("checkout", "--quiet", gittest.DefaultBranch)
	move(store.ImplementWaiting, store.ImplementRunning)
	move(store.ImplementRunning, store.ImplementDone)
	res, second, err := g.merge()
	if err != nil || res.Outcome != "merged" || second.Commit == first.Commit {
		t.Fatalf("second merge: %+v %v", second, err)
	}
	head := g.remoteFeature()
	if second.Commit != head || g.repo.Git("merge-base", tip, head) != tip || g.remoteFile("feature.txt") != "round two" {
		t.Fatalf("new work did not reach feature head %s: %+v", head, second)
	}
	if row := f.row("story-003"); row.State != store.ImplementMerged || row.MergeCommit != head {
		t.Fatalf("second merge ledger: %+v", row)
	}
	notes := g.forge.bodies(13)
	if len(notes) != 2 || !strings.Contains(notes[0], first.Commit) || !strings.Contains(notes[1], head) {
		t.Fatalf("merge notes: %q", notes)
	}
	candidate := implementCompletionCandidate(f.rows(), implementFeature, head, false)
	for _, want := range []string{"now at commit `" + head + "`", "**story-003**: merged as `" + head + "`"} {
		if !strings.Contains(candidate.Comment, want) {
			t.Fatalf("completion lacks %q: %s", want, candidate.Comment)
		}
	}
	if strings.Contains(candidate.Comment, first.Commit) {
		t.Fatalf("completion reused the first round: %s", candidate.Comment)
	}
}

func TestImplementMergeGitStaleLedgerFailureHandling(t *testing.T) {
	for _, scenario := range []string{"missing", "fetch", "conflict", "push race", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			g := newImplementGit(t, implementStories(1, nil)...)
			g.pushStory("story-001", "shared.txt", "first\n")
			g.start("story-001")
			if res, _, err := g.merge(); err != nil || res.Outcome != "merged" {
				t.Fatalf("first merge: %+v %v", res, err)
			}
			old := g.row("story-001")
			g.repo.Git("checkout", "--quiet", "lf/story-001")
			g.repo.WriteFile("shared.txt", "second\n")
			g.repo.Commit("new story work")
			g.repo.Git("push", "--quiet", "origin", "lf/story-001")
			g.repo.Git("checkout", "--quiet", gittest.DefaultBranch)
			ctx := t.Context()
			switch scenario {
			case "missing":
				g.repo.Git("push", "--quiet", "origin", "--delete", "lf/story-001")
			case "fetch":
				g.repo.Git("remote", "set-url", "origin", filepath.Join(t.TempDir(), "absent.git"))
			case "conflict", "push race":
				advance := func() {
					g.repo.Git("fetch", "--quiet", "origin")
					g.repo.Git("checkout", "--quiet", "--detach", "origin/"+implementFeature)
					file := "rival.txt"
					if scenario == "conflict" {
						file = "shared.txt"
					}
					g.repo.WriteFile(file, "rival\n")
					g.repo.Commit("rival work")
					g.repo.Git("push", "--quiet", "origin", "HEAD:refs/heads/"+implementFeature)
					g.repo.Git("checkout", "--quiet", gittest.DefaultBranch)
				}
				if scenario == "conflict" {
					advance()
				} else {
					g.bindings.Repo = &movingRepo{ImplementRepo: g.bindings.Repo, advance: advance}
				}
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			res, err := (ImplementMergeActivity{b: g.bindings}).Execute(ctx, Options{}, g.req())
			row := g.row("story-001")
			switch scenario {
			case "conflict":
				if err != nil || res.Outcome != "parked" || row.State != store.ImplementWaiting {
					t.Fatalf("conflict: %+v %+v %v", res, row, err)
				}
			case "push race":
				if err != nil || res.Outcome != "retry" || row.State != store.ImplementDone {
					t.Fatalf("race: %+v %+v %v", res, row, err)
				}
			default:
				if err == nil || row != old {
					t.Fatalf("uncertainty changed ledger: %+v %v", row, err)
				}
			}
			if g.forge.posts != 1 || g.scratchLeft() != 0 {
				t.Fatal("failure posted a merge note or leaked scratch")
			}
			unlock, lockErr := g.db.LockStoryProgress(t.Context(), g.auth.Parent.String(), g.lockDir)
			if lockErr != nil {
				t.Fatal(lockErr)
			}
			if err := unlock(); err != nil {
				t.Fatal(err)
			}
			if scenario == "push race" {
				if res, _, err := g.merge(); err != nil || res.Outcome != "merged" {
					t.Fatalf("retry: %+v %v", res, err)
				}
			}
		})
	}
}

func TestImplementScratchSiblingPrivacyAndCleanup(t *testing.T) {
	g := newImplementGit(t, implementStories(1, nil)...)
	repo := g.bindings.Repo
	one, err := repo.Begin(t.Context(), implementFeature)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = one.Close(context.WithoutCancel(t.Context())) }()
	two, err := repo.Begin(t.Context(), implementFeature)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = two.Close(context.WithoutCancel(t.Context())) }()
	for _, dir := range []string{g.scratchRoot, filepath.Dir(one.Path()), filepath.Dir(two.Path())} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Errorf("%s mode = %o", dir, info.Mode().Perm())
		}
	}
	if one.Path() == two.Path() {
		t.Fatal("scratch operations share a directory")
	}
	if err := one.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(one.Path())); !os.IsNotExist(err) {
		t.Fatalf("closed operation remains: %v", err)
	}
	if _, err := os.Stat(two.Path()); err != nil {
		t.Fatalf("sibling removed: %v", err)
	}
	if err := two.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	worktrees, err := g.clone.WorktreeList(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, wt := range worktrees {
		if wt.Path == one.Path() || wt.Path == two.Path() {
			t.Fatal("scratch remains registered")
		}
	}
}
