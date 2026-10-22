package agent

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/cloudvane/litefactory/internal/config"
)

// JudgeRequest grants read-only access to WorkingDirectory (a detached checkout
// of the pinned commit) and Invocation.ReadableDirectories (the context files),
// plus advisory result delivery. It carries no workflow session, source writer
// or approval authority.
type JudgeRequest struct {
	Invocation DoctorRequest
	Thinking   config.Thinking
}

// JudgeToolNames returns the exact advisory broker catalog. The judge reads
// files with the provider's own read-only tools, not through the broker.
func JudgeToolNames() []string {
	return []string{"communication_preflight", "communication_receipt", "communication_submit"}
}

// Validate checks isolated factory attribution, readable roots and its exact permitted tools.
func (r JudgeRequest) Validate() error {
	if err := r.Invocation.validateContext(); err != nil {
		return err
	}
	for _, dir := range r.Invocation.ReadableDirectories {
		if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir || dir == string(filepath.Separator) || dir == r.Invocation.WorkingDirectory {
			return errors.New("judge readable directory is not a canonical absolute path")
		}
	}
	return validateDoctorTools(r.Invocation.MCPLaunch, JudgeToolNames())
}

// JudgeRunResult records provider observations independently of advisory content.
type JudgeRunResult = DoctorRunResult

// JudgeRunner runs one isolated advisory invocation without workflow authority.
type JudgeRunner interface {
	RunJudge(context.Context, JudgeRequest) (JudgeRunResult, error)
}

// ErrJudgeIsolationUnverified refuses a provider without the required tool ceiling.
var ErrJudgeIsolationUnverified = errors.New("judge provider cannot enforce the read-only native tool ceiling")

// ErrJudgeUnavailable identifies an availability failure only. Output validation,
// authorization, configuration, isolation and exhausted budgets never wrap it.
var ErrJudgeUnavailable = errors.New("judge provider is unavailable")
