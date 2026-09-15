package codex

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
)

// mcpServerName is the one reserved Codex mcp_servers key litefactory ever
// writes. It is never taken from request data: [agent.MCPLaunchSpec] carries
// no server name at all, only the executable, descriptor and catalog, so
// there is nothing in the registration itself that could rename the entry
// this package emits.
const mcpServerName = "litefactory"

// SupportsMCP advertises the provider's attempt-local registration support.
func (r *Runner) SupportsMCP() bool { return true }

// mcpOverrides encodes one attempt's neutral registration into Codex's own
// mcp_servers configuration mechanism: structured command, args, a minimal
// explicit env and the attempt's exact enabled_tools — nothing ambient and
// nothing broader than the installed catalog the registration carries.
//
// Every value is emitted as a repeated "-c key=value" pair, the same
// mechanism [Runner.args] and [Runner.skillOverrides] already use for
// features.*, agents.* and skills.config; there is no shell wrapper and no
// file written to disk, so the registration is attempt-local and leaves no
// persistent trace once the process exits. A nil spec (disabled/absent
// registration) returns no overrides and changes nothing.
//
// ignoreUserConfig must be the exact value [Runner.args] is about to pass as
// --ignore-user-config for this same invocation: when it is true Codex never
// reads the operator's own config.toml, so the isolation collision check
// below is unnecessary and skipped, preserving that existing isolation
// exactly rather than doing redundant work under it.
func (r *Runner) mcpOverrides(spec *agent.MCPLaunchSpec, ignoreUserConfig bool) ([]string, error) {
	if spec == nil {
		return nil, nil
	}
	if err := spec.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", agent.ErrInvalidRequest, err)
	}
	if !ignoreUserConfig {
		if err := r.checkMCPCollision(); err != nil {
			return nil, err
		}
	}
	prefix := "mcp_servers." + mcpServerName + "."
	launchArgs := spec.Arguments()
	encodedArgs := make([]string, len(launchArgs))
	for i, a := range launchArgs {
		encodedArgs[i] = strconv.Quote(a)
	}
	tools := spec.PermittedTools()
	encodedTools := make([]string, len(tools))
	for i, name := range tools {
		encodedTools[i] = strconv.Quote(name)
	}
	return []string{
		"-c", prefix + "command=" + strconv.Quote(spec.Executable()),
		"-c", prefix + "args=[" + strings.Join(encodedArgs, ",") + "]",
		// Explicitly empty: the descriptor path and every other input the
		// installed host needs travels in args, not the environment, so no
		// ambient environment ever needs to reach this server.
		"-c", prefix + "env={}",
		"-c", prefix + "enabled_tools=[" + strings.Join(encodedTools, ",") + "]",
	}, nil
}

// checkMCPCollision refuses to admit the reserved MCP server name when the
// operator's own $CODEX_HOME/config.toml (or ~/.codex/config.toml) already
// defines it, rather than relying on override precedence to silently win.
//
// This is a raw scan of the file's lines for the reserved dotted key or
// table header, not a TOML parse: no TOML parser is a project dependency,
// and adding one is out of scope for this per-attempt encoding change (see
// the cross-story "no new dependency" constraint). It only runs when Codex
// will actually read that file for this invocation — ignoreUserConfig=false
// — because --ignore-user-config already removes it from consideration
// entirely, and this check must not widen or narrow that existing isolation.
func (r *Runner) checkMCPCollision() error {
	path := r.userConfigPath()
	if path == "" {
		return nil
	}
	// #nosec G304 -- path is derived from the operator's own CODEX_HOME/HOME
	// environment, mirroring the ambient discovery roots in skillOverrides.
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read codex config for MCP isolation: %w", err)
	}
	data, readErr := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err := errors.Join(readErr, f.Close()); err != nil {
		return fmt.Errorf("read codex config for MCP isolation: %w", err)
	}
	if len(data) > 1<<20 {
		return fmt.Errorf("%w: codex config exceeds MCP isolation check limit", agent.ErrInvalidRequest)
	}
	reserved := "mcp_servers." + mcpServerName
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "["+reserved+"]") || strings.HasPrefix(line, "[["+reserved+"]]") ||
			strings.HasPrefix(line, reserved+".") || strings.HasPrefix(line, reserved+" ") || strings.HasPrefix(line, reserved+"=") {
			return fmt.Errorf("%w: codex config collides with the reserved %s MCP server", agent.ErrInvalidRequest, mcpServerName)
		}
	}
	return nil
}

// userConfigPath resolves the same config.toml location skillOverrides'
// ambient discovery roots use, without requiring the file to exist.
func (r *Runner) userConfigPath() string {
	if home := envValue(r.environ, "CODEX_HOME"); home != "" {
		return filepath.Join(home, "config.toml")
	}
	if home := envValue(r.environ, "HOME"); home != "" {
		return filepath.Join(home, ".codex", "config.toml")
	}
	return ""
}
