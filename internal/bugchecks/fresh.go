package bugchecks

import (
	"context"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/config"
)

// RunFresh applies invocation-local result-cache controls while preserving
// configured repetitions and effective Go environment settings.
func (p Isolation) RunFresh(ctx context.Context, worktree string, c config.BugCheckCommand, started func(CommandResult) error) CommandResult {
	// Direct legacy invocations have no adapter facts. Observe effective saved
	// GOFLAGS through the same sandbox before choosing their repeat count.
	if len(c.Argv) > 1 && filepath.Base(c.Argv[0]) == "go" && c.Argv[1] == "test" && !hasTestCount(c.Argv[2:]) {
		probe := c
		probe.Observe = ""
		probe.Argv = []string{c.Argv[0], "env", "GOFLAGS"}
		result := p.Run(ctx, worktree, probe, started)
		if result.Outcome != "passed" || !result.CaptureComplete || !result.CleanupConfirmed {
			result.Outcome = "blocked"
			result.Reason = "effective Go flags could not be observed: " + result.Reason
			return result
		}
		if count, seen := testCount(strings.Fields(result.Output)); seen {
			n, e := strconv.Atoi(count)
			if e != nil || n < 1 {
				result.Outcome = "blocked"
				result.Reason = "configured Go repetitions cannot prove test execution"
				return result
			}
			c.Argv = append(slices.Clone(c.Argv[:2]), append([]string{"-count=" + count}, c.Argv[2:]...)...)
		}
	}
	p, c = p.FreshCommand(c)
	if c.Observe != "" {
		return p.runObserved(ctx, worktree, c, started)
	}
	return p.Run(ctx, worktree, c, started)
}

// FreshCommand returns independent invocation settings. Metadata commands stay
// unchanged; their effective environment is evidence for compatibility.
func (p Isolation) FreshCommand(c config.BugCheckCommand) (Isolation, config.BugCheckCommand) {
	if len(c.Argv) < 2 || c.Argv[1] != "test" {
		return p, c
	}
	switch filepath.Base(c.Argv[0]) {
	case "go":
		if !hasTestCount(c.Argv[2:]) && slices.Contains(c.Env, "GOFLAGS") {
			if count, seen := testCount(strings.Fields(p.Environment["GOFLAGS"])); seen {
				if repetitions, err := strconv.Atoi(count); err == nil && repetitions > 0 {
					return p, c
				}
			}
		}
		if !hasTestCount(c.Argv[2:]) {
			c.Argv = append(slices.Clone(c.Argv[:2]), append([]string{"-count=1"}, c.Argv[2:]...)...)
		}
	case "make":
		// The verified make observer adds controls to actual Go test invocations,
		// retaining the Makefile's own exported GOFLAGS exactly.
		if c.Observe == "make-go" {
			return p, c
		}
		p.Environment = maps.Clone(p.Environment)
		if p.Environment == nil {
			p.Environment = map[string]string{}
		}
		original := ""
		if slices.Contains(c.Env, "GOFLAGS") {
			original = p.Environment["GOFLAGS"]
		}
		p.Environment["GOFLAGS"] = original
		if !hasTestCount(strings.Fields(original)) {
			p.Environment["GOFLAGS"] = strings.TrimSpace(original + " -count=1")
		}
		c.Env = slices.Clone(c.Env)
		if !slices.Contains(c.Env, "GOFLAGS") {
			c.Env = append(c.Env, "GOFLAGS")
		}
	}
	return p, c
}

func hasTestCount(argv []string) bool { _, seen := testCount(argv); return seen }
func testCount(argv []string) (string, bool) {
	for i, arg := range argv {
		if arg == "-args" || arg == "--args" || arg == "--" {
			break
		}
		if strings.HasPrefix(arg, "-count=") {
			return strings.TrimPrefix(arg, "-count="), true
		}
		if arg == "-count" {
			if i+1 < len(argv) {
				return argv[i+1], true
			}
			return "", true
		}
	}
	return "", false
}
