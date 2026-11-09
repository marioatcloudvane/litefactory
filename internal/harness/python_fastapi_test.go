package harness

import (
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/teamsource/teamsourcetest"
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

// releasedPythonProfile reads the stack profile that `litefactory team install`
// installs for python-fastapi, resolved from teams/source.
func releasedPythonProfile(t *testing.T) []byte {
	t.Helper()
	files, err := teamsourcetest.Files(t.Context(), "python-fastapi")
	if err != nil {
		t.Fatal(err)
	}
	raw, ok := files["stack-profile.json"]
	if !ok {
		t.Fatal("python-fastapi source has no stack-profile.json")
	}
	return raw
}
