package cli

import (
	"log/slog"
	"os"

	"github.com/cloudvane/litefactory/internal/codex"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/messaging/response"
	"github.com/cloudvane/litefactory/internal/projectread"
)

// newStandardsAuthoringRunner answers through the response-only model; reads
// serves the project of a project-scoped session.
func newStandardsAuthoringRunner(rt *repoRuntime, reads projectread.Session) *response.Authoring {
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
	return &response.Authoring{Model: &response.NativeModel{Config: rt.cfg, Paths: rt.paths, Providers: providers, Meter: rt.db}, Reads: reads}
}
