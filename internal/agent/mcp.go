package agent

import (
	"errors"
	"path/filepath"
	"strings"
)

// MCPLaunchSpec is optional immutable registration data, not a connected host.
// Provider consumers merge it with existing permitted tools/servers separately.
// Its private fields prevent argument aliasing across request copies.
type MCPLaunchSpec struct{ executable, descriptor, attempt string }

// NewMCPLaunchSpec validates fixed registration data without starting a process.
func NewMCPLaunchSpec(executable, descriptor, attempt string) (*MCPLaunchSpec, error) {
	s := &MCPLaunchSpec{executable: executable, descriptor: descriptor, attempt: attempt}
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
	return &cloned
}
