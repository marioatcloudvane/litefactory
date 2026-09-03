# Plan Brief — we-need-to-rework-the-config-yaml-i-have
# Branch: lf/50-we-need-to-rework-the-config-yaml-i-have
# Date: 2026-09-03
# Parent issue: https://github.com/marioatcloudvane/litefactory/issues/50
# Citation base: 7496d3927bec591e8d364f0d89dc57b7a6fae9c6

## Shared Decisions

- The canonical root hierarchy is `settings`, `providers`, `agents`, `tiers`, and `tool_profiles`; legacy root locations are rejected with actionable replacement paths and are never rewritten automatically.
- `settings` owns daemon and repository defaults. `providers.github` is the only implemented provider; the shape permits future providers without creating abstractions for them now.
- `agents` is an ordered YAML mapping containing fully typed `claude` and/or `codex` entries. At least one agent is required, and the first declared agent is the default for unlabelled work and mode classification.
- Per-agent literal `model` is removed. Each configured agent defaults to `model_tier: balanced`, and the selected tier resolves through `tiers.model_by_tier.<agent>` once at startup.
- `tool_profiles.permissions` contains exactly `research`, `plan`, `implement`, and `validate`; every mode contains exactly one stanza for every configured agent.
- Permissions are selected automatically after the run mode and agent are resolved. Spike remains a persisted run mode but uses Implement permissions.
- Validate becomes a first-class run mode, remains unable to ask the owner questions, and receives read-only/source-non-editing permissions suitable for bounded project verification.
- `lf:tools=` labels cease granting permissions. Any such label refuses dispatch with migration guidance and without echoing hostile label text.
- Agent-specific permission mechanics remain distinct: common profiles express equivalent intent, not identical fields or cross-agent conversion.
- The generated configuration is the complete canonical runnable structure, derives values from runtime defaults, and explains semantic agent ordering and tier selection.

## Workflow

- Shipping order: US-1 settings foundation → US-2 provider hierarchy and US-3 agent hierarchy → US-4 ordered default and US-5 tier-only resolution → US-7 profile matrix and Validate mode → US-6 automatic profile selection → US-8 generated configuration and startup presentation.
- US-2 and US-3 may proceed after US-1. US-4 and US-5 depend on the canonical agent shape. US-7 can add the mode vocabulary independently but needs US-3 for profile validation. US-6 requires both ordered routing and the complete mode/profile contract. US-8 integrates all prior schema decisions and ships last.
- The canonical schema, all consumers, migration diagnostics, generated file, and tests ship together; no intermediate build may accept a partial profile matrix or silently fall back at dispatch.

## Cross-Story Constraints

- Platform: keep YAML ownership, defaulting, normalization, validation, order extraction, and policy checks in `internal/config`; add no package or dependency; preserve dependency direction and keep command wiring thin.
- Platform: preserve agent declaration order through `yaml.v3` node inspection paired with the strict typed decoder; never use Go map iteration as product behavior and never weaken known-field, duplicate-key, or single-document checks.
- Security: preserve literal-secret scanning (CFG-4), forbidden-flag scanning (SEC-1), Claude's non-empty tool ceiling (SEC-3), Codex's opt-in network/user-config/writable-root widening (SEC-4), and sanitized refusal output (SEC-5).
- Security: profile selection must never broaden agent-global authority, and every missing agent, tier mapping, mode, profile stanza, or executable must fail at startup without fallback.
- Tests: retain deterministic table-driven tests and existing fakes (TEST-1/2/3/7); inspect runner arguments rather than execute real agents (EXT-1); avoid sleeps (TEST-8/9); preserve production-path generated-config equivalence (TEST-10).
- Concurrency: loaded configuration is immutable after startup; copy slices crossing request/runner boundaries (CONC-1), propagate contexts (CONC-5), and introduce no new goroutines or lower-layer retry clocks (CONC-6).
- Documentation: update comments and messages made stale by the hierarchy, Validate mode, tier-only models, ordered defaults, and removal of label-selected permissions (STYLE-1); read each touched package's `doc.go` before edits (STYLE-7).
- Ring-fenced: no database schema change unless verification finds a closed mode constraint; no runtime-state edits, automatic config rewriting, new provider implementation, retention automation, or generic provider/agent framework.

## Story Index

| ID | Title | Size | Depends On | Sub-issue |
|----|-------|------|-----------|-----------|
| US-1 | Canonical global settings | M | — | https://github.com/marioatcloudvane/litefactory/issues/51 |
| US-2 | Providers hierarchy | M | US-1 | https://github.com/marioatcloudvane/litefactory/issues/52 |
| US-3 | Unified agent hierarchy | L | US-1 | https://github.com/marioatcloudvane/litefactory/issues/53 |
| US-4 | Ordered default agent | L | US-3 | https://github.com/marioatcloudvane/litefactory/issues/54 |
| US-5 | Tier-only model selection | L | US-3 | https://github.com/marioatcloudvane/litefactory/issues/55 |
| US-6 | Mode-selected permissions | L | US-4, US-7 | https://github.com/marioatcloudvane/litefactory/issues/56 |
| US-7 | Canonical profiles and Validate mode | L | US-3 | https://github.com/marioatcloudvane/litefactory/issues/57 |
| US-8 | Complete generated configuration | L | US-1–US-7 | https://github.com/marioatcloudvane/litefactory/issues/58 |

## Out of Scope

- GitLab, Bitbucket, or any provider other than GitHub.
- Automatic migration or rewriting of existing configuration files.
- Compatibility aliases that continue accepting the legacy hierarchy.
- A generic provider framework or lowest-common-denominator agent settings type.
- Automatic worktree retention cleanup or changed repository-default lifecycle semantics.
- New external dependencies, database features, or concurrency mechanisms.
- Broadening which modes may pause to ask the owner questions.

## Provenance

- Citation base: `7496d3927bec591e8d364f0d89dc57b7a6fae9c6`.
- Known-stale areas: existing comments and generated examples describing Claude-only defaults, Spike-only mode vocabulary, Codex sandbox/tier limitations, and label-selected tool profiles are expected to be replaced by this work.
- Re-pinning requirements: before merge, re-check every closed mode list, configuration consumer, runner constructor, label refusal path, generated-template equivalence test, and store mode constraint against the merge-base revision.

