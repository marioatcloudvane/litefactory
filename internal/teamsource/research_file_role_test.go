package teamsource

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResearchFileOnlyRoleAdditionUsesExistingContract(t *testing.T) {
	root := t.TempDir()
	source := researchTestSource()
	for name, file := range source {
		target := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, file.Data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	registry := researchTestRegistry(t)
	before, err := captureHistoricalResearch(t.Context(), os.DirFS(root), registry)
	if err != nil {
		t.Fatal(err)
	}
	role := strings.ReplaceAll(string(source["research.md"].Data), "name: research", "name: extra")
	role = strings.ReplaceAll(role, "Investigate the request.", "Inspect one additional boundary using the same result contract.")
	if err = os.WriteFile(filepath.Join(root, "extra.md"), []byte(role), 0600); err != nil {
		t.Fatal(err)
	}
	graph := strings.ReplaceAll(string(source["research.dot"].Data), `work -> end [lf_on="success"];`, `extra [lf_action="lf_agent", lf_outcomes="success", lf_agent_markdown="extra", lf_skills="reader", lf_limit="2"];
 work -> extra [lf_on="success"]; extra -> end [lf_on="success"];`)
	if err = os.WriteFile(filepath.Join(root, "research.dot"), []byte(graph), 0600); err != nil {
		t.Fatal(err)
	}
	after, err := captureHistoricalResearch(t.Context(), os.DirFS(root), registry)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Nodes) != 2 || string(after.Files["extra.md"]) != role || len(after.Nodes["extra"].Skills) != len(after.Nodes["work"].Skills) {
		t.Fatalf("file-added role did not reuse captured contract: %+v", after.Nodes)
	}
	if before.Digest == after.Digest {
		t.Fatal("role addition did not rotate captured identity")
	}
	if err = os.WriteFile(filepath.Join(root, "extra.md"), []byte("changed after capture"), 0600); err != nil {
		t.Fatal(err)
	}
	if string(after.Files["extra.md"]) != role {
		t.Fatal("captured role changed with filesystem")
	}
}
