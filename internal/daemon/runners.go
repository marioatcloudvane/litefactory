package daemon

import (
	"fmt"
	"log/slog"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/codex"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// buildRunners constructs every coding-agent-tool implementation the
// configuration selects, once, at daemon startup.
//
// It is what makes a Codex binary that cannot be resolved a startup error
// rather than a later-tick surprise (AC-5.7), the same posture [Open] already
// applies to git and to the forge client: everything that can fail without
// touching anything happens before the singleton lock is taken. It returns a
// concrete [turn.RunnerSet] rather than an interface — "accept interfaces,
// return structs" — because the caller needs the whole registry, not one
// narrowed method of it.
//
// Only Claude is built today. A second implementation is registered here and
// nowhere else: internal/turn and internal/report do not switch on
// [agent.Kind] at all, which is the whole point of the registry-driven design
// (AC-5.6, AC-5.15).
func buildRunners(paths config.Paths, cfg *config.Config, environ []string, logger *slog.Logger) (turn.RunnerSet, error) {
	claudeRunner, err := harness.New(harness.Options{
		Paths:   paths,
		Claude:  cfg.Claude,
		Tiers:   cfg.Tiers,
		Environ: environ,
		Logger:  logger,
	})
	if err != nil {
		return nil, fmt.Errorf("the daemon needs claude: %w", err)
	}

	runners := turn.RunnerSet{
		agent.Kind(store.AgentClaude): claudeRunner,
	}
	if cfg.Agents.Codex != nil {
		codexRunner, err := codex.New(codex.Options{Paths: paths, Codex: *cfg.Agents.Codex, Tiers: cfg.Tiers, Environ: environ, Logger: logger})
		if err != nil {
			return nil, fmt.Errorf("the daemon needs codex: %w", err)
		}
		runners[agent.Kind(store.AgentCodex)] = codexRunner
	}
	return runners, nil
}
