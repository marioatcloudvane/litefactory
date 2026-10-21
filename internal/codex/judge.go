package codex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/providerprocess"
)

// RunJudge starts a fresh native turn with a factory-owned model catalogue and
// a factory permissions profile: the judge reads only the pinned checkout and
// the context files, with shell commands that cannot write or reach the network. The
// catalogue removes model-owned apply_patch before Codex builds its registry.
func (r *Runner) RunJudge(ctx context.Context, request agent.JudgeRequest) (_ agent.JudgeRunResult, err error) {
	if err = request.Validate(); err != nil {
		return agent.JudgeRunResult{}, err
	}
	if err = config.ValidateThinking(config.KindCodex, request.Invocation.Model, request.Thinking); err != nil {
		return agent.JudgeRunResult{}, err
	}
	if err = ctx.Err(); err != nil {
		return agent.JudgeRunResult{}, err
	}
	if _, err = os.Stat(r.binary.Path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return agent.JudgeRunResult{}, agent.ErrJudgeUnavailable
		}
		return agent.JudgeRunResult{}, errors.New("judge client cannot be inspected")
	}
	// The judge reads its working directory, so the private home (with the auth
	// link and the factory catalogue) lives in the system temp directory.
	home, err := os.MkdirTemp("", "codex-judge-private-")
	if err != nil {
		return agent.JudgeRunResult{}, err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(home)) }()
	probeEnv := append(providerprocess.DoctorEnvironment(r.environ, home), "CODEX_HOME="+home)
	versionText, probeErr := providerprocess.ConversationProbe(ctx, r.binary.Path, []string{"--version"}, probeEnv, home)
	if probeErr != nil {
		if ctx.Err() != nil {
			return agent.JudgeRunResult{}, ctx.Err()
		}
		return agent.JudgeRunResult{}, errors.Join(agent.ErrJudgeIsolationUnverified, errors.New("judge client version could not be verified"))
	}
	version := judgeClientVersion.FindStringSubmatch(strings.TrimSpace(versionText))
	if len(version) != 2 {
		return agent.JudgeRunResult{}, agent.ErrJudgeIsolationUnverified
	}

	bundled, err := r.judgeBundledModel(ctx, request, probeEnv)
	if err != nil {
		return agent.JudgeRunResult{}, err
	}
	catalog := filepath.Join(home, "judge-models.json")
	if err = writeJudgeCatalog(catalog, request, bundled); err != nil {
		return agent.JudgeRunResult{}, err
	}
	args, err := r.judgeArguments(request, catalog)
	if err != nil {
		return agent.JudgeRunResult{}, err
	}
	if err = r.linkDoctorAuth(home); err != nil {
		return agent.JudgeRunResult{}, err
	}
	env := append(providerprocess.DoctorEnvironment(r.environ, home), "CODEX_HOME="+home)
	for _, entry := range r.environ {
		if strings.HasPrefix(entry, "CODEX_API_KEY=") {
			env = append(env, entry)
		}
	}
	collector := newUsageCaptureCollector(ctx, request.Invocation.Usage)
	state := streamState{}
	err = providerprocess.RunJudgeProcess(ctx, r.binary.Path, args, env, request, r.timeout, func(line []byte) { collector.observe(line); state.consumeLine(line) })
	if state.failed && ctx.Err() == nil && (err == nil || errors.Is(err, providerprocess.ErrProviderExit)) && judgeCapacity(state.failureDetail) {
		err = agent.ErrJudgeUnavailable
	}
	if err == nil && (!state.completed || state.failed) {
		err = errors.New("judge provider did not report a successful terminal turn")
	}
	if err != nil && state.failureDetail != "" {
		err = fmt.Errorf("judge provider turn failed: %s: %w", boundedDoctorDetail(state.failureDetail), err)
	}
	return agent.JudgeRunResult{ProviderVersion: version[1], Usage: collector.finish()}, err
}

// judgeCapacity applies only to an explicit failed native terminal event.
// Invalid intent, authorization and exhausted budgets cannot grant fallback,
// even when a provider attaches an otherwise retryable HTTP status.
func judgeCapacity(detail string) bool {
	text := strings.ToLower(detail)
	for _, marker := range []string{"unsupported", "not supported", "does not support", "invalid", "unknown model", "model not found", "does not exist", "budget", "context length", "max tokens", "token limit", "timeout", "deadline", "cancel", "quota", "usage limit", "credit", "billing", "payment", "authentication", "unauthorized", "forbidden", "api key", "api_key", "permission", "configuration"} {
		if strings.Contains(text, marker) {
			return false
		}
	}
	if agent.ProviderCapacity(detail) != nil {
		return true
	}
	return judgeUnavailableHTTP.MatchString(text)
}

var judgeUnavailableHTTP = regexp.MustCompile(`\b(?:http(?: status)?|status(?: code)?)[:= ]+(500|502|503|504)\b`)

// The native tool registry and catalogue controls were verified for 0.160.
// A different client family must be checked before it gains this capability.
var judgeClientVersion = regexp.MustCompile(`^codex-cli (0\.160\.[0-9]+)$`)
