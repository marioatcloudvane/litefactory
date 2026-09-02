# US-6 — Capability-tier vocabulary

> As a maintainer, I want specialist agent definitions expressed in a
> vendor-neutral capability tier (`flagship`/`balanced`/`fast`) with a per-tool
> model mapping, so that `model: opus` stops being the source of truth once the
> executing tool might not be Claude.

Introduces a tool-neutral `Tier` vocabulary and a `map[Kind]map[Tier]string`
mapping to concrete model names, and moves the canonical
specialist→tier assignment out of Claude-specific frontmatter into one
litefactory-owned table.

## Why This Matters

The team's specialist definitions currently say `model: opus` or `model:
sonnet` — values that only mean anything if the tool running them is Claude.
Once a specialist might run under Codex instead, that value is not just wrong,
it's meaningless, and Codex's own model names are due to partially change within
the month regardless. This story doesn't change what any specialist actually
runs on today (`opus` stays `flagship`, `sonnet` stays `balanced` — a pure
relabeling), but it moves the source of truth to a place where "which tool, which
tier, which model" can be answered without hardcoding a vendor's naming scheme
into every specialist file.

## Architecture Notes

- Tier is a closed set — `flagship` / `balanced` / `fast` — validated at config
  load with a named error on an unknown value.
- The canonical specialist→tier assignment lives in one litefactory-owned table,
  `.aiteam/AGENT_TIERS.md`, not in `.claude/agents/*.md` frontmatter — that
  frontmatter is parsed by Claude Code, not by litefactory, so an unrecognized
  `tier:` key there would be silently ignored and the specialist would silently
  fall back to the session default. `.claude/agents/*.md` keeps a literal
  `model:` value; a test keeps it honest against `ModelByTier[claude][tier]`.
- The session-wide `--model` flag, which litefactory already passes
  unconditionally on every run, is resolved from tier → model at dispatch time
  by the selected implementation. Empty configuration still means "the
  default," never "omit the flag."
- This story splits into two halves with different risk profiles, and should be
  planned as such: the `--model` tier-resolution half is real runtime code and
  needs US-5's `RunnerSet`/factory to exist; the `.claude/agents/*.md` +
  `AGENT_TIERS.md` bookkeeping half has zero runtime effect (litefactory never
  reads either file at runtime) and can be drafted independently.
- One task (Codex tier→model values) is gated on US-1's measurement — Codex's
  actual tier names must come from `codex --help`/its config, not from web
  research.
- `internal/config/secretscan.go`'s map-of-map recursion needs a direct test
  against `Tiers.ModelByTier` (`map[Kind]map[Tier]string`) — this is currently
  unverified and is a real gap if it doesn't recurse correctly.
- No specialist's actual tier assignment changes in this feature.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-6.1 | `Tier` closed set + `ModelByTier` config shape (`map[Kind]map[Tier]string`) | Config | US-5 | M |
| T-6.2 | Tier→model resolution at dispatch time by the selected implementation; `--model` still unconditional, empty still means default | Code | T-6.1, US-5 (factory) | M |
| T-6.3 | `config.Defaults()` + `template.go` updated for the tier map | Config | T-6.1 | S |
| T-6.4 | `.aiteam/AGENT_TIERS.md` assigning every `.claude/agents/*.md` file exactly one tier; no assignment changes | Doc | — | S |
| T-6.5 | 🔒 Codex tier→model values from US-1's measurement, cited in a comment | Config | US-1, T-6.1 | S |
| T-6.6 | Consistency test: fails when a `.claude/agents/*.md` `model:` value disagrees with `ModelByTier[claude][tier]` for that specialist | Test | T-6.3, T-6.4 | S |
| T-6.7 | `secretscan.go` map-of-map recursion test against `Tiers.ModelByTier` | Test | T-6.1 | S |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-6.1 | `Tier` is a closed set of `flagship`, `balanced`, `fast`, validated at config load with a named error on an unknown value |
| AC-6.2 | `.aiteam/AGENT_TIERS.md` assigns every file in `.claude/agents/` exactly one tier |
| AC-6.3 | A test fails when a `.claude/agents/*.md` `model:` value disagrees with `ModelByTier[claude][tier]` for that specialist |
| AC-6.4 | Every model string in the mapping passes the existing `modelNameRE` |
| AC-6.5 | `--model` is still passed unconditionally on every Claude run; empty config still means the default, never an omitted flag |
| AC-6.6 | The Codex tier→model values come from US-1's measurement, cited in a comment, not from web research |
| AC-6.7 | `secretscan.go`'s map-of-map recursion is verified against `Tiers.ModelByTier` by a direct test |
| AC-6.8 | No specialist's actual model assignment changes (`opus → flagship`, `sonnet → balanced`, unchanged behavior) |
