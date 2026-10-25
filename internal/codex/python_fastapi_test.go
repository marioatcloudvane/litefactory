package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
)

func TestCodexRecordsReleasedPythonFastAPIProfile(t *testing.T) {
	raw := releasedPythonProfile(t)
	profile, err := config.ResolveStackProfile(raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	runner, _ := fakeRunner(t, `env > python-profile.dump
printf '%s\n' '{"type":"turn.completed"}'`)
	req := request(t, agent.PathCold)
	req.StackProfile = &profile
	if _, err := runner.Run(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	dump, err := os.ReadFile(filepath.Join(req.WorktreePath, "python-profile.dump"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"CI=1", "PYTHONDONTWRITEBYTECODE=1"} {
		if !strings.Contains(string(dump), want) {
			t.Fatal("released environment absent", want)
		}
	}
	if err := profile.ValidateRetained(); err != nil {
		t.Fatal("provider changed captured profile", err)
	}
	if profile.Effective.Guidance[0].ID != "python-fastapi" {
		t.Fatal("wrong selected guidance")
	}
}

// releasedPythonProfile reads the stack profile of the python-fastapi release the
// canonical source currently names, so the test follows the owner's releases.
func releasedPythonProfile(t *testing.T) []byte {
	t.Helper()
	meta, err := os.ReadFile("../../teams/source/python-fastapi/release.json")
	if err != nil {
		t.Fatal(err)
	}
	var release struct {
		Download string `json:"download"`
	}
	if err := json.Unmarshal(meta, &release); err != nil || !strings.HasPrefix(release.Download, "python-fastapi/") || strings.Contains(release.Download, "..") {
		t.Fatalf("python-fastapi release metadata: %q %v", release.Download, err)
	}
	raw, err := os.ReadFile("../../teams/" + release.Download + "/stack-profile.json")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
