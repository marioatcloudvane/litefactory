package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/cloudvane/litefactory/internal/teamsource/teamsourcetest"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/teampackage"
)

func teamFiles(t *testing.T, modes ...string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	var mapping strings.Builder
	for _, mode := range modes {
		resolved, err := teamsourcetest.Files(teamsourcetest.Go)
		if err != nil {
			t.Fatal(err)
		}
		for name, raw := range resolved {
			if strings.HasPrefix(name, mode+"/") {
				files[name] = raw
			}
		}
		mapping.WriteString(mode + ": " + mode + "/" + mode + ".dot\n")
	}
	files["team.config"] = []byte(mapping.String())
	if len(modes) == 0 {
		t.Fatal("team fixture requires a workflow")
	}
	skillPath := modes[0] + "/skills/fixture-stack/SKILL.md"
	profile, err := json.Marshal(map[string]any{"version": 1, "guidance": []config.StackGuidance{{ID: "fixture-stack", Skill: skillPath}}})
	if err != nil {
		t.Fatal(err)
	}
	files[config.StackProfilePath] = profile
	files[skillPath] = []byte("---\nname: fixture-stack\ndescription: Synthetic team fixture guidance\n---\nAdvice.\n")
	return files
}
func cliRelease(files map[string][]byte, ver string) teampackage.Release {
	r := teampackage.Release{Name: "Standard", Slug: "standard", Version: ver, Description: "unchanged builtin fixture", Tags: []string{}, Download: "standard/" + ver}
	for name, raw := range files {
		sum := sha256.Sum256(raw)
		r.Files = append(r.Files, teampackage.File{Path: name, SHA256: hex.EncodeToString(sum[:])})
	}
	slices.SortFunc(r.Files, func(a, b teampackage.File) int { return strings.Compare(a.Path, b.Path) })
	h := sha256.New()
	for _, f := range r.Files {
		_, _ = h.Write([]byte(f.Path + "\x00" + f.SHA256 + "\n"))
	}
	r.Checksum = hex.EncodeToString(h.Sum(nil))
	return r
}
func TestTeamCommandsOutsideRepository(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.EnvHome, home)
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	files := teamFiles(t, "research", "plan", "implement", "validate", "spike", "bug")
	t.Chdir(home)
	t.Setenv("PATH", home)
	var mu sync.Mutex
	versions := []string{"1.0.0"}
	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		requests++
		if r.URL.Path == "/teams/teams.json" {
			c := teampackage.Catalogue{Schema: 1}
			for _, ver := range versions {
				c.Releases = append(c.Releases, cliRelease(files, ver))
			}
			if err := json.NewEncoder(w).Encode(c); err != nil {
				t.Error(err)
			}
			return
		}
		parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/teams/standard/"), "/", 2)
		if len(parts) != 2 {
			http.NotFound(w, r)
			return
		}
		raw, ok := files[parts[1]]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if _, err := w.Write(raw); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	source := server.URL + "/teams/teams.json"
	// Configured source is read without resolving bot credentials or opening SQL.
	body := []byte("settings:\n  team_source: " + source + "\n")
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	env := &Env{Stdout: &stdout, Stderr: &stderr, Logger: slog.New(slog.DiscardHandler), TeamTransport: server.Client().Transport}
	router := NewRouter("litefactory", env, Commands())
	run := func(args ...string) string {
		t.Helper()
		stdout.Reset()
		stderr.Reset()
		if code := router.Run(t.Context(), args); code != ExitOK {
			t.Fatalf("%v exit %d: %s", args, code, stderr.String())
		}
		return stdout.String()
	}
	if output := run("team", "install", "standard"); !strings.Contains(output, "Installed standard 1.0.0") {
		t.Fatal(output)
	}
	// An explicit source overrides another configured catalogue without fetching it.
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("settings:\n  team_source: https://unused.example/teams.json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if output := run("team", "install", "standard", "1.0.0", "--source", source); !strings.Contains(output, "Already installed") {
		t.Fatal(output)
	}
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	versions = append(versions, "1.1.1")
	mu.Unlock()
	if output := run("team", "update", "standard", "--source", source); !strings.Contains(output, "Installed update") {
		t.Fatal(output)
	}
	if output := run("team", "update", "standard"); !strings.Contains(output, "No update") {
		t.Fatal(output)
	}
	mu.Lock()
	priorRequests := requests
	mu.Unlock()
	server.Close()
	// Offline commands ignore even a now-invalid configuration.
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("broken: config"), 0o600); err != nil {
		t.Fatal(err)
	}

	cloned := run("team", "clone", "standard", "1.0.0", "custom", "2.0.0", "--source", source)
	for _, want := range []string{"Cloned custom 2.0.0", "Editable folder:", "Origin: " + source + " standard 1.0.0"} {
		if !strings.Contains(cloned, want) {
			t.Fatal(cloned)
		}
	}
	localPath := filepath.Join(config.NewPaths(home).TeamsInstalledDir(), "local", "custom", "2.0.0", "content")
	// A graph edit that factory capture would reject is valid editable content.
	if err := os.WriteFile(filepath.Join(localPath, "research", "research.dot"), []byte("editable graph bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	localInspect := run("team", "inspect", "custom", "2.0.0", "--source", "local")
	if !strings.Contains(localInspect, "Source: local, editable") || !strings.Contains(localInspect, "Origin: "+source+" standard 1.0.0") {
		t.Fatal(localInspect)
	}
	if output := run("team", "list"); !strings.Contains(output, "local custom 2.0.0") {
		t.Fatal(output)
	}
	run("team", "clone", "custom", "2.0.0", "second", "3.0.0", "--source", "local")
	for _, args := range [][]string{
		{"team", "clone", "missing", "1.0.0", "new", "1.0.0"},
		{"team", "clone", "custom", "2.0.0", "second", "3.0.0"},
		{"team", "clone", "custom", "2.0.0", "Bad", "1.0.0"},
		{"team", "clone", "custom", "2.0.0", "new", "v1"},
		{"team", "update", "custom", "--source", "local"},
	} {
		stdout.Reset()
		stderr.Reset()
		if code := router.Run(t.Context(), args); code == ExitOK {
			t.Fatalf("accepted %v", args)
		}
	}
	run("team", "remove", "custom", "2.0.0", "--source", "local")
	if output := run("team", "inspect", "second", "3.0.0"); !strings.Contains(output, "Origin: local custom 2.0.0") {
		t.Fatal(output)
	}
	run("team", "remove", "second", "3.0.0", "--source", "local")
	listed := run("team", "list")
	if !strings.Contains(listed, "1.0.0") || !strings.Contains(listed, "1.1.1") {
		t.Fatal(listed)
	}
	inspected := run("team", "inspect", "standard", "1.0.0", "--source", source)
	for _, want := range []string{"Integrity: verified", "Source: " + source, "Checksum:", "Path:", "bug, implement, plan, research, spike, validate"} {
		if !strings.Contains(inspected, want) {
			t.Fatalf("inspect missing %q: %s", want, inspected)
		}
	}
	if output := run("team", "remove", "standard", "1.0.0"); !strings.Contains(output, "Removed") {
		t.Fatal(output)
	}
	if output := run("team", "list"); strings.Contains(output, "1.0.0") || !strings.Contains(output, "1.1.1") {
		t.Fatal(output)
	}
	if _, err := os.Stat(filepath.Join(home, "litefactory.db")); !os.IsNotExist(err) {
		t.Fatalf("SQL created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "daemon.lock")); !os.IsNotExist(err) {
		t.Fatalf("daemon accessed: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if requests != priorRequests {
		t.Fatal("offline commands fetched")
	}
}
func TestTeamWorkflowValidationRefusesUnknownActionAndMissingResource(t *testing.T) {
	t.Parallel()
	files := teamFiles(t, "research")
	if err := validateTeamPackage(t.Context(), byteFiles(files), map[string]string{"research": "research/research.dot"}); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []string{"action", "missing", "mode"} {
		t.Run(mutation, func(t *testing.T) {
			local := map[string][]byte{}
			for p, raw := range files {
				local[p] = bytes.Clone(raw)
			}
			switch mutation {
			case "action":
				local["research/research.dot"] = bytes.ReplaceAll(local["research/research.dot"], []byte("lf_research_"), []byte("unsupported_"))
			case "missing":
				for p := range local {
					if strings.HasSuffix(p, "SKILL.md") {
						delete(local, p)
					}
				}
			case "mode":
				local["research/research.dot"] = bytes.ReplaceAll(local["research/research.dot"], []byte(`mode="research"`), []byte(`mode="implement"`))
			}
			if err := validateTeamPackage(t.Context(), byteFiles(local), map[string]string{"research": "research/research.dot"}); err == nil {
				t.Fatalf("accepted %s", mutation)
			}
		})
	}
}

func byteFiles(files map[string][]byte) fstest.MapFS {
	result := fstest.MapFS{}
	for p, raw := range files {
		result[p] = &fstest.MapFile{Data: raw, Mode: 0o600}
	}
	return result
}

type teamSourceRequestSpy struct{ called bool }

func (s *teamSourceRequestSpy) RoundTrip(*http.Request) (*http.Response, error) {
	s.called = true
	return nil, errors.New("unexpected source request")
}

func TestTeamSourceSettingsCannotBeSilentlyTruncated(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.EnvHome, home)
	raw := "#" + strings.Repeat("padding", (1<<20)/7+1) + "\nsettings:\n  team_source: http://invalid.example/teams.json\n"
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	spy := &teamSourceRequestSpy{}
	var stdout, stderr bytes.Buffer
	env := &Env{Stdout: &stdout, Stderr: &stderr, Logger: slog.New(slog.DiscardHandler), TeamTransport: spy}
	router := NewRouter("litefactory", env, Commands())
	code := router.Run(t.Context(), []string{"team", "install", "standard"})
	if code != ExitFailure || !strings.Contains(stderr.String(), "team_source") || spy.called {
		t.Fatalf("truncated source setting: exit=%d error=%s fetched=%v", code, stderr.String(), spy.called)
	}
}
