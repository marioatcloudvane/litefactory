package cli

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/teamsource/teamsourcetest"
)

func teamFiles(t *testing.T, modes ...string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	var mapping strings.Builder
	for _, mode := range modes {
		resolved, err := teamsourcetest.Files(t.Context(), teamsourcetest.Go)
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

// teamSourceCopy copies the repository's base-team and go-development source
// folders into dir/teams/source, optionally at another release version, and
// returns that teams/source folder.
func teamSourceCopy(t *testing.T, dir, version string) string {
	t.Helper()
	from := filepath.Join(teamsourcetest.RepoRoot(), "teams", "source")
	to := filepath.Join(dir, "teams", "source")
	for _, team := range []string{"base-team", teamsourcetest.Go} {
		err := filepath.WalkDir(filepath.Join(from, team), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, err := filepath.Rel(from, p)
			if err != nil {
				return err
			}
			raw, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			if version != "" && rel == filepath.Join(teamsourcetest.Go, "release.json") {
				raw = bytes.Replace(raw, []byte(`"version": "1.0.0"`), []byte(`"version": "`+version+`"`), 1)
			}
			target := filepath.Join(to, rel)
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return err
			}
			return os.WriteFile(target, raw, 0o600)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return to
}

// changeTeamSource edits one installed file of the go-development team.
func changeTeamSource(t *testing.T, source, line string) {
	t.Helper()
	name := filepath.Join(source, "base-team", "research", "skills", "research-evidence", "SKILL.md")
	raw, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, append(raw, []byte("\n"+line+"\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestTeamCommandsOutsideRepository(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.EnvHome, home)
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	source := teamSourceCopy(t, filepath.Join(home, "checkout"), "")
	identity := "file://" + filepath.ToSlash(source)
	t.Chdir(home)
	t.Setenv("PATH", home)
	// The configured folder is read without resolving bot credentials or opening SQL.
	body := []byte("settings:\n  team_source: " + source + "\n")
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	env := &Env{Stdout: &stdout, Stderr: &stderr, Logger: slog.New(slog.DiscardHandler)}
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
	refused := func(want string, args ...string) {
		t.Helper()
		stdout.Reset()
		stderr.Reset()
		if code := router.Run(t.Context(), args); code == ExitOK || !strings.Contains(stderr.String(), want) {
			t.Fatalf("%v: exit %d: %s", args, code, stderr.String())
		}
	}
	installed := run("team", "install", "go-development")
	for _, want := range []string{"Installed go-development 1.0.0 from " + identity, "Checksum: ", "Source commit: unknown"} {
		if !strings.Contains(installed, want) {
			t.Fatalf("install missing %q: %s", want, installed)
		}
	}
	// An explicit folder overrides an old catalogue URL left in config.yaml.
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("settings:\n  team_source: https://unused.example/teams.json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if output := run("team", "install", "go-development", "--source", source+"/"); !strings.Contains(output, "Already installed") {
		t.Fatal(output)
	}
	folderForm := "--source /absolute/path/to/teams/source"
	refused(folderForm, "team", "install", "go-development")
	refused(folderForm, "team", "install", "go-development", "--source", "https://raw.githubusercontent.com/example/repo/main/teams/teams.json")
	catalogue := filepath.Join(home, "teams.json")
	if err := os.WriteFile(catalogue, []byte(`{"schema":1,"releases":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	refused(folderForm, "team", "install", "go-development", "--source", catalogue)
	refused(folderForm, "team", "install", "go-development", "--source", "file://"+filepath.ToSlash(catalogue))
	refused("base", "team", "install", "base-team", "--source", source)
	refused("no team folder", "team", "install", "missing", "--source", source)
	// A changed source replaces the installed version in place.
	changeTeamSource(t, source, "Installed again after an edit.")
	if output := run("team", "install", "go-development", "--source", source); !strings.Contains(output, "Replaced go-development 1.0.0") {
		t.Fatal(output)
	}
	// Offline commands ignore even a now-invalid configuration.
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("broken: config"), 0o600); err != nil {
		t.Fatal(err)
	}

	cloned := run("team", "clone", "go-development", "1.0.0", "custom", "2.0.0", "--source", identity)
	for _, want := range []string{"Cloned custom 2.0.0", "Editable folder:", "Origin: " + identity + " go-development 1.0.0"} {
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
	if !strings.Contains(localInspect, "Source: local, editable") || !strings.Contains(localInspect, "Origin: "+identity+" go-development 1.0.0") {
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
		{"team", "update", "go-development"},
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
	inspected := run("team", "inspect", "go-development", "1.0.0", "--source", identity)
	for _, want := range []string{"Integrity: verified", "Source: " + identity, "Checksum:", "Path:", "bug, implement, plan, research, spike, validate", "Source commit:"} {
		if !strings.Contains(inspected, want) {
			t.Fatalf("inspect missing %q: %s", want, inspected)
		}
	}
	if output := run("team", "remove", "go-development", "1.0.0"); !strings.Contains(output, "Removed") {
		t.Fatal(output)
	}
	if output := run("team", "list"); strings.Contains(output, "1.0.0") {
		t.Fatal(output)
	}
	if _, err := os.Stat(filepath.Join(home, "litefactory.db")); !os.IsNotExist(err) {
		t.Fatalf("SQL created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "daemon.lock")); !os.IsNotExist(err) {
		t.Fatalf("daemon accessed: %v", err)
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
				local["research/research.dot"] = bytes.ReplaceAll(local["research/research.dot"], []byte(`"lf_comment"`), []byte(`"unsupported_comment"`))
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
	var stdout, stderr bytes.Buffer
	env := &Env{Stdout: &stdout, Stderr: &stderr, Logger: slog.New(slog.DiscardHandler)}
	router := NewRouter("litefactory", env, Commands())
	code := router.Run(t.Context(), []string{"team", "install", "go-development"})
	if code != ExitFailure || !strings.Contains(stderr.String(), "team_source") {
		t.Fatalf("truncated source setting: exit=%d error=%s", code, stderr.String())
	}
}
