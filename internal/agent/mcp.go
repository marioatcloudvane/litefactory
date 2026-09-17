package agent

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
)

// MCPLaunchSpec is optional immutable registration data, not a connected host.
// Provider consumers merge it with existing permitted tools/servers separately.
// Its private fields prevent argument aliasing across request copies.
type MCPLaunchSpec struct {
	executable, descriptor, attempt string
	tools                           []string
}

// NewMCPLaunchSpec validates fixed registration data without starting a process.
func NewMCPLaunchSpec(executable, descriptor, attempt string, tools ...string) (*MCPLaunchSpec, error) {
	s := &MCPLaunchSpec{executable: executable, descriptor: descriptor, attempt: attempt, tools: slices.Clone(tools)}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return s, nil
}

// Validate checks absolute paths and the bounded existing attempt reference.
func (s *MCPLaunchSpec) Validate() error {
	if s == nil || strings.ContainsRune(s.executable, '\x00') || strings.ContainsRune(s.descriptor, '\x00') || !filepath.IsAbs(s.executable) || filepath.Clean(s.executable) != s.executable || !filepath.IsAbs(s.descriptor) || filepath.Clean(s.descriptor) != s.descriptor || filepath.Base(s.descriptor) != "descriptor.json" || s.attempt == "" || len(s.attempt) > 256 || strings.ContainsAny(s.attempt, "\x00\r\n") {
		return errors.New("invalid neutral MCP launch specification")
	}
	if len(s.tools) > 64 {
		return errors.New("MCP catalog exceeds limit")
	}
	seen := make(map[string]bool, len(s.tools))
	for _, name := range s.tools {
		if name == "" || len(name) > 128 || seen[name] {
			return errors.New("invalid MCP tool name")
		}
		for _, ch := range name {
			if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '_' {
				return errors.New("invalid MCP tool name")
			}
		}
		seen[name] = true
	}
	return nil
}

// Executable returns the resolved absolute factory executable.
func (s *MCPLaunchSpec) Executable() string { return s.executable }

// DescriptorPath returns the private descriptor path, never its capability.
func (s *MCPLaunchSpec) DescriptorPath() string { return s.descriptor }

// AttemptReference identifies the existing factory launch represented by this value.
func (s *MCPLaunchSpec) AttemptReference() string { return s.attempt }

// Arguments returns a fresh slice of fixed, structured serve arguments.
func (s *MCPLaunchSpec) Arguments() []string {
	return []string{"mcp", "serve", "--descriptor", s.descriptor}
}

// Clone copies immutable registration data and preserves nil for omitted policy.
func (s *MCPLaunchSpec) Clone() *MCPLaunchSpec {
	if s == nil {
		return nil
	}
	cloned := *s
	cloned.tools = slices.Clone(s.tools)
	return &cloned
}

// PermittedTools returns a defensive copy of the exact installed catalog.
func (s *MCPLaunchSpec) PermittedTools() []string { return slices.Clone(s.tools) }
