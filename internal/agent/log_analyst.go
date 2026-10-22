package agent

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/config"
)

// ErrLogAnalystIsolation forbids launch when the exact tool ceiling is unverified.
var ErrLogAnalystIsolation = errors.New("log analyst provider isolation is unavailable")

// LogAnalystRequest carries no workflow, native resume or source access authority.
type LogAnalystRequest struct {
	Attempt          string
	Model            string
	Thinking         config.Thinking
	Prompt           string
	WorkingDirectory string
	UsageAttemptID   string
	Usage            *UsageCapture
	MCPLaunch        *MCPLaunchSpec
	// CatalogVerified opens the host tool gate only after exact native initialization.
	CatalogVerified func(context.Context, string) error
}

// LogAnalystToolNames returns the complete authorized broker catalog.
func LogAnalystToolNames() []string {
	return []string{"communication_preflight", "communication_receipt", "communication_submit", "incidents_list", "run_digest_read", "run_logs_read", "runs_list", "standards_categories_list", "standards_read", "standards_select"}
}

// Validate rejects malformed requests and any widening of the tool catalog.
func (r LogAnalystRequest) Validate() error {
	if r.Attempt == "" || len(r.Attempt) > 256 || strings.TrimSpace(r.Model) == "" || strings.TrimSpace(r.Prompt) == "" || len(r.Prompt) > 64<<10 || !filepath.IsAbs(r.WorkingDirectory) || filepath.Clean(r.WorkingDirectory) != r.WorkingDirectory {
		return errors.New("invalid log analyst invocation")
	}
	if err := r.MCPLaunch.Validate(); err != nil {
		return err
	}
	if r.MCPLaunch.AttemptReference() != r.Attempt {
		return errors.New("log analyst MCP attempt mismatch")
	}
	got := r.MCPLaunch.PermittedTools()
	slices.Sort(got)
	if !slices.Equal(got, LogAnalystToolNames()) {
		return errors.New("log analyst requires its exact scoped tool catalog")
	}
	if r.Usage != nil && (r.UsageAttemptID == "" || r.Usage.AttemptID != r.UsageAttemptID) {
		return errors.New("log analyst usage attempt mismatch")
	}
	return nil
}

// LogAnalystRunResult contains provider observations, never an accepted report.
type LogAnalystRunResult struct {
	ObservedModel    string
	ProviderVersion  string
	IsolationProfile string
	Usage            *UsageCoverage
}

// LogAnalystRunner is the provider-owned standalone execution boundary.
type LogAnalystRunner interface {
	LogAnalystIsolation(context.Context) (string, error)
	RunLogAnalyst(context.Context, LogAnalystRequest) (LogAnalystRunResult, error)
}
