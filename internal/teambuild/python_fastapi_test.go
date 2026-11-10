package teambuild

import (
	"bytes"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/bugchecks"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/teampackage"
	"github.com/cloudvane/litefactory/internal/teamsource"
	"github.com/cloudvane/litefactory/internal/turn"
)

const pythonTeam = "python-fastapi"

func pythonProfile(t *testing.T) config.StackProfile {
	t.Helper()
	raw, err := os.ReadFile("../../teams/source/python-fastapi/stack-profile.json")
	if err != nil {
		t.Fatal(err)
	}
	p, err := config.DecodeStackProfile(raw)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPythonFastAPIBuildCarriesItsClosure(t *testing.T) {
	files, _, err := Build(t.Context(), repoSource(), pythonTeam)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"team.config", "stack-profile.json", "plan/skills/plan-python-fastapi-guidance/backend.md", "plan/skills/plan-python-fastapi-guidance/frontend.md", "implement/skills/python-fastapi-stack/supported-versions.md", "bug/skills/python-fastapi-stack/python-fastapi.md", "validate/skills/python-fastapi-stack/react-typescript-vite.md"} {
		if len(files[name]) == 0 {
			t.Fatal("missing built resource", name)
		}
	}
}

func TestPythonFastAPICaptureForBothProvidersAndReplay(t *testing.T) {
	source := repoSource()
	version := sourceVersion(t, pythonTeam)
	manager, installed := install(t, source, pythonTeam)
	retained := teamsource.RetainedFiles{}
	err := manager.WithCatalogue(t.Context(), func(access *teampackage.CatalogueAccess) error {
		return access.WithSelected(t.Context(), identity(source), pythonTeam, version, func(selected teampackage.Installed, files fs.FS) error {
			if !reflect.DeepEqual(selected.Identity, installed.Identity) || !selected.Integrity || len(selected.Workflows) != 6 {
				t.Fatal("exact linked selection lost release identity or integrity")
			}
			for _, f := range selected.Release.Files {
				if outOfGraphPath(f.Path) {
					continue
				}
				raw, err := fs.ReadFile(files, f.Path)
				if err != nil {
					return err
				}
				retained[f.Path] = raw
			}
			return nil
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	var files fs.FS = retained
	raw, err := fs.ReadFile(files, "stack-profile.json")
	if err != nil {
		t.Fatal(err)
	}
	profile, err := config.ResolveStackProfile(raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	guidance, err := teamsource.CaptureStackGuidance(t.Context(), files, profile.Effective)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := turn.ImplementSpecs()
	if err != nil {
		t.Fatal(err)
	}
	wf, err := fs.Sub(files, "implement")
	if err != nil {
		t.Fatal(err)
	}
	definition, err := teamsource.CaptureWorkflowIdentity(t.Context(), wf, "implement.dot", pythonTeam, agent.WorkflowImplement, registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := teamsource.AttachStackProfile(t.Context(), definition, profile, strings.Repeat("a", 40), guidance); err != nil {
		t.Fatal(err)
	}
	replay, err := teamsource.CaptureWorkflowIdentity(t.Context(), teamsource.RetainedFiles(definition.Files), "implement.dot", pythonTeam, agent.WorkflowImplement, registry)
	if err != nil {
		t.Fatal(err)
	}
	if definition.Digest != replay.Digest || !reflect.DeepEqual(definition.StackProfile, replay.StackProfile) {
		t.Fatal("cold replay changed capture")
	}
	if len(definition.Nodes) == 0 {
		t.Fatal("missing role inventory")
	}
	for id, node := range definition.Nodes {
		for _, provider := range []string{"claude", "codex"} {
			native, err := teamsource.NativeWorkflow(provider, node, "model", []string{"Read"}, "read-only")
			if err != nil {
				t.Fatal(id, provider, err)
			}
			delivered := map[string][]byte{}
			for _, f := range native {
				delivered[f.Path] = f.Bytes
			}
			for name, raw := range guidance {
				_, relative, ok := strings.Cut(name, "/skills/")
				if !ok {
					t.Fatal("bad guidance path", name)
				}
				if !bytes.Equal(delivered["plugin/skills/"+relative], raw) {
					t.Fatal("provider missing exact guidance", id, provider, name)
				}
			}
		}
	}
}

func TestPythonFastAPIProfileAndGuidanceBoundaries(t *testing.T) {
	p := pythonProfile(t)
	if !reflect.DeepEqual(p.Requirements.Executables, []string{"python3"}) || !reflect.DeepEqual(p.Requirements.OS, []string{"darwin"}) || !reflect.DeepEqual(p.Requirements.Arch, []string{"arm64", "amd64"}) {
		t.Fatal("host requirements drift")
	}
	raw, err := os.ReadFile("../../teams/source/python-fastapi/stack-profile.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, override := range []string{`{"version":1,"checks":[]}`, `{"version":1,"setup":[]}`, `{"version":1,"guidance":[]}`, `{"version":1,"environment":[]}`, `{"version":1,"checks":[{"name":"escape","argv":["node"],"cwd":"../outside","timeout_seconds":1}]}`} {
		if _, err := config.ResolveStackProfile(raw, []byte(override)); err == nil {
			t.Fatal("invalid override admitted", override)
		}
	}
	override := []byte(`{"version":1,"setup":[{"name":"repo-setup","argv":["python3","--version"],"cwd":".","timeout_seconds":60}],"checks":[{"name":"repo-check","argv":["npm","run","test","--","--run"],"cwd":"web","timeout_seconds":60}]}`)
	if _, err := config.ResolveStackProfile(raw, override); err != nil {
		t.Fatal("confined command override rejected", err)
	}
	repo, err := os.OpenRoot("../..")
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	files, err := teamsource.ResolveSource(t.Context(), repo, "teams/source/"+pythonTeam, pythonTeam)
	if err != nil {
		t.Fatal(err)
	}
	source := fstest.MapFS{}
	for name, raw := range files {
		source[name] = &fstest.MapFile{Data: raw}
	}
	guidance, err := teamsource.CaptureStackGuidance(t.Context(), source, p)
	if err != nil {
		t.Fatal(err)
	}
	for _, selected := range p.Guidance {
		if _, ok := guidance[selected.Skill]; !ok {
			t.Errorf("selected skill missing from captured closure: %s", selected.ID)
		}
	}
	for _, command := range append(p.Setup, p.Checks...) {
		if command.CWD != "." || command.Argv[0] != "python3" {
			t.Fatal("unconfined default", command.Name)
		}
		if command.TimeoutSeconds < 1 || command.TimeoutSeconds > 900 {
			t.Fatal("unbounded command", command.Name)
		}
		if command.Name == "backend-sync" && !strings.Contains(strings.Join(command.Argv, " "), "--no-python-downloads") {
			t.Fatal("automatic host download possible", command.Name)
		}
	}
}

// Dispatch fixtures exercise the embedded recipes, not copies of their selection logic.
func pythonRecipe(t *testing.T, root, path, name string, wantFailure bool) string {
	t.Helper()
	p := pythonProfile(t)
	for _, c := range append(p.Setup, p.Checks...) {
		if c.Name != name {
			continue
		}
		python, err := config.ResolveExecutable("fixture.python", "python3", os.Getenv("PATH"))
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(t.Context(), python.Path, c.Argv[1:]...)
		cmd.Dir = root
		cmd.Env = []string{"PATH=" + path, "HOME=" + root, "CI=1", "PYTHONDONTWRITEBYTECODE=1"}
		out, err := cmd.CombinedOutput()
		if (err != nil) != wantFailure {
			t.Fatalf("%s: %s, %v", name, out, err)
		}
		return string(out)
	}
	t.Fatal("missing recipe", name)
	return ""
}

func pythonRecorder(t *testing.T, root, name string, fail bool) {
	t.Helper()
	python, err := config.ResolveExecutable("fixture.python", "python3", os.Getenv("PATH"))
	if err != nil {
		t.Fatal(err)
	}
	code := "#!" + python.Path + "\nimport os,sys\nprint(os.getcwd()+' ARGV '+repr(sys.argv[1:]), flush=True)\n"
	if fail {
		code += "sys.exit(7)\n"
	}
	path := filepath.Join(root, name)
	put(t, path, []byte(code))
	if err := os.Chmod(path, 0700); err != nil {
		t.Fatal(err)
	}
}

func TestPythonFastAPIRecipeSelection(t *testing.T) {
	for _, tt := range []struct {
		name         string
		files        map[string]string
		recipe, want string
	}{
		{"root locked", map[string]string{"pyproject.toml": "[project]\nname='fixture'\n", "uv.lock": "version=1"}, "backend-sync", "'sync', '--locked', '--all-groups', '--no-python-downloads'"},
		{"backend fallback", map[string]string{"backend/pyproject.toml": "[project]\nname='fixture'\n", "backend/uv.lock": "version=1"}, "backend-sync", "/backend ARGV ['sync'"},
		{"root precedence", map[string]string{"requirements.txt": "", "backend/pyproject.toml": "[project]\n", "backend/uv.lock": ""}, "backend-sync", "'-m', 'pip', 'install'"},
		{"unlocked pyproject", map[string]string{"pyproject.toml": "[project]\nname='fixture'\n"}, "backend-sync", "'.']"},
		{"requirements preferred unlocked", map[string]string{"pyproject.toml": "[project]\n", "requirements.txt": ""}, "backend-sync", "'-r'"},
		{"pytest config", map[string]string{"requirements.txt": "", "pytest.ini": "[pytest]\ntestpaths=specs\n"}, "backend-tests", "ARGV ['-m', 'pytest']"},
		{"ruff toml", map[string]string{"requirements.txt": "", "ruff.toml": ""}, "backend-format", "'ruff', 'format', '--check', '.'"},
		{"ruff project", map[string]string{"pyproject.toml": "[tool.ruff]\n"}, "backend-lint", "'ruff', 'check', '.'"},
		{"mypy project", map[string]string{"pyproject.toml": "[tool.mypy]\n"}, "backend-types", "'mypy', '.'"},
		{"mypy ini", map[string]string{"requirements.txt": "", "mypy.ini": "[mypy]\n"}, "backend-types", "'mypy', '.'"},
		{"mypy setup", map[string]string{"requirements.txt": "", "setup.cfg": "[mypy]\n"}, "backend-types", "'mypy', '.'"},
		{"frontend root", map[string]string{"package.json": "{}"}, "frontend-ci", "ARGV ['install', '--engine-strict', '--ignore-scripts'"},
		{"frontend locked", map[string]string{"frontend/package.json": "{}", "frontend/package-lock.json": "{}"}, "frontend-ci", "/frontend ARGV ['ci'"},
		{"client script", map[string]string{"client/package.json": `{"scripts":{"test":"node test.js"}}`}, "frontend-test", "/client ARGV ['run', 'test']"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := canonicalTemp(t)
			for name, raw := range tt.files {
				put(t, filepath.Join(root, name), []byte(raw))
			}
			for _, name := range []string{"bin/uv", "bin/npm", "bin/node", ".venv/bin/python", "backend/.venv/bin/python"} {
				pythonRecorder(t, root, name, false)
			}
			out := pythonRecipe(t, root, filepath.Join(root, "bin"), tt.recipe, false)
			if !strings.Contains(out, tt.want) {
				t.Fatalf("want %q in %s", tt.want, out)
			}
		})
	}
}

func TestPythonFastAPIOptionalChecksAndFailures(t *testing.T) {
	root := canonicalTemp(t)
	put(t, filepath.Join(root, "requirements.txt"), nil)
	for _, dir := range []string{"node_modules/pkg", ".venv/lib/pkg", ".git", "dist", "build", ".cache"} {
		put(t, filepath.Join(root, dir, "package.json"), []byte(`{"scripts":{"test":"false"}}`))
	}
	for _, name := range []string{"backend-format", "backend-lint", "backend-types", "frontend-ci", "frontend-test", "frontend-build"} {
		if out := pythonRecipe(t, root, root, name, false); out != "" {
			t.Fatal("optional tool launched", out)
		}
	}
	for _, name := range []string{"backend-tests", "backend-sync"} {
		pythonRecipe(t, root, root, name, true)
	}
	pythonRecorder(t, root, ".venv/bin/python", true)
	put(t, filepath.Join(root, "pyproject.toml"), []byte("[tool.ruff]\n[tool.mypy]\n"))
	for _, name := range []string{"backend-format", "backend-lint", "backend-types", "backend-tests", "backend-sync"} {
		pythonRecipe(t, root, root, name, true)
	}
	put(t, filepath.Join(root, "client/package.json"), []byte(`{"scripts":{"typecheck":"false","lint":"false","test":"false","build":"false"}}`))
	if out := pythonRecipe(t, root, root, "frontend-ci", true); !strings.Contains(out, "Missing required executable: node") {
		t.Fatal(out)
	}
	pythonRecorder(t, root, "bin/node", false)
	pythonRecorder(t, root, "bin/npm", true)
	for _, name := range []string{"frontend-ci", "frontend-typecheck", "frontend-lint", "frontend-test", "frontend-build"} {
		pythonRecipe(t, root, filepath.Join(root, "bin"), name, true)
	}
}

func TestPythonFastAPISelectedPathsStayConfined(t *testing.T) {
	for _, name := range []string{"requirements.txt", "backend", ".venv", ".cache", "client", "package.json"} {
		t.Run(name, func(t *testing.T) {
			root, outside := canonicalTemp(t), canonicalTemp(t)
			if name != "requirements.txt" && name != "backend" {
				put(t, filepath.Join(root, "requirements.txt"), nil)
			}
			target := outside
			if name == "requirements.txt" || name == "package.json" {
				target = filepath.Join(outside, name)
				put(t, target, []byte("{}"))
			}
			if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
				t.Fatal(err)
			}
			recipe := "backend-sync"
			if name == "client" || name == "package.json" {
				recipe = "frontend-ci"
			}
			if out := pythonRecipe(t, root, root, recipe, true); !strings.Contains(out, "escapes worktree") {
				t.Fatal(out)
			}
		})
	}
}

func TestPythonFastAPIAdmissionWithoutComponentsOrTools(t *testing.T) {
	root := canonicalTemp(t)
	python, err := config.ResolveExecutable("fixture.python", "python3", os.Getenv("PATH"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../teams/source/python-fastapi/stack-profile.json")
	if err != nil {
		t.Fatal(err)
	}
	p, err := config.ResolveStackProfile(raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	isolation := bugchecks.Isolation{Binary: python.Path, PATH: filepath.Dir(python.Path), Home: root, Revision: "fixture"}
	if err := isolation.ObserveStackCommands(root, &p); err != nil {
		t.Fatal(err)
	}
}

func TestPythonFastAPIVersionDocumentationMatchesRecipes(t *testing.T) {
	doc, err := os.ReadFile("../../teams/source/python-fastapi/plan/skills/plan-python-fastapi-guidance/supported-versions.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"implement", "bug", "validate"} {
		other, err := os.ReadFile("../../teams/source/python-fastapi/" + mode + "/skills/python-fastapi-stack/supported-versions.md")
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(doc, other) {
			t.Fatal("version documents disagree", mode)
		}
	}
	for _, c := range append(pythonProfile(t).Setup, pythonProfile(t).Checks...) {
		for _, pin := range []string{"3.14.0", "24.16.0", "0.10.12", "11.13.0"} {
			if strings.Contains(strings.Join(c.Argv, " "), pin) {
				t.Fatal("universal exact pin", pin)
			}
		}
	}
}
