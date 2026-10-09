package teamrelease

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/teampackage"
	"github.com/cloudvane/litefactory/internal/teamsource"
	"github.com/cloudvane/litefactory/internal/turn"
)

func canonicalTemp(t *testing.T) string {
	t.Helper()
	p, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func put(t *testing.T, name string, raw []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, raw, 0600); err != nil {
		t.Fatal(err)
	}
}
func snapshot(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	err := filepath.WalkDir(dir, func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, name)
		if err != nil {
			return err
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = raw
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func fixture(t *testing.T) string {
	t.Helper()
	root := canonicalTemp(t)
	for _, dir := range append(append([]string{}, workflows...), "shared") {
		for name, raw := range snapshot(t, "../teamsource/"+dir) {
			if strings.HasPrefix(name, "skills/team-guidance/") {
				continue
			}
			put(t, filepath.Join(root, "internal/teamsource", dir, filepath.FromSlash(name)), raw)
		}
	}
	raw, err := os.ReadFile("../teamsource/release.json")
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(root, "internal/teamsource/release.json"), raw)
	profile, err := os.ReadFile("../teamsource/stack-profile.json")
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(root, "internal/teamsource/stack-profile.json"), profile)
	return root
}
func generate(t *testing.T, root string) {
	t.Helper()
	if err := Generate(t.Context(), root, false); err != nil {
		t.Fatal(err)
	}
}

func TestGenerationDeterminismAndReadOnlyDrift(t *testing.T) {
	root := fixture(t)
	generate(t, root)
	first := snapshot(t, root)
	generate(t, root)
	if !reflect.DeepEqual(first, snapshot(t, root)) {
		t.Fatal("second generation changed bytes")
	}
	other := fixture(t)
	generate(t, other)
	if !reflect.DeepEqual(first, snapshot(t, other)) {
		t.Fatal("generation depends on root or time")
	}
	if err := Generate(t.Context(), root, true); err != nil {
		t.Fatal(err)
	}
	name := "teams/go-development/v1.2.0/research/research.md"
	put(t, filepath.Join(root, filepath.FromSlash(name)), []byte("drift"))
	changed := snapshot(t, root)
	if err := Generate(t.Context(), root, true); err == nil || !strings.Contains(err.Error(), name) {
		t.Fatalf("drift: %v", err)
	}
	if !reflect.DeepEqual(changed, snapshot(t, root)) {
		t.Fatal("check rewrote drifted output")
	}
	generate(t, root)
	if !reflect.DeepEqual(first, snapshot(t, root)) {
		t.Fatal("repair did not reproduce release")
	}
	source := filepath.Join(root, "internal/teamsource/shared/team-guidance/environment-safety.md")
	put(t, source, append(first["internal/teamsource/shared/team-guidance/environment-safety.md"], []byte("\nRetained new guidance.\n")...))
	before := snapshot(t, root)
	if err := Generate(t.Context(), root, true); err == nil {
		t.Fatal("source drift was missed")
	}
	if !reflect.DeepEqual(before, snapshot(t, root)) {
		t.Fatal("source check wrote output")
	}
	meta := filepath.Join(root, "internal/teamsource/release.json")
	b, err := os.ReadFile(meta)
	if err != nil {
		t.Fatal(err)
	}
	put(t, meta, bytes.ReplaceAll(b, []byte("1.2.0"), []byte("1.2.1")))
	generate(t, root)
	after := snapshot(t, root)
	if bytes.Equal(first["teams/teams.json"], after["teams/teams.json"]) {
		t.Fatal("source edit did not change catalogue hashes")
	}
	for _, w := range workflows {
		if !bytes.Contains(after["internal/teamsource/"+w+"/skills/team-guidance/environment-safety.md"], []byte("Retained new guidance")) {
			t.Fatal("missing materialization", w)
		}
	}
}

func TestInvalidCandidatePreservesValidOutput(t *testing.T) {
	for _, tt := range []struct {
		name, file, cause string
		change            func(*testing.T, string)
	}{
		{"missing resource", "workflow.md", "workflow.md", func(t *testing.T, root string) {
			if err := os.Remove(filepath.Join(root, "internal/teamsource/shared/team-guidance/workflow.md")); err != nil {
				t.Fatal(err)
			}
		}},
		{"unsafe resource", "SKILL.md", "unsafe", func(t *testing.T, root string) {
			p := filepath.Join(root, "internal/teamsource/shared/team-guidance/SKILL.md")
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			put(t, p, bytes.ReplaceAll(b, []byte("  - workflow.md"), []byte("  - ../workflow.md")))
		}},
		{"symlink", "workflow.md", "symlink", func(t *testing.T, root string) {
			p := filepath.Join(root, "internal/teamsource/shared/team-guidance/workflow.md")
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("litefactory.md", p); err != nil {
				t.Fatal(err)
			}
		}},
		{"unsafe download", "release.json", "download", func(t *testing.T, root string) {
			p := filepath.Join(root, "internal/teamsource/release.json")
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			put(t, p, bytes.ReplaceAll(b, []byte("go-development/v1.2.0"), []byte("../outside")))
		}},
		{"invalid version", "release.json", "SemVer", func(t *testing.T, root string) {
			p := filepath.Join(root, "internal/teamsource/release.json")
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			put(t, p, bytes.ReplaceAll(b, []byte("1.2.0"), []byte("invalid")))
		}},
		{"invalid identity", "research.dot", "identity", func(t *testing.T, root string) {
			p := filepath.Join(root, "internal/teamsource/research/research.dot")
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			put(t, p, bytes.ReplaceAll(b, []byte("go-development"), []byte("foreign")))
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := fixture(t)
			generate(t, root)
			prior := snapshot(t, filepath.Join(root, "teams"))
			tt.change(t, root)
			mutated := snapshot(t, filepath.Join(root, "internal"))
			err := Generate(t.Context(), root, false)
			if err == nil || !strings.Contains(err.Error(), tt.cause) {
				t.Fatalf("expected refusal with cause %s: %v", tt.cause, err)
			}
			if !reflect.DeepEqual(prior, snapshot(t, filepath.Join(root, "teams"))) || !reflect.DeepEqual(mutated, snapshot(t, filepath.Join(root, "internal"))) {
				t.Fatal("invalid candidate changed prior output")
			}
		})
	}
}

func TestCanonicalMigrationAndGraphContracts(t *testing.T) {
	for _, name := range []string{"AGENTS.md", "CLAUDE.md", ".claude", ".codex"} {
		if _, err := os.Lstat(filepath.Join("../..", name)); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("former host root %s remains: %v", name, err)
		}
	}
	root := fixture(t)
	generate(t, root)
	for _, w := range workflows {
		original, err := os.ReadFile(filepath.Join("../../teams/go-development/v1", w, w+".dot"))
		if err != nil {
			t.Fatal(err)
		}
		current, err := os.ReadFile(filepath.Join(root, "internal/teamsource", w, w+".dot"))
		if err != nil {
			t.Fatal(err)
		}
		normalized := bytes.ReplaceAll(current, []byte(`team="go-development"`), []byte(`team="builtin"`))
		normalized = bytes.ReplaceAll(normalized, []byte(",team-guidance"), nil)
		for _, role := range []string{"architect", "code-reviewer", "implementation-engineer", "unit-test-engineer", "integration-test-engineer"} {
			normalized = bytes.ReplaceAll(normalized, []byte(`role="`+role+`"`), []byte(`role="go-`+role+`"`))
		}
		if !bytes.Equal(original, normalized) {
			t.Fatalf("%s graph changed beyond team and guidance", w)
		}
	}
	for name, raw := range snapshot(t, "../teamsource/shared/team-guidance") {
		if bytes.Contains(raw, []byte(".claude/")) || bytes.Contains(raw, []byte(".codex/")) || bytes.Contains(raw, []byte("teams/source/")) {
			t.Errorf("%s retains active host reference", name)
		}
	}
	for _, entry := range []struct {
		file  string
		terms []string
	}{
		{"litefactory.md", []string{"only when working on litefactory", "dist/litefactory", "$LF_HOME", "one", "modernc.org/sqlite", "UTC millisecond", "internal/store", "internal/git", "internal/daemon", "-race", "pinned", "golangci-lint"}},
		{"workflow.md", []string{"preflight", "request identity", "list B", "Historical acceptance", "approval", "numeric owner", "Pending"}},
		{"environment-safety.md", []string{"production", "TEST_BASE_URL", "TEST_DATABASE_URL", "no executable hook"}},
		{"planning-responsibilities.md", []string{"version bounds", "missing prerequisites", "Unavailable proof", "frozen story"}},
	} {
		raw, err := os.ReadFile("../teamsource/shared/team-guidance/" + entry.file)
		if err != nil {
			t.Fatal(err)
		}
		for _, term := range entry.terms {
			if !strings.Contains(strings.Join(strings.Fields(strings.ToLower(string(raw))), " "), strings.ToLower(term)) {
				t.Errorf("%s missing retained rule %q", entry.file, term)
			}
		}
	}
}

func registry(t *testing.T, w string) graphexec.Registry {
	t.Helper()
	builders := map[string]func() (graphexec.Registry, error){"research": turn.ResearchSpecs, "plan": turn.PlanSpecs, "implement": turn.ImplementSpecs, "validate": turn.ValidateFlowSpecs, "spike": turn.SpikeSpecs, "bug": turn.SimplifiedBugSpecs}
	r, err := builders[w]()
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func capture(t *testing.T, source fs.FS, w, team string) *teamsource.WorkflowDefinition {
	t.Helper()
	sub, err := fs.Sub(source, w)
	if err != nil {
		t.Fatal(err)
	}
	d, err := teamsource.CaptureWorkflowIdentity(t.Context(), sub, w+".dot", team, agent.Workflow(w), registry(t, w))
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func bundle(t *testing.T, d *teamsource.WorkflowDefinition, node teamsource.WorkflowNode, kind string) agent.Delivery {
	t.Helper()
	rendered, err := teamsource.NativeWorkflow(kind, node, "test-model", []string{"Read"}, "read-only")
	if err != nil {
		t.Fatal(err)
	}
	b := agent.Delivery{Root: canonicalTemp(t), Identity: d.Digest, RenderedFiles: map[string]string{}, Evidence: agent.DeliveryEvidence{Team: d.Profile.Team, SnapshotDigest: d.Digest, ResourceHashes: d.Hashes}}
	for _, file := range rendered {
		put(t, filepath.Join(b.Root, filepath.FromSlash(file.Path)), file.Bytes)
		sum := sha256.Sum256(file.Bytes)
		b.RenderedFiles[file.Path] = hex.EncodeToString(sum[:])
	}
	if err := agent.VerifyDelivery(t.Context(), b); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestInstallationCaptureNativeDeliveryAndHistoricalReplay(t *testing.T) {
	// Old catalogue bytes and delivery evidence are retained before new generation.
	oldSource := os.DirFS("../../teams/go-development/v1")
	oldDefinitions := map[string]*teamsource.WorkflowDefinition{}
	oldFiles := map[string]map[string][]byte{}
	oldBundles := map[string]agent.Delivery{}
	oldRendered := map[string]map[string][]byte{}
	for _, w := range workflows {
		old := capture(t, oldSource, w, "builtin")
		oldDefinitions[w] = old
		saved := map[string][]byte{}
		for name, raw := range old.Files {
			saved[name] = bytes.Clone(raw)
		}
		oldFiles[w] = saved
		for id, node := range old.Nodes {
			for _, kind := range []string{"claude", "codex"} {
				key := w + "/" + id + "/" + kind
				b := bundle(t, old, node, kind)
				oldBundles[key] = b
				oldRendered[key] = snapshot(t, b.Root)
			}
		}
	}
	root := fixture(t)
	generate(t, root)
	server := httptest.NewTLSServer(http.FileServer(http.Dir(filepath.Join(root, "teams"))))
	defer server.Close()
	for _, source := range []string{filepath.Join(root, "teams/teams.json"), server.URL + "/teams.json"} {
		m, err := teampackage.New(teampackage.Options{Paths: config.NewPaths(canonicalTemp(t)), Validate: turn.ValidateTeamWorkflows, Transport: server.Client().Transport})
		if err != nil {
			t.Fatal(err)
		}
		installed, _, err := m.Install(t.Context(), source, "go-development", "1.2.0")
		if err != nil {
			t.Fatal(err)
		}
		if !installed.Integrity || len(installed.Workflows) != 6 {
			t.Fatal("incomplete installed release")
		}
		clone, err := m.Clone(t.Context(), source, "go-development", "1.2.0", "custom", "2.0.0")
		if err != nil {
			t.Fatal(err)
		}
		if clone.Origin == nil || clone.Origin.Checksum != installed.Identity.Digest {
			t.Fatal("clone lost origin")
		}
		err = m.WithCatalogue(t.Context(), func(a *teampackage.CatalogueAccess) error {
			return a.WithSelected(t.Context(), source, "go-development", "1.2.0", func(got teampackage.Installed, sourceFS fs.FS) error {
				if !reflect.DeepEqual(got.Identity, installed.Identity) {
					t.Fatal("selection lost installed identity")
				}
				for _, w := range workflows {
					d := capture(t, sourceFS, w, "go-development")
					shared := snapshot(t, filepath.Join(root, "internal/teamsource", w, "skills/team-guidance"))
					for id, node := range d.Nodes {
						for name, raw := range shared {
							if !bytes.Equal(raw, node.Files["skills/team-guidance/"+name]) {
								t.Fatalf("%s/%s misses migrated %s", w, id, name)
							}
						}
						for _, kind := range []string{"claude", "codex"} {
							b := bundle(t, d, node, kind)
							nativeFiles := snapshot(t, b.Root)
							for name, raw := range shared {
								if !bytes.Equal(raw, nativeFiles["plugin/skills/team-guidance/"+name]) {
									t.Fatalf("%s/%s/%s omits migrated %s", w, id, kind, name)
								}
							}
						}
					}
				}
				return nil
			})
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, w := range workflows {
		old := oldDefinitions[w]
		replay, err := teamsource.CaptureWorkflowIdentity(t.Context(), teamsource.RetainedFiles(oldFiles[w]), w+".dot", "builtin", agent.Workflow(w), registry(t, w))
		if err != nil {
			t.Fatal(err)
		}
		if replay.Digest != old.Digest || !reflect.DeepEqual(replay.Hashes, old.Hashes) || !reflect.DeepEqual(replay.Files, old.Files) || replay.Profile.Team != "builtin" {
			t.Fatal("rewrote historical capture", w)
		}
	}
	for key, b := range oldBundles {
		if err := agent.VerifyDelivery(t.Context(), b); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(oldRendered[key], snapshot(t, b.Root)) || b.Evidence.Team != "builtin" {
			t.Fatal("rewrote historical delivery", key)
		}
	}
}

func TestBadCatalogueHashPreservesInstalledRelease(t *testing.T) {
	root := fixture(t)
	generate(t, root)
	m, err := teampackage.New(teampackage.Options{Paths: config.NewPaths(canonicalTemp(t)), Validate: turn.ValidateTeamWorkflows})
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "teams/teams.json")
	installed, _, err := m.Install(t.Context(), source, "go-development", "1.2.0")
	if err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, installed.Path)
	raw, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	var cat teampackage.Catalogue
	if err := json.Unmarshal(raw, &cat); err != nil {
		t.Fatal(err)
	}
	file := cat.Releases[0].Files[0].Path
	cat.Releases[0].Version = "1.3.0"
	updated, err := json.Marshal(cat)
	if err != nil {
		t.Fatal(err)
	}
	put(t, source, updated)
	put(t, filepath.Join(root, "teams/go-development/v1.2.0", filepath.FromSlash(file)), []byte("bad bytes"))
	if _, _, err := m.Install(t.Context(), source, "go-development", "1.3.0"); err == nil || !strings.Contains(err.Error(), file) {
		t.Fatalf("bad file/hash accepted: %v", err)
	}
	if !reflect.DeepEqual(before, snapshot(t, installed.Path)) {
		t.Fatal("invalid fetched file changed installed output")
	}
}

func TestCancelledGenerationPreservesOutput(t *testing.T) {
	root := fixture(t)
	generate(t, root)
	before := snapshot(t, root)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := Generate(ctx, root, false); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, snapshot(t, root)) {
		t.Fatal("cancelled generation changed output")
	}
}

func TestPublicationFailureRestoresEntireBatch(t *testing.T) {
	repo := fixture(t)
	generate(t, repo)
	before := snapshot(t, repo)
	root, err := os.OpenRoot(repo)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	calls := 0
	rename := func(oldPath, newPath string) error {
		calls++
		if calls == 4 {
			return errors.New("injected publication failure")
		}
		return root.Rename(oldPath, newPath)
	}
	outputs := map[string]map[string][]byte{
		"internal/teamsource/bug/skills/team-guidance": {"SKILL.md": []byte("candidate")},
		"teams/go-development/v1.2.0":                  {"candidate.md": []byte("candidate")},
	}
	if err := publish(t.Context(), root, repo, outputs, []byte("new catalogue"), rename); err == nil || !strings.Contains(err.Error(), "injected publication") {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, snapshot(t, repo)) {
		t.Fatal("partial publication damaged prior output")
	}
}
