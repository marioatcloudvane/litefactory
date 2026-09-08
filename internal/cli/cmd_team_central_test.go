package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/teampublish"
)

// CR-B-004: a checkout directory must never override an operator source alias.
func TestTeamSourceAliasWinsDirectoryCollision(t *testing.T) {
	source, err := filepath.Abs("../../teams/source")
	if err != nil {
		t.Fatal(err)
	}
	output := t.TempDir()
	if err := teampublish.Publish(t.Context(), source, output, "go-team", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(http.FileServer(http.Dir(output)))
	defer server.Close()
	home := t.TempDir()
	t.Setenv(config.EnvHome, home)
	configuration := strings.Replace(string(config.DefaultFileContents()), "teams:\n", fmt.Sprintf("teams:\n  sources:\n    corp:\n      index_url: %s/teams.json\n", server.URL), 1)
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(configuration), 0600); err != nil {
		t.Fatal(err)
	}
	working := t.TempDir()
	if err := os.Mkdir(filepath.Join(working, "corp"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(working)
	router, out, stderr := newTestRouter(t)
	router.env.TeamTransport = server.Client().Transport
	if code := router.Run(t.Context(), []string{"team", "add", "go-team", "--source", "corp"}); code != ExitOK {
		t.Fatalf("alias lost to local directory: exit %d %s", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(home, "teams", "index.json")); err != nil {
		t.Fatal(err)
	}
	server.Close()
	out.Reset()
	stderr.Reset()
	if code := router.Run(t.Context(), []string{"team", "status", "--json"}); code != ExitOK {
		t.Fatalf("offline cached status: %d %s", code, stderr)
	}
	if !strings.Contains(out.String(), "1.0.0") {
		t.Fatalf("status omitted installed version: %s", out)
	}
	if code := router.Run(t.Context(), []string{"team", "add", "go-team", "--source", "./corp"}); code == ExitOK {
		t.Fatal("explicit empty local directory unexpectedly became remote alias")
	}
}

func TestCentralTeamCommandsOutsideGit(t *testing.T) {
	source, err := filepath.Abs("../../teams/source")
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	t.Setenv(config.EnvHome, home)
	t.Chdir(t.TempDir())
	router, out, stderr := newTestRouter(t)
	if code := router.Run(t.Context(), []string{"team", "add", "go-team", "--source", source}); code != ExitOK {
		t.Fatalf("add: %d %s", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(home, "teams/index.json")); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range [][]string{{"team", "list", "--json"}, {"team", "show", "go-team", "--mode", "research", "--json"}, {"team", "validate", "go-team", "--json"}, {"team", "status", "--json"}, {"team", "update", "--json"}} {
		out.Reset()
		stderr.Reset()
		if code := router.Run(t.Context(), cmd); code != ExitOK {
			t.Fatalf("%v: %d %s", cmd, code, stderr)
		}
		first := out.String()
		out.Reset()
		if code := router.Run(t.Context(), cmd); code != ExitOK {
			t.Fatalf("repeat %v: %d %s", cmd, code, stderr)
		}
		if first != out.String() {
			t.Fatalf("nondeterministic %v", cmd)
		}
	}
	for _, legacy := range []string{"--repo", "--all-worktrees"} {
		if code := router.Run(t.Context(), []string{"team", "update", legacy, "."}); code == ExitOK {
			t.Fatal("legacy option accepted", legacy)
		}
	}
}

func TestRepositoryExternalTeamAssociationDoesNotWriteClones(t *testing.T) {
	source, err := filepath.Abs("../../teams/source")
	if err != nil {
		t.Fatal(err)
	}
	paths := newRepoHome(t)
	router, out, stderr := newTestRouter(t)
	if code := router.Run(t.Context(), []string{"team", "add", "go-team", "--source", source}); code != ExitOK {
		t.Fatal(stderr)
	}
	first := newRepoFixture(t)
	second := newRepoFixture(t)
	for _, repo := range []*gittest.Repo{first, second} {
		gittest.AssertUnchanged(t, repo.Dir, func() {
			if code := router.Run(t.Context(), []string{"repo", "add", repo.Dir, "--team", "go-team"}); code != ExitOK {
				t.Fatalf("register %d: %s", code, stderr)
			}
		})
	}
	rows := listRepos(t, paths)
	if len(rows) != 2 {
		t.Fatal(rows)
	}
	for _, r := range rows {
		if r.ExternalTeam != "go-team" || r.AiteamTeam != "go-backend" {
			t.Fatal(r)
		}
	}
	before := rows[0]
	out.Reset()
	stderr.Reset()
	if code := router.Run(t.Context(), []string{"repo", "add", first.Dir}); code != ExitOK {
		t.Fatal(stderr)
	}
	rows = listRepos(t, paths)
	for _, r := range rows {
		if r.Path == before.Path && (r.ExternalTeam != before.ExternalTeam || r.WorktreeRoot != before.WorktreeRoot || r.BaseBranch != before.BaseBranch) {
			t.Fatal("re-registration changed association/settings", r)
		}
	}
	if code := router.Run(t.Context(), []string{"repo", "add", first.Dir, "--team", "missing"}); code == ExitOK {
		t.Fatal("unknown external team accepted")
	}
	if !strings.Contains(stderr.String(), "missing") {
		t.Fatal(stderr)
	}
}
