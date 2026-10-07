package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teampackage"
)

func TestTeamBindingCommandsOfflineLifecycle(t *testing.T) {
	paths := newRepoHome(t)
	canonicalHome, err := filepath.EvalSymlinks(paths.Home())
	if err != nil {
		t.Fatal(err)
	}
	paths = config.NewPaths(canonicalHome)
	t.Setenv(config.EnvHome, canonicalHome)
	clone := newRepoFixture(t)
	router, _, stderr := newTestRouter(t)
	if code := router.Run(t.Context(), []string{"repo", "add", clone.Dir}); code != ExitOK {
		t.Fatal(code, stderr.String())
	}
	db, err := openTeamBindingDB(t.Context(), paths)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	repo, err := db.GetRepoByPath(t.Context(), clone.Dir)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(clone.Dir, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	files := teamFiles(t, "research")
	r1 := cliRelease(files, "1.0.0")
	r2 := cliRelease(files, "2.0.0")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath := strings.TrimPrefix(r.URL.Path, "/alias")
		if requestPath == "/teams.json" {
			if err := json.NewEncoder(w).Encode(map[string]any{"schema": 1, "releases": []any{r1, r2}}); err != nil {
				t.Error(err)
			}
			return
		}
		for _, release := range []string{"1.0.0", "2.0.0"} {
			prefix := "/standard/" + release + "/"
			if strings.HasPrefix(requestPath, prefix) {
				raw, ok := files[strings.TrimPrefix(requestPath, prefix)]
				if ok {
					if _, err := w.Write(raw); err != nil {
						t.Error(err)
					}
					return
				}
			}
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	source := server.URL + "/teams.json"
	aliasSource := server.URL + "/alias/teams.json"
	var output, diagnostic bytes.Buffer
	env := &Env{Stdout: &output, Stderr: &diagnostic, Logger: slog.New(slog.DiscardHandler), TeamTransport: server.Client().Transport}
	commands := NewRouter("litefactory", env, Commands())
	run := func(want int, args ...string) string {
		t.Helper()
		output.Reset()
		diagnostic.Reset()
		code := commands.Run(t.Context(), args)
		if code != want {
			t.Fatalf("%v: exit %d want %d; %s", args, code, want, diagnostic.String())
		}
		return output.String() + diagnostic.String()
	}
	run(ExitOK, "team", "install", "standard", "1.0.0", "--source", source)
	run(ExitOK, "team", "install", "standard", "2.0.0", "--source", source)
	run(ExitOK, "team", "install", "standard", "1.0.0", "--source", aliasSource)
	server.Close()
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	// Link/unlink do not read runner configuration or resolve credentials.
	if err := os.Remove(paths.ConfigPath()); err != nil {
		t.Fatal(err)
	}
	if out := run(ExitFailure, "team", "link", "standard", "1.0.0", "--repo", clone.Dir); !strings.Contains(out, source) || !strings.Contains(out, aliasSource) || !strings.Contains(out, "ambiguous") {
		t.Fatal(out)
	}
	if _, err := db.GetRepositoryTeamBinding(t.Context(), repo.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	out := run(ExitOK, "team", "link", "standard", "1.0.0", "--repo", clone.Dir, "--source", source)
	if !strings.Contains(out, repo.Path) || !strings.Contains(out, source) || !strings.Contains(out, "standard 1.0.0") {
		t.Fatal(out)
	}
	if out := run(ExitOK, "team", "link", "standard", "1.0.0", "--repo", clone.Dir, "--source", source); !strings.Contains(out, "Already linked") {
		t.Fatal(out)
	}
	binding, err := db.GetRepositoryTeamBinding(t.Context(), repo.ID)
	if err != nil || binding.ReleaseDigest != r1.Checksum {
		t.Fatal(binding, err)
	}
	// Failed linking preserves the prior binding.
	run(ExitFailure, "team", "link", "standard", "9.0.0", "--repo", clone.Dir)
	if got, err := db.GetRepositoryTeamBinding(t.Context(), repo.ID); err != nil || got != binding {
		t.Fatal(got, err)
	}
	if out := run(ExitFailure, "team", "link", "standard", "1.0.0", "--repo", clone.Dir); !strings.Contains(out, "ambiguous") {
		t.Fatal(out)
	}
	if got, err := db.GetRepositoryTeamBinding(t.Context(), repo.ID); err != nil || got != binding {
		t.Fatal(got, err)
	}
	run(ExitOK, "team", "link", "standard", "1.0.0", "--repo", clone.Dir, "--source", aliasSource)
	if got, err := db.GetRepositoryTeamBinding(t.Context(), repo.ID); err != nil || got.Source != aliasSource {
		t.Fatal(got, err)
	}
	run(ExitOK, "team", "link", "standard", "2.0.0", "--repo", clone.Dir)
	// A nested current directory resolves to the registered top level.
	sub := filepath.Join(clone.Dir, "nested")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	if out := run(ExitOK, "team", "link", "standard", "2.0.0"); !strings.Contains(out, "Already linked") {
		t.Fatal(out)
	}
	if out := run(ExitFailure, "team", "remove", "standard", "2.0.0"); !strings.Contains(out, repo.Path) || !strings.Contains(out, "unlink") {
		t.Fatal(out)
	}
	run(ExitOK, "team", "remove", "standard", "1.0.0", "--source", source)
	run(ExitOK, "team", "remove", "standard", "1.0.0", "--source", aliasSource)
	if out := run(ExitOK, "team", "unlink"); !strings.Contains(out, "standard 2.0.0") || !strings.Contains(out, repo.Path) {
		t.Fatal(out)
	}
	if out := run(ExitOK, "team", "unlink", "--repo", clone.Dir); !strings.Contains(out, "already unlinked") {
		t.Fatal(out)
	}
	run(ExitOK, "team", "remove", "standard", "2.0.0")
	if _, err := db.GetRepositoryTeamBinding(t.Context(), repo.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(clone.Dir, "CLAUDE.md"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("checkout changed", err)
	}
	unregistered := newRepoFixture(t)
	if out := run(ExitPrecondition, "team", "link", "standard", "1.0.0", "--repo", unregistered.Dir); !strings.Contains(out, "repo add") {
		t.Fatal(out)
	}
}

func TestTeamRemovalGuardAbsentAndInvalidDatabase(t *testing.T) {
	t.Parallel()
	paths := config.NewPaths(t.TempDir())
	guard := teamRemovalGuard(paths)
	if uses, err := guard(t.Context(), teampackage.Identity{Source: "https://example.com/teams.json", Slug: "standard", Version: "1.0.0"}); err != nil || len(uses) != 0 {
		t.Fatal(uses, err)
	}
	if err := os.WriteFile(paths.DBPath(), []byte("invalid database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := guard(t.Context(), teampackage.Identity{Source: "https://example.com/teams.json", Slug: "standard", Version: "1.0.0"}); err == nil || errors.Is(err, fs.ErrNotExist) {
		t.Fatal("invalid database allowed removal", err)
	}
}
