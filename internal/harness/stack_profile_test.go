package harness

import (
	"github.com/cloudvane/litefactory/internal/config"
	"os"
	"strings"
	"testing"
)

func TestClaudeConsumesResolvedStackEnvironment(t *testing.T) {
	f := newFixture(t, successScript(t))
	profile, err := config.ResolveStackProfile([]byte(`{"version":1,"guidance":[{"id":"stack","skill":"plan/skills/stack/SKILL.md"}],"environment":[{"name":"CI","value":"1"},{"name":"GOTOOLCHAIN","value":"local"}]}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	req := f.request()
	req.StackProfile = &profile
	if _, err = f.runner.Run(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	dump := f.dump(t, "env.dump")
	for _, want := range []string{"CI=1", "GOTOOLCHAIN=local"} {
		if !strings.Contains(dump, want) {
			t.Fatal("missing resolved profile setting", want)
		}
	}
	if strings.Contains(dump, "leakcanary") {
		t.Fatal("ambient credentials reached provider")
	}
}
func TestClaudeTamperedProfileStartsNoProcess(t *testing.T) {
	f := newFixture(t, successScript(t))
	profile, err := config.ResolveStackProfile([]byte(`{"version":1,"guidance":[{"id":"stack","skill":"plan/skills/stack/SKILL.md"}]}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	profile.EffectiveDigest = "tampered"
	req := f.request()
	req.StackProfile = &profile
	if _, err = f.runner.Run(t.Context(), req); err == nil {
		t.Fatal("tampered capture accepted")
	}
	// Fake provider writes this file only if it starts.
	if files, err := os.ReadDir(f.worktree); err != nil {
		t.Fatal(err)
	} else {
		for _, file := range files {
			if file.Name() == "env.dump" {
				t.Fatal("provider started before capture validation")
			}
		}
	}
}
