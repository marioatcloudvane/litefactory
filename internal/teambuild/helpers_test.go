package teambuild

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/teampackage"
	"github.com/cloudvane/litefactory/internal/teamsource"
	"github.com/cloudvane/litefactory/internal/teamsource/teamsourcetest"
	"github.com/cloudvane/litefactory/internal/turn"
)

// teams are the team folders under teams/source; base-team is a base.
var teams = []string{"go-development", "python-fastapi", "dotnet-web", "swift-ios", "nextjs-web"}

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
	if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, raw, 0o600); err != nil {
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

// repoSource is the repository's teams/source folder.
func repoSource() string { return filepath.Join(teamsourcetest.RepoRoot(), "teams", "source") }

// copySource copies the whole repository teams/source tree into a private
// folder and returns that copy's teams/source path, for tests that mutate it.
func copySource(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(canonicalTemp(t), "teams", "source")
	for name, raw := range snapshot(t, repoSource()) {
		put(t, filepath.Join(dir, filepath.FromSlash(name)), raw)
	}
	return dir
}

// flatFixture writes the resolved team (base folded in, no inherit.json) and
// its release.json into a private teams/source folder and returns it.
func flatFixture(t *testing.T, team string) string {
	t.Helper()
	files, err := teamsourcetest.Files(t.Context(), team)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(canonicalTemp(t), "teams", "source")
	for name, raw := range files {
		put(t, filepath.Join(dir, team, filepath.FromSlash(name)), raw)
	}
	raw, err := os.ReadFile(filepath.Join(repoSource(), team, "release.json"))
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(dir, team, "release.json"), raw)
	return dir
}

// resolvedDir writes the resolved team source into a private folder.
func resolvedDir(t *testing.T, team string) string {
	t.Helper()
	files, err := teamsourcetest.Files(t.Context(), team)
	if err != nil {
		t.Fatal(err)
	}
	dir := canonicalTemp(t)
	for name, raw := range files {
		put(t, filepath.Join(dir, filepath.FromSlash(name)), raw)
	}
	return dir
}

// builtDir builds team from the repository source and writes the installable
// files into a private folder.
func builtDir(t *testing.T, team string) string {
	t.Helper()
	files, _, err := Build(t.Context(), repoSource(), team)
	if err != nil {
		t.Fatal(err)
	}
	dir := canonicalTemp(t)
	for name, raw := range files {
		put(t, filepath.Join(dir, filepath.FromSlash(name)), raw)
	}
	return dir
}

func sourceVersion(t *testing.T, team string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoSource(), team, "release.json"))
	if err != nil {
		t.Fatal(err)
	}
	var meta struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil || meta.Version == "" {
		t.Fatalf("release version of %s: %v", team, err)
	}
	return meta.Version
}

func identity(dir string) string { return "file://" + filepath.ToSlash(dir) }

func newManager(t *testing.T) *teampackage.Manager {
	t.Helper()
	m, err := teampackage.New(teampackage.Options{Paths: config.NewPaths(canonicalTemp(t)), Validate: turn.ValidateTeamWorkflows})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// install builds team from sourceDir and installs it into a fresh home.
func install(t *testing.T, sourceDir, team string) (*teampackage.Manager, teampackage.Installed) {
	t.Helper()
	files, release, err := Build(t.Context(), sourceDir, team)
	if err != nil {
		t.Fatal(err)
	}
	m := newManager(t)
	installed, result, err := m.InstallBuilt(t.Context(), identity(sourceDir), files, release, teampackage.BuildInfo{})
	if err != nil || result != teampackage.InstallNew {
		t.Fatalf("install %s: %v, %v", team, result, err)
	}
	return m, installed
}

func registry(t *testing.T, w string) graphexec.Registry {
	t.Helper()
	builders := map[string]func() (graphexec.Registry, error){"research": turn.ResearchSpecs, "plan": turn.PlanSpecs, "implement": turn.ImplementSpecs, "validate": turn.ValidateFlowSpecs, "spike": turn.SpikeSpecs, "doctor": turn.DoctorSpecs, "bug": turn.BugSpecs}
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
