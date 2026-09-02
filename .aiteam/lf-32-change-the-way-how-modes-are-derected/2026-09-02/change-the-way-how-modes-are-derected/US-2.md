# US-2 — Configure and validate the default coding agent

> As an operator, I want to choose the default coding agent so that unlabelled work and mode classification use the implementation I trust.

This story replaces the implicit Claude fallback with an explicit compatible setting.

## Why This Matters

The daemon currently assumes one vendor whenever an issue has no agent label.
Adding classification would otherwise duplicate and deepen that hidden default.
One visible setting keeps ordinary dispatch and routing aligned as more coding
agents are configured.

## Architecture Notes

- Add `default_agent` to configuration defaults and the generated template,
  preserving strict decoding and aggregate validation.
- Validate name shape in `internal/config`, then validate registry membership in
  daemon setup where configured implementations are known.
- Platform: configuration must not import runtime agent or daemon packages.
- Risk: an agent can be syntactically valid but unavailable because its binary
  or stanza was not configured; startup must fail rather than defer this to a
  comment-triggered run.

## Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|------------|-----|
| T2.1 | Add `default_agent` with the compatibility default `Claude` to config and template | Code | — | S |
| T2.2 | Add pure shape validation and runtime runner-registry validation | Code | T2.1 | M |
| T2.3 | Replace hard-coded unlabelled-agent fallbacks and expose the resolved value in startup diagnostics | Code | T2.2 | S |
| T2.4 | Cover decode, drift, aggregated errors, unavailable agent, and Codex-default cases | Test | T2.3 | S |

## Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC2.1 | An omitted setting resolves to Claude and preserves existing unlabelled dispatch. |
| AC2.2 | A configured canonical agent with a registered runner becomes the default for ordinary turns and classification. |
| AC2.3 | Malformed and configured-but-unavailable defaults refuse daemon startup with an actionable error. |
| AC2.4 | The generated default configuration decodes to `Defaults()` without drift. |
| AC2.5 | Configuration validation remains aggregate and `internal/config` retains its dependency purity. |

