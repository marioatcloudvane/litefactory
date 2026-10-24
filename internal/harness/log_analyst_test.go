package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
)

func TestLogAnalystIsolationCatalog(t *testing.T) {
	for _, mode := range []string{"valid", "extra_tool", "duplicate_terminal", "failure_then_success", "tool_after_terminal", "foreign_session", "delegated_tool", "callback_refused"} {
		t.Run(mode, func(t *testing.T) {
			tools := agent.LogAnalystToolNames()
			for i := range tools {
				tools[i] = "mcp__litefactory__" + tools[i]
			}
			if mode == "extra_tool" {
				tools = append(tools, "Bash")
			}
			init, _ := json.Marshal(map[string]any{"type": "system", "subtype": "init", "session_id": "fixture-session", "claude_code_version": "1.2.3", "tools": tools})
			dir := t.TempDir()
			binary := filepath.Join(t.TempDir(), "provider-fixture")
			terminal := `'{"type":"result","session_id":"fixture-session"}'`
			if mode == "duplicate_terminal" {
				terminal += " " + terminal
			}
			if mode == "failure_then_success" {
				terminal = `'{"type":"result","session_id":"fixture-session","is_error":true}' ` + terminal
			}
			if mode == "tool_after_terminal" {
				terminal += ` '{"type":"assistant","session_id":"fixture-session","message":{"content":[{"type":"tool_use","name":"mcp__litefactory__runs_list"}]}}'`
			}
			if mode == "foreign_session" {
				terminal = `'{"type":"assistant","session_id":"another-session","message":{"content":[{"type":"tool_use","name":"mcp__litefactory__runs_list"}]}}' ` + terminal
			}
			if mode == "delegated_tool" {
				terminal = `'{"type":"assistant","session_id":"fixture-session","parent_tool_use_id":"delegated","message":{"content":[{"type":"tool_use","name":"mcp__litefactory__runs_list"}]}}' ` + terminal
			}
			script := fmt.Sprintf(`#!/bin/sh
case "$1" in
--help) printf '%%s\n' '--tools --safe-mode --restricted --disable-slash-commands --strict-mcp-config --setting-sources --system-prompt --settings --agents --no-chrome --no-session-persistence'; exit 0;;
--version) echo 1.2.3; exit 0;;
esac
printf '%%s\n' '%s' %s
`, init, terminal)
			if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			spec, err := agent.NewMCPLaunchSpec(binary, filepath.Join(dir, "descriptor.json"), "analysis", agent.LogAnalystToolNames()...)
			if err != nil {
				t.Fatal(err)
			}
			verifiedCalls := 0
			request := agent.LogAnalystRequest{Attempt: "analysis", Model: "model", Prompt: "bounded task", WorkingDirectory: dir, MCPLaunch: spec, CatalogVerified: func(_ context.Context, version string) error {
				verifiedCalls++
				if version != "1.2.3" {
					t.Fatalf("unexpected verified provider version %q", version)
				}
				if mode == "callback_refused" {
					return errors.New("host gate refused")
				}
				return nil
			}}
			args := logAnalystArguments(request, filepath.Join(dir, "mcp.json"))
			for _, flag := range []string{"--tools", "--setting-sources"} {
				i := slices.Index(args, flag)
				if i < 0 || args[i+1] != "" {
					t.Fatalf("missing empty %s", flag)
				}
			}
			if !slices.Contains(args, "--no-session-persistence") || !strings.Contains(strings.Join(args, " "), "disableAllHooks") {
				t.Fatal("missing isolation controls")
			}
			runner := &Runner{claudeConfigDir: t.TempDir(), binary: config.Executable{Path: binary}, timeout: time.Minute, environ: []string{"PATH=/usr/bin:/bin"}}
			result, err := runner.RunLogAnalyst(t.Context(), request)
			wantVerifiedCalls := 1
			if mode == "extra_tool" {
				wantVerifiedCalls = 0
			}
			if verifiedCalls != wantVerifiedCalls {
				t.Fatalf("catalog callback calls=%d want=%d", verifiedCalls, wantVerifiedCalls)
			}
			if mode != "valid" && err == nil {
				t.Fatal("accepted invalid provider output")
			}
			if mode == "valid" && (err != nil || result.ProviderVersion != "1.2.3") {
				t.Fatalf("fixture transport: %+v %v", result, err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 0 {
				t.Fatalf("private cleanup: %v %v", entries, err)
			}
		})
	}
}

func TestLogAnalystUnsupportedAndCancellation(t *testing.T) {
	for _, supported := range []bool{false, true} {
		t.Run(fmt.Sprint(supported), func(t *testing.T) {
			dir := t.TempDir()
			binary := filepath.Join(t.TempDir(), "provider-fixture")
			help := "--tools"
			if supported {
				help = "--tools --safe-mode --restricted --disable-slash-commands --strict-mcp-config --setting-sources --system-prompt --settings --agents --no-chrome --no-session-persistence"
			}
			script := fmt.Sprintf(`#!/bin/sh
case "$1" in
--help) echo '%s'; exit 0;;
--version) echo 1.2.3; exit 0;;
esac
sleep 30
`, help)
			if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			spec, err := agent.NewMCPLaunchSpec(binary, filepath.Join(dir, "descriptor.json"), "analysis", agent.LogAnalystToolNames()...)
			if err != nil {
				t.Fatal(err)
			}
			runner := &Runner{claudeConfigDir: t.TempDir(), binary: config.Executable{Path: binary}, timeout: time.Minute, environ: []string{"PATH=/usr/bin:/bin"}}
			ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
			defer cancel()
			_, err = runner.RunLogAnalyst(ctx, agent.LogAnalystRequest{Attempt: "analysis", Model: "model", Prompt: "bounded task", WorkingDirectory: dir, MCPLaunch: spec})
			if supported && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("cancellation: %v", err)
			}
			if !supported && !errors.Is(err, agent.ErrLogAnalystIsolation) {
				t.Fatalf("unsupported controls: %v", err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 0 {
				t.Fatalf("private cleanup: %v %v", entries, err)
			}
		})
	}
}

// Similar flag names cannot attest that the required isolation switch exists.
func TestLogAnalystIsolationRejectsFlagPrefix(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "provider-fixture")
	script := `#!/bin/sh
printf '%s\n' '--tools-extra --safe-mode --restricted --disable-slash-commands --strict-mcp-config --setting-sources --system-prompt --settings --agents --no-chrome --no-session-persistence'
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	runner := &Runner{binary: config.Executable{Path: binary}, environ: []string{"PATH=/usr/bin:/bin"}}
	if _, err := runner.LogAnalystIsolation(t.Context()); !errors.Is(err, agent.ErrLogAnalystIsolation) {
		t.Fatalf("similar flag accepted: %v", err)
	}
}
