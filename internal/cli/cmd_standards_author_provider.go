package cli

import (
	"context"
	"log/slog"
	"os"

	"github.com/cloudvane/litefactory/internal/codex"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/messaging/response"
	"github.com/cloudvane/litefactory/internal/standards"
)

type standardsAuthoringNative struct {
	model                                   *response.NativeModel
	provider, selectedModel, usageReference string
}

func (r *standardsAuthoringNative) Metadata() (string, string, string) {
	return r.provider, r.selectedModel, r.usageReference
}

func (r *standardsAuthoringNative) Answer(ctx context.Context, session, prompt string) ([]byte, error) {
	return r.model.AnswerAuthoring(ctx, session, prompt)
}

func (r *standardsAuthoringNative) AnswerAttempt(ctx context.Context, session, attempt, token, prompt string) ([]byte, error) {
	return r.model.AnswerAuthoring(ctx, session, prompt, attempt, token)
}

func newStandardsAuthoringRunner(rt *repoRuntime) standards.AuthoringRunner {
	providers := make(map[string]response.ChatRunner)
	logger := slog.New(slog.DiscardHandler)
	environ := os.Environ()
	if rt.cfg.Agents.Claude != nil {
		model, _ := rt.cfg.Tiers.ResolveModel(config.Kind("claude"), config.TierBalanced)
		if runner, err := harness.New(harness.Options{Paths: rt.paths, Claude: *rt.cfg.Agents.Claude, Model: model, Environ: environ, Logger: logger}); err == nil {
			providers["claude"] = runner
		}
	}
	if rt.cfg.Agents.Codex != nil {
		model, _ := rt.cfg.Tiers.ResolveModel(config.Kind("codex"), config.TierBalanced)
		if runner, err := codex.New(codex.Options{Paths: rt.paths, Codex: *rt.cfg.Agents.Codex, Model: model, Environ: environ, Logger: logger}); err == nil {
			providers["codex"] = runner
		}
	}
	r := &standardsAuthoringNative{model: &response.NativeModel{Config: rt.cfg, Paths: rt.paths, Providers: providers, Meter: rt.db}}
	r.model.AuthoringCall = func(provider, model, ref string) {
		r.provider, r.selectedModel, r.usageReference = provider, model, ref
	}
	return r
}
