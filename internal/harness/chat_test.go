package harness

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
)

func TestChatNativeTransportDisablesToolsAndDiscardsAmbientSecrets(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(t.TempDir(), "fixture")
	script := `#!/bin/sh
[ -z "$GH_TOKEN$LF_HOME$ANTHROPIC_API_KEY$ANTHROPIC_BASE_URL" ] || exit 9
[ "$HOME" = "OS_HOME" ] || exit 10
[ "$CLAUDE_CONFIG_DIR" = "CONFIG_DIR" ] || exit 12
[ "$TMPDIR" != "$CLAUDE_CONFIG_DIR" ] || exit 13
read -r prompt
[ "$prompt" = "prepared chat data" ] || exit 11
printf '%s\n' '{"type":"system","subtype":"init","session_id":"s","tools":[]}' '{"type":"result","result":"{\"points\":[]}"}'
`
	osHome := t.TempDir()
	credentials := filepath.Join(osHome, ".claude", ".credentials.json")
	if err := os.MkdirAll(filepath.Dir(credentials), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(credentials, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	configDir := filepath.Join(t.TempDir(), "claude-home")
	script = strings.ReplaceAll(strings.ReplaceAll(script, "OS_HOME", osHome), "CONFIG_DIR", configDir)
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	runner := &Runner{claudeConfigDir: configDir, osHome: osHome, binary: config.Executable{Path: binary}, timeout: 60 * time.Second, environ: []string{"PATH=/usr/bin:/bin", "GH_TOKEN=private", "LF_HOME=private", "ANTHROPIC_API_KEY=private", "ANTHROPIC_BASE_URL=private"}}
	result, err := runner.RunChat(t.Context(), agent.ChatRequest{Prompt: "prepared chat data\n", Model: "balanced-fixture", WorkingDirectory: dir})
	if err != nil || string(result.Answer) != `{"points":[]}` {
		t.Fatalf("chat transport: %+v %v", result, err)
	}
	args := chatArguments("balanced-fixture", "/private/mcp.json")
	i := slices.Index(args, "--tools")
	if i < 0 || args[i+1] != "" || !slices.Contains(args, "--strict-mcp-config") || !slices.Contains(args, "--no-session-persistence") || strings.Contains(strings.Join(args, " "), "Bash") {
		t.Fatalf("unsafe chat tool ceiling: %v", args)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("per-run scratch retained: %v %v", entries, err)
	}
	if target, err := os.Readlink(filepath.Join(configDir, ".credentials.json")); err != nil || target != credentials {
		t.Fatalf("persistent claude home did not survive the chat: %q %v", target, err)
	}
	// A provider advertising tools is refused rather than trusted silently.
	if err := os.WriteFile(binary, []byte(strings.ReplaceAll(script, `"tools":[]`, `"tools":["Bash"]`)), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.RunChat(t.Context(), agent.ChatRequest{Prompt: "prepared chat data\n", Model: "balanced-fixture", WorkingDirectory: dir}); err == nil {
		t.Fatal("tool-bearing chat invocation accepted")
	}
}
