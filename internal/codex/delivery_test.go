package codex

import (
	"github.com/cloudvane/litefactory/internal/agent"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeliveryArgsForceIsolationAndNativeRoleFiles(t *testing.T) {
	t.Parallel()
	runner := &Runner{userConfig: true}
	req := request(t, agent.PathCold)
	req.Delivery = &agent.Delivery{Root: t.TempDir(), Identity: strings.Repeat("a", 64), Roles: []string{"researcher"}}
	argv, err := runner.args(req, "read-only")
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(argv, " ")
	for _, want := range []string{"--ignore-user-config", "--ignore-rules", "agents.researcher.config_file", filepath.Join(req.Delivery.Root, "roles", "researcher.toml"), "read-only"} {
		if !strings.Contains(args, want) {
			t.Errorf("missing %q: %s", want, args)
		}
	}
	if strings.Contains(args, "SPECIALIST_BODY") || strings.Contains(args, "workspace-write") {
		t.Fatalf("unsafe argv: %s", args)
	}
}
