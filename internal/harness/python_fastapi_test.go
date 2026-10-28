package harness

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
)

func TestClaudeRecordsReleasedPythonFastAPIProfile(t *testing.T) {
	raw := releasedPythonProfile(t)
	profile, err := config.ResolveStackProfile(raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	f := newFixture(t, successScript(t))
	req := f.request()
	req.StackProfile = &profile
	if _, err := f.runner.Run(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	dump := f.dump(t, "env.dump")
	for _, want := range []string{"CI=1", "PYTHONDONTWRITEBYTECODE=1"} {
		if !strings.Contains(dump, want) {
			t.Fatal("released environment absent", want)
		}
	}
	if err := profile.ValidateRetained(); err != nil {
		t.Fatal("provider changed captured profile", err)
	}
	// The profile may list shared guidance (owner-report) first; the resolver
	// keeps the declared order, so look the stack guidance up by ID.
	if !slices.ContainsFunc(profile.Effective.Guidance, func(g config.StackGuidance) bool { return g.ID == "python-fastapi" }) {
		t.Fatal("python-fastapi guidance not selected")
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
