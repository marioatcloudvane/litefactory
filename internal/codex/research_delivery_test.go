package codex

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func TestResearchMainRoleUsesRetainedDeveloperInstructions(t *testing.T) {
	t.Parallel()
	for _, path := range []agent.RunPath{agent.PathCold, agent.PathResume, agent.PathColdAfterResumeRefused} {
		t.Run(string(path), func(t *testing.T) {
			t.Parallel()
			runner, argv := fakeRunner(t, `cat > stdin.txt
printf '%s\n' '{"type":"turn.completed"}'`)
			req := request(t, path)
			req.Mode = "research"
			req.Permissions = agent.Permissions{Profile: "research", Payload: Sandbox{Mode: "read-only"}}
			if path.Resuming() {
				req.Session.Resume = "existing-thread"
			}
			req.Delivery = &agent.Delivery{Root: t.TempDir(), Identity: strings.Repeat("a", 64), MainRole: "research", MainModel: "worker", Roles: []string{"research"}}
			const instructions = "Selected main role\nPersistent result instructions\n"
			if err := os.WriteFile(filepath.Join(req.Delivery.Root, "main-instructions.md"), []byte(instructions), 0o600); err != nil {
				t.Fatal(err)
			}
			req.Prompt = "Incremental owner input only"
			result, err := runner.Run(t.Context(), req)
			if err != nil || result == nil || !result.Succeeded() {
				t.Fatalf("main role launch failed: result=%+v error=%v", result, err)
			}
			args := string(mustRead(t, argv))
			for _, want := range []string{"developer_instructions=", "Selected main role", "Persistent result instructions", "--model worker", "--sandbox read-only", "--ignore-user-config", "features.multi_agent=false", "skills.config="} {
				if !strings.Contains(args, want) {
					t.Errorf("missing %q in %s", want, args)
				}
			}
			for _, forbidden := range []string{"agents.research.config_file", "Incremental owner input", "workspace-write"} {
				if strings.Contains(args, forbidden) {
					t.Errorf("unexpected %q in %s", forbidden, args)
				}
			}
			if got := string(mustRead(t, filepath.Join(req.WorktreePath, "stdin.txt"))); got != req.Prompt {
				t.Fatalf("prompt gained bootstrap: %q", got)
			}
		})
	}
}

func TestResearchMainRoleRejectsInvalidRetainedInstructions(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"missing", "symlink", "directory", "empty", "oversized", "encoded-oversized", "invalid-utf8"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			runner, argv := fakeRunner(t, `printf '%s\n' '{"type":"turn.completed"}'`)
			req := request(t, agent.PathCold)
			req.Delivery = &agent.Delivery{Root: t.TempDir(), Identity: strings.Repeat("a", 64), MainRole: "research", MainModel: "worker", Roles: []string{"research"}}
			target := filepath.Join(req.Delivery.Root, "main-instructions.md")
			var raw []byte
			switch kind {
			case "missing":
			case "directory":
				if err := os.Mkdir(target, 0o700); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink(filepath.Join(t.TempDir(), "elsewhere"), target); err != nil {
					t.Fatal(err)
				}
			case "empty":
				raw = []byte(" ")
			case "oversized":
				raw = []byte(strings.Repeat("x", mainInstructionLimit+1))
			case "encoded-oversized":
				raw = []byte(strings.Repeat("x\n", mainInstructionLimit/2-1))
			case "invalid-utf8":
				raw = []byte{0xff}
			}
			if raw != nil {
				if err := os.WriteFile(target, raw, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := runner.Run(t.Context(), req); !errors.Is(err, agent.ErrInvalidRequest) {
				t.Fatalf("expected prelaunch refusal, got %v", err)
			}
			if _, err := os.Stat(argv); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("invalid retained instructions launched process: %v", err)
			}
		})
	}
}

func TestResearchMainRolePreservesInstructionCharactersInOneArgument(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		body string
	}{
		{"quotes and newlines", "Role says \"retain findings\".\n\tNext line\r\n"},
		{"backslashes and control characters", "A\\B\x00\x01\b\f"},
		{"unicode", "Research: Österreich 日本語 🔎\u2028\u2029"},
		{"shell metacharacters", "Literal $(printf injected); `printf injected` $HOME & | > <"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runner, _ := fakeRunner(t, `printf '%s\000' "$@" > exact-argv
printf '%s\n' '{"type":"turn.completed"}'`)
			req := request(t, agent.PathCold)
			req.Mode = "research"
			req.Permissions = agent.Permissions{Profile: "research", Payload: Sandbox{Mode: "read-only"}}
			req.Delivery = &agent.Delivery{Root: t.TempDir(), Identity: strings.Repeat("a", 64), MainRole: "research", MainModel: "worker", Roles: []string{"research"}}
			if err := os.WriteFile(filepath.Join(req.Delivery.Root, "main-instructions.md"), []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}

			result, err := runner.Run(t.Context(), req)

			if err != nil || result == nil || !result.Succeeded() {
				t.Fatalf("launch: result=%+v error=%v", result, err)
			}
			args := strings.Split(string(mustRead(t, filepath.Join(req.WorktreePath, "exact-argv"))), "\x00")
			var overrides []string
			for i, arg := range args {
				if strings.HasPrefix(arg, "developer_instructions=") {
					if i == 0 || args[i-1] != "-c" {
						t.Fatal("developer instructions were not a config value")
					}
					overrides = append(overrides, strings.TrimPrefix(arg, "developer_instructions="))
				}
			}
			if len(overrides) != 1 {
				t.Fatalf("want one developer instruction argument, got %d", len(overrides))
			}
			var decoded string
			if err := json.Unmarshal([]byte(overrides[0]), &decoded); err != nil || decoded != tc.body {
				t.Fatalf("instruction bytes changed: got %q, want %q, error=%v", decoded, tc.body, err)
			}
			if strings.Contains(overrides[0], `\x`) {
				t.Fatalf("TOML-incompatible hex escape: %q", overrides[0])
			}
		})
	}
}
