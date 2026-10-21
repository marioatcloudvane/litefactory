package teampackage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/graphexec"
)

// TestShippedCatalogueMatchesItsFiles keeps teams/teams.json installable: every
// listed file exists with its hash, nothing unlisted ships, and each release
// preserves its checksummed immutable bytes. Only the canonical current release
// carries the same role and workflow bytes as the editable teamsource, except
// for the pending Bug and validated Implement sync gates; releasing sources
// remains owner-controlled.
func TestShippedCatalogueMatchesItsFiles(t *testing.T) {
	raw, err := os.ReadFile("../../teams/teams.json")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := parseCatalogue(raw)
	if err != nil {
		t.Fatalf("teams.json does not parse: %v", err)
	}
	canonicalRaw, err := os.ReadFile("../teamsource/release.json")
	if err != nil {
		t.Fatal(err)
	}
	var canonical Release
	if err := json.Unmarshal(canonicalRaw, &canonical); err != nil {
		t.Fatal(err)
	}
	foundCanonical := false
	for _, r := range cat.Releases {
		if r.Slug == "standard" {
			t.Errorf("release %s: there is no standard team; name it after what it is", r.Version)
		}
		current := r.Slug == canonical.Slug && r.Version == canonical.Version
		if current {
			foundCanonical = true
		}
		root := filepath.Join("../../teams", filepath.FromSlash(r.Download))
		listed := map[string]bool{}
		for _, f := range r.Files {
			listed[f.Path] = true
			b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f.Path)))
			if err != nil {
				t.Errorf("%s: %v", f.Path, err)
				continue
			}
			if sum := sha256.Sum256(b); hex.EncodeToString(sum[:]) != f.SHA256 {
				t.Errorf("%s/%s: listed hash is stale", r.Slug, f.Path)
			}
			if !current || f.Path == "team.config" || f.Path == "GENERATED.md" {
				continue
			}
			live, err := os.ReadFile(filepath.Join("../teamsource", filepath.FromSlash(f.Path)))
			pendingBug := pendingBugSyncSourceMatches(r, f.Path, b, live)
			pendingWorker := f.Path == "implement/implement.dot" && pendingWorkerSyncMatchesRelease(live, b)
			if err != nil || (string(live) != string(b) && !pendingBug && !pendingWorker) {
				t.Errorf("%s/%s differs from internal/teamsource/%s", r.Slug, f.Path, f.Path)
			}
		}
		if err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(root, p)
			if rel = filepath.ToSlash(rel); !listed[rel] {
				t.Errorf("%s/%s ships but is not listed", r.Slug, rel)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if !foundCanonical {
		t.Fatal("canonical current release missing from catalogue")
	}
}

// pendingBugSyncSourceMatches permits only the approved, unreleased gate delta.
// The catalogue hashes above continue to protect every released byte; comparing
// the full reconstructed source keeps unrelated source changes visible.
func pendingBugSyncSourceMatches(release Release, path string, shipped, live []byte) bool {
	if release.Slug != "go-development" || release.Version != "1.0.2" || path != "bug/bug.dot" {
		return false
	}
	expected := string(shipped)
	for _, change := range []struct{ from, to string }{
		{` fixer [lf_action="lf_agent"`, ` sync_before_fixer [lf_action="lf_sync_base",lf_outcomes="synced,current,conflict,retry",lf_limit="8",lf_park="owner",lf_stop="conflict,failed,invalid,exhausted"];` + "\n" + ` fixer [lf_action="lf_agent"`},
		{` conversation -> fixer [lf_on="approved"];`, ` conversation -> sync_before_fixer [lf_on="approved"];`},
		{` finalization -> fixer [lf_on="revalidate"];`, ` finalization -> sync_before_fixer [lf_on="revalidate"];` + "\n" + ` sync_before_fixer -> fixer [lf_on="synced"];` + "\n" + ` sync_before_fixer -> fixer [lf_on="current"];` + "\n" + ` sync_before_fixer -> sync_before_fixer [lf_on="retry"];`},
	} {
		if strings.Count(expected, change.from) != 1 {
			return false
		}
		expected = strings.Replace(expected, change.from, change.to, 1)
	}
	return string(live) == expected
}

// Editable Implement sources may carry the worker gate before the owner releases
// them. Removing that validated addition must recover the released behavior;
// every other execution attribute and coordinator route still has to match.
func pendingWorkerSyncMatchesRelease(live, released []byte) bool {
	current, err := graphexec.ParseProfile(live)
	if err != nil || graphexec.ValidateImplementGraph(current) != nil {
		return false
	}
	previous, err := graphexec.ParseProfile(released)
	if err != nil {
		return false
	}
	gate, gated := graphexec.ImplementWorkerSyncNode(current)
	if !gated {
		return false
	}
	if _, alreadyGated := graphexec.ImplementWorkerSyncNode(previous); alreadyGated {
		return false
	}
	worker, ok := graphexec.Decide(current, gate.Name, "synced")
	if !ok {
		return false
	}
	normalized := *current
	normalized.Nodes = nil
	for _, node := range current.Nodes {
		if node.Name != gate.Name {
			normalized.Nodes = append(normalized.Nodes, node)
		}
	}
	normalized.Edges = nil
	for _, edge := range current.Edges {
		if edge.From == gate.Name {
			continue
		}
		if edge.To == gate.Name {
			edge.To = worker
		}
		normalized.Edges = append(normalized.Edges, edge)
	}
	want, err := graphexec.SemanticDigest(previous)
	if err != nil {
		return false
	}
	got, err := graphexec.SemanticDigest(&normalized)
	return err == nil && got == want
}

func TestPendingWorkerSyncSourceParityRejectsUnrelatedChanges(t *testing.T) {
	released, err := os.ReadFile("../../teams/go-development/v1.0.2/implement/implement.dot")
	if err != nil {
		t.Fatal(err)
	}
	// The canonical worker gate is now released. Keep testing the narrow
	// historical pending-change exception against its ungated baseline rather
	// than comparing later, independently released coordinator changes to it.
	live := strings.Replace(string(released), " // ---- Worker", " sync_before_worker [lf_action=\"lf_sync_base\", lf_outcomes=\"synced,current,conflict,retry\", lf_limit=\"8\"];\n // ---- Worker", 1)
	live = strings.Replace(live, "budget        -> worker_agent  [lf_on=\"run\"];", "budget -> sync_before_worker [lf_on=\"run\"];\n sync_before_worker -> worker_agent [lf_on=\"synced\"];\n sync_before_worker -> worker_agent [lf_on=\"current\"];\n sync_before_worker -> sync_before_worker [lf_on=\"retry\"];\n sync_before_worker -> park_story [lf_on=\"conflict\"];", 1)
	if !pendingWorkerSyncMatchesRelease([]byte(live), released) {
		t.Fatal("source worker gate did not preserve released behavior")
	}
	for _, mutation := range [][2]string{
		{`lf_limit="64"`, `lf_limit="63"`},
		{`lf_limit="3"`, `lf_limit="2"`},
		{`lf_limit="8"]`, `lf_limit="7"]`},
	} {
		changed := strings.Replace(live, mutation[0], mutation[1], 1)
		if changed == live {
			t.Fatalf("mutation did not change source: %v", mutation)
		}
		if pendingWorkerSyncMatchesRelease([]byte(changed), released) {
			t.Fatalf("unrelated source change admitted: %v", mutation)
		}
	}
}

// TestShippedTeamInstallsFromTheCatalogueFile installs the Go team the way an
// operator does for a private repository: from the checkout's catalogue file.
func TestShippedTeamInstallsFromTheCatalogueFile(t *testing.T) {
	catalogue, err := filepath.Abs("../../teams/teams.json")
	if err != nil {
		t.Fatal(err)
	}
	m, err := New(Options{Paths: config.NewPaths(canonicalTemp(t)), Validate: func(ctx context.Context, source fs.FS, workflows map[string]string) error {
		for _, graph := range workflows {
			if _, err := fs.ReadFile(source, graph); err != nil {
				return err
			}
		}
		return ctx.Err()
	}})
	if err != nil {
		t.Fatal(err)
	}
	got, already, err := m.Install(t.Context(), catalogue, "go-development", "")
	if err != nil || already || !got.Integrity || got.Identity.Source != "file://"+filepath.ToSlash(catalogue) {
		t.Fatalf("install: %+v already=%v %v", got.Identity, already, err)
	}
	if len(got.Workflows) != 6 {
		t.Fatalf("workflows: %v", got.Workflows)
	}
	if _, again, err := m.Install(t.Context(), "file://"+filepath.ToSlash(catalogue), "go-development", got.Identity.Version); err != nil || !again {
		t.Fatalf("reinstall: already=%v %v", again, err)
	}
}
