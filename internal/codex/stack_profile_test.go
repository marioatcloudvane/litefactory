package codex

import (
	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexConsumesResolvedStackEnvironment(t *testing.T) {
	runner, _ := fakeRunner(t, `env > stack-environment.dump
printf '%s\n' '{"type":"turn.completed"}'`)
	profile, err := config.ResolveStackProfile([]byte(`{"version":1,"guidance":[{"id":"stack","skill":"plan/skills/stack/SKILL.md"}],"environment":[{"name":"CI","value":"1"},{"name":"GOTOOLCHAIN","value":"local"}]}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	req := request(t, agent.PathCold)
	req.StackProfile = &profile
	runner.environ = append(runner.environ, "GH_TOKEN=private-canary", "CI=false")
	if _, err = runner.Run(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(req.WorktreePath, "stack-environment.dump"))
	if err != nil {
		t.Fatal(err)
	}
	dump := string(raw)
	for _, want := range []string{"CI=1", "GOTOOLCHAIN=local"} {
		if !strings.Contains(dump, want) {
			t.Fatal("missing resolved profile setting", want)
		}
	}
	if strings.Contains(dump, "private-canary") || strings.Contains(dump, "CI=false") {
		t.Fatal("ambient settings replaced admitted profile")
	}
}
func TestCodexTamperedProfileStartsNoProcess(t *testing.T) {
	runner, argv := fakeRunner(t, `printf '%s\n' '{"type":"turn.completed"}'`)
	profile, err := config.ResolveStackProfile([]byte(`{"version":1,"guidance":[{"id":"stack","skill":"plan/skills/stack/SKILL.md"}]}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	profile.EffectiveDigest = "tampered"
	req := request(t, agent.PathCold)
	req.StackProfile = &profile
	if _, err = runner.Run(t.Context(), req); err == nil {
		t.Fatal("tampered capture accepted")
	}
	if _, err = os.Stat(argv); !os.IsNotExist(err) {
		t.Fatal("provider started before capture validation", err)
	}
}
