package harness

import (
	"bytes"
	"context"
	"errors"
	execution "github.com/cloudvane/litefactory/pkg/execution/agent"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
)

// These recordings exercise the adapter protocol; they do not verify an installed CLI.
func recordedClaudeConversation(t *testing.T, mode string) (*Runner, agent.ConversationRequest) {
	t.Helper()
	state, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(state, "recorded-provider")
	script := `#!/bin/sh
mode='` + mode + `'
if [ "$1" = --help ]; then
 if [ "$mode" = unsupported ]; then echo '--resume'; else echo '--tools --safe-mode --restricted --disable-slash-commands --strict-mcp-config --setting-sources --session-id --resume --system-prompt --settings --agents --no-chrome'; fi
 exit 0
fi
if [ "$1" = --version ]; then echo '2.1.299 (Claude Code)'; exit 0; fi
[ -z "$GH_TOKEN$LF_HOME$ANTHROPIC_API_KEY" ] || exit 90
[ "$HOME" = 'OS_HOME' ] || exit 91
[ "$TMPDIR" != "$CLAUDE_CONFIG_DIR" ] || exit 92
printf '%s\n' "$@" > "$CLAUDE_CONFIG_DIR/launch.args"
printf 'launch\n' >> "$CLAUDE_CONFIG_DIR/launch.count"
resume=''; sid=''
while [ "$#" -gt 0 ]; do
 case "$1" in --resume) shift; resume=$1; sid=$1;; --session-id) shift; sid=$1;; --mcp-config) shift; /bin/cat "$1" > "$CLAUDE_CONFIG_DIR/mcp.json"; printf '%s' "$1" > "$CLAUDE_CONFIG_DIR/mcp.path";; esac
 shift
done
if [ -n "$resume" ] && [ "$mode" = missing ]; then exit 8; fi
[ "$mode" != wrong ] || sid=unexpected-session
/bin/cat > "$CLAUDE_CONFIG_DIR/prompt"
printf 'retained context\n' > "$CLAUDE_CONFIG_DIR/native-state"
case "$mode" in
 tool_use) printf '{"type":"assistant","message":{"content":[{"type":"tool_use"}]}}\n';;
 missing_mcp) printf '{"type":"system","subtype":"init","session_id":"%s","tools":[],"claude_code_version":"2.1.299"}\n' "$sid";;
 tools) printf '{"type":"system","subtype":"init","session_id":"%s","tools":["Bash"],"mcp_servers":[],"claude_code_version":"2.1.299"}\n' "$sid";;
 omitted) printf '{"type":"system","subtype":"init","session_id":"%s","mcp_servers":[],"claude_code_version":"2.1.299"}\n' "$sid";;
 mcp) printf '{"type":"system","subtype":"init","session_id":"%s","tools":[],"mcp_servers":[{"name":"ambient"}],"claude_code_version":"2.1.299"}\n' "$sid";;
 *) printf '{"type":"system","subtype":"init","session_id":"%s","tools":[],"mcp_servers":[],"claude_code_version":"2.1.299"}\n' "$sid";;
esac
[ "$mode" != interrupt ] || /bin/sleep 30
case "$mode" in
 refused) printf '{"type":"result","session_id":"%s","result":"API Error: Claude'"'"'s safeguards flagged this message (aup). Try rephrasing. Details: [reasoning_extraction]","is_error":true}\n' "$sid"; exit 1;;
 authentication) printf '{"type":"result","session_id":"%s","result":"Failed to authenticate: OAuth session expired","is_error":true}\n' "$sid"; exit 1;;
 provider_error) printf '{"type":"result","session_id":"%s","result":"API Error: overloaded","is_error":true}\n' "$sid"; exit 1;;
esac
printf '{"type":"result","session_id":"%s","result":"recorded answer","is_error":false}\n' "$sid"
`
	osHome := t.TempDir()
	script = strings.ReplaceAll(script, "OS_HOME", osHome)
	if err = os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	runner := &Runner{claudeConfigDir: filepath.Join(t.TempDir(), "claude-home"), osHome: osHome, binary: config.Executable{Path: binary}, timeout: 10 * time.Second, environ: []string{"PATH=/usr/bin:/bin", "GH_TOKEN=secret", "LF_HOME=secret", "ANTHROPIC_API_KEY=secret"}}
	request := agent.ConversationRequest{ConversationID: "conversation", TurnID: "turn", Model: "fixture-model", StateDirectory: state, Prompt: "prepared owner input", NativeInstructions: "captured fixture instructions", Session: agent.SessionRef{Ref: "factory-session"}, BeforeLaunch: func(context.Context) error { return nil }, OnSession: func(context.Context, agent.SessionRef) error { return nil }}
	return runner, request
}

func TestRecordedClaudeIsolationRefusals(t *testing.T) {
	for _, mode := range []string{"unsupported", "tools", "omitted", "mcp", "missing_mcp", "tool_use"} {
		t.Run(mode, func(t *testing.T) {
			runner, request := recordedClaudeConversation(t, mode)
			identities := 0
			request.OnSession = func(context.Context, agent.SessionRef) error { identities++; return nil }
			result, err := runner.RunConversation(t.Context(), request)
			if !errors.Is(err, agent.ErrConversationIsolation) || identities != 0 || len(result.Answer) != 0 {
				t.Fatalf("unsafe invocation accepted: result=%+v err=%v identities=%d", result, err, identities)
			}
			if mode == "unsupported" {
				if _, err = os.Stat(filepath.Join(runner.claudeConfigDir, "launch.count")); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("unsupported gate launched: %v", err)
				}
			}
		})
	}
}

func TestRecordedClaudeResumeAndMissingState(t *testing.T) {
	for _, mode := range []string{"normal", "wrong", "missing"} {
		t.Run(mode, func(t *testing.T) {
			runner, request := recordedClaudeConversation(t, mode)
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
			data, err := os.ReadFile(filepath.Join(runner.claudeConfigDir, "launch.args"))
			if err != nil {
				t.Fatal(err)
			}
			args := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
			for _, flag := range []string{"--restricted", "--disable-slash-commands", "--strict-mcp-config", "--no-chrome"} {
				if !slices.Contains(args, flag) {
					t.Fatalf("missing launch control %s", flag)
				}
			}
			// --safe-mode also disables the --mcp-config server the conversation needs.
			if slices.Contains(args, "--safe-mode") {
				t.Fatal("conversation launched with --safe-mode")
			}
			for flag, want := range map[string]string{"--resume": "native-session", "--tools": "", "--setting-sources": "", "--settings": `{"disableAllHooks":true}`, "--agents": "{}", "--system-prompt": request.NativeInstructions} {
				i := slices.Index(args, flag)
				if i < 0 || i+1 == len(args) || args[i+1] != want {
					t.Fatalf("unsafe %s: %v", flag, args)
				}
			}
			for name, want := range map[string]string{"prompt": request.Prompt, "mcp.json": `{"mcpServers":{}}`} {
				contents, readErr := os.ReadFile(filepath.Join(runner.claudeConfigDir, name))
				if readErr != nil || string(contents) != want {
					t.Fatalf("private %s changed: %q %v", name, contents, readErr)
				}
			}
			mcpPath, err := os.ReadFile(filepath.Join(runner.claudeConfigDir, "mcp.path"))
			if err != nil || strings.HasPrefix(string(mcpPath), runner.claudeConfigDir+string(filepath.Separator)) {
				t.Fatalf("per-run mcp.json %q in the persistent home: %v", mcpPath, err)
			}
			if _, err := os.Stat(filepath.Dir(string(mcpPath))); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("per-run dir survived the turn: %v", err)
			}
			if _, err := os.Stat(runner.claudeConfigDir); err != nil {
				t.Fatalf("persistent home removed: %v", err)
			}
			if slices.Contains(args, "--session-id") || slices.Contains(args, "--continue") || slices.Contains(args, "--no-session-persistence") || slices.Contains(args, request.Prompt) {
				t.Fatalf("resume lost persistence or exposed prompt: %v", args)
			}
		})
	}
}

func TestRecordedClaudeInterruptionRetainsNativeState(t *testing.T) {
	runner, request := recordedClaudeConversation(t, "interrupt")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	request.OnSession = func(context.Context, agent.SessionRef) error { cancel(); return nil }
	result, err := runner.RunConversation(ctx, request)
	if !errors.Is(err, context.Canceled) || result.Session.Resume != request.Session.Ref || len(result.Answer) != 0 {
		t.Fatalf("interruption fabricated completion: %+v %v", result, err)
	}
	if data, e := os.ReadFile(filepath.Join(runner.claudeConfigDir, "native-state")); e != nil || string(data) != "retained context\n" {
		t.Fatalf("native state lost: %q %v", data, e)
	}
}

func TestRecordedClaudePreStartFailures(t *testing.T) {
	for _, mode := range []string{"validation", "isolation_probe", "directory_preparation"} {
		t.Run(mode, func(t *testing.T) {
			runner, request := recordedClaudeConversation(t, "normal")
			switch mode {
			case "validation":
				request.Model = ""
			case "isolation_probe":
				runner.binary.Path += "-missing"
			case "directory_preparation":
				request.StateDirectory = filepath.Join(request.StateDirectory, "recorded-provider", "child")
			}
			_, err := runner.RunConversation(t.Context(), request)
			var failure *agent.ConversationFailure
			if !errors.As(err, &failure) || failure.Phase != mode || !failure.NoProcessStarted {
				t.Fatalf("lost pre-start evidence: %v %#v", err, failure)
			}
		})
	}
}

func TestRecordedClaudePatchVersionEvidence(t *testing.T) {
	runner, request := recordedClaudeConversation(t, "normal")
	var logs bytes.Buffer
	runner.logger = slog.New(slog.NewTextHandler(&logs, nil))
	result, err := runner.RunConversation(t.Context(), request)
	if err != nil || result.Session.Resume != request.Session.Ref {
		t.Fatalf("patch rejected: %+v %v", result, err)
	}
	for _, want := range []string{"phase=probe version=2.1.299", "phase=init version=2.1.299"} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("missing version evidence %s: %s", want, logs.String())
		}
	}
}

func TestRecordedClaudeProbeRecovery(t *testing.T) {
	for _, failure := range []string{"kill -KILL $$", "exec /bin/sleep 30"} {
		t.Run(failure, func(t *testing.T) {
			runner, request := recordedClaudeConversation(t, "normal")
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
			count, err := os.ReadFile(filepath.Join(runner.claudeConfigDir, "launch.count"))
			if err != nil || string(count) != "launch\n" {
				t.Fatalf("model launches: %q %v", count, err)
			}
			prompt, err := os.ReadFile(filepath.Join(runner.claudeConfigDir, "prompt"))
			if err != nil || string(prompt) != request.Prompt {
				t.Fatalf("original prompt lost: %q %v", prompt, err)
			}

		})
	}
}

func TestRecordedClaudeProbeExhaustion(t *testing.T) {
	runner, request := recordedClaudeConversation(t, "normal")
	if err := os.WriteFile(runner.binary.Path, []byte("#!/bin/sh\nkill -KILL $$\n"), 0700); err != nil {
		t.Fatal(err)
	}
	_, err := runner.RunConversation(t.Context(), request)
	if !errors.Is(err, execution.ErrProbeTransient) || errors.Is(err, agent.ErrConversationIsolation) {
		t.Fatalf("transient cause lost: %v", err)
	}
	if _, err = os.Stat(filepath.Join(runner.claudeConfigDir, "launch.count")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("launched after exhausted probe: %v", err)
	}
}

func TestConversationArgumentsReapplyCapturedNativeInstructions(t *testing.T) {
	for _, resume := range []string{"", "native-session"} {
		request := agent.ConversationRequest{Model: "model", NativeInstructions: "captured instructions with \"quotes\"\nand exact spaces ", Session: agent.SessionRef{Ref: "factory-session", Resume: resume}}
		args := conversationArguments(request, "/private/mcp.json")
		i := slices.Index(args, "--system-prompt")
		if i < 0 || i+1 >= len(args) || args[i+1] != request.NativeInstructions {
			t.Fatal("native instructions changed")
		}
	}
}

func TestRecordedClaudeMissingNativeInstructionsRefusesLaunch(t *testing.T) {
	runner, request := recordedClaudeConversation(t, "normal")
	request.NativeInstructions = ""
	if _, err := runner.RunConversation(t.Context(), request); err == nil {
		t.Fatal("missing retained instructions accepted")
	}
	if _, err := os.Stat(filepath.Join(runner.claudeConfigDir, "launch.count")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid request launched model: %v", err)
	}
}

func TestRecordedClaudeAdoptsLegacyConversationSession(t *testing.T) {
	runner, request := recordedClaudeConversation(t, "normal")
	request.Session.Resume = "native-session"
	project := filepath.Join("projects", transcriptProjectKey(filepath.Join(request.StateDirectory, "scratch")))
	legacy := filepath.Join(request.StateDirectory, "home", project, "native-session.jsonl")
	if err := os.MkdirAll(filepath.Dir(legacy), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("legacy transcript\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.RunConversation(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	adopted, err := os.ReadFile(filepath.Join(runner.claudeConfigDir, project, "native-session.jsonl"))
	if err != nil || string(adopted) != "legacy transcript\n" {
		t.Fatalf("legacy session not adopted: %q %v", adopted, err)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("legacy session removed: %v", err)
	}
	// An existing persistent transcript is never overwritten.
	if err := os.WriteFile(legacy, []byte("stale\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.RunConversation(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if adopted, err = os.ReadFile(filepath.Join(runner.claudeConfigDir, project, "native-session.jsonl")); err != nil || string(adopted) != "legacy transcript\n" {
		t.Fatalf("persistent session overwritten: %q %v", adopted, err)
	}
}

// A provider error result keeps its category and bounded provider message.
func TestRecordedClaudeErrorResultCarriesProviderCause(t *testing.T) {
	for mode, want := range map[string]error{"refused": agent.ErrConversationRefused, "authentication": agent.ErrConversationAuthentication, "provider_error": nil} {
		t.Run(mode, func(t *testing.T) {
			runner, request := recordedClaudeConversation(t, mode)
			request.Session.Resume = "native-session"
			result, err := runner.RunConversation(t.Context(), request)
			if err == nil || len(result.Answer) != 0 {
				t.Fatalf("error result accepted: %+v %v", result, err)
			}
			var failure *agent.ConversationFailure
			if !errors.As(err, &failure) || failure.Phase != "terminal" || failure.NoProcessStarted {
				t.Fatalf("failure boundary lost: %v %#v", err, failure)
			}
			for _, category := range []error{agent.ErrConversationRefused, agent.ErrConversationAuthentication} {
				if errors.Is(err, category) != errors.Is(want, category) {
					t.Fatalf("category %v: %v", category, err)
				}
			}
			text := map[string]string{"refused": "safeguards flagged this message", "authentication": "OAuth session expired", "provider_error": "overloaded"}[mode]
			if !strings.Contains(err.Error(), text) {
				t.Fatalf("provider message dropped: %v", err)
			}
		})
	}
}
