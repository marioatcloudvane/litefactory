package codex

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func TestVerifiedDeliveryRefusesTamperedResourcesBeforeEveryAttempt(t *testing.T) {
	for _, path := range []agent.RunPath{agent.PathCold, agent.PathResume, agent.PathColdAfterResumeRefused} {
		t.Run(string(path), func(t *testing.T) {
			for _, condition := range []string{"changed", "missing", "extra", "symlink", "directory", "cancelled", "unsafe"} {
				t.Run(condition, func(t *testing.T) {
					runner, argv := fakeRunner(t, `printf '%s\n' '{"type":"turn.completed"}'`)
					req := request(t, path)
					if path.Resuming() {
						req.Session.Resume = "saved-thread"
					}
					req.Delivery = &agent.Delivery{Root: t.TempDir(), Identity: strings.Repeat("a", 64), MainRole: "worker", MainModel: "worker", Roles: []string{"worker"}}
					target := filepath.Join(req.Delivery.Root, "main-instructions.md")
					if err := os.WriteFile(target, []byte("Verified worker instructions\n"), 0600); err != nil {
						t.Fatal(err)
					}
					manifestDelivery(t, req.Delivery)
					ctx := t.Context()
					switch condition {
					case "changed":
						if err := os.WriteFile(target, []byte("changed"), 0600); err != nil {
							t.Fatal(err)
						}
					case "missing":
						if err := os.Remove(target); err != nil {
							t.Fatal(err)
						}
					case "extra":
						if err := os.WriteFile(filepath.Join(req.Delivery.Root, "extra.md"), []byte("extra"), 0600); err != nil {
							t.Fatal(err)
						}
					case "symlink":
						if err := os.Remove(target); err != nil {
							t.Fatal(err)
						}
						if err := os.Symlink(filepath.Join(t.TempDir(), "other"), target); err != nil {
							t.Fatal(err)
						}
					case "directory":
						if err := os.Remove(target); err != nil {
							t.Fatal(err)
						}
						if err := os.Mkdir(target, 0700); err != nil {
							t.Fatal(err)
						}
					case "unsafe":
						req.Delivery.RenderedFiles["../escape"] = strings.Repeat("a", 64)
					case "cancelled":
						var cancel func()
						ctx, cancel = context.WithCancel(ctx)
						cancel()
					}
					if _, err := runner.Run(ctx, req); err == nil {
						t.Fatal("invalid delivery accepted")
					}
					if _, err := os.Stat(argv); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("process started: %v", err)
					}
				})
			}
		})
	}
}

func TestVerifiedDeliveryRechecksRetainedBytesOnResume(t *testing.T) {
	runner, argv := fakeRunner(t, `printf '%s\n' '{"type":"turn.completed"}'`)
	req := request(t, agent.PathCold)
	req.Delivery = &agent.Delivery{Root: t.TempDir(), Identity: strings.Repeat("a", 64), MainRole: "worker", MainModel: "worker", Roles: []string{"worker"}}
	target := filepath.Join(req.Delivery.Root, "main-instructions.md")
	if err := os.WriteFile(target, []byte("worker instructions"), 0600); err != nil {
		t.Fatal(err)
	}
	manifestDelivery(t, req.Delivery)
	if _, err := runner.Run(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(argv); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	req.Path = agent.PathResume
	req.Session.Resume = "saved-thread"
	if _, err := runner.Run(t.Context(), req); err == nil {
		t.Fatal("tampered resume accepted")
	}
	if _, err := os.Stat(argv); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("resume started process")
	}
	if got := string(mustRead(t, target)); got != "tampered" {
		t.Fatal("saved bytes replaced")
	}
}

func TestVerifiedDeliverySelectsOnlyDeliveredSkillsAndPreservesSandbox(t *testing.T) {
	runner, argv := fakeRunner(t, `printf '%s\n' '{"type":"turn.completed"}'`)
	req := request(t, agent.PathCold)
	req.Mode = "implement"
	req.Invocation = agent.InvocationGraphNode
	req.Graph = &agent.GraphInvocation{InstanceID: 1, AttemptID: 1, RunID: 1, Node: "story-worker"}
	req.Delivery = &agent.Delivery{Root: t.TempDir(), Identity: strings.Repeat("a", 64), MainRole: "worker", MainModel: "worker", Roles: []string{"worker"}, Skills: []string{"plugin/skills/selected/SKILL.md"}, RequiredSkills: []string{"plugin/skills/selected/SKILL.md"}}
	req.Delivery.Evidence.Specialists = []agent.SpecialistEvidence{{ID: "worker", Model: "worker"}}
	files := map[string]string{"bootstrap.md": "Bootstrap", "plugin/.claude-plugin/plugin.json": "{}", "roles/worker.toml": "model = \"worker\"", "main-instructions.md": "Selected delivered worker", "plugin/skills/selected/SKILL.md": "Selected skill", "plugin/skills/selected/resource.md": "Required resource"}
	req.Delivery.RenderedFiles = map[string]string{}
	for name, body := range files {
		target := filepath.Join(req.Delivery.Root, name)
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		req.Delivery.RenderedFiles[name] = fmt.Sprintf("%x", sha256.Sum256([]byte(body)))
	}
	ambient := filepath.Join(req.WorktreePath, ".agents", "skills", "ambient", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(ambient), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ambient, []byte("Ambient replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	canonicalAmbient, err := filepath.EvalSymlinks(ambient)
	if err != nil {
		t.Fatal(err)
	}
	args := string(mustRead(t, argv))
	for _, want := range []string{"--strict-config", "--ignore-user-config", "--ignore-rules", "--sandbox workspace-write", "Selected delivered worker", "path=\"" + filepath.Join(req.Delivery.Root, req.Delivery.Skills[0]) + "\",enabled=true", "path=\"" + canonicalAmbient + "\",enabled=false"} {
		if !strings.Contains(args, want) {
			t.Errorf("missing %q: %s", want, args)
		}
	}
	if strings.Contains(args, "Ambient replacement") || strings.Contains(args, "features.multi_agent=false") {
		t.Fatalf("instruction or delegation changed: %s", args)
	}
}

func TestVerifiedGraphDeliveryMissingOrInconsistent(t *testing.T) {
	for _, condition := range []string{"missing", "model", "role", "required skill", "manifest", "wrong provider", "read limit"} {
		t.Run(condition, func(t *testing.T) {
			runner, argv := fakeRunner(t, `printf '%s\n' '{"type":"turn.completed"}'`)
			req := request(t, agent.PathCold)
			req.Invocation = agent.InvocationGraphNode
			req.Graph = &agent.GraphInvocation{InstanceID: 1, AttemptID: 1, RunID: 1, Node: "worker"}
			d := &agent.Delivery{Root: t.TempDir(), Identity: "capture", MainRole: "worker", MainModel: "worker", Roles: []string{"worker"}, Evidence: agent.DeliveryEvidence{Specialists: []agent.SpecialistEvidence{{ID: "worker", Model: "worker"}}}}
			files := map[string]string{"bootstrap.md": "bootstrap", "plugin/.claude-plugin/plugin.json": "{}", "roles/worker.toml": "model = 'worker'", "main-instructions.md": "worker instructions"}
			if condition == "wrong provider" {
				delete(files, "roles/worker.toml")
				delete(files, "main-instructions.md")
				files["plugin/agents/worker.md"] = "delivered Claude role"
			}
			d.RenderedFiles = map[string]string{}
			for name, body := range files {
				target := filepath.Join(d.Root, name)
				if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(target, []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
				d.RenderedFiles[name] = fmt.Sprintf("%x", sha256.Sum256([]byte(body)))
			}
			req.Delivery = d
			switch condition {
			case "missing":
				req.Delivery = nil
			case "model":
				d.MainModel = "unselected"
			case "role":
				d.MainRole = "unselected"
			case "required skill":
				d.RequiredSkills = []string{"plugin/skills/missing/SKILL.md"}
			case "manifest":
				d.RenderedFiles = nil
			case "read limit":
				if err := os.Truncate(filepath.Join(d.Root, "bootstrap.md"), 5<<20); err != nil {
					t.Fatal(err)
				}
			}
			if result, err := runner.Run(t.Context(), req); !errors.Is(err, agent.ErrDeliveryRefused) || result != nil {
				t.Fatalf("expected delivery refusal: %v %+v", err, result)
			}
			if _, err := os.Stat(argv); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("process started: %v", err)
			}
		})
	}
}
