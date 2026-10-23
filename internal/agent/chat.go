package agent

import (
	"errors"
	"path/filepath"
	"strings"
)

// ChatRequest is response-only, with no resume, workflow or files. Providers
// must use their doctor isolation gate and disable all native tools. The
// scratch directory contains no repository data. MCPLaunch optionally grants
// exactly its own read-only factory tools; nil grants none.
type ChatRequest struct {
	Prompt, Model, WorkingDirectory string
	Usage                           *UsageCapture
	MCPLaunch                       *MCPLaunchSpec
}

// Validate checks bounded context before a provider process may be launched.
func (r ChatRequest) Validate() error {
	if strings.TrimSpace(r.Prompt) == "" || len(r.Prompt) > 256<<10 || strings.TrimSpace(r.Model) == "" || !filepath.IsAbs(r.WorkingDirectory) || filepath.Clean(r.WorkingDirectory) != r.WorkingDirectory {
		return errors.New("invalid chat provider context")
	}
	if r.Usage != nil && r.Usage.AttemptID == "" {
		return errors.New("chat usage identity missing")
	}
	if r.MCPLaunch != nil {
		return r.MCPLaunch.Validate()
	}
	return nil
}

// ChatResult carries typed answer bytes and existing provider usage observations.
// Unlike workflow results, this response cannot request a factory operation.
type ChatResult struct {
	Answer []byte
	Usage  *UsageCoverage
}
