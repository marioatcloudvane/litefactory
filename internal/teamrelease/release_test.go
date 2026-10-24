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
	for _, dir := range workflows {
		for name, raw := range snapshot(t, "../teamsource/"+dir) {
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
	version := sourceVersion(t, "../teamsource")
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
	rules := "internal/teamsource/implement/skills/litefactory-rules/litefactory.md"
	put(t, filepath.Join(root, filepath.FromSlash(rules)), append(first[rules], []byte("\nRetained new guidance.\n")...))
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
			if err := os.Remove(filepath.Join(root, "internal/teamsource/implement/skills/litefactory-rules/litefactory.md")); err != nil {
				t.Fatal(err)
			}
		}},
		{"unsafe resource", "SKILL.md", "unsafe", func(t *testing.T, root string) {
			p := filepath.Join(root, "internal/teamsource/implement/skills/litefactory-rules/SKILL.md")
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			put(t, p, bytes.ReplaceAll(b, []byte("  - litefactory.md"), []byte("  - ../litefactory.md")))
		}},
		{"symlink", "litefactory.md", "symlink", func(t *testing.T, root string) {
			p := filepath.Join(root, "internal/teamsource/implement/skills/litefactory-rules/litefactory.md")
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("SKILL.md", p); err != nil {
				t.Fatal(err)
			}
		}},
		{"unsafe download", "release.json", "download", func(t *testing.T, root string) {
			p := filepath.Join(root, "internal/teamsource/release.json")
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			put(t, p, bytes.ReplaceAll(b, []byte("go-development/v"+sourceVersion(t, "../teamsource")), []byte("../outside")))
		}},
		{"invalid version", "release.json", "SemVer", func(t *testing.T, root string) {
			p := filepath.Join(root, "internal/teamsource/release.json")
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			put(t, p, bytes.ReplaceAll(b, []byte(sourceVersion(t, "../teamsource")), []byte("invalid")))
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
		current = withoutStandardsGrant(t, current)
		if w == "bug" {
			current = legacyBugSourceWithoutSyncGate(t, current)
		}
		if w == "implement" {
			current = withoutGoStoryWorkerSlotPolicy(t, current)
			current = legacyImplementSourceWithoutOwnerResumeSync(t, current)
		}
		if w == "validate" {
			current = legacyValidateSourceWithoutCandidateAdoption(t, current)
		}
		if w == "spike" {
			current = legacySpikeSourceWithoutRepair(t, current)
		}
		normalized := bytes.ReplaceAll(current, []byte(`team="go-development"`), []byte(`team="builtin"`))
		normalized = bytes.ReplaceAll(normalized, []byte(",litefactory-rules"), nil)
		for _, role := range []string{"architect", "code-reviewer", "implementation-engineer", "unit-test-engineer", "integration-test-engineer"} {
			normalized = bytes.ReplaceAll(normalized, []byte(`role="`+role+`"`), []byte(`role="go-`+role+`"`))
		}
		if !bytes.Equal(original, normalized) && (w != "implement" || !pendingWorkerSyncMatchesRelease(normalized, original)) {
			t.Fatalf("%s graph changed beyond team and litefactory rules", w)
		}
	}
	if _, err := os.Lstat("../teamsource/shared"); !errors.Is(err, fs.ErrNotExist) {
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
	version := sourceVersion(t, "../teamsource")
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
						rules = snapshot(t, filepath.Join(root, "internal/teamsource", w, "skills/litefactory-rules"))
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
	version := sourceVersion(t, "../teamsource")
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
		"internal/teamsource/bug/skills/litefactory-rules":           {"SKILL.md": []byte("candidate")},
		"teams/go-development/v" + sourceVersion(t, "../teamsource"): {"candidate.md": []byte("candidate")},
	}
	if err := publish(t.Context(), root, repo, outputs, []byte("new catalogue"), rename); err == nil || !strings.Contains(err.Error(), "injected publication") {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, snapshot(t, repo)) {
		t.Fatal("partial publication damaged prior output")
	}
}

// Released Bug definitions retain their direct repair routes. Admit only the
// exact new canonical sync gate when comparing editable sources with releases.
// legacyImplementSourceWithoutOwnerResumeSync reverts the unreleased source
// change that resumes the Implement coordinator's owner wait at its base sync
// (#966), so released snapshots stay byte-identical until a new team release.
func legacyImplementSourceWithoutOwnerResumeSync(t *testing.T, source []byte) []byte {
	t.Helper()
	changes := []struct{ current, legacy string }{
		{"(main) into the feature branch, so stories build on a current base. An owner\n// reply that resumes parked stories enters at this base sync again, so a fix\n// that reached main while the stories waited reaches them too. Then it\n", "(main) into the feature branch, so stories build on a current base. Then it\n"},
		{`lf_action="lf_implement_ask", lf_outcomes="published", lf_source="sync_start"`, `lf_action="lf_implement_ask", lf_outcomes="published", lf_source="merge"`},
		{`sync_start          [lf_action="lf_sync_base", lf_outcomes="synced,current,conflict,retry", lf_limit="32"]`, `sync_start          [lf_action="lf_sync_base", lf_outcomes="synced,current,conflict,retry", lf_limit="8"]`},
	}
	for _, change := range changes {
		if bytes.Count(source, []byte(change.current)) != 1 {
			t.Fatalf("canonical Implement owner resume differs from approved source delta: %s", change.current)
		}
		source = bytes.Replace(source, []byte(change.current), []byte(change.legacy), 1)
	}
	return source
}

func legacyBugSourceWithoutSyncGate(t *testing.T, source []byte) []byte {
	t.Helper()
	changes := []struct{ current, legacy string }{
		{` sync_before_fixer [lf_action="lf_sync_base",lf_outcomes="synced,current,conflict,retry",lf_limit="8",lf_park="owner",lf_stop="conflict,failed,invalid,exhausted"];` + "\n", ""},
		{` conversation -> sync_before_fixer [lf_on="approved"];`, ` conversation -> fixer [lf_on="approved"];`},
		{` finalization -> sync_before_fixer [lf_on="revalidate"];`, ` finalization -> fixer [lf_on="revalidate"];`},
		{` sync_before_fixer -> fixer [lf_on="synced"];` + "\n", ""},
		{` sync_before_fixer -> fixer [lf_on="current"];` + "\n", ""},
		{` sync_before_fixer -> sync_before_fixer [lf_on="retry"];` + "\n", ""},
	}
	for _, change := range changes {
		if bytes.Count(source, []byte(change.current)) != 1 {
			t.Fatalf("canonical Bug sync gate differs from approved source delta: %s", change.current)
		}
		source = bytes.Replace(source, []byte(change.current), []byte(change.legacy), 1)
	}
	return source
}

// legacySpikeSourceWithoutRepair reverses the approved source delta that gave
// Spike one repair pass after a failed check (issue #984), so the source can
// still be compared with releases cut before it.
func legacySpikeSourceWithoutRepair(t *testing.T, source []byte) []byte {
	t.Helper()
	cut := func(from, to string) {
		start := bytes.Index(source, []byte(from))
		if start < 0 || bytes.Count(source, []byte(from)) != 1 {
			t.Fatalf("canonical Spike repair differs from approved source delta: %s", from)
		}
		end := bytes.Index(source[start:], []byte(to))
		if end < 0 {
			t.Fatalf("canonical Spike repair differs from approved source delta: %s", to)
		}
		source = append(source[:start:start], source[start+end:]...)
	}
	cut(" // ---- One repair after a failed check", " // ---- Record the outcome")
	cut("\n\n // The one repair:", "\n}")
	changes := []struct{ current, legacy string }{
		{"// Spike answers one narrow question. It investigates read-only (it may run the\n// project's checks, never change a file) and, when the proposed change is small,\n// makes that one change with a write-capable fixer and has it measured and\n// checked. It writes SPIKE.md with a declared status and\n",
			"// Spike answers one narrow question. It investigates read-only and, when the\n// proposed change is small, makes that one change with a write-capable fixer and\n// has it measured and checked. It writes SPIKE.md with a declared status and\n"},
		{"// The fix path allows exactly one repair: a failed check hands the change and the\n// check result to a second fixer pass (refix_agent), whose change is checked\n// again. A second failure is reported as unverified, not repaired. The second\n// pass is its own nodes rather than an edge back, so every node still runs at\n// most once and nothing can loop.\n",
			"// The fix path has no edge back to the fixer or the check, which is what limits\n// each to one run and rules out a retry loop: a failed check is reported as\n// unverified, not repaired.\n"},
		{` note_fixed           [lf_action="lf_spike_note", lf_status="fixed", lf_outcomes="success,failed", lf_source="fix_agent", lf_limit="1"];` + "\n",
			` note_fixed           [lf_action="lf_spike_note", lf_status="fixed", lf_outcomes="success,failed", lf_source="fix_agent", lf_limit="1"];` + "\n" + ` note_fix_unverified  [lf_action="lf_spike_note", lf_status="fix_unverified", lf_outcomes="success,failed", lf_source="fix_agent", lf_limit="1"];` + "\n"},
		{" // Fix, measure, and on a failed check repair once. The fix_agent's ask is not an\n // owner wait: it is recorded as blocked. Nothing leads back to the same fixer\n // or check.\n",
			" // Fix once, measure once, report once. The fix_agent's ask is not an owner wait: it\n // is recorded as blocked. Nothing leads back to the fixer or the check.\n"},
		{` fix_check           -> refix_agent         [lf_on="failed"];`, ` fix_check           -> note_fix_unverified [lf_on="failed"];`},
		{` note_fixed          -> report_fix          [lf_on="success"];` + "\n", ` note_fixed          -> report_fix          [lf_on="success"];` + "\n" + ` note_fix_unverified -> report_fix          [lf_on="success"];` + "\n"},
		{` note_fixed          -> report_failure [lf_on="failed"];` + "\n", ` note_fixed          -> report_failure [lf_on="failed"];` + "\n" + ` note_fix_unverified -> report_failure [lf_on="failed"];` + "\n"},
	}
	for _, change := range changes {
		if bytes.Count(source, []byte(change.current)) != 1 {
			t.Fatalf("canonical Spike repair differs from approved source delta: %s", change.current)
		}
		source = bytes.Replace(source, []byte(change.current), []byte(change.legacy), 1)
	}
	return source
}

// legacyValidateSourceWithoutCandidateAdoption reverses only the approved exact
// retained-candidate owner adoption route. Historical releases retain their
// existing bytes, budgets, authority and automatic repair policy.
func legacyValidateSourceWithoutCandidateAdoption(t *testing.T, source []byte) []byte {
	t.Helper()
	changes := []struct{ current, legacy string }{
		{"// and runs the suite on it again. Flagged and handback candidates are retained\n// completely before reset for exact owner review; they are not adopted automatically. Three\n", "// and runs the suite on it again, and rejects a change over the limits. Three\n"},
		{`lf_outcomes="wait,create_issue,push,merge,close,implement,stale,repair,apply_repair"`, `lf_outcomes="wait,create_issue,push,merge,close,implement,stale,repair"`},
		{" // Exact owner adoption returns through the suite and all reviewers.\n owner_repair_apply [lf_action=\"lf_validate_repair_apply\", lf_outcomes=\"applied,blocked\", lf_limit=\"4\"];\n\n", ""},
		{" owner_resolve -> owner_repair_apply [lf_on=\"apply_repair\"];\n owner_repair_apply -> suite [lf_on=\"applied\"];\n owner_repair_apply -> owner_report [lf_on=\"blocked\"];\n", ""},
	}
	for _, change := range changes {
		if bytes.Count(source, []byte(change.current)) != 1 {
			t.Fatalf("canonical Validate adoption differs from approved source delta: %s", change.current)
		}
		source = bytes.Replace(source, []byte(change.current), []byte(change.legacy), 1)
	}
	return source
}
