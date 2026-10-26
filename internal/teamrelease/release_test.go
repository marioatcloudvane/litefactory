package teamrelease

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/cloudvane/litefactory/internal/teamsource/teamsourcetest"
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
	files, err := teamsourcetest.Files("go-development")
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range files {
		put(t, filepath.Join(root, "teams/source/go-development", filepath.FromSlash(name)), raw)
	}
	raw, err := os.ReadFile("../../teams/source/go-development/release.json")
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(root, "teams/source/go-development/release.json"), raw)
	return root
}

// resolvedSource writes the resolved teams/source/<team> (base included) into a
// private folder, so tests read current team content without a published release.
func resolvedSource(t *testing.T, team string) string {
	t.Helper()
	files, err := teamsourcetest.Files(team)
	if err != nil {
		t.Fatal(err)
	}
	dir := canonicalTemp(t)
	for name, raw := range files {
		put(t, filepath.Join(dir, filepath.FromSlash(name)), raw)
	}
	return dir
}

// sourceVersion returns the version the source at dir currently releases, so tests
// that mean "the current release" follow the owner's releases instead of pinning one.
func sourceVersion(t *testing.T, dir string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "release.json"))
	if err != nil {
		t.Fatal(err)
	}
	var meta struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil || meta.Version == "" {
		t.Fatalf("release version in %s: %v", dir, err)
	}
	return meta.Version
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
	version := sourceVersion(t, "../../teams/source/go-development")
	name := "teams/go-development/v" + version + "/research/research.md"
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
	rules := "teams/source/go-development/implement/skills/litefactory-rules/litefactory.md"
	put(t, filepath.Join(root, filepath.FromSlash(rules)), append(first[rules], []byte("\nRetained new guidance.\n")...))
	before := snapshot(t, root)
	if err := Generate(t.Context(), root, true); err == nil {
		t.Fatal("source drift was missed")
	}
	if !reflect.DeepEqual(before, snapshot(t, root)) {
		t.Fatal("source check wrote output")
	}
	meta := filepath.Join(root, "teams/source/go-development/release.json")
	b, err := os.ReadFile(meta)
	if err != nil {
		t.Fatal(err)
	}
	// A changed source needs a new version: the published one is immutable.
	put(t, meta, bytes.ReplaceAll(b, []byte(version), []byte("99.0.0")))
	generate(t, root)
	after := snapshot(t, root)
	if bytes.Equal(first["teams/teams.json"], after["teams/teams.json"]) {
		t.Fatal("source edit did not change catalogue hashes")
	}
	if !bytes.Contains(after["teams/go-development/v99.0.0/implement/skills/litefactory-rules/litefactory.md"], []byte("Retained new guidance")) {
		t.Fatal("missing materialization")
	}
}

func TestInvalidCandidatePreservesValidOutput(t *testing.T) {
	for _, tt := range []struct {
		name, file, cause string
		change            func(*testing.T, string)
	}{
		{"missing resource", "litefactory.md", "litefactory.md", func(t *testing.T, root string) {
			if err := os.Remove(filepath.Join(root, "teams/source/go-development/implement/skills/litefactory-rules/litefactory.md")); err != nil {
				t.Fatal(err)
			}
		}},
		{"unsafe resource", "SKILL.md", "unsafe", func(t *testing.T, root string) {
			p := filepath.Join(root, "teams/source/go-development/implement/skills/litefactory-rules/SKILL.md")
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			put(t, p, bytes.ReplaceAll(b, []byte("  - litefactory.md"), []byte("  - ../litefactory.md")))
		}},
		{"symlink", "litefactory.md", "symlink", func(t *testing.T, root string) {
			p := filepath.Join(root, "teams/source/go-development/implement/skills/litefactory-rules/litefactory.md")
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("SKILL.md", p); err != nil {
				t.Fatal(err)
			}
		}},
		{"unsafe download", "release.json", "download", func(t *testing.T, root string) {
			p := filepath.Join(root, "teams/source/go-development/release.json")
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			put(t, p, bytes.ReplaceAll(b, []byte("go-development/v"+sourceVersion(t, "../../teams/source/go-development")), []byte("../outside")))
		}},
		{"invalid version", "release.json", "SemVer", func(t *testing.T, root string) {
			p := filepath.Join(root, "teams/source/go-development/release.json")
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			put(t, p, bytes.ReplaceAll(b, []byte(sourceVersion(t, "../../teams/source/go-development")), []byte("invalid")))
		}},
		{"invalid identity", "research.dot", "identity", func(t *testing.T, root string) {
			p := filepath.Join(root, "teams/source/go-development/research/research.dot")
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
			tt.change(t, root)
			prior := snapshot(t, filepath.Join(root, "teams"))
			mutated := snapshot(t, filepath.Join(root, "teams/source"))
			err := Generate(t.Context(), root, false)
			if err == nil || !strings.Contains(err.Error(), tt.cause) {
				t.Fatalf("expected refusal with cause %s: %v", tt.cause, err)
			}
			if !reflect.DeepEqual(prior, snapshot(t, filepath.Join(root, "teams"))) || !reflect.DeepEqual(mutated, snapshot(t, filepath.Join(root, "teams/source"))) {
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
	if _, err := os.Lstat("../../teams/source/go-development/shared"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("shared guidance tree remains: %v", err)
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

func TestInstallationCaptureAndNativeDelivery(t *testing.T) {
	root := fixture(t)
	generate(t, root)
	version := sourceVersion(t, "../../teams/source/go-development")
	server := httptest.NewTLSServer(http.FileServer(http.Dir(filepath.Join(root, "teams"))))
	defer server.Close()
	for _, source := range []string{filepath.Join(root, "teams/teams.json"), server.URL + "/teams.json"} {
		m, err := teampackage.New(teampackage.Options{Paths: config.NewPaths(canonicalTemp(t)), Validate: turn.ValidateTeamWorkflows, Transport: server.Client().Transport})
		if err != nil {
			t.Fatal(err)
		}
		installed, _, err := m.Install(t.Context(), source, "go-development", version)
		if err != nil {
			t.Fatal(err)
		}
		if !installed.Integrity || len(installed.Workflows) != 6 {
			t.Fatal("incomplete installed release")
		}
		clone, err := m.Clone(t.Context(), source, "go-development", version, "custom", "2.0.0")
		if err != nil {
			t.Fatal(err)
		}
		if clone.Origin == nil || clone.Origin.Checksum != installed.Identity.Digest {
			t.Fatal("clone lost origin")
		}
		err = m.WithCatalogue(t.Context(), func(a *teampackage.CatalogueAccess) error {
			return a.WithSelected(t.Context(), source, "go-development", version, func(got teampackage.Installed, sourceFS fs.FS) error {
				if !reflect.DeepEqual(got.Identity, installed.Identity) {
					t.Fatal("selection lost installed identity")
				}
				for _, w := range workflows {
					d := capture(t, sourceFS, w, "go-development")
					rules := map[string][]byte{}
					if w == "implement" || w == "bug" || w == "validate" {
						rules = snapshot(t, filepath.Join(root, "teams/source/go-development", w, "skills/litefactory-rules"))
					}
					for id, node := range d.Nodes {
						for name := range node.Files {
							if strings.Contains(name, "team-guidance") {
								t.Fatalf("%s/%s still carries %s", w, id, name)
							}
						}
						for name, raw := range rules {
							if !bytes.Equal(raw, node.Files["skills/litefactory-rules/"+name]) {
								t.Fatalf("%s/%s misses litefactory rule %s", w, id, name)
							}
						}
						for _, kind := range []string{"claude", "codex"} {
							b := bundle(t, d, node, kind)
							nativeFiles := snapshot(t, b.Root)
							for name, raw := range rules {
								if !bytes.Equal(raw, nativeFiles["plugin/skills/litefactory-rules/"+name]) {
									t.Fatalf("%s/%s/%s omits litefactory rule %s", w, id, kind, name)
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
}

func TestBadCatalogueHashPreservesInstalledRelease(t *testing.T) {
	root := fixture(t)
	generate(t, root)
	version := sourceVersion(t, "../../teams/source/go-development")
	m, err := teampackage.New(teampackage.Options{Paths: config.NewPaths(canonicalTemp(t)), Validate: turn.ValidateTeamWorkflows})
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "teams/teams.json")
	installed, _, err := m.Install(t.Context(), source, "go-development", version)
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
	put(t, filepath.Join(root, "teams/go-development/v"+version, filepath.FromSlash(file)), []byte("bad bytes"))
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
		"teams/source/go-development/bug/skills/litefactory-rules":                       {"SKILL.md": []byte("candidate")},
		"teams/go-development/v" + sourceVersion(t, "../../teams/source/go-development"): {"candidate.md": []byte("candidate")},
	}
	if err := publish(t.Context(), root, repo, outputs, []byte("new catalogue"), rename); err == nil || !strings.Contains(err.Error(), "injected publication") {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, snapshot(t, repo)) {
		t.Fatal("partial publication damaged prior output")
	}
}
