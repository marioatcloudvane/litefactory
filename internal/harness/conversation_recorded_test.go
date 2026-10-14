package harness

import (
	"context"
	"errors"
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
if [ "$1" = --version ]; then echo '2.1.296 (Claude Code)'; exit 0; fi
[ -z "$GH_TOKEN$LF_HOME$ANTHROPIC_API_KEY" ] || exit 90
printf '%s\n' "$@" > "$CLAUDE_CONFIG_DIR/launch.args"
printf 'launch\n' >> "$CLAUDE_CONFIG_DIR/launch.count"
resume=''; sid=''
while [ "$#" -gt 0 ]; do
 case "$1" in --resume) shift; resume=$1; sid=$1;; --session-id) shift; sid=$1;; esac
 shift
done
if [ -n "$resume" ] && [ "$mode" = missing ]; then exit 8; fi
[ "$mode" != wrong ] || sid=unexpected-session
/bin/cat > "$CLAUDE_CONFIG_DIR/prompt"
printf 'retained context\n' > "$CLAUDE_CONFIG_DIR/native-state"
case "$mode" in
 tools) printf '{"type":"system","subtype":"init","session_id":"%s","tools":["Bash"],"mcp_servers":[],"claude_code_version":"2.1.296"}\n' "$sid";;
 omitted) printf '{"type":"system","subtype":"init","session_id":"%s","mcp_servers":[],"claude_code_version":"2.1.296"}\n' "$sid";;
 mcp) printf '{"type":"system","subtype":"init","session_id":"%s","tools":[],"mcp_servers":[{"name":"ambient"}],"claude_code_version":"2.1.296"}\n' "$sid";;
 *) printf '{"type":"system","subtype":"init","session_id":"%s","tools":[],"mcp_servers":[],"claude_code_version":"2.1.296"}\n' "$sid";;
esac
[ "$mode" != interrupt ] || /bin/sleep 30
printf '{"type":"result","session_id":"%s","result":"recorded answer","is_error":false}\n' "$sid"
`
	if err = os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	runner := &Runner{binary: config.Executable{Path: binary}, timeout: 10 * time.Second, environ: []string{"PATH=/usr/bin:/bin", "GH_TOKEN=secret", "LF_HOME=secret", "ANTHROPIC_API_KEY=secret"}}
	request := agent.ConversationRequest{ConversationID: "conversation", TurnID: "turn", Model: "fixture-model", StateDirectory: state, Prompt: "prepared owner input", Session: agent.SessionRef{Ref: "factory-session"}, BeforeLaunch: func(context.Context) error { return nil }, OnSession: func(context.Context, agent.SessionRef) error { return nil }}
	return runner, request
}

func TestRecordedClaudeIsolationRefusals(t *testing.T) {
	for _, mode := range []string{"unsupported", "tools", "omitted", "mcp"} {
		t.Run(mode, func(t *testing.T) {
			runner, request := recordedClaudeConversation(t, mode)
			identities := 0
			request.OnSession = func(context.Context, agent.SessionRef) error { identities++; return nil }
			result, err := runner.RunConversation(t.Context(), request)
			if !errors.Is(err, agent.ErrConversationIsolation) || identities != 0 || len(result.Answer) != 0 {
				t.Fatalf("unsafe invocation accepted: result=%+v err=%v identities=%d", result, err, identities)
			}
			if mode == "unsupported" {
				if _, err = os.Stat(filepath.Join(request.StateDirectory, "home", "launch.count")); !errors.Is(err, os.ErrNotExist) {
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
			data, err := os.ReadFile(filepath.Join(request.StateDirectory, "home", "launch.args"))
			if err != nil {
				t.Fatal(err)
			}
			args := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
			for _, flag := range []string{"--safe-mode", "--restricted", "--disable-slash-commands", "--strict-mcp-config", "--no-chrome"} {
				if !slices.Contains(args, flag) {
					t.Fatalf("missing launch control %s", flag)
				}
			}
			for flag, want := range map[string]string{"--resume": "native-session", "--tools": "", "--setting-sources": "", "--settings": `{"disableAllHooks":true}`, "--agents": "{}", "--system-prompt": agent.ConversationInstructions} {
				i := slices.Index(args, flag)
				if i < 0 || i+1 == len(args) || args[i+1] != want {
					t.Fatalf("unsafe %s: %v", flag, args)
				}
			}
			for name, want := range map[string]string{"prompt": request.Prompt, "mcp.json": `{"mcpServers":{}}`} {
				contents, readErr := os.ReadFile(filepath.Join(request.StateDirectory, "home", name))
				if readErr != nil || string(contents) != want {
					t.Fatalf("private %s changed: %q %v", name, contents, readErr)
				}
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
	if data, e := os.ReadFile(filepath.Join(request.StateDirectory, "home", "native-state")); e != nil || string(data) != "retained context\n" {
		t.Fatalf("native state lost: %q %v", data, e)
	}
}
