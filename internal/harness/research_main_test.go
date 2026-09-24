package harness

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func TestResearchMainRoleEveryLaunchPath(t *testing.T) {
	t.Parallel()
	runner := &Runner{model: "router-model", tools: []string{"Read"}}
	for _, p := range []agent.RunPath{PathCold, PathResume, PathColdAfterResumeRefused} {
		req := agent.Request{Path: p, Delivery: &agent.Delivery{Root: "/retained", Identity: "hash", Roles: []string{"research"}, MainRole: "research", MainModel: "node-model"}}
		args := strings.Join(runner.args(req), " ")
		for _, want := range []string{"--agent litefactory-workflow:research", "--model node-model", "--plugin-dir /retained/plugin", "--append-system-prompt-file /retained/bootstrap.md", "--tools Read", "--strict-mcp-config"} {
			if !strings.Contains(args, want) {
				t.Errorf("%s missing %q: %s", p, want, args)
			}
		}
		if strings.Contains(args, "router-model") {
			t.Errorf("%s used dispatcher model", p)
		}
	}
}

func TestResearchMainRoleFakeProcess(t *testing.T) {
	f := newFixture(t, successScript(t))
	for _, launch := range []RunPath{PathCold, PathResume, PathColdAfterResumeRefused} {
		req := f.request()
		req.Path = launch
		req.Delivery = &agent.Delivery{Root: t.TempDir(), Identity: "effective", MainRole: "research", MainModel: "worker-model", Roles: []string{"research"}}
		result, err := f.runner.Run(t.Context(), req)
		if err != nil || !result.Succeeded() {
			t.Fatalf("%s: %v %+v", launch, err, result)
		}
		args := f.dump(t, "argv.dump")
		for _, want := range []string{"--agent\nlitefactory-workflow:research\n", "--model\nworker-model\n", "--tools\nRead,Edit\n"} {
			if !strings.Contains(args, want) {
				t.Errorf("%s missing %q", launch, want)
			}
		}
		env := f.dump(t, "env.dump")
		if strings.Contains(env, "leakcanary") {
			t.Fatalf("%s leaked environment", launch)
		}
	}
}
