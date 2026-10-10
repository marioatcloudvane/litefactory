package harness

import (
	"os"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
)

func TestClaudeRecordsReleasedPythonFastAPIProfile(t *testing.T) {
	raw, err := os.ReadFile("../../teams/python-fastapi/v1.0.0/stack-profile.json")
	if err != nil {
		t.Fatal(err)
	}
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
	if profile.Effective.Guidance[0].ID != "python-fastapi" {
		t.Fatal("wrong selected guidance")
	}
}
