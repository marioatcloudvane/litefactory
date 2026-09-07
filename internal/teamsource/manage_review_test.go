package teamsource

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/team"
)

func overrideResourceFixture(t *testing.T) (string, string, string, Override) {
	t.Helper()
	origin := managementPackage(t)
	root := filepath.Join(t.TempDir(), "teams")
	if err := Install(t.Context(), root, "blue", origin, false); err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	writePackageFile(t, source, "reader.md", "---\nname: reader\ndescription: Reads sources\ntier: fast\nresources: [detail.txt]\n---\nOriginal override.\n")
	writePackageFile(t, source, "detail.txt", "original resource")
	o, err := Manage(t.Context(), root, "shared:reader", "set", filepath.Join(source, "reader.md"))
	if err != nil {
		t.Fatal(err)
	}
	return root, source, filepath.ToSlash(filepath.Join(filepath.Dir(o.Path), "detail.txt")), o
}

func requireStatus(t *testing.T, root, p, state string) {
	t.Helper()
	records, err := Status(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if record.Path == p && record.State == state {
			return
		}
	}
	t.Fatalf("missing %s state %s: %#v", p, state, records)
}

func TestOverrideResourceOnlyUpstreamChange(t *testing.T) {
	t.Parallel()
	root, source, p, _ := overrideResourceFixture(t)
	writePackageFile(t, source, "detail.txt", "updated resource")
	requireStatus(t, root, p, "upstream-updated")
	if err := Update(t.Context(), root, false); err != nil {
		t.Fatal(err)
	}
	b, err := readCatalogFile(t.Context(), root, p)
	if err != nil || string(b) != "updated resource" {
		t.Fatalf("resource not updated: %s %v", b, err)
	}
	requireStatus(t, root, p, "in-sync")
}

func TestOverrideLocalResourceConflictRequiresForce(t *testing.T) {
	t.Parallel()
	root, source, p, o := overrideResourceFixture(t)
	writePackageFile(t, root, p, "local resource")
	writePackageFile(t, source, "reader.md", "---\nname: reader\ndescription: Reads sources\ntier: fast\nresources: [detail.txt]\n---\nChanged root.\n")
	requireStatus(t, root, p, "locally-modified")
	before, err := os.ReadFile(filepath.Join(root, metadataPath))
	if err != nil {
		t.Fatal(err)
	}
	if err := Update(t.Context(), root, false); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("local dependency conflict: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(root, metadataPath))
	if err != nil || string(before) != string(after) {
		t.Fatal("refused override update changed provenance", err)
	}
	if err := Update(t.Context(), root, true); err != nil {
		t.Fatal(err)
	}
	current, err := Manage(t.Context(), root, "shared:reader", "show", "")
	if err != nil {
		t.Fatal(err)
	}
	if current.Path == o.Path {
		t.Fatal("root update did not select new override")
	}
	nextResource := filepath.ToSlash(filepath.Join(filepath.Dir(current.Path), "detail.txt"))
	b, err := readCatalogFile(t.Context(), root, nextResource)
	if err != nil || string(b) != "original resource" {
		t.Fatal(string(b), err)
	}
}

func TestOverrideMissingInstalledResourceRestored(t *testing.T) {
	t.Parallel()
	root, _, p, _ := overrideResourceFixture(t)
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(p))); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, root, p, "missing")
	if err := Update(t.Context(), root, false); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, root, p, "in-sync")
}

func TestOverrideMissingOriginResourceIsVisible(t *testing.T) {
	t.Parallel()
	root, source, p, _ := overrideResourceFixture(t)
	if err := os.Remove(filepath.Join(source, "detail.txt")); err != nil {
		t.Fatal(err)
	}
	status, err := Status(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range status {
		if r.Path == p && r.Error != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing upstream resource invisible: %#v", status)
	}
	if err := Update(t.Context(), root, false); err == nil {
		t.Fatal("missing origin dependency ignored")
	}
	if _, err := CaptureRoot(t.Context(), root, "blue", "research"); err != nil {
		t.Fatal("installed closure stopped working", err)
	}
}

func TestAbsoluteContainedImportIsPortable(t *testing.T) {
	t.Parallel()
	origin := managementPackage(t)
	idx, err := readIndex(t.Context(), origin)
	if err != nil {
		t.Fatal(err)
	}
	shared := idx.Shared["reader"]
	shared.Path = filepath.Join(origin, "agents/shared/reader.md")
	idx.Shared["reader"] = shared
	entry := idx.Teams["blue"]
	entry.Plan = []team.AgentEntry{{ID: "writer", Desc: "Writes sources", Path: filepath.Join(origin, "agents/blue/writer.md"), Tier: "balanced"}}
	idx.Teams["blue"] = entry
	writePackageFile(t, origin, "agents/blue/writer.md", "---\nname: writer\ndescription: Writes sources\ntier: balanced\n---\nWrite.\n")
	data, err := json.Marshal(idx)
	if err != nil {
		t.Fatal(err)
	}
	writePackageFile(t, origin, "index.json", string(data))
	root := filepath.Join(t.TempDir(), "teams")
	if err := Install(t.Context(), root, "blue", origin, false); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(root, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	installed, err := team.Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.IsAbs(installed.Shared["reader"].Path) || filepath.IsAbs(installed.Teams["blue"].Plan[0].Path) {
		t.Fatal("origin paths retained", string(b))
	}
	m, err := Metadata(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.IsAbs(m.Teams["blue"].Shared["reader"].Path) || filepath.IsAbs(m.Teams["blue"].Definition.Plan[0].Path) {
		t.Fatal("base provenance paths not portable")
	}
	if err := os.Rename(origin, origin+"-gone"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(origin + "-gone") })
	for _, mode := range []string{"research", "plan"} {
		if _, err := CaptureRoot(t.Context(), root, "blue", mode); err != nil {
			t.Fatal(mode, err)
		}
	}
	source := t.TempDir()
	writePackageFile(t, source, "reader.md", packageAgent("override"))
	if _, err := Manage(t.Context(), root, "shared:reader", "set", filepath.Join(source, "reader.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := Manage(t.Context(), root, "shared:reader", "unset", ""); err != nil {
		t.Fatal(err)
	}
}

func TestExistingAbsoluteCatalogCanUpdateAndManage(t *testing.T) {
	t.Parallel()
	origin := managementPackage(t)
	root := filepath.Join(t.TempDir(), "teams")
	if err := Install(t.Context(), root, "blue", origin, false); err != nil {
		t.Fatal(err)
	}
	idx, err := readIndex(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	a := idx.Shared["reader"]
	a.Path = filepath.Join(root, a.Path)
	idx.Shared["reader"] = a
	b, err := json.Marshal(idx)
	if err != nil {
		t.Fatal(err)
	}
	writePackageFile(t, root, "index.json", string(b))
	s, err := CaptureRoot(t.Context(), root, "blue", "research")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.IsAbs(s.Agents[0].Definition.Path) {
		t.Fatal("snapshot definition not portable")
	}
	source := t.TempDir()
	writePackageFile(t, source, "reader.md", packageAgent("override"))
	o, err := Manage(t.Context(), root, "shared:reader", "set", filepath.Join(source, "reader.md"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.IsAbs(o.Original.Path) {
		t.Fatal("override retained absolute base")
	}
	if err := Update(t.Context(), root, false); err != nil {
		t.Fatal(err)
	}
	if _, err := Manage(t.Context(), root, "shared:reader", "unset", ""); err != nil {
		t.Fatal(err)
	}
}

func TestRootOnlyTeamReportsMissingOrigin(t *testing.T) {
	t.Parallel()
	origin := managementPackage(t)
	idx, err := readIndex(t.Context(), origin)
	if err != nil {
		t.Fatal(err)
	}
	entry := idx.Teams["blue"]
	entry.Research = []team.AgentEntry{}
	idx.Teams["blue"] = entry
	b, err := json.Marshal(idx)
	if err != nil {
		t.Fatal(err)
	}
	writePackageFile(t, origin, "index.json", string(b))
	root := filepath.Join(t.TempDir(), "teams")
	if err := Install(t.Context(), root, "blue", origin, false); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(origin, "index.json")); err != nil {
		t.Fatal(err)
	}
	status, err := Status(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(status) != 1 || status[0].Error == "" || status[0].Team != "blue" {
		t.Fatalf("missing root-only origin invisible: %#v", status)
	}
}

func TestIdenticalInlineAgentOverrideRoundTripPreservesModes(t *testing.T) {
	t.Parallel()
	origin := managementPackage(t)
	idx, err := readIndex(t.Context(), origin)
	if err != nil {
		t.Fatal(err)
	}
	a := team.AgentEntry{ID: "reader", Desc: "Reads sources", Path: "agents/blue/reader.md", Tier: "fast"}
	entry := idx.Teams["blue"]
	entry.Research = []team.AgentEntry{a}
	entry.Plan = []team.AgentEntry{a}
	idx.Teams["blue"] = entry
	b, err := json.Marshal(idx)
	if err != nil {
		t.Fatal(err)
	}
	writePackageFile(t, origin, "index.json", string(b))
	writePackageFile(t, origin, a.Path, packageAgent("inline base"))
	root := filepath.Join(t.TempDir(), "teams")
	if err := Install(t.Context(), root, "blue", origin, false); err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	writePackageFile(t, source, "reader.md", packageAgent("inline override"))
	if _, err := Manage(t.Context(), root, "team:blue:reader", "set", filepath.Join(source, "reader.md")); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"research", "plan"} {
		s, err := CaptureRoot(t.Context(), root, "blue", mode)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(s.Agents[0].Source.Body, "inline override") {
			t.Fatal(mode, "did not select override")
		}
	}
	if _, err := Manage(t.Context(), root, "team:blue:reader", "unset", ""); err != nil {
		t.Fatal(err)
	}
	actual, err := readIndex(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"research", "plan"} {
		roster, _ := actual.Teams["blue"].Mode(mode)
		if len(roster) != 1 || roster[0].Path != a.Path || roster[0].Tier != a.Tier || roster[0].Desc != a.Desc {
			t.Fatalf("%s changed after round-trip: %#v", mode, roster)
		}
	}
}
