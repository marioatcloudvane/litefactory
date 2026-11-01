package codex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	execution "github.com/cloudvane/litefactory/pkg/execution/agent"
)

// This executable records the protocol only; its sandbox response is simulated.
func recordedCodexConversation(t *testing.T, mode string) (*Runner, agent.ConversationRequest) {
	t.Helper()
	state, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(state, "recorded-provider")
	script := `#!/bin/sh
mode='` + mode + `'
if [ "$1" = sandbox ]; then
 if [ "$mode" = probe-failed ]; then exit 71; fi
 if [ "$mode" = probe-uncertain ]; then echo 'unexpected capability response'; else echo 'conversation-isolation-verified'; fi
 exit 0
fi
for arg in "$@"; do
 if [ "$arg" = --help ]; then
  if [ "$mode" = unsupported ]; then echo '--json'; else echo '--strict-config --ignore-user-config --ignore-rules --json'; fi
  exit 0
 fi
done
[ -z "$GH_TOKEN$LF_HOME$CODEX_API_KEY" ] || exit 90
printf '%s\n' "$@" > "$CODEX_HOME/launch.args"
printf 'launch\n' >> "$CODEX_HOME/launch.count"
sid=native-session
if [ "$2" = resume ]; then
 [ "$mode" != missing ] || exit 8
 for arg in "$@"; do [ "$arg" != native-session ] || sid=$arg; done
fi
[ "$mode" != wrong ] || sid=unexpected-session
/bin/cat > "$CODEX_HOME/prompt"
printf 'retained context\n' > "$CODEX_HOME/native-state"
printf '{"type":"thread.started","thread_id":"%s"}\n' "$sid"
[ "$mode" != interrupt ] || /bin/sleep 30
printf '%s\n' '{"type":"item.completed","item":{"type":"agent_message","text":"recorded answer"}}' '{"type":"turn.completed"}'
`
	if err = os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	runner := &Runner{binary: config.Executable{Path: binary}, timeout: 10 * time.Second, environ: []string{"PATH=/usr/bin:/bin", "GH_TOKEN=secret", "LF_HOME=secret", "CODEX_API_KEY=secret"}}
	request := agent.ConversationRequest{ConversationID: "conversation", TurnID: "turn", Model: "fixture-model", StateDirectory: state, Prompt: "prepared owner input", NativeInstructions: "captured fixture instructions", Session: agent.SessionRef{Ref: "factory-session"}, BeforeLaunch: func(context.Context) error { return nil }, OnSession: func(context.Context, agent.SessionRef) error { return nil }}
	return runner, request
}

func TestRecordedCodexIsolationRefusals(t *testing.T) {
	for _, mode := range []string{"unsupported", "probe-failed", "probe-uncertain"} {
		t.Run(mode, func(t *testing.T) {
			runner, request := recordedCodexConversation(t, mode)
			identities := 0
			request.OnSession = func(context.Context, agent.SessionRef) error { identities++; return nil }
			result, err := runner.RunConversation(t.Context(), request)
			if !errors.Is(err, agent.ErrConversationIsolation) || identities != 0 || len(result.Answer) != 0 {
				t.Fatalf("unsupported isolation accepted: %+v %v", result, err)
			}
			if _, err = os.Stat(filepath.Join(request.StateDirectory, "home", "launch.count")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("refused capability launched a model: %v", err)
			}
		})
	}
}

func TestRecordedCodexResumeAndMissingState(t *testing.T) {
	for _, mode := range []string{"normal", "wrong", "missing"} {
		t.Run(mode, func(t *testing.T) {
			runner, request := recordedCodexConversation(t, mode)
			request.Session.Resume = "native-session"
			identities := 0
			request.OnSession = func(_ context.Context, ref agent.SessionRef) error {
				identities++
				if ref != request.Session {
					return errors.New("native identity changed")
				}
				return nil
			}
			result, err := runner.RunConversation(t.Context(), request)
			if mode != "normal" {
				if !errors.Is(err, agent.ErrConversationSessionMissing) || identities != 0 || len(result.Answer) != 0 {
					t.Fatalf("lost session replaced: %+v %v", result, err)
				}
				return
			}
			if err != nil || identities != 1 || string(result.Answer) != "recorded answer" {
				t.Fatalf("resume failed: %+v %v", result, err)
			}
			data, err := os.ReadFile(filepath.Join(request.StateDirectory, "home", "launch.args"))
			if err != nil {
				t.Fatal(err)
			}
			args := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
			if len(args) < 4 || args[0] != "exec" || args[1] != "resume" || args[len(args)-2] != "native-session" || args[len(args)-1] != "-" {
				t.Fatalf("not exact native resume: %v", args)
			}
			for _, flag := range []string{"--strict-config", "--ignore-user-config", "--ignore-rules", "--json", "--skip-git-repo-check"} {
				if !slices.Contains(args, flag) {
					t.Fatalf("missing launch control %s", flag)
				}
			}
			for _, setting := range []string{`default_permissions="conversation"`, `approval_policy="never"`, `web_search="disabled"`, `mcp_servers={}`, `project_doc_max_bytes=0`, `features.apps=false`, `features.plugins=false`, `features.shell_tool=false`, `features.unified_exec=false`, `features.view_image=false`, `features.image_generation=false`, `features.multi_agent=false`, `developer_instructions=` + strconv.Quote(request.NativeInstructions)} {
				if !slices.Contains(args, setting) {
					t.Fatalf("missing prompt-only policy %s", setting)
				}
			}
			prompt, readErr := os.ReadFile(filepath.Join(request.StateDirectory, "home", "prompt"))
			if readErr != nil || string(prompt) != request.Prompt {
				t.Fatalf("private prompt changed: %q %v", prompt, readErr)
			}
			profileFound := false
			for _, arg := range args {
				if strings.HasPrefix(arg, "permissions=") {
					profileFound = strings.Contains(arg, `":root"="deny"`) && strings.Contains(arg, `":minimal"="read"`) && strings.Contains(arg, `network={enabled=false}`) && strings.Contains(arg, filepath.Join(request.StateDirectory, "scratch"))
				}
			}
			if !profileFound || slices.Contains(args, "--last") || slices.Contains(args, "--ephemeral") || slices.Contains(args, request.Prompt) {
				t.Fatalf("unsafe resume isolation/persistence: %v", args)
			}
		})
	}
}

func TestRecordedCodexInterruptionRetainsNativeState(t *testing.T) {
	runner, request := recordedCodexConversation(t, "interrupt")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	request.OnSession = func(context.Context, agent.SessionRef) error { cancel(); return nil }
	result, err := runner.RunConversation(ctx, request)
	if !errors.Is(err, context.Canceled) || result.Session.Resume != "native-session" || len(result.Answer) != 0 {
		t.Fatalf("interruption fabricated completion: %+v %v", result, err)
	}
	if data, e := os.ReadFile(filepath.Join(request.StateDirectory, "home", "native-state")); e != nil || string(data) != "retained context\n" {
		t.Fatalf("native state lost: %q %v", data, e)
	}
}

func TestRecordedCodexProbeRecovery(t *testing.T) {
	for _, failure := range []string{"kill -KILL $$", "exec /bin/sleep 30"} {
		t.Run(failure, func(t *testing.T) {
			runner, request := recordedCodexConversation(t, "normal")
			binary := runner.binary.Path
			original, err := os.ReadFile(binary)
			if err != nil {
				t.Fatal(err)
			}
			script := "#!/bin/sh\nif [ ! -f \"$0.once\" ]; then touch \"$0.once\"; " + failure + "; fi\n" + string(original)
			if err = os.WriteFile(binary, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			identities := 0
			request.OnSession = func(context.Context, agent.SessionRef) error { identities++; return nil }
			result, err := runner.RunConversation(t.Context(), request)
			if err != nil || identities != 1 || string(result.Answer) != "recorded answer" {
				t.Fatalf("recovery: %+v %v identities=%d", result, err, identities)
			}
			count, err := os.ReadFile(filepath.Join(request.StateDirectory, "home", "launch.count"))
			if err != nil || string(count) != "launch\n" {
				t.Fatalf("model launches: %q %v", count, err)
			}
			prompt, err := os.ReadFile(filepath.Join(request.StateDirectory, "home", "prompt"))
			if err != nil || string(prompt) != request.Prompt {
				t.Fatalf("original prompt lost: %q %v", prompt, err)
			}

		})
	}
}

func TestRecordedCodexProbeExhaustion(t *testing.T) {
	runner, request := recordedCodexConversation(t, "normal")
	if err := os.WriteFile(runner.binary.Path, []byte("#!/bin/sh\nkill -KILL $$\n"), 0700); err != nil {
		t.Fatal(err)
	}
	_, err := runner.RunConversation(t.Context(), request)
	if !errors.Is(err, execution.ErrProbeTransient) || errors.Is(err, agent.ErrConversationIsolation) {
		t.Fatalf("transient cause lost: %v", err)
	}
	if _, err = os.Stat(filepath.Join(request.StateDirectory, "home", "launch.count")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("launched after exhausted probe: %v", err)
	}
}

func TestConversationArgumentsReapplyCapturedNativeInstructions(t *testing.T) {
	for _, resume := range []string{"", "native-session"} {
		request := agent.ConversationRequest{Model: "model", NativeInstructions: "captured instructions with \"quotes\"\nand exact spaces ", Session: agent.SessionRef{Ref: "factory-session", Resume: resume}}
		args := conversationArguments(request, "/private/scratch")
		if !slices.Contains(args, "developer_instructions="+strconv.Quote(request.NativeInstructions)) {
			t.Fatal("native instructions changed")
		}
	}
}

func TestRecordedCodexMissingNativeInstructionsRefusesLaunch(t *testing.T) {
	runner, request := recordedCodexConversation(t, "normal")
	request.NativeInstructions = ""
	if _, err := runner.RunConversation(t.Context(), request); err == nil {
		t.Fatal("missing retained instructions accepted")
	}
	if _, err := os.Stat(filepath.Join(request.StateDirectory, "home", "launch.count")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid request launched model: %v", err)
	}
}
