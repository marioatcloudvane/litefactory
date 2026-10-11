package teamrelease

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/teampackage"
	"github.com/cloudvane/litefactory/internal/teamsource"
	"github.com/cloudvane/litefactory/internal/turn"
)

const pythonSource = "internal/teamsource/python-fastapi"

func pythonFixture(t *testing.T) string {
	t.Helper()
	root := fixture(t)
	for name, raw := range snapshot(t, "../teamsource/python-fastapi") {
		put(t, filepath.Join(root, pythonSource, filepath.FromSlash(name)), raw)
	}
	return root
}

func pythonProfile(t *testing.T) config.StackProfile {
	t.Helper()
	raw, err := os.ReadFile("../teamsource/python-fastapi/stack-profile.json")
	if err != nil {
		t.Fatal(err)
	}
	p, err := config.DecodeStackProfile(raw)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPythonFastAPIReleasePreservesHistoryAndClosure(t *testing.T) {
	root := pythonFixture(t)
	generate(t, root)
	history := snapshot(t, filepath.Join(root, "teams"))
	old, err := os.ReadFile(filepath.Join(root, "teams/teams.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := GenerateSources(t.Context(), root, false, []string{pythonSource}); err != nil {
		t.Fatal(err)
	}
	after := snapshot(t, filepath.Join(root, "teams"))
	for name, raw := range history {
		if name != "teams.json" && !bytes.Equal(after[name], raw) {
			t.Fatal("historical release changed", name)
		}
	}
	var beforeCatalogue, afterCatalogue struct {
		Releases []json.RawMessage `json:"releases"`
	}
	if err := json.Unmarshal(old, &beforeCatalogue); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(after["teams.json"], &afterCatalogue); err != nil {
		t.Fatal(err)
	}
	if len(afterCatalogue.Releases) != len(beforeCatalogue.Releases)+1 || !bytes.Equal(beforeCatalogue.Releases[0], afterCatalogue.Releases[0]) {
		t.Fatal("existing catalogue entry changed")
	}
	if err := GenerateSources(t.Context(), root, true, []string{pythonSource}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, snapshot(t, filepath.Join(root, "teams"))) {
		t.Fatal("check mode wrote release")
	}
	current := "python-fastapi/v" + sourceVersion(t, "../teamsource/python-fastapi") + "/"
	for _, w := range workflows {
		original, err := os.ReadFile(filepath.Join("../teamsource", w, w+".dot"))
		if err != nil {
			t.Fatal(err)
		}
		want := bytes.ReplaceAll(original, []byte(`team="go-development"`), []byte(`team="python-fastapi"`))
		want = bytes.ReplaceAll(want, []byte(",litefactory-rules"), []byte(",python-fastapi-stack"))
		if !bytes.Equal(after[current+w+"/"+w+".dot"], want) {
			t.Fatal("workflow contract changed", w)
		}
	}
	for _, name := range []string{"team.config", "stack-profile.json", "plan/skills/plan-python-fastapi-guidance/backend.md", "plan/skills/plan-python-fastapi-guidance/frontend.md", "implement/skills/python-fastapi-stack/supported-versions.md", "bug/skills/python-fastapi-stack/python-fastapi.md", "validate/skills/python-fastapi-stack/react-typescript-vite.md"} {
		if len(after[current+name]) == 0 {
			t.Fatal("missing packaged resource", name)
		}
	}
}

func TestPythonFastAPICaptureForBothProvidersAndReplay(t *testing.T) {
	root := pythonFixture(t)
	if err := GenerateSources(t.Context(), root, false, []string{pythonSource}); err != nil {
		t.Fatal(err)
	}
	manager, err := teampackage.New(teampackage.Options{Paths: config.NewPaths(canonicalTemp(t)), Validate: turn.ValidateTeamWorkflows})
	if err != nil {
		t.Fatal(err)
	}
	catalogue := filepath.Join(root, "teams/teams.json")
	version := sourceVersion(t, "../teamsource/python-fastapi")
	installed, _, err := manager.Install(t.Context(), catalogue, "python-fastapi", version)
	if err != nil {
		t.Fatal(err)
	}
	retained := teamsource.RetainedFiles{}
	err = manager.WithCatalogue(t.Context(), func(access *teampackage.CatalogueAccess) error {
		return access.WithSelected(t.Context(), catalogue, "python-fastapi", version, func(selected teampackage.Installed, files fs.FS) error {
			if !reflect.DeepEqual(selected.Identity, installed.Identity) || !selected.Integrity || len(selected.Workflows) != 6 {
				t.Fatal("exact linked selection lost release identity or integrity")
			}
			for _, f := range selected.Release.Files {
				if f.Path == "GENERATED.md" {
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
	var source fs.FS = retained
	raw, err := fs.ReadFile(source, "stack-profile.json")
	if err != nil {
		t.Fatal(err)
	}
	profile, err := config.ResolveStackProfile(raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	guidance, err := teamsource.CaptureStackGuidance(t.Context(), source, profile.Effective)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := turn.ImplementSpecs()
	if err != nil {
		t.Fatal(err)
	}
	wf, err := fs.Sub(source, "implement")
	if err != nil {
		t.Fatal(err)
	}
	definition, err := teamsource.CaptureWorkflowIdentity(t.Context(), wf, "implement.dot", "python-fastapi", agent.WorkflowImplement, registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := teamsource.AttachStackProfile(t.Context(), definition, profile, strings.Repeat("a", 40), guidance); err != nil {
		t.Fatal(err)
	}
	replay, err := teamsource.CaptureWorkflowIdentity(t.Context(), teamsource.RetainedFiles(definition.Files), "implement.dot", "python-fastapi", agent.WorkflowImplement, registry)
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
			files, err := teamsource.NativeWorkflow(provider, node, "model", []string{"Read"}, "read-only")
			if err != nil {
				t.Fatal(id, provider, err)
			}
			delivered := map[string][]byte{}
			for _, f := range files {
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
	if !reflect.DeepEqual(p.Requirements.Executables, []string{"python3", "node", "uv", "npm"}) || !reflect.DeepEqual(p.Requirements.OS, []string{"darwin"}) || !reflect.DeepEqual(p.Requirements.Arch, []string{"arm64", "amd64"}) {
		t.Fatal("host requirements drift")
	}
	raw, err := os.ReadFile("../teamsource/python-fastapi/stack-profile.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, override := range []string{`{"version":1,"checks":[]}`, `{"version":1,"setup":[]}`, `{"version":1,"guidance":[]}`, `{"version":1,"environment":[]}`, `{"version":1,"checks":[{"name":"escape","argv":["node"],"cwd":"../outside","timeout_seconds":1}]}`} {
		if _, err := config.ResolveStackProfile(raw, []byte(override)); err == nil {
			t.Fatal("invalid override admitted", override)
		}
	}
	override := []byte(`{"version":1,"checks":[{"name":"repo-check","argv":["npm","run","test","--","--run"],"cwd":"web","timeout_seconds":60}]}`)
	if _, err := config.ResolveStackProfile(raw, override); err != nil {
		t.Fatal("confined command override rejected", err)
	}
	source := os.DirFS("../teamsource/python-fastapi")
	guidance, err := teamsource.CaptureStackGuidance(t.Context(), source, p)
	if err != nil {
		t.Fatal(err)
	}
	combined := []byte{}
	for _, raw := range guidance {
		combined = append(combined, raw...)
	}
	for _, topic := range []string{"request", "response", "lifespan", "blocking", "error", "pytest", "OpenAPI", "typed API", "loading", "accessible", "Vitest", "strict", "read-only", "uv sync --locked", "npm ci", "package-only"} {
		// The evidence boundary is stated as package compatibility, not application proof.
		if topic == "package-only" {
			topic = "does not prove an example application"
		}
		if !bytes.Contains(bytes.ToLower(combined), []byte(strings.ToLower(topic))) {
			t.Fatal("missing stack guidance", topic)
		}
	}
	for name, raw := range snapshot(t, "../teamsource/python-fastapi/implement/skills/python-fastapi-stack") {
		for _, bad := range []string{"go test", "gofmt", "golangci-lint", "multiple Go modules", "aws-sdk-go", "All SQL lives"} {
			if bytes.Contains(raw, []byte(bad)) {
				t.Fatal("inherited application instruction", name, bad)
			}
		}
	}
	for _, command := range append(p.Setup, p.Checks...) {
		if command.CWD != "backend" && command.CWD != "frontend" {
			t.Fatal("unconfined default", command.Name)
		}
		if command.TimeoutSeconds < 1 || command.TimeoutSeconds > 900 {
			t.Fatal("unbounded command", command.Name)
		}
		if command.Argv[0] == "uv" && !strings.Contains(strings.Join(command.Argv, " "), "--no-python-downloads") {
			t.Fatal("automatic host download possible", command.Name)
		}
	}
}

func TestPythonFastAPIManifestAndTestDiagnostics(t *testing.T) {
	p := pythonProfile(t)
	for _, tt := range []struct {
		name, component, want string
		files                 map[string]string
	}{
		{"backend manifest", "backend", "Missing required backend manifest: pyproject.toml", nil},
		{"backend lock", "backend", "Missing required backend manifest: uv.lock", map[string]string{"pyproject.toml": "[project]\nname='fixture'\n"}},
		{"backend tests", "backend", "Missing required backend tests", map[string]string{"pyproject.toml": "[project]\nname='fixture'\n", "uv.lock": "version=1"}},
		{"frontend manifest", "frontend", "Missing required frontend manifest: package.json", nil},
		{"frontend lock", "frontend", "Missing required frontend manifest: package-lock.json", map[string]string{"package.json": "{}"}},
		{"frontend script", "frontend", "Missing required frontend script: typecheck", map[string]string{"package.json": "{}", "package-lock.json": "{}", "tsconfig.json": "{}"}},
		{"strict types", "frontend", "TypeScript strict checking required", map[string]string{"package.json": `{"scripts":{"typecheck":"tsc","lint":"eslint","test":"vitest","build":"vite"}}`, "package-lock.json": "{}", "tsconfig.json": "{}"}},
		{"frontend tests", "frontend", "Missing required frontend tests", map[string]string{"package.json": `{"scripts":{"typecheck":"tsc","lint":"eslint","test":"vitest","build":"vite"}}`, "package-lock.json": "{}", "tsconfig.json": `{"compilerOptions":{"strict":true}}`}},
		{"test skip", "frontend", "Successful test skips forbidden", map[string]string{"package.json": `{"scripts":{"typecheck":"tsc","lint":"eslint","test":"vitest --passWithNoTests","build":"vite"}}`, "package-lock.json": "{}", "tsconfig.json": `{"compilerOptions":{"strict":true}}`}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, raw := range tt.files {
				put(t, filepath.Join(dir, name), []byte(raw))
			}
			for _, c := range p.Checks {
				if c.Name == tt.component+"-inputs" {
					cmd := exec.CommandContext(t.Context(), c.Argv[0], c.Argv[1:]...)
					cmd.Dir = dir
					out, err := cmd.CombinedOutput()
					if err == nil || !strings.Contains(string(out), tt.want) {
						t.Fatalf("expected %s: %s, %v", tt.want, out, err)
					}
				}
			}
		})
	}
}

func TestPythonFastAPIGuardsAcceptInputsAndRejectVersions(t *testing.T) {
	p := pythonProfile(t)
	for _, component := range []string{"backend", "frontend"} {
		t.Run(component, func(t *testing.T) {
			dir := t.TempDir()
			if component == "backend" {
				put(t, filepath.Join(dir, "pyproject.toml"), []byte("[project]\nname='fixture'\n"))
				put(t, filepath.Join(dir, "uv.lock"), []byte("version=1"))
				put(t, filepath.Join(dir, "tests/test_contract.py"), []byte("def test_contract(): pass\n"))
			} else {
				put(t, filepath.Join(dir, "package.json"), []byte(`{"scripts":{"typecheck":"tsc","lint":"eslint","test":"vitest","build":"vite"}}`))
				put(t, filepath.Join(dir, "package-lock.json"), []byte(`{}`))
				put(t, filepath.Join(dir, "tsconfig.json"), []byte(`{"compilerOptions":{"strict":true}}`))
				put(t, filepath.Join(dir, "src/contract.test.ts"), []byte("// contract inventory fixture\n"))
			}
			for _, c := range p.Checks {
				if c.Name == component+"-inputs" {
					cmd := exec.CommandContext(t.Context(), c.Argv[0], c.Argv[1:]...)
					cmd.Dir = dir
					if out, err := cmd.CombinedOutput(); err != nil {
						t.Fatalf("valid input rejected: %s, %v", out, err)
					}
				}
				if c.Name == component+"-host" {
					argv := append([]string{}, c.Argv...)
					// A deliberately impossible pin exercises the failure path on any test host.
					argv[2] = strings.ReplaceAll(strings.ReplaceAll(argv[2], "(3,14,0)", "(999,0,0)"), "24.16.0", "999.0.0")
					cmd := exec.CommandContext(t.Context(), argv[0], argv[1:]...)
					cmd.Dir = dir
					out, err := cmd.CombinedOutput()
					if err == nil || !strings.Contains(string(out), "version incompatible") {
						t.Fatalf("incompatible host passed: %s, %v", out, err)
					}
				}
			}
		})
	}
}

func TestPythonFastAPIVersionDocumentationMatchesRecipes(t *testing.T) {
	p := pythonProfile(t)
	doc, err := os.ReadFile("../teamsource/python-fastapi/plan/skills/plan-python-fastapi-guidance/supported-versions.md")
	if err != nil {
		t.Fatal(err)
	}
	other, err := os.ReadFile("../teamsource/python-fastapi/implement/skills/python-fastapi-stack/supported-versions.md")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(doc, other) {
		t.Fatal("version documents disagree")
	}
	recipes := ""
	for _, c := range append(p.Setup, p.Checks...) {
		recipes += strings.Join(c.Argv, " ") + "\n"
	}
	for _, pin := range []string{"3.14.0", "24.16.0", "0.10.12", "11.13.0", "0.142.4", "2.14.0", "0.17.0", "2.4.0", "9.1.1", "0.28.1", "19.3.0", "8.3.4", "7.0.2", "6.1.2", "10.12.0", "5.0.3", "16.3.3"} {
		if !strings.Contains(string(doc), "| "+pin+" |") || !strings.Contains(recipes, pin) {
			t.Fatal("pin missing from documentation or recipe", pin)
		}
	}
	for _, name := range []string{"backend-format", "backend-lint", "backend-types", "backend-tests", "frontend-typecheck", "frontend-lint", "frontend-test", "frontend-build"} {
		found := false
		for _, c := range p.Checks {
			if c.Name == name {
				found = true
			}
		}
		if !found {
			t.Fatal("required check absent", name)
		}
	}
	for _, bad := range []string{"@latest", "--upgrade", "--if-present", "--passWithNoTests"} {
		if strings.Contains(recipes, bad) {
			t.Fatal("floating setup or successful skip", bad)
		}
	}
}
