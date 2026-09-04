# Plan Brief — we-need-to-rework-the-config-yaml-i-have-post-merge
# Branch: lf/50-we-need-to-rework-the-config-yaml-i-have
# Date: 2026-09-04
# Parent issue: https://github.com/marioatcloudvane/litefactory/issues/50
# Citation base: a7d58babf0a7bc0ad3c923f08f11656671f6fd88
# Research brief: RESEARCH_BRIEF.md

## Shared Decisions

- The canonical roots and multi-agent mode/profile design remain fixed; this plan covers only the post-merge delta.
- Four attachment ceilings move from Claude into optional global `settings`. Omission retains existing defaults; explicit zero and out-of-range values remain invalid.
- Research and Plan profiles default `attachments: true`; Implement and Validate default false; Spike remains Spike while borrowing Implement policy. Explicit canonical overrides remain supported.
- Codex mode-profile sandboxes are the sole sandbox source. `agents.codex.default_sandbox` is removed without alias or fallback.
- Obsolete old-profile replies, mappings, and guidance are removed without compatibility. The non-echoing `lf:tools=` refusal remains.
- Generated configuration remains the complete canonical reference and exposes ceilings and explicit attachment flags.
- No new packages, dependencies, providers, agents, migrations, interfaces, goroutines, or ingestion behavior.

## Workflow

- Shipping order: US-1, US-2, US-3, and US-5 establish independent schema/policy cleanup; US-4 integrates them into generation; US-6 proves routing; US-7 audits regressions and completes all gates/artifacts.
- Global limits, mode defaults, and Codex fallback removal must land before template and routing tests become authoritative.
- All stories ship together because strict decoding intentionally rejects hybrid legacy/canonical files.

## Cross-Story Constraints

- Platform: configuration owns YAML/defaults/validation, daemon owns routing, turn owns ingestion, report owns rendering, and runners consume resolved permissions.
- Preserve CFG-1 through CFG-6, SEC-1/3/5, strict removed-key rejection, aggregate validation, and no authority fallback.
- Use established table-driven suites/fakes (TEST-1/2/3/7), quiescence rather than sleeps (TEST-8/9), and retain design-enforcement tests (TEST-10).
- Copy permission slices across boundaries (CONC-1); add no goroutines or test observables.
- No live agent execution (EXT-1), `make build`, `dist/` writes, or runtime-state mutation.
- Ring-fenced: attachment fetching/storage/manifests, classifier behavior, provider expansion, config migration, dependencies, lint configuration, and unrelated cleanup.

## Story Index

| ID | Title | Size | Depends On | Sub-issue |
|----|-------|------|-----------|-----------|
| US-1 | Global optional attachment ceilings | M | — | *(filled by litefactory after sub-issue creation)* |
| US-2 | Mode-specific attachment defaults | S | — | *(filled by litefactory after sub-issue creation)* |
| US-3 | Profile-only Codex sandbox | M | — | *(filled by litefactory after sub-issue creation)* |
| US-4 | Complete generated configuration | M | US-1, US-2, US-3 | *(filled by litefactory after sub-issue creation)* |
| US-5 | Remove obsolete profile reply surfaces | S | — | *(filled by litefactory after sub-issue creation)* |
| US-6 | Attachment routing coverage | M | US-1, US-2, US-3 | *(filled by litefactory after sub-issue creation)* |
| US-7 | Regression and delivery closure | L | US-1–US-6 | *(filled by litefactory after sub-issue creation)* |

## Out of Scope

- Reopening the canonical hierarchy or mode-selected permissions.
- Compatibility aliases or automatic migration for removed keys/replies.
- Changing attachment limits, download behavior, storage, manifests, or failures.
- New providers, agents, database work, infrastructure, dependencies, or integration tests.

## Provenance

- Citation base: `a7d58babf0a7bc0ad3c923f08f11656671f6fd88`; runtime delta audited at `3003244b9d11b6644e1a21d4932ed84243fe8043`.
- Known-stale areas: attachment ceilings under Claude, incomplete generated template, legacy profile comments/replies, and redundant Codex default sandbox.
- Re-pin all delta-touched config/daemon/turn/report files before merge and run the full gate after final fixes.

