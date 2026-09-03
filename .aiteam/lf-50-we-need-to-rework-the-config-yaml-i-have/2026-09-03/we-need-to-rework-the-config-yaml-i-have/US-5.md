# US-5 — Tier-only model selection

> As an operator, I want agents to select a model tier so that concrete model names are managed centrally and consistently.

This story removes redundant per-agent model names and resolves each agent's tier at startup.

## Why This Matters

Keeping both a literal model and a tier creates precedence that is easy to misunderstand. Tier-only selection leaves one clear control on each agent while retaining concrete model choices in one central table.

## Architecture Notes

- Keep literal model identifiers only in the tier mapping; resolve configured-agent tiers once during startup before constructing runners.
- Platform: cross-validation participates in aggregated CFG-5 errors; runners consume resolved concrete models and do not implement tier policy.
- Risk: missing mappings must not fall back silently, and model metadata must reflect the resolved value.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-5.1 | Make model tier the sole per-agent selector and validate tier tables | logic | T-3.1 | L |
| T-5.2 | Pass startup-resolved models to Claude and Codex runners | logic | T-3.2, T-5.1 | M |
| T-5.3 | Cover tier defaults, cross-references, diagnostics, runner arguments, and metadata | test | T-5.2 | L |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-5.1 | Per-agent literal `model` is rejected with guidance to use tier mappings. |
| AC-5.2 | Every configured agent defaults to the `balanced` tier when its selector is omitted. |
| AC-5.3 | Each selected tier resolves once through that agent's mapping before runner construction. |
| AC-5.4 | Missing, empty, unknown, or agent-mismatched mappings fail startup without fallback. |
| AC-5.5 | Both runners and recorded metadata use the resolved concrete model. |

