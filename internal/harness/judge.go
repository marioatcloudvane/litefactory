package harness

import (
	"context"
	"errors"
	"fmt"
	execution "github.com/cloudvane/litefactory/pkg/execution/agent"
	native "github.com/cloudvane/litefactory/pkg/execution/providers/claude"
	"os"
	"regexp"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
)

// RunJudge uses the scoped broker transport with only the read-only native
// tools Read, Grep and Glob, confined to the pinned checkout and context files.
// This separate gate neither opens nor alters diagnosis isolation.
func (r *Runner) RunJudge(ctx context.Context, request agent.JudgeRequest) (agent.JudgeRunResult, error) {
	if err := request.Validate(); err != nil {
		return agent.JudgeRunResult{}, err
	}
	if err := config.ValidateThinking(config.KindClaude, request.Invocation.Model, request.Thinking); err != nil {
		return agent.JudgeRunResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return agent.JudgeRunResult{}, err
	}
	if request.Thinking != "" {
		// This bounded local help request cannot invoke a model or inherit model
		// alias environment overrides, user settings or authentication credentials.
		help, err := native.Probe(ctx, r.binary.Path, []string{"--help"}, execution.IsolatedEnvironment(r.environ, request.Invocation.WorkingDirectory), request.Invocation.WorkingDirectory)
		if err != nil {
			var pathErr *os.PathError
			if errors.As(err, &pathErr) && pathErr.Path == r.binary.Path && errors.Is(err, os.ErrNotExist) {
				return agent.JudgeRunResult{}, agent.ErrJudgeUnavailable
			}
			if ctx.Err() != nil {
				return agent.JudgeRunResult{}, ctx.Err()
			}
			return agent.JudgeRunResult{}, errors.New("judge client effort capability could not be verified")
		}
		if err = validateJudgeEffortHelp(help, request.Thinking); err != nil {
			return agent.JudgeRunResult{}, err
		}
	}
	request.Invocation.Thinking = request.Thinking
	result, err := r.runScopedDoctorTransport(ctx, request.Invocation, "judge")
	if err == nil && request.Thinking != "" && result.ObservedModel != "" {
		if capabilityErr := config.ValidateThinking(config.KindClaude, result.ObservedModel, request.Thinking); capabilityErr != nil {
			return result, fmt.Errorf("observed judge model cannot verify requested effort: %w", capabilityErr)
		}
	}
	return result, err
}

var judgeEffortWord = regexp.MustCompile(`\b(low|medium|high|xhigh|max|ultra|minimal)\b`)

func validateJudgeEffortHelp(help string, thinking config.Thinking) error {
	lines := strings.Split(help, "\n")
	for i, line := range lines {
		if !strings.Contains(line, "--effort") {
			continue
		}
		section := line
		for j := i + 1; j < len(lines) && j < i+4; j++ {
			continuation := strings.TrimSpace(lines[j])
			if continuation == "" || strings.HasPrefix(continuation, "-") {
				break
			}
			section += " " + continuation
		}
		// Some clients advertise an open <level> argument; the exact model table
		// remains the ceiling then. When help enumerates levels, honor that ceiling too.
		levels := judgeEffortWord.FindAllString(strings.ToLower(section), -1)
		if len(levels) > 0 {
			for _, level := range levels {
				if level == string(thinking) {
					return nil
				}
			}
			return fmt.Errorf("installed judge client does not advertise effort %q", thinking)
		}
		return nil
	}
	return errors.New("installed judge client does not advertise --effort")
}
