package harness

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func verifiedClaudeDelivery(t *testing.T) *agent.Delivery {
	t.Helper()
	d := &agent.Delivery{Root: t.TempDir(), Identity: "effective", MainRole: "research", MainModel: "worker-model", Roles: []string{"research"}, Evidence: agent.DeliveryEvidence{Specialists: []agent.SpecialistEvidence{{ID: "research", Model: "worker-model"}}}, RenderedFiles: map[string]string{}}
	for path, body := range map[string]string{
		"bootstrap.md":                      "Select the delivered role.",
		"plugin/.claude-plugin/plugin.json": `{"name":"litefactory-workflow"}`,
		"plugin/agents/research.md":         "---\nname: research\nmodel: worker-model\n---\nDelivered research role.",
	} {
		name := filepath.Join(d.Root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		d.RenderedFiles[path] = fmt.Sprintf("%x", sha256.Sum256([]byte(body)))
	}
	return d
}

func TestClaudeDeliveryRefusesBeforeProcess(t *testing.T) {
	cases := []struct {
		name      string
		mutate    func(*testing.T, *agent.Delivery)
		cancelled bool
	}{
		{name: "missing", mutate: func(t *testing.T, d *agent.Delivery) {
			if err := os.Remove(filepath.Join(d.Root, "bootstrap.md")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "changed", mutate: func(t *testing.T, d *agent.Delivery) {
			if err := os.WriteFile(filepath.Join(d.Root, "bootstrap.md"), []byte("changed"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "extra", mutate: func(t *testing.T, d *agent.Delivery) {
			if err := os.WriteFile(filepath.Join(d.Root, "extra.md"), []byte("extra"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "symlink", mutate: func(t *testing.T, d *agent.Delivery) {
			p := filepath.Join(d.Root, "bootstrap.md")
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("plugin/agents/research.md", p); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "nonregular", mutate: func(t *testing.T, d *agent.Delivery) {
			p := filepath.Join(d.Root, "bootstrap.md")
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(p, 0o700); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "unsafe", mutate: func(t *testing.T, d *agent.Delivery) { d.RenderedFiles["../escape"] = d.RenderedFiles["bootstrap.md"] }},
		{name: "read limit", mutate: func(t *testing.T, d *agent.Delivery) {
			f, err := os.OpenFile(filepath.Join(d.Root, "bootstrap.md"), os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.Truncate(5 << 20); err != nil {
				t.Fatal(err)
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "cancelled", cancelled: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, successScript(t))
			req := f.request()
			req.Delivery = verifiedClaudeDelivery(t)
			if tc.mutate != nil {
				tc.mutate(t, req.Delivery)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.cancelled {
				cancel()
			}
			result, err := f.runner.Run(ctx, req)
			if err == nil || result != nil {
				t.Fatalf("expected delivery refusal, got %v %+v", err, result)
			}
			if _, err := os.Stat(filepath.Join(f.worktree, "argv.dump")); !os.IsNotExist(err) {
				t.Fatalf("provider started: %v", err)
			}
		})
	}
}

func TestClaudeDeliveryReverifiedEveryLaunchPath(t *testing.T) {
	for _, path := range []RunPath{PathCold, PathResume, PathColdAfterResumeRefused} {
		t.Run(path.String(), func(t *testing.T) {
			f := newFixture(t, successScript(t))
			req := f.request()
			req.Path = path
			req.Delivery = verifiedClaudeDelivery(t)
			if result, err := f.runner.Run(t.Context(), req); err != nil || !result.Succeeded() {
				t.Fatalf("valid delivery: %v %+v", err, result)
			}
			if err := os.Remove(filepath.Join(f.worktree, "argv.dump")); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(req.Delivery.Root, "bootstrap.md"), []byte("changed after first attempt"), 0o600); err != nil {
				t.Fatal(err)
			}
			req.RunOrdinal++
			if result, err := f.runner.Run(t.Context(), req); err == nil || result != nil {
				t.Fatalf("second launch did not refuse: %v %+v", err, result)
			}
			if _, err := os.Stat(filepath.Join(f.worktree, "argv.dump")); !os.IsNotExist(err) {
				t.Fatalf("provider restarted: %v", err)
			}
		})
	}
}

func TestClaudeVerifiedSelectionPreservesPermissions(t *testing.T) {
	f := newFixture(t, successScript(t))
	req := f.request()
	req.Delivery = verifiedClaudeDelivery(t)
	req.Invocation = agent.InvocationGraphNode
	req.Graph = &agent.GraphInvocation{InstanceID: 1, AttemptID: 1, RunID: 1, Node: "worker"}
	req.Workflow = agent.WorkflowSpike
	if result, err := f.runner.Run(t.Context(), req); err != nil || !result.Succeeded() {
		t.Fatalf("launch: %v %+v", err, result)
	}
	argv := f.dump(t, "argv.dump")
	for _, want := range []string{"--setting-sources\n\n", "--agent\nlitefactory-workflow:research\n", "--permission-mode\n" + PermissionMode + "\n", "--tools\nRead,Edit\n", "--allowedTools\nRead,Edit(./**)\n", "--strict-mcp-config\n"} {
		if !strings.Contains(argv, want) {
			t.Errorf("missing permission or native selection %q: %s", want, argv)
		}
	}
}

func TestClaudeGraphRequiresDelivery(t *testing.T) {
	f := newFixture(t, successScript(t))
	req := f.request()
	req.Invocation = agent.InvocationGraphNode
	req.Graph = &agent.GraphInvocation{InstanceID: 1, AttemptID: 1, RunID: 1, Node: "worker"}
	req.Workflow = agent.WorkflowSpike
	if result, err := f.runner.Run(t.Context(), req); err == nil || result != nil {
		t.Fatalf("missing graph delivery was accepted: %v %+v", err, result)
	}
	if _, err := os.Stat(filepath.Join(f.worktree, "argv.dump")); !os.IsNotExist(err) {
		t.Fatalf("provider started: %v", err)
	}
}

func TestClaudeGraphRefusesCodexRole(t *testing.T) {
	f := newFixture(t, successScript(t))
	req := f.request()
	req.Delivery = verifiedClaudeDelivery(t)
	req.Invocation = agent.InvocationGraphNode
	req.Graph = &agent.GraphInvocation{InstanceID: 1, AttemptID: 1, RunID: 1, Node: "worker"}
	req.Workflow = agent.WorkflowSpike
	rolePath := filepath.Join(req.Delivery.Root, "plugin/agents/research.md")
	if err := os.Remove(rolePath); err != nil {
		t.Fatal(err)
	}
	delete(req.Delivery.RenderedFiles, "plugin/agents/research.md")
	if err := os.Remove(filepath.Join(req.Delivery.Root, "plugin/agents")); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{"roles/research.toml": "instructions = 'research'", "main-instructions.md": "Research role."} {
		name := filepath.Join(req.Delivery.Root, path)
		if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		req.Delivery.RenderedFiles[path] = fmt.Sprintf("%x", sha256.Sum256([]byte(body)))
	}
	if result, err := f.runner.Run(t.Context(), req); err == nil || result != nil || !strings.Contains(err.Error(), "missing Claude main-role resource") {
		t.Fatalf("wrong native role was accepted: %v %+v", err, result)
	}
	if _, err := os.Stat(filepath.Join(f.worktree, "argv.dump")); !os.IsNotExist(err) {
		t.Fatalf("provider started: %v", err)
	}
}
