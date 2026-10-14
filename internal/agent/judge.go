package agent

import (
	"context"
	"errors"

	"github.com/cloudvane/litefactory/internal/config"
)

// JudgeRequest grants only immutable evidence reads and advisory result delivery.
// It carries no workflow session, source writer or approval authority.
type JudgeRequest struct {
	Invocation DoctorRequest
	Thinking   config.Thinking
}

// JudgeToolNames returns the exact evidence-only advisory broker catalog.
func JudgeToolNames() []string {
	return []string{"communication_preflight", "communication_receipt", "communication_submit", "evidence_list", "evidence_read"}
}

// Validate checks isolated factory attribution and its exact permitted tools.
func (r JudgeRequest) Validate() error {
	if err := r.Invocation.validateContext(); err != nil {
		return err
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
var ErrJudgeIsolationUnverified = errors.New("judge provider cannot enforce the evidence-only native tool ceiling")

// ErrJudgeUnavailable identifies an availability failure only. Output validation,
// authorization, configuration, isolation and exhausted budgets never wrap it.
var ErrJudgeUnavailable = errors.New("judge provider is unavailable")
