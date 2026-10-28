package turn

import (
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/coverage"
)

// coverageFreshnessReadiness applies the same trusted rollout policy to actual
// and injected discoveries. Dependency capability does not establish that a
// native runner has passed the real test-body freshness checks.
func coverageFreshnessReadiness(commands []config.BugCheckCommand, inventory coverage.Inventory, facts map[int]coverage.Facts, bindings *ValidateCoverageBindings) (map[string]bool, string) {
	policy := map[string]bool{}
	verified := func(runner string) bool {
		if value, known := policy[runner]; known {
			return value
		}
		ready := runner == coverage.RunnerGo || runner == coverage.RunnerPytest
		if bindings != nil && bindings.FreshnessVerified != nil {
			ready = bindings.FreshnessVerified(runner)
		}
		policy[runner] = ready
		return ready
	}
	// Record every supported policy bit even when this recipe uses one runner,
	// so a changed rollout contract cannot silently inherit incompatible proof.
	for _, runner := range []string{coverage.RunnerGo, coverage.RunnerPytest, coverage.RunnerVSTest, coverage.RunnerMTP, coverage.RunnerXcode} {
		verified(runner)
	}
	pending := []string{}
	check := func(runner string) {
		if runner != "" && !verified(runner) {
			pending = append(pending, runner)
		}
	}
	for i, command := range commands {
		check(facts[i].Runner)
		// Also inspect recognizable original commands, so an injected discovery
		// cannot label an actual native command as an enabled runner.
		check(coverageCommandRunner(command))
	}
	for _, unit := range inventory.Units {
		check(unit.Runner)
	}
	if len(pending) == 0 {
		return policy, ""
	}
	sort.Strings(pending)
	pending = slices.Compact(pending)
	return policy, "pending native freshness evidence for " + strings.Join(pending, ", ") + "; executing the unchanged full recipe"
}

func coverageCommandRunner(command config.BugCheckCommand) string {
	if runner, known := coverage.KnownWrapper(command); known {
		return runner
	}
	argv := command.Argv
	if len(argv) == 0 {
		return ""
	}
	switch filepath.Base(argv[0]) {
	case "go", "make":
		if len(argv) > 1 && argv[1] == "test" {
			return coverage.RunnerGo
		}
	case "pytest":
		return coverage.RunnerPytest
	case "python", "python3":
		if len(argv) > 2 && argv[1] == "-m" && argv[2] == "pytest" {
			return coverage.RunnerPytest
		}
	case "dotnet":
		if len(argv) > 1 && argv[1] == "test" {
			if slices.Contains(argv, "--project") {
				return coverage.RunnerMTP
			}
			return coverage.RunnerVSTest
		}
	case "xcodebuild":
		return coverage.RunnerXcode
	}
	return ""
}

func appendFreshnessReason(reason, pending string) string {
	if pending == "" || strings.Contains(reason, pending) {
		return reason
	}
	if reason == "" {
		return pending
	}
	return reason + "; " + pending
}
