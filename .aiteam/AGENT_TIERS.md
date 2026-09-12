# AGENT_TIERS — specialist → capability tier

This is the canonical specialist→tier assignment (US-6). It exists because
`.claude/agents/*.md` frontmatter is parsed by Claude Code, not by
litefactory: an unrecognised `tier:` key there would be silently ignored and
the specialist would silently fall back to whatever the session default
turns out to be. This file is the litefactory-owned place that answer
survives instead — every file in `.claude/agents/` is assigned exactly one
tier here (AC-6.2), and `internal/config`'s consistency test
(`TestAgentTiersMatchClaudeAgentsFrontmatter`, T-6.6) fails the build if a
row here and the `model:` value in the specialist's own frontmatter ever
disagree with `config.Defaults().Tiers.ModelByTier["claude"]` for that tier.

**This is bookkeeping, not runtime code.** litefactory never reads this file
or `.claude/agents/*.md` at runtime — nothing here changes what any
specialist actually runs on (AC-6.8). `.claude/agents/*.md` keeps its
literal `model:` value because Claude Code needs a literal value there; this
table is what makes that value traceable back to a vendor-neutral tier
instead of being the only record of it.

Tiers are the closed set `flagship` / `balanced` / `fast`
(`internal/config.Tier`). The concrete model each tier resolves to, per
coding-agent tool, lives in `internal/config`'s `Tiers.ModelByTier` —
`claude` resolves to `opus` / `sonnet` / `haiku` and `codex` to
`gpt-6-astra` / `gpt-6-astra` / `gpt-5.6-luna`, for `flagship` / `balanced`
/ `fast` respectively. Codex's GPT-6 family lists a single slug, so its
flagship and balanced tiers share it; see that package for the measurement
behind those values.

No specialist's actual model assignment changes in this feature: every row
below is a pure relabelling of the `model:` value the file already had.

| Specialist file | Tier | Claude model (unchanged) |
|---|---|---|
| `go-architect.md` | flagship | opus |
| `go-code-reviewer.md` | balanced | sonnet |
| `go-implementation-engineer.md` | balanced | sonnet |
| `go-integration-test-engineer.md` | balanced | sonnet |
| `go-unit-test-engineer.md` | balanced | sonnet |
| `implement-orchestrator.md` | balanced | sonnet |
| `plan-orchestrator.md` | balanced | sonnet |
| `proxy-product-owner.md` | balanced | sonnet |
| `requirements-engineer.md` | flagship | opus |
| `research-orchestrator.md` | balanced | sonnet |

No specialist is currently assigned `fast` — nothing in the current roster
needs a model cheaper than `balanced`. `fast` stays in the vocabulary
(`internal/config.TierFast`) for the specialist that does.
