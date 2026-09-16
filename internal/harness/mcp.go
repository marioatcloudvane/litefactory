package harness

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/cloudvane/litefactory/internal/agent"
)

const mcpServerName = "litefactory"

type mcpServerConfig struct {
	Type    string            `json:"type"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
}

type mcpConfig struct {
	Servers map[string]mcpServerConfig `json:"mcpServers"`
}

// SupportsMCP advertises the provider's attempt-local registration support.
func (r *Runner) SupportsMCP() bool { return true }

// prepareMCP creates only provider configuration. The runtime owns descriptor
// authentication, installed grants and broker lifetime. A unique private directory
// keeps parallel launches independent even when they share a run ordinal.
func (r *Runner) prepareMCP(spec *agent.MCPLaunchSpec) (path string, cleanup func() error, err error) {
	if spec == nil {
		return "", func() error { return nil }, nil
	}
	if err := spec.Validate(); err != nil {
		return "", nil, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	if err := r.checkMCPCollision(); err != nil {
		return "", nil, err
	}
	data, err := json.Marshal(mcpConfig{Servers: map[string]mcpServerConfig{
		mcpServerName: {Type: "stdio", Command: spec.Executable(), Args: spec.Arguments(), Env: map[string]string{}},
	}})
	if err != nil {
		return "", nil, fmt.Errorf("encode MCP config: %w", err)
	}
	dir, err := os.MkdirTemp(filepath.Dir(spec.DescriptorPath()), "claude-mcp-")
	if err != nil {
		return "", nil, fmt.Errorf("create private MCP directory: %w", err)
	}
	cleanup = func() error {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("remove private MCP config: %w", err)
		}
		return nil
	}
	path = filepath.Join(dir, "config.json")
	// #nosec G306 -- this private provider config is deliberately mode 0600.
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", nil, errors.Join(fmt.Errorf("write private MCP config: %w", err), cleanup())
	}
	return path, cleanup, nil
}

// Strict MCP mode excludes ambient project/user registrations. Also refuse an
// explicit operator settings collision instead of relying on merge precedence.
func (r *Runner) checkMCPCollision() error {
	// #nosec G304 -- settingsPath is validated operator-owned configuration.
	f, err := os.Open(r.settingsPath)
	if err != nil {
		return fmt.Errorf("read settings for MCP isolation: %w", err)
	}
	data, readErr := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err := errors.Join(readErr, f.Close()); err != nil {
		return fmt.Errorf("read settings for MCP isolation: %w", err)
	}
	if len(data) > 1<<20 {
		return invalidRequest("settings exceed MCP isolation check limit")
	}
	var settings struct {
		Servers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return fmt.Errorf("decode settings for MCP isolation: %w", err)
	}
	if _, exists := settings.Servers[mcpServerName]; exists {
		return invalidRequest("settings collide with trusted MCP server")
	}
	return nil
}
