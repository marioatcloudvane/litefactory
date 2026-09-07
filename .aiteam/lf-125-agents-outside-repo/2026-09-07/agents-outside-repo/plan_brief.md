# Plan Brief — agents-outside-repo
# Branch: lf/125-agents-outside-repo
# Date: 2026-09-07
# Parent issue: https://github.com/marioatcloudvane/litefactory/issues/125
# Citation base: 832c730882a13176f63e1e8eb1bd985150199fed
# Research brief: [historical research and delivery diagnosis](RESEARCH_BRIEF.md)

## Shared Decisions

- Run 0013 supersedes embedded distribution and the old hard Codex capability prerequisite. Extend implemented central registry/management/repository selection; do not repeat completed US-1/US-2 work. Remote HTTPS distribution is primary and independent of litefactory builds. Explicit local packages remain development/offline imports only. Remove production embedded content entirely, with no seed/runtime fallback.
- Installed sources stay under `$LF_HOME/teams`, management provenance under `teams/.litefactory`, verified downloads under a `config.Paths`-owned `$LF_HOME/cache/teams`, and immutable bundles under `$LF_HOME/bundles/<forge>/<owner>/<repo>/<issue>/<run-ordinal>/`. Repositories select installed team IDs; repository text/agent output cannot choose trust sources. App startup/update never installs or refreshes teams.
- Operator `teams.sources` maps aliases to HTTPS discovery URLs and optional secret references; `teams.default_source` selects one alias. No first-party endpoint is verified: ship an unset default until configured. Add without a source uses that default or gives setup guidance, never startup failure. Corporate hosting uses the same protocol. Direct URLs receive no ambient credentials. Record resolved source URL; changing alias/default never silently retargets an installation.
- Distribution schema 1: discovery index → immutable per-team/version manifest → relative files. Retain catalog schema 2, ordered five-mode rosters and complete skill/resource closure. Discovery maps team/version to manifest path and exact-byte SHA-256 and explicitly names stable. Manifest repeats team/version/schema and lists all relative paths, sizes and hashes; exactly one payload `index.json` describes one team's closure. Resolve manifest refs from discovery's directory and file refs from manifest's directory, always beneath the configured index directory and exact HTTPS origin. No archives/hooks/registry server.
- Content versions use strict SemVer 2 independently of schemas and application releases. Stable policy records a concrete installed version/digest; exact `--version` remains exact until explicit change, may select prereleases and may deliberately downgrade. Stable never rolls back silently. Reject leading v/ranges; build metadata does not affect precedence and equal-precedence variants are never selected arbitrarily.
- Optional independently supplied manifest SHA-256 pins persist: moving releases requires explicit replacement/clearing with an exact version. Retain verified source/team/version digest history outside evictable blobs; changed same-version bytes always fail, including force. Force authorizes local-edit overwrite only. Newer version with identical bytes is a version difference, not byte drift.
- Preserve five-state comparison order: missing local → missing; local equals candidate → in-sync; local equals baseline → upstream-updated; candidate equals baseline → locally-modified; otherwise conflict. Freshness is separate: never-checked/fresh/stale/unavailable, last attempt/success/error and identified verified candidate. Twenty-four hours changes display only. Unknown upstream cannot appear verified in-sync; cached comparisons disclose age/candidate. Discovery-only version evidence does not establish file drift.
- Fetch only on explicit add/update/status refresh. Ordinary status/list/show/validate, daemon startup, classifiers, work and resume make zero source requests. Installed valid content works indefinitely offline without cache availability. Failed refresh preserves last verified observations and reports failure; failed add/update never silently selects an older cached release. Removed upstream releases never automatically delete installed sources.
- Require normal TLS validation; reject HTTP, userinfo, fragments and credential-bearing queries. At most three redirects, exact HTTPS origin including effective port and distribution-root containment. Reject unsafe/absolute/traversal/encoded-escape/duplicate paths, symlinks, nonregular entries and undeclared dependencies. Bounds: 1 MiB discovery, 1 MiB manifest, 1 MiB/file, 16 MiB total, 1,024 files, 120 seconds or earlier caller cancellation; count actual decoded bytes.
- Verify manifest against discovery/operator pin, every file size/hash and complete closure before cache/publication; reverify cached bytes. Source choice plus TLS is initial publisher trust; same-source hashes are not independent authentication. Source-specific secrets resolve only at acquisition, never in URLs/logs/provenance/cache/bundles/child environments. Remote instructions cannot alter local permission ceilings/settings or install plugins.
- Reuse `team` validation/write guards, `teamsource` composition/snapshot/provenance/overrides, pure `render`, commands, association and conversation machinery. Narrow `internal/teamremote` owns acquisition/protocol/version/cache, wired by CLI; no network in config/render/store/runners/startup. Fetch/validate outside lock, recheck installed baseline inside lock, preserve shared conflicts/overrides. Add complete-installation recovery/generation publication; individual file rename alone does not prove crash atomicity.
- After mode selection snapshot installed sources, release lock, render a distinct immutable bundle and send a vendor-neutral request to the CLI in the task worktree. Classifiers receive no team catalog. Source/version/manifest provenance and effective override/dependency/render/bootstrap/model bytes define identity; changed identity opens a fresh existing conversation segment. Keep durable metadata and valid/restorable resume resources; no shared writable catalogs or unsafe finalized-bundle TTL deletion.
- Exact rosters: research = research-orchestrator, codebase-tour, web-researcher; plan = plan-orchestrator, requirements-engineer, go-architect, proxy-product-owner; implement = implement-orchestrator, go-implementation-engineer, go-unit-test-engineer, go-integration-test-engineer; validate = go-code-reviewer; spike = empty. Preserve known main fix `6eb29bf`. Daemon mode selection replaces redundant root routing, not orchestration. Selected skills supply language detail; small bootstrap retains project/session guidance without full-body duplication.
- Implement owns production/test authoring and verification. Validate returns inline acceptance/code/evidence/context-drift findings under unchanged read-only permissions, no shell/test execution expansion. No retired updater, automatic remediation, new Maintenance mode or extra language stacks.
- Owner-reported Codex 0.153.4 success outside the outer sandbox supersedes run 0011's hard stop. Adapter coding has no nested-probe prerequisite. Exact-version auth, delegation, permissions, tiers, concurrent isolation and resume remain suitable-environment delivery acceptance. Environment denial means missing evidence there, not unsupported transport; never bypass the outer sandbox.

## Workflow

- US-1/US-2 centralization is implemented. Revised tasks extend release/source contracts and remote acquisition/management. Assigned-team refusal remains unfinished US-3 work, not intended final behavior.
- Start US-1, then US-2 consumer management. US-3 adapter coding proceeds after shared snapshot/provenance contracts without waiting for external probes. US-4 content preparation starts after US-1 and incorporates the known main roster correction.
- Preserve the historical full-catalog benchmark fixtures before US-4 changes content; comparative measurements remain US-5 work after delivery exists.
- US-4/US-6 form a coordinated content cutover: T-4.1 → T-6.1 → T-4.4; US-2 commands are required before embedded acquisition removal. US-6 roundtrip acceptance uses those commands. This task order avoids a circular story dependency.
- Shipping groups: US-1 → US-2; US-3 may proceed after US-1; US-4 + US-6 complete with US-2/US-3 integration; US-5 accepts distribution/delivery/content together. Publisher artifacts and default/corporate setup instructions are required; an unprovisioned public URL is not claimed deployed.
- Authorized litefactory publication must synchronize #127–#131, create US-6 from its complete local body and verify links/read-back before forge bodies are consumed. Planning artifacts do not prove that publication happened.

## Cross-Story Constraints

- Preserve paths in config.Paths, naming in naming, SQL/migrations in store, git CLI in git, process options in adapters, clock/mode in daemon and single-turn sequencing in turn. No compatibility reader/converter/backfill; additive store migrations for new conversation metadata remain valid.
- Follow APP_CONSTRAINTS including config purity, guarded writes, redaction, permission ceilings, CONC-4/7 ordering and SD-4 Claude authentication isolation. Preserve overrides, unrelated teams, active bundles and source identity.
- Tests use scratch roots, TLS fixtures, fake runners, existing SQLite ownership and deterministic concurrency/fault injection. No normal tests require public Internet/paid CLIs; no invented integration tags. Finish make check and safe go build ./...; report current failures from evidence, not old counts.
- This plan does not authorize live LF_HOME/registered clone/dist/daemon changes, trust bypasses, commits/pushes, public hosting deployment or remote issue edits by the planning agent.

## Story Index

| ID | Title | Size | Depends On | Sub-issue |
|----|-------|------|------------|-----------|
| US-1 | Extend central registry with remote release and source contracts | L | — | https://github.com/marioatcloudvane/litefactory/issues/127 |
| US-2 | Fetch, update and inspect trusted remote teams | L | US-1 | https://github.com/marioatcloudvane/litefactory/issues/128 |
| US-3 | CLI bundle delivery, lifecycle and conversation isolation | L | US-1 | https://github.com/marioatcloudvane/litefactory/issues/129 |
| US-4 | Preserve orchestrators and cut over focused external content | L | US-1; ships with US-2, US-3, US-6 | https://github.com/marioatcloudvane/litefactory/issues/130 |
| US-5 | Provenance, token evidence and end-to-end handoff | M | US-2, US-3, US-4, US-6 | https://github.com/marioatcloudvane/litefactory/issues/131 |
| US-6 | Publish independent versioned team releases | L | US-1; US-4 source preparation; US-2 roundtrip | https://github.com/marioatcloudvane/litefactory/issues/154 |

## Out of Scope

- Backward compatibility/conversion of unused team formats, embedded seeds/fallback, unattended updates, repository-owned registries, new hosting service and archive distribution.
- Signature/key rotation infrastructure, arbitrary plugin installation, second model registry and extra language stacks.
- Writable Validate, Maintenance mode, automatic remediation/transitions and retired context updater.
- Guaranteed token savings before measurement, production hosting deployment in this run and general issue synchronization changes.

## Provenance

- Run 0013 baseline: 832c730882a13176f63e1e8eb1bd985150199fed. Current source and implemented foundation supersede embedded/local assumptions. feature-spec.md and ARCH_REVIEW.md carry approved requirements/review and task guidance.
- 6eb29bf2a66ea9ee80a99130845cf2fcfb7bb10a is verified on local main but not task ancestry; it contains the exact first-position orchestrator arrays. Integrate/preserve this known correction when reconciling main; no branch merge occurred here.
- Outside-sandbox Codex success is owner-supplied evidence, not a probe repeated here. Historical RUNNER_CAPABILITIES/DRIFT/TEST/RESEARCH reports describe their runs, not current blockers. EPERM-first dependency is withdrawn.
- US-1/US-2 centralization is delivered foundation; remote acquisition/publishing are new remaining deltas. US-3 delivery and US-4/US-5 workflow/evidence remain incomplete. No implementation, CLI probe or product test occurred in this Plan run.
- Existing #127–#131 links retain ownership; earlier runs confirmed stale bodies and publication rejection. No remote synchronization is claimed. US-6 has a complete local creation body and no known forge URL: publisher must fill its pending link. This is an outstanding publication handoff, not a fabricated URL.
- Re-pin affected APIs at implementation start and distinguish independently publishable artifacts from an actually configured public endpoint.
