package cli

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// newBaseFixture is a registrable clone whose GitHub-shaped origin URL is
// rewritten by git to the fixture's local bare remote, which has main and stage.
func newBaseFixture(t *testing.T) *gittest.Repo {
	t.Helper()
	repo := newRepoFixture(t)
	repo.Git("push", "--quiet", repo.Origin, "main:stage")
	return repo
}

// redirectOrigin makes git resolve the GitHub-shaped origin URL to target (or
// back to itself when target is empty). Registration reads the URL as GitHub, so
// the redirect is switched on only around commands that talk to the remote.
func redirectOrigin(t *testing.T, repo *gittest.Repo, target string) {
	t.Helper()
	if current, ok := redirected[repo.Dir]; ok {
		repo.Git("config", "--remove-section", "url."+current)
		delete(redirected, repo.Dir)
	}
	if target != "" {
		repo.Git("config", "url."+target+".insteadOf", repoTestRemoteURL)
		redirected[repo.Dir] = target
	}
}

// redirected records the active redirect per clone (clone directories are unique per test).
var redirected = map[string]string{}

func openBaseDB(t *testing.T, repoPathsDB string) *store.DB {
	t.Helper()
	db, err := store.Open(t.Context(), repoPathsDB)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestRepoBaseMovesRegisteredRepositoryToStage drives `repo base` against a
// real database and a local bare remote.
func TestRepoBaseMovesRegisteredRepositoryToStage(t *testing.T) {
	paths := newRepoHome(t)
	clone := newBaseFixture(t)
	router, stdout, stderr := newTestRouter(t)
	run := func(args ...string) {
		t.Helper()
		stdout.Reset()
		stderr.Reset()
		if code := router.Run(t.Context(), args); code != ExitOK {
			t.Fatalf("%v: exit %d, stderr %s", args, code, stderr)
		}
	}
	run("repo", "add", clone.Dir)
	registered := listRepos(t, paths)
	if len(registered) != 1 || registered[0].BaseBranch != "main" {
		t.Fatalf("registered = %+v, want one repo on main", registered)
	}
	repo := registered[0]
	id := strconv.FormatInt(repo.ID, 10)

	before, err := openBaseDB(t, paths.DBPath()).CurrentConnection(t.Context(), repo.ID, store.CodeConnection)
	if err != nil || before.BaseBranch != "main" {
		t.Fatalf("code connection before = %+v, %v", before, err)
	}

	redirectOrigin(t, clone, clone.Origin)
	run("repo", "base", id, "stage")
	if !strings.Contains(stdout.String(), "base main -> stage") {
		t.Fatalf("stdout = %q, want base main -> stage", stdout)
	}
	run("repo", "list")
	if !strings.Contains(stdout.String(), "stage") || strings.Contains(stdout.String(), "main") {
		t.Fatalf("repo list = %q, want stage only", stdout)
	}

	db := openBaseDB(t, paths.DBPath())
	after, err := db.CurrentConnection(t.Context(), repo.ID, store.CodeConnection)
	if err != nil {
		t.Fatal(err)
	}
	if after.BaseBranch != "stage" || after.Revision <= before.Revision {
		t.Fatalf("code connection after = base %q rev %d, before rev %d", after.BaseBranch, after.Revision, before.Revision)
	}
	history, err := db.GetConnectionVersion(t.Context(), before.ID)
	if err != nil || history.BaseBranch != "main" {
		t.Fatalf("earlier revision = %+v, %v; want kept with main", history, err)
	}

	// New work: a session admitted now retains the current code version, so its
	// execution target is cut from stage.
	tickets, err := db.CurrentConnection(t.Context(), repo.ID, store.TicketConnection)
	if err != nil {
		t.Fatal(err)
	}
	live, err := db.GetRepo(t.Context(), repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(t.Context(), live, 7)
	if err != nil {
		t.Fatal(err)
	}
	const origin = "https://api.github.com"
	ref := ticket.Ref{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: "707"}, Locator: ticket.Locator{Provider: ticket.GitHub, Origin: origin, Container: live.Slug(), Key: "7"}}
	if _, err = db.BindSessionTicket(t.Context(), session.ID, ref); err != nil {
		t.Fatal(err)
	}
	binding, err := db.AddTicketBinding(t.Context(), store.TicketBinding{RepoID: repo.ID, ConnectionVersionID: tickets.ID, Ref: ref, Enabled: true, Provenance: "explicit"})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AttachExecutionSnapshot(t.Context(), store.ExecutionSnapshot{SessionID: session.ID, RepoID: repo.ID, BindingID: binding.ID, CodeVersionID: after.ID, TicketVersionID: tickets.ID, ActorID: 99, ActorLogin: "publisher"}); err != nil {
		t.Fatal(err)
	}
	target, err := db.SessionRepository(t.Context(), session)
	if err != nil || target.BaseBranch != "stage" {
		t.Fatalf("new session base = %q, %v; want stage", target.BaseBranch, err)
	}
}

func TestRepoBaseRefusalsAndRegistrationGuard(t *testing.T) {
	paths := newRepoHome(t)
	clone := newBaseFixture(t)
	router, stdout, stderr := newTestRouter(t)
	run := func(args ...string) int {
		t.Helper()
		stdout.Reset()
		stderr.Reset()
		return router.Run(t.Context(), args)
	}
	if code := run("repo", "add", clone.Dir); code != ExitOK {
		t.Fatalf("add: %d %s", code, stderr)
	}
	id := strconv.FormatInt(listRepos(t, paths)[0].ID, 10)
	base := func() string { return listRepos(t, paths)[0].BaseBranch }

	redirectOrigin(t, clone, clone.Origin)
	// A branch that is not on the remote is refused and nothing changes.
	if code := run("repo", "base", id, "nope"); code == ExitOK || !strings.Contains(stderr.String(), "not on the remote") {
		t.Fatalf("missing branch: exit %d stderr %q", code, stderr)
	}
	// Unknown id and unknown path.
	for _, ref := range []string{"9999", filepath.Join(t.TempDir(), "absent")} {
		if code := run("repo", "base", ref, "stage"); code == ExitOK || !strings.Contains(stderr.String(), "no registered repository") {
			t.Fatalf("unknown %s: exit %d stderr %q", ref, code, stderr)
		}
	}
	// A registered path works as the reference, and asking for the current base is a no-op.
	if code := run("repo", "base", clone.Dir, "main"); code != ExitOK || !strings.Contains(stdout.String(), "no change") {
		t.Fatalf("no change: exit %d stdout %q stderr %q", code, stdout, stderr)
	}
	redirectOrigin(t, clone, "")
	// repo add must not change the base of a registered repository.
	if code := run("repo", "add", clone.Dir, "--base", "stage"); code == ExitOK || !strings.Contains(stderr.String(), "repo base") {
		t.Fatalf("add --base: exit %d stderr %q", code, stderr)
	}
	if got := base(); got != "main" {
		t.Fatalf("base after refused add = %q, want main", got)
	}
	// Same or no base still works.
	if code := run("repo", "add", clone.Dir, "--base", "main"); code != ExitOK {
		t.Fatalf("add same base: %d %s", code, stderr)
	}
	if code := run("repo", "add", clone.Dir); code != ExitOK {
		t.Fatalf("add no base: %d %s", code, stderr)
	}

	// An unreachable remote refuses and leaves the base alone.
	redirectOrigin(t, clone, filepath.Join(t.TempDir(), "gone.git"))
	if code := run("repo", "base", id, "stage"); code == ExitOK || !strings.Contains(stderr.String(), "cannot be reached") {
		t.Fatalf("unreachable: exit %d stderr %q", code, stderr)
	}
	if got := base(); got != "main" {
		t.Fatalf("base after unreachable = %q, want main", got)
	}
}
