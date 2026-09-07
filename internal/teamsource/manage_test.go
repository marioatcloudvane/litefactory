package teamsource

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/team"
)

func writePackageFile(t *testing.T, root, p, body string) {
	t.Helper()
	name := filepath.Join(root, filepath.FromSlash(p))
	if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
func packageAgent(body string) string {
	return "---\nname: reader\ndescription: Reads sources\ntier: fast\n---\n" + body + "\n"
}
func managementPackage(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	a := team.AgentEntry{ID: "reader", Desc: "Reads sources", Path: "agents/shared/reader.md", Tier: "fast"}
	tm := team.TeamEntry{Desc: "Test team", Research: []team.AgentEntry{{Ref: "reader"}}, Plan: []team.AgentEntry{}, Implement: []team.AgentEntry{}, Validate: []team.AgentEntry{}, Spike: []team.AgentEntry{}}
	idx := team.Index{SchemaVersion: 2, Teams: map[string]team.TeamEntry{"blue": tm}, Shared: map[string]team.AgentEntry{"reader": a}}
	b, err := json.Marshal(idx)
	if err != nil {
		t.Fatal(err)
	}
	writePackageFile(t, root, "index.json", string(b))
	writePackageFile(t, root, a.Path, packageAgent("original"))
	return root
}

func TestCentralInstallOriginIndependence(t *testing.T) {
	t.Parallel()
	origin := managementPackage(t)
	root := filepath.Join(t.TempDir(), "teams")
	if err := Install(t.Context(), root, "blue", origin, false); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(origin, origin+"-removed"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(origin + "-removed") })
	s, err := CaptureRoot(t.Context(), root, "blue", "research")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Agents) != 1 {
		t.Fatal(s)
	}
	status, err := Status(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(status) != 1 || status[0].Error == "" {
		t.Fatalf("missing origin must be actionable: %#v", status)
	}
	if err := Update(t.Context(), root, false); err == nil {
		t.Fatal("update with removed origin succeeded")
	}
	if _, err := CaptureRoot(t.Context(), root, "blue", "research"); err != nil {
		t.Fatal(err)
	}
}

func TestCentralDriftAndForce(t *testing.T) {
	for _, tc := range []struct {
		name, local, upstream, state string
		missing                      bool
	}{{"same", "original", "original", "in-sync", false}, {"local", "local", "original", "locally-modified", false}, {"upstream", "original", "next", "upstream-updated", false}, {"both", "local", "next", "conflict", false}, {"missing", "", "original", "missing", true}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			origin := managementPackage(t)
			root := filepath.Join(t.TempDir(), "teams")
			if err := Install(t.Context(), root, "blue", origin, false); err != nil {
				t.Fatal(err)
			}
			if tc.missing {
				if err := os.Remove(filepath.Join(root, "agents/shared/reader.md")); err != nil {
					t.Fatal(err)
				}
			} else {
				writePackageFile(t, root, "agents/shared/reader.md", packageAgent(tc.local))
			}
			writePackageFile(t, origin, "agents/shared/reader.md", packageAgent(tc.upstream))
			status, err := Status(t.Context(), root)
			if err != nil {
				t.Fatal(err)
			}
			if len(status) != 1 || status[0].State != tc.state {
				t.Fatalf("got %#v, want %s", status, tc.state)
			}
			err = Update(t.Context(), root, false)
			unsafe := tc.state == "locally-modified" || tc.state == "conflict"
			if (err != nil) != unsafe {
				t.Fatalf("update err=%v unsafe=%v", err, unsafe)
			}
			if unsafe {
				if err := Update(t.Context(), root, true); err != nil {
					t.Fatal(err)
				}
			}
			status, err = Status(t.Context(), root)
			if err != nil || status[0].State != "in-sync" {
				t.Fatalf("after update %#v %v", status, err)
			}
		})
	}
}

func TestCentralPreflightMissingResourceWritesNothing(t *testing.T) {
	t.Parallel()
	origin := managementPackage(t)
	root := filepath.Join(t.TempDir(), "teams")
	writePackageFile(t, origin, "agents/shared/reader.md", "---\nname: reader\ndescription: Reads sources\ntier: fast\nresources: [missing.txt]\n---\nRead.\n")
	if err := Install(t.Context(), root, "blue", origin, false); err == nil {
		t.Fatal("missing resource accepted")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("failed source preflight created catalog: %v", err)
	}
}

func TestCentralOverrideSurvivesBaseUpdateAndUnsets(t *testing.T) {
	t.Parallel()
	origin := managementPackage(t)
	root := filepath.Join(t.TempDir(), "teams")
	if err := Install(t.Context(), root, "blue", origin, false); err != nil {
		t.Fatal(err)
	}
	overrideDir := t.TempDir()
	source := filepath.Join(overrideDir, "reader.md")
	writePackageFile(t, overrideDir, "reader.md", packageAgent("override"))
	o, err := Manage(t.Context(), root, "shared:reader", "set", source)
	if err != nil {
		t.Fatal(err)
	}
	if o.Origin != source || o.Hash == "" {
		t.Fatal(o)
	}
	writePackageFile(t, origin, "agents/shared/reader.md", packageAgent("upstream"))
	if err := Update(t.Context(), root, false); err != nil {
		t.Fatal(err)
	}
	s, err := CaptureRoot(t.Context(), root, "blue", "research")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s.Agents[0].Source.Body, "override") {
		t.Fatal("base update replaced override")
	}
	if _, err := Manage(t.Context(), root, "shared:reader", "unset", ""); err != nil {
		t.Fatal(err)
	}
	s, err = CaptureRoot(t.Context(), root, "blue", "research")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s.Agents[0].Source.Body, "upstream") {
		t.Fatal("unset failed to restore updated base")
	}
}

func TestCentralEmbeddedInstall(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "teams")
	if err := Install(t.Context(), root, "go-team", "", false); err != nil {
		t.Fatal(err)
	}
	for _, mode := range team.Modes() {
		if _, err := CaptureRoot(t.Context(), root, "go-team", mode); err != nil {
			t.Fatal(mode, err)
		}
	}
}

func TestCentralConflictingSharedImportDoesNotPublish(t *testing.T) {
	t.Parallel()
	origin := managementPackage(t)
	root := filepath.Join(t.TempDir(), "teams")
	if err := Install(t.Context(), root, "blue", origin, false); err != nil {
		t.Fatal(err)
	}
	idx, err := readIndex(t.Context(), origin)
	if err != nil {
		t.Fatal(err)
	}
	idx.Teams["red"] = idx.Teams["blue"]
	data, err := json.Marshal(idx)
	if err != nil {
		t.Fatal(err)
	}
	writePackageFile(t, origin, "index.json", string(data))
	if err := Install(t.Context(), root, "red", origin, false); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	writePackageFile(t, origin, "agents/shared/reader.md", packageAgent("conflicting shared source"))
	if err := Install(t.Context(), root, "red", origin, true); err == nil {
		t.Fatal("shared conflict accepted despite another installed owner")
	}
	after, err := os.ReadFile(filepath.Join(root, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("failed import published registry")
	}
}

func TestCentralRegistryMetadataDriftNeedsForce(t *testing.T) {
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
	entry := idx.Teams["blue"]
	entry.Desc = "local description"
	idx.Teams["blue"] = entry
	b, err := json.Marshal(idx)
	if err != nil {
		t.Fatal(err)
	}
	writePackageFile(t, root, "index.json", string(b))
	status, err := Status(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range status {
		if item.Path == "index.json#blue" && item.State == "locally-modified" {
			found = true
		}
	}
	if !found {
		t.Fatalf("metadata drift unreported: %#v", status)
	}
	if err := Update(t.Context(), root, false); err == nil {
		t.Fatal("metadata local edit lost without force")
	}
	if err := Update(t.Context(), root, true); err != nil {
		t.Fatal(err)
	}
}

func TestCentralOverrideCopiesResourceClosure(t *testing.T) {
	t.Parallel()
	origin := managementPackage(t)
	root := filepath.Join(t.TempDir(), "teams")
	if err := Install(t.Context(), root, "blue", origin, false); err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	writePackageFile(t, source, "reader.md", "---\nname: reader\ndescription: Reads sources\ntier: fast\nresources: [details.txt]\n---\noverride\n")
	writePackageFile(t, source, "details.txt", "supporting detail")
	o, err := Manage(t.Context(), root, "shared:reader", "set", filepath.Join(source, "reader.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(source, "details.txt")); err != nil {
		t.Fatal(err)
	}
	s, err := CaptureRoot(t.Context(), root, "blue", "research")
	if err != nil {
		t.Fatal(err)
	}
	resource := filepath.ToSlash(filepath.Join(filepath.Dir(o.Path), "details.txt"))
	if string(s.Files[resource]) != "supporting detail" {
		t.Fatal("override resource was not captured", s.Files)
	}
}
