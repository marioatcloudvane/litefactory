package graphexec

import "github.com/cloudvane/litefactory/internal/agent"

// ValidateSuiteAction runs the full suite of the project's declared tests on
// one exact commit and retains the result. It runs nothing after that.
const ValidateSuiteAction = "lf_validate_suite"

// ValidateSuiteSpec declares the outcomes of a suite run: passed when every
// full-phase command exited 0, failed when one exited 1, blocked when the suite
// could not give a trustworthy answer (no recipe, an empty full phase, an
// infrastructure exit, a changed snapshot or an unconfirmed earlier run), and
// retry when another suite holds the machine-wide lock.
func ValidateSuiteSpec() ActionSpec {
	return ActionSpec{Name: ValidateSuiteAction, Mode: agent.ModeValidate, Outcomes: []string{"passed", "covered", "failed", "blocked", "retry"}, Attributes: map[string]AttributeSpec{"lf_coverage": {Values: []string{"v1"}}}}
}
