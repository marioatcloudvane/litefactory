package harness

import (
	"encoding/json"
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

// Scripted providers exercise admission and transport, not live isolation certification.
func driftRecordedProvider(t *testing.T, mode string) (*Runner, agent.DriftRequest, string) {
	t.Helper()
	dir := t.TempDir()
	binary := filepath.Join(t.TempDir(), "provider")
	tools := []string{}
	for _, name := range agent.DriftToolNames() {
		tools = append(tools, "mcp__litefactory__"+name)
	}
	init := map[string]any{"type": "system", "subtype": "init", "tools": tools, "mcp_servers": []map[string]string{{"name": "litefactory", "status": "connected"}}, "claude_code_version": "fixture-version"}
	switch mode {
	case "native_catalog":
		init["tools"] = append(tools, "Bash")
	case "missing_tool":
		init["tools"] = tools[1:]
	case "omitted_catalog":
		delete(init, "tools")
	case "ambient_mcp":
		init["mcp_servers"] = []map[string]string{{"name": "litefactory", "status": "connected"}, {"name": "ambient", "status": "connected"}}
	case "disconnected":
		init["mcp_servers"] = []map[string]string{{"name": "litefactory", "status": "failed"}}
	}
	raw, err := json.Marshal(init)
	if err != nil {
		t.Fatal(err)
	}
	help := "--tools --safe-mode --restricted --disable-slash-commands --strict-mcp-config --setting-sources --system-prompt --settings --agents --no-chrome"
	if mode == "unsupported" {
		help = "--tools"
	}
	script := "#!/bin/sh\nif [ \"$1\" = --help ]; then echo '" + help + "'; exit 0; fi\nif [ \"$1\" = --version ]; then echo 'fixture-version'; exit 0; fi\n" + `
[ -z "$GH_TOKEN$LF_HOME$ANTHROPIC_API_KEY" ] || exit 90
[ "$HOME" = "$CLAUDE_CONFIG_DIR" ] || exit 91
printf '%s\n' "$@" > "$0.args"
/bin/cat > "$0.prompt"
while [ "$#" -gt 0 ]; do
 if [ "$1" = --mcp-config ]; then shift; /bin/cat "$1" > "$0.mcp"; fi
 shift
done
` + "printf '%s\\n' '" + string(raw) + "'\n"
	if mode == "native_call" {
		script += "printf '%s\\n' '{\"type\":\"assistant\",\"message\":{\"content\":[{\"type\":\"tool_use\",\"name\":\"Bash\"}]}}'\n"
	}
	script += "printf '%s\\n' '{\"type\":\"result\",\"is_error\":false}'\n"
	if err = os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	spec, err := agent.NewMCPLaunchSpec(binary, filepath.Join(dir, "descriptor.json"), "drift-attempt", agent.DriftToolNames()...)
	if err != nil {
		t.Fatal(err)
	}
	request := agent.DriftRequest{Invocation: agent.DoctorRequest{Attempt: "drift-attempt", Model: "fixture-model", Prompt: "bounded source context", WorkingDirectory: dir, MCPLaunch: spec}}
	runner := &Runner{binary: config.Executable{Path: binary}, timeout: 10 * time.Second, environ: []string{"PATH=/usr/bin:/bin", "GH_TOKEN=fixture-secret", "LF_HOME=unrelated", "ANTHROPIC_API_KEY=fixture-secret"}}
	return runner, request, binary
}

func TestDriftRecordedIsolationRefusals(t *testing.T) {
	for _, mode := range []string{"unsupported", "native_catalog", "missing_tool", "omitted_catalog", "ambient_mcp", "disconnected", "native_call"} {
		t.Run(mode, func(t *testing.T) {
			runner, request, binary := driftRecordedProvider(t, mode)
			if _, err := runner.RunDrift(t.Context(), request); !errors.Is(err, agent.ErrDriftIsolation) {
				t.Fatalf("unsafe launch accepted: %v", err)
			}
			if mode == "unsupported" {
				if _, err := os.Stat(binary + ".args"); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("unsupported profile launched: %v", err)
				}
			}
		})
	}
}

func TestDriftRecordedLaunchControls(t *testing.T) {
	runner, request, binary := driftRecordedProvider(t, "normal")
	var observed agent.DoctorRunResult
	request.OnInitialized = func(result agent.DoctorRunResult) error { observed = result; return nil }
	result, err := runner.RunDrift(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.ProviderVersion != "fixture-version" || observed.ProviderVersion != result.ProviderVersion {
		t.Fatalf("lost provider provenance: %+v", result)
	}
	raw, err := os.ReadFile(binary + ".args")
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	for _, flag := range []string{"--restricted", "--safe-mode", "--disable-slash-commands", "--strict-mcp-config", "--no-chrome"} {
		if !slices.Contains(args, flag) {
			t.Fatalf("missing launch control %s", flag)
		}
	}
	for flag, want := range map[string]string{"--tools": "", "--setting-sources": "", "--agents": "{}", "--settings": `{"disableAllHooks":true}`} {
		i := slices.Index(args, flag)
		if i < 0 || i+1 >= len(args) || args[i+1] != want {
			t.Fatalf("unsafe %s: %v", flag, args)
		}
	}
	raw, err = os.ReadFile(binary + ".prompt")
	if err != nil || string(raw) != request.Invocation.Prompt {
		t.Fatalf("prepared prompt changed: %q %v", raw, err)
	}
	raw, err = os.ReadFile(binary + ".mcp")
	if err != nil {
		t.Fatal(err)
	}
	var registration struct {
		Servers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err = json.Unmarshal(raw, &registration); err != nil || len(registration.Servers) != 1 || registration.Servers["litefactory"] == nil {
		t.Fatalf("ambient MCP registration: %s %v", raw, err)
	}
	entries, err := os.ReadDir(request.Invocation.WorkingDirectory)
	if err != nil || len(entries) != 0 {
		t.Fatalf("private home not removed: %v %v", entries, err)
	}
}
