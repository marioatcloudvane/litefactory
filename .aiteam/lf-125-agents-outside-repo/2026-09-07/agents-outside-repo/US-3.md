# US-3 — Per-run bundle isolation and conversation boundaries

> As an operator, I want each run to see only its selected mode's specialists so that unrelated instructions do not consume tokens or remain in resumed conversations.

Prove supported runner capabilities and connect immutable external bundles to existing execution and conversation machinery.

## Why This Matters

Moving files alone does not reduce the instructions a coding agent sees. Both supported tools must actually exclude unrelated catalogs while keeping project instructions and authentication working. Their current renderers do not yet prove that behavior, so the transport choice needs direct evidence.

## Architecture Notes

- Exact-version scratch probes must settle transport before production argv/configuration decisions. Prove catalog exclusion, classifier isolation, authentication, project bootstrap retention, permission ceilings, tier/model handling and concurrent mode isolation. Document unsupported combinations explicitly.
- #68 Codex rendering rejects Model/Tools; do not discard tier metadata to force success or infer native agent capability from skill-file rendering. Reuse main's `EnsureClaudeConfigDir` credential lifecycle; that directory alone does not suppress repository discovery and must not become a shared writable mode catalog.
- Extend pure renderers and neutral `agent.Request`; adapters exclusively own vendor syntax. Resolve after daemon mode selection in `turn`. Never place vendor config formats into daemon/domain contracts.
- Publish run-specific bundles through the existing write owner; cancellation cannot publish partial output. Capture locks are bounded and released before subprocess execution; deep-copy request data.
- Extend existing store segment allocation/read paths, removing assumptions that the first ordinal is always enough. Compare bundle identity before resume and preserve the agent-switch guard and existing final state ordering.
- Platform: existing contexts own subprocess/snapshot lifetime; no new clock or worker pool. Risk: ambient discovery and old conversation context can reintroduce excluded specialists despite correct file contents.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|------------|-----|
| T-3.1 | Prove exact-version capabilities and record production transport decision | infra | T-1.1 | L |
| T-3.2 | Adapt native rendering and immutable bundle publication | logic | T-1.3, T-1.4, T-3.1 | L |
| T-3.3 | Extend neutral request and runner adapters for proven transport | logic | T-3.2 | L |
| T-3.4 | Wire post-classification resolution and bundle-aware conversation segments | logic | T-2.1, T-3.3 | L |
| T-3.5 | Test classifier/work separation, resume rotation and concurrent bundles | test | T-3.3, T-3.4 | L |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-3.1 | Recorded real scratch evidence identifies supported CLI versions and proves selected catalog, auth, tier and permission behavior; fake argv tests alone cannot satisfy this gate. |
| AC-3.2 | Classifier requests/catalogs contain zero team specialists/skills; work sees precisely selected mode membership while retaining project instructions and session conventions. |
| AC-3.3 | Five concurrent runs with differing modes cannot overwrite/observe another run's native catalog; agents cannot modify source definitions or sibling bundles. |
| AC-3.4 | Stable identity resumes an eligible conversation; changed team/mode/content/model mapping or absent old identity allocates a fresh segment while preserving history and vendor-switch protection. |
| AC-3.5 | Failed validation, cancellation or unsupported transport prevents work execution or partial bundle publication, with an actionable reported outcome. |
| AC-3.6 | Deterministic fake-process/store tests cover cold/resumed launches, classifier isolation, segment metadata and concurrency without real paid subprocesses in ordinary tests. |
